// SPDX-License-Identifier: AGPL-3.0-only
package services

import (
	"context"
	"errors"
)

var ErrUnavailable = errors.New("systemd is unavailable")

type Unit struct {
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	LoadState    string `json:"loadState,omitempty"`
	ActiveState  string `json:"activeState,omitempty"`
	SubState     string `json:"subState,omitempty"`
	EnabledState string `json:"enabledState,omitempty"`
}

type API interface {
	Available() bool
	List(ctx context.Context) ([]Unit, error)
	SubscribeChanges(ctx context.Context) (<-chan Unit, error)
}

func diffUnits(prev map[string]Unit, cur []Unit) []Unit {
	var out []Unit
	for _, u := range cur {
		p, known := prev[u.Name]
		if !known {
			out = append(out, u)
			continue
		}
		d := Unit{Name: u.Name}
		changed := false
		if u.Description != p.Description {
			d.Description = u.Description
			changed = true
		}
		if u.LoadState != p.LoadState {
			d.LoadState = u.LoadState
			changed = true
		}
		if u.ActiveState != p.ActiveState {
			d.ActiveState = u.ActiveState
			changed = true
		}
		if u.SubState != p.SubState {
			d.SubState = u.SubState
			changed = true
		}
		if u.EnabledState != p.EnabledState {
			d.EnabledState = u.EnabledState
			changed = true
		}
		if changed {
			out = append(out, d)
		}
	}
	return out
}
