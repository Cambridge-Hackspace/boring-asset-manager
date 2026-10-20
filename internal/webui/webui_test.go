package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func get(t *testing.T, h http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func builtApp() fstest.MapFS {
	return fstest.MapFS{
		"placeholder.html":       {Data: []byte("placeholder")},
		"app/index.html":         {Data: []byte("<!doctype html><title>app</title>")},
		"app/favicon.svg":        {Data: []byte("<svg/>")},
		"app/assets/app-1a2b.js": {Data: []byte("console.log(1)")},
	}
}

func TestServesTheBuiltApp(t *testing.T) {
	h := newHandler(builtApp())
	const page = "<title>app</title>"
	for _, c := range []struct {
		target, body, cache string
		status              int
	}{
		{"/", page, "no-cache", 200},
		{"/index.html", page, "no-cache", 200},
		{"/assets/123", page, "no-cache", 200}, // a client route, not a file
		{"/templates/qr-small/edit", page, "no-cache", 200},
		{"/assets/app-1a2b.js", "console.log(1)", "public, max-age=31536000, immutable", 200},
		{"/favicon.svg", "<svg/>", "no-cache", 200},
		{"/assets/missing.js", "", "", 404},
		{"/../../etc/passwd", page, "no-cache", 200},
	} {
		rec := get(t, h, http.MethodGet, c.target)
		if rec.Code != c.status || !strings.Contains(rec.Body.String(), c.body) || rec.Header().Get("Cache-Control") != c.cache {
			t.Errorf("GET %s: %d %q cache %q", c.target, rec.Code, rec.Body.String(), rec.Header().Get("Cache-Control"))
		}
	}
	if ct := get(t, h, http.MethodGet, "/").Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("index served as %q", ct)
	}
	if rec := get(t, h, http.MethodHead, "/"); rec.Code != 200 || rec.Body.Len() != 0 {
		t.Errorf("HEAD /: %d with %d bytes", rec.Code, rec.Body.Len())
	}
}

func TestUnknownAPIRoutesAreNotTheApp(t *testing.T) {
	rec := get(t, newHandler(builtApp()), http.MethodGet, "/api/v1/nothing")
	if rec.Code != 404 || rec.Header().Get("Content-Type") != "application/json" || strings.Contains(rec.Body.String(), "app") {
		t.Errorf("GET /api/v1/nothing: %d %s %q", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
}

func TestOnlyReads(t *testing.T) {
	if rec := get(t, newHandler(builtApp()), http.MethodPost, "/"); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /: %d", rec.Code)
	}
}

func TestPlaceholderWhenNotBuilt(t *testing.T) {
	h := newHandler(fstest.MapFS{"placeholder.html": {Data: []byte("not built")}})
	for _, target := range []string{"/", "/assets/x.js", "/categories"} {
		if rec := get(t, h, http.MethodGet, target); rec.Code != 200 || rec.Body.String() != "not built" {
			t.Errorf("GET %s: %d %q", target, rec.Code, rec.Body.String())
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	for _, h := range []http.Handler{newHandler(builtApp()), newHandler(fstest.MapFS{"placeholder.html": {}})} {
		hdr := get(t, h, http.MethodGet, "/").Header()
		csp := hdr.Get("Content-Security-Policy")
		// WebAssembly may be compiled, for the editor's live renderer, and
		// script still never evaluated.
		if !strings.Contains(csp, "script-src 'self' 'wasm-unsafe-eval';") || strings.Contains(csp, "'unsafe-eval'") {
			t.Errorf("script-src in %q", csp)
		}
		if !strings.Contains(csp, "default-src 'self'") || !strings.Contains(csp, "frame-ancestors 'none'") ||
			hdr.Get("X-Content-Type-Options") != "nosniff" || hdr.Get("Referrer-Policy") != "same-origin" {
			t.Errorf("headers %v", hdr)
		}
	}
}

func TestEmbeddedFilesServe(t *testing.T) {
	// The placeholder is always committed, so the real embed serves
	// something whether or not the app was built.
	if _, err := embedded.ReadFile("static/placeholder.html"); err != nil {
		t.Fatal(err)
	}
	if rec := get(t, Handler(), http.MethodGet, "/"); rec.Code != 200 {
		t.Errorf("GET / from the embedded files: %d", rec.Code)
	}
}

// TestServesPrecompressedAssets sends the Brotli or gzip copy the build
// wrote beside an asset to a browser that takes it, typed as the asset, and
// the asset itself to one that does not.
func TestServesPrecompressedAssets(t *testing.T) {
	fsys := builtApp()
	fsys["app/assets/bamlive-9f8e.wasm"] = &fstest.MapFile{Data: []byte("raw wasm")}
	fsys["app/assets/bamlive-9f8e.wasm.br"] = &fstest.MapFile{Data: []byte("brotli wasm")}
	fsys["app/assets/bamlive-9f8e.wasm.gz"] = &fstest.MapFile{Data: []byte("gzip wasm")}
	fsys["app/assets/app-1a2b.js.gz"] = &fstest.MapFile{Data: []byte("gzip js")}
	h := newHandler(fsys)
	for _, c := range []struct{ target, accept, body, encoding, contentType string }{
		{"/assets/bamlive-9f8e.wasm", "gzip, deflate, br, zstd", "brotli wasm", "br", "application/wasm"},
		{"/assets/bamlive-9f8e.wasm", "gzip", "gzip wasm", "gzip", "application/wasm"},
		{"/assets/bamlive-9f8e.wasm", "br;q=0, gzip;q=0.5", "gzip wasm", "gzip", "application/wasm"},
		{"/assets/bamlive-9f8e.wasm", "BR", "brotli wasm", "br", "application/wasm"},
		{"/assets/bamlive-9f8e.wasm", "", "raw wasm", "", "application/wasm"},
		{"/assets/bamlive-9f8e.wasm", "identity, br;q=0.000", "raw wasm", "", "application/wasm"},
		{"/assets/app-1a2b.js", "br, gzip", "gzip js", "gzip", "text/javascript; charset=utf-8"},
	} {
		req := httptest.NewRequest(http.MethodGet, c.target, nil)
		if c.accept != "" {
			req.Header.Set("Accept-Encoding", c.accept)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		hdr := rec.Header()
		if rec.Code != 200 || rec.Body.String() != c.body || hdr.Get("Content-Encoding") != c.encoding ||
			hdr.Get("Content-Type") != c.contentType || hdr.Get("Vary") != "Accept-Encoding" ||
			hdr.Get("Cache-Control") != "public, max-age=31536000, immutable" {
			t.Errorf("%s with %q: %d %q, encoding %q, type %q, vary %q", c.target, c.accept, rec.Code, rec.Body.String(),
				hdr.Get("Content-Encoding"), hdr.Get("Content-Type"), hdr.Get("Vary"))
		}
	}
	// The compressed copies are not pages of their own.
	if rec := get(t, h, http.MethodGet, "/index.html.br"); rec.Code != 404 {
		t.Errorf("GET /index.html.br: %d", rec.Code)
	}
}
