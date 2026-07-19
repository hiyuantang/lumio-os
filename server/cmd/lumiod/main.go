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

var version = "0.4.0"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "gateway":
			runGateway(os.Args[2:])
			return
		case "sessiond":
			runSessiond(os.Args[2:])
			return
		case "agent":
			runAgent(os.Args[2:])
			return
		case "broker":
			runBroker(os.Args[2:])
			return
		}
	}
	runLegacy(os.Args[1:])
}

func runLegacy(args []string) {
	cfg, err := config.Parse(args)
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

	log.Printf("lumiod %s listening on %s (single-process mode)", version, cfg.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("listen: %v", err)
	}
}
