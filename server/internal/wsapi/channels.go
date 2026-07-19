// SPDX-License-Identifier: AGPL-3.0-only
package wsapi

import (
	"context"
	"encoding/json"
	"time"

	"lumio-os/server/internal/httpapi"
	"lumio-os/server/internal/journal"
	"lumio-os/server/internal/services"
)

func (c *conn) startChannel(ctx context.Context, ch *channel, params json.RawMessage) error {
	switch ch.capability {
	case "system.metrics":
		var p struct {
			IntervalMs int `json:"intervalMs"`
		}
		if len(params) > 0 {
			if err := json.Unmarshal(params, &p); err != nil {
				return errValidation("invalid params: " + err.Error())
			}
		}
		interval := 2000
		if p.IntervalMs != 0 {
			interval = p.IntervalMs
		}
		if interval < 500 || interval > 60000 {
			return errValidation("intervalMs must be between 500 and 60000")
		}
		c.enqueue(subscribedFrame(ch.id))
		go c.runMetrics(ctx, ch, time.Duration(interval)*time.Millisecond)
		return nil
	case "services.subscribe":
		if !c.hub.deps.Services.Available() {
			return services.ErrUnavailable
		}
		c.enqueue(subscribedFrame(ch.id))
		go c.runServices(ctx, ch)
		return nil
	case "journal.stream":
		if !c.hub.deps.Journal.Available() {
			return journal.ErrUnavailable
		}
		var p struct {
			Unit     string `json:"unit"`
			Priority string `json:"priority"`
			Since    string `json:"since"`
			After    string `json:"after"`
		}
		if len(params) > 0 {
			if err := json.Unmarshal(params, &p); err != nil {
				return errValidation("invalid params: " + err.Error())
			}
		}
		q := journal.Query{Unit: p.Unit, Priority: p.Priority, Since: p.Since, After: p.After}
		if err := q.Validate(); err != nil {
			return err
		}
		c.enqueue(subscribedFrame(ch.id))
		go c.runJournal(ctx, ch, q)
		return nil
	}
	return errValidation("unknown capability")
}

func errValidation(msg string) error {
	return httpapi.NewError(httpapi.CodeValidationFailed, msg)
}

func (c *conn) runMetrics(ctx context.Context, ch *channel, interval time.Duration) {
	c.trySendEvent(ch, c.hub.deps.Sampler.Sample())
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.trySendEvent(ch, c.hub.deps.Sampler.Sample())
		}
	}
}

func (c *conn) runServices(ctx context.Context, ch *channel) {
	units, err := c.hub.deps.Services.List(ctx)
	if err != nil {
		if ctx.Err() == nil {
			c.failChannel(ch, err)
		}
		return
	}
	if units == nil {
		units = []services.Unit{}
	}
	if !c.sendEvent(ctx, ch, map[string]any{"kind": "snapshot", "units": units}) {
		return
	}
	changes, err := c.hub.deps.Services.SubscribeChanges(ctx)
	if err != nil {
		if ctx.Err() == nil {
			c.failChannel(ch, err)
		}
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case u, ok := <-changes:
			if !ok {
				return
			}
			if !c.sendEvent(ctx, ch, map[string]any{"kind": "changed", "unit": u}) {
				return
			}
		}
	}
}

func (c *conn) runJournal(ctx context.Context, ch *channel, q journal.Query) {
	err := c.hub.deps.Journal.Follow(ctx, q, func(entry journal.Entry) bool {
		return c.sendEvent(ctx, ch, entry)
	})
	if err != nil && ctx.Err() == nil {
		c.failChannel(ch, err)
	}
}
