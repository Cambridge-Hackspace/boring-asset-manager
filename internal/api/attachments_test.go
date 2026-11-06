package api

import (
	"bytes"
	"encoding/json"
	"image"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// attached is an attachments route's answer.
type attached struct {
	Attachments []models.Attachment `json:"attachments"`
	Attachment  models.Attachment   `json:"attachment"`
	Limits      AttachmentLimits    `json:"limits"`
}

// attach uploads a file to an attachments route, with the form's other
// fields, answering the recorder.
func attach(t *testing.T, srv *Server, who *http.Cookie, path, filename string, data []byte, fields map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	body, ct := multipartFile(t, filename, data, fields)
	return do(t, srv, who, http.MethodPost, path, ct, bytes.NewReader(body))
}

// multipartFile is a form with the file in its "file" part, by its name.
func multipartFile(t *testing.T, filename string, data []byte, fields map[string]string) ([]byte, string) {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(data)
	for k, v := range fields {
		_ = w.WriteField(k, v)
	}
	_ = w.Close()
	return b.Bytes(), w.FormDataContentType()
}

func decodeAttached(t *testing.T, rec *httptest.ResponseRecorder) attached {
	t.Helper()
	var out attached
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("%d %s: %v", rec.Code, rec.Body, err)
	}
	return out
}

var testPDF = []byte("%PDF-1.4\n1 0 obj << /Type /Catalog >> endobj\ntrailer << /Root 1 0 R >>\n%%EOF\n")

func TestAssetAttachmentsAPI(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	reader := userWithPermissions(t, srv, "reader@hackspace.org", rbac.PermAssetsRead)
	a, err := srv.store.CreateAsset("Drill", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/assets/" + a.Asset.ID + "/attachments"

	// A PDF, named and described; an image; Markdown.
	rec := attach(t, srv, cookie, path, "manual.pdf", testPDF, map[string]string{"name": "Manual", "description": "From the maker"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("PDF: %d %s", rec.Code, rec.Body)
	}
	got := decodeAttached(t, rec)
	pdf := got.Attachment
	if pdf.Kind != models.AttachmentDocument || pdf.ContentType != "application/pdf" || pdf.Name != "Manual" || pdf.Description != "From the maker" ||
		pdf.Filename != "manual.pdf" || pdf.AddedBy != admin.ID || pdf.AddedByName == "" || got.Limits.MaxPerObject != store.DefaultMaxAttachments {
		t.Errorf("PDF: %+v, limits %+v", pdf, got.Limits)
	}
	rec = attach(t, srv, cookie, path, "front.jpg", testJPEG(t, 3000, 1000), nil)
	front := decodeAttached(t, rec).Attachment
	if rec.Code != http.StatusCreated || front.Kind != models.AttachmentImage || front.WidthPx != 2048 || front.Name != "front.jpg" {
		t.Errorf("image: %d %+v", rec.Code, front)
	}
	rec = attach(t, srv, cookie, path, "notes.md", []byte("# Notes\n\nOil it monthly.\n"), nil)
	notes := decodeAttached(t, rec).Attachment
	if rec.Code != http.StatusCreated || notes.Kind != models.AttachmentDocument || notes.ContentType != "text/markdown; charset=utf-8" {
		t.Errorf("Markdown: %d %+v", rec.Code, notes)
	}
	back := decodeAttached(t, attach(t, srv, cookie, path, "back.jpg", testJPEG(t, 40, 30), nil)).Attachment

	// Its photos are its images, in order; readers see all four, but not
	// who added them.
	asset, _ := srv.store.GetAssetByID(a.Asset.ID)
	if len(asset.Photos) != 2 || asset.Photos[0].ID != front.ID || asset.Photos[1].ID != back.ID {
		t.Errorf("photos: %+v", asset.Photos)
	}
	list := decodeAttached(t, doJSON(t, srv, reader, http.MethodGet, path, ""))
	if len(list.Attachments) != 4 || list.Attachments[0].ID != pdf.ID || list.Attachments[0].AddedBy != "" || list.Attachments[0].AddedByName != "" {
		t.Errorf("as a reader sees them: %+v", list.Attachments)
	}

	// A document downloads; an image shows, as photos always did.
	rec = doJSON(t, srv, reader, http.MethodGet, path+"/"+pdf.ID, "")
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), testPDF) || rec.Header().Get("Content-Type") != "application/pdf" ||
		!strings.HasPrefix(rec.Header().Get("Content-Disposition"), "attachment") || !strings.Contains(rec.Header().Get("Content-Disposition"), "manual.pdf") {
		t.Errorf("the PDF: %d %v", rec.Code, rec.Header())
	}
	rec = doJSON(t, srv, reader, http.MethodGet, path+"/"+front.ID+"?size=thumb", "")
	if cfg, _, err := image.DecodeConfig(rec.Body); err != nil || cfg.Width != 320 || rec.Header().Get("Content-Disposition") != "" || rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Errorf("the thumbnail: %v %+v %v", err, cfg, rec.Header())
	}
	if rec := doJSON(t, srv, reader, http.MethodGet, path+"/missing", ""); rec.Code != http.StatusNotFound {
		t.Errorf("a missing attachment: %d", rec.Code)
	}

	// The photo routes see the images alone.
	photos := "/api/v1/assets/" + a.Asset.ID + "/photos"
	if rec := doJSON(t, srv, reader, http.MethodGet, photos+"/"+pdf.ID, ""); rec.Code != http.StatusNotFound {
		t.Errorf("a document as a photo: %d", rec.Code)
	}
	if rec := doJSON(t, srv, cookie, http.MethodDelete, photos+"/"+pdf.ID, ""); rec.Code != http.StatusNotFound {
		t.Errorf("removing a document as a photo: %d", rec.Code)
	}
	// Reordering photos moves them among the places images have.
	if rec := doJSON(t, srv, cookie, http.MethodPut, photos, `{"photo_ids":["`+back.ID+`","`+front.ID+`"]}`); rec.Code != http.StatusOK {
		t.Fatalf("reordering photos: %d %s", rec.Code, rec.Body)
	}
	order := func() []string {
		var ids []string
		for _, x := range decodeAttached(t, doJSON(t, srv, cookie, http.MethodGet, path, "")).Attachments {
			ids = append(ids, x.ID)
		}
		return ids
	}
	if got := strings.Join(order(), ","); got != strings.Join([]string{pdf.ID, back.ID, notes.ID, front.ID}, ",") {
		t.Errorf("after reordering photos: %s", got)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPut, photos, `{"photo_ids":["`+back.ID+`","`+pdf.ID+`"]}`); rec.Code != http.StatusBadRequest {
		t.Errorf("a document among photos: %d", rec.Code)
	}

	// Ordered, renamed and described.
	want := []string{front.ID, notes.ID, pdf.ID, back.ID}
	raw, _ := json.Marshal(map[string]any{"attachment_ids": want})
	if rec := doJSON(t, srv, cookie, http.MethodPut, path, string(raw)); rec.Code != http.StatusOK || strings.Join(order(), ",") != strings.Join(want, ",") {
		t.Errorf("reorder: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPut, path, `{"attachment_ids":["`+front.ID+`"]}`); rec.Code != http.StatusBadRequest {
		t.Errorf("an order missing some: %d", rec.Code)
	}
	rec = doJSON(t, srv, cookie, http.MethodPut, path+"/"+notes.ID, `{"name":" Care notes ","description":"Weekly"}`)
	if renamed := decodeAttached(t, rec).Attachment; rec.Code != http.StatusOK || renamed.Name != "Care notes" || renamed.Description != "Weekly" || renamed.Filename != "notes.md" {
		t.Errorf("rename: %d %+v", rec.Code, renamed)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPut, path+"/"+notes.ID, `{"name":"  "}`); rec.Code != http.StatusBadRequest {
		t.Errorf("an empty name: %d", rec.Code)
	}
	for what, r := range map[string]*httptest.ResponseRecorder{
		"add":    attach(t, srv, reader, path, "x.txt", []byte("x"), nil),
		"rename": doJSON(t, srv, reader, http.MethodPut, path+"/"+notes.ID, `{"name":"x"}`),
		"order":  doJSON(t, srv, reader, http.MethodPut, path, string(raw)),
		"remove": doJSON(t, srv, reader, http.MethodDelete, path+"/"+notes.ID, ""),
	} {
		if r.Code != http.StatusForbidden {
			t.Errorf("a reader could %s: %d", what, r.Code)
		}
	}

	// Removing one removes its file, and its history says so.
	files := func() int { return storedFiles(t, srv, "photos/") }
	if n := files(); n != 6 {
		t.Errorf("%d files for two documents and two images", n)
	}
	rec = doJSON(t, srv, cookie, http.MethodDelete, path+"/"+pdf.ID, "")
	if left := decodeAttached(t, rec).Attachments; rec.Code != http.StatusOK || len(left) != 3 {
		t.Errorf("remove: %d %s", rec.Code, rec.Body)
	}
	if n := files(); n != 5 {
		t.Errorf("%d files after removing the PDF", n)
	}
	history, _, err := srv.store.ListAssetEvents(a.Asset.ID, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	var seen []string
	for _, e := range history {
		for _, c := range e.Payload.Changes {
			seen = append(seen, c.What+" "+c.To+c.From)
		}
	}
	if s := strings.Join(seen, "; "); !strings.Contains(s, "attachment Manual") || !strings.Contains(s, "photo front.jpg") {
		t.Errorf("history: %s", s)
	}
}

// TestAttachmentsNeverRunInABrowser: whatever is uploaded, HTML and SVG
// among it, is served as a download that a browser never shows or runs,
// even where the instance allows such types.
func TestAttachmentsNeverRunInABrowser(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	a, _ := srv.store.CreateAsset("Kiosk", nil, nil, nil)
	path := "/api/v1/assets/" + a.Asset.ID + "/attachments"
	html := []byte("<!DOCTYPE html><html><body><script>alert(document.cookie)</script></body></html>")
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(document.cookie)</script></svg>`)
	xmlSVG := append([]byte(`<?xml version="1.0"?>`), svg...)

	// Not allowed, as themselves.
	for name, data := range map[string][]byte{"page.html": html, "picture.svg": svg, "picture.png": svg} {
		if rec := attach(t, srv, cookie, path, name, data, nil); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
	body, ct := multipartFile(t, "picture.png", svg, nil)
	if rec := do(t, srv, cookie, http.MethodPost, "/api/v1/assets/"+a.Asset.ID+"/photos", ct, bytes.NewReader(body)); rec.Code != http.StatusBadRequest {
		t.Errorf("an SVG as a photo: %d", rec.Code)
	}

	// As text, and where the instance allows them, they are kept, but
	// only ever downloaded.
	srv.cfg.Attachments.AllowedTypes = []string{"text/html", "text/xml", ".svg"}
	served := 0
	for name, data := range map[string][]byte{"page.txt": html, "page.html": html, "picture.svg": svg, "drawing.svg": xmlSVG, "notes.md": svg} {
		rec := attach(t, srv, cookie, path, name, data, nil)
		if rec.Code != http.StatusCreated {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
			continue
		}
		added := decodeAttached(t, rec).Attachment
		if added.Kind == models.AttachmentImage {
			t.Errorf("%s is an image", name)
		}
		got := doJSON(t, srv, cookie, http.MethodGet, path+"/"+added.ID, "")
		h := got.Header()
		contentType := h.Get("Content-Type")
		if got.Code != http.StatusOK || !strings.HasPrefix(h.Get("Content-Disposition"), "attachment;") || h.Get("X-Content-Type-Options") != "nosniff" ||
			!strings.HasPrefix(h.Get("Content-Security-Policy"), "sandbox") || strings.Contains(contentType, "html") || strings.Contains(contentType, "svg") || strings.Contains(contentType, "xml") {
			t.Errorf("%s served as %d %v", name, got.Code, h)
		}
		served++
	}
	if served != 5 {
		t.Errorf("served %d", served)
	}
}

func TestAttachmentLimits(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	a, _ := srv.store.CreateAsset("Lathe", nil, nil, nil)
	path := "/api/v1/assets/" + a.Asset.ID + "/attachments"
	srv.cfg.Attachments.MaxFileMB, srv.cfg.Attachments.MaxPerObject = 1, 3
	files := func() int { return storedFiles(t, srv, "photos/") }

	// Each file at most max_file_mb, read or declared.
	if rec := attach(t, srv, cookie, path, "big.txt", bytes.Repeat([]byte("x"), 1<<20+1), nil); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("too large: %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, srv, cookie, http.MethodPost, path, "multipart/form-data; boundary=x", bytes.NewReader(bytes.Repeat([]byte("x"), 2<<20))); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("a body too large: %d", rec.Code)
	}
	if rec := attach(t, srv, cookie, path, "just.txt", bytes.Repeat([]byte("x"), 1<<20), nil); rec.Code != http.StatusCreated {
		t.Errorf("just small enough: %d %s", rec.Code, rec.Body)
	}

	// Types by their content: a PDF must be one, text must be text, and
	// others only those allowed.
	zip := []byte("PK\x03\x04\x14\x00\x00\x00\x08\x00rest of a zip")
	for name, data := range map[string][]byte{"fake.pdf": []byte("not a PDF"), "binary.txt": {0, 1, 2, 3}, "parts.zip": zip, "model.stl": {0, 1, 2, 3}} {
		if rec := attach(t, srv, cookie, path, name, data, nil); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
	srv.cfg.Attachments.AllowedTypes = []string{"application/zip"}
	rec := attach(t, srv, cookie, path, "parts.zip", zip, nil)
	if got := decodeAttached(t, rec).Attachment; rec.Code != http.StatusCreated || got.Kind != models.AttachmentOther || got.ContentType != "application/zip" {
		t.Errorf("an allowed type: %d %+v", rec.Code, got)
	}
	// An allowed extension is enough only for content that shows no type
	// of its own.
	srv.cfg.Attachments.AllowedTypes = []string{".stl"}
	if rec := attach(t, srv, cookie, path, "parts.stl", zip, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("a zip named as an allowed extension: %d %s", rec.Code, rec.Body)
	}
	rec = attach(t, srv, cookie, path, "model.stl", []byte{0, 1, 2, 3}, nil)
	if got := decodeAttached(t, rec).Attachment; rec.Code != http.StatusCreated || got.Kind != models.AttachmentOther || got.ContentType != "application/octet-stream" {
		t.Errorf("an allowed extension: %d %+v", rec.Code, got)
	}

	// No more than max_per_object, leaving no files behind.
	before := files()
	if rec := attach(t, srv, cookie, path, "third.txt", []byte("three"), nil); rec.Code != http.StatusConflict {
		t.Errorf("one too many: %d %s", rec.Code, rec.Body)
	}
	if n := files(); n != before {
		t.Errorf("a refused upload left files: %d, was %d", n, before)
	}
}

// TestAttachmentPermissions: an asset's follow assets.write and its scopes;
// a category's follow categories.manage, and are read as the category is.
func TestAttachmentPermissions(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	st := srv.store
	scope, err := st.CreateScope("workshop")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateScope("kitchen"); err != nil {
		t.Fatal(err)
	}
	cat, err := st.CreateCategory("Lathes", nil, "#123456", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetCategoryScopes(cat.ID, []string{scope.ID}); err != nil {
		t.Fatal(err)
	}
	a, err := st.CreateAsset("Lathe", []string{cat.ID}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	assetPath := "/api/v1/assets/" + a.Asset.ID + "/attachments"
	catPath := "/api/v1/categories/" + cat.ID + "/attachments"
	onAsset := decodeAttached(t, attach(t, srv, cookie, assetPath, "manual.pdf", testPDF, nil)).Attachment
	rec := attach(t, srv, cookie, catPath, "guide.txt", []byte("How to use a lathe."), nil)
	onCat := decodeAttached(t, rec).Attachment
	if rec.Code != http.StatusCreated || onCat.Kind != models.AttachmentDocument {
		t.Fatalf("on a category: %d %s", rec.Code, rec.Body)
	}

	inScope, elsewhere := scopedReader(t, srv, "workshop"), scopedReader(t, srv, "kitchen")
	reader := userWithPermissions(t, srv, "reader@hackspace.org", rbac.PermAssetsRead)
	writer := userWithPermissions(t, srv, "writer@hackspace.org", rbac.PermAssetsRead, rbac.PermAssetsWrite)
	keeper := userWithPermissions(t, srv, "keeper@hackspace.org", rbac.PermAssetsRead, rbac.PermCategoriesManage)
	for _, c := range []struct {
		what   string
		who    *http.Cookie
		method string
		path   string
		want   int
	}{
		{"the scope's reader lists the asset's", inScope, http.MethodGet, assetPath, http.StatusOK},
		{"the scope's reader reads the asset's", inScope, http.MethodGet, assetPath + "/" + onAsset.ID, http.StatusOK},
		{"another scope's reader does not see the asset's", elsewhere, http.MethodGet, assetPath, http.StatusNotFound},
		{"nor read one", elsewhere, http.MethodGet, assetPath + "/" + onAsset.ID, http.StatusNotFound},
		{"a reader reads the category's", reader, http.MethodGet, catPath + "/" + onCat.ID, http.StatusOK},
		{"a scoped reader lists the category's, as they see the category", elsewhere, http.MethodGet, catPath, http.StatusOK},
		{"a writer of assets cannot rename the category's", writer, http.MethodPut, catPath + "/" + onCat.ID, http.StatusForbidden},
		{"a writer of assets renames the asset's", writer, http.MethodPut, assetPath + "/" + onAsset.ID, http.StatusOK},
		{"a keeper of categories renames the category's", keeper, http.MethodPut, catPath + "/" + onCat.ID, http.StatusOK},
		{"a keeper of categories cannot rename the asset's", keeper, http.MethodPut, assetPath + "/" + onAsset.ID, http.StatusForbidden},
		{"a reader cannot remove the category's", reader, http.MethodDelete, catPath + "/" + onCat.ID, http.StatusForbidden},
		{"a missing category's", cookie, http.MethodGet, "/api/v1/categories/nope/attachments", http.StatusNotFound},
	} {
		if rec := doJSON(t, srv, c.who, c.method, c.path, `{"description":"Checked"}`); rec.Code != c.want {
			t.Errorf("%s: %d, want %d: %s", c.what, rec.Code, c.want, rec.Body)
		}
	}
	if rec := attach(t, srv, writer, catPath, "x.txt", []byte("x"), nil); rec.Code != http.StatusForbidden {
		t.Errorf("a writer of assets added to a category: %d", rec.Code)
	}
	if rec := attach(t, srv, keeper, catPath, "y.txt", []byte("y"), nil); rec.Code != http.StatusCreated {
		t.Errorf("a keeper of categories adding: %d %s", rec.Code, rec.Body)
	}
	rec = doJSON(t, srv, keeper, http.MethodDelete, catPath+"/"+onCat.ID, "")
	if left := decodeAttached(t, rec).Attachments; rec.Code != http.StatusOK || len(left) != 1 {
		t.Errorf("a keeper of categories removing: %d %s", rec.Code, rec.Body)
	}
}

// TestCopyingAttachments: a copy of an asset gets copies of its
// attachments, each with files of its own; the photo route copies images.
func TestCopyingAttachments(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	from, _ := srv.store.CreateAsset("Yellow spool", nil, nil, nil)
	path := func(id string) string { return "/api/v1/assets/" + id + "/attachments" }
	attach(t, srv, cookie, path(from.Asset.ID), "spool.jpg", testJPEG(t, 30, 30), nil)
	attach(t, srv, cookie, path(from.Asset.ID), "datasheet.pdf", testPDF, map[string]string{"name": "Datasheet"})

	to, _ := srv.store.CreateAsset("Blue spool", nil, nil, nil)
	rec := doJSON(t, srv, cookie, http.MethodPost, path(to.Asset.ID)+"/copy", `{"from_asset_id":"`+from.Asset.ID+`"}`)
	copies := decodeAttached(t, rec).Attachments
	if rec.Code != http.StatusOK || len(copies) != 2 || copies[1].Name != "Datasheet" || copies[1].Kind != models.AttachmentDocument {
		t.Fatalf("copy: %d %s", rec.Code, rec.Body)
	}
	originals := decodeAttached(t, doJSON(t, srv, cookie, http.MethodGet, path(from.Asset.ID), "")).Attachments
	for i, c := range copies {
		if c.ID == originals[i].ID {
			t.Errorf("copy %d shares its original's ID", i)
		}
		mine := doJSON(t, srv, cookie, http.MethodGet, path(to.Asset.ID)+"/"+c.ID, "").Body.Bytes()
		theirs := doJSON(t, srv, cookie, http.MethodGet, path(from.Asset.ID)+"/"+originals[i].ID, "").Body.Bytes()
		if !bytes.Equal(mine, theirs) || len(mine) == 0 {
			t.Errorf("copy %d is not the same file", i)
		}
	}
	// Files of their own: three each (an image, its thumbnail, a PDF).
	if n := storedFiles(t, srv, "photos/"); n != 6 {
		t.Errorf("%d files", n)
	}
	doJSON(t, srv, cookie, http.MethodDelete, path(to.Asset.ID)+"/"+copies[1].ID, "")
	if rec := doJSON(t, srv, cookie, http.MethodGet, path(from.Asset.ID)+"/"+originals[1].ID, ""); rec.Code != http.StatusOK {
		t.Errorf("removing a copy took the original: %d", rec.Code)
	}

	// The photo route copies only the images.
	other, _ := srv.store.CreateAsset("Green spool", nil, nil, nil)
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/assets/"+other.Asset.ID+"/photos/copy", `{"from_asset_id":"`+from.Asset.ID+`"}`)
	var got struct{ Asset models.AssetWithDetails }
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if rec.Code != http.StatusOK || len(got.Asset.Photos) != 1 {
		t.Errorf("copying photos: %d %s", rec.Code, rec.Body)
	}
	if n := len(decodeAttached(t, doJSON(t, srv, cookie, http.MethodGet, path(other.Asset.ID), "")).Attachments); n != 1 {
		t.Errorf("copying photos gave %d attachments", n)
	}
}

// TestSweepingAttachments: bam storage's sweep keeps every attachment's
// files, a category's and a document's among them.
func TestSweepingAttachments(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	a, _ := srv.store.CreateAsset("Drill", nil, nil, nil)
	cat, _ := srv.store.CreateCategory("Drills", nil, "#123456", false, false)
	attach(t, srv, cookie, "/api/v1/assets/"+a.Asset.ID+"/attachments", "front.jpg", testJPEG(t, 20, 20), nil)
	attach(t, srv, cookie, "/api/v1/assets/"+a.Asset.ID+"/attachments", "manual.pdf", testPDF, nil)
	attach(t, srv, cookie, "/api/v1/categories/"+cat.ID+"/attachments", "guide.md", []byte("# Guide\n"), nil)
	named, err := srv.store.StoredFiles()
	if err != nil || len(named) != 4 {
		t.Fatalf("named: %v %v", named, err)
	}
	res, err := srv.SweepStorage(t.Context(), false)
	if err != nil || len(res.Removed) != 0 || len(res.Missing) != 0 {
		t.Errorf("swept: %+v %v", res, err)
	}
	if n := storedFiles(t, srv, "photos/"); n != 4 {
		t.Errorf("%d files left", n)
	}
}
