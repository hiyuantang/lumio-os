// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type frame struct {
	Type    string          `json:"type"`
	Channel int             `json:"channel"`
	Seq     int             `json:"seq"`
	Data    json.RawMessage `json:"data"`
	Error   json.RawMessage `json:"error"`
}

type opts struct {
	url     string
	mode    string
	unit    string
	expect  string
	match   string
	cmd     string
	cookie  string
	csrf    string
	timeout time.Duration
}

func pass(format string, args ...any) {
	fmt.Printf("WSCHECK PASS "+format+"\n", args...)
	os.Exit(0)
}

func fail(format string, args ...any) {
	fmt.Printf("WSCHECK FAIL "+format+"\n", args...)
	os.Exit(1)
}

func main() {
	var o opts
	flag.StringVar(&o.url, "url", "ws://127.0.0.1:18080/api/v1/ws", "WebSocket endpoint")
	flag.StringVar(&o.mode, "mode", "metrics", "metrics | services | journal | terminal | terminal-reattach")
	flag.StringVar(&o.unit, "unit", "cron.service", "unit name for services mode")
	flag.StringVar(&o.expect, "expect", "", "expected activeState value in services mode (empty: any change)")
	flag.StringVar(&o.match, "match", "", "substring expected in the observed payload")
	flag.StringVar(&o.cmd, "cmd", "", "shell input to send in terminal mode")
	flag.StringVar(&o.cookie, "cookie", "", "Cookie header value (e.g. lumio_session=...)")
	flag.StringVar(&o.csrf, "csrf", "", "CSRF token appended as ?csrf= to the URL")
	flag.DurationVar(&o.timeout, "timeout", 10*time.Second, "overall timeout")
	flag.Parse()
	o.cmd = strings.ReplaceAll(o.cmd, `\n`, "\n")
	if o.csrf != "" {
		sep := "?"
		if strings.Contains(o.url, "?") {
			sep = "&"
		}
		o.url += sep + "csrf=" + o.csrf
	}

	switch o.mode {
	case "metrics", "services", "journal":
		runStreamMode(o)
	case "terminal":
		runTerminalMode(o)
	case "terminal-reattach":
		runReattachMode(o)
	default:
		fail("unknown mode %q", o.mode)
	}
}

type client struct {
	ws      *websocket.Conn
	writeMu sync.Mutex
}

func (o opts) headers() http.Header {
	h := http.Header{}
	if o.cookie != "" {
		h.Set("Cookie", o.cookie)
	}
	return h
}

func (o opts) dial() *client {
	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	ws, _, err := dialer.Dial(o.url, o.headers())
	if err != nil {
		fail("dial: %v", err)
	}
	c := &client{ws: ws}
	_ = ws.SetReadDeadline(time.Now().Add(o.timeout))
	hello := c.read()
	if hello.Type != "hello" {
		fail("expected hello, got %s", hello.Type)
	}
	fmt.Printf("WSCHECK hello received\n")
	return c
}

func (c *client) read() frame {
	var f frame
	if err := c.ws.ReadJSON(&f); err != nil {
		fail("read: %v", err)
	}
	return f
}

func (c *client) send(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		fail("marshal: %v", err)
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := c.ws.WriteMessage(websocket.TextMessage, b); err != nil {
		fail("write: %v", err)
	}
}

func (c *client) subscribe(channel int, capability string, params any) {
	c.send(map[string]any{"type": "subscribe", "channel": channel, "capability": capability, "params": params})
}

func (c *client) stdin(channel int, data string) {
	c.send(map[string]any{"type": "input", "channel": channel, "data": map[string]any{
		"kind": "stdin", "data": base64.StdEncoding.EncodeToString([]byte(data)),
	}})
}

func (c *client) resize(channel int, cols, rows int) {
	c.send(map[string]any{"type": "input", "channel": channel, "data": map[string]any{
		"kind": "resize", "cols": cols, "rows": rows,
	}})
}

func runStreamMode(o opts) {
	capability := map[string]string{
		"metrics":  "system.metrics",
		"services": "services.subscribe",
		"journal":  "journal.stream",
	}[o.mode]
	c := o.dial()
	defer c.ws.Close()

	params := map[string]any{}
	if o.mode == "metrics" {
		params["intervalMs"] = 1000
	}
	if o.mode == "journal" && o.unit != "" {
		params["unit"] = o.unit
	}
	c.subscribe(1, capability, params)

	for {
		f := c.read()
		switch f.Type {
		case "ping":
			c.send(map[string]any{"type": "pong"})
		case "subscribed":
			fmt.Printf("WSCHECK subscribed %s\n", capability)
		case "error":
			fail("error frame: %s", string(f.Error))
		case "closed":
			fail("channel closed: %s", string(f.Error))
		case "event":
			handleStreamEvent(o, f)
		}
	}
}

func handleStreamEvent(o opts, f frame) {
	switch o.mode {
	case "metrics":
		fmt.Printf("WSCHECK metrics event seq=%d\n", f.Seq)
		pass("metrics tick observed")
	case "services":
		var data struct {
			Kind  string `json:"kind"`
			Units []struct {
				Name string `json:"name"`
			} `json:"units"`
			Unit struct {
				Name        string `json:"name"`
				ActiveState string `json:"activeState"`
				SubState    string `json:"subState"`
			} `json:"unit"`
		}
		if err := json.Unmarshal(f.Data, &data); err != nil {
			fail("bad services event: %v", err)
		}
		switch data.Kind {
		case "snapshot":
			fmt.Printf("WSCHECK snapshot %d units\n", len(data.Units))
		case "changed":
			fmt.Printf("WSCHECK changed %s activeState=%s subState=%s\n", data.Unit.Name, data.Unit.ActiveState, data.Unit.SubState)
			if data.Unit.Name != o.unit {
				return
			}
			if o.expect == "" || data.Unit.ActiveState == o.expect {
				pass("services.subscribe observed %s activeState=%s", o.unit, data.Unit.ActiveState)
			}
		}
	case "journal":
		var data struct {
			Cursor  string `json:"cursor"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(f.Data, &data); err != nil {
			fail("bad journal event: %v", err)
		}
		if o.match == "" || strings.Contains(data.Message, o.match) {
			pass("journal.stream observed entry %q", truncate(data.Message, 80))
		}
	}
}

type termData struct {
	Kind string `json:"kind"`
	Data string `json:"data"`
	Code int    `json:"code"`
}

func runTerminalMode(o opts) {
	if o.cmd == "" {
		fail("-cmd is required in terminal mode")
	}
	c := o.dial()
	defer c.ws.Close()
	c.subscribe(1, "terminal.open", map[string]any{"cols": 80, "rows": 24})

	var accumulated strings.Builder
	stage := 0
	for {
		f := c.read()
		switch f.Type {
		case "ping":
			c.send(map[string]any{"type": "pong"})
		case "subscribed":
			fmt.Printf("WSCHECK subscribed terminal.open\n")
			go func() {
				time.Sleep(500 * time.Millisecond)
				c.stdin(1, o.cmd)
			}()
		case "error":
			fail("error frame: %s", string(f.Error))
		case "closed":
			if stage == 2 {
				pass("terminal exit observed, channel closed cleanly")
			}
			fail("channel closed early: %s", string(f.Error))
		case "event":
			var data termData
			if err := json.Unmarshal(f.Data, &data); err != nil {
				fail("bad terminal event: %v", err)
			}
			switch data.Kind {
			case "stdout":
				decoded, err := base64.StdEncoding.DecodeString(data.Data)
				if err != nil {
					fail("bad stdout base64: %v", err)
				}
				fmt.Printf("WSCHECK stdout %q\n", truncate(string(decoded), 120))
				accumulated.Write(decoded)
				if stage == 0 && strings.Contains(accumulated.String(), o.match) {
					fmt.Printf("WSCHECK terminal output matched %q\n", o.match)
					stage = 1
					c.resize(1, 132, 43)
					go func() {
						time.Sleep(300 * time.Millisecond)
						c.stdin(1, "exit\n")
					}()
					stage = 2
				}
			case "exit":
				fmt.Printf("WSCHECK terminal exit code=%d\n", data.Code)
			}
		}
	}
}

func runReattachMode(o opts) {
	if o.cmd == "" {
		fail("-cmd is required in terminal-reattach mode")
	}
	first := o.dial()
	first.subscribe(1, "terminal.open", map[string]any{"cols": 80, "rows": 24})

	var token string
	var accumulated strings.Builder
	for token == "" || !strings.Contains(accumulated.String(), o.match) {
		f := first.read()
		switch f.Type {
		case "ping":
			first.send(map[string]any{"type": "pong"})
		case "subscribed":
			var data struct {
				Session string `json:"session"`
			}
			_ = json.Unmarshal(f.Data, &data)
			token = data.Session
			fmt.Printf("WSCHECK session %s\n", token)
			go func() {
				time.Sleep(500 * time.Millisecond)
				first.stdin(1, o.cmd)
			}()
		case "error":
			fail("error frame: %s", string(f.Error))
		case "event":
			var data termData
			if err := json.Unmarshal(f.Data, &data); err != nil {
				fail("bad terminal event: %v", err)
			}
			if data.Kind == "stdout" {
				decoded, _ := base64.StdEncoding.DecodeString(data.Data)
				accumulated.Write(decoded)
			}
		}
	}
	fmt.Printf("WSCHECK first socket saw %q; dropping it\n", o.match)
	_ = first.ws.Close()
	time.Sleep(500 * time.Millisecond)

	second := o.dial()
	defer second.ws.Close()
	second.subscribe(1, "terminal.open", map[string]any{"session": token})
	for {
		f := second.read()
		switch f.Type {
		case "ping":
			second.send(map[string]any{"type": "pong"})
		case "subscribed":
			fmt.Printf("WSCHECK reattached with session\n")
		case "error":
			fail("reattach error frame: %s", string(f.Error))
		case "closed":
			fail("reattach channel closed: %s", string(f.Error))
		case "event":
			var data termData
			if err := json.Unmarshal(f.Data, &data); err != nil {
				fail("bad terminal event: %v", err)
			}
			if data.Kind == "stdout" {
				decoded, _ := base64.StdEncoding.DecodeString(data.Data)
				if strings.Contains(string(decoded), o.match) {
					pass("reattached and replayed scrollback contains %q", o.match)
				}
			}
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
