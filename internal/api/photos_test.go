package api

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

func testJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 128, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestAssetPhotosAPI(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	reader := userWithPermissions(t, srv, "reader@hackspace.org", rbac.PermAssetsRead)
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/assets", `{"name":"Drill"}`)
	var created struct{ Asset models.AssetWithDetails }
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	path := "/api/v1/assets/" + created.Asset.Asset.ID + "/photos"

	upload := func(who *http.Cookie, data []byte) *bytes.Buffer {
		t.Helper()
		body, ct := multipartBody(nil, false, map[string][]byte{"file": data}, nil)
		rec := do(t, srv, who, http.MethodPost, path, ct, bytes.NewReader(body))
		if rec.Code != http.StatusCreated {
			return bytes.NewBufferString(rec.Result().Status + " " + rec.Body.String())
		}
		return rec.Body
	}
	if got := upload(reader, testJPEG(t, 40, 30)); !strings.HasPrefix(got.String(), "403") {
		t.Errorf("a reader uploaded: %s", got)
	}
	if got := upload(cookie, []byte("not a photo")); !strings.HasPrefix(got.String(), "400") {
		t.Errorf("not a photo: %s", got)
	}
	body := upload(cookie, testJPEG(t, 3000, 1000))
	var a struct{ Asset models.AssetWithDetails }
	if err := json.Unmarshal(body.Bytes(), &a); err != nil || len(a.Asset.Photos) != 1 {
		t.Fatalf("upload: %s", body)
	}
	first := a.Asset.Photos[0]
	if first.WidthPx != 2048 || first.HeightPx != 682 || first.ContentType != "image/jpeg" {
		t.Errorf("scaled to %+v", first)
	}

	// Readers see it, and its thumbnail.
	for query, width := range map[string]int{"": 2048, "?size=thumb": 320} {
		rec := doJSON(t, srv, reader, http.MethodGet, path+"/"+first.ID+query, "")
		cfg, _, err := image.DecodeConfig(rec.Body)
		if rec.Code != http.StatusOK || err != nil || cfg.Width != width || rec.Header().Get("Content-Type") != "image/jpeg" {
			t.Errorf("%q: %d %v %+v", query, rec.Code, err, cfg)
		}
	}
	if rec := doJSON(t, srv, reader, http.MethodGet, path+"/missing", ""); rec.Code != http.StatusNotFound {
		t.Errorf("a missing photo: %d", rec.Code)
	}

	// A second, then put it first.
	upload(cookie, testJPEG(t, 50, 50))
	got, _ := srv.store.GetAssetByID(created.Asset.Asset.ID)
	second := got.Photos[1]
	rec = doJSON(t, srv, cookie, http.MethodPut, path, `{"photo_ids":["`+second.ID+`","`+first.ID+`"]}`)
	_ = json.Unmarshal(rec.Body.Bytes(), &a)
	if rec.Code != http.StatusOK || a.Asset.Photos[0].ID != second.ID {
		t.Errorf("reorder: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPut, path, `{"photo_ids":["`+first.ID+`"]}`); rec.Code != http.StatusBadRequest {
		t.Errorf("an order missing one: %d", rec.Code)
	}

	// Removing it removes its files.
	files := func() int {
		entries, _ := os.ReadDir(filepath.Join(srv.cfg.Storage.LocalPath, "photos"))
		return len(entries)
	}
	if n := files(); n != 4 {
		t.Errorf("%d files for two photos", n)
	}
	if rec := doJSON(t, srv, reader, http.MethodDelete, path+"/"+first.ID, ""); rec.Code != http.StatusForbidden {
		t.Errorf("a reader removed a photo: %d", rec.Code)
	}
	if rec := doJSON(t, srv, cookie, http.MethodDelete, path+"/"+first.ID, ""); rec.Code != http.StatusOK {
		t.Errorf("remove: %d %s", rec.Code, rec.Body)
	}
	if n := files(); n != 2 {
		t.Errorf("%d files after removing one of two photos", n)
	}

	// No more than MaxPhotos.
	for i := 1; i < store.MaxPhotos; i++ {
		upload(cookie, testJPEG(t, 8, 8))
	}
	if got := upload(cookie, testJPEG(t, 8, 8)); !strings.HasPrefix(got.String(), "409") {
		t.Errorf("one too many: %s", got)
	}
	if n := files(); n != 2*store.MaxPhotos {
		t.Errorf("a refused upload left files: %d", n)
	}
	// Too large is refused before it is read.
	big := bytes.Repeat([]byte("x"), photoUploadLimit+1)
	if rec := do(t, srv, cookie, http.MethodPost, path, "multipart/form-data; boundary=x", bytes.NewReader(big)); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("too large: %d", rec.Code)
	}
}

func TestCopyingPhotos(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	reader := userWithPermissions(t, srv, "reader@hackspace.org", rbac.PermAssetsRead)
	made := func(name string) string {
		t.Helper()
		a, err := srv.store.CreateAsset(name, nil, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		return a.Asset.ID
	}
	upload := func(id string, data []byte) {
		t.Helper()
		body, ct := multipartBody(nil, false, map[string][]byte{"file": data}, nil)
		if rec := do(t, srv, cookie, http.MethodPost, "/api/v1/assets/"+id+"/photos", ct, bytes.NewReader(body)); rec.Code != http.StatusCreated {
			t.Fatalf("upload: %d %s", rec.Code, rec.Body)
		}
	}
	files := func() int {
		entries, _ := os.ReadDir(filepath.Join(srv.cfg.Storage.LocalPath, "photos"))
		return len(entries)
	}
	served := func(id, photoID string) []byte {
		t.Helper()
		rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/assets/"+id+"/photos/"+photoID, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("serving %s: %d", photoID, rec.Code)
		}
		return rec.Body.Bytes()
	}

	from, to := made("Yellow spool"), made("Blue spool")
	upload(from, testJPEG(t, 60, 40))
	upload(from, testJPEG(t, 30, 30))
	path := "/api/v1/assets/" + to + "/photos/copy"
	body := `{"from_asset_id":"` + from + `"}`

	for what, c := range map[string]struct {
		who  *http.Cookie
		body string
		want int
	}{
		"a reader":         {reader, body, http.StatusForbidden},
		"no asset to copy": {cookie, `{}`, http.StatusBadRequest},
		"broken JSON":      {cookie, `{`, http.StatusBadRequest},
		"a missing asset":  {cookie, `{"from_asset_id":"nope"}`, http.StatusNotFound},
	} {
		if rec := doJSON(t, srv, c.who, http.MethodPost, path, c.body); rec.Code != c.want {
			t.Errorf("%s: %d %s", what, rec.Code, rec.Body)
		}
	}
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/assets/nope/photos/copy", body); rec.Code != http.StatusNotFound {
		t.Errorf("copying to a missing asset: %d", rec.Code)
	}

	rec := doJSON(t, srv, cookie, http.MethodPost, path, body)
	var got struct{ Asset models.AssetWithDetails }
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("copy: %d %s", rec.Code, rec.Body)
	}
	original, _ := srv.store.GetAssetByID(from)
	if len(got.Asset.Photos) != 2 {
		t.Fatalf("copied %d photos", len(got.Asset.Photos))
	}
	for i, c := range got.Asset.Photos {
		o := original.Photos[i]
		if c.ID == o.ID || c.ThumbID == o.ThumbID || c.Filename != o.Filename || c.WidthPx != o.WidthPx || c.ByteSize != o.ByteSize {
			t.Errorf("photo %d: %+v, of %+v", i, c, o)
		}
		if !bytes.Equal(served(to, c.ID), served(from, o.ID)) {
			t.Errorf("photo %d is not the same picture", i)
		}
	}
	// Files of their own: removing a copy leaves the original.
	if n := files(); n != 8 {
		t.Errorf("%d files for two photos and their copies", n)
	}
	if rec := doJSON(t, srv, cookie, http.MethodDelete, "/api/v1/assets/"+to+"/photos/"+got.Asset.Photos[0].ID, ""); rec.Code != http.StatusOK {
		t.Fatalf("remove: %d", rec.Code)
	}
	served(from, original.Photos[0].ID)

	// All or none: too many leaves no files behind.
	full := made("Full")
	for i := 0; i < store.MaxPhotos-1; i++ {
		upload(full, testJPEG(t, 8, 8))
	}
	before := files()
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/assets/"+full+"/photos/copy", body); rec.Code != http.StatusConflict {
		t.Errorf("one too many: %d %s", rec.Code, rec.Body)
	}
	if n := files(); n != before {
		t.Errorf("a refused copy left files: %d, was %d", n, before)
	}
	// An asset without photos gives none.
	if rec := doJSON(t, srv, cookie, http.MethodPost, path, `{"from_asset_id":"`+made("Bare")+`"}`); rec.Code != http.StatusOK {
		t.Errorf("copying none: %d %s", rec.Code, rec.Body)
	}
}
