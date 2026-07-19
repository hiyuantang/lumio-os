// SPDX-License-Identifier: AGPL-3.0-only
package config

import "flag"

type Config struct {
	Addr   string
	WebDir string
}

func Parse(args []string) (Config, error) {
	fs := flag.NewFlagSet("lumiod", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:8080", "listen address")
	web := fs.String("web", "", "serve the frontend from this directory instead of the embedded assets")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	return Config{Addr: *addr, WebDir: *web}, nil
}
