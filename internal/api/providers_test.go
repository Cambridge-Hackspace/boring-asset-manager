package api

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/auth"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// providerServer is a server that offers the test protocol, behind a real
// listener so that a browser's redirects and cookies can be followed, and
// its admin.
func providerServer(t *testing.T) (*Server, *httptest.Server, *models.User) {
	t.Helper()
	cfg := config.DefaultConfig()
	testStorage(t, cfg)
	cfg.Auth.SecretKey = testSecretKey
	cfg.Auth.TestProtocol = true
	st := setupTestStore(t)
	admin, err := st.CreateAdminUser("admin@test.org", "AdminSecret123!")
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(cfg, st)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	srv.cfg.Server.BaseURL = ts.URL
	return srv, ts, admin
}

// browser is a client that keeps cookies and follows redirects, as a
// browser does.
func browser(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Jar: jar}
}

// signInThrough starts a sign-in through a provider and follows it back,
// answering where it ended: the page's path and query.
func signInThrough(t *testing.T, ts *httptest.Server, b *http.Client, key, next string) *url.URL {
	t.Helper()
	resp, err := b.Get(ts.URL + "/api/v1/auth/providers/" + key + "/start?next=" + url.QueryEscape(next))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.Request.URL
}

// me is who a browser is signed in as, or "" for no one.
func me(t *testing.T, ts *httptest.Server, b *http.Client) string {
	t.Helper()
	resp, err := b.Get(ts.URL + "/api/v1/auth/me")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		User *models.User `json:"user"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body.User == nil {
		return ""
	}
	return body.User.Email
}

// saveProvider saves a provider through the API as an admin, answering it.
func saveProvider(t *testing.T, srv *Server, admin *models.User, id string, req ProviderRequest) providerView {
	t.Helper()
	body, _ := json.Marshal(req)
	method, path, want := http.MethodPost, "/api/v1/auth/providers", http.StatusCreated
	if id != "" {
		method, path, want = http.MethodPut, path+"/"+id, http.StatusOK
	}
	rec := doJSON(t, srv, sessionCookie(t, srv, admin.ID), method, path, string(body))
	if rec.Code != want {
		t.Fatalf("saving the provider: %d %s", rec.Code, rec.Body)
	}
	var resp struct {
		Provider providerView `json:"provider"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	return resp.Provider
}

func testProviderRequest(settings map[string]string) ProviderRequest {
	return ProviderRequest{Key: "hackspace", Name: "Hackspace", Protocol: "test", Enabled: true, Settings: settings}
}

func TestAccountsAreMadeOnFirstSignInWithRolesFromClaims(t *testing.T) {
	srv, ts, admin := providerServer(t)
	members, _ := srv.store.CreateRole(store.RoleInput{Name: "Members", Permissions: []string{rbac.PermAssetsRead}})
	writers, _ := srv.store.CreateRole(store.RoleInput{Name: "Writers", Permissions: []string{rbac.PermAssetsWrite}})
	wood, _ := srv.store.CreateScope("woodshop")
	settings := map[string]string{"subject": "ada-1", "email": "ada@hackspace.org", "email_verified": "yes", "name": "Ada", "groups": "wood, members"}
	req := testProviderRequest(settings)
	req.Provision, req.DefaultRoleIDs, req.AllowedDomains = true, []string{members.ID}, []string{"hackspace.org"}
	req.Rules = []store.ProviderRule{{Claim: "groups", Value: "WOOD", RoleID: writers.ID, ScopeIDs: []string{wood.ID}}}
	p := saveProvider(t, srv, admin, "", req)

	b := browser(t)
	if end := signInThrough(t, ts, b, "hackspace", "/assets"); end.Path != "/assets" {
		t.Fatalf("ended at %s", end)
	}
	if got := me(t, ts, b); got != "ada@hackspace.org" {
		t.Fatalf("signed in as %q", got)
	}
	ada, err := srv.store.GetUserByEmail("ada@hackspace.org")
	if err != nil || ada.Name != "Ada" || store.HasPassword(ada) {
		t.Fatalf("made: %+v, %v", ada, err)
	}
	all, _ := srv.store.UserRoleAssignments()
	roles := map[string][]string{}
	for _, a := range all[ada.ID] {
		roles[a.RoleID] = a.ScopeIDs
	}
	if len(roles) != 2 || len(roles[members.ID]) != 0 || len(roles[writers.ID]) != 1 || roles[writers.ID][0] != wood.ID {
		t.Errorf("roles: %v", roles)
	}
	if grants, _ := srv.store.RoleGrants(ada.ID); grants[writers.ID] != p.ID || grants[members.ID] != "" {
		t.Errorf("who gave them: %v", grants)
	}

	// Out of the group, the next sign-in takes the rule's role away, and
	// leaves the default one, given by hand at first.
	settings["groups"] = "members"
	saveProvider(t, srv, admin, p.ID, req)
	b = browser(t)
	signInThrough(t, ts, b, "hackspace", "/")
	all, _ = srv.store.UserRoleAssignments()
	if len(all[ada.ID]) != 1 || all[ada.ID][0].RoleID != members.ID {
		t.Errorf("roles after leaving the group: %+v", all[ada.ID])
	}
	// The same subject is the same account, whatever the email says now.
	settings["email"] = "ada.lovelace@hackspace.org"
	saveProvider(t, srv, admin, p.ID, req)
	b = browser(t)
	signInThrough(t, ts, b, "hackspace", "/")
	if got := me(t, ts, b); got != "ada@hackspace.org" {
		t.Errorf("after a changed email: %q", got)
	}
}

func TestAccountsAreMadeOnlyWhereTheProviderAllows(t *testing.T) {
	srv, ts, admin := providerServer(t)
	settings := map[string]string{"subject": "s-1", "email": "eve@elsewhere.org", "email_verified": "yes", "groups": "visitors"}
	req := testProviderRequest(settings)
	p := saveProvider(t, srv, admin, "", req)

	cases := []struct {
		what   string
		change func()
		want   string
	}{
		{"off", func() {}, "No account here is linked to your Hackspace sign-in."},
		{"another domain", func() { req.Provision, req.AllowedDomains = true, []string{"hackspace.org"} }, "only for emails at hackspace.org"},
		{"another group", func() { req.AllowedDomains, req.AllowedGroups = nil, []string{"members"} }, "only for members of members"},
		{"an email not vouched for", func() { req.AllowedGroups, settings["email_verified"] = nil, "no" }, "did not vouch for your email"},
	}
	for _, c := range cases {
		c.change()
		saveProvider(t, srv, admin, p.ID, req)
		b := browser(t)
		end := signInThrough(t, ts, b, "hackspace", "/assets")
		if end.Path != "/sign-in" || !strings.Contains(end.Query().Get("error"), c.want) || end.Query().Get("next") != "/assets" {
			t.Errorf("%s: ended at %s", c.what, end)
		}
		if got := me(t, ts, b); got != "" {
			t.Errorf("%s: signed in as %q", c.what, got)
		}
	}
	if _, err := srv.store.GetUserByEmail("eve@elsewhere.org"); err == nil {
		t.Error("an account was made")
	}
}

func TestLinkingByEmailNeedsItOnAndVerified(t *testing.T) {
	srv, ts, admin := providerServer(t)
	bob, _ := srv.store.CreateUser("bob@hackspace.org", "BobSecret123!", true)
	settings := map[string]string{"subject": "bob-at-idp", "email": "Bob@Hackspace.org", "email_verified": "no"}
	req := testProviderRequest(settings)
	req.Provision = true
	p := saveProvider(t, srv, admin, "", req)

	for what, change := range map[string]func(){
		"linking off":       func() {},
		"an unverified one": func() { req.LinkByEmail = true },
	} {
		change()
		saveProvider(t, srv, admin, p.ID, req)
		b := browser(t)
		// Told just what someone with no account is, so as not to say who has one.
		if end := signInThrough(t, ts, b, "hackspace", "/"); end.Query().Get("error") != noAccount(&store.Provider{Name: "Hackspace"}) {
			t.Errorf("%s: ended at %s", what, end)
		}
	}
	// Verified, by the provider or the admin's word, it links.
	req.TrustEmail = true
	saveProvider(t, srv, admin, p.ID, req)
	b := browser(t)
	signInThrough(t, ts, b, "hackspace", "/")
	if got := me(t, ts, b); got != "bob@hackspace.org" {
		t.Fatalf("linked: signed in as %q", got)
	}
	if ids, _ := srv.store.UserIdentities(bob.ID); len(ids) != 1 || ids[0].Subject != "bob-at-idp" {
		t.Errorf("identities: %+v", ids)
	}
	// Linked, a changed email never moves the identity to its owner.
	srv.store.CreateUser("carol@hackspace.org", "CarolSecret123!", true)
	settings["email"] = "carol@hackspace.org"
	saveProvider(t, srv, admin, p.ID, req)
	b = browser(t)
	signInThrough(t, ts, b, "hackspace", "/")
	if got := me(t, ts, b); got != "bob@hackspace.org" {
		t.Errorf("after the email changed: %q", got)
	}
}

func TestAProviderSignInStillAsksForASecondFactor(t *testing.T) {
	srv, ts, admin := providerServer(t)
	secret := setUpApp(t, srv, admin.ID)
	saveProvider(t, srv, admin, "", testProviderRequest(map[string]string{"subject": "admin-at-idp"}))
	if _, err := srv.store.LinkIdentity(admin.ID, mustProvider(t, srv, "hackspace").ID, "admin-at-idp", ""); err != nil {
		t.Fatal(err)
	}
	b := browser(t)
	end := signInThrough(t, ts, b, "hackspace", "/assets")
	if end.Path != "/sign-in" || end.Query().Get("factor") != "required" || end.Query().Get("next") != "/assets" {
		t.Fatalf("ended at %s", end)
	}
	if got := me(t, ts, b); got != "" {
		t.Fatalf("signed in before the code: %q", got)
	}
	code := auth.TOTPCode(secret, auth.TOTPStep(time.Now())+1)
	resp, err := b.Post(ts.URL+"/api/v1/auth/login/factor", "application/json", strings.NewReader(`{"code":"`+code+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || me(t, ts, b) != admin.Email {
		t.Errorf("after the code: %d", resp.StatusCode)
	}
}

func mustProvider(t *testing.T, srv *Server, key string) *store.Provider {
	t.Helper()
	p, err := srv.store.GetProviderByKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAProviderAnswerMustBeForThisBrowsersSignIn(t *testing.T) {
	srv, ts, admin := providerServer(t)
	saveProvider(t, srv, admin, "", testProviderRequest(map[string]string{"subject": "admin-at-idp"}))
	srv.store.LinkIdentity(admin.ID, mustProvider(t, srv, "hackspace").ID, "admin-at-idp", "")

	// No sign-in started here.
	b := browser(t)
	resp, err := b.Get(ts.URL + "/api/v1/auth/providers/hackspace/callback?state=forged")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !strings.Contains(resp.Request.URL.Query().Get("error"), "expired") || me(t, ts, b) != "" {
		t.Errorf("no sign-in: %s", resp.Request.URL)
	}
	// Started here, but answered with another state.
	noFollow := browser(t)
	noFollow.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err = noFollow.Get(ts.URL + "/api/v1/auth/providers/hackspace/start")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	resp, err = noFollow.Get(ts.URL + "/api/v1/auth/providers/hackspace/callback?state=forged")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "could+not+be+checked") || me(t, ts, noFollow) != "" {
		t.Errorf("another state: %s", loc)
	}
	// A provider turned off has no button, and cannot be used.
	req := testProviderRequest(map[string]string{"subject": "admin-at-idp"})
	req.Enabled = false
	saveProvider(t, srv, admin, mustProvider(t, srv, "hackspace").ID, req)
	if end := signInThrough(t, ts, browser(t), "hackspace", "/"); !strings.Contains(end.Query().Get("error"), "not available") {
		t.Errorf("turned off: %s", end)
	}
	rec := doJSON(t, srv, nil, http.MethodGet, "/api/v1/auth/providers/public", "")
	if strings.Contains(rec.Body.String(), "hackspace") {
		t.Errorf("a button for one turned off: %s", rec.Body)
	}
}

func TestProviderSecretsAreNeverReturned(t *testing.T) {
	srv, _, admin := providerServer(t)
	req := testProviderRequest(map[string]string{"subject": "s"})
	req.Secrets = map[string]string{"secret": "hunter2"}
	p := saveProvider(t, srv, admin, "", req)
	if !p.SecretsSet["secret"] {
		t.Errorf("not set: %+v", p)
	}
	stored := mustProvider(t, srv, "hackspace")
	if strings.Contains(stored.SealedSecrets, "hunter2") {
		t.Error("stored readable")
	}
	rec := doJSON(t, srv, sessionCookie(t, srv, admin.ID), http.MethodGet, "/api/v1/auth/providers", "")
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "hunter2") || !strings.Contains(rec.Body.String(), `"secrets_set":{"secret":true}`) {
		t.Errorf("listed: %d %s", rec.Code, rec.Body)
	}
	// Saved without it, it is kept.
	req.Secrets = nil
	saveProvider(t, srv, admin, p.ID, req)
	if secrets, ok := srv.providerSecrets(mustProvider(t, srv, "hackspace")); !ok || secrets["secret"] != "hunter2" {
		t.Errorf("kept: %v %v", secrets, ok)
	}
	// Without a key, secrets cannot be kept.
	srv.sealer, _ = auth.NewSealer(nil)
	req.Key, req.Secrets = "other", map[string]string{"secret": "x"}
	body, _ := json.Marshal(req)
	if rec := doJSON(t, srv, sessionCookie(t, srv, admin.ID), http.MethodPost, "/api/v1/auth/providers", string(body)); rec.Code != http.StatusConflict {
		t.Errorf("without a key: %d %s", rec.Code, rec.Body)
	}
	// Nor can someone without settings.manage see them at all.
	reader, _ := srv.store.CreateUser("reader@test.org", "ReaderSecret123!", true)
	if rec := doJSON(t, srv, sessionCookie(t, srv, reader.ID), http.MethodGet, "/api/v1/auth/providers", ""); rec.Code != http.StatusForbidden {
		t.Errorf("without settings.manage: %d", rec.Code)
	}
}

func TestPasswordsOffAndBreakGlass(t *testing.T) {
	srv, _, admin := providerServer(t)
	cookie := sessionCookie(t, srv, admin.ID)
	if rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/auth/settings", `{"local_passwords": false}`); rec.Code != http.StatusConflict {
		t.Errorf("off with no provider: %d", rec.Code)
	}
	saveProvider(t, srv, admin, "", testProviderRequest(map[string]string{"subject": "s"}))
	if rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/auth/settings", `{"local_passwords": false}`); rec.Code != http.StatusOK {
		t.Fatalf("off: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, nil, http.MethodGet, "/api/v1/auth/providers/public", ""); !strings.Contains(rec.Body.String(), `"local_passwords":false`) {
		t.Errorf("public: %s", rec.Body)
	}
	post := func(path, email, password string) int {
		body, _ := json.Marshal(LoginRequest{Email: email, Password: password})
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(body))))
		return rec.Code
	}
	if code := post("/api/v1/auth/login", admin.Email, "AdminSecret123!"); code != http.StatusForbidden {
		t.Errorf("a password, while off: %d", code)
	}
	srv.store.CreateUser("member@test.org", "MemberSecret123!", true)
	if code := post("/api/v1/auth/login/local", "member@test.org", "MemberSecret123!"); code != http.StatusForbidden {
		t.Errorf("break-glass for someone who does not manage users: %d", code)
	}
	if code := post("/api/v1/auth/login/local", admin.Email, "wrong"); code != http.StatusUnauthorized {
		t.Errorf("break-glass with a wrong password: %d", code)
	}
	if code := post("/api/v1/auth/login/local", admin.Email, "AdminSecret123!"); code != http.StatusOK {
		t.Errorf("break-glass: %d", code)
	}
	events, _ := srv.store.ListEvents(1)
	if len(events) != 1 || events[0].Kind != models.EventAuthBreakGlass || events[0].Payload.Account.ID != admin.ID {
		t.Errorf("history: %+v", events)
	}
}

func TestIdentitiesAreLinkedAndUnlinked(t *testing.T) {
	srv, ts, admin := providerServer(t)
	p := saveProvider(t, srv, admin, "", testProviderRequest(map[string]string{"subject": "ada-at-idp", "email": "ada@hackspace.org"}))
	ada, _ := srv.store.CreateUser("ada@hackspace.org", "AdaSecret123!", true)
	cookie := sessionCookie(t, srv, admin.ID)

	// By hand, by an admin.
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/users/"+ada.ID+"/identities", `{"provider_id":"`+p.ID+`","subject":"ada-by-hand"}`); rec.Code != http.StatusCreated {
		t.Fatalf("by hand: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/users/"+admin.ID+"/identities", `{"provider_id":"`+p.ID+`","subject":"ada-by-hand"}`); rec.Code != http.StatusConflict {
		t.Errorf("someone else's: %d", rec.Code)
	}
	if rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/users/"+admin.ID+"/identities", `{"provider_key":"hackspace","subject":"admin-by-key"}`); rec.Code != http.StatusCreated {
		t.Errorf("by the provider's key: %d %s", rec.Code, rec.Body)
	}

	// By herself, signed in, through the provider.
	b := browser(t)
	resp, err := b.Post(ts.URL+"/api/v1/auth/login", "application/json", strings.NewReader(`{"email":"ada@hackspace.org","password":"AdaSecret123!"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	resp, err = b.Get(ts.URL + "/api/v1/auth/providers/hackspace/start?link=1")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if end := resp.Request.URL; end.Path != "/account" || end.Query().Get("linked") != "Hackspace" {
		t.Errorf("linking ended at %s", end)
	}
	resp, err = b.Get(ts.URL + "/api/v1/auth/me/identities")
	if err != nil {
		t.Fatal(err)
	}
	var mine struct {
		Identities []store.Identity `json:"identities"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&mine)
	resp.Body.Close()
	if len(mine.Identities) != 2 {
		t.Fatalf("hers: %+v", mine)
	}

	// Without a password, the last one stays.
	srv.store.SetPassword(ada.ID, "AdaSecret123!", "")
	for i, identity := range mine.Identities {
		rec := doJSON(t, srv, cookie, http.MethodDelete, "/api/v1/users/"+ada.ID+"/identities/"+identity.ID, "")
		if rec.Code != http.StatusNoContent {
			t.Errorf("unlinking %d: %d %s", i, rec.Code, rec.Body)
		}
	}
	if rec := doJSON(t, srv, cookie, http.MethodGet, "/api/v1/users/"+ada.ID+"/identities", ""); !strings.Contains(rec.Body.String(), `"identities":[]`) {
		t.Errorf("after unlinking: %s", rec.Body)
	}
	provisioned, _ := srv.store.CreateProvisionedUser("eve@hackspace.org", "Eve", nil)
	only, _ := srv.store.LinkIdentity(provisioned.ID, p.ID, "eve-at-idp", "")
	if rec := doJSON(t, srv, cookie, http.MethodDelete, "/api/v1/users/"+provisioned.ID+"/identities/"+only.ID, ""); rec.Code != http.StatusConflict {
		t.Errorf("the last way in: %d", rec.Code)
	}
}

func TestProvidersFromTheConfigurationAreReadOnly(t *testing.T) {
	cfg := config.DefaultConfig()
	testStorage(t, cfg)
	cfg.Auth.TestProtocol = true
	cfg.Auth.Providers = []config.ProviderConfig{{Key: "corp", Name: "Corp", Protocol: "test", Settings: map[string]string{"subject": "s"},
		Secrets: map[string]string{"secret": "from-the-file"}, DefaultRoles: []string{"admin"}}}
	st := setupTestStore(t)
	admin, _ := st.CreateAdminUser("admin@test.org", "AdminSecret123!")
	srv := NewServer(cfg, st)
	p := mustProvider(t, srv, "corp")
	if !p.FromConfig || len(p.DefaultRoleIDs) != 1 {
		t.Fatalf("synced: %+v", p)
	}
	if secrets, ok := srv.providerSecrets(p); !ok || secrets["secret"] != "from-the-file" {
		t.Errorf("secrets: %v", secrets)
	}
	body, _ := json.Marshal(ProviderRequest{Key: "corp", Name: "Changed", Protocol: "test", Settings: map[string]string{"subject": "s"}})
	cookie := sessionCookie(t, srv, admin.ID)
	if rec := doJSON(t, srv, cookie, http.MethodPut, "/api/v1/auth/providers/"+p.ID, string(body)); rec.Code != http.StatusConflict {
		t.Errorf("changed under Admin: %d", rec.Code)
	}
	if rec := doJSON(t, srv, cookie, http.MethodDelete, "/api/v1/auth/providers/"+p.ID, ""); rec.Code != http.StatusConflict {
		t.Errorf("deleted under Admin: %d", rec.Code)
	}
}
