package api

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

func TestCloningACategoryOverTheAPI(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	src, err := srv.store.CreateCategory("Filament", nil, "#ffcc00", true, false)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := srv.store.CreateScope("workshop")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.store.SetCategoryScopes(src.ID, []string{scope.ID}); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/categories/" + src.ID + "/clone"

	var got struct{ Category models.Category }
	// With no body, it is named for its original.
	rec := do(t, srv, cookie, http.MethodPost, path, "", nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("clone: %d %s", rec.Code, rec.Body)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Category.Name != "Filament copy" || got.Category.ColorHex != "#ffcc00" || !got.Category.IsConsumable {
		t.Fatalf("clone: %+v, %v", got.Category, err)
	}

	// Named, by someone who manages categories but not scopes, who still
	// copies them: a copy without them would show what they hide.
	manager := userWithPermissions(t, srv, "cats@example.org", "assets.read", "categories.manage")
	rec = doJSON(t, srv, manager, http.MethodPost, path, `{"name": "Resin"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("clone by a manager: %d %s", rec.Code, rec.Body)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Category.Name != "Resin" {
		t.Fatalf("named clone: %+v, %v", got.Category, err)
	}
	if scopes, _ := srv.store.CategoryScopes(got.Category.ID); !reflect.DeepEqual(scopes, []string{scope.ID}) {
		t.Errorf("scopes: %v", scopes)
	}

	reader := userWithPermissions(t, srv, "reader@example.org", "assets.read")
	for _, c := range []struct {
		name   string
		cookie *http.Cookie
		path   string
		body   string
		want   int
	}{
		{"without categories.manage", reader, path, `{}`, http.StatusForbidden},
		{"a missing category", cookie, "/api/v1/categories/nope/clone", `{}`, http.StatusNotFound},
		{"Uncategorized", cookie, "/api/v1/categories/uncategorized/clone", `{}`, http.StatusBadRequest},
		{"broken JSON", cookie, path, `{"name":`, http.StatusBadRequest},
		{"a name not text", cookie, path, `{"name": 3}`, http.StatusBadRequest},
	} {
		if rec := doJSON(t, srv, c.cookie, http.MethodPost, c.path, c.body); rec.Code != c.want {
			t.Errorf("%s: %d, want %d: %s", c.name, rec.Code, c.want, rec.Body)
		}
	}
}

func TestMCPCloneCategory(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cs := mcpSession(t, srv, tokenWith(t, srv, admin.ID, "Curator", rbac.PermAssetsRead, rbac.PermCategoriesManage))
	src, err := srv.store.CreateCategory("Drills", nil, "", false, false)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := srv.store.CreateField("serial", "Serial", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.store.AssignCategoryField(src.ID, serial.ID, true); err != nil {
		t.Fatal(err)
	}

	var out struct{ Category models.Category }
	res := callTool(t, cs, "clone_category", map[string]any{"id": src.ID})
	structured(t, res, &out)
	if out.Category.Name != "Drills copy" || out.Category.ID == src.ID || !strings.Contains(toolText(res), `Made "Drills copy"`) {
		t.Fatalf("clone: %+v, %s", out.Category, toolText(res))
	}
	if fields, _ := srv.store.GetEffectiveFieldsForCategories([]string{out.Category.ID}); len(fields) != 1 || !fields[0].IsRequired {
		t.Errorf("fields: %+v", fields)
	}
	structured(t, callTool(t, cs, "clone_category", map[string]any{"id": src.ID, "name": "Saws"}), &out)
	if out.Category.Name != "Saws" {
		t.Errorf("named: %+v", out.Category)
	}
	if msg := toolError(t, cs, "clone_category", map[string]any{"id": "nope"}); !strings.Contains(msg, "not found") {
		t.Errorf("a missing category: %s", msg)
	}
}
