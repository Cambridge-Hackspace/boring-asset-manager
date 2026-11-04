package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/idp"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// oidcProvider is a test OpenID Connect provider beside srv, saved as the
// provider "hackspace" with req's other settings.
func oidcProvider(t *testing.T, srv *Server, adminID string, req ProviderRequest) *idp.TestOIDCProvider {
	t.Helper()
	p := idp.NewTestOIDCProvider("", "bam", "s3cret")
	ts := httptest.NewServer(p)
	t.Cleanup(ts.Close)
	p.Issuer = ts.URL
	req.Key, req.Name, req.Protocol, req.Enabled = "hackspace", "Hackspace", idp.OIDCName, true
	req.Settings = map[string]string{"issuer": ts.URL, "client_id": "bam", "scopes": "openid email profile groups"}
	req.Secrets = map[string]string{"client_secret": "s3cret"}
	admin, _ := srv.store.GetUserByID(adminID)
	saveProvider(t, srv, admin, "", req)
	return p
}

// throughOIDC starts a sign-in, fills in the provider's form with who and
// follows the browser back, answering where it ended.
func throughOIDC(t *testing.T, ts *httptest.Server, b *http.Client, p *idp.TestOIDCProvider, next string, who url.Values) *url.URL {
	t.Helper()
	return throughIssuer(t, ts, b, p.Issuer, next, who)
}

func throughIssuer(t *testing.T, ts *httptest.Server, b *http.Client, issuer, next string, who url.Values) *url.URL {
	t.Helper()
	form := signInThrough(t, ts, b, "hackspace", next)
	if !strings.HasPrefix(form.String(), issuer+"/authorize") {
		t.Fatalf("not at the provider's form: %s", form)
	}
	ask := form.Query()
	for k, v := range who {
		ask[k] = v
	}
	resp, err := b.PostForm(issuer+"/authorize", ask)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp.Request.URL
}

func TestSigningInThroughOpenIDConnect(t *testing.T) {
	srv, ts, admin := providerServer(t)
	woodshop, _ := srv.store.CreateRole(store.RoleInput{Name: "Woodshop", Permissions: []string{rbac.PermAssetsRead}})
	p := oidcProvider(t, srv, admin.ID, ProviderRequest{Provision: true,
		Rules: []store.ProviderRule{{Claim: "groups", Value: "woodshop", RoleID: woodshop.ID}}})

	b := browser(t)
	end := throughOIDC(t, ts, b, p, "/assets", url.Values{"sub": {"ada-1"}, "email": {"ada@hackspace.org"}, "name": {"Ada"}, "groups": {"woodshop"}})
	if end.Path != "/assets" || me(t, ts, b) != "ada@hackspace.org" {
		t.Fatalf("ended at %s as %q", end, me(t, ts, b))
	}
	// The groups came from userinfo, and gave the role.
	ada, _ := srv.store.GetUserByEmail("ada@hackspace.org")
	all, _ := srv.store.UserRoleAssignments()
	if len(all[ada.ID]) != 1 || all[ada.ID][0].RoleID != woodshop.ID {
		t.Errorf("roles: %+v", all[ada.ID])
	}

	// The same subject again is the same account.
	b = browser(t)
	if end := throughOIDC(t, ts, b, p, "/", url.Values{"sub": {"ada-1"}, "email": {"ada@hackspace.org"}}); end.Path != "/" || me(t, ts, b) != "ada@hackspace.org" {
		t.Errorf("again: ended at %s", end)
	}
}

func TestAFailedOpenIDConnectSignInSaysSoAndNoMore(t *testing.T) {
	srv, ts, admin := providerServer(t)
	p := oidcProvider(t, srv, admin.ID, ProviderRequest{Provision: true})

	// Cancelled at the provider.
	b := browser(t)
	end := throughOIDC(t, ts, b, p, "/assets", url.Values{"action": {"deny"}})
	if end.Path != "/sign-in" || end.Query().Get("error") != "Hackspace did not sign you in." || end.Query().Get("next") != "/assets" {
		t.Errorf("cancelled: ended at %s", end)
	}

	// A token that does not check names neither the reason nor the secret.
	p.BreakSignature = true
	b = browser(t)
	end = throughOIDC(t, ts, b, p, "/", url.Values{"sub": {"eve"}, "email": {"eve@hackspace.org"}})
	if msg := end.Query().Get("error"); end.Path != "/sign-in" || msg != "The answer from Hackspace could not be checked. Sign in again." {
		t.Errorf("a bad signature: ended at %s", end)
	}
	if me(t, ts, b) != "" {
		t.Error("signed in")
	}
	if _, err := srv.store.GetUserByEmail("eve@hackspace.org"); err == nil {
		t.Error("an account was made")
	}
}

func TestSigningInThroughTheTestProviderBesideBAM(t *testing.T) {
	// The server's own address is its base URL, as in the browser tests.
	var h http.Handler
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.ServeHTTP(w, r) }))
	t.Cleanup(ts.Close)
	cfg := config.DefaultConfig()
	testStorage(t, cfg)
	cfg.Auth.SecretKey, cfg.Auth.TestProtocol, cfg.Server.BaseURL = testSecretKey, true, ts.URL
	st := setupTestStore(t)
	admin, _ := st.CreateAdminUser("admin@test.org", "AdminSecret123!")
	srv := NewServer(cfg, st)
	h = srv.Handler()
	saveProvider(t, srv, admin, "", ProviderRequest{Key: "hackspace", Name: "Hackspace", Protocol: idp.OIDCName, Enabled: true, Provision: true,
		Settings: map[string]string{"issuer": ts.URL + "/test-oidc", "client_id": TestOIDCClientID},
		Secrets:  map[string]string{"client_secret": TestOIDCClientSecret}})

	// Its userinfo reads its own bearer tokens, not BAM!'s.
	b := browser(t)
	end := throughIssuer(t, ts, b, ts.URL+"/test-oidc", "/", url.Values{"sub": {"grace"}, "email": {"grace@hackspace.org"}, "groups": {"members"}})
	if end.Path != "/" || me(t, ts, b) != "grace@hackspace.org" {
		t.Errorf("ended at %s as %q", end, me(t, ts, b))
	}
}

func TestTheTestOpenIDConnectProviderIsServedOnlyForTests(t *testing.T) {
	srv, _, _ := providerServer(t)
	srv.cfg.Server.BaseURL = "http://bam.test"
	srv.cfg.Auth.TestProtocol = true
	on := NewServer(srv.cfg, srv.store)
	rec := httptest.NewRecorder()
	on.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/test-oidc/.well-known/openid-configuration", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"issuer":"http://bam.test/test-oidc"`) {
		t.Errorf("on: %d %s", rec.Code, rec.Body)
	}

	srv.cfg.Auth.TestProtocol = false
	off := NewServer(srv.cfg, srv.store)
	rec = httptest.NewRecorder()
	off.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/test-oidc/.well-known/openid-configuration", nil))
	if strings.Contains(rec.Body.String(), "issuer") {
		t.Errorf("off: %d %s", rec.Code, rec.Body)
	}
}
