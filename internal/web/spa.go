// SPDX-License-Identifier: GPL-3.0-only

package web

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// SPAHandler serves embedded static files and returns index.html for client-side
// routes. Thanks to this, direct opening and refreshing of /labs/<folder>/<lab>
// does not lead to 404. API and WebSocket requests remain with their own handlers.
func SPAHandler(content fs.FS) http.Handler {
	files := http.FileServer(http.FS(content))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api" ||
			strings.HasPrefix(r.URL.Path, "/ws/") || r.URL.Path == "/ws" {
			http.NotFound(w, r)
			return
		}

		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name != "" && name != "." {
			if info, err := fs.Stat(content, name); err == nil && !info.IsDir() {
				files.ServeHTTP(w, r)
				return
			}
			// A missing file with an extension has no client route:
			// return 404 rather than HTML in place of a script or image.
			if strings.Contains(path.Base(name), ".") {
				http.NotFound(w, r)
				return
			}
		}

		index, err := fs.ReadFile(content, "index.html")
		if err != nil {
			http.Error(w, "frontend is not built", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = w.Write(index)
		}
	})
}
