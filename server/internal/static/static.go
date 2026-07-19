// SPDX-License-Identifier: AGPL-3.0-only
//go:build !webdist

package static

import "net/http"

func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("frontend not embedded; rebuild with -tags webdist (scripts/build-with-web.sh) or start lumiod with -web <dir>\n"))
	})
}

func Available() bool {
	return false
}
