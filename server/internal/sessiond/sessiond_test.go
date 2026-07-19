// SPDX-License-Identifier: AGPL-3.0-only
package sessiond

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"testing"
	"time"

	"lumio-os/server/internal/ipc"
)

func testDaemon(t *testing.T) (*Daemon, *http.Client, string) {
	t.Helper()
	current, err := user.Current()
	if err != nil {
		t.Skipf("no current user: %v", err)
	}
	dir, err := os.MkdirTemp("/tmp", "ltsd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sockPath := filepath.Join(dir, "sessiond.sock")
	d := New(Config{RunDir: dir, SocketPath: sockPath, InsecureDevAuth: current.Username})
	d.spawnAgentFn = func(uid uint32) (*agentProc, error) {
		return &agentProc{socketPath: filepath.Join(dir, "agent.sock")}, nil
	}
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = serveUnix(ln, d.mux()) }()
	t.Cleanup(func() { _ = ln.Close() })
	return d, ipc.HTTPClient(sockPath), current.Username
}

func post(t *testing.T, client *http.Client, path string, body any) (int, map[string]any) {
	t.Helper()
	payload, _ := json.Marshal(body)
	resp, err := client.Post("http://sessiond"+path, "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestLoginValidateReauthLogout(t *testing.T) {
	_, client, username := testDaemon(t)

	status, _ := post(t, client, "/login", map[string]any{"username": "mallory", "password": "x"})
	if status != http.StatusUnauthorized {
		t.Fatalf("wrong user: %d", status)
	}

	status, login := post(t, client, "/login", map[string]any{"username": username, "password": "anything"})
	if status != http.StatusOK {
		t.Fatalf("login: %d %v", status, login)
	}
	token, _ := login["token"].(string)
	csrf, _ := login["csrf"].(string)
	if token == "" || csrf == "" {
		t.Fatalf("missing tokens: %v", login)
	}

	status, validated := post(t, client, "/validate", map[string]any{"token": token})
	if status != http.StatusOK {
		t.Fatalf("validate: %d", status)
	}
	userObj, _ := validated["user"].(map[string]any)
	if userObj["name"] != username {
		t.Errorf("user = %v", userObj)
	}

	status, reauth := post(t, client, "/reauth", map[string]any{"token": token, "password": "x"})
	if status != http.StatusOK {
		t.Fatalf("reauth: %d %v", status, reauth)
	}
	until, _ := reauth["reauthenticatedUntil"].(float64)
	if int64(until) <= time.Now().UnixMilli() {
		t.Errorf("reauthUntil = %v", until)
	}

	status, check := post(t, client, "/session/check", map[string]any{"token": token})
	if status != http.StatusOK || check["reauthUntil"].(float64) != until {
		t.Errorf("check: %d %v", status, check)
	}

	status, _ = post(t, client, "/logout", map[string]any{"token": token})
	if status != http.StatusOK {
		t.Fatalf("logout: %d", status)
	}
	status, _ = post(t, client, "/validate", map[string]any{"token": token})
	if status != http.StatusNotFound {
		t.Errorf("validate after logout: %d", status)
	}
}

func TestLoginUnavailableWithoutPAM(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "ltsd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sockPath := filepath.Join(dir, "sessiond.sock")
	d := New(Config{RunDir: dir, SocketPath: sockPath})
	d.spawnAgentFn = func(uid uint32) (*agentProc, error) {
		return &agentProc{}, nil
	}
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = serveUnix(ln, d.mux()) }()
	t.Cleanup(func() { _ = ln.Close() })
	client := ipc.HTTPClient(sockPath)

	status, _ := post(t, client, "/login", map[string]any{"username": "root", "password": "x"})
	if status != http.StatusServiceUnavailable && status != http.StatusUnauthorized {
		t.Errorf("nopam login: %d", status)
	}
}

func TestSessionExpiry(t *testing.T) {
	d, _, _ := testDaemon(t)
	sess := &Session{Token: "t1", CSRF: "c1", User: User{Name: "alice"}, CreatedAt: time.Now().Add(-31 * 24 * time.Hour), LastSeenAt: time.Now()}
	d.sessions["t1"] = sess
	if _, err := d.lookup("t1", false); err == nil {
		t.Error("absolute expiry should evict the session")
	}
}
