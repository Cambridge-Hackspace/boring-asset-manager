package api

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

type testManifest struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	ShortName       string `json:"short_name"`
	StartURL        string `json:"start_url"`
	Scope           string `json:"scope"`
	Display         string `json:"display"`
	ThemeColor      string `json:"theme_color"`
	BackgroundColor string `json:"background_color"`
	Icons           []struct {
		Src, Sizes, Type, Purpose string
	} `json:"icons"`
	Shortcuts []struct {
		Name, URL string
	} `json:"shortcuts"`
}

func manifestOf(t *testing.T, srv *Server) testManifest {
	t.Helper()
	rec := doJSON(t, srv, nil, http.MethodGet, "/manifest.webmanifest", "")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/manifest+json" || rec.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("manifest: %d %v", rec.Code, rec.Header())
	}
	var m testManifest
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("manifest is not JSON: %v\n%s", err, rec.Body)
	}
	return m
}

// iconAt fetches an icon as a phone would, signed out.
func iconAt(t *testing.T, srv *Server, src string) image.Image {
	t.Helper()
	rec := doJSON(t, srv, nil, http.MethodGet, src, "")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("%s: %d %s", src, rec.Code, rec.Header().Get("Content-Type"))
	}
	img, err := png.Decode(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("%s: %v", src, err)
	}
	return img
}

// TestManifestIsValidAndItsIconsExist is the manifest a phone installs BAM!
// from: what installing needs, in BAM!'s name and colors until an admin
// gives others, pointing at icons that exist at the sizes it says.
func TestManifestIsValidAndItsIconsExist(t *testing.T) {
	srv, _, _ := setupTestServer(t)
	m := manifestOf(t, srv)
	if m.ID != "/" || m.StartURL != "/" || m.Scope != "/" || m.Display != "standalone" || m.Name != "BAM!" || m.ShortName != "BAM!" ||
		m.ThemeColor != defaultAppTheme || m.BackgroundColor != defaultAppBackground {
		t.Errorf("manifest %+v", m)
	}
	// Chrome installs an app with a 192 and a 512 px icon; launchers that
	// cut icons to shape take the maskable ones.
	have := map[string]bool{}
	for _, ic := range m.Icons {
		have[ic.Sizes+" "+ic.Purpose] = true
		img := iconAt(t, srv, ic.Src)
		if want := strings.Split(ic.Sizes, "x")[0]; ic.Type != "image/png" || strconv.Itoa(img.Bounds().Dx()) != want || img.Bounds().Dx() != img.Bounds().Dy() {
			t.Errorf("%s: %s %s, drawn %v", ic.Src, ic.Type, ic.Sizes, img.Bounds())
		}
	}
	for _, want := range []string{"192x192 any", "512x512 any", "192x192 maskable", "512x512 maskable"} {
		if !have[want] {
			t.Errorf("no %s icon among %v", want, have)
		}
	}
	var names []string
	for _, sc := range m.Shortcuts {
		names = append(names, sc.Name+" "+sc.URL)
	}
	if strings.Join(names, ", ") != "Scan /scan, Shopping list /shopping, Loans /loans" {
		t.Errorf("shortcuts %v", names)
	}
	// iOS reads its icon from the page's apple-touch-icon instead.
	if img := iconAt(t, srv, "/app-icons/apple-touch-icon.png"); img.Bounds().Dx() != 180 {
		t.Errorf("apple-touch-icon %v", img.Bounds())
	}
	if rec := doJSON(t, srv, nil, http.MethodGet, "/app-icons/nothing.png", ""); rec.Code != http.StatusNotFound {
		t.Errorf("an icon not listed: %d", rec.Code)
	}
	// Unchanged, it is not sent again.
	first := doJSON(t, srv, nil, http.MethodGet, m.Icons[0].Src, "")
	req := httptest.NewRequest(http.MethodGet, m.Icons[0].Src, nil)
	req.Header.Set("If-None-Match", first.Header().Get("ETag"))
	again := httptest.NewRecorder()
	srv.Handler().ServeHTTP(again, req)
	if first.Header().Get("ETag") == "" || again.Code != http.StatusNotModified {
		t.Errorf("ETag %q, then %d", first.Header().Get("ETag"), again.Code)
	}
}

// TestAppSettings are the installed app's name, colors and icon: read by
// anyone, set by those who manage settings, the name following the
// instance's until one is given.
func TestAppSettings(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	rec := doJSON(t, srv, nil, http.MethodGet, "/api/v1/settings/app", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"","short_name":"","theme_color":"#faf7f0","theme_color_dark":"#0b0b0a","background_color":"#faf7f0","icon_image_id":"","instance_name":"BAM!"`) {
		t.Errorf("defaults, signed out: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/settings/documents", `{"instance_name":"Cambridge Hackspace","page_size":"a4"}`); rec.Code != http.StatusOK {
		t.Fatal(rec.Body)
	}
	if m := manifestOf(t, srv); m.Name != "Cambridge Hackspace" || m.ShortName != "Cambridge Hackspace" {
		t.Errorf("following the instance's name: %q %q", m.Name, m.ShortName)
	}

	good := `{"name":" Hackspace stores ","short_name":"Stores","theme_color":"#D41920","theme_color_dark":"#141413","background_color":"#ffffff","icon_image_id":""}`
	if rec := doJSON(t, srv, nil, http.MethodPut, "/api/v1/settings/app", good); rec.Code != http.StatusUnauthorized {
		t.Errorf("signed out: %d", rec.Code)
	}
	for body, want := range map[string]int{
		strings.Replace(good, `"#D41920"`, `"red"`, 1):                                    http.StatusBadRequest,
		strings.Replace(good, `"#141413"`, `"#14141"`, 1):                                 http.StatusBadRequest,
		strings.Replace(good, `"Stores"`, `"`+strings.Repeat("s", 31)+`"`, 1):             http.StatusBadRequest,
		strings.Replace(good, `"icon_image_id":""`, `"icon_image_id":"no-such-image"`, 1): http.StatusBadRequest,
		good: http.StatusOK,
	} {
		if rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/settings/app", body); rec.Code != want {
			t.Errorf("%s: %d %s", body, rec.Code, rec.Body)
		}
	}
	m := manifestOf(t, srv)
	if m.Name != "Hackspace stores" || m.ShortName != "Stores" || m.ThemeColor != "#d41920" || m.BackgroundColor != "#ffffff" {
		t.Errorf("saved: %+v", m)
	}
}

// TestAppIconFromALogo draws the icons from a logo uploaded for them, which
// stays in the library while it is the icon, and from BAM!'s again if it
// cannot be read.
func TestAppIconFromALogo(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	before := manifestOf(t, srv).Icons[0].Src

	// A solid blue logo.
	logo := image.NewRGBA(image.Rect(0, 0, 64, 32))
	blue := color.RGBA{0x1e, 0x3a, 0x8a, 0xff}
	for y := 0; y < 32; y++ {
		for x := 0; x < 64; x++ {
			logo.Set(x, y, blue)
		}
	}
	var data, body bytes.Buffer
	if err := png.Encode(&data, logo); err != nil {
		t.Fatal(err)
	}
	mw := multipart.NewWriter(&body)
	part, _ := mw.CreateFormFile("file", "logo.png")
	_, _ = part.Write(data.Bytes())
	_ = mw.Close()
	rec := do(t, srv, cookie, http.MethodPost, "/api/v1/settings/app/icon", mw.FormDataContentType(), &body)
	var got AppSettings
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil || got.IconImageID == "" {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}

	m := manifestOf(t, srv)
	if m.Icons[0].Src == before {
		t.Errorf("the icons' address did not change: %s", before)
	}
	img := iconAt(t, srv, m.Icons[0].Src)
	if r, g, b, _ := img.At(96, 96).RGBA(); r>>8 != 0x1e || g>>8 != 0x3a || b>>8 != 0x8a {
		t.Errorf("middle %v, not the logo", img.At(96, 96))
	}
	if r, _, _, _ := img.At(0, 0).RGBA(); r>>8 != 0xfa {
		t.Errorf("corner %v, not the background", img.At(0, 0))
	}

	if rec := doJSON(t, srv, cookie, http.MethodDelete, "/api/v1/images/"+got.IconImageID, ""); rec.Code != http.StatusConflict {
		t.Errorf("deleting the icon's image: %d %s", rec.Code, rec.Body)
	}

	// Its file gone, BAM!'s burst is drawn instead.
	if err := srv.images.Remove(t.Context(), got.IconImageID); err != nil {
		t.Fatal(err)
	}
	srv.appIcons.drawn = nil
	img = iconAt(t, srv, m.Icons[0].Src)
	if r, g, b, _ := img.At(96, 96).RGBA(); (r>>8 == 0x1e && g>>8 == 0x3a && b>>8 == 0x8a) || (r>>8 == 0xfa && g>>8 == 0xf7) {
		t.Errorf("fallback %v, not BAM!'s burst", img.At(96, 96))
	}
}

// TestServiceWorkerLeavesTheServersPathsAlone checks the service worker's
// list of the paths the server answers itself (web/src/lib/install/shell.ts)
// against the server's routes outside the API: a page it answered from the
// app's shell instead, such as an OAuth sign-in, would never reach the server.
func TestServiceWorkerLeavesTheServersPathsAlone(t *testing.T) {
	src, err := os.ReadFile("../../web/src/lib/install/shell.ts")
	if err != nil {
		t.Fatal(err)
	}
	list := regexp.MustCompile(`(?s)export const serverPaths = \[(.*?)\];`).FindSubmatch(src)
	if list == nil {
		t.Fatal("no serverPaths in shell.ts")
	}
	var paths []string
	for _, m := range regexp.MustCompile(`'([^']+)'`).FindAllSubmatch(list[1], -1) {
		paths = append(paths, string(m[1]))
	}
	covered := func(path string) bool {
		for _, p := range paths {
			if path == p || (strings.HasSuffix(p, "/") && strings.HasPrefix(path, p)) || strings.HasPrefix(path, p+"/") {
				return true
			}
		}
		return false
	}
	srv, _, _ := setupTestServer(t)
	others := []string{"/manifest.webmanifest", "/app-icons/icon-192.png", "/api/v1/mcp", "/sw.js"}
	for _, route := range srv.routes {
		_, path, _ := strings.Cut(route, " ")
		others = append(others, path)
	}
	for _, path := range others {
		if !covered(path) {
			t.Errorf("%s is the server's, but the service worker would answer it with the app's shell", path)
		}
	}
}

// TestAppSettingsOverMCP sets only what is given, keeping the rest.
func TestAppSettingsOverMCP(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cs := mcpSession(t, srv, tokenWith(t, srv, admin.ID, "Settings", rbac.PermSettingsManage))
	callTool(t, cs, "set_app_settings", map[string]any{"short_name": "Stores", "theme_color": "#D41920"})
	a, _, err := srv.appSettings()
	if err != nil || a.ShortName != "Stores" || a.ThemeColor != "#d41920" || a.ThemeColorDark != defaultAppThemeDark || a.Name != "" {
		t.Errorf("%+v %v", a, err)
	}
	res := callTool(t, cs, "get_app_settings", map[string]any{})
	if text := toolText(res); res.IsError || !strings.Contains(text, `short name "Stores"`) {
		t.Errorf("get_app_settings: %s", text)
	}
}
