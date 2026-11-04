package api

import (
	"encoding/base32"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/auth"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/idp"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// woodshopper is someone holding a role that requires two-factor sign-in.
func woodshopper(t *testing.T, st *store.Store, email string) (*models.User, *models.RoleWithPermissions) {
	t.Helper()
	on := true
	role, err := st.CreateRole(store.RoleInput{Name: "Woodshop " + email, Permissions: []string{rbac.PermAssetsRead}, RequiresTwoFactor: &on})
	if err != nil {
		t.Fatal(err)
	}
	u, err := st.CreateUser(email, "WoodSecret123!", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetUserRoleAssignments(u.ID, []models.RoleAssignment{{RoleID: role.ID, ScopeIDs: []string{}}}); err != nil {
		t.Fatal(err)
	}
	return u, role
}

func TestARoleRequiringTwoFactorHoldsASessionToSettingOneUp(t *testing.T) {
	srv, _, _ := factorServer(t)
	ada, _ := woodshopper(t, srv.store, "ada@test.org")

	rec := signIn(t, srv, "192.0.2.10:1", "", ada.Email, "WoodSecret123!")
	cookie := cookieNamed(rec, SessionCookieName)
	if rec.Code != http.StatusOK || cookie == nil || !strings.Contains(rec.Body.String(), `"factor_setup_required":true`) {
		t.Fatalf("signing in: %d %s", rec.Code, rec.Body)
	}
	// Nothing but setting one up, who it is, and signing out.
	if rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/assets", ""); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), ErrFactorSetupRequired) {
		t.Errorf("the assets: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/auth/theme", `{"theme":"dark"}`); rec.Code != http.StatusForbidden {
		t.Errorf("the theme: %d", rec.Code)
	}
	if rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/auth/me", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"factor_setup_required":true`) {
		t.Errorf("who it is: %d %s", rec.Code, rec.Body)
	}

	// Setting one up, through the held session, frees it.
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/auth/me/factors/totp", "")
	var started struct{ Secret string }
	_ = json.NewDecoder(rec.Body).Decode(&started)
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ReplaceAll(started.Secret, " ", ""))
	if rec.Code != http.StatusOK || err != nil {
		t.Fatalf("starting an app: %d %v", rec.Code, err)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/auth/me/factors/totp/confirm", `{"code":"`+auth.TOTPCode(secret, auth.TOTPStep(time.Now()))+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("confirming it: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/assets", ""); rec.Code != http.StatusOK {
		t.Errorf("the assets after: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/auth/me", ""); !strings.Contains(rec.Body.String(), `"factor_setup_required":false`) {
		t.Errorf("who it is after: %s", rec.Body)
	}
}

func TestNobodyIsHeldToSettingUpAFactorWhereNoneCanBe(t *testing.T) {
	cfg := config.DefaultConfig()
	testStorage(t, cfg)
	srv := NewServer(cfg, setupTestStore(t))
	ada, _ := woodshopper(t, srv.store, "ada@test.org")
	// Without [auth] secret_key no factor can be set up, so the role's
	// requirement holds nobody back.
	if srv.sealer.HasKey() {
		t.Fatal("the server has a secret key")
	}
	rec := signIn(t, srv, "192.0.2.10:1", "", ada.Email, "WoodSecret123!")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"factor_setup_required":false`) {
		t.Fatalf("signing in: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, cookieNamed(rec, SessionCookieName), http.MethodGet, "/api/v1/assets", ""); rec.Code != http.StatusOK {
		t.Errorf("the assets: %d", rec.Code)
	}
}

func TestTheAdminRoleRequiresTwoFactorUntilTurnedOffWhichIsRecorded(t *testing.T) {
	srv, admin, _ := factorServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	adminRole, _ := srv.store.GetRoleByName(store.AdminRoleName)
	if r, _ := srv.store.GetRole(adminRole.ID); !r.RequiresTwoFactor {
		t.Fatal("the admin role does not require two-factor sign-in")
	}
	// Its other settings still cannot change.
	if rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/roles/"+adminRole.ID, `{"name":"admin","requires_two_factor":false}`); rec.Code != http.StatusConflict {
		t.Errorf("changing it as a whole: %d", rec.Code)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/roles/"+adminRole.ID+"/two-factor", `{}`); rec.Code != http.StatusBadRequest {
		t.Errorf("no answer: %d", rec.Code)
	}
	rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/roles/"+adminRole.ID+"/two-factor", `{"required":false}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"requires_two_factor":false`) {
		t.Fatalf("turning it off: %d %s", rec.Code, rec.Body)
	}
	events, err := srv.store.ListEvents(1)
	if err != nil || len(events) != 1 || events[0].Kind != models.EventRoleTwoFactorOff || events[0].Payload.Note != "admin" ||
		events[0].Payload.Actor == nil || events[0].Payload.Actor.ID != admin.ID {
		t.Errorf("in history: %+v, %v", events, err)
	}
	// Turning it on again is not recorded.
	doJSON(t, srv, cookie, http.MethodPut, "/api/v1/roles/"+adminRole.ID+"/two-factor", `{"required":true}`)
	if again, _ := srv.store.ListEvents(5); len(again) != 1 {
		t.Errorf("events after turning it on: %+v", again)
	}

	// Another role sets it as a whole, and keeps it when it is left out.
	rec = doJSON(t, srv, cookie, http.MethodPost, "/api/v1/roles", `{"name":"Lathe","permissions":["assets.read"],"requires_two_factor":true}`)
	var made struct{ Role models.RoleWithPermissions }
	_ = json.NewDecoder(rec.Body).Decode(&made)
	if rec.Code != http.StatusCreated || !made.Role.RequiresTwoFactor {
		t.Fatalf("making one: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/roles/"+made.Role.ID, `{"name":"Lathe","permissions":["assets.read"]}`); !strings.Contains(rec.Body.String(), `"requires_two_factor":true`) {
		t.Errorf("left out: %s", rec.Body)
	}
}

func TestWhatFollowsAProvidersSignIn(t *testing.T) {
	p := func(fallback, always bool) *store.Provider {
		return &store.Provider{MFAAMR: store.DefaultMFAAMR, MFAACR: []string{"urn:gold"}, MFAFallback: fallback, MFAAlwaysLocal: always}
	}
	mfa, pwd, gold := &idp.Claims{AMR: []string{"pwd", "mfa"}}, &idp.Claims{AMR: []string{"pwd"}}, &idp.Claims{AMR: []string{"pwd"}, ACR: "urn:gold"}
	for _, c := range []struct {
		what                string
		p                   *store.Provider
		c                   *idp.Claims
		required, hasFactor bool
		want                int
	}{
		{"its amr says mfa", p(false, false), mfa, true, false, stepNone},
		{"its acr is listed", p(false, false), gold, true, false, stepNone},
		{"it does not say, required", p(false, false), pwd, true, true, stepRefuse},
		{"it does not say, falling back", p(true, false), pwd, true, true, stepOwn},
		{"it does not say, falling back, none set up", p(true, false), pwd, true, false, stepSetUp},
		{"not required, with one of their own", p(false, false), pwd, false, true, stepOwn},
		{"not required, none", p(false, false), pwd, false, false, stepNone},
		{"always ask, though it says mfa", p(false, true), mfa, false, true, stepOwn},
		{"always ask, required, none set up", p(false, true), mfa, true, false, stepSetUp},
		{"always ask, none needed or set up", p(false, true), mfa, false, false, stepNone},
	} {
		if got := afterProvider(c.p, c.c, c.required, c.hasFactor); got != c.want {
			t.Errorf("%s: %d, want %d", c.what, got, c.want)
		}
	}
}

func TestProviderSignInsAndTwoFactorByRole(t *testing.T) {
	srv, ts, admin := providerServer(t)
	ada, _ := woodshopper(t, srv.store, "ada@test.org")
	settings := map[string]string{"subject": "ada-at-idp", "amr": "pwd,mfa"}
	req := testProviderRequest(settings)
	p := saveProvider(t, srv, admin, "", req)
	if _, err := srv.store.LinkIdentity(ada.ID, p.ID, "ada-at-idp", ""); err != nil {
		t.Fatal(err)
	}

	// The provider says it used a second factor: signed in.
	b := browser(t)
	if end := signInThrough(t, ts, b, "hackspace", "/assets"); end.Path != "/assets" || me(t, ts, b) != ada.Email {
		t.Errorf("with mfa: ended at %s", end)
	}

	// It does not: refused, saying why.
	settings["amr"] = "pwd"
	saveProvider(t, srv, admin, p.ID, req)
	b = browser(t)
	if end := signInThrough(t, ts, b, "hackspace", "/assets"); end.Path != "/sign-in" || !strings.Contains(end.Query().Get("error"), "did not say you used a second factor") || me(t, ts, b) != "" {
		t.Errorf("without: ended at %s", end)
	}

	// Falling back to BAM!'s own, which they have not set up: signed in,
	// held to setting one up.
	req.MFAFallback = true
	saveProvider(t, srv, admin, p.ID, req)
	b = browser(t)
	signInThrough(t, ts, b, "hackspace", "/")
	resp, err := b.Get(ts.URL + "/api/v1/assets")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden || me(t, ts, b) != ada.Email {
		t.Errorf("falling back: the assets are %d", resp.StatusCode)
	}
}
