// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"encoding/json"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"lumio-os/server/internal/httpapi"
	"lumio-os/server/internal/journal"
	"lumio-os/server/internal/services"
	"lumio-os/server/internal/system"
	"lumio-os/server/internal/terminal"
	"lumio-os/server/internal/wsapi"
)

const agentIdleTimeout = 10 * time.Minute

func runAgent(args []string) {
	fs := flag.NewFlagSet("agent", flag.ContinueOnError)
	fd := fs.Int("fd", -1, "inherited listener file descriptor")
	socket := fs.String("socket", "", "unix socket path (when not using -fd)")
	brokerSock := fs.String("broker-sock", "/run/lumio/broker.sock", "broker socket path")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}

	var listener net.Listener
	if *fd >= 0 {
		f := os.NewFile(uintptr(*fd), "agent.sock")
		ln, err := net.FileListener(f)
		if err != nil {
			log.Fatalf("agent: inherited listener: %v", err)
		}
		listener = ln
	} else if *socket != "" {
		_ = os.Remove(*socket)
		ln, err := net.Listen("unix", *socket)
		if err != nil {
			log.Fatalf("agent: listen %s: %v", *socket, err)
		}
		if err := os.Chmod(*socket, 0o660); err != nil {
			log.Fatalf("agent: chmod: %v", err)
		}
		listener = ln
	} else {
		log.Fatalf("agent: either -fd or -socket is required")
	}

	sampler := system.NewSampler()
	svc := services.NewManager()
	if !svc.Available() {
		log.Printf("agent: systemd D-Bus not reachable; services capability disabled")
	}
	jb := journal.NewCLI()
	if !jb.Available() {
		log.Printf("agent: journalctl not found; journal capability disabled")
	}
	terminals := terminal.NewManager()

	hub := wsapi.NewHub(wsapi.Deps{
		Version:      version,
		Services:     svc,
		Journal:      jb,
		Sampler:      sampler,
		Terminal:     terminals,
		BrokerSocket: *brokerSock,
	})

	api := httpapi.NewServer(httpapi.Deps{
		Version:      version,
		Sampler:      sampler,
		Services:     svc,
		Journal:      jb,
		WS:           hub,
		BrokerSocket: *brokerSock,
	})

	var lastActivity atomic.Int64
	lastActivity.Store(time.Now().Unix())

	mux := http.NewServeMux()
	mux.HandleFunc("GET /agent/state", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"connections":  hub.Connections(),
			"ptySessions":  terminals.Count(),
			"lastActivity": lastActivity.Load(),
		})
	})
	mux.Handle("/", activityTracker(api.Handler(), &lastActivity))

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if hub.Connections() > 0 || terminals.Count() > 0 {
				continue
			}
			if time.Since(time.Unix(lastActivity.Load(), 0)) > agentIdleTimeout {
				log.Printf("agent: idle for %s, exiting", agentIdleTimeout)
				os.Exit(0)
			}
		}
	}()

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("agent: serving uid %d on %s", os.Getuid(), listener.Addr())
	if err := srv.Serve(listener); err != nil {
		log.Fatalf("agent: serve: %v", err)
	}
}

func activityTracker(next http.Handler, lastActivity *atomic.Int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastActivity.Store(time.Now().Unix())
		next.ServeHTTP(w, r)
	})
}
