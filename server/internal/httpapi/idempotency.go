// SPDX-License-Identifier: AGPL-3.0-only
package httpapi

import (
	"bytes"
	"net/http"
	"sync"
	"time"
)

const idemTTL = 24 * time.Hour

type idemEntry struct {
	status int
	body   []byte
	at     time.Time
}

type idemStore struct {
	mu      sync.Mutex
	entries map[string]idemEntry
}

func newIdemStore() *idemStore {
	return &idemStore{entries: map[string]idemEntry{}}
}

func (s *idemStore) get(key string) (idemEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[key]
	if !ok {
		return idemEntry{}, false
	}
	if time.Since(entry.at) > idemTTL {
		delete(s.entries, key)
		return idemEntry{}, false
	}
	return entry, true
}

func (s *idemStore) put(key string, entry idemEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().Add(-idemTTL)
	for k, e := range s.entries {
		if e.at.Before(cutoff) {
			delete(s.entries, k)
		}
	}
	s.entries[key] = entry
}

type responseRecorder struct {
	status int
	body   bytes.Buffer
	header http.Header
}

func newResponseRecorder() *responseRecorder {
	return &responseRecorder{status: http.StatusOK, header: http.Header{}}
}

func (r *responseRecorder) Header() http.Header { return r.header }

func (r *responseRecorder) WriteHeader(status int) { r.status = status }

func (r *responseRecorder) Write(b []byte) (int, error) { return r.body.Write(b) }

func validRequestID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, c := range id {
		if c < 0x21 || c > 0x7e {
			return false
		}
	}
	return true
}

func (s *Server) mutate(w http.ResponseWriter, requestID string, fn func(w http.ResponseWriter)) {
	if entry, ok := s.idem.get(requestID); ok {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("X-Lumio-Idempotent-Replay", "true")
		w.WriteHeader(entry.status)
		_, _ = w.Write(entry.body)
		return
	}
	rec := newResponseRecorder()
	fn(rec)
	s.idem.put(requestID, idemEntry{status: rec.status, body: rec.body.Bytes(), at: time.Now()})
	for k, values := range rec.header {
		for _, v := range values {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(rec.status)
	_, _ = w.Write(rec.body.Bytes())
}
