// SPDX-License-Identifier: AGPL-3.0-only
package gateway

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lumio-os/server/internal/ipc"
)

const testToken = "session-token-1"
const testCSRF = "csrf-token-1"

func startStub(t *testing.T, name string, handler http.Handler) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "ltgw")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sockPath := filepath.Join(dir, name)
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = ipc.ServeUnix(ln, handler) }()
	t.Cleanup(func() { _ = ln.Close() })
	return sockPath
}

func stubSessiond(t *testing.T) string {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login", func(w http.ResponseWriter, r *http.Request) {
		var req map[string]string
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req["password"] != "correct" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token":       testToken,
			"csrf":        testCSRF,
			"user":        map[string]any{"name": "alice", "uid": 1000, "gid": 1000, "home": "/home/alice"},
			"agentSocket": stubAgent(t),
		})
	})
	mux.HandleFunc("POST /validate", func(w http.ResponseWriter, r *http.Request) {
		var req map[string]string
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req["token"] != testToken {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token":       testToken,
			"csrf":        testCSRF,
			"user":        map[string]any{"name": "alice", "uid": 1000, "gid": 1000, "home": "/home/alice"},
			"agentSocket": stubAgent(t),
			"reauthUntil": 0,
		})
	})
	mux.HandleFunc("POST /logout", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})
	return startStub(t, "sessiond.sock", mux)
}

var agentSock string

func stubAgent(t *testing.T) string {
	if agentSock != "" {
		return agentSock
	}
	agentSock = startStub(t, "agent.sock", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":   true,
			"path": r.URL.Path,
			"auth": r.Header.Get("X-Lumio-Session"),
		})
	}))
	return agentSock
}

func testGateway(t *testing.T) (*Gateway, *httptest.Server) {
	t.Helper()
	agentSock = ""
	gw := New(Config{
		Addr:           "127.0.0.1:0",
		SessiondSocket: stubSessiond(t),
		Version:        "test",
	})
	srv := httptest.NewServer(gw.Handler())
	t.Cleanup(srv.Close)
	return gw, srv
}

func loginCookies(t *testing.T, srv *httptest.Server) []*http.Cookie {
	t.Helper()
	resp, err := http.Post(srv.URL+"/api/v1/auth/login", "application/json", strings.NewReader(`{"username":"alice","password":"correct"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("login status %d", resp.StatusCode)
	}
	var sessionCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "lumio_session" {
			sessionCookie = c
			if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/" {
				t.Errorf("session cookie attributes: %+v", c)
			}
		}
		if c.Name == "lumio_csrf" && c.HttpOnly {
			t.Error("csrf cookie must be readable")
		}
	}
	if sessionCookie == nil {
		t.Fatal("no lumio_session cookie")
	}
	return resp.Cookies()
}

func TestLoginAndSession(t *testing.T) {
	_, srv := testGateway(t)
	cookies := loginCookies(t, srv)

	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/auth/session", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("session status %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"name":"alice"`) {
		t.Errorf("body = %s", body)
	}
}

func TestSessionRequired(t *testing.T) {
	_, srv := testGateway(t)
	resp, err := http.Get(srv.URL + "/api/v1/services")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestCSRFRequired(t *testing.T) {
	_, srv := testGateway(t)
	cookies := loginCookies(t, srv)

	do := func(withHeader bool) int {
		req, _ := http.NewRequest("POST", srv.URL+"/api/v1/files/delete", strings.NewReader(`{"path":"/tmp/x","requestId":"r1"}`))
		for _, c := range cookies {
			req.AddCookie(c)
		}
		if withHeader {
			req.Header.Set("X-Lumio-CSRF", testCSRF)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if status := do(false); status != http.StatusForbidden {
		t.Errorf("no csrf header: %d", status)
	}
	if status := do(true); status != http.StatusOK {
		t.Errorf("with csrf: %d", status)
	}
}

func TestProxyForwardsSessionToken(t *testing.T) {
	_, srv := testGateway(t)
	cookies := loginCookies(t, srv)
	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/system/identity", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"auth":"`+testToken+`"`) {
		t.Errorf("agent did not receive session token: %s", body)
	}
}

func TestLoginWrongPassword(t *testing.T) {
	_, srv := testGateway(t)
	resp, err := http.Post(srv.URL+"/api/v1/auth/login", "application/json", strings.NewReader(`{"username":"alice","password":"wrong"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestLoginRateLimit(t *testing.T) {
	limiter := newLoginLimiter()
	for i := 0; i < limiterThreshold-1; i++ {
		limiter.record("k")
		if limiter.blocked("k") != 0 {
			t.Fatalf("blocked too early at %d", i)
		}
	}
	limiter.record("k")
	if limiter.blocked("k") <= 0 {
		t.Fatal("not blocked after threshold")
	}
	limiter.reset("k")
	if limiter.blocked("k") != 0 {
		t.Fatal("still blocked after reset")
	}
}
