package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/fields"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// spoolList is GET /api/v1/spool, decoded.
func spoolList(t *testing.T, srv *Server, cookie *http.Cookie, query string) SpoolResponse {
	t.Helper()
	rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/spool"+query, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("listing the spool: %d %s", rec.Code, rec.Body)
	}
	var resp SpoolResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp
}

// entryFor finds an asset's entry in a spool listing.
func entryFor(resp SpoolResponse, assetID string) *SpoolEntryView {
	for i := range resp.Entries {
		if resp.Entries[i].AssetID == assetID {
			return &resp.Entries[i]
		}
	}
	return nil
}

func TestSpoolThroughTheAPI(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	st := srv.store
	expect := func(what, method, path, body string, want int) string {
		t.Helper()
		rec := doJSON(t, srv, cookie, method, path, body)
		if rec.Code != want {
			t.Errorf("%s: %d, want %d: %s", what, rec.Code, want, rec.Body)
		}
		return rec.Body.String()
	}

	// Shelf labels are 62 × 29 mm: they suit 62 mm tape, not 29 × 90 mm
	// die-cut labels.
	if _, err := st.CreateField("location", "Location", fields.TypeText, nil); err != nil {
		t.Fatal(err)
	}
	wide := createPrinter(t, srv, cookie, 9100, "dk-22205")
	dieCut := createPrinter(t, srv, cookie, 9100, "dk-11201")
	shelf := createTemplate(t, srv, cookie, shelfTemplate)
	bins, _ := st.CreateCategory("Bins", nil, "", false, false)
	expect("naming the bins' label template", http.MethodPut, "/api/v1/categories/"+bins.ID+"/label-template", `{"label_template_id":"`+shelf+`"}`, http.StatusOK)
	expect("naming a missing template", http.MethodPut, "/api/v1/categories/"+bins.ID+"/label-template", `{"label_template_id":"nope"}`, http.StatusBadRequest)
	bin1, _ := st.CreateAsset("Bin 1", []string{bins.ID}, []models.AssetGeneratorKey{{GeneratorType: "micro_qr", AssignedCode: "BIN1"}}, nil)
	bin2, _ := st.CreateAsset("Bin 2", []string{bins.ID}, []models.AssetGeneratorKey{{GeneratorType: "micro_qr", AssignedCode: "BIN2"}}, nil)
	loose, _ := st.CreateAsset("Loose", nil, nil, nil)
	gone, _ := st.CreateAsset("Gone", nil, nil, nil)
	if err := st.DeleteAsset(gone.Asset.ID); err != nil {
		t.Fatal(err)
	}
	if body := expect("the asset's label template", http.MethodGet, "/api/v1/assets/"+bin1.Asset.ID, "", http.StatusOK); !strings.Contains(body, `"label_template":{"template_id":"`+shelf+`"`) {
		t.Errorf("the asset's label template: %s", body)
	}

	// Adding by ID: each takes its categories' template, and the one
	// printer that suits it.
	var added SpoolAddResponse
	body := expect("adding assets", http.MethodPost, "/api/v1/spool", `{"asset_ids":["`+bin1.Asset.ID+`","`+loose.Asset.ID+`","`+gone.Asset.ID+`","nope"]}`, http.StatusCreated)
	_ = json.Unmarshal([]byte(body), &added)
	if len(added.Added) != 2 || len(added.Skipped) != 2 || added.Skipped[0].Reason != "archived" || added.Skipped[1].Reason != "not found" {
		t.Fatalf("adding assets: %s", body)
	}
	if e := added.Added[0]; e.TemplateID == nil || *e.TemplateID != shelf || e.Destination != "printer:"+wide || e.AddedBy.ID != admin.ID {
		t.Errorf("a bin's entry: %+v", e)
	}
	if e := added.Added[1]; e.TemplateID != nil || e.Destination != "" {
		t.Errorf("a loose asset's entry: %+v", e)
	}
	body = expect("adding a category", http.MethodPost, "/api/v1/spool", `{"category_id":"`+bins.ID+`","copies":2}`, http.StatusCreated)
	if !strings.Contains(body, `"already_waiting":["`+bin1.Asset.ID+`"]`) || !strings.Contains(body, `"asset_id":"`+bin2.Asset.ID+`"`) {
		t.Errorf("adding a category: %s", body)
	}
	expect("adding by a filter, all waiting", http.MethodPost, "/api/v1/spool", `{"filter":{"search":"bin"}}`, http.StatusOK)
	expect("adding archived assets by a filter", http.MethodPost, "/api/v1/spool", `{"filter":{"archived":"true"}}`, http.StatusBadRequest)
	expect("adding by a bad filter", http.MethodPost, "/api/v1/spool", `{"filter":{"condition":"melted"}}`, http.StatusBadRequest)
	expect("adding by two sources", http.MethodPost, "/api/v1/spool", `{"asset_ids":["`+bin1.Asset.ID+`"],"category_id":"`+bins.ID+`"}`, http.StatusBadRequest)
	expect("adding by none", http.MethodPost, "/api/v1/spool", `{}`, http.StatusBadRequest)
	expect("adding a missing category", http.MethodPost, "/api/v1/spool", `{"category_id":"nope"}`, http.StatusNotFound)
	expect("adding with a missing template", http.MethodPost, "/api/v1/spool", `{"asset_ids":["`+loose.Asset.ID+`"],"template_id":"nope"}`, http.StatusBadRequest)
	expect("adding with too many copies", http.MethodPost, "/api/v1/spool", `{"asset_ids":["`+loose.Asset.ID+`"],"template_id":"`+shelf+`","copies":21}`, http.StatusBadRequest)

	spool := spoolList(t, srv, cookie, "")
	if len(spool.Entries) != 3 || len(spool.Fits[shelf]) != 1 || spool.Fits[shelf][0] != wide || len(spool.Printers) != 2 {
		t.Fatalf("the spool: %+v", spool)
	}
	if p := entryFor(spool, loose.Asset.ID).Problems; len(p) != 2 || p[0] != "No template chosen" || p[1] != "No printer chosen" {
		t.Errorf("a loose asset's problems: %v", p)
	}
	if p := entryFor(spool, bin1.Asset.ID).Problems; len(p) != 0 {
		t.Errorf("a bin's problems: %v", p)
	}

	// Choosing a template sends entries with nowhere to go to the one
	// printer it suits; a printer it does not suit is a problem.
	looseID, bin1ID, bin2ID := entryFor(spool, loose.Asset.ID).ID, entryFor(spool, bin1.Asset.ID).ID, entryFor(spool, bin2.Asset.ID).ID
	expect("choosing a template", http.MethodPatch, "/api/v1/spool", `{"entry_ids":["`+looseID+`"],"template_id":"`+shelf+`"}`, http.StatusOK)
	expect("choosing a printer it does not suit", http.MethodPatch, "/api/v1/spool", `{"entry_ids":["`+bin2ID+`"],"destination":"printer:`+dieCut+`"}`, http.StatusOK)
	spool = spoolList(t, srv, cookie, "")
	if e := entryFor(spool, loose.Asset.ID); e.Destination != "printer:"+wide || len(e.Problems) != 0 {
		t.Errorf("after choosing a template: %+v", e)
	}
	if p := entryFor(spool, bin2.Asset.ID).Problems; len(p) != 1 || !strings.Contains(p[0], "Shelf does not suit QL dk-11201") {
		t.Errorf("an unsuited printer: %v", p)
	}
	expect("a bad destination", http.MethodPatch, "/api/v1/spool", `{"entry_ids":["`+bin2ID+`"],"destination":"fax"}`, http.StatusBadRequest)
	expect("a missing printer", http.MethodPatch, "/api/v1/spool", `{"entry_ids":["`+bin2ID+`"],"destination":"printer:nope"}`, http.StatusBadRequest)
	expect("a missing entry", http.MethodPatch, "/api/v1/spool", `{"entry_ids":["`+bin2ID+`","nope"],"copies":2}`, http.StatusNotFound)
	expect("no entries", http.MethodPatch, "/api/v1/spool", `{"entry_ids":[],"copies":2}`, http.StatusBadRequest)
	expect("downloading the bins", http.MethodPatch, "/api/v1/spool", `{"entry_ids":["`+bin1ID+`","`+bin2ID+`"],"destination":"download"}`, http.StatusOK)

	// Downloading prints them: they leave for the Printed list and their
	// histories.
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/spool/download", `{"entry_ids":["`+bin1ID+`","`+bin2ID+`"],"format":"pdf"}`)
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Body.String(), "%PDF") || rec.Header().Get("X-Spool-Problems") != "0" {
		t.Fatalf("downloading: %d %s %s", rec.Code, rec.Header(), rec.Body)
	}
	if pages := strings.Count(rec.Body.String(), "/Type /Page "); pages != 3 {
		t.Errorf("one page for the first bin and two for the second, got %d", pages)
	}
	spool = spoolList(t, srv, cookie, "")
	if len(spool.Entries) != 1 || spool.Entries[0].ID != looseID {
		t.Errorf("waiting after downloading: %+v", spool.Entries)
	}
	printed := spoolList(t, srv, cookie, "?printed=true")
	if len(printed.Entries) != 2 || printed.Entries[0].PrintedOn != "Download" || printed.Entries[0].PrintedTemplate != "Shelf" || len(printed.Entries[0].Problems) != 0 {
		t.Errorf("the Printed list: %+v", printed.Entries)
	}
	if body := expect("a bin's history", http.MethodGet, "/api/v1/assets/"+bin2.Asset.ID+"/history", "", http.StatusOK); !strings.Contains(body, `"kind":"asset.label_printed"`) || !strings.Contains(body, `"label":{"template":"Shelf","download":true,"copies":2}`) {
		t.Errorf("a bin's history: %s", body)
	}
	expect("downloading printed entries", http.MethodPost, "/api/v1/spool/download", `{"entry_ids":["`+bin1ID+`"],"format":"pdf"}`, http.StatusNotFound)
	expect("removing printed entries", http.MethodPost, "/api/v1/spool/remove", `{"entry_ids":["`+bin1ID+`"]}`, http.StatusNotFound)

	// Printed entries go back to print again.
	body = expect("printing again", http.MethodPost, "/api/v1/spool/requeue", `{"entry_ids":["`+bin1ID+`"]}`, http.StatusCreated)
	if !strings.Contains(body, `"destination":"download"`) {
		t.Errorf("printing again: %s", body)
	}
	expect("printing waiting entries again", http.MethodPost, "/api/v1/spool/requeue", `{"entry_ids":["`+looseID+`"]}`, http.StatusNotFound)
	expect("removing", http.MethodPost, "/api/v1/spool/remove", `{"entry_ids":["`+looseID+`"]}`, http.StatusOK)
	expect("removing again", http.MethodPost, "/api/v1/spool/remove", `{"entry_ids":["`+looseID+`"]}`, http.StatusNotFound)

	// A label template categories name stays.
	expect("deleting a template a category names", http.MethodDelete, "/api/v1/templates/"+shelf, "", http.StatusConflict)
	expect("conflicts", http.MethodGet, "/api/v1/categories/label-conflicts", "", http.StatusOK)
}

// TestDownloadingFromTheSpoolKeepsWhatCannotBeProduced checks labels that
// cannot be produced stay waiting with the reason while the rest download,
// and that one download holds one template.
func TestDownloadingFromTheSpoolKeepsWhatCannotBeProduced(t *testing.T) {
	srv, cookie, tagged, untagged, tag := labelFixture(t)
	other := createTemplate(t, srv, cookie, shelfTemplate)
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/spool", `{"asset_ids":["`+tagged+`","`+untagged+`"],"template_id":"`+tag+`","destination":"download"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("adding: %d %s", rec.Code, rec.Body)
	}
	spool := spoolList(t, srv, cookie, "")
	taggedID, untaggedID := entryFor(spool, tagged).ID, entryFor(spool, untagged).ID

	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/spool/download", `{"entry_ids":["`+untaggedID+`"],"format":"zip"}`)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), untagged) {
		t.Errorf("downloading only what cannot be produced: %d %s", rec.Code, rec.Body)
	}
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/spool/download", `{"entry_ids":["`+taggedID+`","`+untaggedID+`"],"format":"zip","mono":true}`)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/zip" || rec.Header().Get("X-Spool-Problems") != "1" {
		t.Fatalf("downloading: %d %s %s", rec.Code, rec.Header(), rec.Body)
	}
	spool = spoolList(t, srv, cookie, "")
	if len(spool.Entries) != 1 || spool.Entries[0].ID != untaggedID || len(spool.Entries[0].Problems) != 1 || !strings.HasPrefix(spool.Entries[0].Problems[0], "Could not be produced") {
		t.Errorf("left waiting: %+v", spool.Entries)
	}
	// Changing it clears the problem, to try again.
	if rec := doJSON(t, srv, cookie, http.MethodPatch, "/api/v1/spool", `{"entry_ids":["`+untaggedID+`"],"template_id":"`+other+`"}`); rec.Code != http.StatusOK {
		t.Fatal(rec.Body)
	}
	if p := spoolList(t, srv, cookie, "").Entries[0].Problems; len(p) != 0 {
		t.Errorf("after a change: %v", p)
	}

	// One template at a time, and only with one.
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/spool", `{"asset_ids":["`+tagged+`"],"template_id":"`+tag+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatal(rec.Body)
	}
	againID := entryFor(spoolList(t, srv, cookie, ""), tagged).ID
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/spool/download", `{"entry_ids":["`+againID+`","`+untaggedID+`"],"format":"pdf"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("two templates: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPatch, "/api/v1/spool", `{"entry_ids":["`+againID+`"],"template_id":""}`); rec.Code != http.StatusOK {
		t.Fatal(rec.Body)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/spool/download", `{"entry_ids":["`+againID+`"],"format":"pdf"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("no template: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/spool/download", `{"entry_ids":["`+againID+`"],"format":"svg"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("an SVG: %d %s", rec.Code, rec.Body)
	}
}

// TestSpoolPermissions checks the spool needs labels.print, and shows and
// takes only assets the requester can read.
func TestSpoolPermissions(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	adminCookie := sessionCookie(t, srv, admin.ID)
	st := srv.store
	workshop, err := st.CreateScope("workshop")
	if err != nil {
		t.Fatal(err)
	}
	tools, _ := st.CreateCategory("Tools", nil, "", false, false)
	if _, err := st.SetCategoryScopes(tools.ID, []string{workshop.ID}); err != nil {
		t.Fatal(err)
	}
	saw, _ := st.CreateAsset("Saw", []string{tools.ID}, nil, nil)
	secret, _ := st.CreateAsset("Secret", nil, nil, nil)

	reader := userWithPermissions(t, srv, "reader@test.org", rbac.PermAssetsRead)
	role, err := st.CreateRole(store.RoleInput{Name: "Workshop printers", Permissions: []string{"workshop:" + rbac.PermAssetsRead, rbac.PermLabelsPrint}})
	if err != nil {
		t.Fatal(err)
	}
	printerUser, _ := st.CreateNamedUser("printer@test.org", "Printer", "UserSecret123!", true)
	if err := st.SetUserRoles(printerUser.ID, []string{role.ID}); err != nil {
		t.Fatal(err)
	}
	printer := sessionCookie(t, srv, printerUser.ID)
	if rec := doJSON(t, srv, reader, http.MethodGet, "/api/v1/spool", ""); rec.Code != http.StatusForbidden {
		t.Errorf("a reader listing the spool: %d", rec.Code)
	}
	if rec := doJSON(t, srv, reader, http.MethodPost, "/api/v1/spool", `{"asset_ids":["`+saw.Asset.ID+`"]}`); rec.Code != http.StatusForbidden {
		t.Errorf("a reader adding: %d", rec.Code)
	}
	if rec := doJSON(t, srv, nil, http.MethodGet, "/api/v1/spool", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("signed out: %d", rec.Code)
	}

	// The admin queues both; the workshop printer sees and changes only
	// the saw's.
	if rec := doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/spool", `{"asset_ids":["`+saw.Asset.ID+`","`+secret.Asset.ID+`"]}`); rec.Code != http.StatusCreated {
		t.Fatal(rec.Body)
	}
	all := spoolList(t, srv, adminCookie, "")
	secretID := entryFor(all, secret.Asset.ID).ID
	seen := spoolList(t, srv, printer, "")
	if len(seen.Entries) != 1 || seen.Entries[0].AssetID != saw.Asset.ID {
		t.Errorf("the workshop printer sees %+v", seen.Entries)
	}
	if mine := spoolList(t, srv, printer, "?mine=true"); len(mine.Entries) != 0 {
		t.Errorf("the workshop printer's own: %+v", mine.Entries)
	}
	if rec := doJSON(t, srv, printer, http.MethodPost, "/api/v1/spool/remove", `{"entry_ids":["`+secretID+`"]}`); rec.Code != http.StatusNotFound {
		t.Errorf("removing an unreadable asset's entry: %d", rec.Code)
	}
	rec := doJSON(t, srv, printer, http.MethodPost, "/api/v1/spool", `{"asset_ids":["`+secret.Asset.ID+`"]}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"reason":"not found"`) {
		t.Errorf("adding an unreadable asset: %d %s", rec.Code, rec.Body)
	}
	rec = doJSON(t, srv, printer, http.MethodPost, "/api/v1/spool", `{"filter":{}}`)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), secret.Asset.ID) {
		t.Errorf("adding everything readable: %d %s", rec.Code, rec.Body)
	}
	var mine SpoolAddResponse
	rec = doJSON(t, srv, printer, http.MethodPost, "/api/v1/spool", `{"asset_ids":["`+saw.Asset.ID+`"],"template_id":"","copies":1}`)
	_ = json.Unmarshal(rec.Body.Bytes(), &mine)
	if rec.Code != http.StatusOK || len(mine.AlreadyWaiting) != 1 {
		t.Errorf("adding the saw again: %d %s", rec.Code, rec.Body)
	}
}
