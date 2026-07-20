// SPDX-License-Identifier: AGPL-3.0-only
package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"lumio-os/server/internal/broker"
	"lumio-os/server/internal/files"
	"lumio-os/server/internal/journal"
	"lumio-os/server/internal/network"
	"lumio-os/server/internal/privfiles"
	"lumio-os/server/internal/services"
	"lumio-os/server/internal/system"
)

func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	WriteData(w, map[string]any{
		"version":          s.deps.Version,
		"protocolVersions": []int{1},
	})
}

func (s *Server) handleIdentity(w http.ResponseWriter, _ *http.Request) {
	WriteData(w, system.ReadIdentity())
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	failed := 0
	if s.deps.Services != nil && s.deps.Services.Available() {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		if units, err := s.deps.Services.List(ctx); err == nil {
			for _, u := range units {
				if u.ActiveState == "failed" {
					failed++
				}
			}
		}
	}
	WriteData(w, system.CollectOverview(failed))
}

func (s *Server) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	WriteData(w, s.deps.Sampler.Sample())
}

func (s *Server) handleSystemPower(w http.ResponseWriter, r *http.Request) {
	if s.deps.BrokerSocket == "" {
		s.handleUnavailable(w, r)
		return
	}
	var req struct {
		RequestID string `json:"requestId"`
		Action    string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, NewError(CodeValidationFailed, "Body must be a JSON object."))
		return
	}
	if !validRequestID(req.RequestID) {
		WriteError(w, NewError(CodeValidationFailed, "requestId is required."))
		return
	}
	if req.Action != "reboot" && req.Action != "poweroff" {
		WriteError(w, NewError(CodeValidationFailed, "action must be reboot or poweroff."))
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"requestId":    req.RequestID,
		"action":       "system." + req.Action,
		"arguments":    map[string]any{},
		"expected":     map[string]any{},
		"sessionToken": r.Header.Get("X-Lumio-Session"),
	})
	s.forwardBrokerAction(w, r, payload, 15*time.Second)
}

func (s *Server) handleNetworkSnapshot(w http.ResponseWriter, r *http.Request) {
	if s.deps.Network == nil || !s.deps.Network.Available() {
		s.handleUnavailable(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	snapshot, err := s.deps.Network.Snapshot(ctx)
	if err != nil {
		WriteError(w, NewError(CodeUnavailable, "Network configuration is unavailable."))
		return
	}
	WriteData(w, snapshot)
}

func (s *Server) handleNetworkApply(w http.ResponseWriter, r *http.Request) {
	if s.deps.BrokerSocket == "" {
		s.handleUnavailable(w, r)
		return
	}
	var req struct {
		RequestID        string         `json:"requestId"`
		Config           network.Config `json:"config"`
		ExpectedRevision string         `json:"expectedRevision"`
		ConfirmTimeout   int            `json:"confirmTimeoutSec"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, NewError(CodeValidationFailed, "Body must be a JSON object."))
		return
	}
	if !validRequestID(req.RequestID) {
		WriteError(w, NewError(CodeValidationFailed, "requestId is required."))
		return
	}
	if !network.ValidRevision(req.ExpectedRevision) {
		WriteError(w, NewError(CodeValidationFailed, "expectedRevision is required."))
		return
	}
	if req.ConfirmTimeout == 0 {
		req.ConfirmTimeout = network.DefaultConfirmTimeout
	}
	if req.ConfirmTimeout < network.MinConfirmTimeout || req.ConfirmTimeout > network.MaxConfirmTimeout {
		WriteError(w, NewError(CodeValidationFailed, "confirmTimeoutSec must be between 30 and 300."))
		return
	}
	if err := network.ValidateConfig(req.Config); err != nil {
		WriteError(w, NewError(CodeValidationFailed, err.Error()))
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"requestId": req.RequestID,
		"action":    "network.applyWithRollback",
		"arguments": map[string]any{
			"config":            req.Config,
			"confirmTimeoutSec": req.ConfirmTimeout,
		},
		"expected":     map[string]any{"revision": req.ExpectedRevision},
		"sessionToken": r.Header.Get("X-Lumio-Session"),
	})
	s.forwardBrokerAction(w, r, payload, 45*time.Second)
}

func (s *Server) handleNetworkConfirm(w http.ResponseWriter, r *http.Request) {
	if s.deps.BrokerSocket == "" {
		s.handleUnavailable(w, r)
		return
	}
	var req struct {
		RequestID string `json:"requestId"`
		Token     string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, NewError(CodeValidationFailed, "Body must be a JSON object."))
		return
	}
	if !validRequestID(req.RequestID) {
		WriteError(w, NewError(CodeValidationFailed, "requestId is required."))
		return
	}
	if !networkConfirmToken.MatchString(req.Token) {
		WriteError(w, NewError(CodeValidationFailed, "token must be a network confirmation token."))
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"requestId":    req.RequestID,
		"action":       "network.confirm",
		"arguments":    map[string]any{"token": req.Token},
		"expected":     map[string]any{},
		"sessionToken": r.Header.Get("X-Lumio-Session"),
	})
	s.forwardBrokerAction(w, r, payload, 15*time.Second)
}

func (s *Server) handleServices(w http.ResponseWriter, r *http.Request) {
	if !s.deps.Services.Available() {
		WriteError(w, services.ErrUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	units, err := s.deps.Services.List(ctx)
	if err != nil {
		WriteError(w, err)
		return
	}
	if units == nil {
		units = []services.Unit{}
	}
	WriteData(w, map[string]any{"units": units})
}

func (s *Server) handleServiceDetail(w http.ResponseWriter, r *http.Request) {
	if !s.deps.Services.Available() {
		WriteError(w, services.ErrUnavailable)
		return
	}
	name := r.URL.Query().Get("name")
	if !actionUnitPattern.MatchString(name) {
		WriteError(w, NewError(CodeValidationFailed, "invalid unit name."))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	detail, err := s.deps.Services.Detail(ctx, name)
	if err != nil {
		WriteError(w, err)
		return
	}
	if detail.Documentation == nil {
		detail.Documentation = []string{}
	}
	if detail.Dependencies == nil {
		detail.Dependencies = []services.Dependency{}
	}
	if detail.Files == nil {
		detail.Files = []services.UnitFile{}
	}
	WriteData(w, detail)
}

func (s *Server) handleJournal(w http.ResponseWriter, r *http.Request) {
	if !s.deps.Journal.Available() {
		WriteError(w, journal.ErrUnavailable)
		return
	}
	values := r.URL.Query()
	q := journal.Query{
		Unit:     values.Get("unit"),
		Priority: values.Get("priority"),
		Since:    values.Get("since"),
		Boot:     values.Get("boot"),
		After:    values.Get("after-cursor"),
	}
	if raw := values.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil {
			WriteError(w, NewError(CodeValidationFailed, "limit must be an integer"))
			return
		}
		q.Limit = limit
	}
	if err := q.Validate(); err != nil {
		WriteError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	res, err := s.deps.Journal.Query(ctx, q)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteData(w, res)
}

func (s *Server) handleFilesList(w http.ResponseWriter, r *http.Request) {
	res, err := files.List(r.URL.Query().Get("path"))
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteData(w, res)
}

func (s *Server) handleFilesRead(w http.ResponseWriter, r *http.Request) {
	res, err := files.Read(r.URL.Query().Get("path"))
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteData(w, res)
}

func (s *Server) handleFilesWrite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path             string `json:"path"`
		Content          string `json:"content"`
		ExpectedRevision string `json:"expectedRevision"`
		RequestID        string `json:"requestId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, NewError(CodeValidationFailed, "Body must be a JSON object."))
		return
	}
	if !validRequestID(req.RequestID) {
		WriteError(w, NewError(CodeValidationFailed, "requestId is required."))
		return
	}
	content, err := base64.StdEncoding.DecodeString(req.Content)
	if err != nil {
		WriteError(w, NewError(CodeValidationFailed, "content must be base64."))
		return
	}
	if len(content) > files.MaxWriteBytes {
		WriteError(w, NewError(CodeValidationFailed, "content exceeds the 8 MiB limit for files.write."))
		return
	}
	s.mutate(w, req.RequestID, func(w http.ResponseWriter) {
		res, err := files.Write(req.Path, content, req.ExpectedRevision)
		if err != nil {
			WriteError(w, err)
			return
		}
		WriteData(w, res)
	})
}

func (s *Server) handleFilesDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path      string `json:"path"`
		RequestID string `json:"requestId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, NewError(CodeValidationFailed, "Body must be a JSON object."))
		return
	}
	if !validRequestID(req.RequestID) {
		WriteError(w, NewError(CodeValidationFailed, "requestId is required."))
		return
	}
	s.mutate(w, req.RequestID, func(w http.ResponseWriter) {
		res, err := files.Trash(req.Path)
		if err != nil {
			WriteError(w, err)
			return
		}
		WriteData(w, res)
	})
}

func (s *Server) handleFilesWritePrivileged(w http.ResponseWriter, r *http.Request) {
	if s.deps.BrokerSocket == "" {
		s.handleUnavailable(w, r)
		return
	}
	var req struct {
		Path             string `json:"path"`
		Content          string `json:"content"`
		ExpectedRevision string `json:"expectedRevision"`
		Mode             string `json:"mode"`
		RestartUnit      string `json:"restartUnit"`
		RequestID        string `json:"requestId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, NewError(CodeValidationFailed, "Body must be a JSON object."))
		return
	}
	if !validRequestID(req.RequestID) {
		WriteError(w, NewError(CodeValidationFailed, "requestId is required."))
		return
	}
	if req.Path == "" || !strings.HasPrefix(req.Path, "/etc/") {
		WriteError(w, NewError(CodeValidationFailed, "path must be below /etc."))
		return
	}
	content, err := base64.StdEncoding.DecodeString(req.Content)
	if err != nil || len(content) > privfiles.MaxWriteBytes {
		WriteError(w, NewError(CodeValidationFailed, "content must be base64 and no larger than 1 MiB."))
		return
	}
	if !regexp.MustCompile(`^sha256:[a-f0-9]{64}$`).MatchString(req.ExpectedRevision) {
		WriteError(w, NewError(CodeValidationFailed, "expectedRevision is required."))
		return
	}
	if req.RestartUnit != "" && !actionUnitPattern.MatchString(req.RestartUnit) {
		WriteError(w, NewError(CodeValidationFailed, "invalid restart unit."))
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"requestId": req.RequestID,
		"action":    "files.writePrivileged",
		"arguments": map[string]any{
			"path":          req.Path,
			"contentBase64": req.Content,
			"mode":          req.Mode,
			"restartUnit":   req.RestartUnit,
		},
		"expected":     map[string]any{"revision": req.ExpectedRevision},
		"sessionToken": r.Header.Get("X-Lumio-Session"),
	})
	s.forwardBrokerAction(w, r, payload, 45*time.Second)
}

var actionNamePattern = regexp.MustCompile(`^(start|stop|restart|reload|enable|disable)$`)
var actionUnitPattern = regexp.MustCompile(`^[a-zA-Z0-9@:._\-]+\.service$`)
var networkConfirmToken = regexp.MustCompile(`^[a-f0-9]{64}$`)

func (s *Server) handleServicesAction(w http.ResponseWriter, r *http.Request) {
	if s.deps.BrokerSocket == "" {
		WriteError(w, NewError(CodeUnavailable, "This capability is not available in this build."))
		return
	}
	var req struct {
		RequestID string `json:"requestId"`
		Action    string `json:"action"`
		Unit      string `json:"unit"`
		Expected  *struct {
			ActiveState string `json:"activeState"`
		} `json:"expected"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, NewError(CodeValidationFailed, "Body must be a JSON object."))
		return
	}
	if !validRequestID(req.RequestID) {
		WriteError(w, NewError(CodeValidationFailed, "requestId is required."))
		return
	}
	if !actionNamePattern.MatchString(req.Action) {
		WriteError(w, NewError(CodeValidationFailed, "unknown action."))
		return
	}
	if !actionUnitPattern.MatchString(req.Unit) {
		WriteError(w, NewError(CodeValidationFailed, "invalid unit name."))
		return
	}
	payload := map[string]any{
		"requestId":    req.RequestID,
		"action":       "services." + req.Action,
		"arguments":    map[string]any{"unit": req.Unit},
		"sessionToken": r.Header.Get("X-Lumio-Session"),
	}
	if req.Expected != nil {
		payload["expected"] = req.Expected
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		WriteError(w, NewError(CodeInternal, "Internal server error."))
		return
	}
	s.forwardBrokerAction(w, r, encoded, 30*time.Second)
}

func (s *Server) handleUpdatesRefresh(w http.ResponseWriter, r *http.Request) {
	if s.deps.BrokerSocket == "" {
		s.handleUnavailable(w, r)
		return
	}
	requestID, ok := decodeUpdateRequest(w, r)
	if !ok {
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"requestId":    requestID,
		"action":       "updates.refresh",
		"arguments":    map[string]any{},
		"sessionToken": r.Header.Get("X-Lumio-Session"),
	})
	s.forwardBrokerAction(w, r, payload, 11*time.Minute)
}

func (s *Server) handleUpdatesPlan(w http.ResponseWriter, r *http.Request) {
	if s.deps.BrokerSocket == "" {
		s.handleUnavailable(w, r)
		return
	}
	requestID, ok := decodeUpdateRequest(w, r)
	if !ok {
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"requestId":    requestID,
		"action":       "updates.plan",
		"arguments":    map[string]any{},
		"sessionToken": r.Header.Get("X-Lumio-Session"),
	})
	s.forwardBrokerAction(w, r, payload, 3*time.Minute)
}

func (s *Server) handleUpdatesApply(w http.ResponseWriter, r *http.Request) {
	if s.deps.BrokerSocket == "" {
		s.handleUnavailable(w, r)
		return
	}
	var req struct {
		RequestID string `json:"requestId"`
		PlanID    string `json:"planId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, NewError(CodeValidationFailed, "Body must be a JSON object."))
		return
	}
	if !validRequestID(req.RequestID) {
		WriteError(w, NewError(CodeValidationFailed, "requestId is required."))
		return
	}
	if !regexp.MustCompile(`^pln_[a-f0-9]{24}$`).MatchString(req.PlanID) {
		WriteError(w, NewError(CodeValidationFailed, "invalid planId."))
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"requestId":    req.RequestID,
		"action":       "packages.applyPlan",
		"arguments":    map[string]any{"planId": req.PlanID},
		"expected":     map[string]any{"planId": req.PlanID},
		"sessionToken": r.Header.Get("X-Lumio-Session"),
	})
	s.forwardBrokerAction(w, r, payload, 30*time.Second)
}

func decodeUpdateRequest(w http.ResponseWriter, r *http.Request) (string, bool) {
	var req struct {
		RequestID string `json:"requestId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, NewError(CodeValidationFailed, "Body must be a JSON object."))
		return "", false
	}
	if !validRequestID(req.RequestID) {
		WriteError(w, NewError(CodeValidationFailed, "requestId is required."))
		return "", false
	}
	return req.RequestID, true
}

func (s *Server) forwardBrokerAction(w http.ResponseWriter, r *http.Request, encoded []byte, timeout time.Duration) {
	if s.deps.BrokerSocket == "" {
		WriteError(w, NewError(CodeUnavailable, "This capability is not available in this build."))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	status, headers, body, err := broker.CallAction(ctx, s.deps.BrokerSocket, encoded)
	if err != nil {
		WriteError(w, NewError(CodeUnavailable, "The privileged broker is unavailable."))
		return
	}
	if replay := headers.Get("X-Lumio-Idempotent-Replay"); replay != "" {
		w.Header().Set("X-Lumio-Idempotent-Replay", replay)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func (s *Server) handleUnavailable(w http.ResponseWriter, _ *http.Request) {
	WriteError(w, NewError(CodeUnavailable, "This capability is not available in this build."))
}

func (s *Server) handleNotFound(w http.ResponseWriter, _ *http.Request) {
	WriteError(w, NewError(CodeNotFound, "Unknown endpoint."))
}
