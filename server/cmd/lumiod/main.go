// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"lumio-os/server/internal/config"
	"lumio-os/server/internal/httpapi"
	"lumio-os/server/internal/journal"
	"lumio-os/server/internal/services"
	"lumio-os/server/internal/static"
	"lumio-os/server/internal/system"
	"lumio-os/server/internal/terminal"
	"lumio-os/server/internal/wsapi"
)

var version = "0.2.0"

func main() {
	cfg, err := config.Parse(os.Args[1:])
	if err != nil {
		os.Exit(2)
	}

	sampler := system.NewSampler()

	svc := services.NewManager()
	if !svc.Available() {
		log.Printf("systemd D-Bus not reachable; services capability disabled")
	}

	jb := journal.NewCLI()
	if !jb.Available() {
		log.Printf("journalctl not found; journal capability disabled")
	}

	var staticHandler http.Handler
	if cfg.WebDir != "" {
		staticHandler = static.DirHandler(cfg.WebDir)
	} else {
		staticHandler = static.Handler()
	}

	hub := wsapi.NewHub(wsapi.Deps{
		Version:  version,
		Services: svc,
		Journal:  jb,
		Sampler:  sampler,
		Terminal: terminal.NewManager(),
	})

	api := httpapi.NewServer(httpapi.Deps{
		Version:  version,
		Sampler:  sampler,
		Services: svc,
		Journal:  jb,
		WS:       hub,
		Static:   staticHandler,
	})

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	log.Printf("lumiod %s listening on %s", version, cfg.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("listen: %v", err)
	}
}
