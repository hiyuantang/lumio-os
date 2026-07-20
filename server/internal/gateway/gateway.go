// SPDX-License-Identifier: AGPL-3.0-only
package gateway

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"

	"lumio-os/server/internal/httpapi"
	"lumio-os/server/internal/ipc"
)

const contentSecurityPolicy = "default-src 'self'; base-uri 'none'; object-src 'none'; frame-ancestors 'none'; form-action 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self' ws: wss:; worker-src 'none'; manifest-src 'self'"

type User struct {
	Name string `json:"name"`
	UID  uint32 `json:"uid"`
	GID  uint32 `json:"gid"`
	Home string `json:"home"`
}

type sessionInfo struct {
	Token       string
	CSRF        string
	User        User
	AgentSocket string
}

type Config struct {
	Addr           string
	SessiondSocket string
	Static         http.Handler
	Version        string
}

type Gateway struct {
	cfg      Config
	sessiond *http.Client
	limiter  *loginLimiter

	clientsMu sync.Mutex
	clients   map[string]*http.Client
}

func New(cfg Config) *Gateway {
	return &Gateway{
		cfg:      cfg,
		sessiond: ipc.HTTPClient(cfg.SessiondSocket),
		limiter:  newLoginLimiter(),
		clients:  map[string]*http.Client{},
	}
}

func (g *Gateway) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/login", g.handleLogin)
	mux.HandleFunc("POST /api/v1/auth/logout", g.handleLogout)
	mux.HandleFunc("GET /api/v1/auth/session", g.handleSession)
	mux.HandleFunc("POST /api/v1/auth/reauth", g.handleReauth)
	mux.HandleFunc("GET /api/v1/meta/version", g.handleVersion)
	mux.HandleFunc("GET /api/v1/ws", g.handleWS)
	for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
		mux.HandleFunc(method+" /api/v1/", g.handleAPI)
	}
	if g.cfg.Static != nil {
		mux.Handle("GET /", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				httpapi.WriteError(w, httpapi.NewError(httpapi.CodeNotFound, "Unknown endpoint."))
				return
			}
			g.cfg.Static.ServeHTTP(w, r)
		}))
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		httpapi.WriteError(w, httpapi.NewError(httpapi.CodeNotFound, "Unknown endpoint."))
	})
	return securityHeaders(mux)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", contentSecurityPolicy)
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Permitted-Cross-Domain-Policies", "none")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		if r.TLS != nil {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}

func (g *Gateway) handleVersion(w http.ResponseWriter, _ *http.Request) {
	httpapi.WriteData(w, map[string]any{
		"version":          g.cfg.Version,
		"protocolVersions": []int{1},
	})
}

func (g *Gateway) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpapi.WriteError(w, httpapi.NewError(httpapi.CodeValidationFailed, "Body must be a JSON object."))
		return
	}
	key := req.Username + "|" + clientIP(r)
	if retryAfter := g.limiter.blocked(key); retryAfter > 0 {
		err := httpapi.NewError(httpapi.CodeBusy, "Too many failed attempts; retry later.")
		err.Details = map[string]any{"retryAfterMs": retryAfter.Milliseconds()}
		httpapi.WriteError(w, err)
		return
	}
	var resp struct {
		Token       string `json:"token"`
		CSRF        string `json:"csrf"`
		User        User   `json:"user"`
		AgentSocket string `json:"agentSocket"`
	}
	status, err := g.sessiondCall("/login", map[string]any{"username": req.Username, "password": req.Password}, &resp)
	if err != nil {
		httpapi.WriteError(w, httpapi.NewError(httpapi.CodeUnavailable, "The session daemon is unavailable."))
		return
	}
	if status == http.StatusUnauthorized {
		g.limiter.record(key)
		httpapi.WriteError(w, httpapi.NewError(httpapi.CodeUnauthorized, "Invalid username or password."))
		return
	}
	if status != http.StatusOK {
		httpapi.WriteError(w, httpapi.NewError(httpapi.CodeUnavailable, "Login is unavailable."))
		return
	}
	g.limiter.reset(key)
	g.setSessionCookies(w, r, resp.Token, resp.CSRF)
	httpapi.WriteData(w, map[string]any{"user": resp.User, "csrf": resp.CSRF})
}

func (g *Gateway) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("lumio_session"); err == nil && cookie.Value != "" {
		_, _ = g.sessiondCall("/logout", map[string]any{"token": cookie.Value}, nil)
	}
	g.clearSessionCookies(w, r)
	httpapi.WriteData(w, map[string]any{"ok": true})
}

func (g *Gateway) handleSession(w http.ResponseWriter, r *http.Request) {
	sess, apiErr := g.requireSession(r)
	if apiErr != nil {
		httpapi.WriteError(w, apiErr)
		return
	}
	httpapi.WriteData(w, map[string]any{"user": sess.User})
}

func (g *Gateway) handleReauth(w http.ResponseWriter, r *http.Request) {
	sess, apiErr := g.requireSession(r)
	if apiErr != nil {
		httpapi.WriteError(w, apiErr)
		return
	}
	if apiErr := g.checkCSRF(r, sess); apiErr != nil {
		httpapi.WriteError(w, apiErr)
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpapi.WriteError(w, httpapi.NewError(httpapi.CodeValidationFailed, "Body must be a JSON object."))
		return
	}
	var resp struct {
		ReauthenticatedUntil int64 `json:"reauthenticatedUntil"`
	}
	status, err := g.sessiondCall("/reauth", map[string]any{"token": sess.Token, "password": req.Password}, &resp)
	if err != nil {
		httpapi.WriteError(w, httpapi.NewError(httpapi.CodeUnavailable, "The session daemon is unavailable."))
		return
	}
	if status == http.StatusUnauthorized {
		httpapi.WriteError(w, httpapi.NewError(httpapi.CodeUnauthorized, "Invalid password."))
		return
	}
	if status != http.StatusOK {
		httpapi.WriteError(w, httpapi.NewError(httpapi.CodeUnavailable, "Reauthentication is unavailable."))
		return
	}
	httpapi.WriteData(w, map[string]any{"reauthenticatedUntil": resp.ReauthenticatedUntil})
}

func (g *Gateway) handleAPI(w http.ResponseWriter, r *http.Request) {
	sess, apiErr := g.requireSession(r)
	if apiErr != nil {
		httpapi.WriteError(w, apiErr)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		if apiErr := g.checkCSRF(r, sess); apiErr != nil {
			httpapi.WriteError(w, apiErr)
			return
		}
	}
	g.proxyREST(w, r, sess)
}

func (g *Gateway) requireSession(r *http.Request) (*sessionInfo, *httpapi.Error) {
	cookie, err := r.Cookie("lumio_session")
	if err != nil || cookie.Value == "" {
		return nil, httpapi.NewError(httpapi.CodeUnauthorized, "No session.")
	}
	var resp struct {
		Token       string `json:"token"`
		CSRF        string `json:"csrf"`
		User        User   `json:"user"`
		AgentSocket string `json:"agentSocket"`
	}
	status, err := g.sessiondCall("/validate", map[string]any{"token": cookie.Value}, &resp)
	if err != nil {
		return nil, httpapi.NewError(httpapi.CodeUnavailable, "The session daemon is unavailable.")
	}
	if status != http.StatusOK {
		return nil, httpapi.NewError(httpapi.CodeUnauthorized, "Session expired or unknown.")
	}
	return &sessionInfo{Token: resp.Token, CSRF: resp.CSRF, User: resp.User, AgentSocket: resp.AgentSocket}, nil
}

func (g *Gateway) checkCSRF(r *http.Request, sess *sessionInfo) *httpapi.Error {
	header := r.Header.Get("X-Lumio-CSRF")
	if header == "" || header != sess.CSRF {
		return httpapi.NewError(httpapi.CodeForbidden, "CSRF check failed.")
	}
	cookie, err := r.Cookie("lumio_csrf")
	if err != nil || cookie.Value != sess.CSRF {
		return httpapi.NewError(httpapi.CodeForbidden, "CSRF check failed.")
	}
	return nil
}

func (g *Gateway) setSessionCookies(w http.ResponseWriter, r *http.Request, token, csrf string) {
	secure := r.TLS != nil
	http.SetCookie(w, &http.Cookie{
		Name:     "lumio_session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "lumio_csrf",
		Value:    csrf,
		Path:     "/",
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
}

func (g *Gateway) clearSessionCookies(w http.ResponseWriter, r *http.Request) {
	secure := r.TLS != nil
	for _, name := range []string{"lumio_session", "lumio_csrf"} {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: name == "lumio_session",
			Secure:   secure,
			SameSite: http.SameSiteStrictMode,
		})
	}
}

func (g *Gateway) sessiondCall(path string, body any, out any) (int, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequest("POST", "http://sessiond"+path, bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.sessiond.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if out != nil && resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return 0, err
		}
	} else {
		_, _ = io.Copy(io.Discard, resp.Body)
	}
	return resp.StatusCode, nil
}

func (g *Gateway) agentClient(socketPath string) *http.Client {
	g.clientsMu.Lock()
	defer g.clientsMu.Unlock()
	if c, ok := g.clients[socketPath]; ok {
		return c
	}
	c := ipc.HTTPClient(socketPath)
	g.clients[socketPath] = c
	return c
}

func (g *Gateway) proxyREST(w http.ResponseWriter, r *http.Request, sess *sessionInfo) {
	req2 := r.Clone(r.Context())
	req2.URL.Scheme = "http"
	req2.URL.Host = "agent"
	req2.RequestURI = ""
	req2.Header = r.Header.Clone()
	req2.Header.Set("X-Lumio-Session", sess.Token)
	resp, err := g.agentClient(sess.AgentSocket).Do(req2)
	if err != nil {
		httpapi.WriteError(w, httpapi.NewError(httpapi.CodeUnavailable, "The session agent is unavailable."))
		return
	}
	defer resp.Body.Close()
	copyHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func copyHeader(dst, src http.Header) {
	hopByHop := map[string]bool{
		"Connection": true, "Keep-Alive": true, "Proxy-Authenticate": true,
		"Proxy-Authorization": true, "Te": true, "Trailer": true,
		"Transfer-Encoding": true, "Upgrade": true,
	}
	for k, values := range src {
		if hopByHop[http.CanonicalHeaderKey(k)] {
			continue
		}
		for _, v := range values {
			dst.Add(k, v)
		}
	}
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
