package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// TestPeopleShowOnlyToThoseWhoTrack lends a drill and looks at it as
// someone who reads assets, someone who tracks them, and the borrower:
// people show by name, never by email, and only with assets.track.
func TestPeopleShowOnlyToThoseWhoTrack(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	adminCookie := sessionCookie(t, srv, admin.ID)
	st := srv.store
	tools, err := st.CreateCategoryFrom(store.CategoryInput{Name: "Tools", IsLendable: true})
	if err != nil {
		t.Fatal(err)
	}
	drill, _ := st.CreateAsset("Drill", []string{tools.ID}, nil, nil)
	saw, _ := st.CreateAsset("Saw", []string{tools.ID}, nil, nil)
	molly, _ := st.CreateNamedUser("molly@hackspace.org", "Molly Member", "UserSecret123!", true)
	// Someone from before names were required.
	nameless, _ := st.CreateUser("nameless@hackspace.org", "UserSecret123!", true)
	for _, c := range []struct{ asset, who string }{{drill.Asset.ID, molly.ID}, {saw.Asset.ID, nameless.ID}} {
		if rec := doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/assets/"+c.asset+"/checkout", `{"borrower_id":"`+c.who+`"}`); rec.Code != http.StatusOK {
			t.Fatalf("lending: %d %s", rec.Code, rec.Body)
		}
	}
	borrowers, _ := st.CreateRole(store.RoleInput{Name: "Borrowers", Permissions: []string{rbac.PermAssetsRead, rbac.PermAssetsBorrow}})
	if err := st.SetUserRoles(molly.ID, []string{borrowers.ID}); err != nil {
		t.Fatal(err)
	}
	mollyCookie := sessionCookie(t, srv, molly.ID)
	reader := userWithPermissions(t, srv, "reader@hackspace.org", rbac.PermAssetsRead)
	tracker := userWithPermissions(t, srv, "tracker@hackspace.org", rbac.PermAssetsRead, rbac.PermAssetsTrack)
	report, err := st.CreateReport("Who has what", []byte(`{"sections":[{"type":"table","columns":[{"heading":"Asset","binding":"{{asset.name}}"},{"heading":"Borrower","binding":"{{loan.borrower}}"}]}]}`))
	if err != nil {
		t.Fatal(err)
	}

	get := func(cookie *http.Cookie, path string, want int) string {
		t.Helper()
		rec := doJSON(t, srv, cookie, http.MethodGet, path, "")
		if rec.Code != want {
			t.Fatalf("%s: %d, want %d: %s", path, rec.Code, want, rec.Body)
		}
		if strings.Contains(rec.Body.String(), "@hackspace.org") {
			t.Errorf("%s shows an email: %s", path, rec.Body)
		}
		return rec.Body.String()
	}
	d := "/api/v1/assets/" + drill.Asset.ID
	run := "/api/v1/reports/" + report.ID + "/run"

	// A reader learns it is lent out, not to whom, nor who did what.
	if body := get(reader, d, http.StatusOK); !strings.Contains(body, `"loan":{`) || strings.Contains(body, "Molly") || strings.Contains(body, molly.ID) {
		t.Errorf("the drill, read: %s", body)
	}
	if body := get(reader, d+"/history", http.StatusOK); strings.Contains(body, `"actor"`) || strings.Contains(body, `"borrower"`) || strings.Contains(body, admin.ID) {
		t.Errorf("history, read: %s", body)
	}
	if body := get(reader, "/api/v1/assets", http.StatusOK); strings.Contains(body, "Molly") {
		t.Errorf("the list, read: %s", body)
	}
	if body := get(reader, run, http.StatusOK); strings.Contains(body, "Molly") || strings.Contains(body, UnnamedUser) {
		t.Errorf("a report, read: %s", body)
	}
	get(reader, "/api/v1/loans", http.StatusForbidden)

	// The borrower sees their own loan.
	if body := get(mollyCookie, d, http.StatusOK); !strings.Contains(body, molly.ID) {
		t.Errorf("the drill, by its borrower: %s", body)
	}

	// A tracker sees names.
	if body := get(tracker, d, http.StatusOK); !strings.Contains(body, `"name":"Molly Member"`) {
		t.Errorf("the drill, tracked: %s", body)
	}
	if body := get(tracker, d+"/history", http.StatusOK); !strings.Contains(body, `"borrower":{"id":"`+molly.ID+`","name":"Molly Member"}`) || !strings.Contains(body, `"actor":{"id":"`+admin.ID+`"`) {
		t.Errorf("history, tracked: %s", body)
	}
	if body := get(tracker, "/api/v1/loans", http.StatusOK); !strings.Contains(body, "Molly Member") || !strings.Contains(body, nameless.ID) {
		t.Errorf("loans, tracked: %s", body)
	}
	if body := get(tracker, run, http.StatusOK); !strings.Contains(body, "Molly Member") || !strings.Contains(body, UnnamedUser) {
		t.Errorf("a report, tracked: %s", body)
	}
}
