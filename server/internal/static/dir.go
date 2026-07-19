// SPDX-License-Identifier: AGPL-3.0-only
package static

import (
	"net/http"
	"net/url"
	"path"
)

func DirHandler(dir string) http.Handler {
	return SPAHandler(http.Dir(dir))
}

func SPAHandler(fsys http.FileSystem) http.Handler {
	fileServer := http.FileServer(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := path.Clean("/" + r.URL.Path)
		f, err := fsys.Open(clean)
		if err == nil {
			_ = f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		r2 := new(http.Request)
		*r2 = *r
		r2.URL = new(url.URL)
		*r2.URL = *r.URL
		r2.URL.Path = "/"
		fileServer.ServeHTTP(w, r2)
	})
}
