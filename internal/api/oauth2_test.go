package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestAGitHubProviderIsSavedAndSendsTheBrowserToGitHub(t *testing.T) {
	srv, ts, admin := providerServer(t)
	req := ProviderRequest{Key: "github", Name: "GitHub", Protocol: "oauth2", Enabled: true,
		Settings: map[string]string{"preset": "github", "client_id": "Iv1.bam"}, Secrets: map[string]string{"client_secret": "s3cret"}}

	// A preset that does not exist is refused, saying why.
	bad := req
	bad.Settings = map[string]string{"preset": "myspace", "client_id": "Iv1.bam"}
	body, _ := json.Marshal(bad)
	if rec := doJSON(t, srv, sessionCookie(t, srv, admin.ID), http.MethodPost, "/api/v1/auth/providers", string(body)); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "Preset must be github, discord or blank") {
		t.Errorf("an unknown preset: %d %s", rec.Code, rec.Body)
	}
	saveProvider(t, srv, admin, "", req)

	// Admin lists the protocol and its fields.
	rec := doJSON(t, srv, sessionCookie(t, srv, admin.ID), http.MethodGet, "/api/v1/auth/providers", "")
	var listed struct {
		Protocols []struct {
			Name   string `json:"name"`
			Label  string `json:"label"`
			Fields []struct {
				Name string `json:"name"`
			} `json:"fields"`
		} `json:"protocols"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&listed)
	found := false
	for _, p := range listed.Protocols {
		if p.Name == "oauth2" && p.Label == "OAuth2" && len(p.Fields) > 0 && p.Fields[0].Name == "preset" {
			found = true
		}
	}
	if !found {
		t.Errorf("protocols %+v", listed.Protocols)
	}

	// Its button sends the browser to GitHub, with state and PKCE.
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := noFollow.Get(ts.URL + "/api/v1/auth/providers/github/start")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	to, _ := url.Parse(res.Header.Get("Location"))
	q := to.Query()
	if to.Host != "github.com" || to.Path != "/login/oauth/authorize" || q.Get("client_id") != "Iv1.bam" || q.Get("state") == "" ||
		q.Get("code_challenge") == "" || q.Get("redirect_uri") != ts.URL+"/api/v1/auth/providers/github/callback" {
		t.Errorf("sent to %s", to)
	}
}
