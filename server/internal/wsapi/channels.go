// SPDX-License-Identifier: AGPL-3.0-only
package wsapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"time"

	"lumio-os/server/internal/httpapi"
	"lumio-os/server/internal/journal"
	"lumio-os/server/internal/services"
	"lumio-os/server/internal/terminal"
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
		c.enqueue(subscribedFrame(ch.id, nil))
		go c.runMetrics(ctx, ch, time.Duration(interval)*time.Millisecond)
		return nil
	case "services.subscribe":
		if !c.hub.deps.Services.Available() {
			return services.ErrUnavailable
		}
		c.enqueue(subscribedFrame(ch.id, nil))
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
		c.enqueue(subscribedFrame(ch.id, nil))
		go c.runJournal(ctx, ch, q)
		return nil
	case "terminal.open":
		var p struct {
			Cols    uint16 `json:"cols"`
			Rows    uint16 `json:"rows"`
			Shell   string `json:"shell"`
			Session string `json:"session"`
		}
		if len(params) > 0 {
			if err := json.Unmarshal(params, &p); err != nil {
				return errValidation("invalid params: " + err.Error())
			}
		}
		if c.hub.deps.Terminal == nil {
			return errUnavailable("terminal is not available in this build")
		}
		if p.Session != "" {
			sess, att, err := c.hub.deps.Terminal.Attach(p.Session)
			if err != nil {
				return err
			}
			c.enqueue(subscribedFrame(ch.id, map[string]any{"session": sess.Token()}))
			go c.runTerminal(ctx, ch, sess, att)
			return nil
		}
		sess, err := c.hub.deps.Terminal.Open(terminal.OpenOptions{Cols: p.Cols, Rows: p.Rows, Shell: p.Shell})
		if err != nil {
			return err
		}
		_, att, err := c.hub.deps.Terminal.Attach(sess.Token())
		if err != nil {
			sess.Kill()
			return err
		}
		c.enqueue(subscribedFrame(ch.id, map[string]any{"session": sess.Token()}))
		go c.runTerminal(ctx, ch, sess, att)
		return nil
	}
	return errValidation("unknown capability")
}

func errValidation(msg string) error {
	return httpapi.NewError(httpapi.CodeValidationFailed, msg)
}

func errUnavailable(msg string) error {
	return httpapi.NewError(httpapi.CodeUnavailable, msg)
}

const replayChunkBytes = 16 * 1024

func (c *conn) runTerminal(ctx context.Context, ch *channel, sess *terminal.Session, att *terminal.Attachment) {
	defer att.Detach(ch.kill.Load())
	ch.setInput(func(data json.RawMessage) { c.handleTerminalInput(ch, sess, data) })
	for len(att.Replay) > 0 {
		n := min(len(att.Replay), replayChunkBytes)
		if !c.sendEvent(ctx, ch, map[string]any{"kind": "stdout", "data": base64.StdEncoding.EncodeToString(att.Replay[:n])}) {
			return
		}
		att.Replay = att.Replay[n:]
	}
	for {
		select {
		case <-ctx.Done():
			return
		case data := <-att.Events:
			if !c.sendEvent(ctx, ch, map[string]any{"kind": "stdout", "data": base64.StdEncoding.EncodeToString(data)}) {
				return
			}
		case <-sess.Done():
			for {
				select {
				case data := <-att.Events:
					if !c.sendEvent(ctx, ch, map[string]any{"kind": "stdout", "data": base64.StdEncoding.EncodeToString(data)}) {
						return
					}
				default:
					if !c.sendEvent(ctx, ch, map[string]any{"kind": "exit", "code": sess.ExitCode()}) {
						return
					}
					c.closeChannel(ch, nil)
					return
				}
			}
		}
	}
}

func (c *conn) handleTerminalInput(ch *channel, sess *terminal.Session, raw json.RawMessage) {
	var in struct {
		Kind string `json:"kind"`
		Data string `json:"data"`
		Cols uint16 `json:"cols"`
		Rows uint16 `json:"rows"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		c.enqueue(errorFrame(ch.id, httpapi.NewError(httpapi.CodeValidationFailed, "Malformed input frame.")))
		return
	}
	switch in.Kind {
	case "stdin":
		data, err := base64.StdEncoding.DecodeString(in.Data)
		if err != nil {
			c.enqueue(errorFrame(ch.id, httpapi.NewError(httpapi.CodeValidationFailed, "data must be base64.")))
			return
		}
		_ = sess.Write(data)
	case "resize":
		if err := sess.Resize(in.Cols, in.Rows); err != nil {
			c.enqueue(errorFrame(ch.id, httpapi.NewError(httpapi.CodeValidationFailed, "invalid terminal size.")))
		}
	default:
		c.enqueue(errorFrame(ch.id, httpapi.NewError(httpapi.CodeValidationFailed, "Unknown input kind.")))
	}
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
