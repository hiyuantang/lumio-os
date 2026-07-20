// SPDX-License-Identifier: AGPL-3.0-only
package broker

import (
	"context"
	"fmt"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	sysdBusName      = "org.freedesktop.systemd1"
	sysdManagerPath  = dbus.ObjectPath("/org/freedesktop/systemd1")
	sysdManagerIface = "org.freedesktop.systemd1.Manager"
	sysdUnitIface    = "org.freedesktop.systemd1.Unit"
	sysdPropsIface   = "org.freedesktop.DBus.Properties"
)

type UnitState struct {
	Name         string `json:"name"`
	ActiveState  string `json:"activeState"`
	SubState     string `json:"subState"`
	EnabledState string `json:"enabledState"`
}

type systemdClient struct {
	conn *dbus.Conn
}

func newSystemdClient() (*systemdClient, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, err
	}
	return &systemdClient{conn: conn}, nil
}

func (c *systemdClient) manager() dbus.BusObject {
	return c.conn.Object(sysdBusName, sysdManagerPath)
}

func (c *systemdClient) unitState(ctx context.Context, unit string) (UnitState, error) {
	state := UnitState{Name: unit}
	call := c.manager().CallWithContext(ctx, sysdManagerIface+".GetUnit", 0, unit)
	if call.Err != nil || len(call.Body) == 0 {
		return state, fmt.Errorf("GetUnit: %v", call.Err)
	}
	path, ok := call.Body[0].(dbus.ObjectPath)
	if !ok {
		return state, errInvalidReply
	}
	unitObj := c.conn.Object(sysdBusName, path)
	for _, pair := range []struct {
		prop string
		dst  *string
	}{
		{"ActiveState", &state.ActiveState},
		{"SubState", &state.SubState},
	} {
		propCall := unitObj.CallWithContext(ctx, sysdPropsIface+".Get", 0, sysdUnitIface, pair.prop)
		if propCall.Err != nil || len(propCall.Body) == 0 {
			return state, fmt.Errorf("Get %s: %v", pair.prop, propCall.Err)
		}
		if v, ok := propCall.Body[0].(dbus.Variant); ok {
			*pair.dst, _ = v.Value().(string)
		}
	}
	enabledCall := c.manager().CallWithContext(ctx, sysdManagerIface+".GetUnitFileState", 0, unit)
	if enabledCall.Err == nil && len(enabledCall.Body) > 0 {
		state.EnabledState, _ = enabledCall.Body[0].(string)
	}
	return state, nil
}

func (c *systemdClient) execute(ctx context.Context, action, unit string) (UnitState, error) {
	switch action {
	case "services.start":
		if call := c.manager().CallWithContext(ctx, sysdManagerIface+".StartUnit", 0, unit, "replace"); call.Err != nil {
			return UnitState{}, fmt.Errorf("StartUnit: %v", call.Err)
		}
		return c.waitState(ctx, unit, "active")
	case "services.stop":
		if call := c.manager().CallWithContext(ctx, sysdManagerIface+".StopUnit", 0, unit, "replace"); call.Err != nil {
			return UnitState{}, fmt.Errorf("StopUnit: %v", call.Err)
		}
		return c.waitState(ctx, unit, "inactive")
	case "services.restart":
		if call := c.manager().CallWithContext(ctx, sysdManagerIface+".RestartUnit", 0, unit, "replace"); call.Err != nil {
			return UnitState{}, fmt.Errorf("RestartUnit: %v", call.Err)
		}
		return c.waitState(ctx, unit, "active")
	case "services.reload":
		if call := c.manager().CallWithContext(ctx, sysdManagerIface+".ReloadUnit", 0, unit, "replace"); call.Err != nil {
			return UnitState{}, fmt.Errorf("ReloadUnit: %v", call.Err)
		}
		return c.waitState(ctx, unit, "active")
	case "services.enable":
		if call := c.manager().CallWithContext(ctx, sysdManagerIface+".EnableUnitFiles", 0, []string{unit}, false, true); call.Err != nil {
			return UnitState{}, fmt.Errorf("EnableUnitFiles: %v", call.Err)
		}
		c.manager().CallWithContext(ctx, sysdManagerIface+".Reload", 0)
		return c.waitEnabled(ctx, unit, true)
	case "services.disable":
		if call := c.manager().CallWithContext(ctx, sysdManagerIface+".DisableUnitFiles", 0, []string{unit}, false); call.Err != nil {
			return UnitState{}, fmt.Errorf("DisableUnitFiles: %v", call.Err)
		}
		c.manager().CallWithContext(ctx, sysdManagerIface+".Reload", 0)
		return c.waitEnabled(ctx, unit, false)
	}
	return UnitState{}, fmt.Errorf("unknown action %q", action)
}

func (c *systemdClient) waitState(ctx context.Context, unit, want string) (UnitState, error) {
	deadline := time.Now().Add(8 * time.Second)
	var last UnitState
	for {
		state, err := c.unitState(ctx, unit)
		if err != nil {
			return state, err
		}
		last = state
		if state.ActiveState == want {
			return state, nil
		}
		if state.ActiveState == "failed" && want == "active" {
			return state, fmt.Errorf("unit entered failed state")
		}
		if time.Now().After(deadline) {
			return last, fmt.Errorf("unit reached %q, wanted %q", state.ActiveState, want)
		}
		select {
		case <-ctx.Done():
			return last, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (c *systemdClient) waitEnabled(ctx context.Context, unit string, enabled bool) (UnitState, error) {
	deadline := time.Now().Add(8 * time.Second)
	for {
		state, err := c.unitState(ctx, unit)
		if err != nil {
			return state, err
		}
		isEnabled := state.EnabledState == "enabled" || state.EnabledState == "enabled-runtime" || state.EnabledState == "alias"
		if isEnabled == enabled {
			return state, nil
		}
		if time.Now().After(deadline) {
			return state, fmt.Errorf("unit enabledState %q", state.EnabledState)
		}
		select {
		case <-ctx.Done():
			return state, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}
