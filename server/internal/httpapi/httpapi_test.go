// SPDX-License-Identifier: AGPL-3.0-only
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"lumio-os/server/internal/journal"
	"lumio-os/server/internal/services"
	"lumio-os/server/internal/static"
	"lumio-os/server/internal/system"
)

type fakeServices struct {
	available bool
	units     []services.Unit
}

func (f fakeServices) Available() bool { return f.available }
func (f fakeServices) List(context.Context) ([]services.Unit, error) {
	if !f.available {
		return nil, services.ErrUnavailable
	}
	return f.units, nil
}
func (f fakeServices) SubscribeChanges(context.Context) (<-chan services.Unit, error) {
	return nil, services.ErrUnavailable
}

type fakeJournal struct {
	available bool
}

func (f fakeJournal) Available() bool { return f.available }
func (f fakeJournal) Query(_ context.Context, q journal.Query) (journal.Result, error) {
	if err := q.Validate(); err != nil {
		return journal.Result{}, err
	}
	return journal.Result{
		Entries: []journal.Entry{{
			Cursor:   "s=abc;i=1",
			TS:       "2026-07-19T00:10:02.113Z",
			Priority: "info",
			Unit:     "cron.service",
			Message:  "hello",
		}},
		NextCursor: "s=abc;i=1",
	}, nil
}
func (f fakeJournal) Follow(context.Context, journal.Query, func(journal.Entry) bool) error {
	return nil
}

func testServer(svc services.API, jb journal.Backend) *httptest.Server {
	s := NewServer(Deps{
		Version:  "test",
		Sampler:  system.NewSampler(),
		Services: svc,
		Journal:  jb,
		Static:   static.Handler(),
	})
	return httptest.NewServer(s.Handler())
}

type testEnvelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error *Error          `json:"error"`
}

func get(t *testing.T, url string) (int, testEnvelope) {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var env testEnvelope
	if err := json.NewDecoder(res.Body).Decode(&env); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
	return res.StatusCode, env
}

func TestMetaVersion(t *testing.T) {
	ts := testServer(fakeServices{}, fakeJournal{})
	defer ts.Close()
	status, env := get(t, ts.URL+"/api/v1/meta/version")
	if status != 200 || !env.OK {
		t.Fatalf("status=%d env=%+v", status, env)
	}
	if !strings.Contains(string(env.Data), `"protocolVersions":[1]`) {
		t.Errorf("data = %s", env.Data)
	}
	if !strings.Contains(string(env.Data), `"version":"test"`) {
		t.Errorf("data = %s", env.Data)
	}
}

func TestIdentity(t *testing.T) {
	ts := testServer(fakeServices{}, fakeJournal{})
	defer ts.Close()
	status, env := get(t, ts.URL+"/api/v1/system/identity")
	if status != 200 || !env.OK {
		t.Fatalf("status=%d", status)
	}
	if !strings.Contains(string(env.Data), `"serverTime"`) || !strings.Contains(string(env.Data), `"architecture"`) {
		t.Errorf("data = %s", env.Data)
	}
}

func TestServicesUnavailable(t *testing.T) {
	ts := testServer(fakeServices{available: false}, fakeJournal{})
	defer ts.Close()
	status, env := get(t, ts.URL+"/api/v1/services")
	if status != 503 {
		t.Fatalf("status=%d", status)
	}
	if env.OK || env.Error == nil || env.Error.Code != CodeUnavailable {
		t.Errorf("env = %+v", env)
	}
}

func TestServicesList(t *testing.T) {
	svc := fakeServices{available: true, units: []services.Unit{{
		Name: "cron.service", ActiveState: "active", SubState: "running",
	}}}
	ts := testServer(svc, fakeJournal{})
	defer ts.Close()
	status, env := get(t, ts.URL+"/api/v1/services")
	if status != 200 || !env.OK {
		t.Fatalf("status=%d", status)
	}
	if !strings.Contains(string(env.Data), `"name":"cron.service"`) {
		t.Errorf("data = %s", env.Data)
	}
}

func TestJournalQuery(t *testing.T) {
	ts := testServer(fakeServices{}, fakeJournal{available: true})
	defer ts.Close()
	status, env := get(t, ts.URL+"/api/v1/journal?unit=cron.service&limit=5")
	if status != 200 || !env.OK {
		t.Fatalf("status=%d", status)
	}
	if !strings.Contains(string(env.Data), `"nextCursor":"s=abc;i=1"`) {
		t.Errorf("data = %s", env.Data)
	}
}

func TestJournalValidation(t *testing.T) {
	ts := testServer(fakeServices{}, fakeJournal{available: true})
	defer ts.Close()
	status, env := get(t, ts.URL+"/api/v1/journal?priority=bogus")
	if status != 400 || env.Error == nil || env.Error.Code != CodeValidationFailed {
		t.Errorf("status=%d env=%+v", status, env)
	}
	status, env = get(t, ts.URL+"/api/v1/journal?limit=abc")
	if status != 400 || env.Error == nil || env.Error.Code != CodeValidationFailed {
		t.Errorf("status=%d env=%+v", status, env)
	}
}

func TestUnavailableEndpoints(t *testing.T) {
	ts := testServer(fakeServices{}, fakeJournal{})
	defer ts.Close()
	for _, tc := range []struct {
		method string
		path   string
	}{
		{"POST", "/api/v1/services/action"},
		{"PUT", "/api/v1/files/write"},
		{"POST", "/api/v1/updates/refresh"},
		{"POST", "/api/v1/updates/plan"},
		{"POST", "/api/v1/updates/apply"},
	} {
		req, _ := http.NewRequest(tc.method, ts.URL+tc.path, nil)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var env testEnvelope
		_ = json.NewDecoder(res.Body).Decode(&env)
		res.Body.Close()
		if res.StatusCode != 503 || env.Error == nil || env.Error.Code != CodeUnavailable {
			t.Errorf("%s %s: status=%d env=%+v", tc.method, tc.path, res.StatusCode, env)
		}
	}
}

func TestUnknownRoute(t *testing.T) {
	ts := testServer(fakeServices{}, fakeJournal{})
	defer ts.Close()
	status, env := get(t, ts.URL+"/api/v1/nope")
	if status != 404 || env.Error == nil || env.Error.Code != CodeNotFound {
		t.Errorf("status=%d env=%+v", status, env)
	}
}

func TestFilesEndpoints(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/note.txt", []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}
	ts := testServer(fakeServices{}, fakeJournal{})
	defer ts.Close()
	status, env := get(t, ts.URL+"/api/v1/files/list?path="+dir)
	if status != 200 || !strings.Contains(string(env.Data), `"name":"note.txt"`) {
		t.Errorf("list: status=%d data=%s", status, env.Data)
	}
	status, env = get(t, ts.URL+"/api/v1/files/read?path="+dir+"/note.txt")
	if status != 200 || !strings.Contains(string(env.Data), `"revision":"sha256:`) {
		t.Errorf("read: status=%d data=%s", status, env.Data)
	}
	status, env = get(t, ts.URL+"/api/v1/files/read?path=/missing/nope")
	if status != 404 || env.Error == nil || env.Error.Code != CodeNotFound {
		t.Errorf("missing: status=%d env=%+v", status, env)
	}
	if p, ok := env.Error.Details["path"].(string); !ok || p == "" {
		t.Errorf("details.path missing: %v", env.Error.Details)
	}
}

func TestMapError(t *testing.T) {
	cases := []struct {
		err  error
		code string
	}{
		{&os.PathError{Op: "open", Path: "/x", Err: fs.ErrPermission}, CodeForbidden},
		{&os.PathError{Op: "open", Path: "/x", Err: fs.ErrNotExist}, CodeNotFound},
		{services.ErrUnavailable, CodeUnavailable},
		{journal.ErrUnavailable, CodeUnavailable},
		{journal.ErrValidation, CodeValidationFailed},
		{errors.New("boom"), CodeInternal},
		{NewError(CodeBusy, "locked"), CodeBusy},
	}
	for _, tc := range cases {
		if got := MapError(tc.err); got.Code != tc.code {
			t.Errorf("MapError(%v) = %s, want %s", tc.err, got.Code, tc.code)
		}
	}
}
