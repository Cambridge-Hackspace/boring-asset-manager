package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// kioskShop is a shelf in the public scope and a cupboard in a locked one,
// for a Search flow over both (#397).
type kioskShop struct {
	srv             *Server
	admin           *http.Cookie
	shelf, cupboard string
}

func newKioskShop(t *testing.T) *kioskShop {
	t.Helper()
	srv, admin, _ := setupTestServer(t)
	st := srv.store
	k := &kioskShop{srv: srv, admin: sessionCookie(t, srv, admin.ID)}
	scopes, _ := st.ListScopes()
	var public string
	for _, sc := range scopes {
		if sc.Name == rbac.PublicScope {
			public = sc.ID
		}
	}
	locked, _ := st.CreateScope("locked")
	shelf, _ := st.CreateCategory("Shelf", nil, "", false, false)
	cupboard, _ := st.CreateCategory("Cupboard", nil, "", false, false)
	if _, err := st.SetCategoryScopes(shelf.ID, []string{public}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetCategoryScopes(cupboard.ID, []string{locked.ID}); err != nil {
		t.Fatal(err)
	}
	k.shelf, k.cupboard = shelf.ID, cupboard.ID

	torch, _ := st.CreateAsset("Torch", []string{shelf.ID}, nil, nil)
	broken, _ := st.CreateAsset("Broken torch", []string{shelf.ID}, nil, nil)
	if _, _, err := st.SetAssetCondition(broken.Asset.ID, "out_of_service", ""); err != nil {
		t.Fatal(err)
	}
	spare, _ := st.CreateAsset("Spare torch", []string{shelf.ID}, nil, nil)
	if rec := doJSON(t, srv, k.admin, http.MethodDelete, "/api/v1/assets/"+spare.Asset.ID, ""); rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
		t.Fatalf("archiving the spare: %d %s", rec.Code, rec.Body)
	}
	// The safe is in the locked cupboard, and holds a torch of its own on
	// the shelf: signed out, neither the safe nor where that torch is shows.
	safe, _ := st.CreateAsset("Safe", []string{cupboard.ID}, nil, nil)
	inSafe, _ := st.CreateAsset("Head torch", []string{shelf.ID}, nil, nil)
	if _, err := st.AddLink(safe.Asset.ID, containsType, inSafe.Asset.ID, true); err != nil {
		t.Fatal(err)
	}
	_ = torch
	return k
}

// save saves a flow as the admin, answering with the response body.
func (k *kioskShop) save(t *testing.T, slug string, definition map[string]any, want int) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"slug": slug, "name": "Find it", "definition": definition})
	rec := doJSON(t, k.srv, k.admin, http.MethodPost, "/api/v1/flows", string(body))
	if rec.Code != want {
		t.Fatalf("saving %s: %d, want %d: %s", slug, rec.Code, want, rec.Body)
	}
	return rec.Body.String()
}

func searchStep(extra map[string]any) map[string]any {
	step := map[string]any{"id": "find", "kind": "search", "title": "Find something", "filter": "condition = ok", "columns": []string{"where"}, "reset_after": 90}
	for k, v := range extra {
		step[k] = v
	}
	return step
}

// kioskNames searches a Search flow, answering with the names found.
func kioskNames(t *testing.T, srv *Server, cookie *http.Cookie, slug, search string) []string {
	t.Helper()
	rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/flows/"+slug+"/search?search="+url.QueryEscape(search), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("searching %s for %q: %d %s", slug, search, rec.Code, rec.Body)
	}
	var resp struct {
		Assets []struct {
			Asset    struct{ Name string }   `json:"asset"`
			Location []struct{ Name string } `json:"location"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, a := range resp.Assets {
		name := a.Asset.Name
		for _, p := range a.Location {
			name += " in " + p.Name
		}
		names = append(names, name)
	}
	return names
}

// TestAPublicSearchFlowFindsOnlyWhatThePublicMayRead opens a kiosk's
// Search flow signed out: it finds, through the flow's categories and its
// filter, only the active assets the public role may read, and nothing
// hidden from it shows or finds anything.
func TestAPublicSearchFlowFindsOnlyWhatThePublicMayRead(t *testing.T) {
	k := newKioskShop(t)
	srv := k.srv
	k.save(t, "kiosk", map[string]any{"version": 2, "public": true, "category_ids": []string{k.shelf, k.cupboard}, "steps": []any{searchStep(nil)}}, http.StatusCreated)

	// Opened with nothing searched, it says what it is and lists nothing.
	rec := doJSON(t, srv, nil, http.MethodGet, "/api/v1/flows/kiosk/search", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"assets":[]`) || !strings.Contains(rec.Body.String(), `"reset_after":90`) ||
		!strings.Contains(rec.Body.String(), `"columns":["where"]`) || !strings.Contains(rec.Body.String(), `"title":"Find something"`) {
		t.Fatalf("opened: %d %s", rec.Code, rec.Body)
	}

	for search, want := range map[string]string{
		// Not the broken one (the filter), the archived spare, nor where
		// the head torch is.
		"torch": "Head torch,Torch",
		// Nothing in the locked cupboard, nor what is in it by its name.
		"safe": "",
		// A query, joined to the filter.
		`name ~ head`: "Head torch",
		// The filter can't be undone by what is typed.
		`condition = out_of_service`: "",
		// Nor can a query reach archived assets.
		`archived = true`: "",
	} {
		if got := strings.Join(kioskNames(t, srv, nil, "kiosk", search), ","); got != want {
			t.Errorf("signed out, %q found %q, want %q", search, got, want)
		}
	}
	// Signed in, it searches as the person: the admin sees the safe, and
	// where the head torch is.
	for search, want := range map[string]string{
		"safe":  "Head torch in Safe,Safe",
		"torch": "Head torch in Safe,Torch",
	} {
		if got := strings.Join(kioskNames(t, srv, k.admin, "kiosk", search), ","); got != want {
			t.Errorf("as the admin, %q found %q, want %q", search, got, want)
		}
	}
	// Finding nothing is an empty list, never null.
	if rec := doJSON(t, srv, nil, http.MethodGet, "/api/v1/flows/kiosk/search?search=qqqqzzzzxx", ""); !strings.Contains(rec.Body.String(), `"assets":[]`) {
		t.Errorf("finding nothing: %s", rec.Body)
	}
	// A mistake in what is typed says where, as the asset list does.
	if rec := doJSON(t, srv, nil, http.MethodGet, "/api/v1/flows/kiosk/search?search="+url.QueryEscape("field.nope = 1"), ""); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "query_error") {
		t.Errorf("a mistake: %d %s", rec.Code, rec.Body)
	}

	// With nothing for the public role, a public flow finds nothing signed
	// out, and still finds for those signed in.
	nothingPublic(t, srv)
	if got := kioskNames(t, srv, nil, "kiosk", "torch"); len(got) != 0 {
		t.Errorf("with nothing public: %v", got)
	}

	// A search flow has no run to start, and is no step of another's.
	if rec := doJSON(t, srv, k.admin, http.MethodPost, "/api/v1/flows/kiosk/runs", ""); rec.Code != http.StatusConflict {
		t.Errorf("starting a run: %d %s", rec.Code, rec.Body)
	}

	// Turned off, it is found by no one.
	if err := setFlowEnabled(srv.store, "kiosk", false); err != nil {
		t.Fatal(err)
	}
	for who, cookie := range map[string]*http.Cookie{"signed out": nil, "the admin": k.admin} {
		if rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/flows/kiosk/search", ""); rec.Code != http.StatusNotFound {
			t.Errorf("turned off, %s: %d", who, rec.Code)
		}
	}
}

func setFlowEnabled(st *store.Store, slug string, enabled bool) error {
	f, err := st.GetFlow(slug)
	if err != nil {
		return err
	}
	_, err = st.UpdateFlow(f.ID, store.FlowInput{Slug: f.Slug, Name: f.Name, Enabled: enabled, Definition: f.Definition})
	return err
}

// TestASearchFlowThatIsNotPublic is for those who may run it: signed out,
// it asks to sign in, and those who may not run it are told there is none.
func TestASearchFlowThatIsNotPublic(t *testing.T) {
	k := newKioskShop(t)
	srv := k.srv
	k.save(t, "members", map[string]any{"version": 2, "category_ids": []string{k.shelf}, "steps": []any{searchStep(nil)}}, http.StatusCreated)
	if rec := doJSON(t, srv, nil, http.MethodGet, "/api/v1/flows/members/search?search=torch", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("signed out: %d %s", rec.Code, rec.Body)
	}
	readers, err := srv.store.CreateRole(store.RoleInput{Name: "Readers", Permissions: []string{rbac.PermAssetsRead}})
	if err != nil {
		t.Fatal(err)
	}
	reader, _ := srv.store.CreateNamedUser("reader@hackspace.org", "Reader", "UserSecret123!", true)
	if err := srv.store.SetUserRoles(reader.ID, []string{readers.ID}); err != nil {
		t.Fatal(err)
	}
	if rec := doJSON(t, srv, sessionCookie(t, srv, reader.ID), http.MethodGet, "/api/v1/flows/members/search?search=torch", ""); rec.Code != http.StatusNotFound {
		t.Errorf("without flows.run: %d %s", rec.Code, rec.Body)
	}
	if got := strings.Join(kioskNames(t, srv, k.admin, "members", "torch"), ","); got != "Head torch in Safe,Torch" {
		t.Errorf("as the admin: %q", got)
	}
}

// TestSavingASearchFlow checks a Search step as it is saved: alone in its
// flow, its filter one that runs, its columns and its wait those it may
// have; public only when it only searches; and warned when the public
// role would find nothing with it.
func TestSavingASearchFlow(t *testing.T) {
	k := newKioskShop(t)
	for name, c := range map[string]struct {
		definition map[string]any
		says       string
	}{
		"with another step":        {map[string]any{"version": 2, "category_ids": []string{k.shelf}, "steps": []any{searchStep(nil), map[string]any{"id": "done", "kind": "done"}}}, "only step"},
		"a filter naming no field": {map[string]any{"version": 2, "category_ids": []string{k.shelf}, "steps": []any{searchStep(map[string]any{"filter": "field.nope = 1"})}}, "filter"},
		"a broken filter":          {map[string]any{"version": 2, "category_ids": []string{k.shelf}, "steps": []any{searchStep(map[string]any{"filter": "name = ("})}}, "filter"},
		"a column it has not":      {map[string]any{"version": 2, "category_ids": []string{k.shelf}, "steps": []any{searchStep(map[string]any{"columns": []string{"price"}})}}, "price"},
		"too short a wait":         {map[string]any{"version": 2, "category_ids": []string{k.shelf}, "steps": []any{searchStep(map[string]any{"reset_after": 3})}}, "seconds"},
		"a public flow that files": {map[string]any{"version": 2, "public": true, "category_ids": []string{k.shelf}, "steps": []any{
			map[string]any{"id": "file", "kind": "create", "name": "Thing"}, map[string]any{"id": "done", "kind": "done"},
		}}, "signed-out"},
	} {
		if body := k.save(t, "x", c.definition, http.StatusBadRequest); !strings.Contains(body, c.says) {
			t.Errorf("%s: %s", name, body)
		}
	}
	// Over the locked cupboard alone, a public flow is saved, and says the
	// public role would find nothing with it.
	body := k.save(t, "cupboard", map[string]any{"version": 2, "public": true, "category_ids": []string{k.cupboard}, "steps": []any{searchStep(nil)}}, http.StatusCreated)
	if !strings.Contains(body, `"warnings":["The public role may read assets in none`) {
		t.Errorf("over the cupboard: %s", body)
	}
	if body := k.save(t, "shelf", map[string]any{"version": 2, "public": true, "category_ids": []string{k.shelf}, "steps": []any{searchStep(map[string]any{"filter": "", "columns": nil, "reset_after": 0})}}, http.StatusCreated); strings.Contains(body, "warnings") {
		t.Errorf("over the shelf: %s", body)
	}
}
