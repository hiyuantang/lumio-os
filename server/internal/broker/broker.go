// SPDX-License-Identifier: AGPL-3.0-only
package broker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
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
	"strings"
	"sync"
	"time"

	"lumio-os/server/internal/ipc"
	"lumio-os/server/internal/privfiles"
	"lumio-os/server/internal/updates"
)

const (
	servicesManageActionID = "os.lumio.services.manage"
	packagesApplyActionID  = "os.lumio.packages.apply"
	filesWriteActionID     = "os.lumio.files.write-privileged"
)

var unitNamePattern = regexp.MustCompile(`^[a-zA-Z0-9@:._\-]+\.service$`)

var serviceActions = map[string]bool{
	"services.start":   true,
	"services.stop":    true,
	"services.restart": true,
	"services.reload":  true,
	"services.enable":  true,
	"services.disable": true,
}

var updateActions = map[string]bool{
	"updates.refresh":    true,
	"updates.plan":       true,
	"packages.applyPlan": true,
}

var fileActions = map[string]bool{
	"files.writePrivileged": true,
}

type ActionRequest struct {
	RequestID string `json:"requestId"`
	Action    string `json:"action"`
	Arguments struct {
		Unit          string `json:"unit"`
		PlanID        string `json:"planId"`
		Path          string `json:"path"`
		ContentBase64 string `json:"contentBase64"`
		Mode          string `json:"mode"`
		RestartUnit   string `json:"restartUnit"`
	} `json:"arguments"`
	Expected *struct {
		ActiveState string `json:"activeState"`
		PlanID      string `json:"planId"`
		Revision    string `json:"revision"`
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
	RollbackDir    string
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
	updates  *updates.Worker
	files    *privfiles.Writer
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
	rollbackDir := cfg.RollbackDir
	if rollbackDir == "" {
		rollbackDir = "/var/lib/lumio/rollback/files"
	}
	s := &Server{
		cfg:      cfg,
		audit:    audit,
		authz:    authz,
		sessiond: ipc.HTTPClient(cfg.SessiondSocket),
		updates:  updates.NewWorker(),
		files:    privfiles.NewWriter(rollbackDir),
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
	mux.HandleFunc("GET /updates/progress", s.handleUpdateProgress)
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
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(&req); err != nil {
		s.writeErr(w, http.StatusBadRequest, &apiError{Code: "validation_failed", Message: "Body must be a JSON object."})
		return
	}
	if apiErr := req.validate(); apiErr != nil {
		s.writeErr(w, http.StatusBadRequest, apiErr)
		return
	}
	if serviceActions[req.Action] && s.sys == nil {
		s.writeErr(w, http.StatusServiceUnavailable, &apiError{Code: "unavailable", Message: "systemd is unavailable"})
		return
	}
	if fileActions[req.Action] && s.files == nil {
		s.writeErr(w, http.StatusServiceUnavailable, &apiError{Code: "unavailable", Message: "protected file editing is unavailable"})
		return
	}
	if fileActions[req.Action] && req.Arguments.RestartUnit != "" && s.sys == nil {
		s.writeErr(w, http.StatusServiceUnavailable, &apiError{Code: "unavailable", Message: "systemd is unavailable"})
		return
	}
	if updateActions[req.Action] && (s.updates == nil || !s.updates.Available()) {
		s.writeErr(w, http.StatusServiceUnavailable, &apiError{Code: "unavailable", Message: "the package manager is unavailable"})
		return
	}

	if status, body, ok := s.audit.StoredResult(req.RequestID); ok {
		w.Header().Set("X-Lumio-Idempotent-Replay", "true")
		s.writeRaw(w, status, body)
		return
	}
	if req.Action == "packages.applyPlan" {
		if progress, ok := s.updates.Progress(req.RequestID); ok {
			w.Header().Set("X-Lumio-Idempotent-Replay", "true")
			s.writeData(w, map[string]any{"requestId": progress.RequestID, "planId": progress.PlanID})
			return
		}
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

	if serviceActions[req.Action] {
		s.handleServiceAction(w, r, req, uid, userName, polkitResult)
		return
	}
	if updateActions[req.Action] {
		s.handleUpdateAction(w, r, req, uid, userName, polkitResult)
		return
	}
	s.handlePrivilegedFileAction(w, r, req, uid, userName, polkitResult)
}

func (s *Server) handlePrivilegedFileAction(w http.ResponseWriter, r *http.Request, req ActionRequest, uid uint32, userName, polkitResult string) {
	content, err := base64.StdEncoding.DecodeString(req.Arguments.ContentBase64)
	if err != nil {
		s.writeErr(w, http.StatusBadRequest, &apiError{Code: "validation_failed", Message: "contentBase64 must be valid base64."})
		return
	}
	beginID := s.audit.Begin(req, uid, userName, polkitResult)
	started := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	result, err := s.files.Write(ctx, req.Arguments.Path, content, req.Expected.Revision, req.Arguments.Mode, req.RequestID)
	if err != nil {
		status := http.StatusInternalServerError
		apiErr := &apiError{Code: "internal", Message: "The protected file could not be saved."}
		switch {
		case errors.Is(err, privfiles.ErrValidation):
			status = http.StatusBadRequest
			apiErr = &apiError{Code: "validation_failed", Message: err.Error()}
		case errors.Is(err, privfiles.ErrStale):
			status = http.StatusConflict
			apiErr = &apiError{Code: "stale_revision", Message: "The file changed on disk since it was read."}
			var stale *privfiles.StaleError
			if errors.As(err, &stale) {
				apiErr.Details = map[string]any{"expectedRevision": stale.Expected, "actualRevision": stale.Actual}
			}
		case errors.Is(err, os.ErrPermission):
			status = http.StatusForbidden
			apiErr = &apiError{Code: "forbidden", Message: "Permission denied."}
		}
		s.audit.End(beginID, req, uid, userName, polkitResult, "failed", err.Error(), nil, time.Since(started))
		s.writeErr(w, status, apiErr)
		return
	}
	data := map[string]any{"file": result}
	if req.Arguments.RestartUnit != "" {
		unit, restartErr := s.sys.execute(ctx, "services.restart", req.Arguments.RestartUnit)
		if restartErr != nil {
			data["restart"] = map[string]any{"success": false, "error": restartErr.Error()}
		} else {
			data["restart"] = map[string]any{"success": true, "unit": unit}
		}
	}
	s.audit.End(beginID, req, uid, userName, polkitResult, "success", "", data, time.Since(started))
	s.writeData(w, data)
}

func (s *Server) handleServiceAction(w http.ResponseWriter, r *http.Request, req ActionRequest, uid uint32, userName, polkitResult string) {
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

func (s *Server) handleUpdateAction(w http.ResponseWriter, r *http.Request, req ActionRequest, uid uint32, userName, polkitResult string) {
	beginID := s.audit.Begin(req, uid, userName, polkitResult)
	started := time.Now()
	switch req.Action {
	case "updates.refresh":
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
		defer cancel()
		result, err := s.updates.Refresh(ctx)
		if err != nil {
			s.writeUpdateError(w, beginID, req, uid, userName, polkitResult, started, err)
			return
		}
		data := map[string]any{"refreshedAt": result.RefreshedAt}
		s.audit.End(beginID, req, uid, userName, polkitResult, "success", "", data, time.Since(started))
		s.writeData(w, data)
	case "updates.plan":
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
		defer cancel()
		plan, err := s.updates.CalculatePlan(ctx)
		if err != nil {
			s.writeUpdateError(w, beginID, req, uid, userName, polkitResult, started, err)
			return
		}
		data := map[string]any{"plan": plan}
		s.audit.End(beginID, req, uid, userName, polkitResult, "success", "", data, time.Since(started))
		s.writeData(w, data)
	case "packages.applyPlan":
		progress, _, err := s.updates.StartApply(req.Arguments.PlanID, req.Expected.PlanID, req.RequestID, func(progress updates.Progress) {
			outcome := "failed"
			if progress.Success {
				outcome = "success"
			}
			result := map[string]any{"requestId": progress.RequestID, "planId": progress.PlanID, "progress": progress}
			s.audit.End(beginID, req, uid, userName, polkitResult, outcome, progress.Error, result, time.Since(started))
		})
		if err != nil {
			s.writeUpdateError(w, beginID, req, uid, userName, polkitResult, started, err)
			return
		}
		s.writeData(w, map[string]any{"requestId": progress.RequestID, "planId": progress.PlanID})
	}
}

func (s *Server) writeUpdateError(w http.ResponseWriter, beginID int64, req ActionRequest, uid uint32, userName, polkitResult string, started time.Time, err error) {
	status := http.StatusInternalServerError
	apiErr := &apiError{Code: "internal", Message: "The package operation failed: " + err.Error()}
	switch {
	case errors.Is(err, updates.ErrUnavailable):
		status = http.StatusServiceUnavailable
		apiErr = &apiError{Code: "unavailable", Message: "The package manager is unavailable."}
	case errors.Is(err, updates.ErrBusy):
		status = http.StatusConflict
		apiErr = &apiError{Code: "busy", Message: "Another package operation is in progress."}
	case errors.Is(err, updates.ErrPlanMissing), errors.Is(err, updates.ErrPlanStale):
		status = http.StatusConflict
		apiErr = &apiError{Code: "conflict", Message: "The saved update plan is missing or expired. Calculate it again."}
	}
	s.audit.End(beginID, req, uid, userName, polkitResult, "failed", err.Error(), nil, time.Since(started))
	s.writeErr(w, status, apiErr)
}

func (s *Server) handleUpdateProgress(w http.ResponseWriter, r *http.Request) {
	conn, ok := r.Context().Value(connKey{}).(net.Conn)
	if !ok {
		s.writeErr(w, http.StatusInternalServerError, &apiError{Code: "internal", Message: "peer credentials unavailable"})
		return
	}
	uid, _, err := ipc.PeerCreds(conn)
	if err != nil {
		s.writeErr(w, http.StatusInternalServerError, &apiError{Code: "internal", Message: "peer credentials unavailable"})
		return
	}
	requestID := r.URL.Query().Get("requestId")
	if requestID == "" || len(requestID) > 128 {
		s.writeErr(w, http.StatusBadRequest, &apiError{Code: "validation_failed", Message: "requestId is required."})
		return
	}
	if !s.audit.RequestOwned(requestID, uid) {
		s.writeErr(w, http.StatusNotFound, &apiError{Code: "not_found", Message: "update progress was not found"})
		return
	}
	if s.updates == nil {
		s.writeErr(w, http.StatusServiceUnavailable, &apiError{Code: "unavailable", Message: "the package manager is unavailable"})
		return
	}
	progress, ok := s.updates.Progress(requestID)
	if !ok {
		s.writeErr(w, http.StatusNotFound, &apiError{Code: "not_found", Message: "update progress was not found"})
		return
	}
	s.writeData(w, progress)
}

func (s *Server) authorize(ctx context.Context, uid, pid uint32, req ActionRequest) (string, *apiError) {
	actionID := servicesManageActionID
	details := map[string]string{"unit": req.Arguments.Unit}
	if updateActions[req.Action] {
		actionID = packagesApplyActionID
		details = map[string]string{"planId": req.Arguments.PlanID}
	}
	if fileActions[req.Action] {
		actionID = filesWriteActionID
		details = map[string]string{"path": req.Arguments.Path}
	}
	result, err := s.authz.Check(ctx, uid, pid, actionID, details)
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
			Details: map[string]any{"actionId": actionID},
		}
	case Challenge:
		if s.reauthFresh(ctx, uid, req.SessionToken) {
			return "challenge+reauth", nil
		}
		return "challenge", &apiError{
			Code:    "forbidden",
			Message: "The action requires reauthentication.",
			Details: map[string]any{"actionId": actionID, "reauthRequired": true},
		}
	}
	return "deny", &apiError{Code: "forbidden", Message: "The action was denied by the authorisation policy.", Details: map[string]any{"actionId": actionID}}
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
	if !serviceActions[req.Action] && !updateActions[req.Action] && !fileActions[req.Action] {
		return &apiError{Code: "validation_failed", Message: "unknown action."}
	}
	if serviceActions[req.Action] && !unitNamePattern.MatchString(req.Arguments.Unit) {
		return &apiError{Code: "validation_failed", Message: "invalid unit name."}
	}
	if req.Expected != nil && req.Expected.ActiveState != "" {
		switch req.Expected.ActiveState {
		case "active", "inactive", "failed", "activating", "deactivating", "reloading":
		default:
			return &apiError{Code: "validation_failed", Message: "invalid expected activeState."}
		}
	}
	if req.Action == "packages.applyPlan" {
		if !validPlanID(req.Arguments.PlanID) {
			return &apiError{Code: "validation_failed", Message: "invalid planId."}
		}
		if req.Expected == nil || req.Expected.PlanID != req.Arguments.PlanID {
			return &apiError{Code: "validation_failed", Message: "expected planId must match arguments.planId."}
		}
	}
	if req.Action == "files.writePrivileged" {
		if req.Arguments.Path == "" || !filepath.IsAbs(req.Arguments.Path) || filepath.Clean(req.Arguments.Path) != req.Arguments.Path || !strings.HasPrefix(req.Arguments.Path, "/etc/") {
			return &apiError{Code: "validation_failed", Message: "path must be a canonical path below /etc."}
		}
		if req.Expected == nil || !revisionPattern.MatchString(req.Expected.Revision) {
			return &apiError{Code: "validation_failed", Message: "expected revision is required."}
		}
		content, err := base64.StdEncoding.DecodeString(req.Arguments.ContentBase64)
		if err != nil || len(content) > privfiles.MaxWriteBytes {
			return &apiError{Code: "validation_failed", Message: "contentBase64 must contain at most 1 MiB."}
		}
		if req.Arguments.Mode != "" && !modePattern.MatchString(req.Arguments.Mode) {
			return &apiError{Code: "validation_failed", Message: "invalid mode."}
		}
		if req.Arguments.RestartUnit != "" && !unitNamePattern.MatchString(req.Arguments.RestartUnit) {
			return &apiError{Code: "validation_failed", Message: "invalid restart unit."}
		}
	}
	return nil
}

var planIDPattern = regexp.MustCompile(`^pln_[a-f0-9]{24}$`)
var revisionPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
var modePattern = regexp.MustCompile(`^0[0-6][0-4][0-4]$`)

func validPlanID(value string) bool {
	return planIDPattern.MatchString(value)
}

func argumentsHash(req ActionRequest) string {
	h := sha256.New()
	h.Write([]byte(req.Action))
	h.Write([]byte{0})
	encoded, _ := json.Marshal(req.Arguments)
	h.Write(encoded)
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
