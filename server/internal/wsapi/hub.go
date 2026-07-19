// SPDX-License-Identifier: AGPL-3.0-only
package wsapi

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/websocket"

	"lumio-os/server/internal/journal"
	"lumio-os/server/internal/services"
	"lumio-os/server/internal/system"
	"lumio-os/server/internal/terminal"
)

const (
	maxFrameBytes  = 64 << 10
	pingInterval   = 30
	maxMissedPongs = 2
	sendBuffer     = 64
)

type Deps struct {
	Version  string
	Services services.API
	Journal  journal.Backend
	Sampler  *system.Sampler
	Terminal *terminal.Manager
}

type Hub struct {
	deps     Deps
	upgrader websocket.Upgrader
}

func NewHub(deps Deps) *Hub {
	return &Hub{
		deps: deps,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			CheckOrigin:     originAllowed,
		},
	}
}

func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ws, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := newConn(ws, h)
	c.enqueue(map[string]any{
		"type":          "hello",
		"protocol":      1,
		"serverVersion": h.deps.Version,
	})
	go c.writeLoop()
	go c.pingLoop()
	c.readLoop()
}

func originAllowed(r *http.Request) bool {
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
