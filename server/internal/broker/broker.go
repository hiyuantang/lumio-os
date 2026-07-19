// SPDX-License-Identifier: AGPL-3.0-only
package broker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"lumio-os/server/internal/ipc"
)

const servicesManageActionID = "os.lumio.services.manage"

var unitNamePattern = regexp.MustCompile(`^[a-zA-Z0-9@:._\-]+\.service$`)

var serviceActions = map[string]bool{
	"services.start":   true,
	"services.stop":    true,
	"services.restart": true,
	"services.enable":  true,
	"services.disable": true,
}

type ActionRequest struct {
	RequestID string `json:"requestId"`
	Action    string `json:"action"`
	Arguments struct {
		Unit string `json:"unit"`
	} `json:"arguments"`
	Expected *struct {
		ActiveState string `json:"activeState"`
	} `json:"expected"`
	SessionToken string `json:"sessionToken"`
}

type apiError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

type Config struct {
	SocketPath     string
	DBPath         string
	SessiondSocket string
	Authorizer     Authorizer
}

type systemdIface interface {
	unitState(ctx context.Context, unit string) (UnitState, error)
	execute(ctx context.Context, action, unit string) (UnitState, error)
}

type Server struct {
	cfg      Config
	audit    *Audit
	authz    Authorizer
	sys      systemdIface
	sessiond *http.Client

	unitLocks sync.Map
}

func New(cfg Config) (*Server, error) {
	audit, err := OpenAudit(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("open audit db: %w", err)
	}
	sys, err := newSystemdClient()
	if err != nil {
		log.Printf("broker: systemd D-Bus unavailable: %v", err)
	}
	authz := cfg.Authorizer
	if authz == nil {
		authz = newPolkitAuthorizer()
	}
	s := &Server{
		cfg:      cfg,
		audit:    audit,
		authz:    authz,
		sessiond: ipc.HTTPClient(cfg.SessiondSocket),
	}
	if err == nil {
		s.sys = sys
	}
	return s, nil
}

func (s *Server) Run() error {
	if err := os.MkdirAll(filepath.Dir(s.cfg.SocketPath), 0o755); err != nil {
		return err
	}
	_ = os.Remove(s.cfg.SocketPath)
	ln, err := net.Listen("unix", s.cfg.SocketPath)
	if err != nil {
		return err
	}
	if err := os.Chmod(s.cfg.SocketPath, 0o666); err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /action", s.handleAction)
	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			return context.WithValue(ctx, connKey{}, c)
		},
	}
	log.Printf("broker: listening on %s", s.cfg.SocketPath)
	return srv.Serve(ln)
}

type connKey struct{}

func (s *Server) handleAction(w http.ResponseWriter, r *http.Request) {
	uid, pid, err := ipc.PeerCreds(r.Context().Value(connKey{}).(net.Conn))
	if err != nil {
		s.writeErr(w, http.StatusInternalServerError, &apiError{Code: "internal", Message: "peer credentials unavailable"})
		return
	}
	var req ActionRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		s.writeErr(w, http.StatusBadRequest, &apiError{Code: "validation_failed", Message: "Body must be a JSON object."})
		return
	}
	if apiErr := req.validate(); apiErr != nil {
		s.writeErr(w, http.StatusBadRequest, apiErr)
		return
	}
	if s.sys == nil {
		s.writeErr(w, http.StatusServiceUnavailable, &apiError{Code: "unavailable", Message: "systemd is unavailable"})
		return
	}

	if status, body, ok := s.audit.StoredResult(req.RequestID); ok {
		w.Header().Set("X-Lumio-Idempotent-Replay", "true")
		s.writeRaw(w, status, body)
		return
	}

	userName := nameForUID(uid)
	polkitResult, apiErr := s.authorize(r.Context(), uid, pid, req)
	if apiErr != nil {
		status := http.StatusForbidden
		if apiErr.Code == "unavailable" {
			status = http.StatusServiceUnavailable
		} else {
			s.audit.Deny(req, uid, userName, polkitResult, apiErr)
		}
		s.writeErr(w, status, apiErr)
		return
	}

	unlock := s.lockUnit(req.Arguments.Unit)
	defer unlock()

	if req.Expected != nil && req.Expected.ActiveState != "" {
		state, err := s.sys.unitState(r.Context(), req.Arguments.Unit)
		if err != nil {
			s.writeErr(w, http.StatusInternalServerError, &apiError{Code: "internal", Message: "reading unit state failed"})
			return
		}
		if state.ActiveState != req.Expected.ActiveState {
			s.writeErr(w, http.StatusConflict, &apiError{
				Code:    "conflict",
				Message: "The unit state does not match the expected state.",
				Details: map[string]any{"expectedActiveState": req.Expected.ActiveState, "actualActiveState": state.ActiveState},
			})
			return
		}
	}

	beginID := s.audit.Begin(req, uid, userName, polkitResult)
	started := time.Now()
	unit, execErr := s.sys.execute(r.Context(), req.Action, req.Arguments.Unit)
	duration := time.Since(started)

	if execErr != nil {
		apiErr := &apiError{Code: "internal", Message: "The action failed: " + execErr.Error()}
		s.audit.End(beginID, req, uid, userName, polkitResult, "failed", execErr.Error(), nil, duration)
		s.writeErr(w, http.StatusInternalServerError, apiErr)
		return
	}
	result := map[string]any{"unit": unit}
	s.audit.End(beginID, req, uid, userName, polkitResult, "success", "", result, duration)
	s.writeData(w, map[string]any{"unit": unit})
}

func (s *Server) authorize(ctx context.Context, uid, pid uint32, req ActionRequest) (string, *apiError) {
	result, err := s.authz.Check(ctx, uid, pid, servicesManageActionID, map[string]string{"unit": req.Arguments.Unit})
	if err != nil {
		log.Printf("broker: authz check failed: %v", err)
		return "error", &apiError{Code: "unavailable", Message: "polkit is unavailable"}
	}
	switch result {
	case Allow:
		return "allow", nil
	case Deny:
		return "deny", &apiError{
			Code:    "forbidden",
			Message: "The action was denied by the authorisation policy.",
			Details: map[string]any{"actionId": servicesManageActionID},
		}
	case Challenge:
		if s.reauthFresh(ctx, uid, req.SessionToken) {
			return "challenge+reauth", nil
		}
		return "challenge", &apiError{
			Code:    "forbidden",
			Message: "The action requires reauthentication.",
			Details: map[string]any{"actionId": servicesManageActionID, "reauthRequired": true},
		}
	}
	return "deny", &apiError{Code: "forbidden", Message: "The action was denied by the authorisation policy.", Details: map[string]any{"actionId": servicesManageActionID}}
}

func (s *Server) reauthFresh(ctx context.Context, uid uint32, token string) bool {
	if token == "" {
		return false
	}
	var resp struct {
		UID         uint32 `json:"uid"`
		ReauthUntil int64  `json:"reauthUntil"`
	}
	if err := postJSON(ctx, s.sessiond, "http://sessiond/session/check", map[string]any{"token": token}, &resp); err != nil {
		return false
	}
	return resp.UID == uid && time.Now().UnixMilli() < resp.ReauthUntil
}

func (s *Server) lockUnit(unit string) func() {
	value, _ := s.unitLocks.LoadOrStore(unit, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func (s *Server) writeData(w http.ResponseWriter, data any) {
	s.writeRaw(w, http.StatusOK, map[string]any{"ok": true, "data": data})
}

func (s *Server) writeErr(w http.ResponseWriter, status int, apiErr *apiError) {
	s.writeRaw(w, status, map[string]any{"ok": false, "error": apiErr})
}

func (s *Server) writeRaw(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (req *ActionRequest) validate() *apiError {
	if req.RequestID == "" || len(req.RequestID) > 128 {
		return &apiError{Code: "validation_failed", Message: "requestId is required."}
	}
	if !serviceActions[req.Action] {
		return &apiError{Code: "validation_failed", Message: "unknown action."}
	}
	if !unitNamePattern.MatchString(req.Arguments.Unit) {
		return &apiError{Code: "validation_failed", Message: "invalid unit name."}
	}
	if req.Expected != nil && req.Expected.ActiveState != "" {
		switch req.Expected.ActiveState {
		case "active", "inactive", "failed", "activating", "deactivating", "reloading":
		default:
			return &apiError{Code: "validation_failed", Message: "invalid expected activeState."}
		}
	}
	return nil
}

func argumentsHash(req ActionRequest) string {
	h := sha256.New()
	h.Write([]byte(req.Action))
	h.Write([]byte{0})
	h.Write([]byte(req.Arguments.Unit))
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

func nameForUID(uid uint32) string {
	u, err := user.LookupId(fmt.Sprint(uid))
	if err != nil {
		return fmt.Sprint(uid)
	}
	return u.Username
}

func postJSON(ctx context.Context, client *http.Client, url string, body any, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("unexpected status " + resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
