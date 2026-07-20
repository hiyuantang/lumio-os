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
	"lumio-os/server/internal/network"
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

func TestReloadIsTypedAndValidated(t *testing.T) {
	req := ActionRequest{RequestID: "reload-1", Action: "services.reload"}
	req.Arguments.Unit = "nginx.service"
	if err := req.validate(); err != nil {
		t.Fatalf("reload rejected: %v", err)
	}
	authz := StaticAuthorizer{Rules: func(uint32, string, map[string]string) Result { return Allow }}
	_, client, sys := testBroker(t, authz, nil)
	status, _, body := callAction(t, client, `{"requestId":"reload-1","action":"services.reload","arguments":{"unit":"nginx.service"}}`)
	if status != 200 {
		t.Fatalf("status=%d body=%v", status, body)
	}
	if len(sys.calls) != 1 || sys.calls[0] != "services.reload nginx.service" {
		t.Errorf("calls = %v", sys.calls)
	}
}

func TestPhase5ActionsAreTypedAndValidated(t *testing.T) {
	plan := ActionRequest{RequestID: "plan-1", Action: "updates.plan"}
	if err := plan.validate(); err != nil {
		t.Fatalf("plan rejected: %v", err)
	}
	apply := ActionRequest{RequestID: "apply-1", Action: "packages.applyPlan"}
	apply.Arguments.PlanID = "pln_000000000000000000000000"
	apply.Expected = &struct {
		ActiveState string `json:"activeState"`
		PlanID      string `json:"planId"`
		Revision    string `json:"revision"`
	}{PlanID: apply.Arguments.PlanID}
	if err := apply.validate(); err != nil {
		t.Fatalf("apply rejected: %v", err)
	}
	file := ActionRequest{RequestID: "file-1", Action: "files.writePrivileged"}
	file.Arguments.Path = "/etc/example.conf"
	file.Arguments.ContentBase64 = "dGVzdAo="
	file.Expected = &struct {
		ActiveState string `json:"activeState"`
		PlanID      string `json:"planId"`
		Revision    string `json:"revision"`
	}{Revision: "sha256:" + strings.Repeat("0", 64)}
	if err := file.validate(); err != nil {
		t.Fatalf("file action rejected: %v", err)
	}
	file.Arguments.Path = "/tmp/example.conf"
	if err := file.validate(); err == nil {
		t.Fatal("file action accepted a path outside /etc")
	}
}

func TestPhase6PowerActionsAreTypedAndValidated(t *testing.T) {
	for _, action := range []string{"system.reboot", "system.poweroff"} {
		req := ActionRequest{RequestID: "power-1", Action: action}
		if err := req.validate(); err != nil {
			t.Fatalf("%s rejected: %v", action, err)
		}
	}
	req := ActionRequest{RequestID: "power-2", Action: "system.poweroff; reboot"}
	if err := req.validate(); err == nil {
		t.Fatal("injection-shaped power action was accepted")
	}
}

func TestPhase6NetworkActionsAreTypedAndValidated(t *testing.T) {
	req := ActionRequest{RequestID: "network-1", Action: "network.applyWithRollback"}
	req.Arguments.Config = network.Config{
		Version:   2,
		Ethernets: map[string]network.EthernetConfig{"eth0": {DHCP4: true}},
	}
	req.Arguments.ConfirmTimeoutSec = 30
	req.Expected = &struct {
		ActiveState string `json:"activeState"`
		PlanID      string `json:"planId"`
		Revision    string `json:"revision"`
	}{Revision: "sha256:" + strings.Repeat("0", 64)}
	if err := req.validate(); err != nil {
		t.Fatalf("network apply rejected: %v", err)
	}
	req.Arguments.Config.Ethernets = map[string]network.EthernetConfig{"eth0.dhcp4=false": {DHCP4: true}}
	if err := req.validate(); err == nil {
		t.Fatal("injection-shaped interface was accepted")
	}
	confirm := ActionRequest{RequestID: "network-2", Action: "network.confirm"}
	confirm.Arguments.Token = strings.Repeat("a", 64)
	if err := confirm.validate(); err != nil {
		t.Fatalf("network confirmation rejected: %v", err)
	}
}

type fakeSystemd struct {
	calls []string
	state UnitState
	err   error
}

type fakePower struct {
	calls []string
	err   error
}

type fakeNetwork struct {
	applies  []network.Config
	confirms []string
	pending  network.Pending
	err      error
}

func (f *fakeNetwork) Apply(_ context.Context, config network.Config, _ string, _ int) (network.Pending, error) {
	f.applies = append(f.applies, config)
	return f.pending, f.err
}

func (f *fakeNetwork) Confirm(_ context.Context, token string) (network.Pending, error) {
	f.confirms = append(f.confirms, token)
	return f.pending, f.err
}

func (f *fakePower) schedule(_ context.Context, action string, at time.Time) error {
	f.calls = append(f.calls, action+" "+at.UTC().Format(time.RFC3339Nano))
	return f.err
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
	s.power = &fakePower{}
	s.network = &fakeNetwork{pending: network.Pending{
		Token:            strings.Repeat("a", 64),
		PreviousRevision: "sha256:" + strings.Repeat("0", 64),
		ExpiresAt:        time.Now().UTC().Add(time.Minute),
		ConfirmTimeout:   30,
	}}

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

func TestPowerActionUsesDedicatedPolicyAndAudit(t *testing.T) {
	var checkedActionID string
	authz := StaticAuthorizer{Rules: func(_ uint32, actionID string, details map[string]string) Result {
		checkedActionID = actionID
		if details["action"] != "system.reboot" {
			t.Errorf("details = %v", details)
		}
		return Allow
	}}
	s, client, _ := testBroker(t, authz, nil)
	status, _, body := callAction(t, client, `{"requestId":"power-allow","action":"system.reboot","arguments":{}}`)
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%v", status, body)
	}
	if checkedActionID != systemPowerActionID {
		t.Errorf("action id = %q", checkedActionID)
	}
	power := s.power.(*fakePower)
	if len(power.calls) != 1 || !strings.HasPrefix(power.calls[0], "system.reboot ") {
		t.Errorf("calls = %v", power.calls)
	}
	data, _ := body["data"].(map[string]any)
	if data["action"] != "system.reboot" || data["scheduledAt"] == "" {
		t.Errorf("data = %v", data)
	}
	var outcome string
	_ = s.audit.db.QueryRow(`SELECT outcome FROM audit WHERE request_id = 'power-allow' AND kind = 'end'`).Scan(&outcome)
	if outcome != "success" {
		t.Errorf("audit outcome = %q", outcome)
	}
}

func TestPowerActionRequiresReauthentication(t *testing.T) {
	var checkedActionID string
	authz := StaticAuthorizer{Rules: func(_ uint32, actionID string, _ map[string]string) Result {
		checkedActionID = actionID
		return Challenge
	}}
	s, client, _ := testBroker(t, authz, nil)
	status, _, body := callAction(t, client, `{"requestId":"power-challenge","action":"system.poweroff","arguments":{}}`)
	if status != http.StatusForbidden {
		t.Fatalf("status=%d body=%v", status, body)
	}
	if checkedActionID != systemPowerActionID {
		t.Errorf("action id = %q", checkedActionID)
	}
	if len(s.power.(*fakePower).calls) != 0 {
		t.Fatal("power action executed without reauthentication")
	}
	errObj, _ := body["error"].(map[string]any)
	details, _ := errObj["details"].(map[string]any)
	if details["reauthRequired"] != true {
		t.Errorf("details = %v", details)
	}
}

func TestNetworkApplyUsesDedicatedPolicyAndAudit(t *testing.T) {
	var checkedActionID string
	authz := StaticAuthorizer{Rules: func(_ uint32, actionID string, details map[string]string) Result {
		checkedActionID = actionID
		if details["action"] != "network.applyWithRollback" {
			t.Errorf("details = %v", details)
		}
		return Allow
	}}
	s, client, _ := testBroker(t, authz, nil)
	payload := `{"requestId":"network-allow","action":"network.applyWithRollback","arguments":{"config":{"version":2,"ethernets":{"eth0":{"dhcp4":true,"dhcp6":false}}},"confirmTimeoutSec":30},"expected":{"revision":"sha256:` + strings.Repeat("0", 64) + `"}}`
	status, _, body := callAction(t, client, payload)
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%v", status, body)
	}
	if checkedActionID != networkApplyActionID {
		t.Errorf("action id = %q", checkedActionID)
	}
	networkFake := s.network.(*fakeNetwork)
	if len(networkFake.applies) != 1 {
		t.Fatalf("applies = %v", networkFake.applies)
	}
	data, _ := body["data"].(map[string]any)
	if data["token"] != strings.Repeat("a", 64) || data["expiresAt"] == "" {
		t.Errorf("data = %v", data)
	}
	var outcome string
	_ = s.audit.db.QueryRow(`SELECT outcome FROM audit WHERE request_id = 'network-allow' AND kind = 'end'`).Scan(&outcome)
	if outcome != "success" {
		t.Errorf("audit outcome = %q", outcome)
	}
}

func TestNetworkActionRequiresReauthentication(t *testing.T) {
	var checkedActionID string
	authz := StaticAuthorizer{Rules: func(_ uint32, actionID string, _ map[string]string) Result {
		checkedActionID = actionID
		return Challenge
	}}
	s, client, _ := testBroker(t, authz, nil)
	status, _, body := callAction(t, client, `{"requestId":"network-challenge","action":"network.confirm","arguments":{"token":"`+strings.Repeat("a", 64)+`"}}`)
	if status != http.StatusForbidden {
		t.Fatalf("status=%d body=%v", status, body)
	}
	if checkedActionID != networkApplyActionID {
		t.Errorf("action id = %q", checkedActionID)
	}
	if len(s.network.(*fakeNetwork).confirms) != 0 {
		t.Fatal("network action executed without reauthentication")
	}
	errObj, _ := body["error"].(map[string]any)
	details, _ := errObj["details"].(map[string]any)
	if details["reauthRequired"] != true {
		t.Errorf("details = %v", details)
	}
}

func TestNetworkConfirmRejectsWrongTokenBeforeExecution(t *testing.T) {
	authz := StaticAuthorizer{Rules: func(uint32, string, map[string]string) Result { return Allow }}
	s, client, _ := testBroker(t, authz, nil)
	status, _, body := callAction(t, client, `{"requestId":"network-confirm","action":"network.confirm","arguments":{"token":"bad"}}`)
	if status != http.StatusBadRequest {
		t.Fatalf("status=%d body=%v", status, body)
	}
	if len(s.network.(*fakeNetwork).confirms) != 0 {
		t.Fatal("invalid confirmation token reached the network controller")
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
