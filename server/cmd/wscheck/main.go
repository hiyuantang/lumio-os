// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
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

func pass(format string, args ...any) {
	fmt.Printf("WSCHECK PASS "+format+"\n", args...)
	os.Exit(0)
}

func fail(format string, args ...any) {
	fmt.Printf("WSCHECK FAIL "+format+"\n", args...)
	os.Exit(1)
}

func main() {
	url := flag.String("url", "ws://127.0.0.1:18080/api/v1/ws", "WebSocket endpoint")
	mode := flag.String("mode", "metrics", "metrics | services | journal")
	unit := flag.String("unit", "cron.service", "unit name for services mode")
	expect := flag.String("expect", "", "expected activeState value in services mode (empty: any change)")
	match := flag.String("match", "", "substring expected in the journal message")
	timeout := flag.Duration("timeout", 10*time.Second, "overall timeout")
	flag.Parse()

	capability := map[string]string{
		"metrics":  "system.metrics",
		"services": "services.subscribe",
		"journal":  "journal.stream",
	}[*mode]
	if capability == "" {
		fail("unknown mode %q", *mode)
	}

	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	ws, _, err := dialer.Dial(*url, http.Header{})
	if err != nil {
		fail("dial: %v", err)
	}
	defer ws.Close()
	deadline := time.Now().Add(*timeout)
	_ = ws.SetReadDeadline(deadline)

	read := func() frame {
		var f frame
		if err := ws.ReadJSON(&f); err != nil {
			fail("read: %v", err)
		}
		return f
	}

	hello := read()
	if hello.Type != "hello" {
		fail("expected hello, got %s", hello.Type)
	}
	fmt.Printf("WSCHECK hello received\n")

	params := `{}`
	if *mode == "metrics" {
		params = `{"intervalMs":1000}`
	} else if *mode == "journal" && *unit != "" {
		params = fmt.Sprintf(`{"unit":%q}`, *unit)
	}
	sub := fmt.Sprintf(`{"type":"subscribe","channel":1,"capability":%q,"params":%s}`, capability, params)
	if err := ws.WriteMessage(websocket.TextMessage, []byte(sub)); err != nil {
		fail("subscribe write: %v", err)
	}

	for {
		f := read()
		switch f.Type {
		case "ping":
			_ = ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"pong"}`))
		case "subscribed":
			fmt.Printf("WSCHECK subscribed %s\n", capability)
		case "error":
			fail("error frame: %s", string(f.Error))
		case "closed":
			fail("channel closed: %s", string(f.Error))
		case "event":
			handleEvent(*mode, *unit, *expect, *match, f)
		}
	}
}

func handleEvent(mode, unit, expect, match string, f frame) {
	switch mode {
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
			if data.Unit.Name != unit {
				return
			}
			if expect == "" || data.Unit.ActiveState == expect {
				pass("services.subscribe observed %s activeState=%s", unit, data.Unit.ActiveState)
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
		if match == "" || strings.Contains(data.Message, match) {
			pass("journal.stream observed entry %q", truncate(data.Message, 80))
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
