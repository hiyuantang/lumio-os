// SPDX-License-Identifier: AGPL-3.0-only
package wsapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"lumio-os/server/internal/journal"
	"lumio-os/server/internal/services"
	"lumio-os/server/internal/system"
	"lumio-os/server/internal/terminal"
)

type fakeServices struct{ available bool }

func (f fakeServices) Available() bool { return f.available }
func (f fakeServices) List(context.Context) ([]services.Unit, error) {
	if !f.available {
		return nil, services.ErrUnavailable
	}
	return []services.Unit{{Name: "cron.service", ActiveState: "active"}}, nil
}
func (f fakeServices) SubscribeChanges(context.Context) (<-chan services.Unit, error) {
	return nil, services.ErrUnavailable
}

type fakeJournal struct{}

func (fakeJournal) Available() bool { return true }
func (fakeJournal) Query(context.Context, journal.Query) (journal.Result, error) {
	return journal.Result{}, nil
}
func (fakeJournal) Follow(ctx context.Context, q journal.Query, emit func(journal.Entry) bool) error {
	emit(journal.Entry{Cursor: "c1", Message: "first", Unit: q.Unit})
	emit(journal.Entry{Cursor: "c2", Message: "second"})
	<-ctx.Done()
	return nil
}

type frame struct {
	Type    string          `json:"type"`
	Channel int             `json:"channel"`
	Seq     int             `json:"seq"`
	Data    json.RawMessage `json:"data"`
	Error   *struct {
		Code string `json:"code"`
	} `json:"error"`
}

func dial(t *testing.T, hub *Hub, headers http.Header) (*websocket.Conn, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(hub.ServeHTTP))
	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(url, headers)
	if err != nil {
		srv.Close()
		t.Fatalf("dial: %v", err)
	}
	return ws, func() { ws.Close(); srv.Close() }
}

func readFrame(t *testing.T, ws *websocket.Conn) frame {
	t.Helper()
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	var f frame
	if err := ws.ReadJSON(&f); err != nil {
		t.Fatalf("read: %v", err)
	}
	return f
}

func subscribe(t *testing.T, ws *websocket.Conn, channel int, capability string, params string) {
	t.Helper()
	msg := `{"type":"subscribe","channel":` + strconv.Itoa(channel) + `,"capability":"` + capability + `","params":` + params + `}`
	if err := ws.WriteMessage(websocket.TextMessage, []byte(msg)); err != nil {
		t.Fatal(err)
	}
}

func newTestHub() *Hub {
	return NewHub(Deps{
		Version:  "test",
		Services: fakeServices{available: false},
		Journal:  fakeJournal{},
		Sampler:  system.NewSampler(),
		Terminal: terminal.NewManager(),
	})
}

func TestHello(t *testing.T) {
	ws, cleanup := dial(t, newTestHub(), nil)
	defer cleanup()
	f := readFrame(t, ws)
	if f.Type != "hello" {
		t.Fatalf("frame = %+v", f)
	}
}

func TestMetricsChannel(t *testing.T) {
	ws, cleanup := dial(t, newTestHub(), nil)
	defer cleanup()
	readFrame(t, ws)
	subscribe(t, ws, 1, "system.metrics", `{"intervalMs":500}`)
	f := readFrame(t, ws)
	if f.Type != "subscribed" || f.Channel != 1 {
		t.Fatalf("frame = %+v", f)
	}
	f = readFrame(t, ws)
	if f.Type != "event" || f.Seq != 1 {
		t.Fatalf("frame = %+v", f)
	}
	if !strings.Contains(string(f.Data), `"cpu"`) {
		t.Errorf("data = %s", f.Data)
	}
}

func TestServicesUnavailable(t *testing.T) {
	ws, cleanup := dial(t, newTestHub(), nil)
	defer cleanup()
	readFrame(t, ws)
	subscribe(t, ws, 2, "services.subscribe", `{}`)
	f := readFrame(t, ws)
	if f.Type != "error" || f.Error == nil || f.Error.Code != "unavailable" {
		t.Fatalf("frame = %+v", f)
	}
}

func TestUnavailableCapability(t *testing.T) {
	ws, cleanup := dial(t, newTestHub(), nil)
	defer cleanup()
	readFrame(t, ws)
	subscribe(t, ws, 3, "terminal.open", `{"session":"no-such-token"}`)
	f := readFrame(t, ws)
	if f.Type != "error" || f.Error == nil || f.Error.Code != "not_found" {
		t.Fatalf("frame = %+v", f)
	}
	subscribe(t, ws, 4, "updates.progress", `{}`)
	f = readFrame(t, ws)
	if f.Type != "error" || f.Error == nil || f.Error.Code != "unavailable" {
		t.Fatalf("frame = %+v", f)
	}
	subscribe(t, ws, 5, "bogus.cap", `{}`)
	f = readFrame(t, ws)
	if f.Type != "error" || f.Error == nil || f.Error.Code != "validation_failed" {
		t.Fatalf("frame = %+v", f)
	}
}

func TestJournalStream(t *testing.T) {
	ws, cleanup := dial(t, newTestHub(), nil)
	defer cleanup()
	readFrame(t, ws)
	subscribe(t, ws, 5, "journal.stream", `{"unit":"cron.service"}`)
	f := readFrame(t, ws)
	if f.Type != "subscribed" {
		t.Fatalf("frame = %+v", f)
	}
	first := readFrame(t, ws)
	second := readFrame(t, ws)
	if first.Type != "event" || first.Seq != 1 || second.Seq != 2 {
		t.Fatalf("frames = %+v %+v", first, second)
	}
	if !strings.Contains(string(first.Data), `"cursor":"c1"`) {
		t.Errorf("data = %s", first.Data)
	}
}

func TestDuplicateChannelRejected(t *testing.T) {
	ws, cleanup := dial(t, newTestHub(), nil)
	defer cleanup()
	readFrame(t, ws)
	subscribe(t, ws, 1, "system.metrics", `{}`)
	if f := readFrame(t, ws); f.Type != "subscribed" {
		t.Fatalf("frame = %+v", f)
	}
	subscribe(t, ws, 1, "journal.stream", `{}`)
	for {
		f := readFrame(t, ws)
		if f.Type == "event" {
			continue
		}
		if f.Type != "error" || f.Error == nil || f.Error.Code != "validation_failed" {
			t.Fatalf("frame = %+v", f)
		}
		return
	}
}

func TestUnsubscribe(t *testing.T) {
	ws, cleanup := dial(t, newTestHub(), nil)
	defer cleanup()
	readFrame(t, ws)
	subscribe(t, ws, 7, "journal.stream", `{}`)
	if f := readFrame(t, ws); f.Type != "subscribed" {
		t.Fatalf("frame = %+v", f)
	}
	if err := ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"unsubscribe","channel":7}`)); err != nil {
		t.Fatal(err)
	}
	for {
		f := readFrame(t, ws)
		if f.Type == "event" {
			continue
		}
		if f.Type != "closed" || f.Channel != 7 {
			t.Fatalf("frame = %+v", f)
		}
		return
	}
}

func TestTerminalFlow(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh")
	}
	ws, cleanup := dial(t, newTestHub(), nil)
	defer cleanup()
	readFrame(t, ws)
	subscribe(t, ws, 9, "terminal.open", `{"cols":80,"rows":24,"shell":"/bin/sh"}`)
	f := readFrame(t, ws)
	if f.Type != "subscribed" {
		t.Fatalf("frame = %+v", f)
	}
	var subData struct {
		Session string `json:"session"`
	}
	if err := json.Unmarshal(f.Data, &subData); err != nil || subData.Session == "" {
		t.Fatalf("subscribed data = %s err=%v", f.Data, err)
	}
	sendInput := func(v map[string]any) {
		b, _ := json.Marshal(map[string]any{"type": "input", "channel": 9, "data": v})
		if err := ws.WriteMessage(websocket.TextMessage, b); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(500 * time.Millisecond)
	sendInput(map[string]any{"kind": "stdin", "data": base64.StdEncoding.EncodeToString([]byte("echo ws-term-ok\n"))})

	var output strings.Builder
	sawExit := false
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		_ = ws.SetReadDeadline(time.Now().Add(8 * time.Second))
		f := readFrame(t, ws)
		if f.Type != "event" {
			continue
		}
		var data struct {
			Kind string `json:"kind"`
			Data string `json:"data"`
			Code int    `json:"code"`
		}
		if err := json.Unmarshal(f.Data, &data); err != nil {
			t.Fatalf("bad event: %v", err)
		}
		switch data.Kind {
		case "stdout":
			decoded, err := base64.StdEncoding.DecodeString(data.Data)
			if err != nil {
				t.Fatalf("bad base64: %v", err)
			}
			output.Write(decoded)
			if strings.Contains(output.String(), "ws-term-ok") {
				sendInput(map[string]any{"kind": "resize", "cols": 132, "rows": 43})
				time.Sleep(300 * time.Millisecond)
				sendInput(map[string]any{"kind": "stdin", "data": base64.StdEncoding.EncodeToString([]byte("exit\n"))})
			}
		case "exit":
			sawExit = true
		}
		if sawExit {
			break
		}
	}
	if !sawExit {
		t.Fatalf("no exit event; output so far: %q", output.String())
	}
	f = readFrame(t, ws)
	if f.Type != "closed" || f.Channel != 9 || f.Error != nil {
		t.Fatalf("expected clean close, got %+v", f)
	}
}

func TestOriginRejected(t *testing.T) {
	hub := newTestHub()
	srv := httptest.NewServer(http.HandlerFunc(hub.ServeHTTP))
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	headers := http.Header{"Origin": []string{"https://evil.example.com"}}
	_, res, err := websocket.DefaultDialer.Dial(url, headers)
	if err == nil {
		t.Fatal("cross-origin dial should fail")
	}
	if res == nil || res.StatusCode != http.StatusForbidden {
		t.Errorf("status = %v", res)
	}
}

func TestOriginLocalhostAllowed(t *testing.T) {
	hub := newTestHub()
	srv := httptest.NewServer(http.HandlerFunc(hub.ServeHTTP))
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	headers := http.Header{"Origin": []string{"http://localhost:5173"}}
	ws, _, err := websocket.DefaultDialer.Dial(url, headers)
	if err != nil {
		t.Fatalf("localhost origin should pass: %v", err)
	}
	ws.Close()
}
