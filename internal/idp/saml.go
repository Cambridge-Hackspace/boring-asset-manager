package idp

import (
	"bytes"
	"compress/flate"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/beevik/etree"
	dsig "github.com/russellhaering/goxmldsig"
	"github.com/russellhaering/goxmldsig/etreeutils"
)

// SAMLName is SAML 2.0's protocol key.
const SAMLName = "saml"

const (
	samlProtocolNS  = "urn:oasis:names:tc:SAML:2.0:protocol"
	samlAssertionNS = "urn:oasis:names:tc:SAML:2.0:assertion"
	samlMetadataNS  = "urn:oasis:names:tc:SAML:2.0:metadata"
	samlRedirect    = "urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect"
	samlPOST        = "urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST"
	samlBearer      = "urn:oasis:names:tc:SAML:2.0:cm:bearer"
	samlSuccess     = "urn:oasis:names:tc:SAML:2.0:status:Success"
	nameIDPersist   = "urn:oasis:names:tc:SAML:2.0:nameid-format:persistent"
	nameIDTransient = "urn:oasis:names:tc:SAML:2.0:nameid-format:transient"
	nameIDEmail     = "urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress"
	rsaSHA256       = "http://www.w3.org/2001/04/xmldsig-more#rsa-sha256"
	// samlMetadataType is the content type of SAML metadata.
	samlMetadataType = "application/samlmetadata+xml"
)

// samlSkew is how far the provider's clock may be from BAM!'s.
const samlSkew = 3 * time.Minute

// samlMetadataAge is how long metadata read from a URL is kept before it
// is read again.
const samlMetadataAge = 24 * time.Hour

// The attributes tried for the email, the name and the groups when their
// settings are blank: the usual names, the LDAP OIDs, and those of ADFS
// and Entra.
var (
	samlEmailAttributes = []string{"email", "mail", "urn:oid:0.9.2342.19200300.100.1.3",
		"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress"}
	samlNameAttributes = []string{"displayName", "name", "urn:oid:2.16.840.1.113730.3.1.241",
		"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/name", "cn"}
	samlGroupsAttributes = []string{"groups", "memberOf", "urn:oid:1.3.6.1.4.1.5923.1.5.1.1",
		"http://schemas.microsoft.com/ws/2008/06/identity/claims/groups", "http://schemas.xmlsoap.org/claims/Group"}
)

// SAML signs in through SAML 2.0 identity providers, with BAM! as the
// service provider (docs/access.md#saml-20). The provider's signatures are
// checked by goxmldsig (docs/architecture.md#saml), and everything is read
// from the element whose signature was checked, never from the document
// it came in.
type SAML struct {
	// Client reads providers' metadata; nil is one with a timeout.
	Client *http.Client
	// Now is the clock, for tests; nil is time.Now.
	Now func() time.Time

	mu       sync.Mutex
	metadata map[string]samlMetadataCache
}

// samlMetadataCache is a provider's metadata as last read from its URL.
type samlMetadataCache struct {
	md *samlIDP
	at time.Time
}

// samlIDP is what BAM! needs of an identity provider's metadata.
type samlIDP struct {
	EntityID string
	// SSOURL takes sign-ins by HTTP-Redirect.
	SSOURL string
	Certs  []*x509.Certificate
}

func (*SAML) Name() string  { return SAMLName }
func (*SAML) Label() string { return "SAML 2.0" }

func (*SAML) Fields() []Field {
	return []Field{
		{Name: "metadata_url", Label: "Metadata URL",
			Help: "Where the identity provider publishes its metadata, read again daily. Give this or the metadata XML."},
		{Name: "metadata", Label: "Metadata XML", Multiline: true,
			Help: "The identity provider's metadata, pasted, for one that does not publish it at a URL."},
		{Name: "entity_id", Label: "Identity provider entity ID",
			Help: "Which identity provider, for metadata that lists several, such as a federation's. Blank is the only one."},
		{Name: "nameid_format", Label: "NameID format",
			Help: "persistent (blank) or email. Use email only when an email at this provider never passes from one person to another."},
		{Name: "email_attribute", Label: "Email attribute",
			Help: "Blank tries email, mail, and their usual OID and ADFS names. Several separated by commas are tried in order."},
		{Name: "name_attribute", Label: "Name attribute",
			Help: "Blank tries displayName, name, cn, and their usual OID and ADFS names."},
		{Name: "groups_attribute", Label: "Groups attribute",
			Help: "Blank tries groups, memberOf, and their usual OID, ADFS and Entra names."},
		{Name: "authn_context", Label: "Ask for (AuthnContextClassRef)",
			Help: "Classes to ask the identity provider to sign people in at, separated by spaces, such as one that requires a second factor. Blank asks for none."},
		{Name: "idp_initiated", Label: "Sign-ins started at the provider",
			Help: "on to take sign-ins started from the identity provider's own page. Blank or off refuses them."},
		{Name: "sp_key", Label: "Service provider key", Secret: true,
			Help: "BAM!'s own key for this provider, which signs its requests and opens encrypted assertions. BAM! makes one when the provider is saved; paste a PEM RSA private key to use your own."},
	}
}

func (s *SAML) Check(c Config) error {
	if err := CheckRequired(s, c); err != nil {
		return err
	}
	u, x := c.Settings["metadata_url"], c.Settings["metadata"]
	switch {
	case u == "" && x == "":
		return errors.New("Give the metadata URL or paste the metadata XML")
	case u != "" && x != "":
		return errors.New("Give the metadata URL or paste the metadata XML, not both")
	case u != "":
		if err := checkEndpoint(u); err != nil {
			return fmt.Errorf("Metadata URL %s", err)
		}
	default:
		if _, err := parseSAMLMetadata([]byte(x), c.Settings["entity_id"]); err != nil {
			return fmt.Errorf("Metadata XML: %s", err)
		}
	}
	if f := c.Settings["nameid_format"]; f != "" && f != "persistent" && f != "email" {
		return errors.New("NameID format must be persistent, email or blank")
	}
	if v := c.Settings["idp_initiated"]; v != "" && v != "on" && v != "off" {
		return errors.New("Sign-ins started at the provider must be on, off or blank")
	}
	if k := c.Secrets["sp_key"]; k != "" {
		if _, err := parseSAMLKey(k); err != nil {
			return fmt.Errorf("Service provider key: %s", err)
		}
	}
	return nil
}

// Prepare makes the provider's key, when it has none or a new one is
// asked for.
func (*SAML) Prepare(c *Config, renew bool) (bool, error) {
	if c.Secrets["sp_key"] != "" && !renew {
		return false, nil
	}
	key, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		return false, err
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return false, err
	}
	if c.Secrets == nil {
		c.Secrets = map[string]string{}
	}
	c.Secrets["sp_key"] = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	return true, nil
}

func (*SAML) AcceptsUnsolicited(c Config) bool { return c.Settings["idp_initiated"] == "on" }

// SAMLEntityID is BAM!'s entity ID for a provider: where its metadata is.
func SAMLEntityID(c Config) string {
	return strings.TrimRight(c.BaseURL, "/") + "/auth/saml/" + url.PathEscape(c.Key) + "/metadata"
}

func nameIDFormat(c Config) string {
	if c.Settings["nameid_format"] == "email" {
		return nameIDEmail
	}
	return nameIDPersist
}

// Publish answers BAM!'s service provider metadata for the provider.
func (*SAML) Publish(c Config) ([]byte, string, error) {
	doc := etree.NewDocument()
	ed := doc.CreateElement("md:EntityDescriptor")
	ed.CreateAttr("xmlns:md", samlMetadataNS)
	ed.CreateAttr("entityID", SAMLEntityID(c))
	sp := ed.CreateElement("md:SPSSODescriptor")
	key, err := samlKey(c)
	if err != nil {
		return nil, "", err
	}
	sp.CreateAttr("AuthnRequestsSigned", fmt.Sprint(key != nil))
	sp.CreateAttr("WantAssertionsSigned", "true")
	sp.CreateAttr("protocolSupportEnumeration", samlProtocolNS)
	if key != nil {
		cert, err := samlCertificate(c, key)
		if err != nil {
			return nil, "", err
		}
		for _, use := range []string{"signing", "encryption"} {
			kd := sp.CreateElement("md:KeyDescriptor")
			kd.CreateAttr("use", use)
			ki := kd.CreateElement("ds:KeyInfo")
			ki.CreateAttr("xmlns:ds", dsig.Namespace)
			ki.CreateElement("ds:X509Data").CreateElement("ds:X509Certificate").SetText(base64.StdEncoding.EncodeToString(cert.Raw))
		}
	}
	sp.CreateElement("md:NameIDFormat").SetText(nameIDFormat(c))
	acs := sp.CreateElement("md:AssertionConsumerService")
	acs.CreateAttr("Binding", samlPOST)
	acs.CreateAttr("Location", c.CallbackURL)
	acs.CreateAttr("index", "0")
	acs.CreateAttr("isDefault", "true")
	doc.Indent(2)
	out, err := doc.WriteToBytes()
	return out, samlMetadataType, err
}

func (s *SAML) Begin(ctx context.Context, c Config) (string, map[string]string, error) {
	md, err := s.idpMetadata(ctx, c)
	if err != nil {
		return "", nil, err
	}
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	id := "_" + hex.EncodeToString(raw)

	doc := etree.NewDocument()
	req := doc.CreateElement("samlp:AuthnRequest")
	req.CreateAttr("xmlns:samlp", samlProtocolNS)
	req.CreateAttr("xmlns:saml", samlAssertionNS)
	req.CreateAttr("ID", id)
	req.CreateAttr("Version", "2.0")
	req.CreateAttr("IssueInstant", s.now().UTC().Format(time.RFC3339))
	req.CreateAttr("Destination", md.SSOURL)
	req.CreateAttr("AssertionConsumerServiceURL", c.CallbackURL)
	req.CreateAttr("ProtocolBinding", samlPOST)
	req.CreateElement("saml:Issuer").SetText(SAMLEntityID(c))
	policy := req.CreateElement("samlp:NameIDPolicy")
	policy.CreateAttr("Format", nameIDFormat(c))
	policy.CreateAttr("AllowCreate", "true")
	// Asking for a level of sign-in, such as one with a second factor
	// (docs/access.md#requiring-two-factor-sign-in).
	if classes := strings.Fields(c.Settings["authn_context"]); len(classes) > 0 {
		rac := req.CreateElement("samlp:RequestedAuthnContext")
		rac.CreateAttr("Comparison", "exact")
		for _, class := range classes {
			rac.CreateElement("saml:AuthnContextClassRef").SetText(class)
		}
	}
	xmlBytes, err := doc.WriteToBytes()
	if err != nil {
		return "", nil, err
	}
	var deflated bytes.Buffer
	w, _ := flate.NewWriter(&deflated, flate.BestCompression)
	_, _ = w.Write(xmlBytes)
	if err := w.Close(); err != nil {
		return "", nil, err
	}

	// The HTTP-Redirect binding signs the query itself, as written.
	query := "SAMLRequest=" + url.QueryEscape(base64.StdEncoding.EncodeToString(deflated.Bytes())) +
		"&RelayState=" + url.QueryEscape(c.State)
	key, err := samlKey(c)
	if err != nil {
		return "", nil, err
	}
	if key != nil {
		query += "&SigAlg=" + url.QueryEscape(rsaSHA256)
		sum := sha256.Sum256([]byte(query))
		sig, err := rsa.SignPKCS1v15(nil, key, crypto.SHA256, sum[:])
		if err != nil {
			return "", nil, err
		}
		query += "&Signature=" + url.QueryEscape(base64.StdEncoding.EncodeToString(sig))
	}
	sep := "?"
	if strings.Contains(md.SSOURL, "?") {
		sep = "&"
	}
	return md.SSOURL + sep + query, map[string]string{"state": c.State, "request_id": id}, nil
}

func (s *SAML) Finish(ctx context.Context, c Config, r *http.Request, state map[string]string) (*Claims, error) {
	if err := r.ParseForm(); err != nil {
		return nil, fmt.Errorf("reading the answer: %w", err)
	}
	requestID := state["request_id"]
	if requestID == "" {
		if !s.AcceptsUnsolicited(c) {
			return nil, errors.New("a sign-in started at the provider, which this provider does not take")
		}
	} else if subtle.ConstantTimeCompare([]byte(r.PostForm.Get("RelayState")), []byte(state["state"])) != 1 {
		return nil, errors.New("the answer is not for this sign-in")
	}
	encoded := r.PostForm.Get("SAMLResponse")
	if encoded == "" {
		return nil, errors.New("the answer has no SAMLResponse")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(encoded), ""))
	if err != nil {
		return nil, errors.New("the SAMLResponse is not base64")
	}
	md, err := s.idpMetadata(ctx, c)
	if err != nil {
		return nil, err
	}
	key, err := samlKey(c)
	if err != nil {
		return nil, err
	}
	assertion, err := s.verifiedAssertion(c, md, key, raw, requestID)
	if err != nil {
		return nil, err
	}
	return s.claimsOf(c, md, assertion, requestID)
}

// verifiedAssertion reads the one assertion in a response, from the
// element whose signature was checked: the response's, holding it, or
// its own. Anything outside that element is never read, against
// signature wrapping.
func (s *SAML) verifiedAssertion(c Config, md *samlIDP, key *rsa.PrivateKey, raw []byte, requestID string) (*etree.Element, error) {
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(raw); err != nil {
		return nil, fmt.Errorf("the SAMLResponse is not XML: %w", err)
	}
	for _, t := range doc.Child {
		if _, ok := t.(*etree.Directive); ok {
			return nil, errors.New("the SAMLResponse has a DOCTYPE")
		}
	}
	root := doc.Root()
	if root == nil || !isSAML(root, samlProtocolNS, "Response") {
		return nil, errors.New("the SAMLResponse is not a Response")
	}
	validator := dsig.NewDefaultValidationContext(&dsig.MemoryX509CertificateStore{Roots: md.Certs})
	validator.Clock = s.clock()

	response := root
	signed := hasSignature(root)
	if signed {
		v, err := validator.Validate(root)
		if err != nil {
			return nil, fmt.Errorf("the response's signature does not check: %w", err)
		}
		response = v
	}

	// A failed sign-in, such as one cancelled. Its status may be unsigned,
	// but can only refuse.
	if code := samlStatus(response); code != samlSuccess {
		return nil, fmt.Errorf("%w: %s", ErrRefused, code)
	}
	if d := response.SelectAttrValue("Destination", ""); d != "" && d != c.CallbackURL {
		return nil, errors.New("the response is for somewhere else")
	}
	if irt := response.SelectAttrValue("InResponseTo", ""); irt != requestID {
		return nil, errors.New("the response is not for this sign-in")
	}

	var found []*etree.Element
	for _, el := range response.ChildElements() {
		if isSAML(el, samlAssertionNS, "Assertion") || isSAML(el, samlAssertionNS, "EncryptedAssertion") {
			found = append(found, el)
		}
	}
	if len(found) != 1 {
		return nil, fmt.Errorf("the response has %d assertions, not one", len(found))
	}
	assertion, err := detach(found[0])
	if err != nil {
		return nil, err
	}
	if isSAML(assertion, samlAssertionNS, "EncryptedAssertion") {
		if key == nil {
			return nil, errors.New("the assertion is encrypted, and this provider has no key to open it")
		}
		if assertion, err = decryptAssertion(assertion, key); err != nil {
			return nil, fmt.Errorf("opening the encrypted assertion: %w", err)
		}
	}
	if hasSignature(assertion) {
		v, err := validator.Validate(assertion)
		if err != nil {
			return nil, fmt.Errorf("the assertion's signature does not check: %w", err)
		}
		assertion = v
	} else if !signed {
		return nil, errors.New("neither the response nor its assertion is signed")
	}
	return assertion, nil
}

// claimsOf checks a verified assertion's conditions, and reads who it says
// signed in.
func (s *SAML) claimsOf(c Config, md *samlIDP, a *etree.Element, requestID string) (*Claims, error) {
	now := s.now()
	if issuer := text(child(a, samlAssertionNS, "Issuer")); issuer != md.EntityID {
		return nil, fmt.Errorf("the assertion is from %q, not the identity provider", issuer)
	}
	id := a.SelectAttrValue("ID", "")
	if id == "" {
		return nil, errors.New("the assertion has no ID")
	}
	until := now.Add(time.Hour)

	// Conditions: when, and for whom.
	if cond := child(a, samlAssertionNS, "Conditions"); cond != nil {
		if t, ok := samlTime(cond, "NotBefore"); ok && now.Add(samlSkew).Before(t) {
			return nil, errors.New("the assertion is not valid yet")
		}
		if t, ok := samlTime(cond, "NotOnOrAfter"); ok {
			if !now.Add(-samlSkew).Before(t) {
				return nil, errors.New("the assertion has expired")
			}
			until = t.Add(samlSkew)
		}
		entityID := SAMLEntityID(c)
		for _, ar := range children(cond, samlAssertionNS, "AudienceRestriction") {
			ok := false
			for _, aud := range children(ar, samlAssertionNS, "Audience") {
				ok = ok || text(aud) == entityID
			}
			if !ok {
				return nil, errors.New("the assertion is for another audience")
			}
		}
	} else {
		return nil, errors.New("the assertion has no conditions")
	}

	subject := child(a, samlAssertionNS, "Subject")
	if subject == nil {
		return nil, errors.New("the assertion has no subject")
	}
	// A bearer confirmation for here, for this sign-in, not yet expired.
	confirmed := false
	for _, sc := range children(subject, samlAssertionNS, "SubjectConfirmation") {
		data := child(sc, samlAssertionNS, "SubjectConfirmationData")
		if sc.SelectAttrValue("Method", "") != samlBearer || data == nil {
			continue
		}
		t, ok := samlTime(data, "NotOnOrAfter")
		if !ok || !now.Add(-samlSkew).Before(t) || data.SelectAttrValue("Recipient", "") != c.CallbackURL ||
			data.SelectAttrValue("InResponseTo", "") != requestID {
			continue
		}
		confirmed = true
		if t.Add(samlSkew).After(until) {
			until = t.Add(samlSkew)
		}
	}
	if !confirmed {
		return nil, errors.New("the assertion has no bearer confirmation for this sign-in")
	}

	nameID := child(subject, samlAssertionNS, "NameID")
	if nameID == nil || strings.TrimSpace(nameID.Text()) == "" {
		return nil, errors.New("the assertion has no NameID")
	}
	format, value := nameID.SelectAttrValue("Format", ""), strings.TrimSpace(nameID.Text())
	claims := &Claims{Values: map[string][]string{}}
	switch {
	case c.Settings["nameid_format"] == "email":
		claims.Subject = strings.ToLower(value)
	case format == nameIDTransient:
		return nil, errors.New("the NameID is transient, so says nothing of who signed in: set the provider to send a persistent one")
	case format == nameIDEmail:
		return nil, errors.New("the NameID is an email: set NameID format to email, or the provider to send a persistent one")
	default:
		claims.Subject = value
	}

	if c.UseOnce != nil {
		if err := c.UseOnce(id, until); err != nil {
			return nil, err
		}
	}

	if as := child(a, samlAssertionNS, "AttributeStatement"); as != nil {
		for _, attr := range children(as, samlAssertionNS, "Attribute") {
			var values []string
			for _, v := range children(attr, samlAssertionNS, "AttributeValue") {
				if t := strings.TrimSpace(v.Text()); t != "" {
					values = append(values, t)
				}
			}
			for _, name := range []string{attr.SelectAttrValue("Name", ""), attr.SelectAttrValue("FriendlyName", "")} {
				if name != "" {
					claims.Values[name] = append(claims.Values[name], values...)
				}
			}
		}
	}
	first := func(setting string, fallback []string) []string {
		names := fallback
		if v := c.Settings[setting]; v != "" {
			names = strings.Split(v, ",")
		}
		for _, n := range names {
			if v := claims.Values[strings.TrimSpace(n)]; len(v) > 0 {
				return v
			}
		}
		return nil
	}
	if v := first("email_attribute", samlEmailAttributes); len(v) > 0 {
		claims.Email = v[0]
	} else if c.Settings["nameid_format"] == "email" {
		claims.Email = value
	}
	if v := first("name_attribute", samlNameAttributes); len(v) > 0 {
		claims.Name = v[0]
	}
	// Rules and the groups allowed read "groups", whichever attribute has
	// them.
	claims.Values[defaultGroupsClaim] = first("groups_attribute", samlGroupsAttributes)
	// How they signed in, kept for #192, as OIDC's acr is.
	if as := child(a, samlAssertionNS, "AuthnStatement"); as != nil {
		claims.ACR = strings.TrimSpace(text(child(child(as, samlAssertionNS, "AuthnContext"), samlAssertionNS, "AuthnContextClassRef")))
	}
	return claims, nil
}

// samlStatus is a response's top-level status code, with the second level
// after a slash when there is one.
func samlStatus(response *etree.Element) string {
	code := child(child(response, samlProtocolNS, "Status"), samlProtocolNS, "StatusCode")
	if code == nil {
		return ""
	}
	out := code.SelectAttrValue("Value", "")
	if sub := child(code, samlProtocolNS, "StatusCode"); sub != nil {
		out += " / " + sub.SelectAttrValue("Value", "")
	}
	return out
}

// isSAML reports whether an element is the tag in the namespace.
func isSAML(el *etree.Element, ns, tag string) bool {
	return el != nil && el.Tag == tag && el.NamespaceURI() == ns
}

func children(el *etree.Element, ns, tag string) []*etree.Element {
	if el == nil {
		return nil
	}
	var out []*etree.Element
	for _, c := range el.ChildElements() {
		if isSAML(c, ns, tag) {
			out = append(out, c)
		}
	}
	return out
}

func child(el *etree.Element, ns, tag string) *etree.Element {
	if c := children(el, ns, tag); len(c) > 0 {
		return c[0]
	}
	return nil
}

func text(el *etree.Element) string {
	if el == nil {
		return ""
	}
	return strings.TrimSpace(el.Text())
}

// hasSignature reports whether an element is signed: whether it has a
// Signature of its own, as a child.
func hasSignature(el *etree.Element) bool {
	return child(el, dsig.Namespace, dsig.SignatureTag) != nil
}

// detach copies an element out of its document, declaring the namespaces
// it was given by those around it.
func detach(el *etree.Element) (*etree.Element, error) {
	ctx, err := etreeutils.NSBuildParentContext(el)
	if err != nil {
		return nil, err
	}
	return etreeutils.NSDetatch(ctx, el)
}

func samlTime(el *etree.Element, attr string) (time.Time, bool) {
	v := el.SelectAttrValue(attr, "")
	if v == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, v)
	return t, err == nil
}

func (s *SAML) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// clock is the clock certificates are checked against: the real one, or
// the tests'.
func (s *SAML) clock() *dsig.Clock {
	if s.Now == nil {
		return nil
	}
	return dsig.NewFakeClockAt(s.Now())
}

func (s *SAML) client() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return &http.Client{Timeout: 10 * time.Second}
}

// idpMetadata is the provider's metadata: pasted, or read from its URL,
// at most daily. When reading it again fails, the copy kept is used.
func (s *SAML) idpMetadata(ctx context.Context, c Config) (*samlIDP, error) {
	if x := c.Settings["metadata"]; x != "" {
		return parseSAMLMetadata([]byte(x), c.Settings["entity_id"])
	}
	u := c.Settings["metadata_url"]
	cacheKey := u + " " + c.Settings["entity_id"]
	s.mu.Lock()
	kept, ok := s.metadata[cacheKey]
	s.mu.Unlock()
	if ok && s.now().Sub(kept.at) < samlMetadataAge {
		return kept.md, nil
	}
	md, err := s.fetchMetadata(ctx, u, c.Settings["entity_id"])
	if err != nil {
		if ok {
			return kept.md, nil
		}
		return nil, err
	}
	s.mu.Lock()
	if s.metadata == nil {
		s.metadata = map[string]samlMetadataCache{}
	}
	s.metadata[cacheKey] = samlMetadataCache{md: md, at: s.now()}
	s.mu.Unlock()
	return md, nil
}

func (s *SAML) fetchMetadata(ctx context.Context, u, entityID string) (*samlIDP, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	res, err := s.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("reading the metadata: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("reading the metadata: %s", res.Status)
	}
	// Federations' metadata can be large.
	body, err := io.ReadAll(io.LimitReader(res.Body, 64<<20))
	if err != nil {
		return nil, fmt.Errorf("reading the metadata: %w", err)
	}
	return parseSAMLMetadata(body, entityID)
}

// mdDescriptor is an EntityDescriptor or EntitiesDescriptor, which holds
// more of either.
type mdDescriptor struct {
	EntityID string         `xml:"entityID,attr"`
	IDPs     []mdIDP        `xml:"urn:oasis:names:tc:SAML:2.0:metadata IDPSSODescriptor"`
	Entities []mdDescriptor `xml:"urn:oasis:names:tc:SAML:2.0:metadata EntitiesDescriptor"`
	Entity   []mdDescriptor `xml:"urn:oasis:names:tc:SAML:2.0:metadata EntityDescriptor"`
}

type mdIDP struct {
	Keys []struct {
		Use   string   `xml:"use,attr"`
		Certs []string `xml:"http://www.w3.org/2000/09/xmldsig# KeyInfo>X509Data>X509Certificate"`
	} `xml:"urn:oasis:names:tc:SAML:2.0:metadata KeyDescriptor"`
	SSO []struct {
		Binding  string `xml:"Binding,attr"`
		Location string `xml:"Location,attr"`
	} `xml:"urn:oasis:names:tc:SAML:2.0:metadata SingleSignOnService"`
}

// parseSAMLMetadata reads the identity provider from metadata: the only
// one there, or the one with entityID.
func parseSAMLMetadata(data []byte, entityID string) (*samlIDP, error) {
	var root mdDescriptor
	if err := xml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("it is not XML: %w", err)
	}
	var found []*mdDescriptor
	var walk func(d *mdDescriptor)
	walk = func(d *mdDescriptor) {
		if len(d.IDPs) > 0 && (entityID == "" || d.EntityID == entityID) {
			found = append(found, d)
		}
		for i := range d.Entities {
			walk(&d.Entities[i])
		}
		for i := range d.Entity {
			walk(&d.Entity[i])
		}
	}
	walk(&root)
	switch {
	case len(found) == 0 && entityID != "":
		return nil, fmt.Errorf("it has no identity provider %q", entityID)
	case len(found) == 0:
		return nil, errors.New("it has no identity provider")
	case len(found) > 1:
		return nil, fmt.Errorf("it has %d identity providers: give the entity ID of one", len(found))
	}
	d := found[0]
	md := &samlIDP{EntityID: d.EntityID}
	for _, idp := range d.IDPs {
		for _, sso := range idp.SSO {
			if sso.Binding == samlRedirect && md.SSOURL == "" {
				md.SSOURL = sso.Location
			}
		}
		for _, k := range idp.Keys {
			if k.Use != "" && k.Use != "signing" {
				continue
			}
			for _, c := range k.Certs {
				der, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(c), ""))
				if err != nil {
					return nil, errors.New("a signing certificate is not base64")
				}
				cert, err := x509.ParseCertificate(der)
				if err != nil {
					return nil, fmt.Errorf("a signing certificate cannot be read: %w", err)
				}
				md.Certs = append(md.Certs, cert)
			}
		}
	}
	switch {
	case md.EntityID == "":
		return nil, errors.New("the identity provider has no entity ID")
	case md.SSOURL == "":
		return nil, errors.New("the identity provider takes no sign-ins by HTTP-Redirect, which BAM! sends")
	case len(md.Certs) == 0:
		return nil, errors.New("the identity provider has no signing certificate")
	}
	if err := checkEndpoint(md.SSOURL); err != nil {
		return nil, fmt.Errorf("its sign-in URL %s", err)
	}
	return md, nil
}

var pemArmor = regexp.MustCompile(`-----(BEGIN|END) [A-Z ]+-----`)

// parseSAMLKey reads a PEM RSA private key, PKCS #8 or #1. Its line breaks
// may have been lost, pasted into a one-line field.
func parseSAMLKey(s string) (*rsa.PrivateKey, error) {
	body := strings.Join(strings.Fields(pemArmor.ReplaceAllString(s, "")), "")
	der, err := base64.StdEncoding.DecodeString(body)
	if err != nil || !pemArmor.MatchString(s) {
		return nil, errors.New("it is not a PEM private key")
	}
	var key any
	if key, err = x509.ParsePKCS8PrivateKey(der); err != nil {
		if key, err = x509.ParsePKCS1PrivateKey(der); err != nil {
			return nil, errors.New("it is not a PEM private key")
		}
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("it must be an RSA key, which every identity provider takes")
	}
	if rsaKey.N.BitLen() < 2048 {
		return nil, errors.New("it must be at least 2048 bits")
	}
	return rsaKey, nil
}

// samlKey is the provider's own key, or nil for one without.
func samlKey(c Config) (*rsa.PrivateKey, error) {
	if c.Secrets["sp_key"] == "" {
		return nil, nil
	}
	return parseSAMLKey(c.Secrets["sp_key"])
}

// samlCertificate is the certificate published for a key. It is made from
// the key alone, the same each time, so only the key is kept; identity
// providers check the key, not the certificate's dates or issuer.
func samlCertificate(c Config, key *rsa.PrivateKey) (*x509.Certificate, error) {
	pub, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(pub)
	template := &x509.Certificate{
		SerialNumber: new(big.Int).SetBytes(sum[:16]),
		Subject:      pkix.Name{CommonName: "BAM! " + c.Key},
		NotBefore:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:     time.Date(2126, 1, 1, 0, 0, 0, 0, time.UTC),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	return x509.ParseCertificate(der)
}
