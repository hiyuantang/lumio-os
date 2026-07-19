// SPDX-License-Identifier: AGPL-3.0-only
package terminal

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestOpenWriteReadExit(t *testing.T) {
	m := NewManager()
	sess, err := m.Open(OpenOptions{Cols: 100, Rows: 30, Shell: "/bin/sh"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if sess.Token() == "" {
		t.Fatal("empty session token")
	}
	_, att, err := m.Attach(sess.Token())
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	defer att.Detach(true)

	if _, _, err := m.Attach(sess.Token()); !errors.Is(err, ErrConflict) {
		t.Errorf("second attach should conflict, got %v", err)
	}

	if err := sess.Write([]byte("echo term-test-ok\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	waitFor(t, "echo output", func() bool {
		sess.mu.Lock()
		defer sess.mu.Unlock()
		return strings.Contains(string(sess.scrollback), "term-test-ok")
	})
	if err := sess.Resize(120, 40); err != nil {
		t.Errorf("Resize: %v", err)
	}
	if err := sess.Resize(0, 40); !errors.Is(err, ErrValidation) {
		t.Errorf("bad resize: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	if err := sess.Write([]byte("exit 7\n")); err != nil {
		t.Fatalf("Write exit: %v", err)
	}
	select {
	case <-sess.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("session did not exit")
	}
	waitFor(t, "exit code", func() bool { return sess.ExitCode() == 7 })
}

func TestDetachReattachReplay(t *testing.T) {
	m := NewManager()
	m.Grace = 2 * time.Second
	sess, err := m.Open(OpenOptions{Shell: "/bin/sh"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_, att, err := m.Attach(sess.Token())
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if err := sess.Write([]byte("echo replay-marker\n")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "marker in scrollback", func() bool {
		sess.mu.Lock()
		defer sess.mu.Unlock()
		return strings.Contains(string(sess.scrollback), "replay-marker")
	})
	att.Detach(false)

	_, att2, err := m.Attach(sess.Token())
	if err != nil {
		t.Fatalf("reattach: %v", err)
	}
	defer att2.Detach(true)
	if !strings.Contains(string(att2.Replay), "replay-marker") {
		t.Errorf("replay does not contain marker: %q", att2.Replay)
	}
}

func TestGraceKill(t *testing.T) {
	m := NewManager()
	m.Grace = 200 * time.Millisecond
	sess, err := m.Open(OpenOptions{Shell: "/bin/sh"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_, att, err := m.Attach(sess.Token())
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	att.Detach(false)
	select {
	case <-sess.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("detached session was not killed after grace")
	}
	if _, _, err := m.Attach(sess.Token()); !errors.Is(err, ErrNotFound) {
		t.Errorf("killed session should be gone, got %v", err)
	}
}

func TestUnknownToken(t *testing.T) {
	m := NewManager()
	if _, _, err := m.Attach("does-not-exist"); !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v", err)
	}
}

func TestResolveShell(t *testing.T) {
	if _, err := resolveShell("sh"); err == nil {
		t.Error("relative shell should fail")
	}
	if _, err := resolveShell("/no/such/shell"); err == nil {
		t.Error("missing shell should fail")
	}
	if _, err := resolveShell("/tmp"); err == nil {
		t.Error("directory shell should fail")
	}
	if sh, err := resolveShell(""); err != nil || sh == "" {
		t.Errorf("default shell: %v %q", err, sh)
	}
}

func TestScrollbackCap(t *testing.T) {
	m := NewManager()
	sess, err := m.Open(OpenOptions{Shell: "/bin/sh"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer sess.Kill()
	if err := sess.Write([]byte("yes this-is-a-scrollback-test-line\n")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "scrollback cap", func() bool {
		sess.mu.Lock()
		defer sess.mu.Unlock()
		return len(sess.scrollback) > 0
	})
	time.Sleep(1500 * time.Millisecond)
	sess.mu.Lock()
	size := len(sess.scrollback)
	sess.mu.Unlock()
	if size > MaxScrollback {
		t.Errorf("scrollback size %d exceeds cap %d", size, MaxScrollback)
	}
}
