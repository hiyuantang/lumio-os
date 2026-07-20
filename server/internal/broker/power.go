// SPDX-License-Identifier: AGPL-3.0-only
package broker

import (
	"context"
	"fmt"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	loginBusName      = "org.freedesktop.login1"
	loginManagerPath  = dbus.ObjectPath("/org/freedesktop/login1")
	loginManagerIface = "org.freedesktop.login1.Manager"
)

type powerClient struct {
	conn *dbus.Conn
}

func newPowerClient() (*powerClient, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, err
	}
	return &powerClient{conn: conn}, nil
}

func (c *powerClient) schedule(ctx context.Context, action string, at time.Time) error {
	kind := ""
	switch action {
	case "system.reboot":
		kind = "reboot"
	case "system.poweroff":
		kind = "poweroff"
	default:
		return fmt.Errorf("unknown power action %q", action)
	}
	manager := c.conn.Object(loginBusName, loginManagerPath)
	call := manager.CallWithContext(ctx, loginManagerIface+".ScheduleShutdown", 0, kind, uint64(at.UnixMicro()))
	if call.Err != nil {
		return fmt.Errorf("ScheduleShutdown: %w", call.Err)
	}
	return nil
}
