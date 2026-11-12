package api

import (
	"encoding/json"
	"image/png"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

func TestAnOpenPaperFramePullsWithBasicAuth(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	display := createDisplay(t, srv, cookie, "Front desk", printers.DriverPaperlessPaper, "epd7", `{"type":"pull"}`)
	tpl := createTemplate(t, srv, cookie, panelTemplate("Frame", 800, 480, 128))
	a, _ := srv.store.CreateAsset("Lathe", nil, nil, nil)
	printTo(t, srv, cookie, display, tpl, a.Asset.ID)

	// Its token goes in Basic's password: the frame sends no other header,
	// and takes an address of at most 128 characters.
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/displays/"+display+"/tokens", `{"name":"Front desk"}`)
	var made struct {
		Secret, URL, Username string
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &made)
	if rec.Code != http.StatusCreated || strings.Contains(made.URL, "token") || !strings.HasSuffix(made.URL, "/api/v1/displays/"+display+"/image") || len(made.URL) > 128 || made.Username != "bam" {
		t.Fatalf("made %d %+v", rec.Code, made)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/displays/"+display+"/image", nil)
	req.SetBasicAuth(made.Username, made.Secret)
	got := httptest.NewRecorder()
	srv.Handler().ServeHTTP(got, req)
	b := got.Body.Bytes()
	if got.Code != http.StatusOK || got.Header().Get("Content-Type") != "image/bmp" || len(b) < 30 || b[28] != 4 {
		t.Fatalf("image %d %v", got.Code, got.Header())
	}
	// It sleeps for what it is told, by its own header.
	if got.Header().Get("X-OpenPaper-Sleep-Seconds") != strconv.Itoa(store.DefaultDisplayPullRefresh) || len(got.Header().Get("ETag")) > 128 {
		t.Fatalf("headers %v", got.Header())
	}
	req.SetBasicAuth("bam", store.DisplayTokenPrefix+"wrong")
	got = httptest.NewRecorder()
	srv.Handler().ServeHTTP(got, req)
	if got.Code != http.StatusUnauthorized {
		t.Fatalf("a wrong password: %d", got.Code)
	}
	// Other displays are not told to sleep so.
	other := createDisplay(t, srv, cookie, "Bin", printers.DriverDisplay, "2.9in-296x128", `{"type":"pull"}`)
	_, secret := makeDisplayToken(t, srv, cookie, other)
	if rec := fetch(srv, http.MethodGet, "/api/v1/displays/"+other+"/image?token="+secret, nil); rec.Code != http.StatusOK || rec.Header().Get("X-OpenPaper-Sleep-Seconds") != "" {
		t.Fatalf("another display: %d %v", rec.Code, rec.Header())
	}
}

func TestAnOpenPaperFrameIsPushedThroughPaperlessPapersCloud(t *testing.T) {
	srv, admin, _ := factorServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	var mu sync.Mutex
	var keys, paths []string
	var forms []*multipart.Form
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		form, _ := multipart.NewReader(r.Body, params["boundary"]).ReadForm(1 << 22)
		mu.Lock()
		keys, paths, forms = append(keys, r.Header.Get("x-api-key")), append(paths, r.URL.Path), append(forms, form)
		mu.Unlock()
		_, _ = io.WriteString(w, `{}`)
	}))
	defer cloud.Close()

	// Its API and key are the instance's, the key sealed and never shown.
	settings := func(body string) DisplaySettings {
		rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/settings/displays", body)
		if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "pk_live") {
			t.Fatalf("settings: %d %s", rec.Code, rec.Body)
		}
		var s DisplaySettings
		_ = json.Unmarshal(rec.Body.Bytes(), &s)
		return s
	}
	if s := settings(`{"refresh_window_seconds":30,"pull_refresh_seconds":900}`); s.PaperlessPaperBaseURL != printers.DefaultPaperlessPaperBase || s.PaperlessPaperAPIKeySet {
		t.Fatalf("defaults %+v", s)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/settings/displays", `{"refresh_window_seconds":30,"pull_refresh_seconds":900,"paperlesspaper_base_url":"ftp://x"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("an ftp URL: %d", rec.Code)
	}
	if s := settings(`{"refresh_window_seconds":30,"pull_refresh_seconds":900,"paperlesspaper_base_url":"` + cloud.URL + `/v1/","paperlesspaper_api_key":"pk_live"}`); s.PaperlessPaperBaseURL != cloud.URL+"/v1" || !s.PaperlessPaperAPIKeySet {
		t.Fatalf("set %+v", s)
	}
	if sealed, _ := srv.store.Setting(store.PaperlessPaperKeySetting, ""); sealed == "" || strings.Contains(sealed, "pk_live") {
		t.Fatalf("kept as %q", sealed)
	}
	// Leaving the key out keeps it.
	if s := settings(`{"refresh_window_seconds":30,"pull_refresh_seconds":900,"paperlesspaper_base_url":"` + cloud.URL + `/v1"}`); !s.PaperlessPaperAPIKeySet {
		t.Fatalf("kept %+v", s)
	}

	display := createDisplay(t, srv, cookie, "Front desk", printers.DriverPaperlessPaper, "openpaper13", `{"type":"paperlesspaper","address":"65A1F0C2E4B3D5A6B7C8D9E0"}`)
	tpl := createTemplate(t, srv, cookie, panelTemplate("Frame", 1200, 1600, 150))
	a, _ := srv.store.CreateAsset("Lathe", nil, nil, nil)
	printTo(t, srv, cookie, display, tpl, a.Asset.ID)
	mu.Lock()
	if len(paths) != 1 || paths[0] != "/v1/papers/uploadSingleImage/65a1f0c2e4b3d5a6b7c8d9e0" || keys[0] != "pk_live" || forms[0] == nil {
		mu.Unlock()
		t.Fatalf("uploads %v %v", paths, keys)
	}
	f, _ := forms[0].File["pictureDevice"][0].Open()
	img, err := png.Decode(f)
	f.Close()
	mu.Unlock()
	if err != nil || img.Bounds().Dx() != 1200 || img.Bounds().Dy() != 1600 {
		t.Fatalf("image %v %v", img.Bounds(), err)
	}

	// Without its key, printing says what to do.
	settings(`{"refresh_window_seconds":30,"pull_refresh_seconds":900,"paperlesspaper_base_url":"` + cloud.URL + `/v1","paperlesspaper_api_key":""}`)
	ids, _ := json.Marshal([]string{a.Asset.ID})
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+display+"/print", `{"template_id":"`+tpl+`","asset_ids":`+string(ids)+`}`)
	if rec.Code == http.StatusOK || !strings.Contains(rec.Body.String(), "API key is not set") {
		t.Fatalf("no key: %d %s", rec.Code, rec.Body)
	}
}

func TestPaperlessPapersKeyNeedsASecretKey(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/settings/displays", `{"refresh_window_seconds":30,"pull_refresh_seconds":900,"paperlesspaper_api_key":"pk_live"}`)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "secret_key") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}
