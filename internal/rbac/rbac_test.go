package rbac

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestResolveUnionPermissions(t *testing.T) {
	role1 := []string{PermAssetsRead, PermCategoriesManage}
	role2 := []string{PermAssetsWrite, PermTemplatesManage}
	role3 := []string{PermAssetsRead} // Duplicate in role1

	effective := ResolveUnionPermissions(role1, role2, role3)

	expected := []string{
		PermAssetsRead,
		PermCategoriesManage,
		PermAssetsWrite,
		PermTemplatesManage,
	}

	for _, exp := range expected {
		if !HasPermission(effective, exp) {
			t.Errorf("expected permission %s to be present in union", exp)
		}
	}

	if HasPermission(effective, PermUsersManage) {
		t.Errorf("expected permission %s NOT to be present", PermUsersManage)
	}
}

func TestAntiLockoutGuard(t *testing.T) {
	// Case 1: Removing an admin when 2 active admins exist -> Allowed
	if err := ValidateAntiLockout(2, true); err != nil {
		t.Errorf("expected operation to succeed when 2 admins exist, got %v", err)
	}

	// Case 2: Removing an admin when only 1 active admin exists -> Blocked
	err := ValidateAntiLockout(1, true)
	if err == nil {
		t.Fatal("expected anti-lockout error when removing sole active admin, got nil")
	}
	if !errors.Is(err, ErrAntiLockoutTriggered) {
		t.Errorf("expected ErrAntiLockoutTriggered, got %v", err)
	}

	// Case 3: Zero active admins (corrupted state or bootstrap) -> Blocked
	if err := ValidateAntiLockout(0, true); err == nil {
		t.Error("expected error when zero active admins exist")
	}

	// Case 4: Not removing an admin (e.g., editing a regular user) -> Allowed
	if err := ValidateAntiLockout(1, false); err != nil {
		t.Errorf("expected operation to succeed when not removing admin, got %v", err)
	}
}

func TestScopedPermissions(t *testing.T) {
	perms := map[string]bool{"tools:" + PermAssetsWrite: true, PermAssetsRead: true, "tools:" + PermUsersManage: true}
	cases := []struct {
		perm   string
		scopes []string
		want   bool
	}{
		{PermAssetsRead, []string{"anything"}, true},
		{PermAssetsWrite, []string{"tools"}, true},
		{PermAssetsWrite, []string{"kitchen", "tools"}, true},
		{PermAssetsWrite, []string{"kitchen"}, false},
		{PermAssetsWrite, []string{DefaultScope}, false},
		// Only asset permissions are scoped; a scoped key of another counts for nothing.
		{PermUsersManage, []string{"tools"}, false},
	}
	for _, c := range cases {
		if got := Allows(perms, c.perm, c.scopes); got != c.want {
			t.Errorf("Allows(%s, %v) = %v", c.perm, c.scopes, got)
		}
	}
	if !HasPermission(perms, PermAssetsWrite) || HasPermission(perms, PermAssetsDelete) || HasPermission(perms, PermUsersManage) {
		t.Error("HasPermission should hold a permission held in any scope, and only then")
	}
	if s, p := SplitKey("tools:assets.read"); s != "tools" || p != "assets.read" {
		t.Errorf("SplitKey: %q %q", s, p)
	}
	if s, p := SplitKey("assets.read"); s != "" || p != "assets.read" {
		t.Errorf("SplitKey unscoped: %q %q", s, p)
	}
}

// TestNarrow is the intersection table of #118: a role's permission in every
// scope, or in some, against the scopes an assignment names.
func TestNarrow(t *testing.T) {
	role := []string{PermAssetsRead, "tools:" + PermAssetsWrite, "kitchen:" + PermAssetsWrite, PermUsersManage, PermLabelsPrint}
	cases := []struct {
		name   string
		scopes []string
		want   []string
		out    []string
	}{
		{"unrestricted", nil, role, nil},
		{"every scope × one", []string{"tools"},
			[]string{"tools:" + PermAssetsRead, "tools:" + PermAssetsWrite}, []string{PermLabelsPrint, PermUsersManage}},
		{"some scopes × overlapping", []string{"tools", "workshop"},
			[]string{"tools:" + PermAssetsRead, "tools:" + PermAssetsWrite, "workshop:" + PermAssetsRead}, []string{PermLabelsPrint, PermUsersManage}},
		{"some scopes × disjoint", []string{"workshop"},
			[]string{"workshop:" + PermAssetsRead}, []string{PermAssetsWrite, PermLabelsPrint, PermUsersManage}},
		{"the reserved scopes", []string{DefaultScope, PublicScope},
			[]string{DefaultScope + ":" + PermAssetsRead, PublicScope + ":" + PermAssetsRead}, []string{PermAssetsWrite, PermLabelsPrint, PermUsersManage}},
	}
	for _, c := range cases {
		if got := Narrow(role, c.scopes); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: Narrow = %v, want %v", c.name, got, c.want)
		}
		if got := NarrowedOut(role, c.scopes); !reflect.DeepEqual(got, c.out) {
			t.Errorf("%s: NarrowedOut = %v, want %v", c.name, got, c.out)
		}
	}
	// What a narrowed assignment grants is checked as any other grant.
	perms := ResolveUnionPermissions(Narrow(role, []string{"tools"}))
	if !Allows(perms, PermAssetsWrite, []string{"tools"}) || Allows(perms, PermAssetsRead, []string{"kitchen"}) || HasPermission(perms, PermUsersManage) {
		t.Errorf("narrowed to tools: %v", perms)
	}
}

func TestEffectiveScopes(t *testing.T) {
	parents := map[string]string{"drills": "tools", "tools": "", "food": ""}
	own := map[string][]string{"tools": {"workshop"}, "drills": {"power", "workshop"}}
	parent := func(id string) string { return parents[id] }
	ownOf := func(id string) []string { return own[id] }
	if got := EffectiveScopes([]string{"drills"}, parent, ownOf); !reflect.DeepEqual(got, []string{"power", "workshop"}) {
		t.Errorf("cascade: %v", got)
	}
	if got := EffectiveScopes([]string{"food"}, parent, ownOf); !reflect.DeepEqual(got, []string{DefaultScope}) {
		t.Errorf("no scope: %v", got)
	}
	if got := EffectiveScopes(nil, parent, ownOf); !reflect.DeepEqual(got, []string{DefaultScope}) {
		t.Errorf("no categories: %v", got)
	}
}

func TestScopeNames(t *testing.T) {
	for name, want := range map[string]bool{"tools": true, "wood-shop_2": true, "": false, "Tools": false, "-x": false, "a:b": false, "a.b": false, strings.Repeat("a", 65): false} {
		if ValidScopeName(name) != want {
			t.Errorf("ValidScopeName(%q) != %v", name, want)
		}
	}
}
