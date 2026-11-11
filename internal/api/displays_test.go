package api

import (
	"bytes"
	"encoding/json"
	"image/jpeg"
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
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// panelTemplate is a template of a panel's size at its resolution, naming
// the asset and its condition, so that a change of condition changes the
// image.
func panelTemplate(name string, w, h, dpi int) string {
	mm := func(px int) string { return strconv.FormatFloat(float64(px)*25.4/float64(dpi), 'f', -1, 64) }
	return `{"name":"` + name + `","units":"mm","width":` + mm(w) + `,"height":` + mm(h) + `,"dpi":` + strconv.Itoa(dpi) + `,"elements":[
  {"type":"text","x":2,"y":2,"width":40,"height":8,"font_size":12,"content_binding":"{{asset.name}}"},
  {"type":"text","x":2,"y":12,"width":40,"height":8,"font_size":12,"content_binding":"{{asset.condition}}"}]}`
}

// createDisplay makes a display, answering its ID.
func createDisplay(t *testing.T, srv *Server, cookie *http.Cookie, name, driver, model, connection string) string {
	t.Helper()
	body := `{"name":"` + name + `","driver":"` + driver + `","model":"` + model + `","media":"` + model + `","connection":` + connection + `}`
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("creating display: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct{ Printer models.Printer }
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	return resp.Printer.ID
}

func printTo(t *testing.T, srv *Server, cookie *http.Cookie, printerID, templateID string, assetIDs ...string) {
	t.Helper()
	ids, _ := json.Marshal(assetIDs)
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers/"+printerID+"/print", `{"template_id":"`+templateID+`","asset_ids":`+string(ids)+`}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("printing to the display: %d %s", rec.Code, rec.Body.String())
	}
}

// fetch gets a path with headers, outside any session.
func fetch(srv *Server, method, path string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func makeDisplayToken(t *testing.T, srv *Server, cookie *http.Cookie, displayID string) (string, string) {
	t.Helper()
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/displays/"+displayID+"/tokens", `{"name":"Shelf frame"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("making a display token: %d %s", rec.Code, rec.Body.String())
	}
	var made struct {
		Token  models.DisplayToken
		Secret string
		URL    string
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &made)
	if !strings.HasPrefix(made.Secret, store.DisplayTokenPrefix) || !strings.HasSuffix(made.URL, "/api/v1/displays/"+displayID+"/image?token="+made.Secret) {
		t.Fatalf("made %+v", made)
	}
	return made.Token.ID, made.Secret
}

func TestThePullURLServesTheCurrentImageWithAnETag(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	display := createDisplay(t, srv, cookie, "Bin A3", printers.DriverDisplay, "2.9in-296x128", `{"type":"pull"}`)
	other := createDisplay(t, srv, cookie, "Bin A4", printers.DriverDisplay, "2.9in-296x128", `{"type":"pull"}`)
	tpl := createTemplate(t, srv, cookie, panelTemplate("Bin", 296, 128, 112))
	drill, _ := srv.store.CreateAsset("Drill", nil, nil, nil)
	saw, _ := srv.store.CreateAsset("Saw", nil, nil, nil)

	_, secret := makeDisplayToken(t, srv, cookie, display)
	image := "/api/v1/displays/" + display + "/image"

	// Shown nothing yet, it is blank, and says when to ask again.
	rec := fetch(srv, http.MethodGet, image+"?token="+secret, nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" || rec.Header().Get("Refresh") != strconv.Itoa(store.DefaultDisplayPullRefresh) {
		t.Fatalf("blank: %d %v", rec.Code, rec.Header())
	}
	blank := rec.Header().Get("ETag")

	printTo(t, srv, cookie, display, tpl, saw.Asset.ID, drill.Asset.ID)
	rec = fetch(srv, http.MethodGet, image, map[string]string{"Authorization": "Bearer " + secret})
	if rec.Code != http.StatusOK {
		t.Fatalf("image: %d %s", rec.Code, rec.Body)
	}
	img, err := png.Decode(bytes.NewReader(rec.Body.Bytes()))
	if err != nil || img.Bounds().Dx() != 296 || img.Bounds().Dy() != 128 {
		t.Fatalf("image %v, %v", img.Bounds(), err)
	}
	etag := rec.Header().Get("ETag")
	if etag == "" || etag == blank || rec.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("headers %v", rec.Header())
	}
	// Unchanged, it is not sent again.
	rec = fetch(srv, http.MethodGet, image+"?token="+secret, map[string]string{"If-None-Match": etag})
	if rec.Code != http.StatusNotModified || rec.Body.Len() != 0 {
		t.Fatalf("not modified: %d", rec.Code)
	}

	// It remembers the last label it was shown, and as whom.
	rec = doJSON(t, srv, cookie, http.MethodGet, "/api/v1/displays/"+display, "")
	var got struct{ Display DisplayView }
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if s := got.Display.Shown; s == nil || s.AssetID == nil || *s.AssetID != drill.Asset.ID || s.TemplateID == nil || *s.TemplateID != tpl ||
		s.ShownBy != admin.ID || s.FetchedAt == nil || !strings.HasSuffix(got.Display.PullURL, image) {
		t.Fatalf("display %+v %+v", got.Display, got.Display.Shown)
	}

	// Shown another asset, its image changes.
	printTo(t, srv, cookie, display, tpl, saw.Asset.ID)
	rec = fetch(srv, http.MethodGet, image+"?token="+secret, map[string]string{"If-None-Match": etag})
	if rec.Code != http.StatusOK || rec.Header().Get("ETag") == etag {
		t.Fatalf("changed: %d %v", rec.Code, rec.Header())
	}

	// Its own options say how often it asks.
	rec = doJSON(t, srv, cookie, http.MethodPut, "/api/v1/printers/"+display, `{"name":"Bin A3","driver":"display","model":"2.9in-296x128","media":"2.9in-296x128","connection":{"type":"pull"},"options":{"refresh_seconds":120}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("options: %d %s", rec.Code, rec.Body)
	}
	if rec := fetch(srv, http.MethodGet, image+"?token="+secret, nil); rec.Header().Get("Refresh") != "120" {
		t.Fatalf("refresh %q", rec.Header().Get("Refresh"))
	}
	// And the instance's settings, for the rest.
	if rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/settings/displays", `{"refresh_window_seconds":10,"pull_refresh_seconds":30}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("a pull every 30 seconds: %d", rec.Code)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/settings/displays", `{"refresh_window_seconds":10,"pull_refresh_seconds":600}`); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"pull_refresh_seconds":600`) {
		t.Fatalf("settings: %d %s", rec.Code, rec.Body)
	}
	if rec := fetch(srv, http.MethodGet, "/api/v1/displays/"+other+"/image?token=x", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a bad token: %d", rec.Code)
	}
}

func TestADisplaysTokenReachesOnlyItsOwnImage(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	display := createDisplay(t, srv, cookie, "Bin A3", printers.DriverDisplay, "2.9in-296x128", `{"type":"pull"}`)
	other := createDisplay(t, srv, cookie, "Bin A4", printers.DriverDisplay, "2.9in-296x128", `{"type":"pull"}`)
	tokenID, secret := makeDisplayToken(t, srv, cookie, display)

	for _, c := range []struct {
		why    string
		method string
		path   string
		header map[string]string
		want   int
	}{
		{"its own image", http.MethodGet, "/api/v1/displays/" + display + "/image?token=" + secret, nil, http.StatusOK},
		{"its own image, by TRMNL's header", http.MethodGet, "/api/v1/displays/" + display + "/image", map[string]string{"Access-Token": secret}, http.StatusOK},
		{"another display's image", http.MethodGet, "/api/v1/displays/" + other + "/image?token=" + secret, nil, http.StatusForbidden},
		{"the display itself", http.MethodGet, "/api/v1/displays/" + display + "?token=" + secret, nil, http.StatusUnauthorized},
		{"as an API token", http.MethodGet, "/api/v1/printers", map[string]string{"Authorization": "Bearer " + secret}, http.StatusUnauthorized},
		{"as an API token, on its display", http.MethodGet, "/api/v1/displays/" + display, map[string]string{"Authorization": "Bearer " + secret}, http.StatusUnauthorized},
		{"no token at all", http.MethodGet, "/api/v1/displays/" + display + "/image", nil, http.StatusUnauthorized},
		{"changing anything", http.MethodPost, "/api/v1/displays/" + display + "/refresh?token=" + secret, nil, http.StatusUnauthorized},
	} {
		if rec := fetch(srv, c.method, c.path, c.header); rec.Code != c.want {
			t.Errorf("%s: %d %s, want %d", c.why, rec.Code, rec.Body, c.want)
		}
	}

	// Those who may see printers see it signed in; others do not.
	if rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/displays/"+display+"/image", ""); rec.Code != http.StatusOK {
		t.Errorf("signed in: %d", rec.Code)
	}
	nobody := userWithPermissions(t, srv, "nobody@test.org", rbac.PermAssetsRead)
	if rec := doJSON(t, srv, nobody, http.MethodGet, "/api/v1/displays/"+display+"/image", ""); rec.Code != http.StatusForbidden {
		t.Errorf("signed in without printers: %d", rec.Code)
	}

	// Revoked, it fetches nothing.
	if rec := doJSON(t, srv, cookie, http.MethodDelete, "/api/v1/displays/"+display+"/tokens/"+tokenID, ""); rec.Code != http.StatusOK {
		t.Fatalf("revoking: %d", rec.Code)
	}
	if rec := fetch(srv, http.MethodGet, "/api/v1/displays/"+display+"/image?token="+secret, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("revoked: %d", rec.Code)
	}
	// The token is kept as a hash.
	var hash string
	if err := srv.store.QueryRow("SELECT token_hash FROM display_tokens WHERE printer_id = ?", other).Scan(&hash); err == nil {
		t.Fatal("a token for the other display")
	}
	_, secret = makeDisplayToken(t, srv, cookie, display)
	if err := srv.store.QueryRow("SELECT token_hash FROM display_tokens WHERE printer_id = ?", display).Scan(&hash); err != nil || strings.Contains(hash, secret) || len(hash) != 64 {
		t.Fatalf("hash %q, %v", hash, err)
	}

	// A token needs a name.
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/displays/"+display+"/tokens", `{"name":""}`); rec.Code != http.StatusBadRequest {
		t.Errorf("no name: %d", rec.Code)
	}
}

func TestTRMNLsAPIAnswersAsDocumented(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	display := createDisplay(t, srv, cookie, "Workshop", printers.DriverTRMNL, "og", `{"type":"pull","address":"aa:bb:cc:dd:ee:ff"}`)

	// A device BAM! does not know is told so.
	rec := fetch(srv, http.MethodGet, "/api/setup", map[string]string{"ID": "11:22:33:44:55:66"})
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), `"status":404`) {
		t.Fatalf("unknown device: %d %s", rec.Code, rec.Body)
	}
	rec = fetch(srv, http.MethodGet, "/api/setup", map[string]string{"ID": "AA:BB:CC:DD:EE:FF", "FW-Version": "1.8.16", "Model": "og"})
	var setup struct {
		Status     int    `json:"status"`
		APIKey     string `json:"api_key"`
		FriendlyID string `json:"friendly_id"`
		ImageURL   string `json:"image_url"`
		Message    string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &setup); err != nil || rec.Code != http.StatusOK || setup.Status != 200 ||
		!strings.HasPrefix(setup.APIKey, store.DisplayTokenPrefix) || len(setup.FriendlyID) != 6 || setup.ImageURL == "" || setup.Message == "" {
		t.Fatalf("setup: %d %s", rec.Code, rec.Body)
	}
	// Set up once: again, it is refused until its tokens are revoked.
	if rec := fetch(srv, http.MethodGet, "/api/setup", map[string]string{"ID": "AA:BB:CC:DD:EE:FF"}); rec.Code != http.StatusConflict {
		t.Fatalf("setting up again: %d", rec.Code)
	}

	// What to show: TRMNL's fields, refresh_rate a number.
	var shown map[string]any
	for _, header := range []string{"Access-Token", "ACCESS_TOKEN"} {
		rec = fetch(srv, http.MethodGet, "/api/display", map[string]string{"ID": "AA:BB:CC:DD:EE:FF", header: setup.APIKey, "Battery-Voltage": "4.1", "RSSI": "-60"})
		shown = map[string]any{}
		if err := json.Unmarshal(rec.Body.Bytes(), &shown); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("display, %s: %d %s", header, rec.Code, rec.Body)
		}
	}
	filename, _ := shown["filename"].(string)
	if shown["status"] != float64(0) || shown["refresh_rate"] != float64(store.DefaultDisplayPullRefresh) || shown["special_function"] != "none" ||
		shown["reset_firmware"] != false || shown["update_firmware"] != false || filename == "" || len(filename) > 31 {
		t.Fatalf("display: %v", shown)
	}
	imageURL, _ := shown["image_url"].(string)
	path := imageURL[strings.Index(imageURL, "/api/"):]
	rec = fetch(srv, http.MethodGet, path, nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("image: %d %v", rec.Code, rec.Header())
	}
	if img, err := png.Decode(bytes.NewReader(rec.Body.Bytes())); err != nil || img.Bounds().Dx() != 800 || img.Bounds().Dy() != 480 || rec.Body.Len() > 90000 {
		t.Fatalf("image: %v, %d bytes", err, rec.Body.Len())
	}

	// Its file name changes when what it shows does.
	tpl := createTemplate(t, srv, cookie, panelTemplate("Dashboard", 800, 480, 125))
	a, _ := srv.store.CreateAsset("Laser cutter", nil, nil, nil)
	printTo(t, srv, cookie, display, tpl, a.Asset.ID)
	rec = fetch(srv, http.MethodGet, "/api/display", map[string]string{"Access-Token": setup.APIKey})
	var after map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &after)
	if after["filename"] == filename {
		t.Fatalf("the file name did not change: %v", after)
	}

	// Another device's key, or none, is refused.
	if rec := fetch(srv, http.MethodGet, "/api/display", map[string]string{"ID": "11:22:33:44:55:66", "Access-Token": setup.APIKey}); rec.Code != http.StatusForbidden {
		t.Errorf("another device: %d", rec.Code)
	}
	if rec := fetch(srv, http.MethodGet, "/api/display", map[string]string{"Access-Token": "bamd_nope"}); rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), `"status":401`) {
		t.Errorf("a bad key: %d %s", rec.Code, rec.Body)
	}
	// Its logs are taken, and not kept.
	req := httptest.NewRequest(http.MethodPost, "/api/log", strings.NewReader(`{"logs":[{"message":"woke"}]}`))
	req.Header.Set("Access-Token", setup.APIKey)
	req.Header.Set("Content-Type", "application/json")
	logged := httptest.NewRecorder()
	srv.Handler().ServeHTTP(logged, req)
	if logged.Code != http.StatusNoContent {
		t.Errorf("log: %d", logged.Code)
	}
}

// fakeAP is an OpenEPaperLink access point, keeping the images uploaded.
type fakeAP struct {
	srv  *httptest.Server
	mu   sync.Mutex
	tags []string
	jpgs [][]byte
}

func newFakeAP(t *testing.T) *fakeAP {
	t.Helper()
	ap := &fakeAP{}
	ap.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if r.URL.Path != "/imgupload" || err != nil {
			http.Error(w, "parameters incomplete", http.StatusBadRequest)
			return
		}
		form := multipart.NewReader(r.Body, params["boundary"])
		var tag string
		var img []byte
		for {
			part, err := form.NextPart()
			if err != nil {
				break
			}
			b, _ := io.ReadAll(part)
			switch part.FormName() {
			case "mac":
				tag = string(b)
			case "file":
				img = b
			}
		}
		ap.mu.Lock()
		ap.tags, ap.jpgs = append(ap.tags, tag), append(ap.jpgs, img)
		ap.mu.Unlock()
		_, _ = io.WriteString(w, "Ok, saved")
	}))
	t.Cleanup(ap.srv.Close)
	return ap
}

func (ap *fakeAP) uploads() int {
	ap.mu.Lock()
	defer ap.mu.Unlock()
	return len(ap.jpgs)
}

func TestAnAssetChangeRefreshesItsDisplaysOncePerBurst(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	ap := newFakeAP(t)
	display := createDisplay(t, srv, cookie, "Screws", printers.DriverOpenEPaperLink, "2.9in", `{"type":"openepaperlink","address":"`+ap.srv.URL+`?tag=0197E5CB3B38"}`)
	tpl := createTemplate(t, srv, cookie, panelTemplate("Bin", 296, 128, 112))

	// The screws are in a bin, whose category offers a hook that
	// refreshes displays when its condition changes.
	cat, err := srv.store.CreateCategory("Bins", nil, "", false, false)
	if err != nil {
		t.Fatal(err)
	}
	bin, _ := srv.store.CreateAsset("Bin A3", []string{cat.ID}, nil, nil)
	screws, _ := srv.store.CreateAsset("M3 screws", nil, nil, nil)
	types, err := srv.store.ListRelationTypes()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range types {
		if r.Name == "contains" {
			if _, err := srv.store.AddLink(bin.Asset.ID, r.ID, screws.Asset.ID, true); err != nil {
				t.Fatal(err)
			}
		}
	}
	// A hook that refreshes displays cannot fire on the labels its
	// refreshes print.
	loop := `{"key":"refresh","label":"Refresh","actions":["refresh_displays"],"triggers":[{"type":"event","kinds":["asset.label_printed"]}]}`
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/hooks", loop); rec.Code != http.StatusBadRequest {
		t.Fatalf("a hook firing itself: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/hooks", `{"key":"refresh","label":"Refresh","actions":["reboot"]}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("an unknown action: %d %s", rec.Code, rec.Body)
	}
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/hooks", `{"key":"refresh","label":"Refresh","actions":["refresh_displays"],"triggers":[{"type":"event","kinds":["asset.condition_changed"]}]}`)
	var saved struct{ Hook models.AssetHook }
	if err := json.Unmarshal(rec.Body.Bytes(), &saved); err != nil || rec.Code != http.StatusCreated || len(saved.Hook.Actions) != 1 {
		t.Fatalf("saving the hook: %d %s", rec.Code, rec.Body)
	}
	// Replaced without actions, it keeps them.
	rec = doJSON(t, srv, cookie, http.MethodPut, "/api/v1/hooks/"+saved.Hook.ID, `{"key":"refresh","label":"Refresh displays"}`)
	if err := json.Unmarshal(rec.Body.Bytes(), &saved); err != nil || len(saved.Hook.Actions) != 1 {
		t.Fatalf("replacing the hook: %d %s", rec.Code, rec.Body)
	}
	if _, err := srv.store.SetCategoryHooks(cat.ID, []models.CategoryHook{{HookID: saved.Hook.ID}}); err != nil {
		t.Fatal(err)
	}

	printTo(t, srv, cookie, display, tpl, screws.Asset.ID)
	if ap.uploads() != 1 || ap.tags[0] != "00000197E5CB3B38" {
		t.Fatalf("uploads %d, tags %v", ap.uploads(), ap.tags)
	}
	if cfg, err := jpeg.DecodeConfig(bytes.NewReader(ap.jpgs[0])); err != nil || cfg.Width != 296 || cfg.Height != 128 {
		t.Fatalf("pushed %+v, %v", cfg, err)
	}

	// A burst of changes to the bin, the screws' container.
	for _, c := range []string{models.ConditionNeedsMaintenance, models.ConditionOK, models.ConditionOutOfService} {
		if _, _, err := srv.store.SetAssetCondition(bin.Asset.ID, c, ""); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	if fired := srv.hookTriggerRound(now); fired != 3 {
		t.Fatalf("fired %d, want 3", fired)
	}
	// One refresh, waiting for the rest of the burst.
	if n := srv.refreshDueDisplays(now); n != 0 {
		t.Fatalf("refreshed %d before the window ended", n)
	}
	later := now.Add(time.Duration(store.DefaultDisplayRefreshWindow+1) * time.Second)
	if n := srv.refreshDueDisplays(later); n != 1 {
		t.Fatalf("refreshed %d, want 1", n)
	}
	// Once: not again, here or on another server.
	if n := srv.refreshDueDisplays(later); n != 0 {
		t.Fatalf("refreshed again: %d", n)
	}
	if n := NewServer(srv.cfg, srv.store).refreshDueDisplays(later); n != 0 {
		t.Fatalf("another server refreshed: %d", n)
	}
	// Through the print queue, as a job.
	srv.drainSpool(t.Context())
	if ap.uploads() != 2 {
		t.Fatalf("uploads %d, want 2", ap.uploads())
	}
	jobs, err := srv.store.ListSpoolJobs(time.Time{}, nil)
	if err != nil || len(jobs) != 1 || jobs[0].State != models.SpoolDone || jobs[0].Target != "Screws" {
		t.Fatalf("jobs %+v, %v", jobs, err)
	}

	// Refreshing by hand goes the same way.
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/displays/"+display+"/refresh", ""); rec.Code != http.StatusAccepted {
		t.Fatalf("refresh: %d %s", rec.Code, rec.Body)
	}
	srv.drainSpool(t.Context())
	if ap.uploads() != 3 {
		t.Fatalf("uploads %d, want 3", ap.uploads())
	}
}

func TestADisplayIsPushedThroughAWebhookChannel(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	var mu sync.Mutex
	var got []*http.Request
	var bodies [][]byte
	frame := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		got, bodies = append(got, r), append(bodies, b)
		mu.Unlock()
	}))
	defer frame.Close()

	// Only a webhook channel that is there.
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/printers", `{"name":"Frame","driver":"display","model":"7.5in-800x480","media":"7.5in-800x480","connection":{"type":"webhook","address":"nope"}}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("no channel: %d %s", rec.Code, rec.Body)
	}
	ch, err := srv.store.CreateChannel(models.NotificationChannel{Name: "Frame", Driver: "webhook", Enabled: true, Events: []string{},
		Options: map[string]string{"url": frame.URL + "/in", "secret": "s3cret", "allow_private_networks": "true"}})
	if err != nil {
		t.Fatal(err)
	}
	display := createDisplay(t, srv, cookie, "Frame", printers.DriverDisplay, "7.5in-800x480", `{"type":"webhook","address":"`+ch.ID+`"}`)
	tpl := createTemplate(t, srv, cookie, panelTemplate("Frame", 800, 480, 125))
	a, _ := srv.store.CreateAsset("Laser cutter", nil, nil, nil)
	printTo(t, srv, cookie, display, tpl, a.Asset.ID)

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0].Header.Get("Content-Type") != "image/png" || got[0].Header.Get("Bam-Event") != "display.image" || got[0].Header.Get("Bam-Display") != display || got[0].Header.Get("Bam-Signature") == "" {
		t.Fatalf("posted %d: %v", len(got), got)
	}
	if img, err := png.Decode(bytes.NewReader(bodies[0])); err != nil || img.Bounds().Dx() != 800 {
		t.Fatalf("image %v", err)
	}
}
