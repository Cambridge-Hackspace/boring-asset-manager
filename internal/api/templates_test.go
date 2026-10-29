package api

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/fields"
	"github.com/google/uuid"
)

// sessionCookie logs a user in directly through the store.
func sessionCookie(t *testing.T, srv *Server, userID string) *http.Cookie {
	t.Helper()
	sess, err := srv.store.CreateSession(userID, time.Hour)
	if err != nil {
		t.Fatalf("creating session: %v", err)
	}
	return &http.Cookie{Name: SessionCookieName, Value: sess.ID}
}

func do(t *testing.T, srv *Server, cookie *http.Cookie, method, path, contentType string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func doJSON(t *testing.T, srv *Server, cookie *http.Cookie, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	return do(t, srv, cookie, method, path, "application/json", strings.NewReader(body))
}

const shelfTemplate = `{
  "id": "shelf", "name": "Shelf", "units": "mm", "width": 62, "height": 29, "dpi": 300, "layering": "list",
  "elements": [
    {"type": "generator", "generator_type": "micro_qr", "x": 2, "y": 2, "width": 25, "height": 25},
    {"type": "text", "x": 29, "y": 2, "width": 31, "height": 8, "font_size": 12,
     "content_binding": "{{asset.name}} ({{field.location}})"}
  ]
}`

func TestTemplateAPIWorkflow(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)

	// The binding names a field that does not exist yet.
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/templates", shelfTemplate)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"path":"elements[1].content_binding"`) {
		t.Fatalf("missing field: got %d %s", rec.Code, rec.Body.String())
	}

	if _, err := srv.store.CreateField("location", "Location", fields.TypeText, nil); err != nil {
		t.Fatal(err)
	}
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/templates", shelfTemplate)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: got %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Template struct {
			ID       string `json:"id"`
			Elements []struct {
				FontWeight string `json:"font_weight"`
			} `json:"elements"`
		} `json:"template"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	// The server names it; the id in the document is ignored.
	shelf := created.Template.ID
	if _, err := uuid.Parse(shelf); err != nil || created.Template.Elements[1].FontWeight != "normal" {
		t.Errorf("unexpected created template: %s", rec.Body.String())
	}
	if rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/templates/shelf", ""); rec.Code != http.StatusNotFound {
		t.Errorf("stored under the id in the document: got %d", rec.Code)
	}
	// Creating the same document again makes a second template, and an id
	// that would not be a valid one is ignored too.
	again := createTemplate(t, srv, cookie, strings.Replace(shelfTemplate, `"id": "shelf"`, `"id": "Not An ID!"`, 1))
	if again == shelf {
		t.Errorf("two templates share the id %s", shelf)
	}
	createTemplate(t, srv, cookie, strings.Replace(shelfTemplate, `"id": "shelf",`, "", 1))

	if rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/templates/"+shelf, ""); rec.Code != http.StatusOK {
		t.Errorf("get: got %d", rec.Code)
	}
	rec = doJSON(t, srv, cookie, http.MethodGet, "/api/v1/templates", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"id":"`+shelf+`"`) {
		t.Errorf("list: got %d %s", rec.Code, rec.Body.String())
	}

	// An update may leave the id out, but cannot change it.
	unnamed := strings.Replace(shelfTemplate, `"id": "shelf",`, "", 1)
	renamed := strings.Replace(unnamed, `"name": "Shelf"`, `"name": "Shelf v2"`, 1)
	if rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/templates/"+shelf, renamed); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"Shelf v2"`) {
		t.Errorf("update: got %d %s", rec.Code, rec.Body.String())
	}
	withID := strings.Replace(shelfTemplate, `"id": "shelf"`, `"id": "`+shelf+`"`, 1)
	if rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/templates/"+shelf, withID); rec.Code != http.StatusOK {
		t.Errorf("update giving the id: got %d %s", rec.Code, rec.Body.String())
	}
	if rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/templates/"+again, withID); rec.Code != http.StatusBadRequest {
		t.Errorf("update with mismatched id: got %d, want 400", rec.Code)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/templates/nope", unnamed); rec.Code != http.StatusNotFound {
		t.Errorf("update missing: got %d, want 404", rec.Code)
	}

	if rec := doJSON(t, srv, cookie, http.MethodDelete, "/api/v1/templates/"+shelf, ""); rec.Code != http.StatusOK {
		t.Errorf("delete: got %d", rec.Code)
	}
	if rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/templates/"+shelf, ""); rec.Code != http.StatusNotFound {
		t.Errorf("get after delete: got %d, want 404", rec.Code)
	}
}

// createTemplate creates a template through the API and returns the ID the
// server gave it.
func createTemplate(t *testing.T, srv *Server, cookie *http.Cookie, doc string) string {
	t.Helper()
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/templates", doc)
	if rec.Code != http.StatusCreated {
		t.Fatalf("creating template: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Template struct {
			ID string `json:"id"`
		} `json:"template"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.Template.ID == "" {
		t.Fatalf("creating template: %v %s", err, rec.Body.String())
	}
	return created.Template.ID
}

func TestTemplateValidationDetails(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)

	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/templates",
		`{"id":"x","name":"X","units":"cm","width":10,"height":10,"dpi":300,"elements":[]}`)
	var resp struct {
		Error   string `json:"error"`
		Details []struct {
			Path    string `json:"path"`
			Message string `json:"message"`
		} `json:"details"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadRequest || len(resp.Details) != 1 || resp.Details[0].Path != "units" {
		t.Errorf("got %d %s", rec.Code, rec.Body.String())
	}

	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/templates", `{"id":`); rec.Code != http.StatusBadRequest {
		t.Errorf("malformed JSON: got %d, want 400", rec.Code)
	}
	huge := `{"id":"x","name":"` + strings.Repeat("a", maxTemplateBytes) + `"}`
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/templates", huge); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized body: got %d, want 413", rec.Code)
	}
}

func TestTemplateAndImagePermissions(t *testing.T) {
	srv, _, _ := setupTestServer(t)
	nothingPublic(t, srv)
	user, err := srv.store.CreateUser("viewer@test.org", "ViewerSecret123!", true)
	if err != nil {
		t.Fatal(err)
	}
	cookie := sessionCookie(t, srv, user.ID)

	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/templates"},
		{http.MethodPost, "/api/v1/templates"},
		{http.MethodDelete, "/api/v1/templates/shelf"},
		{http.MethodGet, "/api/v1/images"},
		{http.MethodPost, "/api/v1/images"},
	} {
		if rec := doJSON(t, srv, cookie, c.method, c.path, "{}"); rec.Code != http.StatusForbidden {
			t.Errorf("%s %s without permissions: got %d, want 403", c.method, c.path, rec.Code)
		}
		if rec := doJSON(t, srv, nil, c.method, c.path, "{}"); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s anonymously: got %d, want 401", c.method, c.path, rec.Code)
		}
	}
}

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.White)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func upload(t *testing.T, srv *Server, cookie *http.Cookie, filename string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(data)
	_ = mw.Close()
	return do(t, srv, cookie, http.MethodPost, "/api/v1/images", mw.FormDataContentType(), &body)
}

func TestImageAPIWorkflow(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	data := pngBytes(t, 40, 20)

	rec := upload(t, srv, cookie, `C:\Users\me\logo.png`, data)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload: got %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Image struct {
			ID          string `json:"id"`
			Filename    string `json:"filename"`
			ContentType string `json:"content_type"`
			WidthPx     int    `json:"width_px"`
			HeightPx    int    `json:"height_px"`
		} `json:"image"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	img := resp.Image
	if img.Filename != "logo.png" || img.ContentType != "image/png" || img.WidthPx != 40 || img.HeightPx != 20 {
		t.Errorf("unexpected metadata: %+v", img)
	}

	rec = doJSON(t, srv, cookie, http.MethodGet, "/api/v1/images/"+img.ID+"/content", "")
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), data) || rec.Header().Get("Content-Type") != "image/png" {
		t.Errorf("content: got %d, %d bytes, type %q", rec.Code, rec.Body.Len(), rec.Header().Get("Content-Type"))
	}

	logo := createTemplate(t, srv, cookie, `{"name":"Logo","units":"mm","width":20,"height":10,"dpi":300,
	  "elements":[{"type":"static_image","x":0,"y":0,"width":20,"height":10,"image_id":"`+img.ID+`"}]}`)
	rec = doJSON(t, srv, cookie, http.MethodDelete, "/api/v1/images/"+img.ID, "")
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"template_ids":["`+logo+`"]`) {
		t.Errorf("deleting a used image: got %d %s", rec.Code, rec.Body.String())
	}

	doJSON(t, srv, cookie, http.MethodDelete, "/api/v1/templates/"+logo, "")
	if rec := doJSON(t, srv, cookie, http.MethodDelete, "/api/v1/images/"+img.ID, ""); rec.Code != http.StatusOK {
		t.Errorf("delete: got %d %s", rec.Code, rec.Body.String())
	}
	if _, err := srv.images.Read(context.Background(), img.ID); err == nil {
		t.Error("image file survived deletion")
	}
	if rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/images/"+img.ID, ""); rec.Code != http.StatusNotFound {
		t.Errorf("get after delete: got %d, want 404", rec.Code)
	}
}

func TestImageUploadRejections(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)

	gif := []byte("GIF89a\x01\x00\x01\x00\x80\x00\x00\x00\x00\x00\xff\xff\xff,\x00\x00\x00\x00\x01\x00\x01\x00\x00\x02\x02D\x01\x00;")
	truncated := pngBytes(t, 50, 50)
	truncated = truncated[:len(truncated)-20]

	cases := []struct {
		name string
		data []byte
		want int
	}{
		{"gif", gif, http.StatusBadRequest},
		{"text", []byte("not an image"), http.StatusBadRequest},
		{"truncated png", truncated, http.StatusBadRequest},
		{"too many pixels", pngBytes(t, MaxImagePixels+1, 1), http.StatusBadRequest},
		{"too many bytes", append(pngBytes(t, 1, 1), make([]byte, MaxImageBytes)...), http.StatusRequestEntityTooLarge},
	}
	for _, c := range cases {
		if rec := upload(t, srv, cookie, c.name, c.data); rec.Code != c.want {
			t.Errorf("%s: got %d %s, want %d", c.name, rec.Code, rec.Body.String(), c.want)
		}
	}

	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/images", "{}"); rec.Code != http.StatusBadRequest {
		t.Errorf("non-multipart body: got %d, want 400", rec.Code)
	}
	if images, _ := srv.store.ListImages(); len(images) != 0 {
		t.Errorf("rejected uploads were recorded: %v", images)
	}
}
