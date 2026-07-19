// SPDX-License-Identifier: AGPL-3.0-only
//go:build webdist

package static

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:dist
var distFS embed.FS

func Handler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return http.NotFoundHandler()
	}
	return SPAHandler(http.FS(sub))
}

func Available() bool {
	return true
}
