// SPDX-License-Identifier: AGPL-3.0-only
package broker

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lumio-os/server/internal/ipc"
)

func TestValidate(t *testing.T) {
	valid := ActionRequest{RequestID: "r1", Action: "services.restart"}
	valid.Arguments.Unit = "cron.service"
	if err := valid.validate(); err != nil {
		t.Errorf("valid request rejected: %v", err)
	}
	cases := []ActionRequest{
		{Action: "services.restart"},
		{RequestID: "r", Action: "runRootCommand"},
		{RequestID: "r", Action: "services.restart"},
	}
	cases[2].Arguments.Unit = "../../etc"
	for i := range cases {
		if err := cases[i].validate(); err == nil {
			t.Errorf("case %d should fail validation", i)
		}
	}
	badActions := []string{"restart; rm -rf /", "services.restart;rm", ""}
	for _, a := range badActions {
		req := ActionRequest{RequestID: "r", Action: a}
		req.Arguments.Unit = "cron.service"
		if err := req.validate(); err == nil {
			t.Errorf("action %q should fail", a)
		}
	}
}

type fakeSystemd struct {
	calls []string
	state UnitState
	err   error
}

func (f *fakeSystemd) unitState(context.Context, string) (UnitState, error) { return f.state, f.err }
func (f *fakeSystemd) execute(_ context.Context, action, unit string) (UnitState, error) {
	f.calls = append(f.calls, action+" "+unit)
	return UnitState{Name: unit, ActiveState: "active", SubState: "running", EnabledState: "enabled"}, nil
}

func testBroker(t *testing.T, authz Authorizer, sessiondHandler http.Handler) (*Server, *http.Client, *fakeSystemd) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "ltbk")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sessiondSock := startHTTPServer(t, dir, "sessiond.sock", sessiondHandler)
	s := &Server{
		cfg:      Config{SocketPath: filepath.Join(dir, "broker.sock"), DBPath: filepath.Join(dir, "audit.db"), SessiondSocket: sessiondSock},
		authz:    authz,
		sessiond: ipc.HTTPClient(sessiondSock),
	}
	audit, err := OpenAudit(s.cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	s.audit = audit
	sys := &fakeSystemd{state: UnitState{ActiveState: "active", SubState: "running"}}
	s.sys = sys

	_ = os.Remove(s.cfg.SocketPath)
	ln, err := net.Listen("unix", s.cfg.SocketPath)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /action", s.handleAction)
	srv := &http.Server{
		Handler: mux,
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			return context.WithValue(ctx, connKey{}, c)
		},
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close(); _ = ln.Close() })
	return s, ipc.HTTPClient(s.cfg.SocketPath), sys
}

func startHTTPServer(t *testing.T, dir, name string, handler http.Handler) string {
	t.Helper()
	sockPath := filepath.Join(dir, name)
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = ipc.ServeUnix(ln, handler) }()
	t.Cleanup(func() { _ = ln.Close() })
	return sockPath
}

func callAction(t *testing.T, client *http.Client, payload string) (int, http.Header, map[string]any) {
	t.Helper()
	resp, err := client.Post("http://broker/action", "application/json", bytes.NewReader([]byte(payload)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(body, &out)
	return resp.StatusCode, resp.Header, out
}

func TestActionAllow(t *testing.T) {
	authz := StaticAuthorizer{Rules: func(uint32, string, map[string]string) Result { return Allow }}
	_, client, sys := testBroker(t, authz, nil)
	status, _, body := callAction(t, client, `{"requestId":"b1","action":"services.restart","arguments":{"unit":"cron.service"}}`)
	if status != 200 {
		t.Fatalf("status=%d body=%v", status, body)
	}
	if len(sys.calls) != 1 || sys.calls[0] != "services.restart cron.service" {
		t.Errorf("calls = %v", sys.calls)
	}
	data, _ := body["data"].(map[string]any)
	unit, _ := data["unit"].(map[string]any)
	if unit["activeState"] != "active" {
		t.Errorf("unit = %v", unit)
	}
}

func TestActionDeny(t *testing.T) {
	authz := StaticAuthorizer{Rules: func(uint32, string, map[string]string) Result { return Deny }}
	s, client, sys := testBroker(t, authz, nil)
	status, _, body := callAction(t, client, `{"requestId":"b2","action":"services.restart","arguments":{"unit":"nginx.service"}}`)
	if status != 403 {
		t.Fatalf("status=%d body=%v", status, body)
	}
	if len(sys.calls) != 0 {
		t.Error("denied action must not execute")
	}
	errObj, _ := body["error"].(map[string]any)
	if errObj["code"] != "forbidden" {
		t.Errorf("error = %v", errObj)
	}
	var outcome string
	_ = s.audit.db.QueryRow(`SELECT outcome FROM audit WHERE request_id = 'b2'`).Scan(&outcome)
	if outcome != "denied" {
		t.Errorf("audit outcome = %q", outcome)
	}
}

func TestActionChallengeReauth(t *testing.T) {
	authz := StaticAuthorizer{Rules: func(uint32, string, map[string]string) Result { return Challenge }}
	_, client, _ := testBroker(t, authz, nil)
	payload := `{"requestId":"b3","action":"services.restart","arguments":{"unit":"ssh.service"},"sessionToken":"tok"}`
	status, _, body := callAction(t, client, payload)
	if status != 403 {
		t.Fatalf("status=%d body=%v", status, body)
	}
	errObj, _ := body["error"].(map[string]any)
	details, _ := errObj["details"].(map[string]any)
	if details["reauthRequired"] != true {
		t.Errorf("details = %v", details)
	}

	authz2 := StaticAuthorizer{Rules: func(uint32, string, map[string]string) Result { return Challenge }}
	sessiondOK := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"uid": uint32(os.Getuid()), "reauthUntil": time.Now().Add(time.Minute).UnixMilli()})
	})
	dir2, err := os.MkdirTemp("/tmp", "ltbk")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir2) })
	sessiondSock := startHTTPServer(t, dir2, "sessiond.sock", sessiondOK)
	s2 := &Server{cfg: Config{SocketPath: filepath.Join(dir2, "broker.sock"), DBPath: filepath.Join(dir2, "audit.db"), SessiondSocket: sessiondSock}, authz: authz2, sessiond: ipc.HTTPClient(sessiondSock)}
	audit, err := OpenAudit(s2.cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	s2.audit = audit
	sys2 := &fakeSystemd{state: UnitState{ActiveState: "active"}}
	s2.sys = sys2
	ln, err := net.Listen("unix", s2.cfg.SocketPath)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /action", s2.handleAction)
	srv := &http.Server{Handler: mux, ConnContext: func(ctx context.Context, c net.Conn) context.Context {
		return context.WithValue(ctx, connKey{}, c)
	}}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close(); _ = ln.Close() })
	status, _, body = callAction(t, ipc.HTTPClient(s2.cfg.SocketPath), payload)
	if status != 200 {
		t.Fatalf("with reauth: status=%d body=%v", status, body)
	}
	if len(sys2.calls) != 1 {
		t.Errorf("calls = %v", sys2.calls)
	}
}

func TestActionExpectedConflict(t *testing.T) {
	authz := StaticAuthorizer{Rules: func(uint32, string, map[string]string) Result { return Allow }}
	_, client, sys := testBroker(t, authz, nil)
	status, _, body := callAction(t, client, `{"requestId":"b4","action":"services.restart","arguments":{"unit":"cron.service"},"expected":{"activeState":"inactive"}}`)
	if status != 409 {
		t.Fatalf("status=%d body=%v", status, body)
	}
	if len(sys.calls) != 0 {
		t.Error("conflicted action must not execute")
	}
	errObj, _ := body["error"].(map[string]any)
	if errObj["code"] != "conflict" {
		t.Errorf("error = %v", errObj)
	}
}

func TestActionIdempotentReplay(t *testing.T) {
	authz := StaticAuthorizer{Rules: func(uint32, string, map[string]string) Result { return Allow }}
	s, client, sys := testBroker(t, authz, nil)
	payload := `{"requestId":"b5","action":"services.restart","arguments":{"unit":"cron.service"}}`
	status, _, _ := callAction(t, client, payload)
	if status != 200 {
		t.Fatalf("first: %d", status)
	}
	status, headers, _ := callAction(t, client, payload)
	if status != 200 {
		t.Fatalf("replay: %d", status)
	}
	if headers.Get("X-Lumio-Idempotent-Replay") != "true" {
		t.Error("missing replay header")
	}
	if len(sys.calls) != 1 {
		t.Errorf("replay executed again: %v", sys.calls)
	}
	var begins int
	_ = s.audit.db.QueryRow(`SELECT count(*) FROM audit WHERE request_id = 'b5' AND kind = 'begin'`).Scan(&begins)
	if begins != 1 {
		t.Errorf("begin rows = %d", begins)
	}
}

func TestAuditStoredResultErrors(t *testing.T) {
	dir := t.TempDir()
	audit, err := OpenAudit(filepath.Join(dir, "audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	req := ActionRequest{RequestID: "b6", Action: "services.restart"}
	req.Arguments.Unit = "cron.service"
	audit.Begin(req, 1000, "alice", "allow")
	audit.End(1, req, 1000, "alice", "allow", "failed", "boom", nil, time.Second)
	status, body, ok := audit.StoredResult("b6")
	if !ok || status != 500 || !strings.Contains(body, "boom") {
		t.Errorf("failed replay: %d %s %v", status, body, ok)
	}
	if _, _, ok := audit.StoredResult("unknown"); ok {
		t.Error("unknown requestId should not replay")
	}
}
