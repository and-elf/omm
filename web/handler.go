package web

import (
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
)

func init() {
	// OpenWrt's minimal userland may lack a system MIME table, and Go does not
	// register these by default. Browsers reject a PWA manifest served as
	// text/plain, so register the types the frontend relies on explicitly.
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
	_ = mime.AddExtensionType(".svg", "image/svg+xml")
	_ = mime.AddExtensionType(".js", "text/javascript; charset=utf-8")
	_ = mime.AddExtensionType(".css", "text/css; charset=utf-8")
}

// NewHandler returns an http.Handler that serves a single-page application
// from fsys.
//
// Real files (index.html, hashed assets, the PWA manifest and service worker)
// are served directly. Any request that does not map to a file falls back to
// index.html, allowing a client-side router to handle deep links. If
// index.html is absent the handler responds with 404.
//
// fsys is injected rather than hard-wired to the embedded assets so the
// behaviour can be tested in isolation.
func NewHandler(fsys fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(fsys))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}

		if f, err := fsys.Open(name); err == nil {
			_ = f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}

		serveIndex(w, fsys)
	})
}

func serveIndex(w http.ResponseWriter, fsys fs.FS) {
	index, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(index)
}
