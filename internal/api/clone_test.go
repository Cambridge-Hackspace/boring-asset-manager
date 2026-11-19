package api

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
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

func TestCloningATemplateOverTheAPI(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	if _, err := srv.store.CreateField("location", "Location", "text", nil); err != nil {
		t.Fatal(err)
	}
	src := createTemplate(t, srv, cookie, shelfTemplate)
	cat, err := srv.store.CreateCategory("Shelves", nil, "", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.store.SetCategoryLabelTemplates(cat.ID, []string{src}); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/templates/" + src + "/clone"

	// With no body, it is named for its original, and has everything else.
	rec := do(t, srv, cookie, http.MethodPost, path, "", nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("clone: %d %s", rec.Code, rec.Body)
	}
	var got struct{ Template labels.Template }
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Template.Name != "Shelf copy" || got.Template.ID == src {
		t.Fatalf("clone: %+v, %v", got.Template, err)
	}
	orig, err := srv.store.GetTemplate(src)
	if err != nil {
		t.Fatal(err)
	}
	copied, err := srv.store.GetTemplate(got.Template.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(copied.Elements, orig.Elements) || copied.Width != orig.Width || copied.Height != orig.Height || copied.DPI != orig.DPI || copied.Units != orig.Units {
		t.Errorf("copy differs: %+v, original %+v", copied, orig)
	}
	// The category that used the original still does, and only it.
	if after, err := srv.store.GetCategoryByID(cat.ID); err != nil || !reflect.DeepEqual(after.LabelTemplateIDs, []string{src}) {
		t.Errorf("category's templates: %+v, %v", after, err)
	}

	rec = doJSON(t, srv, cookie, http.MethodPost, path, `{"name": "  Bin  "}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("named clone: %d %s", rec.Code, rec.Body)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Template.Name != "Bin" {
		t.Fatalf("named clone: %+v, %v", got.Template, err)
	}

	reader := userWithPermissions(t, srv, "reader@example.org", "assets.read")
	for _, c := range []struct {
		name   string
		cookie *http.Cookie
		path   string
		body   string
		want   int
	}{
		{"without templates.manage", reader, path, `{}`, http.StatusForbidden},
		{"a missing template", cookie, "/api/v1/templates/nope/clone", `{}`, http.StatusNotFound},
		{"broken JSON", cookie, path, `{"name":`, http.StatusBadRequest},
		{"a name not text", cookie, path, `{"name": 3}`, http.StatusBadRequest},
		{"a name too long", cookie, path, `{"name": "` + strings.Repeat("a", 1000) + `"}`, http.StatusBadRequest},
	} {
		if rec := doJSON(t, srv, c.cookie, http.MethodPost, c.path, c.body); rec.Code != c.want {
			t.Errorf("%s: %d, want %d: %s", c.name, rec.Code, c.want, rec.Body)
		}
	}
}

func TestMCPCloneTemplate(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	cs := mcpSession(t, srv, tokenWith(t, srv, admin.ID, "Designer", rbac.PermAssetsRead, rbac.PermTemplatesManage))
	if _, err := srv.store.CreateField("location", "Location", "text", nil); err != nil {
		t.Fatal(err)
	}
	src := createTemplate(t, srv, sessionCookie(t, srv, admin.ID), shelfTemplate)

	var out struct{ Template labels.Template }
	res := callTool(t, cs, "clone_template", map[string]any{"id": src})
	structured(t, res, &out)
	if out.Template.Name != "Shelf copy" || out.Template.ID == src || len(out.Template.Elements) != 2 || !strings.Contains(toolText(res), `Made "Shelf copy"`) {
		t.Fatalf("clone: %+v, %s", out.Template, toolText(res))
	}
	structured(t, callTool(t, cs, "clone_template", map[string]any{"id": src, "name": "Bin"}), &out)
	if out.Template.Name != "Bin" {
		t.Errorf("named: %+v", out.Template)
	}
	if msg := toolError(t, cs, "clone_template", map[string]any{"id": "nope"}); !strings.Contains(msg, "not found") {
		t.Errorf("a missing template: %s", msg)
	}
}
