package store

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

func TestRoleManagement(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	stores, err := st.CreateRole(RoleInput{Name: " Stores ", Description: "Records stock", Permissions: []string{rbac.PermStockRecord, rbac.PermAssetsRead, rbac.PermAssetsRead}})
	if err != nil {
		t.Fatal(err)
	}
	if stores.Name != "Stores" || !reflect.DeepEqual(stores.Permissions, []string{rbac.PermAssetsRead, rbac.PermStockRecord}) || stores.Protected {
		t.Errorf("created %+v", stores)
	}
	for what, in := range map[string]RoleInput{
		"no name":            {Name: " "},
		"a long name":        {Name: strings.Repeat("r", MaxRoleName+1)},
		"an unknown grant":   {Name: "Odd", Permissions: []string{"assets:fly"}},
		"a long description": {Name: "Odd", Description: strings.Repeat("d", 256)},
	} {
		if _, err := st.CreateRole(in); !errors.Is(err, ErrInvalidRole) {
			t.Errorf("%s: %v", what, err)
		}
	}
	if _, err := st.CreateRole(RoleInput{Name: "Stores"}); !IsUniqueViolation(err) {
		t.Errorf("a name in use: %v", err)
	}

	// Editing renames it and replaces its permissions; holders are counted.
	u, err := st.CreateUser("bo@hackspace.org", "a long enough password", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetUserRoles(u.ID, []string{stores.ID}); err != nil {
		t.Fatal(err)
	}
	edited, err := st.UpdateRole(stores.ID, RoleInput{Name: "Readers", Permissions: []string{rbac.PermAssetsRead}})
	if err != nil || edited.Name != "Readers" || edited.Description != "" || !reflect.DeepEqual(edited.Permissions, []string{rbac.PermAssetsRead}) || edited.UserCount != 1 {
		t.Fatalf("edited %+v, %v", edited, err)
	}
	if perms, _ := st.GetUserPermissions(u.ID); perms[rbac.PermStockRecord] || !perms[rbac.PermAssetsRead] {
		t.Errorf("bo's permissions after the edit: %v", perms)
	}
	if _, err := st.UpdateRole("no-such-role", RoleInput{Name: "x"}); !errors.Is(err, ErrRoleNotFound) {
		t.Errorf("a missing role: %v", err)
	}

	// The admin role cannot be changed.
	admin, err := st.GetRoleByName(AdminRoleName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpdateRole(admin.ID, RoleInput{Name: "boss"}); !errors.Is(err, ErrRoleProtected) {
		t.Errorf("editing admin: %v", err)
	}
	if err := st.DeleteRole(admin.ID); !errors.Is(err, ErrRoleProtected) {
		t.Errorf("deleting admin: %v", err)
	}
	if got, _ := st.GetRole(admin.ID); got == nil || !got.Protected {
		t.Errorf("admin listed as %+v", got)
	}

	// Deleting a role takes it from its holders.
	if err := st.DeleteRole(stores.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetRole(stores.ID); !errors.Is(err, ErrRoleNotFound) {
		t.Errorf("still there: %v", err)
	}
	if ids, _ := st.UserRoleIDs(); len(ids[u.ID]) != 0 {
		t.Errorf("bo still holds %v", ids[u.ID])
	}
	if err := st.DeleteRole(stores.ID); !errors.Is(err, ErrRoleNotFound) {
		t.Errorf("deleting twice: %v", err)
	}
}

func TestRoleChangesFollowTheAntiLockoutRule(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	// The only person who can manage users has it through a role of their own.
	managers, err := st.CreateRole(RoleInput{Name: "Managers", Permissions: []string{rbac.PermUsersManage}})
	if err != nil {
		t.Fatal(err)
	}
	u, err := st.CreateUser("ada@hackspace.org", "a long enough password", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetUserRoles(u.ID, []string{managers.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpdateRole(managers.ID, RoleInput{Name: "Managers", Permissions: []string{rbac.PermAssetsRead}}); !errors.Is(err, rbac.ErrAntiLockoutTriggered) {
		t.Errorf("taking users:manage from the last manager: %v", err)
	}
	if err := st.DeleteRole(managers.ID); !errors.Is(err, rbac.ErrAntiLockoutTriggered) {
		t.Errorf("deleting the last manager's role: %v", err)
	}
	if got, _ := st.GetRole(managers.ID); got == nil || !reflect.DeepEqual(got.Permissions, []string{rbac.PermUsersManage}) {
		t.Errorf("a refused change left %+v", got)
	}

	// With another manager, both are allowed.
	if _, err := st.CreateAdminUser("grace@hackspace.org", "a long enough password"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpdateRole(managers.ID, RoleInput{Name: "Managers", Permissions: []string{rbac.PermAssetsRead}}); err != nil {
		t.Errorf("with another manager: %v", err)
	}
	if err := st.DeleteRole(managers.ID); err != nil {
		t.Errorf("deleting with another manager: %v", err)
	}
}

func TestStockRecordIsGrantedToRolesThatCouldWrite(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	writers, err := st.CreateRole(RoleInput{Name: "Writers", Permissions: []string{rbac.PermAssetsRead, rbac.PermAssetsWrite}})
	if err != nil {
		t.Fatal(err)
	}
	readers, err := st.CreateRole(RoleInput{Name: "Readers", Permissions: []string{rbac.PermAssetsRead}})
	if err != nil {
		t.Fatal(err)
	}
	rerun := func(step string) {
		t.Helper()
		if _, err := st.Exec("DELETE FROM schema_migrations WHERE version = 11"); err != nil {
			t.Fatal(err)
		}
		if err := st.Migrate(); err != nil {
			t.Fatalf("%s: %v", step, err)
		}
		if err := st.SeedDefaultPermissionsAndRoles(); err != nil {
			t.Fatal(err)
		}
		for _, c := range []struct {
			id   string
			want bool
		}{{writers.ID, true}, {readers.ID, false}} {
			r, err := st.GetRole(c.id)
			if err != nil {
				t.Fatal(err)
			}
			has := false
			for _, p := range r.Permissions {
				has = has || p == rbac.PermStockRecord
			}
			if has != c.want {
				t.Errorf("%s: %s has stock:record: %v, want %v", step, r.Name, has, c.want)
			}
		}
		admin, _ := st.GetRoleByName(AdminRoleName)
		if r, _ := st.GetRole(admin.ID); len(r.Permissions) != len(rbac.AllPermissions()) {
			t.Errorf("%s: admin has %v", step, r.Permissions)
		}
	}

	// As the database was before migration 011: no stock:record at all.
	if _, err := st.Exec("DELETE FROM permissions WHERE permission_key = ?", rbac.PermStockRecord); err != nil {
		t.Fatal(err)
	}
	rerun("without the permission")

	// Seeded by this release before migrating: the permission exists, and
	// admin holds it already.
	if err := st.SeedDefaultPermissionsAndRoles(); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpdateRole(writers.ID, RoleInput{Name: "Writers", Permissions: []string{rbac.PermAssetsRead, rbac.PermAssetsWrite}}); err != nil {
		t.Fatal(err)
	}
	rerun("with the permission seeded")
}
