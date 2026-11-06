package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// holderOf is a user holding a role with these permissions, scoped ones
// among them, signed in.
func holderOf(t *testing.T, srv *Server, email string, perms ...string) *http.Cookie {
	t.Helper()
	role, err := srv.store.CreateRole(store.RoleInput{Name: "Role of " + email, Permissions: perms})
	if err != nil {
		t.Fatal(err)
	}
	u, err := srv.store.CreateNamedUser(email, "Someone", "UserSecret123!", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.store.AssignUserRole(u.ID, role.ID); err != nil {
		t.Fatal(err)
	}
	return sessionCookie(t, srv, u.ID)
}

type vendorBody struct {
	Vendor    models.Vendor   `json:"vendor"`
	Vendors   []models.Vendor `json:"vendors"`
	CanManage bool            `json:"can_manage"`
}

func decodeVendor(t *testing.T, rec *httptest.ResponseRecorder) vendorBody {
	t.Helper()
	var out vendorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("%s: %v", rec.Body, err)
	}
	return out
}

func vendorNames(vs []models.Vendor) string {
	var names []string
	for _, v := range vs {
		names = append(names, v.Name)
	}
	return strings.Join(names, ", ")
}

func scopeID(t *testing.T, srv *Server, name string) string {
	t.Helper()
	var id string
	if err := srv.store.QueryRow("SELECT id FROM scopes WHERE name = ?", name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// TestVendorsThroughTheAPI: a vendor is made, read, searched for, edited
// and deleted, with its contacts, and its history says what happened.
func TestVendorsThroughTheAPI(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)

	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/vendors", `{"name":"Acme","website":"https://acme.example","account_number":"A-1",
		"address":"1 Bolt Lane","notes":"**Net 30**","contacts":[{"name":"Wile","role":"sales","email":"wile@acme.example","phone":"123"}]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("making: %d %s", rec.Code, rec.Body)
	}
	v := decodeVendor(t, rec).Vendor
	if v.Name != "Acme" || len(v.Contacts) != 1 || v.Contacts[0].Role != "sales" || v.ScopeIDs == nil {
		t.Errorf("made: %+v", v)
	}
	doJSON(t, srv, cookie, http.MethodPost, "/api/v1/vendors", `{"name":"Bolts R Us","contacts":[{"name":"Bugs","email":"bugs@bolts.example"}]}`)

	got := decodeVendor(t, doJSON(t, srv, cookie, http.MethodGet, "/api/v1/vendors/"+v.ID, ""))
	if got.Vendor.Notes != "**Net 30**" || !got.CanManage {
		t.Errorf("read: %+v", got)
	}
	for q, want := range map[string]string{"": "Acme, Bolts R Us", "acme": "Acme", "BUGS": "Bolts R Us", "net 30": "Acme", "sales wile": "Acme", "nothing": ""} {
		list := decodeVendor(t, doJSON(t, srv, cookie, http.MethodGet, "/api/v1/vendors?q="+strings.ReplaceAll(q, " ", "+"), ""))
		if names := vendorNames(list.Vendors); names != want {
			t.Errorf("searching %q: %q, want %q", q, names, want)
		}
	}

	contact := v.Contacts[0]
	contact.Phone = "456"
	body, _ := json.Marshal(map[string]any{"name": "Acme Ltd", "website": "https://acme.example", "contacts": []models.VendorContact{contact, {Name: "Road", Role: "support"}}})
	rec = doJSON(t, srv, cookie, http.MethodPut, "/api/v1/vendors/"+v.ID, string(body))
	edited := decodeVendor(t, rec).Vendor
	if rec.Code != http.StatusOK || edited.Name != "Acme Ltd" || len(edited.Contacts) != 2 || edited.Contacts[0].ID != contact.ID || edited.Contacts[0].Phone != "456" || edited.Notes != "" {
		t.Errorf("edited: %d %+v", rec.Code, edited)
	}

	rec = doJSON(t, srv, cookie, http.MethodGet, "/api/v1/vendors/"+v.ID+"/history", "")
	var history struct {
		Events []models.Event `json:"events"`
		More   bool           `json:"more"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &history); err != nil || len(history.Events) != 2 || history.Events[0].Kind != models.EventVendorEdited ||
		history.Events[0].Payload.Actor == nil {
		t.Errorf("history: %s", rec.Body)
	}

	for what, c := range map[string]struct {
		method, path, body string
		want               int
	}{
		"no name":            {http.MethodPost, "/api/v1/vendors", `{"name":""}`, http.StatusBadRequest},
		"a javascript: site": {http.MethodPost, "/api/v1/vendors", `{"name":"X","website":"javascript:alert(1)"}`, http.StatusBadRequest},
		"the default scope":  {http.MethodPost, "/api/v1/vendors", `{"name":"X","scope_ids":["` + scopeID(t, srv, rbac.DefaultScope) + `"]}`, http.StatusConflict},
		"an unknown scope":   {http.MethodPost, "/api/v1/vendors", `{"name":"X","scope_ids":["nope"]}`, http.StatusBadRequest},
		"a missing vendor":   {http.MethodGet, "/api/v1/vendors/nope", "", http.StatusNotFound},
		"a missing history":  {http.MethodGet, "/api/v1/vendors/nope/history", "", http.StatusNotFound},
		"a bad page":         {http.MethodGet, "/api/v1/vendors/" + v.ID + "/history?before=nope", "", http.StatusBadRequest},
	} {
		if rec := doJSON(t, srv, cookie, c.method, c.path, c.body); rec.Code != c.want {
			t.Errorf("%s: %d, want %d: %s", what, rec.Code, c.want, rec.Body)
		}
	}

	if rec := doJSON(t, srv, cookie, http.MethodDelete, "/api/v1/vendors/"+v.ID, ""); rec.Code != http.StatusOK {
		t.Errorf("deleting: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/vendors/"+v.ID, ""); rec.Code != http.StatusNotFound {
		t.Errorf("deleted: %d", rec.Code)
	}
}

// TestVendorScopes: a vendor's scopes decide who sees it, everyone who
// reads assets sees one in none, and the public role sees those in the
// public scope; vendors.manage changes those in its scopes only.
func TestVendorScopes(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	st := srv.store
	tools, err := st.CreateScope("tools")
	if err != nil {
		t.Fatal(err)
	}
	kitchen, err := st.CreateScope("kitchen")
	if err != nil {
		t.Fatal(err)
	}
	public := scopeID(t, srv, rbac.PublicScope)
	mk := func(name string, scopes ...string) models.Vendor {
		t.Helper()
		v, err := st.CreateVendor(store.VendorInput{Name: name, ScopeIDs: scopes})
		if err != nil {
			t.Fatal(err)
		}
		return *v
	}
	open, toolShop, grocer, everyone := mk("Open"), mk("Tool shop", tools.ID), mk("Grocer", kitchen.ID), mk("Everyone's", public)

	toolReader := holderOf(t, srv, "tools@hackspace.org", "tools:"+rbac.PermAssetsRead)
	noone := holderOf(t, srv, "noone@hackspace.org")
	toolManager := holderOf(t, srv, "keeper@hackspace.org", "tools:"+rbac.PermAssetsRead, "tools:"+rbac.PermVendorsManage)
	manager := holderOf(t, srv, "buyer@hackspace.org", rbac.PermAssetsRead, rbac.PermVendorsManage)
	reader := holderOf(t, srv, "reader@hackspace.org", rbac.PermAssetsRead)

	for who, c := range map[string]struct {
		cookie *http.Cookie
		want   string
	}{
		"an admin":                          {cookie, "Everyone's, Grocer, Open, Tool shop"},
		"a reader of tools":                 {toolReader, "Everyone's, Open, Tool shop"},
		"someone with the public role only": {noone, "Everyone's, Open"},
	} {
		list := decodeVendor(t, doJSON(t, srv, c.cookie, http.MethodGet, "/api/v1/vendors", ""))
		if got := vendorNames(list.Vendors); got != c.want {
			t.Errorf("%s sees %q, want %q", who, got, c.want)
		}
	}
	// Scopes show only to those who see scopes.
	for _, v := range decodeVendor(t, doJSON(t, srv, toolReader, http.MethodGet, "/api/v1/vendors", "")).Vendors {
		if len(v.ScopeIDs) != 0 {
			t.Errorf("a reader sees %s's scopes", v.Name)
		}
	}
	if v := decodeVendor(t, doJSON(t, srv, toolManager, http.MethodGet, "/api/v1/vendors/"+toolShop.ID, "")).Vendor; len(v.ScopeIDs) != 1 {
		t.Errorf("a manager of vendors does not see its scopes: %+v", v)
	}

	signedOut := doJSON(t, srv, nil, http.MethodGet, "/api/v1/vendors/"+everyone.ID, "")
	if signedOut.Code != http.StatusUnauthorized {
		t.Errorf("signed out: %d", signedOut.Code)
	}

	rename := func(v models.Vendor, scopes ...string) string {
		body, _ := json.Marshal(map[string]any{"name": v.Name + " renamed", "scope_ids": append([]string{}, scopes...)})
		return string(body)
	}
	for _, c := range []struct {
		what               string
		cookie             *http.Cookie
		method, path, body string
		want               int
	}{
		{"a reader cannot see another scope's", toolReader, http.MethodGet, "/api/v1/vendors/" + grocer.ID, "", http.StatusNotFound},
		{"nor its history", toolReader, http.MethodGet, "/api/v1/vendors/" + grocer.ID + "/history", "", http.StatusNotFound},
		{"nor its files", toolReader, http.MethodGet, "/api/v1/vendors/" + grocer.ID + "/attachments", "", http.StatusNotFound},
		{"the public role sees a public one", noone, http.MethodGet, "/api/v1/vendors/" + everyone.ID, "", http.StatusOK},
		{"but not one in a scope", noone, http.MethodGet, "/api/v1/vendors/" + toolShop.ID, "", http.StatusNotFound},
		{"a reader cannot make one", reader, http.MethodPost, "/api/v1/vendors", `{"name":"X"}`, http.StatusForbidden},
		{"a reader cannot edit one", reader, http.MethodPut, "/api/v1/vendors/" + open.ID, rename(open), http.StatusForbidden},
		{"a scoped manager makes one in its scope", toolManager, http.MethodPost, "/api/v1/vendors", `{"name":"Drills","scope_ids":["` + tools.ID + `"]}`, http.StatusCreated},
		{"but not in no scope", toolManager, http.MethodPost, "/api/v1/vendors", `{"name":"Drills"}`, http.StatusForbidden},
		{"nor in another scope", toolManager, http.MethodPost, "/api/v1/vendors", `{"name":"Drills","scope_ids":["` + kitchen.ID + `"]}`, http.StatusForbidden},
		{"a scoped manager edits one in its scope", toolManager, http.MethodPut, "/api/v1/vendors/" + toolShop.ID, rename(toolShop, tools.ID), http.StatusOK},
		{"but cannot move it out of its scope", toolManager, http.MethodPut, "/api/v1/vendors/" + toolShop.ID, rename(toolShop, kitchen.ID), http.StatusForbidden},
		{"nor show it to everyone", toolManager, http.MethodPut, "/api/v1/vendors/" + toolShop.ID, rename(toolShop), http.StatusForbidden},
		{"nor edit one in no scope, which it sees", toolManager, http.MethodPut, "/api/v1/vendors/" + open.ID, rename(open, tools.ID), http.StatusForbidden},
		{"nor delete one in another scope", toolManager, http.MethodDelete, "/api/v1/vendors/" + grocer.ID, "", http.StatusNotFound},
		{"a manager everywhere edits one in no scope", manager, http.MethodPut, "/api/v1/vendors/" + open.ID, rename(open), http.StatusOK},
		{"a manager everywhere moves one between scopes", manager, http.MethodPut, "/api/v1/vendors/" + grocer.ID, rename(grocer, tools.ID, kitchen.ID), http.StatusOK},
	} {
		if rec := doJSON(t, srv, c.cookie, c.method, c.path, c.body); rec.Code != c.want {
			t.Errorf("%s: %d, want %d: %s", c.what, rec.Code, c.want, rec.Body)
		}
	}
	// Leaving scope_ids out leaves them as they are.
	rec := doJSON(t, srv, manager, http.MethodPut, "/api/v1/vendors/"+toolShop.ID, `{"name":"Tool shop"}`)
	if v := decodeVendor(t, rec).Vendor; rec.Code != http.StatusOK || len(v.ScopeIDs) != 1 || v.ScopeIDs[0] != tools.ID {
		t.Errorf("without scope_ids: %d %s", rec.Code, rec.Body)
	}
	// A scope on a vendor cannot be deleted.
	if rec := doJSON(t, srv, cookie, http.MethodDelete, "/api/v1/scopes/"+kitchen.ID, ""); rec.Code != http.StatusConflict {
		t.Errorf("deleting a vendor's scope: %d %s", rec.Code, rec.Body)
	}
	// A manager of vendors reads the scopes, to choose among them.
	if rec := doJSON(t, srv, toolManager, http.MethodGet, "/api/v1/scopes", ""); rec.Code != http.StatusOK {
		t.Errorf("a manager of vendors listing scopes: %d", rec.Code)
	}
}

// TestPreferredVendors: a category names the vendors it is bought from, in
// order, and each requester sees only those they may.
func TestPreferredVendors(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	st := srv.store
	tools, _ := st.CreateScope("tools")
	open, _ := st.CreateVendor(store.VendorInput{Name: "Open", Website: "https://open.example"})
	hidden, _ := st.CreateVendor(store.VendorInput{Name: "Hidden", ScopeIDs: []string{tools.ID}})
	cat, err := st.CreateCategoryFrom(store.CategoryInput{Name: "Screws", IsConsumable: true, IsRunningLow: true})
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/categories/" + cat.ID + "/vendors"
	rec := doJSON(t, srv, cookie, http.MethodPut, path, `{"vendor_ids":["`+hidden.ID+`","`+open.ID+`"]}`)
	var body struct {
		Category   models.Category   `json:"category"`
		Categories []models.Category `json:"categories"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusOK || strings.Join(body.Category.VendorIDs, ",") != hidden.ID+","+open.ID {
		t.Fatalf("naming: %d %s", rec.Code, rec.Body)
	}

	// One who cannot read tools sees only the open vendor, on the running-low
	// list's categories and on the category.
	other := holderOf(t, srv, "other@hackspace.org", "public:"+rbac.PermAssetsRead, rbac.PermCategoriesManage)
	rec = doJSON(t, srv, other, http.MethodGet, "/api/v1/categories", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, c := range body.Categories {
		if c.ID == cat.ID && strings.Join(c.VendorIDs, ",") != open.ID {
			t.Errorf("listed to one who cannot see the hidden vendor: %v", c.VendorIDs)
		}
	}
	rec = doJSON(t, srv, other, http.MethodGet, "/api/v1/categories/"+cat.ID, "")
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || strings.Join(body.Category.VendorIDs, ",") != open.ID {
		t.Errorf("read by one who cannot see the hidden vendor: %s", rec.Body)
	}
	// They cannot name the hidden one, and naming what they see keeps it,
	// after.
	if rec := doJSON(t, srv, other, http.MethodPut, path, `{"vendor_ids":["`+hidden.ID+`"]}`); rec.Code != http.StatusBadRequest {
		t.Errorf("naming a vendor one cannot see: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, other, http.MethodPut, path, `{"vendor_ids":["`+open.ID+`"]}`); rec.Code != http.StatusOK {
		t.Errorf("naming: %d %s", rec.Code, rec.Body)
	}
	if got, _ := st.CategoryVendorIDs(cat.ID); strings.Join(got, ",") != open.ID+","+hidden.ID {
		t.Errorf("the hidden vendor is not kept: %v", got)
	}
	if rec := doJSON(t, srv, holderOf(t, srv, "plain@hackspace.org", rbac.PermAssetsRead), http.MethodPut, path, `{"vendor_ids":[]}`); rec.Code != http.StatusForbidden {
		t.Errorf("a reader naming vendors: %d", rec.Code)
	}
}

// TestVendorAttachmentsThroughTheAPI: a vendor's files follow
// vendors.manage in its scopes to change, and seeing it to read.
func TestVendorAttachmentsThroughTheAPI(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	tools, _ := srv.store.CreateScope("tools")
	if _, err := srv.store.CreateScope("kitchen"); err != nil {
		t.Fatal(err)
	}
	v, _ := srv.store.CreateVendor(store.VendorInput{Name: "Tool shop", ScopeIDs: []string{tools.ID}})
	path := "/api/v1/vendors/" + v.ID + "/attachments"
	rec := attach(t, srv, cookie, path, "prices.csv", []byte("part,price\nM3,0.02\n"), map[string]string{"description": "2026"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("adding: %d %s", rec.Code, rec.Body)
	}
	doc := decodeAttached(t, rec).Attachment
	if doc.Kind != models.AttachmentDocument || doc.AddedBy == "" {
		t.Errorf("added: %+v", doc)
	}
	reader := holderOf(t, srv, "reader@hackspace.org", "tools:"+rbac.PermAssetsRead)
	elsewhere := holderOf(t, srv, "elsewhere@hackspace.org", "kitchen:"+rbac.PermAssetsRead)
	keeper := holderOf(t, srv, "keeper@hackspace.org", "tools:"+rbac.PermAssetsRead, "tools:"+rbac.PermVendorsManage)
	for what, c := range map[string]struct {
		who    *http.Cookie
		method string
		path   string
		want   int
	}{
		"a reader in its scope lists them":      {reader, http.MethodGet, path, http.StatusOK},
		"and reads one, as a download":          {reader, http.MethodGet, path + "/" + doc.ID, http.StatusOK},
		"a reader elsewhere does not see them":  {elsewhere, http.MethodGet, path, http.StatusNotFound},
		"a reader cannot describe one":          {reader, http.MethodPut, path + "/" + doc.ID, http.StatusForbidden},
		"a keeper of its vendors describes one": {keeper, http.MethodPut, path + "/" + doc.ID, http.StatusOK},
	} {
		rec := doJSON(t, srv, c.who, c.method, c.path, `{"description":"Checked"}`)
		if rec.Code != c.want {
			t.Errorf("%s: %d, want %d: %s", what, rec.Code, c.want, rec.Body)
		}
		if c.want == http.StatusOK && c.path == path+"/"+doc.ID && c.method == http.MethodGet && !strings.HasPrefix(rec.Header().Get("Content-Disposition"), "attachment") {
			t.Errorf("a document is not served as a download: %v", rec.Header())
		}
	}
	// Who added it shows only to those who track assets in its scopes.
	var listed attached
	if err := json.Unmarshal(doJSON(t, srv, reader, http.MethodGet, path, "").Body.Bytes(), &listed); err != nil || len(listed.Attachments) != 1 || listed.Attachments[0].AddedBy != "" {
		t.Errorf("listed to a reader: %+v %v", listed, err)
	}
	if rec := attach(t, srv, reader, path, "x.txt", []byte("x"), nil); rec.Code != http.StatusForbidden {
		t.Errorf("a reader adding: %d", rec.Code)
	}
	if rec := attach(t, srv, keeper, path, "y.txt", []byte("y"), nil); rec.Code != http.StatusCreated {
		t.Errorf("a keeper adding: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, keeper, http.MethodDelete, path+"/"+doc.ID, ""); rec.Code != http.StatusOK {
		t.Errorf("a keeper removing: %d %s", rec.Code, rec.Body)
	}
	if rec := attach(t, srv, cookie, "/api/v1/vendors/nope/attachments", "z.txt", []byte("z"), nil); rec.Code != http.StatusNotFound {
		t.Errorf("adding to no vendor: %d", rec.Code)
	}
}
