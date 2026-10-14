package store

import (
	"errors"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/auth"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/fields"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

func TestListRolesAndSetUserRoles(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	admin, err := st.CreateAdminUser("admin@hackspace.org", "Pass1234!")
	if err != nil {
		t.Fatal(err)
	}
	roles, err := st.ListRoles()
	if err != nil {
		t.Fatal(err)
	}
	var adminRoleID string
	for _, r := range roles {
		if r.Name == "admin" {
			adminRoleID = r.ID
			if len(r.Permissions) != len(rbac.AllPermissions()) {
				t.Errorf("admin role lists %v", r.Permissions)
			}
		}
	}
	if adminRoleID == "" {
		t.Fatalf("no admin role in %+v", roles)
	}

	user, err := st.CreateUser("user@hackspace.org", "Pass1234!", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetUserRoles(user.ID, []string{adminRoleID, adminRoleID}); err != nil {
		t.Fatalf("granting admin: %v", err)
	}
	ids, err := st.UserRoleIDs()
	if err != nil || len(ids[user.ID]) != 1 || ids[user.ID][0] != adminRoleID {
		t.Fatalf("UserRoleIDs = %v, %v", ids, err)
	}

	if err := st.SetUserRoles(user.ID, []string{"no-such-role"}); !errors.Is(err, ErrRoleNotFound) {
		t.Errorf("unknown role: got %v", err)
	}
	if ids, _ := st.UserRoleIDs(); len(ids[user.ID]) != 1 {
		t.Errorf("a refused change altered the roles: %v", ids[user.ID])
	}

	// Two admins: one may drop the role; then the last one may not.
	if err := st.SetUserRoles(admin.ID, []string{}); err != nil {
		t.Fatalf("dropping admin while another remains: %v", err)
	}
	if err := st.SetUserRoles(user.ID, nil); !errors.Is(err, rbac.ErrAntiLockoutTriggered) {
		t.Errorf("removing the last admin's roles: got %v", err)
	}
	if err := st.SetUserRoles("missing", []string{adminRoleID}); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("missing user: got %v", err)
	}
}

func TestChangePassword(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	u, err := st.CreateUser("pw@hackspace.org", "OldPass123!", true)
	if err != nil {
		t.Fatal(err)
	}
	keep, _ := st.CreateSession(u.ID, time.Hour)
	other, _ := st.CreateSession(u.ID, time.Hour)

	if err := st.ChangePassword(u.ID, "wrong-password", "NewPass123!", keep.ID); !errors.Is(err, ErrWrongPassword) {
		t.Errorf("wrong current password: got %v", err)
	}
	if err := st.ChangePassword(u.ID, "OldPass123!", "short", keep.ID); !errors.Is(err, auth.ErrPasswordPolicy) {
		t.Errorf("too short: got %v", err)
	}
	if _, _, err := st.GetSession(other.ID); err != nil {
		t.Fatalf("a refused change ended sessions: %v", err)
	}

	if err := st.ChangePassword(u.ID, "OldPass123!", "NewPass123!", keep.ID); err != nil {
		t.Fatal(err)
	}
	reloaded, _ := st.GetUserByID(u.ID)
	if !auth.CheckPassword("NewPass123!", reloaded.PasswordHash) || auth.CheckPassword("OldPass123!", reloaded.PasswordHash) {
		t.Error("password not changed")
	}
	if _, _, err := st.GetSession(keep.ID); err != nil {
		t.Errorf("the current session ended: %v", err)
	}
	if _, _, err := st.GetSession(other.ID); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("another session survived the change: %v", err)
	}
}

func TestEffectiveFieldsForAsset(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	serial, err := st.CreateField("serial", "Serial", fields.TypeText, nil)
	if err != nil {
		t.Fatal(err)
	}
	voltage, err := st.CreateField("voltage", "Voltage", fields.TypeText, nil)
	if err != nil {
		t.Fatal(err)
	}
	tools, _ := st.CreateCategory("Tools", nil, "#112233", false, false)
	power, _ := st.CreateCategory("Power tools", &tools.ID, "#445566", false, false)
	bench, _ := st.CreateCategory("Bench", nil, "#778899", false, false)
	if err := st.AssignCategoryField(tools.ID, serial.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := st.AssignCategoryField(power.ID, voltage.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := st.AssignCategoryField(bench.ID, serial.ID, true); err != nil {
		t.Fatal(err)
	}

	got, err := st.EffectiveFieldsForAsset([]string{power.ID, bench.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Field.Key != "serial" || !got[0].IsRequired || got[1].Field.Key != "voltage" {
		t.Errorf("effective fields = %+v", got)
	}

	if none, err := st.EffectiveFieldsForAsset(nil); err != nil || none == nil {
		t.Errorf("no categories: %v, %v", none, err)
	}
	if _, err := st.EffectiveFieldsForAsset([]string{"missing"}); !errors.Is(err, ErrCategoryNotFound) {
		t.Errorf("unknown category: got %v", err)
	}
}
