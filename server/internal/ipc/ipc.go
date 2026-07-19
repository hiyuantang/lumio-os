// SPDX-License-Identifier: AGPL-3.0-only
package ipc

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

func HTTPClient(socketPath string) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socketPath)
			},
		},
		Timeout: 30 * time.Second,
	}
}

func ServeUnix(listener net.Listener, handler http.Handler) error {
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return srv.Serve(listener)
}

func ListenUnix(path string, mode uint32) (net.Listener, error) {
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if mode != 0 {
		if err := chmodSocket(path, mode); err != nil {
			_ = ln.Close()
			return nil, fmt.Errorf("chmod %s: %w", path, err)
		}
	}
	return ln, nil
}
