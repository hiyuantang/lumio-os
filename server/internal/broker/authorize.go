// SPDX-License-Identifier: AGPL-3.0-only
package broker

import (
	"context"

	"github.com/godbus/dbus/v5"

	"lumio-os/server/internal/ipc"
)

type Result int

const (
	Deny Result = iota
	Allow
	Challenge
)

type Authorizer interface {
	Check(ctx context.Context, uid, pid uint32, actionID string, details map[string]string) (Result, error)
}

type StaticAuthorizer struct {
	Rules func(uid uint32, actionID string, details map[string]string) Result
}

func (s StaticAuthorizer) Check(_ context.Context, uid, _ uint32, actionID string, details map[string]string) (Result, error) {
	return s.Rules(uid, actionID, details), nil
}

type polkitSubject struct {
	Kind    string
	Details map[string]dbus.Variant
}

type polkitAuthorizer struct {
	conn *dbus.Conn
}

func newPolkitAuthorizer() Authorizer {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return &unavailableAuthorizer{err: err}
	}
	return &polkitAuthorizer{conn: conn}
}

func (p *polkitAuthorizer) Check(ctx context.Context, uid, pid uint32, actionID string, details map[string]string) (Result, error) {
	startTime, err := ipc.ProcStartTime(pid)
	if err != nil {
		return Deny, err
	}
	subject := polkitSubject{
		Kind: "unix-process",
		Details: map[string]dbus.Variant{
			"pid":        dbus.MakeVariant(pid),
			"start-time": dbus.MakeVariant(startTime),
		},
	}
	obj := p.conn.Object("org.freedesktop.PolicyKit1", dbus.ObjectPath("/org/freedesktop/PolicyKit1/Authority"))
	call := obj.CallWithContext(ctx, "org.freedesktop.PolicyKit1.Authority.CheckAuthorization", 0,
		subject, actionID, details, uint32(0), "")
	if call.Err != nil {
		return Deny, call.Err
	}
	if len(call.Body) == 0 {
		return Deny, errInvalidReply
	}
	outcome, ok := call.Body[0].([]interface{})
	if !ok || len(outcome) < 2 {
		return Deny, errInvalidReply
	}
	isAuthorized, _ := outcome[0].(bool)
	isChallenge, _ := outcome[1].(bool)
	switch {
	case isAuthorized:
		return Allow, nil
	case isChallenge:
		return Challenge, nil
	default:
		return Deny, nil
	}
}

type unavailableAuthorizer struct {
	err error
}

func (u *unavailableAuthorizer) Check(context.Context, uint32, uint32, string, map[string]string) (Result, error) {
	return Deny, u.err
}
