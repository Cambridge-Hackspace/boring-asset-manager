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
