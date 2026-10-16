package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

func TestAssetLinksAPI(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	adminCookie := sessionCookie(t, srv, admin.ID)
	writer := userWithPermissions(t, srv, "writer@hackspace.org", rbac.PermAssetsRead, rbac.PermAssetsWrite)
	reader := userWithPermissions(t, srv, "reader@hackspace.org", rbac.PermAssetsRead)

	rec := doJSON(t, srv, reader, http.MethodGet, "/api/v1/relation-types", "")
	var types struct {
		RelationTypes []models.RelationType `json:"relation_types"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &types)
	contains := ""
	for _, r := range types.RelationTypes {
		if r.Name == "contains" {
			contains = r.ID
		}
	}
	if rec.Code != http.StatusOK || len(types.RelationTypes) != 3 || contains == "" {
		t.Fatalf("relation types: %d %s", rec.Code, rec.Body)
	}

	// Types are managed with categories:manage.
	body := `{"name":"powers","inverse_name":"powered by","is_single":true}`
	if rec := doJSON(t, srv, writer, http.MethodPost, "/api/v1/relation-types", body); rec.Code != http.StatusForbidden {
		t.Errorf("a writer made a type: %d", rec.Code)
	}
	rec = doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/relation-types", body)
	var made struct {
		RelationType models.RelationType `json:"relation_type"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &made)
	if rec.Code != http.StatusCreated || made.RelationType.Name != "powers" {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	for bad, want := range map[string]int{
		body: http.StatusConflict,
		`{"name":"x","inverse_name":"y","is_location":true}`: http.StatusBadRequest,
		`nope`: http.StatusBadRequest,
	} {
		if rec := doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/relation-types", bad); rec.Code != want {
			t.Errorf("%s: %d %s, want %d", bad, rec.Code, rec.Body, want)
		}
	}

	newAsset := func(name string) string {
		t.Helper()
		rec := doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/assets", `{"name":"`+name+`"}`)
		var v struct{ Asset models.AssetWithDetails }
		_ = json.Unmarshal(rec.Body.Bytes(), &v)
		return v.Asset.Asset.ID
	}
	bin, screws := newAsset("Bin A3"), newAsset("M3 screws")

	// Links need assets:write, and read from the asset the request names.
	path := "/api/v1/assets/" + screws + "/links"
	linkBody := `{"type_id":"` + contains + `","asset_id":"` + bin + `","outgoing":false}`
	if rec := doJSON(t, srv, reader, http.MethodPost, path, linkBody); rec.Code != http.StatusForbidden {
		t.Errorf("a reader linked assets: %d", rec.Code)
	}
	rec = doJSON(t, srv, writer, http.MethodPost, path, linkBody)
	var linked struct{ Asset models.AssetWithDetails }
	_ = json.Unmarshal(rec.Body.Bytes(), &linked)
	if rec.Code != http.StatusOK || len(linked.Asset.Location) != 1 || linked.Asset.Location[0].Name != "Bin A3" || linked.Asset.Links[0].Label != "contained in" {
		t.Fatalf("link: %d %s", rec.Code, rec.Body)
	}
	for bad, want := range map[string]int{
		linkBody: http.StatusConflict,
		`{"type_id":"` + contains + `","asset_id":"` + screws + `"}`: http.StatusBadRequest,
		`{"type_id":"missing","asset_id":"` + bin + `"}`:             http.StatusNotFound,
		`{"type_id":"` + contains + `","asset_id":"missing"}`:        http.StatusNotFound,
		`{}`: http.StatusBadRequest,
	} {
		if rec := doJSON(t, srv, writer, http.MethodPost, path, bad); rec.Code != want {
			t.Errorf("%s: %d %s, want %d", bad, rec.Code, rec.Body, want)
		}
	}
	// A loop is refused.
	if rec := doJSON(t, srv, writer, http.MethodPost, "/api/v1/assets/"+bin+"/links", `{"type_id":"`+contains+`","asset_id":"`+screws+`","outgoing":false}`); rec.Code != http.StatusConflict {
		t.Errorf("a loop: %d %s", rec.Code, rec.Body)
	}

	// The bin cannot be archived while it holds the screws.
	if rec := doJSON(t, srv, adminCookie, http.MethodDelete, "/api/v1/assets/"+bin, ""); rec.Code != http.StatusConflict {
		t.Errorf("archiving a full bin: %d %s", rec.Code, rec.Body)
	}
	// A type in use cannot be deleted.
	if rec := doJSON(t, srv, adminCookie, http.MethodDelete, "/api/v1/relation-types/"+contains, ""); rec.Code != http.StatusConflict {
		t.Errorf("deleting a type in use: %d", rec.Code)
	}

	linkID := linked.Asset.Links[0].ID
	if rec := doJSON(t, srv, writer, http.MethodDelete, path+"/missing", ""); rec.Code != http.StatusNotFound {
		t.Errorf("a missing link: %d", rec.Code)
	}
	if rec := doJSON(t, srv, writer, http.MethodDelete, path+"/"+linkID, ""); rec.Code != http.StatusOK {
		t.Errorf("unlink: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, adminCookie, http.MethodDelete, "/api/v1/assets/"+bin, ""); rec.Code != http.StatusOK {
		t.Errorf("archiving the empty bin: %d %s", rec.Code, rec.Body)
	}

	// Categories suggest types.
	rec = doJSON(t, srv, adminCookie, http.MethodPost, "/api/v1/categories", `{"name":"Storage"}`)
	var cat struct{ Category models.Category }
	_ = json.Unmarshal(rec.Body.Bytes(), &cat)
	catPath := "/api/v1/categories/" + cat.Category.ID + "/relation-types"
	if rec := doJSON(t, srv, writer, http.MethodPut, catPath, `{"relation_type_ids":["`+contains+`"]}`); rec.Code != http.StatusForbidden {
		t.Errorf("a writer set suggestions: %d", rec.Code)
	}
	for bad, want := range map[string]int{`{}`: http.StatusBadRequest, `{"relation_type_ids":["missing"]}`: http.StatusNotFound} {
		if rec := doJSON(t, srv, adminCookie, http.MethodPut, catPath, bad); rec.Code != want {
			t.Errorf("%s: %d", bad, rec.Code)
		}
	}
	if rec := doJSON(t, srv, adminCookie, http.MethodPut, catPath, `{"relation_type_ids":["`+contains+`"]}`); rec.Code != http.StatusOK {
		t.Errorf("suggest: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, reader, http.MethodGet, catPath, ""); rec.Code != http.StatusOK || !containsJSON(rec.Body.Bytes(), contains) {
		t.Errorf("suggestions: %d %s", rec.Code, rec.Body)
	}
}
