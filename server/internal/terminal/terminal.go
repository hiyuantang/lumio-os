// SPDX-License-Identifier: AGPL-3.0-only
package terminal

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sync"
	"time"

	"github.com/creack/pty"
)

const (
	DefaultCols   = 80
	DefaultRows   = 24
	MaxDimension  = 500
	MaxScrollback = 64 * 1024
	DefaultGrace  = 120 * time.Second
)

var (
	ErrNotFound   = errors.New("unknown or expired terminal session")
	ErrConflict   = errors.New("terminal session is already attached")
	ErrValidation = errors.New("invalid terminal options")
)

type OpenOptions struct {
	Cols  uint16
	Rows  uint16
	Shell string
}

type Session struct {
	token  string
	cmd    *exec.Cmd
	ptmx   *os.File
	grace  time.Duration
	onGone func(token string)

	mu         sync.Mutex
	scrollback []byte
	attached   bool
	dataCh     chan []byte
	gen        chan struct{}
	exited     bool
	exitCode   int
	graceTimer *time.Timer

	done chan struct{}
}

func (s *Session) Token() string { return s.token }

func (s *Session) Done() <-chan struct{} { return s.done }

func (s *Session) ExitCode() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exitCode
}

type Attachment struct {
	Replay []byte
	Events <-chan []byte
	done   func(kill bool)
}

func (a *Attachment) Detach(kill bool) { a.done(kill) }

func (s *Session) attach() (*Attachment, error) {
	s.mu.Lock()
	if s.exited {
		s.mu.Unlock()
		return nil, ErrNotFound
	}
	if s.attached {
		s.mu.Unlock()
		return nil, ErrConflict
	}
	s.attached = true
	ch := make(chan []byte, 16)
	s.dataCh = ch
	s.gen = make(chan struct{})
	if s.graceTimer != nil {
		s.graceTimer.Stop()
		s.graceTimer = nil
	}
	replay := append([]byte(nil), s.scrollback...)
	gen := s.gen
	s.mu.Unlock()

	var once sync.Once
	done := func(kill bool) {
		once.Do(func() {
			s.detach(gen)
			if kill {
				s.Kill()
			}
		})
	}
	return &Attachment{Replay: replay, Events: ch, done: done}, nil
}

func (s *Session) detach(gen chan struct{}) {
	s.mu.Lock()
	if s.gen == gen {
		s.attached = false
		s.dataCh = nil
		close(s.gen)
	}
	exited := s.exited
	s.mu.Unlock()
	if exited {
		s.gone()
		return
	}
	s.mu.Lock()
	s.graceTimer = time.AfterFunc(s.grace, func() { s.Kill() })
	s.mu.Unlock()
}

func (s *Session) Write(b []byte) error {
	s.mu.Lock()
	exited := s.exited
	s.mu.Unlock()
	if exited {
		return ErrNotFound
	}
	_, err := s.ptmx.Write(b)
	return err
}

func (s *Session) Resize(cols, rows uint16) error {
	if cols < 1 || rows < 1 || cols > MaxDimension || rows > MaxDimension {
		return ErrValidation
	}
	return pty.Setsize(s.ptmx, &pty.Winsize{Cols: cols, Rows: rows})
}

func (s *Session) Kill() {
	s.mu.Lock()
	if s.graceTimer != nil {
		s.graceTimer.Stop()
		s.graceTimer = nil
	}
	exited := s.exited
	s.mu.Unlock()
	if !exited && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	_ = s.ptmx.Close()
}

func (s *Session) gone() {
	if s.onGone != nil {
		s.onGone(s.token)
	}
}

func (s *Session) readLoop() {
	buf := make([]byte, 4096)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			s.mu.Lock()
			s.scrollback = append(s.scrollback, chunk...)
			if len(s.scrollback) > MaxScrollback {
				s.scrollback = append([]byte(nil), s.scrollback[len(s.scrollback)-MaxScrollback:]...)
			}
			ch := s.dataCh
			gen := s.gen
			s.mu.Unlock()
			if ch != nil {
				select {
				case ch <- chunk:
				case <-gen:
				case <-s.done:
				}
			}
		}
		if err != nil {
			return
		}
	}
}

func (s *Session) waitLoop() {
	err := s.cmd.Wait()
	code := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	}
	s.mu.Lock()
	s.exited = true
	s.exitCode = code
	attached := s.attached
	s.mu.Unlock()
	close(s.done)
	if !attached {
		s.gone()
	}
}

type Manager struct {
	Grace time.Duration

	mu       sync.Mutex
	sessions map[string]*Session
}

func NewManager() *Manager {
	return &Manager{Grace: DefaultGrace, sessions: map[string]*Session{}}
}

func (m *Manager) Open(opts OpenOptions) (*Session, error) {
	if opts.Cols == 0 {
		opts.Cols = DefaultCols
	}
	if opts.Rows == 0 {
		opts.Rows = DefaultRows
	}
	if opts.Cols > MaxDimension || opts.Rows > MaxDimension {
		return nil, fmt.Errorf("%w: cols/rows must be between 1 and %d", ErrValidation, MaxDimension)
	}
	shell, err := resolveShell(opts.Shell)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(shell)
	cmd.Env = cleanEnv(shell)
	if home := homeDir(); home != "" {
		cmd.Dir = home
	}
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: opts.Cols, Rows: opts.Rows})
	if err != nil {
		return nil, fmt.Errorf("starting %s: %w", shell, err)
	}
	s := &Session{
		token: randomToken(),
		cmd:   cmd,
		ptmx:  ptmx,
		grace: m.Grace,
		done:  make(chan struct{}),
	}
	s.onGone = func(token string) {
		m.mu.Lock()
		delete(m.sessions, token)
		m.mu.Unlock()
	}
	m.mu.Lock()
	m.sessions[s.token] = s
	m.mu.Unlock()
	go s.readLoop()
	go s.waitLoop()
	return s, nil
}

func (m *Manager) Attach(token string) (*Session, *Attachment, error) {
	m.mu.Lock()
	s, ok := m.sessions[token]
	m.mu.Unlock()
	if !ok {
		return nil, nil, ErrNotFound
	}
	att, err := s.attach()
	if err != nil {
		return nil, nil, err
	}
	return s, att, nil
}

func (m *Manager) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

func resolveShell(requested string) (string, error) {
	if requested == "" {
		if shell := os.Getenv("SHELL"); shell != "" {
			return shell, nil
		}
		return "/bin/sh", nil
	}
	if !filepath.IsAbs(requested) {
		return "", fmt.Errorf("%w: shell must be an absolute path", ErrValidation)
	}
	info, err := os.Stat(requested)
	if err != nil || info.IsDir() || info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("%w: shell is not executable", ErrValidation)
	}
	return requested, nil
}

func homeDir() string {
	if u, err := user.Current(); err == nil && u.HomeDir != "" {
		return u.HomeDir
	}
	return os.Getenv("HOME")
}

func cleanEnv(shell string) []string {
	env := []string{
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"SHELL=" + shell,
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"LANG=C.UTF-8",
	}
	if u, err := user.Current(); err == nil {
		if u.HomeDir != "" {
			env = append(env, "HOME="+u.HomeDir)
		}
		if u.Username != "" {
			env = append(env, "USER="+u.Username, "LOGNAME="+u.Username)
		}
	}
	if home := os.Getenv("HOME"); home != "" && !hasEnv(env, "HOME") {
		env = append(env, "HOME="+home)
	}
	return env
}

func hasEnv(env []string, key string) bool {
	prefix := key + "="
	for _, e := range env {
		if len(e) > len(prefix) && e[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

func randomToken() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return hex.EncodeToString(buf)
}
