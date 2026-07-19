// SPDX-License-Identifier: AGPL-3.0-only
package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"lumio-os/server/internal/files"
	"lumio-os/server/internal/journal"
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

func (s *Server) handleUnavailable(w http.ResponseWriter, _ *http.Request) {
	WriteError(w, NewError(CodeUnavailable, "This capability is not available in this build."))
}

func (s *Server) handleNotFound(w http.ResponseWriter, _ *http.Request) {
	WriteError(w, NewError(CodeNotFound, "Unknown endpoint."))
}
