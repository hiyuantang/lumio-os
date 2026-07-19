// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"lumio-os/server/internal/gateway"
	"lumio-os/server/internal/static"
)

func runGateway(args []string) {
	fs := flag.NewFlagSet("gateway", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:8080", "listen address")
	web := fs.String("web", "", "serve the frontend from this directory instead of the embedded assets")
	runDir := fs.String("run-dir", "/run/lumio", "runtime directory")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}

	var staticHandler http.Handler
	if *web != "" {
		staticHandler = static.DirHandler(*web)
	} else {
		staticHandler = static.Handler()
	}

	gw := gateway.New(gateway.Config{
		Addr:           *addr,
		SessiondSocket: filepath.Join(*runDir, "sessiond.sock"),
		Static:         staticHandler,
		Version:        version,
	})

	srv := &http.Server{
		Addr:              *addr,
		Handler:           gw.Handler(),
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

	log.Printf("lumiod-gateway %s listening on %s", version, *addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("listen: %v", err)
	}
}
