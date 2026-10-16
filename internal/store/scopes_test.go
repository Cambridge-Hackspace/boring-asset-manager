package store

import (
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

func TestAssetsAreInTheScopesOfTheirCategories(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	workshop, err := st.CreateScope("workshop")
	if err != nil {
		t.Fatal(err)
	}
	power, _ := st.CreateScope("power")
	tools, _ := st.CreateCategory("Tools", nil, "", false, false)
	drills, _ := st.CreateCategory("Drills", &tools.ID, "", false, false)
	food, _ := st.CreateCategory("Food", nil, "", false, false)
	if _, err := st.SetCategoryScopes(tools.ID, []string{workshop.ID}); err != nil {
		t.Fatal(err)
	}
	if got, err := st.SetCategoryScopes(drills.ID, []string{power.ID, power.ID}); err != nil || len(got) != 1 {
		t.Fatalf("drills: %v, %v", got, err)
	}

	for name, c := range map[string]struct {
		cats []string
		want []string
	}{
		"a drill, from its category and the one above": {[]string{drills.ID}, []string{"power", "workshop"}},
		"in a category with no scope":                  {[]string{food.ID}, []string{rbac.DefaultScope}},
		"in no category":                               {nil, []string{rbac.DefaultScope}},
		"in both":                                      {[]string{food.ID, tools.ID}, []string{"workshop"}},
	} {
		a, err := st.CreateAsset(name, c.cats, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(a.Scopes, c.want) {
			t.Errorf("%s: read alone, %v", name, a.Scopes)
		}
		list, _ := st.ListAssets("", name)
		if len(list) != 1 || !reflect.DeepEqual(list[0].Scopes, c.want) {
			t.Errorf("%s: in a list, %+v", name, list)
		}
		if got, _ := st.AssetScopes([]string{a.Asset.ID, "nobody"}); !reflect.DeepEqual(got[a.Asset.ID], c.want) || len(got) != 1 {
			t.Errorf("%s: AssetScopes %v", name, got)
		}
		if got, _ := st.ScopesForCategories(c.cats); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: ScopesForCategories %v", name, got)
		}
	}

	// Taking the label off the parent leaves the drill in power alone.
	if _, err := st.SetCategoryScopes(tools.ID, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.ScopesForCategories([]string{drills.ID}); !reflect.DeepEqual(got, []string{"power"}) {
		t.Errorf("after unlabeling: %v", got)
	}
}

func TestManagingScopes(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	scopes, err := st.ListScopes()
	// Built in: default, and public, which the public role reads in.
	if err != nil || len(scopes) != 2 || scopes[0].Name != rbac.DefaultScope || scopes[1].Name != rbac.PublicScope ||
		!scopes[0].Reserved || !scopes[1].Reserved || scopes[1].RoleCount != 1 || scopes[1].CategoryCount != 0 {
		t.Fatalf("seeded: %+v, %v", scopes, err)
	}
	def, public := scopes[0], scopes[1]
	for what, err := range map[string]error{
		"renaming public": func() error { _, err := st.RenameScope(public.ID, "other"); return err }(),
		"deleting public": st.DeleteScope(public.ID),
	} {
		if !errors.Is(err, ErrScopeReserved) {
			t.Errorf("%s: %v", what, err)
		}
	}
	// Unlike default, public goes on categories: that is how things become public.
	shelf, _ := st.CreateCategory("Shelf", nil, "", false, false)
	if _, err := st.SetCategoryScopes(shelf.ID, []string{public.ID}); err != nil {
		t.Errorf("public on a category: %v", err)
	}
	for _, name := range []string{"", "Tools", "a b", "a:b", strings.Repeat("a", 65)} {
		if _, err := st.CreateScope(name); !errors.Is(err, ErrInvalidScope) {
			t.Errorf("%q: %v", name, err)
		}
	}
	if _, err := st.CreateScope(rbac.DefaultScope); !IsUniqueViolation(err) {
		t.Errorf("a second default: %v", err)
	}
	for what, err := range map[string]error{
		"rename": func() error { _, err := st.RenameScope(def.ID, "other"); return err }(),
		"delete": st.DeleteScope(def.ID),
		"put on category": func() error {
			c, _ := st.CreateCategory("C", nil, "", false, false)
			_, err := st.SetCategoryScopes(c.ID, []string{def.ID})
			return err
		}(),
	} {
		if !errors.Is(err, ErrScopeReserved) {
			t.Errorf("the default scope, %s: %v", what, err)
		}
	}

	// A scope in use stays until nothing uses it; renaming carries roles along.
	tools, _ := st.CreateScope(" tools ")
	cat, _ := st.CreateCategory("Tools", nil, "", false, false)
	if _, err := st.SetCategoryScopes(cat.ID, []string{tools.ID}); err != nil {
		t.Fatal(err)
	}
	role, err := st.CreateRole(RoleInput{Name: "Tool keepers", Permissions: []string{"tools:" + rbac.PermAssetsWrite}})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := st.GetScope(tools.ID); got.Name != "tools" || got.CategoryCount != 1 || got.RoleCount != 1 {
		t.Errorf("counts: %+v", got)
	}
	if err := st.DeleteScope(tools.ID); !errors.Is(err, ErrScopeInUse) {
		t.Errorf("deleting in use: %v", err)
	}
	if _, err := st.RenameScope(tools.ID, "workshop"); err != nil {
		t.Fatal(err)
	}
	if r, _ := st.GetRole(role.ID); !reflect.DeepEqual(r.Permissions, []string{"workshop:" + rbac.PermAssetsWrite}) {
		t.Errorf("after renaming: %v", r.Permissions)
	}
	if _, err := st.SetCategoryScopes(cat.ID, nil); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteRole(role.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteScope(tools.ID); err != nil {
		t.Errorf("deleting unused: %v", err)
	}
	if _, err := st.GetScope(tools.ID); !errors.Is(err, ErrScopeNotFound) {
		t.Errorf("after deleting: %v", err)
	}
}

func TestScopedRolePermissions(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	if _, err := st.CreateScope("tools"); err != nil {
		t.Fatal(err)
	}
	for what, perms := range map[string][]string{
		"an unscopable permission": {"tools:" + rbac.PermUsersManage},
		"an unknown scope":         {"kitchen:" + rbac.PermAssetsRead},
		"an unknown permission":    {"tools:assets.fly"},
	} {
		if _, err := st.CreateRole(RoleInput{Name: what, Permissions: perms}); !errors.Is(err, ErrInvalidRole) {
			t.Errorf("%s: %v", what, err)
		}
	}

	// A permission in every scope covers it in one.
	role, err := st.CreateRole(RoleInput{Name: "Keepers", Permissions: []string{
		"tools:" + rbac.PermAssetsWrite, rbac.PermAssetsRead, "tools:" + rbac.PermAssetsRead, "default:" + rbac.PermAssetsLend,
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{rbac.PermAssetsRead, "default:" + rbac.PermAssetsLend, "tools:" + rbac.PermAssetsWrite}
	if !reflect.DeepEqual(role.Permissions, want) {
		t.Errorf("role: %v", role.Permissions)
	}

	u, _ := st.CreateNamedUser("ada@hackspace.org", "Ada", "a long enough password", true)
	if err := st.SetUserRoles(u.ID, []string{role.ID}); err != nil {
		t.Fatal(err)
	}
	perms, err := st.GetUserPermissions(u.ID)
	if err != nil || !reflect.DeepEqual(perms, map[string]bool{want[0]: true, want[1]: true, want[2]: true}) {
		t.Errorf("user: %v, %v", perms, err)
	}
	_, secret, err := st.CreateToken("Script", []string{role.ID}, nil, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, perms, err := st.AuthenticateToken(secret); err != nil || !reflect.DeepEqual(perms, map[string]bool{want[0]: true, want[1]: true, want[2]: true}) {
		t.Errorf("token: %v, %v", perms, err)
	}

	// Editing the role replaces both kinds of grant.
	if r, err := st.UpdateRole(role.ID, RoleInput{Name: "Keepers", Permissions: []string{"tools:" + rbac.PermAssetsRead}}); err != nil || !reflect.DeepEqual(r.Permissions, []string{"tools:" + rbac.PermAssetsRead}) {
		t.Errorf("edited: %+v, %v", r, err)
	}
}

func TestPermissionKeysAreDotted(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	var colons int
	if err := st.QueryRow("SELECT COUNT(*) FROM permissions WHERE permission_key LIKE '%:%'").Scan(&colons); err != nil || colons != 0 {
		t.Errorf("keys with colons: %d, %v", colons, err)
	}
	admin, _ := st.GetRoleByName(AdminRoleName)
	if r, _ := st.GetRole(admin.ID); !reflect.DeepEqual(r.Permissions, sortedCopy(rbac.AllPermissions())) {
		t.Errorf("admin: %v", r.Permissions)
	}
	var desc string
	if err := st.QueryRow("SELECT description FROM permissions WHERE permission_key = ?", rbac.PermStockRecord).Scan(&desc); err != nil || desc != "Core permission: stock.record" {
		t.Errorf("description: %q, %v", desc, err)
	}
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// TestAssetsTrackIsGrantedToRolesThatRead runs migration 020 again on roles
// made before it, as an upgrade would: whoever read assets keeps seeing the
// people around them, in the same scopes.
func TestAssetsTrackIsGrantedToRolesThatRead(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	if _, err := st.CreateScope("tools"); err != nil {
		t.Fatal(err)
	}
	readers, _ := st.CreateRole(RoleInput{Name: "Readers", Permissions: []string{rbac.PermAssetsRead}})
	keepers, _ := st.CreateRole(RoleInput{Name: "Keepers", Permissions: []string{"tools:" + rbac.PermAssetsRead, rbac.PermLabelsPrint}})
	printers, _ := st.CreateRole(RoleInput{Name: "Printers", Permissions: []string{rbac.PermLabelsPrint}})

	for _, stmt := range []string{
		"DELETE FROM permissions WHERE permission_key = 'assets.track'",
		"DELETE FROM schema_migrations WHERE version = 20",
	} {
		if _, err := st.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ { // and again, changing nothing more
		if err := st.Migrate(); err != nil {
			t.Fatal(err)
		}
		if _, err := st.Exec("DELETE FROM schema_migrations WHERE version = 20"); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		id   string
		want []string
	}{
		{readers.ID, []string{rbac.PermAssetsRead, rbac.PermAssetsTrack}},
		{keepers.ID, []string{rbac.PermLabelsPrint, "tools:" + rbac.PermAssetsRead, "tools:" + rbac.PermAssetsTrack}},
		{printers.ID, []string{rbac.PermLabelsPrint}},
	} {
		if r, _ := st.GetRole(c.id); !reflect.DeepEqual(r.Permissions, c.want) {
			t.Errorf("%s: %v", r.Name, r.Permissions)
		}
	}
}

// TestAPublicRoleFromBeforeStaysTheirs runs migration 021 on a database
// where someone had named a role and a scope public: theirs are renamed,
// so what they allowed never passes to everyone.
func TestAPublicRoleFromBeforeStaysTheirs(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	for _, stmt := range []string{
		"DELETE FROM roles WHERE name = 'public'",
		"DELETE FROM scopes WHERE name = 'public'",
		"DELETE FROM schema_migrations WHERE version = 21",
	} {
		if _, err := st.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	theirScope, err := st.CreateScope("public")
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := st.CreateRole(RoleInput{Name: "public", Permissions: []string{rbac.PermAssetsWrite, "public:" + rbac.PermAssetsDelete}})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	if r, _ := st.GetRole(theirs.ID); r.Name != "public (renamed)" || !reflect.DeepEqual(r.Permissions, []string{rbac.PermAssetsWrite, "public-renamed:" + rbac.PermAssetsDelete}) {
		t.Errorf("theirs: %+v", r)
	}
	if sc, _ := st.GetScope(theirScope.ID); sc.Name != "public-renamed" {
		t.Errorf("their scope: %+v", sc)
	}
	public, err := st.PublicPermissions()
	if err != nil || !reflect.DeepEqual(public, map[string]bool{"public:" + rbac.PermAssetsRead: true}) {
		t.Errorf("the public role: %v, %v", public, err)
	}
}
