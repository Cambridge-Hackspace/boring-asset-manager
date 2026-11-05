package idp

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/beevik/etree"
)

var (
	samlKeyOnce sync.Once
	samlKeyPEM  string
)

// testSAMLKey is BAM!'s key in the tests, made once, as making one is slow.
func testSAMLKey(t *testing.T) string {
	samlKeyOnce.Do(func() {
		c := Config{}
		if _, err := (&SAML{}).Prepare(&c, false); err != nil {
			t.Fatal(err)
		}
		samlKeyPEM = c.Secrets["sp_key"]
	})
	return samlKeyPEM
}

type samlTest struct {
	s    *SAML
	idp  *TestSAMLProvider
	c    Config
	used map[string]time.Time
}

func newSAMLTest(t *testing.T) *samlTest {
	idp := NewTestSAMLProvider("https://idp.example.org/idp", "https://idp.example.org/sso")
	st := &samlTest{s: &SAML{}, idp: idp, used: map[string]time.Time{}}
	st.c = Config{
		Key: "corp", BaseURL: "https://bam.example.org", State: "state-1",
		CallbackURL: "https://bam.example.org/api/v1/auth/providers/corp/callback",
		Settings:    map[string]string{"metadata": idp.Metadata()},
		Secrets:     map[string]string{"sp_key": testSAMLKey(t)},
		UseOnce: func(id string, until time.Time) error {
			if _, ok := st.used[id]; ok {
				return ErrReplayed
			}
			st.used[id] = until
			return nil
		},
	}
	if err := st.s.Check(st.c); err != nil {
		t.Fatal(err)
	}
	return st
}

// begin starts a sign-in, answering the request as the provider reads it
// and what BAM! kept.
func (st *samlTest) begin(t *testing.T) (*TestSAMLRequest, map[string]string) {
	t.Helper()
	redirect, kept, err := st.s.Begin(context.Background(), st.c)
	if err != nil {
		t.Fatal(err)
	}
	req, err := st.idp.ReadRequest(redirect)
	if err != nil {
		t.Fatal(err)
	}
	return req, kept
}

// post is the browser posting an answer to BAM!.
func (st *samlTest) post(kept map[string]string, response, relay string) (*Claims, error) {
	form := url.Values{"SAMLResponse": {response}, "RelayState": {relay}}
	r := httptest.NewRequest(http.MethodPost, st.c.CallbackURL, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return st.s.Finish(context.Background(), st.c, r, kept)
}

// answer signs in as ada, as told, answering the claims or the error.
func (st *samlTest) answer(t *testing.T, a TestSAMLAnswer) (*Claims, error) {
	t.Helper()
	req, kept := st.begin(t)
	if a.NameID == "" {
		a.NameID = "ada-1815"
	}
	resp, err := st.idp.Answer(req, a)
	if err != nil {
		t.Fatal(err)
	}
	return st.post(kept, resp, req.RelayState)
}

func TestSigningInWithSAML(t *testing.T) {
	st := newSAMLTest(t)
	req, kept := st.begin(t)

	// The request: for here, persistent, and signed with BAM!'s key,
	// whose certificate the metadata publishes.
	if req.ACSURL != st.c.CallbackURL || req.Issuer != "https://bam.example.org/auth/saml/corp/metadata" ||
		req.NameIDFormat != nameIDPersist || req.RelayState != "state-1" || req.ID == "" {
		t.Fatalf("request %+v", req)
	}
	md, _, err := st.s.Publish(st.c)
	if err != nil {
		t.Fatal(err)
	}
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(md); err != nil {
		t.Fatal(err)
	}
	der, _ := base64.StdEncoding.DecodeString(doc.FindElement("//X509Certificate").Text())
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	sig, _ := base64.StdEncoding.DecodeString(req.Query.Get("Signature"))
	sum := sha256.Sum256([]byte(req.Signed))
	if req.Query.Get("SigAlg") != rsaSHA256 || rsa.VerifyPKCS1v15(cert.PublicKey.(*rsa.PublicKey), crypto.SHA256, sum[:], sig) != nil {
		t.Error("the request's signature does not check against the published certificate")
	}

	resp, err := st.idp.Answer(req, TestSAMLAnswer{
		NameID: "ada-1815", SignResponse: true, ACR: "https://refeds.org/profile/mfa",
		Attributes: map[string][]string{
			"urn:oid:0.9.2342.19200300.100.1.3": {"ada@example.org"},
			"displayName":                       {"Ada Lovelace"},
			"memberOf":                          {"woodshop", "admins"},
			"department":                        {"Engines"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	c, err := st.post(kept, resp, req.RelayState)
	if err != nil {
		t.Fatal(err)
	}
	if c.Subject != "ada-1815" || c.Email != "ada@example.org" || c.EmailVerified || c.Name != "Ada Lovelace" ||
		c.ACR != "https://refeds.org/profile/mfa" || !c.Has("groups", "woodshop") || !c.Has("department", "engines") {
		t.Errorf("claims %+v", c)
	}
	if len(st.used) != 1 {
		t.Errorf("assertion IDs used %v", st.used)
	}
}

func TestASignedOrEncryptedAssertionSignsIn(t *testing.T) {
	st := newSAMLTest(t)
	_, cert := samlKeyAndCert(t, st.c)
	for name, a := range map[string]TestSAMLAnswer{
		"the assertion signed":                  {SignAssertion: true},
		"both signed":                           {SignAssertion: true, SignResponse: true},
		"encrypted with AES-GCM, signed within": {SignAssertion: true, EncryptTo: cert},
		"encrypted with AES-CBC, signed within": {SignAssertion: true, EncryptTo: cert, CBC: true},
		"encrypted, in a signed response":       {SignResponse: true, EncryptTo: cert},
	} {
		if c, err := st.answer(t, a); err != nil || c.Subject != "ada-1815" {
			t.Errorf("%s: %+v %v", name, c, err)
		}
	}
}

func samlKeyAndCert(t *testing.T, c Config) (*rsa.PrivateKey, *x509.Certificate) {
	key, err := samlKey(c)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := samlCertificate(c, key)
	if err != nil {
		t.Fatal(err)
	}
	return key, cert
}

func TestABadSAMLAnswerIsRefused(t *testing.T) {
	st := newSAMLTest(t)
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	otherKey := Config{Key: "corp", Secrets: map[string]string{"sp_key": string(pem.EncodeToMemory(&pem.Block{
		Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(other)}))}}
	_, otherCert := samlKeyAndCert(t, otherKey)
	_, cert := samlKeyAndCert(t, st.c)
	for name, a := range map[string]TestSAMLAnswer{
		"unsigned":                            {},
		"signed by another key":               {SignResponse: true, SignWith: other},
		"the assertion signed by another key": {SignAssertion: true, SignWith: other},
		"expired":                             {SignResponse: true, NotOnOrAfter: time.Now().Add(-10 * time.Minute)},
		"for another audience":                {SignResponse: true, Audience: "https://elsewhere.example.org"},
		"for another recipient":               {SignResponse: true, Recipient: "https://elsewhere.example.org/acs"},
		"for another request":                 {SignResponse: true, InResponseTo: "_another"},
		"from another issuer":                 {SignResponse: true, Issuer: "https://idp.example.com/evil"},
		"with a transient NameID":             {SignResponse: true, NameIDFormat: nameIDTransient},
		"with an email NameID, not asked for": {SignResponse: true, NameIDFormat: nameIDEmail, NameID: "ada@example.org"},
		"encrypted to another key":            {SignResponse: true, EncryptTo: otherCert},
		"encrypted, unsigned within":          {EncryptTo: cert},
	} {
		if c, err := st.answer(t, a); err == nil {
			t.Errorf("%s: signed in as %+v", name, c)
		} else if errors.Is(err, ErrRefused) {
			t.Errorf("%s: taken for a refusal: %v", name, err)
		}
	}

	// A failure's status is the provider refusing.
	if _, err := st.answer(t, TestSAMLAnswer{Status: "urn:oasis:names:tc:SAML:2.0:status:Responder"}); !errors.Is(err, ErrRefused) {
		t.Errorf("a failed status: %v", err)
	}

	// Another sign-in's RelayState.
	req, kept := st.begin(t)
	resp, _ := st.idp.Answer(req, TestSAMLAnswer{NameID: "ada-1815", SignResponse: true})
	if _, err := st.post(kept, resp, "state-2"); err == nil {
		t.Error("another sign-in's RelayState was taken")
	}
}

func TestASAMLAssertionIsTakenOnce(t *testing.T) {
	st := newSAMLTest(t)
	if _, err := st.answer(t, TestSAMLAnswer{SignResponse: true, AssertionID: "_once"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.answer(t, TestSAMLAnswer{SignResponse: true, AssertionID: "_once"}); !errors.Is(err, ErrReplayed) {
		t.Errorf("replayed: %v", err)
	}
	// Kept until it expires, and a little after, for the clock allowance.
	if until := st.used["_once"]; until.Before(time.Now().Add(5*time.Minute)) || until.After(time.Now().Add(10*time.Minute)) {
		t.Errorf("kept until %s", until)
	}
}

// wrapped is a signed answer whose signed assertion is moved inside one of
// the attacker's, as signature wrapping does.
func wrapped(t *testing.T, st *samlTest, req *TestSAMLRequest, sameID bool) string {
	t.Helper()
	x, err := st.idp.AnswerXML(req, TestSAMLAnswer{NameID: "ada-1815", SignAssertion: true})
	if err != nil {
		t.Fatal(err)
	}
	doc := etree.NewDocument()
	if err := doc.ReadFromString(x); err != nil {
		t.Fatal(err)
	}
	original := doc.FindElement("//Assertion")
	evil := original.Copy()
	evil.FindElement("./Subject/NameID").SetText("mallory")
	if !sameID {
		evil.RemoveAttr("ID")
		evil.CreateAttr("ID", "_evil")
	}
	// The original, signed, hidden inside the attacker's.
	evil.CreateElement("saml:Advice").AddChild(original.Copy())
	resp := doc.Root()
	resp.RemoveChild(original)
	resp.AddChild(evil)
	out, _ := doc.WriteToString()
	return base64.StdEncoding.EncodeToString([]byte(out))
}

func TestAWrappedSAMLSignatureIsRefused(t *testing.T) {
	st := newSAMLTest(t)
	for _, sameID := range []bool{false, true} {
		req, kept := st.begin(t)
		if c, err := st.post(kept, wrapped(t, st, req, sameID), req.RelayState); err == nil {
			t.Errorf("same ID %v: signed in as %+v", sameID, c)
		}
	}

	// Two assertions, the attacker's beside the signed one.
	req, kept := st.begin(t)
	x, _ := st.idp.AnswerXML(req, TestSAMLAnswer{NameID: "ada-1815", SignAssertion: true})
	doc := etree.NewDocument()
	_ = doc.ReadFromString(x)
	evil := doc.FindElement("//Assertion").Copy()
	evil.RemoveChild(evil.FindElement("./Signature"))
	evil.FindElement("./Subject/NameID").SetText("mallory")
	doc.Root().InsertChildAt(2, evil)
	out, _ := doc.WriteToString()
	if c, err := st.post(kept, base64.StdEncoding.EncodeToString([]byte(out)), req.RelayState); err == nil {
		t.Errorf("two assertions: signed in as %+v", c)
	}

	// A DOCTYPE, for entity expansion.
	req, kept = st.begin(t)
	x, _ = st.idp.AnswerXML(req, TestSAMLAnswer{NameID: "ada-1815", SignResponse: true})
	x = `<!DOCTYPE r [<!ENTITY a "a">]>` + strings.TrimPrefix(x, `<?xml version="1.0" encoding="UTF-8"?>`)
	if _, err := st.post(kept, base64.StdEncoding.EncodeToString([]byte(x)), req.RelayState); err == nil {
		t.Error("a DOCTYPE was taken")
	}
}

func TestASAMLSignInStartedAtTheProvider(t *testing.T) {
	st := newSAMLTest(t)
	unsolicited := func() (*Claims, error) {
		resp, err := st.idp.Answer(nil, TestSAMLAnswer{NameID: "ada-1815", SignResponse: true,
			Recipient: st.c.CallbackURL, Audience: SAMLEntityID(st.c)})
		if err != nil {
			t.Fatal(err)
		}
		return st.post(map[string]string{}, resp, "")
	}
	if c, err := unsolicited(); err == nil {
		t.Errorf("taken while off: %+v", c)
	}
	st.c.Settings["idp_initiated"] = "on"
	if !st.s.AcceptsUnsolicited(st.c) {
		t.Fatal("not taken when on")
	}
	if c, err := unsolicited(); err != nil || c.Subject != "ada-1815" {
		t.Errorf("taken when on: %+v %v", c, err)
	}
	// One for a request, with none started here, is not unsolicited.
	resp, _ := st.idp.Answer(nil, TestSAMLAnswer{NameID: "ada-1815", SignResponse: true, InResponseTo: "_someone-elses",
		Recipient: st.c.CallbackURL, Audience: SAMLEntityID(st.c)})
	if c, err := st.post(map[string]string{}, resp, ""); err == nil {
		t.Errorf("another's answer taken as unsolicited: %+v", c)
	}
}

func TestASAMLSignInAsksForALevelWhenSet(t *testing.T) {
	st := newSAMLTest(t)
	if req, _ := st.begin(t); len(req.AuthnContext) != 0 {
		t.Errorf("asked for %v unset", req.AuthnContext)
	}
	st.c.Settings["authn_context"] = "https://refeds.org/profile/mfa  urn:oasis:names:tc:SAML:2.0:ac:classes:TimeSyncToken"
	req, _ := st.begin(t)
	if len(req.AuthnContext) != 2 || req.AuthnContext[0] != "https://refeds.org/profile/mfa" {
		t.Errorf("asked for %v", req.AuthnContext)
	}
}

func TestAnEmailNameIDIsTheSubjectWhenAskedFor(t *testing.T) {
	st := newSAMLTest(t)
	st.c.Settings["nameid_format"] = "email"
	req, _ := st.begin(t)
	if req.NameIDFormat != nameIDEmail {
		t.Errorf("asked for %s", req.NameIDFormat)
	}
	c, err := st.answer(t, TestSAMLAnswer{SignResponse: true, NameIDFormat: nameIDEmail, NameID: "Ada@Example.org"})
	if err != nil || c.Subject != "ada@example.org" || c.Email != "Ada@Example.org" {
		t.Errorf("%+v %v", c, err)
	}
}

func TestSAMLAttributeSettings(t *testing.T) {
	st := newSAMLTest(t)
	st.c.Settings["email_attribute"] = "work_mail, mail"
	st.c.Settings["name_attribute"] = "fullName"
	st.c.Settings["groups_attribute"] = "teams"
	c, err := st.answer(t, TestSAMLAnswer{SignResponse: true, Attributes: map[string][]string{
		"mail": {"ada@home.example.org"}, "fullName": {"Ada"}, "teams": {"engines"}, "groups": {"ignored"}}})
	if err != nil || c.Email != "ada@home.example.org" || c.Name != "Ada" || !c.Has("groups", "engines") || c.Has("groups", "ignored") {
		t.Errorf("%+v %v", c, err)
	}
}

func TestSAMLMetadataIsReadFromItsURLDaily(t *testing.T) {
	idp := NewTestSAMLProvider("https://idp.example.org/idp", "https://idp.example.org/sso")
	hits, fail := 0, false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if fail {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(idp.Metadata()))
	}))
	defer srv.Close()
	now := time.Now()
	s := &SAML{Now: func() time.Time { return now }}
	c := Config{Key: "corp", Settings: map[string]string{"metadata_url": srv.URL}, Secrets: map[string]string{}}
	if err := s.Check(c); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, _, err := s.Begin(context.Background(), c); err != nil {
			t.Fatal(err)
		}
	}
	if hits != 1 {
		t.Errorf("read %d times in a day", hits)
	}
	now = now.Add(25 * time.Hour)
	if _, _, err := s.Begin(context.Background(), c); err != nil || hits != 2 {
		t.Errorf("a day on: %d reads, %v", hits, err)
	}
	// When it cannot be read again, the copy kept is used.
	fail, now = true, now.Add(25*time.Hour)
	if _, _, err := s.Begin(context.Background(), c); err != nil || hits != 3 {
		t.Errorf("unreadable: %d reads, %v", hits, err)
	}
}

func TestSAMLSettings(t *testing.T) {
	idp := NewTestSAMLProvider("https://idp.example.org/idp", "https://idp.example.org/sso")
	key := testSAMLKey(t)
	s := &SAML{}
	check := func(settings map[string]string, secrets map[string]string) string {
		if secrets == nil {
			secrets = map[string]string{}
		}
		if err := s.Check(Config{Settings: settings, Secrets: secrets}); err != nil {
			return err.Error()
		}
		return ""
	}
	federation := `<EntitiesDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata">` +
		strings.TrimPrefix(idp.Metadata(), `<?xml version="1.0" encoding="UTF-8"?>`) +
		strings.ReplaceAll(strings.TrimPrefix(idp.Metadata(), `<?xml version="1.0" encoding="UTF-8"?>`), "idp.example.org/idp", "idp.example.com/idp") +
		`</EntitiesDescriptor>`
	for _, c := range []struct {
		settings, secrets map[string]string
		want              string
	}{
		{map[string]string{}, nil, "Give the metadata URL or paste the metadata XML"},
		{map[string]string{"metadata_url": "https://idp.example.org/md", "metadata": idp.Metadata()}, nil, "not both"},
		{map[string]string{"metadata_url": "http://idp.example.org/md"}, nil, "Metadata URL must be https"},
		{map[string]string{"metadata": "<nope"}, nil, "Metadata XML: it is not XML"},
		{map[string]string{"metadata": strings.ReplaceAll(idp.Metadata(), "HTTP-Redirect", "HTTP-Artifact")}, nil, "HTTP-Redirect"},
		{map[string]string{"metadata": federation}, nil, "it has 2 identity providers"},
		{map[string]string{"metadata": federation, "entity_id": "https://idp.example.com/idp"}, nil, ""},
		{map[string]string{"metadata": federation, "entity_id": "https://idp.example.net/idp"}, nil, "no identity provider"},
		{map[string]string{"metadata": idp.Metadata(), "nameid_format": "transient"}, nil, "NameID format must be"},
		{map[string]string{"metadata": idp.Metadata(), "idp_initiated": "yes"}, nil, "must be on, off or blank"},
		{map[string]string{"metadata": idp.Metadata()}, map[string]string{"sp_key": "nope"}, "Service provider key: it is not a PEM private key"},
		// A key pasted into a one-line field, its line breaks lost.
		{map[string]string{"metadata": idp.Metadata()}, map[string]string{"sp_key": strings.ReplaceAll(key, "\n", " ")}, ""},
		{map[string]string{"metadata_url": "https://idp.example.org/md", "nameid_format": "email", "idp_initiated": "on"}, nil, ""},
	} {
		if got := check(c.settings, c.secrets); (c.want == "") != (got == "") || !strings.Contains(got, c.want) {
			t.Errorf("%v: %q, want %q", c.settings, got, c.want)
		}
	}
}

func TestSAMLKeysAreMadeAndRenewed(t *testing.T) {
	s := &SAML{}
	c := Config{Key: "corp", BaseURL: "https://bam.example.org/", CallbackURL: "https://bam.example.org/acs",
		Settings: map[string]string{}, Secrets: map[string]string{"sp_key": testSAMLKey(t)}}
	if changed, err := s.Prepare(&c, false); err != nil || changed {
		t.Errorf("a provider with a key: %v %v", changed, err)
	}
	// The certificate is made from the key alone, the same each time.
	first, _, _ := s.Publish(c)
	again, _, _ := s.Publish(c)
	if string(first) != string(again) {
		t.Error("the metadata changed without the key")
	}
	for _, want := range []string{`entityID="https://bam.example.org/auth/saml/corp/metadata"`, `AuthnRequestsSigned="true"`,
		`use="signing"`, `use="encryption"`, `Location="https://bam.example.org/acs"`, nameIDPersist} {
		if !strings.Contains(string(first), want) {
			t.Errorf("metadata lacks %s:\n%s", want, first)
		}
	}
	if changed, err := s.Prepare(&c, true); err != nil || !changed || c.Secrets["sp_key"] == testSAMLKey(t) {
		t.Errorf("renewed: %v %v", changed, err)
	}
	renewed, _, _ := s.Publish(c)
	if string(renewed) == string(first) {
		t.Error("the certificate is the same with a new key")
	}

	// Without a key, as for one in the configuration file with none set,
	// requests go unsigned and no certificate is published.
	c.Secrets = map[string]string{}
	md, _, _ := s.Publish(c)
	if !strings.Contains(string(md), `AuthnRequestsSigned="false"`) || strings.Contains(string(md), "KeyDescriptor") {
		t.Errorf("without a key:\n%s", md)
	}
}
