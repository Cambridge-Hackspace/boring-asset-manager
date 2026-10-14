package api

import (
	"archive/zip"
	"bytes"
	"image"
	"image/png"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/fields"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

const labelTemplate = `{"id":"tag","name":"Tag","units":"mm","width":40,"height":20,"dpi":300,"elements":[
  {"type":"generator","generator_type":"micro_qr","x":1,"y":1,"width":18,"height":18},
  {"type":"text","x":20,"y":2,"width":19,"height":8,"font_size":10,"content_binding":"{{asset.name}}"},
  {"type":"text","x":20,"y":11,"width":19,"height":7,"font_size":8,"content_binding":"{{field.location}}"}]}`

// labelFixture creates the template above and two assets, one with a Micro
// QR code and one without.
func labelFixture(t *testing.T) (srv *Server, cookie *http.Cookie, tagged, untagged string) {
	t.Helper()
	srv, admin, _ := setupTestServer(t)
	cookie = sessionCookie(t, srv, admin.ID)
	if _, err := srv.store.CreateField("location", "Location", fields.TypeText, nil); err != nil {
		t.Fatal(err)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/templates", labelTemplate); rec.Code != http.StatusCreated {
		t.Fatalf("creating template: %d %s", rec.Code, rec.Body.String())
	}
	a, err := srv.store.CreateAsset("Drill", nil,
		[]models.AssetGeneratorKey{{GeneratorType: "micro_qr", AssignedCode: "BAM-0042"}},
		map[string]string{"location": "Shelf B3"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := srv.store.CreateAsset("Saw", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return srv, cookie, a.Asset.ID, b.Asset.ID
}

func TestAssetLabelFormats(t *testing.T) {
	srv, cookie, tagged, _ := labelFixture(t)
	base := "/api/v1/assets/" + tagged + "/label?template_id=tag"

	rec := doJSON(t, srv, cookie, http.MethodGet, base, "")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/svg+xml" || !strings.Contains(rec.Body.String(), "<svg") {
		t.Errorf("default SVG: got %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}

	rec = doJSON(t, srv, cookie, http.MethodGet, base+"&format=png", "")
	img, err := png.Decode(rec.Body)
	if rec.Code != http.StatusOK || err != nil {
		t.Fatalf("PNG: got %d, %v", rec.Code, err)
	}
	// 40 mm × 20 mm at 300 DPI.
	if b := img.Bounds(); b.Dx() != 472 || b.Dy() != 236 {
		t.Errorf("PNG is %v, want 472×236", b)
	}
	if _, isGray := img.(*image.Gray); !isGray {
		t.Errorf("default PNG is %T, want grayscale", img)
	}

	rec = doJSON(t, srv, cookie, http.MethodGet, base+"&format=png&mono=true", "")
	if img, err := png.Decode(rec.Body); err != nil {
		t.Fatal(err)
	} else if _, isPaletted := img.(*image.Paletted); !isPaletted {
		t.Errorf("mono PNG is %T, want 1-bit paletted", img)
	}

	rec = doJSON(t, srv, cookie, http.MethodGet, base+"&format=pdf", "")
	if rec.Code != http.StatusOK || !bytes.HasPrefix(rec.Body.Bytes(), []byte("%PDF-1.4")) {
		t.Errorf("PDF: got %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Disposition"); got != `inline; filename="`+tagged+`-tag.pdf"` {
		t.Errorf("Content-Disposition = %q", got)
	}
}

func TestAssetLabelErrors(t *testing.T) {
	srv, cookie, tagged, untagged := labelFixture(t)

	archived, err := srv.store.CreateAsset("Old", nil, []models.AssetGeneratorKey{{GeneratorType: "micro_qr", AssignedCode: "OLD-1"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.store.DeleteAsset(archived.Asset.ID); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name, path string
		status     int
		body       string
	}{
		{"no code", "/api/v1/assets/" + untagged + "/label?template_id=tag", http.StatusUnprocessableEntity, "asset has no micro_qr code"},
		{"archived", "/api/v1/assets/" + archived.Asset.ID + "/label?template_id=tag", http.StatusUnprocessableEntity, "asset is archived"},
		{"unknown asset", "/api/v1/assets/00000000-0000-0000-0000-000000000000/label?template_id=tag", http.StatusNotFound, "asset not found"},
		{"no template", "/api/v1/assets/" + tagged + "/label", http.StatusBadRequest, "template_id is required"},
		{"unknown template", "/api/v1/assets/" + tagged + "/label?template_id=nope", http.StatusNotFound, "Template not found"},
		{"bad format", "/api/v1/assets/" + tagged + "/label?template_id=tag&format=gif", http.StatusBadRequest, "format must be"},
	}
	for _, c := range cases {
		rec := doJSON(t, srv, cookie, http.MethodGet, c.path, "")
		if rec.Code != c.status || !strings.Contains(rec.Body.String(), c.body) {
			t.Errorf("%s: got %d %s, want %d containing %q", c.name, rec.Code, rec.Body.String(), c.status, c.body)
		}
	}
}

func TestPreviewTemplate(t *testing.T) {
	srv, cookie, tagged, _ := labelFixture(t)

	// An unsaved template without an id renders with placeholder data.
	unsaved := strings.Replace(labelTemplate, `"id":"tag",`, "", 1)
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/templates/preview", `{"template":`+unsaved+`}`)
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Body.String(), "<?xml") {
		t.Fatalf("placeholder preview: got %d %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/templates/preview", `{"template":`+labelTemplate+`,"asset_id":"`+tagged+`"}`)
	if rec.Code != http.StatusOK {
		t.Errorf("asset preview: got %d %s", rec.Code, rec.Body.String())
	}

	invalid := strings.Replace(labelTemplate, `"dpi":300`, `"dpi":5`, 1)
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/templates/preview", `{"template":`+invalid+`}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"path":"dpi"`) {
		t.Errorf("invalid template: got %d %s", rec.Code, rec.Body.String())
	}

	missingImage := strings.Replace(labelTemplate, `"elements":[`,
		`"elements":[{"type":"static_image","x":0,"y":0,"width":5,"height":5,"image_id":"7c9e6679-7425-40de-944b-e07fc1f90ae7"},`, 1)
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/templates/preview", `{"template":`+missingImage+`}`)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `"path":"elements[0]"`) {
		t.Errorf("missing image: got %d %s", rec.Code, rec.Body.String())
	}

	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/templates/preview", `{"template":[1]}`); rec.Code != http.StatusBadRequest {
		t.Errorf("non-object template: got %d", rec.Code)
	}
}

func TestExportLabels(t *testing.T) {
	srv, cookie, tagged, untagged := labelFixture(t)
	second, err := srv.store.CreateAsset("Lathe", nil, []models.AssetGeneratorKey{{GeneratorType: "micro_qr", AssignedCode: "BAM-0043"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ids := `["` + tagged + `","` + second.Asset.ID + `"]`

	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/labels/export", `{"template_id":"tag","format":"pdf","asset_ids":`+ids+`}`)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/pdf" {
		t.Fatalf("PDF export: got %d %s", rec.Code, rec.Body.String())
	}
	if pages := strings.Count(rec.Body.String(), "/Type /Page "); pages != 2 {
		t.Errorf("PDF has %d pages, want 2", pages)
	}

	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/labels/export", `{"template_id":"tag","format":"zip","mono":true,"asset_ids":`+ids+`}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("ZIP export: got %d %s", rec.Code, rec.Body.String())
	}
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(zr.File) != 2 || zr.File[0].Name != tagged+".png" || zr.File[1].Name != second.Asset.ID+".png" {
		t.Fatalf("unexpected ZIP entries: %v", zr.File)
	}
	f, _ := zr.File[0].Open()
	if img, err := png.Decode(f); err != nil {
		t.Errorf("ZIP entry is not a PNG: %v", err)
	} else if _, isPaletted := img.(*image.Paletted); !isPaletted {
		t.Errorf("mono ZIP entry is %T", img)
	}

	// One bad asset fails the whole batch and names it.
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/labels/export",
		`{"template_id":"tag","format":"pdf","asset_ids":["`+tagged+`","`+untagged+`","missing"]}`)
	body := rec.Body.String()
	if rec.Code != http.StatusUnprocessableEntity || strings.Contains(body, "%PDF") ||
		!strings.Contains(body, `"asset_id":"`+untagged+`"`) || !strings.Contains(body, `"asset_id":"missing"`) ||
		strings.Contains(body, `"asset_id":"`+tagged+`"`) {
		t.Errorf("partial failure: got %d %s", rec.Code, body)
	}

	tooMany := make([]string, MaxExportAssets+1)
	for i := range tooMany {
		tooMany[i] = strconv.Quote("asset-" + strconv.Itoa(i))
	}
	for name, req := range map[string]string{
		"duplicates":  `{"template_id":"tag","format":"pdf","asset_ids":["` + tagged + `","` + tagged + `"]}`,
		"empty":       `{"template_id":"tag","format":"pdf","asset_ids":[]}`,
		"too many":    `{"template_id":"tag","format":"pdf","asset_ids":[` + strings.Join(tooMany, ",") + `]}`,
		"bad format":  `{"template_id":"tag","format":"tiff","asset_ids":["` + tagged + `"]}`,
		"no template": `{"format":"pdf","asset_ids":["` + tagged + `"]}`,
		"not json":    `{`,
	} {
		rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/labels/export", req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d %s, want 400", name, rec.Code, rec.Body.String())
		}
	}
}

func TestExportPermissions(t *testing.T) {
	srv, _, tagged, _ := labelFixture(t)
	user, err := srv.store.CreateUser("nobody@test.org", "NobodySecret123!", true)
	if err != nil {
		t.Fatal(err)
	}
	cookie := sessionCookie(t, srv, user.ID)
	for _, c := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/assets/" + tagged + "/label?template_id=tag", ""},
		{http.MethodPost, "/api/v1/labels/export", `{}`},
		{http.MethodPost, "/api/v1/templates/preview", `{}`},
	} {
		if rec := doJSON(t, srv, cookie, c.method, c.path, c.body); rec.Code != http.StatusForbidden {
			t.Errorf("%s %s: got %d, want 403", c.method, c.path, rec.Code)
		}
	}
}

func TestRequestBodiesAreLimited(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	big := `{"name":"` + strings.Repeat("a", MaxRequestBytes) + `"}`

	// A declared length over the limit is refused before any handler runs.
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/assets", big)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("declared oversized body: got %d, want 413", rec.Code)
	}

	// Without a declared length, reading stops at the limit.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/assets", strings.NewReader(big))
	req.ContentLength = -1
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code < 400 || rec.Code >= 500 {
		t.Errorf("streamed oversized body: got %d, want a 4xx", rec.Code)
	}
	if assets, _ := srv.store.ListAssets("", ""); len(assets) != 0 {
		t.Errorf("oversized request created an asset")
	}

	// Image uploads keep their own larger limit.
	noise := image.NewGray(image.Rect(0, 0, 1200, 1200))
	rand.New(rand.NewSource(1)).Read(noise.Pix)
	var buf bytes.Buffer
	if err := png.Encode(&buf, noise); err != nil {
		t.Fatal(err)
	}
	if buf.Len() <= MaxRequestBytes || buf.Len() > MaxImageBytes {
		t.Fatalf("test image is %d bytes; it must sit between the two limits", buf.Len())
	}
	if rec := upload(t, srv, cookie, "noise.png", buf.Bytes()); rec.Code != http.StatusCreated {
		t.Errorf("%d-byte image upload: got %d %s", buf.Len(), rec.Code, rec.Body.String())
	}
}

// TestBatchesAreBounded lowers the bound on what a batch may hold and
// checks a batch over it is refused with 422 rather than built, while one
// under it is produced.
func TestBatchesAreBounded(t *testing.T) {
	srv, cookie, tagged, _ := labelFixture(t)
	second, err := srv.store.CreateAsset("Lathe", nil, []models.AssetGeneratorKey{{GeneratorType: "micro_qr", AssignedCode: "BAM-0043"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ids := `["` + tagged + `","` + second.Asset.ID + `"]`
	body := `{"template_id":"tag","format":"pdf","asset_ids":` + ids + `}`

	saved := maxBatchParts
	t.Cleanup(func() { maxBatchParts = saved })
	maxBatchParts = 1
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/labels/export", body)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "too detailed") {
		t.Fatalf("over the bound: %d %s", rec.Code, rec.Body.String())
	}
	maxBatchParts = saved
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/labels/export", body); rec.Code != http.StatusOK {
		t.Fatalf("under the bound: %d %s", rec.Code, rec.Body.String())
	}
}
