// Package webui serves the embedded single-page application
// (docs/architecture.md). The app is built by tools/web/build.sh into
// static/app; a binary built without it serves a placeholder page instead.
package webui

import (
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
)

//go:embed all:static
var embedded embed.FS

// contentSecurityPolicy allows only the app's own scripts, styles and
// requests. Images may also be data: and blob: URLs (label previews), and
// inline style attributes position the template editor's elements.
// 'wasm-unsafe-eval' lets the app compile its own WebAssembly, the editor's
// live renderer, and nothing else: it allows no eval of script.
const contentSecurityPolicy = "default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; style-src 'self' 'unsafe-inline'; " +
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
				if serveCompressed(w, r, app, name) {
					return
				}
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

// serveCompressed serves the Brotli or gzip copy of an asset that the build
// wrote beside it (web/vite.config.ts), when the browser takes that
// encoding, and reports whether it did.
func serveCompressed(w http.ResponseWriter, r *http.Request, fsys fs.FS, name string) bool {
	w.Header().Add("Vary", "Accept-Encoding")
	accepted := r.Header.Get("Accept-Encoding")
	for _, enc := range []struct{ token, ext string }{{"br", ".br"}, {"gzip", ".gz"}} {
		if !acceptsEncoding(accepted, enc.token) || !exists(fsys, name+enc.ext) {
			continue
		}
		if t := mime.TypeByExtension(path.Ext(name)); t != "" {
			w.Header().Set("Content-Type", t)
		}
		w.Header().Set("Content-Encoding", enc.token)
		http.ServeFileFS(w, r, fsys, name+enc.ext)
		return true
	}
	return false
}

// acceptsEncoding reports whether an Accept-Encoding header takes token,
// with a quality above zero.
func acceptsEncoding(header, token string) bool {
	for _, part := range strings.Split(header, ",") {
		name, params, _ := strings.Cut(part, ";")
		if !strings.EqualFold(strings.TrimSpace(name), token) {
			continue
		}
		for _, param := range strings.Split(params, ";") {
			key, value, _ := strings.Cut(param, "=")
			if strings.EqualFold(strings.TrimSpace(key), "q") {
				q, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
				return err == nil && q > 0
			}
		}
		return true
	}
	return false
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
