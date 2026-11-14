package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

// searchNoted is the asset list a search finds, by name, and why each
// matched other than as typed, and the terms it suggests.
func searchNoted(t *testing.T, srv *Server, cookie *http.Cookie, search string) (names []string, matched map[string]string, suggestions []string) {
	t.Helper()
	rec := doJSON(t, srv, cookie, http.MethodGet, searchPath(search), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("searching %q: %d %s", search, rec.Code, rec.Body.String())
	}
	var out struct {
		Assets      []models.AssetWithDetails `json:"assets"`
		Matched     map[string]string         `json:"matched"`
		Suggestions []string                  `json:"suggestions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	byID := map[string]string{}
	for _, a := range out.Assets {
		names = append(names, a.Asset.Name)
		byID[a.Asset.ID] = a.Asset.Name
	}
	matched = map[string]string{}
	for id, why := range out.Matched {
		matched[byID[id]] = why
	}
	return names, matched, out.Suggestions
}

func TestSearchExpandsThroughRelatedTermsAndForgivesTypos(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	filamentShelf(t, srv)
	st := srv.store
	if _, err := st.CreateAsset("Flexible filament sampler", nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	// Terms are made and linked through the API, by someone who manages them.
	post := func(body string) string {
		rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/terms", body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("adding %s: %d %s", body, rec.Code, rec.Body.String())
		}
		var out struct{ Term models.Term }
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return out.Term.ID
	}
	flex := post(`{"label": "Flexible filament", "alt_labels": ["bendy filament"]}`)
	tpu := post(`{"label": "TPU", "alt_labels": ["thermoplastic polyurethane"]}`)
	pla := post(`{"label": "PLA"}`)
	for _, link := range []string{tpu + "/links/broader/" + flex, pla + "/links/related/" + tpu} {
		if rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/terms/"+link, ""); rec.Code != http.StatusOK {
			t.Fatalf("linking %s: %d %s", link, rec.Code, rec.Body.String())
		}
	}
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/terms", `{"label": "tpu"}`); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), tpu) {
		t.Errorf("a second TPU: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/terms/"+flex+"/links/broader/"+tpu, ""); rec.Code != http.StatusBadRequest {
		t.Errorf("a loop: %d %s", rec.Code, rec.Body.String())
	}

	// What matches as typed comes first; what a term expands to after,
	// saying why.
	names, matched, suggestions := searchNoted(t, srv, cookie, "flexible filament")
	if strings.Join(names, ",") != "Flexible filament sampler,Black TPU" {
		t.Errorf("flexible filament found %v", names)
	}
	if len(matched) != 1 || matched["Black TPU"] != "TPU, narrower than Flexible filament" {
		t.Errorf("flexible filament said %v", matched)
	}
	if len(suggestions) != 0 {
		t.Errorf("flexible filament suggested %v", suggestions)
	}
	// Related terms are suggested, not searched.
	names, _, suggestions = searchNoted(t, srv, cookie, "thermoplastic polyurethane")
	if strings.Join(names, ",") != "Black TPU" || strings.Join(suggestions, ",") != "PLA" {
		t.Errorf("thermoplastic polyurethane found %v and suggested %v", names, suggestions)
	}
	// A typing mistake is forgiven when nothing matches as typed.
	names, matched, _ = searchNoted(t, srv, cookie, "Blakc")
	if strings.Join(names, ",") != "Black TPU" || matched["Black TPU"] != "black, close to Blakc" {
		t.Errorf("Blakc found %v, saying %v", names, matched)
	}
	// In a query, ~~ expands and ~ doesn't.
	if got := strings.Join(assetNames(t, srv, cookie, searchPath(`name ~~ "bendy filament" AND category = filament`)), ","); got != "Black TPU" {
		t.Errorf("name ~~ bendy filament found %q", got)
	}
	if got := strings.Join(assetNames(t, srv, cookie, searchPath(`name ~ "flexible filament" AND category = filament`)), ","); got != "" {
		t.Errorf("name ~ found %q", got)
	}

	// What a search expands to can be asked.
	rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/terms/expand?q=bendy+filament", "")
	var expand ExpandResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &expand)
	if len(expand.Expansion) != 3 || expand.Expansion[1].Label != "TPU" {
		t.Errorf("bendy filament expands to %+v", expand)
	}
}

func TestTermsAreManagedWithTheirOwnPermissionAndImported(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	reader := userWithPermissions(t, srv, "reader@hackspace.org", rbac.PermAssetsRead)
	if rec := doJSON(t, srv, reader, http.MethodPost, "/api/v1/terms", `{"label": "TPU"}`); rec.Code != http.StatusForbidden {
		t.Errorf("a reader adding a term: %d", rec.Code)
	}

	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/terms/imports", `{"name": "Filament words", "terms": [
		{"label": "TPU", "alt_labels": ["thermoplastic polyurethane"], "broader": ["Flexible filament"], "related": ["TPE"]},
		{"label": "Flexible filament", "broader": ["Filament"]}
	]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("importing: %d %s", rec.Code, rec.Body.String())
	}
	var made struct{ Import models.TermImport }
	_ = json.Unmarshal(rec.Body.Bytes(), &made)
	if made.Import.Terms != 4 || made.Import.Links != 3 || made.Import.AddedBy != admin.ID {
		t.Errorf("imported %+v", made.Import)
	}

	// A reader sees the terms, and where each came from, but not who added them.
	var seen TermsResponse
	_ = json.Unmarshal(doJSON(t, srv, reader, http.MethodGet, "/api/v1/terms", "").Body.Bytes(), &seen)
	if len(seen.Terms) != 4 || len(seen.Links) != 3 || len(seen.Imports) != 0 {
		t.Fatalf("a reader sees %d terms, %d links, %d imports", len(seen.Terms), len(seen.Links), len(seen.Imports))
	}
	for _, tm := range seen.Terms {
		if tm.AddedBy != "" || tm.AddedByName != "" || tm.Source != "import" {
			t.Errorf("a reader sees %+v", tm.TermSource)
		}
	}
	var managed TermsResponse
	_ = json.Unmarshal(doJSON(t, srv, cookie, http.MethodGet, "/api/v1/terms", "").Body.Bytes(), &managed)
	if len(managed.Imports) != 1 || managed.Terms[0].AddedBy != admin.ID {
		t.Errorf("a manager sees %+v", managed)
	}

	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/terms/imports", `{"name": "", "terms": []}`); rec.Code != http.StatusBadRequest {
		t.Errorf("an empty import: %d", rec.Code)
	}
	if rec := doJSON(t, srv, cookie, http.MethodDelete, "/api/v1/terms/imports/"+made.Import.ID, ""); rec.Code != http.StatusOK {
		t.Fatalf("removing the import: %d %s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(doJSON(t, srv, cookie, http.MethodGet, "/api/v1/terms", "").Body.Bytes(), &managed)
	if len(managed.Terms) != 0 || len(managed.Imports) != 0 {
		t.Errorf("after removing the import: %+v", managed)
	}
	if rec := doJSON(t, srv, cookie, http.MethodDelete, "/api/v1/terms/imports/"+made.Import.ID, ""); rec.Code != http.StatusNotFound {
		t.Errorf("removing it again: %d", rec.Code)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/terms/nope", `{"label": "x"}`); rec.Code != http.StatusNotFound {
		t.Errorf("changing no term: %d", rec.Code)
	}
}

// TestAgentsFillInTermsAsTheirPerson: a person's own assistant, over MCP,
// adds terms and links as that person, recorded as through it, and finds
// what they expand to.
func TestAgentsFillInTermsAsTheirPerson(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	adminCookie := sessionCookie(t, srv, admin.ID)
	filamentShelf(t, srv)
	_, secret := personalToken(t, srv, adminCookie, "Claude")
	cs := mcpSession(t, srv, secret)

	var made struct{ Term models.Term }
	structured(t, callTool(t, cs, "save_term", map[string]any{"label": "Flexible filament", "alt_labels": []string{"bendy filament"}}), &made)
	if made.Term.AddedBy != admin.ID || made.Term.Via == "" || made.Term.Source != "agent" {
		t.Errorf("an assistant's term: %+v", made.Term.TermSource)
	}
	structured(t, callTool(t, cs, "save_term", map[string]any{"id": made.Term.ID, "note": "Bends without breaking."}), &made)
	if made.Term.Note != "Bends without breaking." || made.Term.Label != "Flexible filament" || len(made.Term.AltLabels) != 1 {
		t.Errorf("changing only the note: %+v", made.Term)
	}
	var imported struct{ Import models.TermImport }
	structured(t, callTool(t, cs, "import_terms", map[string]any{"name": "Filament words", "terms": []map[string]any{
		{"label": "TPU", "broader": []string{"Flexible filament"}, "related": []string{"PLA"}},
	}}), &imported)
	if imported.Import.Terms != 2 || imported.Import.Links != 2 || imported.Import.Via == "" {
		t.Errorf("imported %+v", imported.Import)
	}

	var found struct {
		Assets      []summary
		Matched     map[string]string
		Suggestions []string
	}
	structured(t, callTool(t, cs, "search_assets", map[string]any{"search": "bendy filament"}), &found)
	if len(found.Assets) != 1 || found.Assets[0].Name != "Black TPU" || len(found.Matched) != 1 {
		t.Errorf("bendy filament found %+v", found)
	}
	for _, why := range found.Matched {
		if why != "TPU, narrower than Flexible filament" {
			t.Errorf("said why %q", why)
		}
	}
	structured(t, callTool(t, cs, "search_assets", map[string]any{"search": "TPU"}), &found)
	if strings.Join(found.Suggestions, ",") != "PLA" {
		t.Errorf("TPU suggested %v", found.Suggestions)
	}
	var expanded ExpandResponse
	structured(t, callTool(t, cs, "expand_search", map[string]any{"search": "flexible filament"}), &expanded)
	if len(expanded.Expansion) != 2 {
		t.Errorf("expanded %+v", expanded)
	}

	// Someone who only reads assets is offered no tool that changes terms.
	reader := userWithPermissions(t, srv, "reader@hackspace.org", rbac.PermAssetsRead)
	_, readerSecret := personalToken(t, srv, reader, "Reader's Claude")
	offered := strings.Join(toolNames(t, mcpSession(t, srv, readerSecret)), ",")
	if !strings.Contains(offered, "list_terms") || strings.Contains(offered, "save_term") || strings.Contains(offered, "import_terms") {
		t.Errorf("a reader's assistant is offered %s", offered)
	}
}
