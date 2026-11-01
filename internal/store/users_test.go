package store

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

func TestUserCreationAndPermissionsUnion(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	// 1. Create a normal user
	user, err := st.CreateUser("dev@hackspace.org", "UserPassword123!", true)
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	// Normal user has 0 roles, so 0 permissions
	perms, err := st.GetUserPermissions(user.ID)
	if err != nil {
		t.Fatalf("GetUserPermissions failed: %v", err)
	}
	if len(perms) != 0 {
		t.Errorf("expected 0 permissions for unassigned user, got %d", len(perms))
	}

	// 2. Assign admin role
	adminRole, err := st.GetRoleByName("admin")
	if err != nil {
		t.Fatalf("GetRoleByName(admin) failed: %v", err)
	}

	if err := st.AssignUserRole(user.ID, adminRole.ID); err != nil {
		t.Fatalf("AssignUserRole failed: %v", err)
	}

	// User now inherits full admin permissions union
	permsAfter, err := st.GetUserPermissions(user.ID)
	if err != nil {
		t.Fatalf("GetUserPermissions after role assign failed: %v", err)
	}
	if !permsAfter[rbac.PermUsersManage] {
		t.Errorf("expected user to possess %s", rbac.PermUsersManage)
	}
	if !permsAfter[rbac.PermAssetsRead] {
		t.Errorf("expected user to possess %s", rbac.PermAssetsRead)
	}
}

func TestStoreAntiLockoutEnforcement(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	// 1. Create the primary admin
	admin1, err := st.CreateAdminUser("admin1@hackspace.org", "Pass1234!")
	if err != nil {
		t.Fatalf("CreateAdminUser 1 failed: %v", err)
	}

	adminRole, err := st.GetRoleByName("admin")
	if err != nil {
		t.Fatalf("GetRoleByName(admin): %v", err)
	}

	// Verify exactly 1 active admin
	count, err := st.CountActiveAdmins()
	if err != nil || count != 1 {
		t.Fatalf("expected 1 active admin, got count=%d, err=%v", count, err)
	}

	// 2. Test Anti-Lockout on Status Deactivation: must fail
	err = st.UpdateUserStatus(admin1.ID, false)
	if err == nil {
		t.Fatal("expected deactivating sole admin to fail with anti-lockout error")
	}
	if !errors.Is(err, rbac.ErrAntiLockoutTriggered) {
		t.Errorf("expected ErrAntiLockoutTriggered, got %v", err)
	}

	// 3. Test Anti-Lockout on User Deletion: must fail
	err = st.DeleteUser(admin1.ID)
	if err == nil {
		t.Fatal("expected deleting sole admin to fail with anti-lockout error")
	}
	if !errors.Is(err, rbac.ErrAntiLockoutTriggered) {
		t.Errorf("expected ErrAntiLockoutTriggered, got %v", err)
	}

	// 4. Test Anti-Lockout on Admin Role Removal: must fail
	err = st.RemoveUserRole(admin1.ID, adminRole.ID)
	if err == nil {
		t.Fatal("expected revoking admin role from sole admin to fail with anti-lockout error")
	}
	if !errors.Is(err, rbac.ErrAntiLockoutTriggered) {
		t.Errorf("expected ErrAntiLockoutTriggered, got %v", err)
	}

	// 5. Create a second admin
	admin2, err := st.CreateAdminUser("admin2@hackspace.org", "Pass5678!")
	if err != nil {
		t.Fatalf("CreateAdminUser 2 failed: %v", err)
	}

	count2, err := st.CountActiveAdmins()
	if err != nil || count2 != 2 {
		t.Fatalf("expected 2 active admins, got count=%d, err=%v", count2, err)
	}

	// 6. Deactivating admin1 now succeeds because admin2 remains active
	if err := st.UpdateUserStatus(admin1.ID, false); err != nil {
		t.Fatalf("expected deactivating admin1 to succeed with 2 active admins, got %v", err)
	}

	// Verify active admins dropped back to 1
	countAfter, err := st.CountActiveAdmins()
	if err != nil || countAfter != 1 {
		t.Fatalf("expected 1 active admin remaining, got count=%d", countAfter)
	}

	// 7. Now attempting to deactivate or delete admin2 must be blocked!
	err = st.UpdateUserStatus(admin2.ID, false)
	if !errors.Is(err, rbac.ErrAntiLockoutTriggered) {
		t.Errorf("expected ErrAntiLockoutTriggered on admin2, got %v", err)
	}

	err = st.DeleteUser(admin2.ID)
	if !errors.Is(err, rbac.ErrAntiLockoutTriggered) {
		t.Errorf("expected ErrAntiLockoutTriggered on deleting admin2, got %v", err)
	}
}

func TestUserNames(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	u, err := st.CreateNamedUser("ada@hackspace.org", "  Ada Lovelace ", "a long enough password", true)
	if err != nil || u.Name != "Ada Lovelace" {
		t.Fatalf("created %+v, %v", u, err)
	}
	if _, err := st.CreateNamedUser("long@hackspace.org", strings.Repeat("x", 256), "a long enough password", true); !errors.Is(err, ErrInvalidUser) {
		t.Errorf("an overlong name: %v", err)
	}
	if got, _ := st.GetUserByEmail("ADA@hackspace.org"); got == nil || got.Name != "Ada Lovelace" {
		t.Errorf("read back %+v", got)
	}

	// History keeps the name as it read when the change was made.
	id := asset(t, st.As(u.ID), "Drill")
	renamed, err := st.SetUserName(u.ID, "Ada King")
	if err != nil || renamed.Name != "Ada King" {
		t.Fatalf("renamed %+v, %v", renamed, err)
	}
	archive(t, st.As(u.ID), id)
	list := history(t, st, id)
	if a := list[0].Payload.Actor; a == nil || a.Name != "Ada King" {
		t.Errorf("the archiving's actor: %+v", a)
	}
	if a := list[1].Payload.Actor; a == nil || a.Name != "Ada Lovelace" || a.Email != "ada@hackspace.org" {
		t.Errorf("the creation's actor: %+v", a)
	}
	if list, _ := st.ListUsers(); len(list) != 1 || list[0].Name != "Ada King" {
		t.Errorf("listed %+v", list)
	}

	// Clearing it shows the email again; archived and missing users are not renamed.
	if cleared, err := st.SetUserName(u.ID, ""); err != nil || cleared.Name != "" {
		t.Errorf("cleared %+v, %v", cleared, err)
	}
	if _, err := st.SetUserName("no-such-user", "Nobody"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("a missing user: %v", err)
	}
	other, err := st.CreateUser("grace@hackspace.org", "a long enough password", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteUser(other.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetUserName(other.ID, "Grace"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("an archived user: %v", err)
	}
}

func TestUserThemes(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	u, err := st.CreateNamedUser("ada@hackspace.org", "Ada", "a long enough password", true)
	if err != nil {
		t.Fatal(err)
	}
	// Until they choose, the interface follows their system.
	if u.Theme != ThemeSystem {
		t.Errorf("created with %q", u.Theme)
	}
	if got, _ := st.GetUserByID(u.ID); got.Theme != ThemeSystem {
		t.Errorf("read back with %q", got.Theme)
	}

	sess, err := st.CreateSession(u.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	for _, theme := range []string{"dark", "light", "light", ThemeSystem, ThemeSystem, "dark"} {
		got, err := st.SetUserTheme(u.ID, theme)
		if err != nil || got.Theme != theme {
			t.Fatalf("set %s: %+v, %v", theme, got, err)
		}
		// Every read of the user has it: by ID, by email, listed and signed in.
		byEmail, _ := st.GetUserByEmail("ada@hackspace.org")
		list, _ := st.ListUsers()
		_, signedIn, _ := st.GetSession(sess.ID)
		if byEmail.Theme != theme || len(list) != 1 || list[0].Theme != theme || signedIn == nil || signedIn.Theme != theme {
			t.Errorf("%s read back as %q, %+v, %+v", theme, byEmail.Theme, list, signedIn)
		}
	}
	// The system's is kept as no choice at all.
	if _, err := st.SetUserTheme(u.ID, ThemeSystem); err != nil {
		t.Fatal(err)
	}
	var stored sql.NullString
	if err := st.QueryRow("SELECT theme FROM users WHERE id = ?", u.ID).Scan(&stored); err != nil || stored.Valid {
		t.Errorf("system stored as %+v, %v", stored, err)
	}

	// It is a preference, not a change to the account.
	before, _ := st.GetUserByID(u.ID)
	after, err := st.SetUserTheme(u.ID, "dark")
	if err != nil || !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Errorf("updated_at went from %v to %v, %v", before.UpdatedAt, after.UpdatedAt, err)
	}

	for _, bad := range []string{"", "Dark", "sepia", " light", "auto"} {
		if _, err := st.SetUserTheme(u.ID, bad); !errors.Is(err, ErrInvalidUser) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if got, _ := st.GetUserByID(u.ID); got.Theme != "dark" {
		t.Errorf("a bad theme changed it to %q", got.Theme)
	}
	if _, err := st.SetUserTheme("no-such-user", "dark"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("a missing user: %v", err)
	}
	if err := st.DeleteUser(u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetUserTheme(u.ID, "light"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("an archived user: %v", err)
	}
	if got, _ := st.GetUserByID(u.ID); got.Theme != "dark" {
		t.Errorf("an archived user's theme became %q", got.Theme)
	}
}
