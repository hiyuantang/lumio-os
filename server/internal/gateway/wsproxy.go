// SPDX-License-Identifier: AGPL-3.0-only
package gateway

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"lumio-os/server/internal/httpapi"
)

func (g *Gateway) handleWS(w http.ResponseWriter, r *http.Request) {
	sess, apiErr := g.requireSession(r)
	if apiErr != nil {
		httpapi.WriteError(w, apiErr)
		return
	}
	csrf := r.URL.Query().Get("csrf")
	if csrf == "" {
		csrf = r.Header.Get("X-Lumio-CSRF")
	}
	if csrf == "" || csrf != sess.CSRF {
		httpapi.WriteError(w, httpapi.NewError(httpapi.CodeForbidden, "CSRF check failed."))
		return
	}
	if !originAllowedWS(r) {
		httpapi.WriteError(w, httpapi.NewError(httpapi.CodeForbidden, "Origin not allowed."))
		return
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		httpapi.WriteError(w, httpapi.NewError(httpapi.CodeInternal, "WebSocket hijack unsupported."))
		return
	}
	agentConn, err := net.Dial("unix", sess.AgentSocket)
	if err != nil {
		httpapi.WriteError(w, httpapi.NewError(httpapi.CodeUnavailable, "The session agent is unavailable."))
		return
	}

	upstream := new(http.Request)
	*upstream = *r
	upstream.RequestURI = ""
	upstream.URL = &url.URL{Scheme: "http", Host: "agent", Path: r.URL.Path, RawQuery: r.URL.RawQuery}
	upstream.Header = r.Header.Clone()
	upstream.Header.Set("X-Lumio-Session", sess.Token)
	if err := upstream.Write(agentConn); err != nil {
		_ = agentConn.Close()
		httpapi.WriteError(w, httpapi.NewError(httpapi.CodeUnavailable, "The session agent is unavailable."))
		return
	}
	agentReader := bufio.NewReader(agentConn)
	resp, err := http.ReadResponse(agentReader, upstream)
	if err != nil {
		_ = agentConn.Close()
		httpapi.WriteError(w, httpapi.NewError(httpapi.CodeUnavailable, "The session agent is unavailable."))
		return
	}

	clientConn, clientBuf, err := hijacker.Hijack()
	if err != nil {
		_ = agentConn.Close()
		return
	}
	if err := resp.Write(clientBuf); err != nil {
		_ = agentConn.Close()
		_ = clientConn.Close()
		return
	}
	if err := clientBuf.Flush(); err != nil {
		_ = agentConn.Close()
		_ = clientConn.Close()
		return
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		_ = agentConn.Close()
		_ = clientConn.Close()
		return
	}
	go func() {
		_, _ = io.Copy(agentConn, clientConn)
		_ = agentConn.Close()
		_ = clientConn.Close()
	}()
	_, _ = io.Copy(clientConn, agentReader)
	_ = agentConn.Close()
	_ = clientConn.Close()
}

func originAllowedWS(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") || host == "127.0.0.1" || host == "::1" || host == "[::1]" {
		return true
	}
	return strings.EqualFold(u.Host, r.Host)
}
