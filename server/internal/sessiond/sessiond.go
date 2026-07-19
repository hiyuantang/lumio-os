// SPDX-License-Identifier: AGPL-3.0-only
package sessiond

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"time"

	"lumio-os/server/internal/auth"
)

const (
	idleExpiry     = 7 * 24 * time.Hour
	absoluteExpiry = 30 * 24 * time.Hour
	ReauthWindow   = 5 * time.Minute
	agentSockGroup = "lumio-gw"
)

var ErrUnauthorized = errors.New("unauthorized")
var ErrNotFound = errors.New("not found")
var ErrUnavailable = errors.New("session daemon unavailable")

var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

type User struct {
	Name string `json:"name"`
	UID  uint32 `json:"uid"`
	GID  uint32 `json:"gid"`
	Home string `json:"home"`
}

type Session struct {
	Token       string
	CSRF        string
	User        User
	CreatedAt   time.Time
	LastSeenAt  time.Time
	ReauthUntil time.Time
}

type Config struct {
	RunDir          string
	SocketPath      string
	InsecureDevAuth string
}

type Daemon struct {
	cfg Config

	mu       sync.Mutex
	sessions map[string]*Session
	agents   map[uint32]*agentProc

	spawnAgentFn func(uid uint32) (*agentProc, error)
}

func New(cfg Config) *Daemon {
	return &Daemon{
		cfg:      cfg,
		sessions: map[string]*Session{},
		agents:   map[uint32]*agentProc{},
	}
}

func (d *Daemon) Run() error {
	if err := os.MkdirAll(d.cfg.RunDir, 0o755); err != nil {
		return err
	}
	if err := os.Chmod(d.cfg.RunDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(d.cfg.RunDir, "users"), 0o750); err != nil {
		return err
	}
	_ = os.Remove(d.cfg.SocketPath)
	ln, err := net.Listen("unix", d.cfg.SocketPath)
	if err != nil {
		return err
	}
	if os.Geteuid() == 0 {
		if gid, err := groupID(agentSockGroup); err == nil {
			_ = os.Chown(d.cfg.RunDir, 0, int(gid))
			_ = os.Chown(filepath.Join(d.cfg.RunDir, "users"), 0, int(gid))
			_ = os.Chown(d.cfg.SocketPath, 0, int(gid))
		}
	}
	if err := os.Chmod(d.cfg.SocketPath, 0o660); err != nil {
		return err
	}
	if d.cfg.InsecureDevAuth != "" {
		log.Printf("sessiond: WARNING insecure dev auth enabled for user %q; any password is accepted", d.cfg.InsecureDevAuth)
	} else if !auth.Available {
		log.Printf("sessiond: PAM not available in this build; login will answer unavailable")
	}
	log.Printf("sessiond: listening on %s", d.cfg.SocketPath)
	return serveUnix(ln, d.mux())
}

func (d *Daemon) mux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login", d.handleLogin)
	mux.HandleFunc("POST /logout", d.handleLogout)
	mux.HandleFunc("POST /validate", d.handleValidate)
	mux.HandleFunc("POST /reauth", d.handleReauth)
	mux.HandleFunc("POST /session/check", d.handleSessionCheck)
	return mux
}

func (d *Daemon) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	if !usernamePattern.MatchString(req.Username) {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if d.cfg.InsecureDevAuth != "" {
		if req.Username != d.cfg.InsecureDevAuth {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
	} else {
		if !auth.Available {
			writeError(w, http.StatusServiceUnavailable, "PAM unavailable")
			return
		}
		if err := auth.Authenticate(req.Username, req.Password); err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
	}
	u, err := lookupUser(req.Username)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	agentSocket, err := d.ensureAgent(u.UID)
	if err != nil {
		log.Printf("sessiond: spawn agent for uid %d: %v", u.UID, err)
		writeError(w, http.StatusServiceUnavailable, "agent unavailable")
		return
	}
	sess := &Session{
		Token:      randomToken(),
		CSRF:       randomToken(),
		User:       u,
		CreatedAt:  time.Now(),
		LastSeenAt: time.Now(),
	}
	d.mu.Lock()
	d.sessions[sess.Token] = sess
	d.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"token":       sess.Token,
		"csrf":        sess.CSRF,
		"user":        sess.User,
		"agentSocket": agentSocket,
	})
}

func (d *Daemon) handleLogout(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	d.mu.Lock()
	sess, ok := d.sessions[req.Token]
	if ok {
		delete(d.sessions, req.Token)
	}
	remaining := 0
	for _, s := range d.sessions {
		if s.User.UID == sess.User.UID {
			remaining++
		}
	}
	d.mu.Unlock()
	if ok && remaining == 0 {
		d.stopAgentIfIdle(sess.User.UID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (d *Daemon) handleValidate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	sess, err := d.lookup(req.Token, true)
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token":       sess.Token,
		"csrf":        sess.CSRF,
		"user":        sess.User,
		"agentSocket": agentSocketPath(d.cfg.RunDir, sess.User.UID),
		"reauthUntil": sess.ReauthUntil.UnixMilli(),
	})
}

func (d *Daemon) handleReauth(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	sess, err := d.lookup(req.Token, false)
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if d.cfg.InsecureDevAuth == "" {
		if !auth.Available {
			writeError(w, http.StatusServiceUnavailable, "PAM unavailable")
			return
		}
		if err := auth.Authenticate(sess.User.Name, req.Password); err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
	}
	sess.ReauthUntil = time.Now().Add(ReauthWindow)
	writeJSON(w, http.StatusOK, map[string]any{"reauthenticatedUntil": sess.ReauthUntil.UnixMilli()})
}

func (d *Daemon) handleSessionCheck(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	sess, err := d.lookup(req.Token, false)
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"uid":         sess.User.UID,
		"reauthUntil": sess.ReauthUntil.UnixMilli(),
	})
}

func (d *Daemon) lookup(token string, touch bool) (*Session, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	sess, ok := d.sessions[token]
	if !ok {
		return nil, ErrNotFound
	}
	now := time.Now()
	if now.Sub(sess.LastSeenAt) > idleExpiry || now.Sub(sess.CreatedAt) > absoluteExpiry {
		delete(d.sessions, token)
		return nil, ErrNotFound
	}
	if touch {
		sess.LastSeenAt = now
	}
	return sess, nil
}

func lookupUser(name string) (User, error) {
	u, err := user.Lookup(name)
	if err != nil {
		return User{}, err
	}
	uid, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		return User{}, err
	}
	gid, err := strconv.ParseUint(u.Gid, 10, 32)
	if err != nil {
		return User{}, err
	}
	return User{Name: u.Username, UID: uint32(uid), GID: uint32(gid), Home: u.HomeDir}, nil
}

func randomToken() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return hex.EncodeToString(buf)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}
