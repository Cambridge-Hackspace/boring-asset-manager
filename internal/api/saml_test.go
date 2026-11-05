package api

import (
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/idp"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

func samlProviderRequest(idpMetadata string) ProviderRequest {
	return ProviderRequest{Key: "corp", Name: "Corp", Protocol: "saml", Enabled: true, Provision: true, TrustEmail: true,
		Settings: map[string]string{"metadata": idpMetadata}}
}

// samlMetadata reads BAM!'s metadata for the provider, as an identity
// provider does.
func samlMetadata(t *testing.T, ts string) string {
	t.Helper()
	res, err := http.Get(ts + "/auth/saml/corp/metadata")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "application/samlmetadata+xml" {
		t.Fatalf("metadata: %d %s %s", res.StatusCode, res.Header.Get("Content-Type"), body)
	}
	return string(body)
}

// noFollow is a browser that keeps cookies but stops at each redirect.
func noFollow(t *testing.T) *http.Client {
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func TestSigningInWithSAMLThroughTheAPI(t *testing.T) {
	srv, ts, admin := providerServer(t)
	provider := idp.NewTestSAMLProvider("https://idp.example.org/idp", "https://idp.example.org/sso")
	view := saveProvider(t, srv, admin, "", samlProviderRequest(provider.Metadata()))

	// BAM! made the provider a key, and publishes its metadata.
	if !view.SecretsSet["sp_key"] || view.SPMetadataURL != ts.URL+"/auth/saml/corp/metadata" {
		t.Fatalf("saved %+v", view)
	}
	md := samlMetadata(t, ts.URL)
	for _, want := range []string{`entityID="` + ts.URL + `/auth/saml/corp/metadata"`, `use="encryption"`,
		`Location="` + ts.URL + `/api/v1/auth/providers/corp/callback"`} {
		if !strings.Contains(md, want) {
			t.Errorf("metadata lacks %s", want)
		}
	}

	// The button sends the browser to the identity provider.
	browser := noFollow(t)
	res, err := browser.Get(ts.URL + "/api/v1/auth/providers/corp/start")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	req, err := provider.ReadRequest(res.Header.Get("Location"))
	if err != nil {
		t.Fatalf("sent to %s: %v", res.Header.Get("Location"), err)
	}
	answer, err := provider.Answer(req, idp.TestSAMLAnswer{NameID: "ada-1815", SignResponse: true,
		Attributes: map[string][]string{"mail": {"ada@corp.example.org"}, "displayName": {"Ada Lovelace"}}})
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{"SAMLResponse": {answer}, "RelayState": {req.RelayState}}

	// Posted from the provider's site, the answer comes without the
	// sign-in's cookie, and is posted again from BAM!'s own.
	res, err = http.PostForm(ts.URL+"/api/v1/auth/providers/corp/callback", form)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	page := html.UnescapeString(string(raw))
	if res.StatusCode != http.StatusOK || !strings.Contains(page, `name="bam_reposted"`) ||
		!strings.Contains(page, `name="SAMLResponse" value="`+answer+`"`) ||
		!strings.Contains(res.Header.Get("Content-Security-Policy"), "script-src 'sha256-") {
		t.Fatalf("not posted again: %d %s %s", res.StatusCode, res.Header, page)
	}

	// Posted again, the cookie goes with it, and Ada is signed in.
	form.Set(repostedField, "1")
	res, err = browser.PostForm(ts.URL+"/api/v1/auth/providers/corp/callback", form)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/" {
		t.Fatalf("callback: %d %s", res.StatusCode, res.Header.Get("Location"))
	}
	if me := signedInAs(t, browser, ts.URL); me == nil || me.Email != "ada@corp.example.org" {
		t.Errorf("signed in as %+v", me)
	}
}

// signedInAs is who the browser is signed in as, or nil.
func signedInAs(t *testing.T, c *http.Client, base string) *models.User {
	t.Helper()
	res, err := c.Get(base + "/api/v1/auth/me")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil
	}
	var me struct {
		User models.User `json:"user"`
	}
	if err := json.NewDecoder(res.Body).Decode(&me); err != nil {
		t.Fatal(err)
	}
	// Someone not signed in is answered too, as the public role.
	if me.User.ID == "" {
		return nil
	}
	return &me.User
}

func TestASAMLSignInStartedAtTheProviderIsTakenOnceWhenOn(t *testing.T) {
	srv, ts, admin := providerServer(t)
	provider := idp.NewTestSAMLProvider("https://idp.example.org/idp", "https://idp.example.org/sso")
	view := saveProvider(t, srv, admin, "", samlProviderRequest(provider.Metadata()))
	answer, err := provider.Answer(nil, idp.TestSAMLAnswer{NameID: "ada-1815", SignResponse: true,
		Recipient: ts.URL + "/api/v1/auth/providers/corp/callback", Audience: ts.URL + "/auth/saml/corp/metadata",
		Attributes: map[string][]string{"mail": {"ada@corp.example.org"}}})
	if err != nil {
		t.Fatal(err)
	}
	post := func() (*http.Client, *http.Response) {
		browser := noFollow(t)
		res, err := browser.PostForm(ts.URL+"/api/v1/auth/providers/corp/callback",
			url.Values{"SAMLResponse": {answer}, repostedField: {"1"}})
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return browser, res
	}

	// Off, as by default, it is refused.
	if browser, res := post(); signedInAs(t, browser, ts.URL) != nil {
		t.Fatalf("taken while off: %d %s", res.StatusCode, res.Header.Get("Location"))
	}

	req := samlProviderRequest(provider.Metadata())
	req.Settings["idp_initiated"] = "on"
	saveProvider(t, srv, admin, view.ID, req)
	if browser, res := post(); signedInAs(t, browser, ts.URL) == nil {
		t.Fatalf("refused while on: %d %s", res.StatusCode, res.Header.Get("Location"))
	}
	// The same assertion again is refused.
	if browser, _ := post(); signedInAs(t, browser, ts.URL) != nil {
		t.Error("the assertion signed in twice")
	}
}

func TestASAMLProvidersKeyIsMadeAgainWhenAsked(t *testing.T) {
	srv, ts, admin := providerServer(t)
	provider := idp.NewTestSAMLProvider("https://idp.example.org/idp", "https://idp.example.org/sso")
	req := samlProviderRequest(provider.Metadata())
	view := saveProvider(t, srv, admin, "", req)
	before := samlMetadata(t, ts.URL)

	// Saved again, it keeps its key.
	saveProvider(t, srv, admin, view.ID, req)
	if samlMetadata(t, ts.URL) != before {
		t.Error("the key changed without being asked to")
	}
	req.RenewKeys = true
	saveProvider(t, srv, admin, view.ID, req)
	if samlMetadata(t, ts.URL) == before {
		t.Error("the key was not made again")
	}

	// Another protocol's provider has no SAML metadata.
	other := testProviderRequest(map[string]string{"subject": "s"})
	saveProvider(t, srv, admin, "", other)
	if res, err := http.Get(ts.URL + "/auth/saml/hackspace/metadata"); err != nil || res.StatusCode != http.StatusNotFound {
		t.Errorf("another protocol's: %v %v", res.StatusCode, err)
	}
}
