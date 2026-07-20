// SPDX-License-Identifier: AGPL-3.0-only
package wsapi

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"lumio-os/server/internal/httpapi"
)

type inFrame struct {
	Type       string          `json:"type"`
	Channel    int             `json:"channel"`
	Capability string          `json:"capability"`
	Params     json.RawMessage `json:"params"`
	Data       json.RawMessage `json:"data"`
}

type eventFrame struct {
	Type    string `json:"type"`
	Channel int    `json:"channel"`
	Seq     int    `json:"seq"`
	Data    any    `json:"data"`
}

type channel struct {
	id         int
	capability string
	cancel     context.CancelFunc
	seq        int
	kill       atomic.Bool

	inputMu sync.Mutex
	input   func(json.RawMessage)
}

func (ch *channel) setInput(fn func(json.RawMessage)) {
	ch.inputMu.Lock()
	ch.input = fn
	ch.inputMu.Unlock()
}

func (ch *channel) getInput() func(json.RawMessage) {
	ch.inputMu.Lock()
	defer ch.inputMu.Unlock()
	return ch.input
}

type conn struct {
	hub  *Hub
	ws   *websocket.Conn
	send chan []byte
	done chan struct{}

	ctx    context.Context
	cancel context.CancelFunc

	closeOnce sync.Once
	missed    atomic.Int32

	mu       sync.Mutex
	channels map[int]*channel
}

func newConn(ws *websocket.Conn, hub *Hub) *conn {
	ctx, cancel := context.WithCancel(context.Background())
	return &conn{
		hub:      hub,
		ws:       ws,
		send:     make(chan []byte, sendBuffer),
		done:     make(chan struct{}),
		ctx:      ctx,
		cancel:   cancel,
		channels: map[int]*channel{},
	}
}

func (c *conn) close() {
	c.closeOnce.Do(func() {
		close(c.done)
		c.cancel()
		_ = c.ws.Close()
	})
}

func (c *conn) enqueue(v any) bool {
	b, err := json.Marshal(v)
	if err != nil {
		return false
	}
	select {
	case c.send <- b:
		return true
	case <-c.done:
		return false
	}
}

func (c *conn) tryEnqueue(v any) bool {
	b, err := json.Marshal(v)
	if err != nil {
		return false
	}
	select {
	case c.send <- b:
		return true
	default:
		return false
	case <-c.done:
		return false
	}
}

func (c *conn) writeLoop() {
	for {
		select {
		case <-c.done:
			return
		case b := <-c.send:
			_ = c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.ws.WriteMessage(websocket.TextMessage, b); err != nil {
				c.close()
				return
			}
		}
	}
}

func (c *conn) pingLoop() {
	ticker := time.NewTicker(pingInterval * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			if c.missed.Add(1) > maxMissedPongs {
				c.close()
				return
			}
			c.enqueue(map[string]any{"type": "ping", "ts": time.Now().UnixMilli()})
		}
	}
}

func (c *conn) readLoop() {
	defer c.close()
	c.ws.SetReadLimit(maxFrameBytes)
	for {
		_, data, err := c.ws.ReadMessage()
		if err != nil {
			return
		}
		var f inFrame
		if err := json.Unmarshal(data, &f); err != nil {
			c.enqueue(errorFrame(0, httpapi.NewError(httpapi.CodeValidationFailed, "Malformed frame.")))
			continue
		}
		switch f.Type {
		case "pong":
			c.missed.Store(0)
		case "subscribe":
			c.handleSubscribe(f)
		case "unsubscribe":
			c.handleUnsubscribe(f)
		case "input":
			c.handleInput(f)
		default:
			c.enqueue(errorFrame(f.Channel, httpapi.NewError(httpapi.CodeValidationFailed, "Unknown frame type.")))
		}
	}
}

func subscribedFrame(id int, data any) any {
	f := map[string]any{"type": "subscribed", "channel": id}
	if data != nil {
		f["data"] = data
	}
	return f
}

func errorFrame(id int, err *httpapi.Error) any {
	return map[string]any{"type": "error", "channel": id, "error": err}
}

func closedFrame(id int, err *httpapi.Error) any {
	return map[string]any{"type": "closed", "channel": id, "error": err}
}

func (c *conn) registerChannel(ch *channel) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.channels[ch.id]; exists {
		return false
	}
	c.channels[ch.id] = ch
	return true
}

func (c *conn) removeChannel(id int) {
	c.mu.Lock()
	delete(c.channels, id)
	c.mu.Unlock()
}

func (c *conn) handleSubscribe(f inFrame) {
	if f.Channel <= 0 {
		c.enqueue(errorFrame(0, httpapi.NewError(httpapi.CodeValidationFailed, "channel must be a positive integer.")))
		return
	}
	switch f.Capability {
	case "system.metrics", "services.subscribe", "journal.stream", "terminal.open", "updates.progress":
	default:
		c.enqueue(errorFrame(f.Channel, httpapi.NewError(httpapi.CodeValidationFailed, "Unknown capability.")))
		return
	}
	ctx, cancel := context.WithCancel(c.ctx)
	ch := &channel{id: f.Channel, capability: f.Capability, cancel: cancel}
	if !c.registerChannel(ch) {
		cancel()
		c.enqueue(errorFrame(f.Channel, httpapi.NewError(httpapi.CodeValidationFailed, "Channel id is already in use.")))
		return
	}
	if err := c.startChannel(ctx, ch, f.Params); err != nil {
		c.removeChannel(ch.id)
		cancel()
		c.enqueue(errorFrame(f.Channel, httpapi.MapError(err)))
	}
}

func (c *conn) handleUnsubscribe(f inFrame) {
	c.mu.Lock()
	ch, ok := c.channels[f.Channel]
	if ok {
		delete(c.channels, f.Channel)
	}
	c.mu.Unlock()
	if !ok {
		c.enqueue(errorFrame(f.Channel, httpapi.NewError(httpapi.CodeNotFound, "No such channel.")))
		return
	}
	ch.kill.Store(true)
	ch.cancel()
	c.enqueue(closedFrame(f.Channel, nil))
}

func (c *conn) handleInput(f inFrame) {
	c.mu.Lock()
	ch, ok := c.channels[f.Channel]
	c.mu.Unlock()
	if !ok {
		c.enqueue(errorFrame(f.Channel, httpapi.NewError(httpapi.CodeNotFound, "No such channel.")))
		return
	}
	fn := ch.getInput()
	if fn == nil {
		c.enqueue(errorFrame(f.Channel, httpapi.NewError(httpapi.CodeValidationFailed, "This capability does not accept input.")))
		return
	}
	fn(f.Data)
}

func (c *conn) sendEvent(ctx context.Context, ch *channel, data any) bool {
	frame := eventFrame{Type: "event", Channel: ch.id, Seq: ch.seq + 1, Data: data}
	b, err := json.Marshal(frame)
	if err != nil {
		return false
	}
	select {
	case c.send <- b:
		ch.seq++
		return true
	case <-c.done:
		return false
	case <-ctx.Done():
		return false
	}
}

func (c *conn) trySendEvent(ch *channel, data any) {
	frame := eventFrame{Type: "event", Channel: ch.id, Seq: ch.seq + 1, Data: data}
	if c.tryEnqueue(frame) {
		ch.seq++
	}
}

func (c *conn) failChannel(ch *channel, err error) {
	c.closeChannel(ch, httpapi.MapError(err))
}

func (c *conn) closeChannel(ch *channel, apiErr *httpapi.Error) {
	c.enqueue(closedFrame(ch.id, apiErr))
	c.removeChannel(ch.id)
	ch.cancel()
}
