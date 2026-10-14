// Package webui serves the embedded single-page application (README
// "Frontend Architecture"). The app is built by tools/web/build.sh into
// static/app; a binary built without it serves a placeholder page instead.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:static
var embedded embed.FS

// contentSecurityPolicy allows only the app's own scripts, styles and
// requests. Images may also be data: and blob: URLs (label previews), and
// inline style attributes position the template editor's elements.
const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob:; font-src 'self'; connect-src 'self'; object-src 'none'; " +
	"base-uri 'self'; form-action 'self'; frame-ancestors 'none'"

// Handler serves the embedded application.
func Handler() http.Handler {
	static, err := fs.Sub(embedded, "static")
	if err != nil {
		panic(err)
	}
	return newHandler(static)
}

// newHandler serves the app in fsys/app, or fsys/placeholder.html when the
// app has not been built.
func newHandler(fsys fs.FS) http.Handler {
	app, err := fs.Sub(fsys, "app")
	built := err == nil && exists(app, "index.html")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "DENY")

		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			h.Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			// Unknown API routes are errors, never the app.
			h.Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"Not found"}` + "\n"))
			return
		}
		if !built {
			h.Set("Cache-Control", "no-cache")
			serveFile(w, r, fsys, "placeholder.html")
			return
		}

		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		switch {
		case name != "" && name != "index.html" && exists(app, name):
			if strings.HasPrefix(name, "assets/") {
				// Vite puts a content hash in every asset name.
				h.Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				h.Set("Cache-Control", "no-cache")
			}
			http.ServeFileFS(w, r, app, name)
		case path.Ext(name) != "" && name != "index.html":
			// A missing file, not a client-side route.
			http.NotFound(w, r)
		default:
			h.Set("Cache-Control", "no-cache")
			serveFile(w, r, app, "index.html")
		}
	})
}

// serveFile writes an HTML file directly; http.ServeFileFS would redirect
// requests for index.html to the directory.
func serveFile(w http.ResponseWriter, r *http.Request, fsys fs.FS, name string) {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		http.Error(w, name+" missing", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(data)
}

// exists reports whether name is a regular file in fsys.
func exists(fsys fs.FS, name string) bool {
	info, err := fs.Stat(fsys, name)
	return err == nil && !info.IsDir()
}
