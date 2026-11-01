package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

func TestHooksThroughTheAPI(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	adminCookie := sessionCookie(t, srv, admin.ID)
	st := srv.store
	categorizer := userWithPermissions(t, srv, "cat@test.org", rbac.PermAssetsRead, rbac.PermCategoriesManage)
	reader := userWithPermissions(t, srv, "reader@test.org", rbac.PermAssetsRead)
	member := userWithPermissions(t, srv, "member@test.org", rbac.PermAssetsRead, rbac.PermAssetsHook, rbac.PermAssetsScan)
	expect := func(what string, cookie *http.Cookie, method, path, body string, want int) string {
		t.Helper()
		rec := doJSON(t, srv, cookie, method, path, body)
		if rec.Code != want {
			t.Errorf("%s: %d, want %d: %s", what, rec.Code, want, rec.Body)
		}
		return rec.Body.String()
	}

	var saved struct{ Hook models.AssetHook }
	_ = json.Unmarshal([]byte(expect("defining one", categorizer, http.MethodPost, "/api/v1/hooks", `{"key":"identify","label":"Identify"}`, http.StatusCreated)), &saved)
	expect("defining one as a reader", reader, http.MethodPost, "/api/v1/hooks", `{"key":"x","label":"X"}`, http.StatusForbidden)
	expect("a bad key", categorizer, http.MethodPost, "/api/v1/hooks", `{"key":"Light Up","label":"X"}`, http.StatusBadRequest)
	expect("a taken key", categorizer, http.MethodPost, "/api/v1/hooks", `{"key":"identify","label":"X"}`, http.StatusConflict)
	expect("changing it", categorizer, http.MethodPut, "/api/v1/hooks/"+saved.Hook.ID, `{"key":"identify","label":"Identify","confirm":"Light it up?"}`, http.StatusOK)
	expect("changing a missing one", categorizer, http.MethodPut, "/api/v1/hooks/nope", `{"key":"y","label":"Y"}`, http.StatusNotFound)
	if body := expect("listing as a reader", reader, http.MethodGet, "/api/v1/hooks", "", http.StatusOK); !strings.Contains(body, `"confirm":"Light it up?"`) {
		t.Errorf("listing: %s", body)
	}

	parts, _ := st.CreateCategory("Parts", nil, "", false, false)
	resistors, _ := st.CreateCategory("Resistors", &parts.ID, "", false, false)
	expect("offering it", categorizer, http.MethodPut, "/api/v1/categories/"+parts.ID+"/hooks", `{"hooks":[{"hook_id":"`+saved.Hook.ID+`","cascades":true}]}`, http.StatusOK)
	expect("offering it as a reader", reader, http.MethodPut, "/api/v1/categories/"+parts.ID+"/hooks", `{"hooks":[]}`, http.StatusForbidden)
	expect("offering a missing one", categorizer, http.MethodPut, "/api/v1/categories/"+parts.ID+"/hooks", `{"hooks":[{"hook_id":"nope"}]}`, http.StatusNotFound)
	expect("offering on a missing category", categorizer, http.MethodPut, "/api/v1/categories/nope/hooks", `{"hooks":[]}`, http.StatusNotFound)
	if body := expect("what it offers", reader, http.MethodGet, "/api/v1/categories/"+parts.ID+"/hooks", "", http.StatusOK); !strings.Contains(body, `"cascades":true`) {
		t.Errorf("what it offers: %s", body)
	}

	tenK, _ := st.CreateAsset("10k", []string{resistors.ID}, nil, nil)
	plain, _ := st.CreateAsset("Rag", nil, nil, nil)
	if body := expect("the asset", reader, http.MethodGet, "/api/v1/assets/"+tenK.Asset.ID, "", http.StatusOK); !strings.Contains(body, `"hooks":[{"id":"`+saved.Hook.ID+`","key":"identify","label":"Identify","confirm":"Light it up?"}]`) {
		t.Errorf("the asset's hooks: %s", body)
	}

	press := "/api/v1/assets/" + tenK.Asset.ID + "/hooks/identify"
	expect("pressing as a reader", reader, http.MethodPost, press, "", http.StatusForbidden)
	expect("pressing signed out", nil, http.MethodPost, press, "", http.StatusUnauthorized)
	body := expect("pressing", member, http.MethodPost, press, "", http.StatusCreated)
	var pressed struct {
		EventID string `json:"event_id"`
	}
	_ = json.Unmarshal([]byte(body), &pressed)
	events, _, _ := st.ListAssetEvents(tenK.Asset.ID, "", 1)
	if len(events) != 1 || events[0].ID != pressed.EventID || events[0].Payload.Hook.Key != "identify" {
		t.Errorf("the event: %+v", events)
	}
	expect("pressing again at once", member, http.MethodPost, press, "", http.StatusTooManyRequests)
	expect("a hook not offered", member, http.MethodPost, "/api/v1/assets/"+plain.Asset.ID+"/hooks/identify", "", http.StatusNotFound)
	expect("an unknown hook", member, http.MethodPost, "/api/v1/assets/"+tenK.Asset.ID+"/hooks/nope", "", http.StatusNotFound)
	expect("a missing asset", member, http.MethodPost, "/api/v1/assets/nope/hooks/identify", "", http.StatusNotFound)

	// The scan page offers them to those who may press them.
	if _, err := st.CreateScanTemplate("Bins", []byte(`{"sections":[{"type":"actions","actions":["open","hooks"]}]}`)); err != nil {
		t.Fatal(err)
	}
	reports, _ := st.ListReports()
	for _, r := range reports {
		if r.Name == "Bins" {
			if err := st.SetDefaultScanTemplate(r.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	scan := "/api/v1/scan/" + tenK.Asset.ID
	if body := expect("scanning as a member", member, http.MethodGet, scan, "", http.StatusOK); !strings.Contains(body, `"actions":["open","hooks"]`) || !strings.Contains(body, `"key":"identify"`) {
		t.Errorf("scanning as a member: %s", body)
	}
	scanner := userWithPermissions(t, srv, "scanner@test.org", rbac.PermAssetsScan)
	if body := expect("scanning without assets.hook", scanner, http.MethodGet, scan, "", http.StatusOK); strings.Contains(body, `"identify"`) || strings.Contains(body, `"actions":["open","hooks"]`) {
		t.Errorf("scanning without assets.hook: %s", body)
	}

	// The routes hooks had when they were actions are gone (#173), even to
	// the admin, while the hook they named is still there.
	for _, old := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/actions", ""},
		{http.MethodPost, "/api/v1/actions", `{"key":"wave","label":"Wave"}`},
		{http.MethodPut, "/api/v1/actions/" + saved.Hook.ID, `{"key":"identify","label":"Identify"}`},
		{http.MethodDelete, "/api/v1/actions/" + saved.Hook.ID, ""},
		{http.MethodGet, "/api/v1/categories/" + parts.ID + "/actions", ""},
		{http.MethodPut, "/api/v1/categories/" + parts.ID + "/actions", `{"actions":[]}`},
		{http.MethodPost, "/api/v1/assets/" + tenK.Asset.ID + "/actions/identify", ""},
		{http.MethodGet, "/api/v1/assets/" + tenK.Asset.ID + "/actions/replies?event_id=" + pressed.EventID, ""},
	} {
		expect(old.method+" "+old.path, adminCookie, old.method, old.path, old.body, http.StatusNotFound)
	}
	// while a route there by another method is still not allowed
	expect("a method the route has not", adminCookie, http.MethodPatch, "/api/v1/hooks", "", http.StatusMethodNotAllowed)
	if body := expect("what it offers, after", reader, http.MethodGet, "/api/v1/categories/"+parts.ID+"/hooks", "", http.StatusOK); !strings.Contains(body, saved.Hook.ID) {
		t.Errorf("the old routes changed the offer: %s", body)
	}

	expect("deleting it", categorizer, http.MethodDelete, "/api/v1/hooks/"+saved.Hook.ID, "", http.StatusOK)
	expect("deleting it again", categorizer, http.MethodDelete, "/api/v1/hooks/"+saved.Hook.ID, "", http.StatusNotFound)
}

// TestHooksInScopes checks assets.hook counts only in the scopes it is
// held in.
func TestHooksInScopes(t *testing.T) {
	srv, _, _ := setupTestServer(t)
	st := srv.store
	tools, _ := st.CreateScope("tools")
	toolsCat, _ := st.CreateCategory("Tools", nil, "", false, false)
	if _, err := st.SetCategoryScopes(toolsCat.ID, []string{tools.ID}); err != nil {
		t.Fatal(err)
	}
	identify, _ := st.SaveHook("", store.HookInput{Key: "identify", Label: "Identify"})
	kitchen, _ := st.CreateCategory("Kitchen", nil, "", false, false)
	for _, c := range []string{toolsCat.ID, kitchen.ID} {
		if _, err := st.SetCategoryHooks(c, []models.CategoryHook{{HookID: identify.ID}}); err != nil {
			t.Fatal(err)
		}
	}
	drill, _ := st.CreateAsset("Drill", []string{toolsCat.ID}, nil, nil)
	kettle, _ := st.CreateAsset("Kettle", []string{kitchen.ID}, nil, nil)
	role, err := st.CreateRole(store.RoleInput{Name: "Tool keepers", Permissions: []string{rbac.PermAssetsRead, "tools:" + rbac.PermAssetsHook}})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := st.CreateNamedUser("keeper@test.org", "Keeper", "UserSecret123!", true)
	_ = st.SetUserRoles(u.ID, []string{role.ID})
	keeper := sessionCookie(t, srv, u.ID)
	if rec := doJSON(t, srv, keeper, http.MethodPost, "/api/v1/assets/"+drill.Asset.ID+"/hooks/identify", ""); rec.Code != http.StatusCreated {
		t.Errorf("in the tools scope: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, keeper, http.MethodPost, "/api/v1/assets/"+kettle.Asset.ID+"/hooks/identify", ""); rec.Code != http.StatusForbidden {
		t.Errorf("outside it: %d %s", rec.Code, rec.Body)
	}
}

// TestPressLimits checks one person presses an asset's hooks at most
// once every couple of seconds, and only so many times a minute.
func TestPressLimits(t *testing.T) {
	l := newPressLimiter()
	now := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return now }
	if wait := l.allow("ada", "drawer"); wait != 0 {
		t.Fatalf("the first press waits %v", wait)
	}
	if wait := l.allow("ada", "drawer"); wait != pressGap {
		t.Errorf("at once again: wait %v", wait)
	}
	if wait := l.allow("bo", "drawer"); wait != 0 {
		t.Errorf("someone else waits %v", wait)
	}
	now = now.Add(pressGap)
	if wait := l.allow("ada", "drawer"); wait != 0 {
		t.Errorf("after the gap: wait %v", wait)
	}
	// Many drawers, quickly: the burst runs out.
	for i := 0; i < pressBurst-2; i++ {
		if wait := l.allow("ada", "drawer-"+string(rune('a'+i))); wait != 0 {
			t.Fatalf("press %d waits %v", i, wait)
		}
	}
	if wait := l.allow("ada", "another"); wait <= 0 {
		t.Error("more than a burst a minute is allowed")
	}
	now = now.Add(pressWindow)
	if wait := l.allow("ada", "another"); wait != 0 {
		t.Errorf("a minute later: wait %v", wait)
	}
}
