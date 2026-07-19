// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"

	"lumio-os/server/internal/sessiond"
)

func runSessiond(args []string) {
	fs := flag.NewFlagSet("sessiond", flag.ContinueOnError)
	runDir := fs.String("run-dir", "/run/lumio", "runtime directory")
	insecureDevAuth := fs.String("insecure-dev-auth", "", "DEV ONLY: accept this username with any password (never use in production)")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if *insecureDevAuth != "" {
		log.Printf("SECURITY WARNING: -insecure-dev-auth is active; this bypasses PAM and must never be used in production")
	}
	daemon := sessiond.New(sessiond.Config{
		RunDir:          *runDir,
		SocketPath:      filepath.Join(*runDir, "sessiond.sock"),
		InsecureDevAuth: *insecureDevAuth,
	})
	if err := daemon.Run(); err != nil {
		log.Fatalf("sessiond: %v", err)
	}
}
