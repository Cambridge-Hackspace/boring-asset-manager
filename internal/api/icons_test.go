package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// requireIcons skips a test that needs the icon compiler when it is not
// built in, unless BAM_REQUIRE_ISOLEX makes that a failure (README §8.1).
func requireIcons(t *testing.T) {
	t.Helper()
	if !isolex.Default().Available() {
		if os.Getenv("BAM_REQUIRE_ISOLEX") != "" {
			t.Fatal("BAM_REQUIRE_ISOLEX is set, but the icon compiler is not built in")
		}
		t.Skip("the icon compiler is not built in (tools/isolex/fetch.sh)")
	}
}

// iconFuzzRoutes join the fuzzer's routes where the compiler is built
// in; without it every change to icons is a 503, which the fuzzer would
// take for a failure of the server.
var iconFuzzRoutes = []route{
	{method: "GET", path: "/api/v1/icons"},
	{method: "GET", path: "/api/v1/icons/ir"},
	{method: "GET", path: "/api/v1/icons/image", query: map[string][]string{"category": {"{category}"}, "format": {"png", "svg"}, "height": {"48", "1024"}}},
	{method: "GET", path: "/api/v1/icons/image", query: map[string][]string{"name": {"crate{n}", "bam-unknown"}, "theme": {"dark"}, "color": {"#123"}}},
	{method: "GET", path: "/api/v1/icons/palette"},
	{method: "PUT", path: "/api/v1/icons/palette", body: `{"palette":[{"token":"ink","light":"#111","dark":"#eee"},{"token":"outline","light":"#222","dark":"#ddd"},{"token":"body-top","light":"#eee","dark":"#777"},{"token":"body-left","light":"#ddd","dark":"#555"},{"token":"body-right","light":"#ccc","dark":"#444"},{"token":"shadow","light":"#000","dark":"#000"}]}`},
	{method: "GET", path: "/api/v1/icons/settings"},
	{method: "PUT", path: "/api/v1/icons/settings", body: `{"stroke_widths":[],"reserved":[],"limits":{"shape-limit":5000}}`},
	{method: "GET", path: "/api/v1/category-icons"},
	{method: "GET", path: "/api/v1/categories/{category}/icon"},
	{method: "PUT", path: "/api/v1/categories/{category}/icon", body: `{"icon_name":"crate{n}","source":"icon \"crate{n}\" { category \"c\"; footprint 1 1 1; part \"p\" { box 0 0 0 1 1 1 } }"}`},
	{method: "POST", path: "/api/v1/categories/{category}/icon/check", body: `{"icon_name":"crate{n}","source":"icon \"crate{n}\" { category \"c\"; canvas 9 9 }"}`},
	{method: "POST", path: "/api/v1/categories/{category}/icon/rename", body: `{"icon_name":"box{n}"}`},
	{method: "DELETE", path: "/api/v1/categories/{category}/icon/draft"},
	{method: "GET", path: "/api/v1/categories/{category}/icon/revisions"},
	{method: "DELETE", path: "/api/v1/categories/{category}/icon"},
	{method: "GET", path: "/api/v1/icon-library"},
	{method: "GET", path: "/api/v1/icons/export"},
	{method: "POST", path: "/api/v1/icon-library/import", kind: "zip"},
	{method: "POST", path: "/api/v1/icon-library", body: `{"path":"shared/f{n}.ilx","source":"fragment \"f{n}\" { part \"p\" { tile 0 0 0 1 1 } }"}`},
	{method: "GET", path: "/api/v1/icon-library/{library}"},
	{method: "PUT", path: "/api/v1/icon-library/{library}", body: `{"source":"fragment \"g{n}\" { part \"p\" { tile 0 0 0 1 1 } }"}`},
	{method: "POST", path: "/api/v1/icon-library/{library}/rename", body: `{"path":"moved/f{n}.ilx"}`},
	{method: "DELETE", path: "/api/v1/icon-library/{library}/draft"},
	{method: "GET", path: "/api/v1/icon-library/{library}/revisions"},
	{method: "DELETE", path: "/api/v1/icon-library/{library}"},
}

func init() {
	if isolex.Default().Available() {
		fuzzRoutes = append(fuzzRoutes, iconFuzzRoutes...)
	}
}

const crateSource = `icon "crate" { category "storage"; footprint 1 1 1; part "box" { box 0 0 0 1 1 1 } }`

func iconJSON(name, source string) string {
	b, _ := json.Marshal(map[string]string{"icon_name": name, "source": source})
	return string(b)
}

func decodeBody(t *testing.T, body []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(body, v); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
}

type saveAnswer struct {
	Icon models.CategoryIcon `json:"icon"`
	Save models.IconSave     `json:"save"`
}

func TestCategoryIconsThroughTheAPI(t *testing.T) {
	requireIcons(t)
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	cat, err := srv.store.CreateCategoryFrom(store.CategoryInput{Name: "Storage", ColorHex: "#336699"})
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/categories/" + cat.ID + "/icon"

	if rec := doJSON(t, srv, cookie, "GET", path, ""); rec.Code != http.StatusNotFound {
		t.Errorf("an icon before any: %d", rec.Code)
	}

	// a check keeps nothing
	rec := doJSON(t, srv, cookie, "POST", path+"/check", iconJSON("crate", crateSource))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"errors":[]`) || !strings.Contains(rec.Body.String(), `"name":"crate"`) {
		t.Fatalf("check: %d %s", rec.Code, rec.Body)
	}

	rec = doJSON(t, srv, cookie, "PUT", path, iconJSON("crate", crateSource))
	var saved saveAnswer
	decodeBody(t, rec.Body.Bytes(), &saved)
	if rec.Code != http.StatusOK || !saved.Save.Published || saved.Icon.Source == nil || saved.Save.Build == nil {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	hash := saved.Save.Build.Hash

	// a broken edit is a draft, answered 200 with its errors
	rec = doJSON(t, srv, cookie, "PUT", path, iconJSON("crate", `icon "crate" {`))
	decodeBody(t, rec.Body.Bytes(), &saved)
	if rec.Code != http.StatusOK || saved.Save.Published || len(saved.Save.Errors) == 0 || saved.Icon.Draft == nil {
		t.Fatalf("draft: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, cookie, "DELETE", path+"/draft", ""); rec.Code != http.StatusNoContent {
		t.Errorf("discard: %d %s", rec.Code, rec.Body)
	}

	// the build, and its IR by ETag
	rec = doJSON(t, srv, cookie, "GET", "/api/v1/icons", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"available":true`) || !strings.Contains(rec.Body.String(), `{"name":"crate","category":"storage"}`) {
		t.Errorf("overview: %d %s", rec.Code, rec.Body)
	}
	rec = doJSON(t, srv, cookie, "GET", "/api/v1/icons/ir", "")
	etag := rec.Header().Get("ETag")
	if rec.Code != http.StatusOK || etag != `"`+hash+`"` || !strings.Contains(rec.Body.String(), `"ir_version":1`) {
		t.Fatalf("ir: %d %q %.200s", rec.Code, etag, rec.Body)
	}
	req := httptest.NewRequest("GET", "/api/v1/icons/ir", nil)
	req.Header.Set("If-None-Match", etag)
	req.AddCookie(cookie)
	unchanged := httptest.NewRecorder()
	srv.Handler().ServeHTTP(unchanged, req)
	if unchanged.Code != http.StatusNotModified || unchanged.Body.Len() != 0 {
		t.Errorf("unchanged IR: %d", unchanged.Code)
	}

	if rec := doJSON(t, srv, cookie, "GET", path+"/revisions", ""); rec.Code != http.StatusOK || strings.Count(rec.Body.String(), `"source"`) != 1 {
		t.Errorf("revisions: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, cookie, "GET", "/api/v1/category-icons", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"icon_name":"crate"`) {
		t.Errorf("list: %d %s", rec.Code, rec.Body)
	}

	// an icon another extends cannot go, and neither can its category
	sub, _ := srv.store.CreateCategoryFrom(store.CategoryInput{Name: "Small storage", ParentID: &cat.ID})
	rec = doJSON(t, srv, cookie, "PUT", "/api/v1/categories/"+sub.ID+"/icon", iconJSON("small-crate", `icon "small-crate" { extends "crate" }`))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"published":true`) {
		t.Fatalf("child: %d %s", rec.Code, rec.Body)
	}
	for _, del := range []string{path, "/api/v1/categories/" + cat.ID} {
		rec = doJSON(t, srv, cookie, "DELETE", del, "")
		if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"file":"categories/small-crate.ilx"`) {
			t.Errorf("DELETE %s: %d %s", del, rec.Code, rec.Body)
		}
	}
	if _, err := srv.store.GetCategoryByID(cat.ID); err != nil {
		t.Error("a refused delete deleted the category")
	}
	if rec := doJSON(t, srv, cookie, "DELETE", "/api/v1/categories/"+sub.ID, ""); rec.Code != http.StatusOK {
		t.Fatalf("deleting the child: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, cookie, "DELETE", "/api/v1/categories/"+cat.ID, ""); rec.Code != http.StatusOK {
		t.Fatalf("deleting the parent: %d %s", rec.Code, rec.Body)
	}
	if b, _ := srv.store.CurrentIconBuild(); b == nil || strings.Contains(string(b.IR), "crate") {
		t.Error("the build still draws deleted icons")
	}
}

func TestIconPermissions(t *testing.T) {
	requireIcons(t)
	srv, _, _ := setupTestServer(t)
	cat, _ := srv.store.CreateCategoryFrom(store.CategoryInput{Name: "Storage"})
	reader := userWithPermissions(t, srv, "reader@test.org", rbac.PermAssetsRead)
	categories := userWithPermissions(t, srv, "cats@test.org", rbac.PermAssetsRead, rbac.PermCategoriesManage)
	icons := userWithPermissions(t, srv, "icons@test.org", rbac.PermAssetsRead, rbac.PermIconsManage)
	palette := `{"palette":[{"token":"ink","light":"#000","dark":"#fff"},{"token":"outline","light":"#000","dark":"#fff"},` +
		`{"token":"body-top","light":"#eee","dark":"#555"},{"token":"body-left","light":"#ddd","dark":"#444"},{"token":"body-right","light":"#ccc","dark":"#333"}]}`
	for _, c := range []struct {
		who          string
		cookie       *http.Cookie
		method, path string
		body         string
		want         int
	}{
		{"reader", reader, "GET", "/api/v1/icons/palette", "", 200},
		{"reader", reader, "GET", "/api/v1/icons/settings", "", 200},
		{"reader", reader, "PUT", "/api/v1/categories/" + cat.ID + "/icon", iconJSON("crate", crateSource), 403},
		{"reader", reader, "PUT", "/api/v1/icons/palette", palette, 403},
		{"categories", categories, "PUT", "/api/v1/categories/" + cat.ID + "/icon", iconJSON("crate", crateSource), 200},
		{"categories", categories, "PUT", "/api/v1/icons/palette", palette, 403},
		{"categories", categories, "POST", "/api/v1/icon-library", `{"path":"a.ilx","source":""}`, 403},
		{"icons", icons, "PUT", "/api/v1/categories/" + cat.ID + "/icon", iconJSON("crate", crateSource), 403},
		{"icons", icons, "PUT", "/api/v1/icons/palette", palette, 200},
		{"icons", icons, "POST", "/api/v1/icon-library", `{"path":"a.ilx","source":"// nothing yet"}`, 201},
		{"icons", icons, "PUT", "/api/v1/icons/settings", `{"stroke_widths":[1.2,1.5],"reserved":[],"limits":{"shape-limit":1000}}`, 200},
	} {
		if rec := doJSON(t, srv, c.cookie, c.method, c.path, c.body); rec.Code != c.want {
			t.Errorf("%s %s %s: %d, want %d: %s", c.who, c.method, c.path, rec.Code, c.want, rec.Body)
		}
	}
	// a palette without a token an icon paints with is refused, saying where
	rec := doJSON(t, srv, icons, "PUT", "/api/v1/icons/palette", `{"palette":[{"token":"ink","light":"#000","dark":"#fff"}]}`)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"file":"categories/crate.ilx"`) {
		t.Errorf("a palette that breaks an icon: %d %s", rec.Code, rec.Body)
	}
}

// A source as long as allowed, in four-byte characters, is over the
// usual request limit: icon routes take it.
func TestALongSourceFitsTheRequestLimit(t *testing.T) {
	requireIcons(t)
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	cat, _ := srv.store.CreateCategoryFrom(store.CategoryInput{Name: "Storage"})
	comment := "// " + strings.Repeat("𝐱", store.MaxIconSource-4) + "\n"
	body := iconJSON("crate", comment)
	if len(body) <= MaxRequestBytes {
		t.Fatalf("the body is %d bytes, not over the usual limit", len(body))
	}
	rec := doJSON(t, srv, cookie, "PUT", "/api/v1/categories/"+cat.ID+"/icon", body)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"published":false`) {
		t.Errorf("%d %.300s", rec.Code, rec.Body)
	}
	rec = doJSON(t, srv, cookie, "PUT", "/api/v1/categories/"+cat.ID+"/icon", iconJSON("crate", comment+"xx"))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("one character over: %d %.200s", rec.Code, rec.Body)
	}
}

// unavailable is a server built without the compiler.
type unavailable struct{}

func (unavailable) Available() bool { return false }
func (unavailable) Version() (string, error) {
	return "", isolex.ErrUnavailable
}
func (unavailable) Compile(context.Context, []isolex.File) (*isolex.Result, error) {
	return nil, isolex.ErrUnavailable
}

// Without the compiler icons cannot change, and are no reason to keep a
// category from being deleted.
func TestWithoutTheCompilerIconsStayAsTheyAre(t *testing.T) {
	requireIcons(t)
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	cat, _ := srv.store.CreateCategoryFrom(store.CategoryInput{Name: "Storage"})
	if rec := doJSON(t, srv, cookie, "PUT", "/api/v1/categories/"+cat.ID+"/icon", iconJSON("crate", crateSource)); rec.Code != http.StatusOK {
		t.Fatal(rec.Body)
	}
	srv.icons = unavailable{}
	srv.RunIcons(context.Background())
	if rec := doJSON(t, srv, cookie, "PUT", "/api/v1/categories/"+cat.ID+"/icon", iconJSON("crate", crateSource)); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("a save without the compiler: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, cookie, "GET", "/api/v1/icons", ""); !strings.Contains(rec.Body.String(), `"available":false`) {
		t.Errorf("%s", rec.Body)
	}
	if rec := doJSON(t, srv, cookie, "DELETE", "/api/v1/categories/"+cat.ID, ""); rec.Code != http.StatusOK {
		t.Errorf("deleting a category with an icon: %d %s", rec.Code, rec.Body)
	}
}

// At startup the server builds what is published if it has no build of
// it, as after upgrading isolex.
func TestRunIconsBuildsAtStartup(t *testing.T) {
	requireIcons(t)
	srv, _, _ := setupTestServer(t)
	cat, _ := srv.store.CreateCategoryFrom(store.CategoryInput{Name: "Storage"})
	if _, err := srv.store.SaveCategoryIcon(context.Background(), srv.icons, cat.ID, "crate", crateSource); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.store.Exec(`DELETE FROM icon_build`); err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		srv.RunIcons(ctx)
		close(done)
	}()
	for deadline := time.Now().Add(20 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		b, err := srv.store.CurrentIconBuild()
		if err == nil && b != nil && strings.Contains(string(b.IR), `"crate"`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("after startup: %+v %v", b, err)
		}
	}
	// it runs until stopped
	stop()
	<-done
}

// An assistant writes icons, and is told what the compiler said.
func TestAnAssistantWritesIcons(t *testing.T) {
	requireIcons(t)
	srv, admin, _ := setupTestServer(t)
	cat, _ := srv.store.CreateCategoryFrom(store.CategoryInput{Name: "Storage"})
	cs := mcpSession(t, srv, tokenWith(t, srv, admin.ID, "assistant", rbac.PermAssetsRead, rbac.PermCategoriesManage))

	res := callTool(t, cs, "save_category_icon", map[string]any{"category_id": cat.ID, "icon_name": "crate", "source": strings.Replace(crateSource, "1 1 1 }", "1 1 }", 1)})
	if text := toolText(res); !strings.Contains(text, "kept as a draft") || !strings.Contains(text, "categories/crate.ilx:1:") {
		t.Errorf("a broken save: %s", text)
	}
	if text := toolText(callTool(t, cs, "check_category_icon", map[string]any{"category_id": cat.ID, "icon_name": "crate", "source": crateSource})); text != "It compiles." {
		t.Errorf("check: %s", text)
	}
	if text := toolText(callTool(t, cs, "save_category_icon", map[string]any{"category_id": cat.ID, "icon_name": "crate", "source": crateSource})); text != `"crate" is published.` {
		t.Errorf("save: %s", text)
	}
	if text := toolText(callTool(t, cs, "icons_overview", map[string]any{})); !strings.Contains(text, "1 icons built.") {
		t.Errorf("overview: %s", text)
	}
	if text := toolText(callTool(t, cs, "get_icon_ir", map[string]any{"name": "crate"})); !strings.Contains(text, `The IR of "crate"`) {
		t.Errorf("ir: %s", text)
	}
	// the palette is icons.manage, which this assistant has not: it is
	// not offered the tool
	for _, name := range toolNames(t, cs) {
		if name == "set_icon_palette" || name == "save_icon_library_file" {
			t.Errorf("offered %s without icons.manage", name)
		}
	}
}
