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

func TestLendingAPI(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	adminCookie := sessionCookie(t, srv, admin.ID)
	member := userWithPermissions(t, srv, "member@hackspace.org", rbac.PermAssetsRead, rbac.PermAssetsBorrow)
	lender := userWithPermissions(t, srv, "stores@hackspace.org", rbac.PermAssetsRead, rbac.PermAssetsLend)
	reader := userWithPermissions(t, srv, "reader@hackspace.org", rbac.PermAssetsRead)
	memberUser, err := srv.store.GetUserByEmail("member@hackspace.org")
	if err != nil {
		t.Fatal(err)
	}

	tools, err := srv.store.CreateCategoryFrom(store.CategoryInput{Name: "Tools", IsLendable: true})
	if err != nil {
		t.Fatal(err)
	}
	rec := doJSON(t, srv, adminCookie, http.MethodGet, "/api/v1/categories/"+tools.ID, "")
	if rec.Code != http.StatusOK || !json.Valid(rec.Body.Bytes()) || !containsJSON(rec.Body.Bytes(), `"is_lendable":true`) {
		t.Errorf("category: %s", rec.Body)
	}
	assetOf := func(body []byte) models.AssetWithDetails {
		t.Helper()
		var v struct{ Asset models.AssetWithDetails }
		if err := json.Unmarshal(body, &v); err != nil {
			t.Fatal(err)
		}
		return v.Asset
	}
	rec = doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/assets", `{"name":"Drill","category_ids":["`+tools.ID+`"]}`)
	drill := assetOf(rec.Body.Bytes())
	if !drill.Lendable || drill.Loan != nil {
		t.Fatalf("a new drill: %s", rec.Body)
	}
	out := "/api/v1/assets/" + drill.Asset.ID + "/checkout"
	in := "/api/v1/assets/" + drill.Asset.ID + "/checkin"

	if rec := doJSON(t, srv, reader, http.MethodPost, out, ""); rec.Code != http.StatusForbidden {
		t.Errorf("a reader borrowed: %d", rec.Code)
	}
	// A member borrows for themselves, not for others.
	if rec := doJSON(t, srv, member, http.MethodPost, out, `{"borrower_id":"`+admin.ID+`"}`); rec.Code != http.StatusForbidden {
		t.Errorf("a member lent to someone else: %d", rec.Code)
	}
	due := time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339)
	rec = doJSON(t, srv, member, http.MethodPost, out, `{"due_at":"`+due+`"}`)
	if a := assetOf(rec.Body.Bytes()); rec.Code != http.StatusOK || a.Loan == nil || a.Loan.Borrower.ID != memberUser.ID || a.Loan.DueAt == nil {
		t.Fatalf("borrow: %d %s", rec.Code, rec.Body)
	}
	for body, want := range map[string]int{
		`{}`:                http.StatusConflict,
		`{"due_at":"soon"}`: http.StatusBadRequest,
	} {
		if rec := doJSON(t, srv, lender, http.MethodPost, out, body); rec.Code != want {
			t.Errorf("%s: %d %s, want %d", body, rec.Code, rec.Body, want)
		}
	}
	if rec := doJSON(t, srv, adminCookie, http.MethodDelete, "/api/v1/assets/"+drill.Asset.ID, ""); rec.Code != http.StatusConflict {
		t.Errorf("archiving a lent asset: %d %s", rec.Code, rec.Body)
	}

	// The list and loans show it.
	rec = doJSON(t, srv, reader, http.MethodGet, "/api/v1/assets?loan=open", "")
	var listed struct{ Assets []models.AssetWithDetails }
	_ = json.Unmarshal(rec.Body.Bytes(), &listed)
	if len(listed.Assets) != 1 || listed.Assets[0].Loan == nil {
		t.Errorf("lent out: %s", rec.Body)
	}
	if rec := doJSON(t, srv, reader, http.MethodGet, "/api/v1/assets?loan=soon", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("an unknown loan filter: %d", rec.Code)
	}
	rec = doJSON(t, srv, reader, http.MethodGet, "/api/v1/loans?borrower_id="+memberUser.ID+"&state=open", "")
	var loans struct{ Loans []models.Loan }
	_ = json.Unmarshal(rec.Body.Bytes(), &loans)
	if rec.Code != http.StatusOK || len(loans.Loans) != 1 || loans.Loans[0].AssetName != "Drill" {
		t.Errorf("loans: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, reader, http.MethodGet, "/api/v1/loans?state=lost", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("an unknown state: %d", rec.Code)
	}

	// The member checks in their own loan; the lender checks in anyone's.
	if rec := doJSON(t, srv, member, http.MethodPost, in, ""); rec.Code != http.StatusOK {
		t.Errorf("checking in one's own: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, member, http.MethodPost, in, ""); rec.Code != http.StatusConflict {
		t.Errorf("checking in twice: %d", rec.Code)
	}
	if rec := doJSON(t, srv, lender, http.MethodPost, out, `{"borrower_id":"`+admin.ID+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("lending to the admin: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, member, http.MethodPost, in, ""); rec.Code != http.StatusForbidden {
		t.Errorf("checking in someone else's: %d", rec.Code)
	}
	if rec := doJSON(t, srv, lender, http.MethodPost, in, ""); rec.Code != http.StatusOK {
		t.Errorf("a lender checking in: %d %s", rec.Code, rec.Body)
	}

	// Lenders see who they can lend to; readers do not.
	if rec := doJSON(t, srv, lender, http.MethodGet, "/api/v1/people", ""); rec.Code != http.StatusOK || !containsJSON(rec.Body.Bytes(), "member@hackspace.org") || containsJSON(rec.Body.Bytes(), "password") {
		t.Errorf("people: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, reader, http.MethodGet, "/api/v1/people", ""); rec.Code != http.StatusForbidden {
		t.Errorf("a reader listed people: %d", rec.Code)
	}

	// An API token lends, but must name the borrower.
	role, err := srv.store.CreateRole(store.RoleInput{Name: "Lenders", Permissions: []string{rbac.PermAssetsLend}})
	if err != nil {
		t.Fatal(err)
	}
	_, secret, err := srv.store.CreateToken("Kiosk", []string{role.ID}, nil, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rec := withBearer(t, srv, secret, http.MethodPost, out, ""); rec.Code != http.StatusBadRequest {
		t.Errorf("a token borrowing for itself: %d", rec.Code)
	}
	if rec := withBearer(t, srv, secret, http.MethodPost, out, `{"borrower_id":"`+memberUser.ID+`"}`); rec.Code != http.StatusOK {
		t.Errorf("a token lending: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/assets/missing/checkout", ""); rec.Code != http.StatusNotFound {
		t.Errorf("a missing asset: %d", rec.Code)
	}
}

func containsJSON(body []byte, s string) bool {
	return json.Valid(body) && strings.Contains(string(body), s)
}
