// SPDX-License-Identifier: AGPL-3.0-only
package httpapi

import (
	"net/http"
	"strings"

	"lumio-os/server/internal/journal"
	"lumio-os/server/internal/services"
	"lumio-os/server/internal/system"
)

const (
	maxBodyBytes      = 1 << 20
	maxWriteBodyBytes = 12 << 20
)

type Deps struct {
	Version      string
	Sampler      *system.Sampler
	Services     services.API
	Journal      journal.Backend
	WS           http.Handler
	Static       http.Handler
	BrokerSocket string
}

type Server struct {
	deps Deps
	idem *idemStore
}

func NewServer(deps Deps) *Server {
	return &Server{deps: deps, idem: newIdemStore()}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/meta/version", s.handleVersion)
	mux.HandleFunc("GET /api/v1/system/identity", s.handleIdentity)
	mux.HandleFunc("GET /api/v1/system/overview", s.handleOverview)
	mux.HandleFunc("GET /api/v1/system/metrics", s.handleMetrics)
	mux.HandleFunc("GET /api/v1/services", s.handleServices)
	mux.HandleFunc("GET /api/v1/journal", s.handleJournal)
	mux.HandleFunc("GET /api/v1/files/list", s.handleFilesList)
	mux.HandleFunc("GET /api/v1/files/read", s.handleFilesRead)
	mux.HandleFunc("PUT /api/v1/files/write", s.handleFilesWrite)
	mux.HandleFunc("POST /api/v1/files/delete", s.handleFilesDelete)
	mux.HandleFunc("POST /api/v1/services/action", s.handleServicesAction)
	mux.HandleFunc("POST /api/v1/updates/refresh", s.handleUnavailable)
	mux.HandleFunc("POST /api/v1/updates/plan", s.handleUnavailable)
	mux.HandleFunc("POST /api/v1/updates/apply", s.handleUnavailable)
	if s.deps.WS != nil {
		mux.Handle("GET /api/v1/ws", s.deps.WS)
	}
	if s.deps.Static != nil {
		mux.Handle("GET /", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				s.handleNotFound(w, r)
				return
			}
			s.deps.Static.ServeHTTP(w, r)
		}))
	}
	mux.HandleFunc("/", s.handleNotFound)
	return s.wrap(mux)
}

func (s *Server) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recover() != nil {
				WriteError(w, NewError(CodeInternal, "Internal server error."))
			}
		}()
		limit := int64(maxBodyBytes)
		if r.URL.Path == "/api/v1/files/write" {
			limit = maxWriteBodyBytes
		}
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		next.ServeHTTP(w, r)
	})
}
