package store

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

func TestRolesRequiringTwoFactor(t *testing.T) {
	st := setupTestStore(t)
	admin, err := st.CreateAdminUser("admin@test.org", "AdminSecret123!")
	if err != nil {
		t.Fatal(err)
	}
	if required, err := st.RequiresTwoFactor(admin.ID); err != nil || !required {
		t.Errorf("the admin role does not require it: %v, %v", required, err)
	}
	ada, _ := st.CreateUser("ada@test.org", "AdaSecret123!", true)
	if required, _ := st.RequiresTwoFactor(ada.ID); required {
		t.Error("someone with no role is required to")
	}
	public, _ := st.GetRoleByName(rbac.PublicRole)
	if _, err := st.SetRoleTwoFactor(public.ID, true); !errors.Is(err, ErrPublicRole) {
		t.Errorf("the public role: %v", err)
	}
	on := true
	if _, err := st.UpdateRole(public.ID, RoleInput{Name: rbac.PublicRole, RequiresTwoFactor: &on}); !errors.Is(err, ErrPublicRole) {
		t.Errorf("the public role, changed whole: %v", err)
	}
	if _, err := st.SetRoleTwoFactor("missing", true); !errors.Is(err, ErrRoleNotFound) {
		t.Errorf("a role that is not there: %v", err)
	}
}

func TestASessionIsHeldToSettingUpAFactorUntilThereIsOne(t *testing.T) {
	st := setupTestStore(t)
	admin, _ := st.CreateAdminUser("admin@test.org", "AdminSecret123!")

	// Where none can be set up, nobody is held.
	if sess, err := st.StartSession(admin.ID, time.Hour, false); err != nil || sess.FactorSetup {
		t.Errorf("none can be set up: %+v, %v", sess, err)
	}
	sess, err := st.StartSession(admin.ID, time.Hour, true)
	if err != nil || !sess.FactorSetup {
		t.Fatalf("starting: %+v, %v", sess, err)
	}
	if got, _, err := st.GetSession(sess.ID); err != nil || !got.FactorSetup {
		t.Fatalf("read back: %+v, %v", got, err)
	}
	f, _ := st.StartTOTP(admin.ID, func(string) (string, error) { return "v1:sealed", nil })
	if _, err := st.ConfirmTOTP(admin.ID, f.ID, 10); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := st.GetSession(sess.ID); got.FactorSetup {
		t.Error("still held after setting one up")
	}
	if again, _ := st.StartSession(admin.ID, time.Hour, true); again.FactorSetup {
		t.Error("held with a factor")
	}

	// The last factor stays while a role requires one.
	if err := st.DeleteFactor(admin.ID, f.ID); !errors.Is(err, ErrFactorRequired) {
		t.Errorf("the last one: %v", err)
	}
	adminRole, _ := st.GetRoleByName(AdminRoleName)
	if _, err := st.SetRoleTwoFactor(adminRole.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteFactor(admin.ID, f.ID); err != nil {
		t.Errorf("once not required: %v", err)
	}
	events, _ := st.ListEvents(5)
	if len(events) != 1 || events[0].Kind != models.EventRoleTwoFactorOff || events[0].Payload.Note != AdminRoleName {
		t.Errorf("history: %+v", events)
	}
}

func TestProvidersTwoFactorSettings(t *testing.T) {
	st := setupTestStore(t)
	in := ProviderInput{Key: "hackspace", Name: "Hackspace", Protocol: "test"}
	p, err := st.CreateProvider(in)
	if err != nil {
		t.Fatal(err)
	}
	// Left out, the default amr values count; no acr does.
	if !reflect.DeepEqual(p.MFAAMR, DefaultMFAAMR) || len(p.MFAACR) != 0 || p.MFAFallback || p.MFAAlwaysLocal {
		t.Errorf("defaults: %+v", p)
	}
	in.MFAAMR, in.MFAACR, in.MFAFallback, in.MFAAlwaysLocal = []string{" hwk ", ""}, []string{"urn:gold"}, true, true
	if p, err = st.UpdateProvider(p.ID, in); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.MFAAMR, []string{"hwk"}) || !reflect.DeepEqual(p.MFAACR, []string{"urn:gold"}) || !p.MFAFallback || !p.MFAAlwaysLocal {
		t.Errorf("saved: %+v", p)
	}
	// None at all is kept as none.
	in.MFAAMR = []string{}
	if p, _ = st.UpdateProvider(p.ID, in); len(p.MFAAMR) != 0 {
		t.Errorf("none: %+v", p.MFAAMR)
	}
}
