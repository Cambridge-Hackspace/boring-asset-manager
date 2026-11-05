package idp

import (
	"bytes"
	"compress/flate"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"math/big"
	"net/url"
	"time"

	"github.com/beevik/etree"
	dsig "github.com/russellhaering/goxmldsig"
)

// TestSAMLProvider is a SAML identity provider for tests: it publishes
// metadata, reads BAM!'s requests, and signs the answers it is told to
// give, or bad ones. Like the test OIDC provider, it lets anyone sign in
// as anyone.
type TestSAMLProvider struct {
	EntityID string
	SSOURL   string

	key  *rsa.PrivateKey
	cert *x509.Certificate
}

// TestSAMLRequest is a request BAM! sent, as the provider reads it.
type TestSAMLRequest struct {
	ID, ACSURL, Issuer, NameIDFormat, RelayState string
	// AuthnContext are the classes asked for.
	AuthnContext []string
	// Query is the request's query, to check its signature.
	Query url.Values
	// Signed is the part of the query that was signed.
	Signed string
}

// TestSAMLAnswer is what the provider says, and how. The zero value of
// each field is a good answer to the request.
type TestSAMLAnswer struct {
	NameID       string
	NameIDFormat string
	Attributes   map[string][]string
	ACR          string

	// For bad answers: another audience, recipient, request, issuer or
	// assertion ID, and when it expires, by default five minutes on.
	Audience, Recipient, InResponseTo, Issuer, AssertionID string
	NotOnOrAfter                                           time.Time
	// Status, when set, is a failure's status code.
	Status string

	// SignResponse and SignAssertion sign each; neither leaves it
	// unsigned. SignWith signs with another key, sent with a certificate
	// of its own.
	SignResponse, SignAssertion bool
	SignWith                    *rsa.PrivateKey
	// EncryptTo encrypts the assertion to BAM!'s certificate, with AES-GCM
	// or, with CBC, AES-CBC.
	EncryptTo *x509.Certificate
	CBC       bool
}

// NewTestSAMLProvider makes one with a signing key of its own.
func NewTestSAMLProvider(entityID, ssoURL string) *TestSAMLProvider {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return &TestSAMLProvider{EntityID: entityID, SSOURL: ssoURL, key: key, cert: testCertificate(key)}
}

func testCertificate(key *rsa.PrivateKey) *x509.Certificate {
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test SAML provider"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}
	cert, _ := x509.ParseCertificate(der)
	return cert
}

// Metadata is its metadata, as published.
func (p *TestSAMLProvider) Metadata() string {
	doc := etree.NewDocument()
	ed := doc.CreateElement("md:EntityDescriptor")
	ed.CreateAttr("xmlns:md", samlMetadataNS)
	ed.CreateAttr("entityID", p.EntityID)
	idp := ed.CreateElement("md:IDPSSODescriptor")
	idp.CreateAttr("protocolSupportEnumeration", samlProtocolNS)
	kd := idp.CreateElement("md:KeyDescriptor")
	kd.CreateAttr("use", "signing")
	ki := kd.CreateElement("ds:KeyInfo")
	ki.CreateAttr("xmlns:ds", dsig.Namespace)
	ki.CreateElement("ds:X509Data").CreateElement("ds:X509Certificate").SetText(base64.StdEncoding.EncodeToString(p.cert.Raw))
	for _, b := range []string{samlPOST, samlRedirect} {
		sso := idp.CreateElement("md:SingleSignOnService")
		sso.CreateAttr("Binding", b)
		sso.CreateAttr("Location", p.SSOURL)
	}
	out, _ := doc.WriteToString()
	return out
}

// ReadRequest reads the request BAM! sent the browser to the provider
// with.
func (p *TestSAMLProvider) ReadRequest(redirect string) (*TestSAMLRequest, error) {
	u, err := url.Parse(redirect)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	raw, err := base64.StdEncoding.DecodeString(q.Get("SAMLRequest"))
	if err != nil {
		return nil, err
	}
	xmlBytes, err := io.ReadAll(flate.NewReader(bytes.NewReader(raw)))
	if err != nil {
		return nil, err
	}
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(xmlBytes); err != nil {
		return nil, err
	}
	req := doc.Root()
	if !isSAML(req, samlProtocolNS, "AuthnRequest") {
		return nil, errors.New("not an AuthnRequest")
	}
	signed := ""
	if q.Get("Signature") != "" {
		// What was signed, as it was written.
		signed = "SAMLRequest=" + url.QueryEscape(q.Get("SAMLRequest")) + "&RelayState=" + url.QueryEscape(q.Get("RelayState")) +
			"&SigAlg=" + url.QueryEscape(q.Get("SigAlg"))
	}
	var classes []string
	for _, c := range children(child(req, samlProtocolNS, "RequestedAuthnContext"), samlAssertionNS, "AuthnContextClassRef") {
		classes = append(classes, text(c))
	}
	return &TestSAMLRequest{
		AuthnContext: classes,
		ID:           req.SelectAttrValue("ID", ""), ACSURL: req.SelectAttrValue("AssertionConsumerServiceURL", ""),
		Issuer:       text(child(req, samlAssertionNS, "Issuer")),
		NameIDFormat: child(req, samlProtocolNS, "NameIDPolicy").SelectAttrValue("Format", ""),
		RelayState:   q.Get("RelayState"), Query: q, Signed: signed,
	}, nil
}

// Answer is the SAMLResponse it gives to a request, as the browser posts
// it, base64. With a nil request, it is one started at the provider, for
// an ACS URL and audience given in the answer.
func (p *TestSAMLProvider) Answer(req *TestSAMLRequest, a TestSAMLAnswer) (string, error) {
	resp, err := p.AnswerXML(req, a)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString([]byte(resp)), nil
}

// AnswerXML is the answer as XML, for tests that change it.
func (p *TestSAMLProvider) AnswerXML(req *TestSAMLRequest, a TestSAMLAnswer) (string, error) {
	now := time.Now().UTC()
	or := func(v, fallback string) string {
		if v != "" {
			return v
		}
		return fallback
	}
	inResponseTo, recipient, audience := "", a.Recipient, a.Audience
	if req != nil {
		inResponseTo, recipient, audience = req.ID, or(a.Recipient, req.ACSURL), or(a.Audience, req.Issuer)
	}
	inResponseTo = or(a.InResponseTo, inResponseTo)
	expires := a.NotOnOrAfter
	if expires.IsZero() {
		expires = now.Add(5 * time.Minute)
	}
	stamp := func(t time.Time) string { return t.UTC().Format(time.RFC3339) }

	assertionDoc := etree.NewDocument()
	as := assertionDoc.CreateElement("saml:Assertion")
	as.CreateAttr("xmlns:saml", samlAssertionNS)
	as.CreateAttr("ID", or(a.AssertionID, testID()))
	as.CreateAttr("Version", "2.0")
	as.CreateAttr("IssueInstant", stamp(now))
	as.CreateElement("saml:Issuer").SetText(or(a.Issuer, p.EntityID))
	subject := as.CreateElement("saml:Subject")
	nameID := subject.CreateElement("saml:NameID")
	nameID.CreateAttr("Format", or(a.NameIDFormat, nameIDPersist))
	nameID.SetText(a.NameID)
	sc := subject.CreateElement("saml:SubjectConfirmation")
	sc.CreateAttr("Method", samlBearer)
	scd := sc.CreateElement("saml:SubjectConfirmationData")
	if inResponseTo != "" {
		scd.CreateAttr("InResponseTo", inResponseTo)
	}
	scd.CreateAttr("Recipient", recipient)
	scd.CreateAttr("NotOnOrAfter", stamp(expires))
	cond := as.CreateElement("saml:Conditions")
	cond.CreateAttr("NotBefore", stamp(now.Add(-time.Minute)))
	cond.CreateAttr("NotOnOrAfter", stamp(expires))
	cond.CreateElement("saml:AudienceRestriction").CreateElement("saml:Audience").SetText(audience)
	authn := as.CreateElement("saml:AuthnStatement")
	authn.CreateAttr("AuthnInstant", stamp(now))
	authn.CreateElement("saml:AuthnContext").CreateElement("saml:AuthnContextClassRef").
		SetText(or(a.ACR, "urn:oasis:names:tc:SAML:2.0:ac:classes:PasswordProtectedTransport"))
	if len(a.Attributes) > 0 {
		st := as.CreateElement("saml:AttributeStatement")
		for name, values := range a.Attributes {
			attr := st.CreateElement("saml:Attribute")
			attr.CreateAttr("Name", name)
			for _, v := range values {
				attr.CreateElement("saml:AttributeValue").SetText(v)
			}
		}
	}
	if a.SignAssertion {
		signed, err := p.sign(as, a.SignWith)
		if err != nil {
			return "", err
		}
		as = signed
	}

	doc := etree.NewDocument()
	resp := doc.CreateElement("samlp:Response")
	resp.CreateAttr("xmlns:samlp", samlProtocolNS)
	resp.CreateAttr("xmlns:saml", samlAssertionNS)
	resp.CreateAttr("ID", testID())
	resp.CreateAttr("Version", "2.0")
	resp.CreateAttr("IssueInstant", stamp(now))
	resp.CreateAttr("Destination", recipient)
	if inResponseTo != "" {
		resp.CreateAttr("InResponseTo", inResponseTo)
	}
	resp.CreateElement("saml:Issuer").SetText(p.EntityID)
	resp.CreateElement("samlp:Status").CreateElement("samlp:StatusCode").CreateAttr("Value", or(a.Status, samlSuccess))
	if a.Status == "" {
		if a.EncryptTo != nil {
			enc, err := testEncrypt(as, a.EncryptTo, a.CBC)
			if err != nil {
				return "", err
			}
			resp.AddChild(enc)
		} else {
			resp.AddChild(as)
		}
	}
	if a.SignResponse {
		signed, err := p.sign(resp, a.SignWith)
		if err != nil {
			return "", err
		}
		doc.SetRoot(signed)
	}
	return doc.WriteToString()
}

// sign signs an element, putting its signature after its Issuer, where
// SAML's schema has it.
func (p *TestSAMLProvider) sign(el *etree.Element, with *rsa.PrivateKey) (*etree.Element, error) {
	key, cert := p.key, p.cert
	if with != nil {
		key, cert = with, testCertificate(with)
	}
	ctx, err := dsig.NewSigningContext(key, [][]byte{cert.Raw})
	if err != nil {
		return nil, err
	}
	ctx.Canonicalizer = dsig.MakeC14N10ExclusiveCanonicalizerWithPrefixList("")
	signed, err := ctx.SignEnveloped(el)
	if err != nil {
		return nil, err
	}
	// Read back as written, as the element signed shares parts with the
	// one given, which moving the signature would change.
	doc := etree.NewDocument()
	doc.SetRoot(signed)
	x, err := doc.WriteToString()
	if err != nil {
		return nil, err
	}
	if doc = etree.NewDocument(); doc.ReadFromString(x) != nil {
		return nil, errors.New("reading the signed element back")
	}
	signed = doc.Root()
	sig := signed.ChildElements()[len(signed.ChildElements())-1]
	signed.RemoveChild(sig)
	signed.InsertChildAt(1, sig)
	return signed, nil
}

// testEncrypt seals an assertion to a certificate, as an EncryptedAssertion.
func testEncrypt(as *etree.Element, to *x509.Certificate, cbc bool) (*etree.Element, error) {
	doc := etree.NewDocument()
	doc.SetRoot(as.Copy())
	plain, err := doc.WriteToBytes()
	if err != nil {
		return nil, err
	}
	aesKey := make([]byte, 32)
	_, _ = rand.Read(aesKey)
	block, _ := aes.NewCipher(aesKey)
	var sealed []byte
	method := xmlenc11NS + "aes256-gcm"
	if cbc {
		method = xmlencNS + "aes256-cbc"
		pad := aes.BlockSize - len(plain)%aes.BlockSize
		plain = append(plain, bytes.Repeat([]byte{byte(pad)}, pad)...)
		iv := make([]byte, aes.BlockSize)
		_, _ = rand.Read(iv)
		sealed = append(iv, make([]byte, len(plain))...)
		cipher.NewCBCEncrypter(block, iv).CryptBlocks(sealed[aes.BlockSize:], plain)
	} else {
		aead, _ := cipher.NewGCM(block)
		nonce := make([]byte, aead.NonceSize())
		_, _ = rand.Read(nonce)
		sealed = aead.Seal(nonce, nonce, plain, nil)
	}
	keySealed, err := rsa.EncryptOAEP(sha1.New(), rand.Reader, to.PublicKey.(*rsa.PublicKey), aesKey, nil)
	if err != nil {
		return nil, err
	}

	enc := etree.NewElement("saml:EncryptedAssertion")
	data := enc.CreateElement("xenc:EncryptedData")
	data.CreateAttr("xmlns:xenc", xmlencNS)
	data.CreateAttr("Type", xmlencNS+"Element")
	data.CreateElement("xenc:EncryptionMethod").CreateAttr("Algorithm", method)
	ki := data.CreateElement("ds:KeyInfo")
	ki.CreateAttr("xmlns:ds", dsig.Namespace)
	ek := ki.CreateElement("xenc:EncryptedKey")
	em := ek.CreateElement("xenc:EncryptionMethod")
	em.CreateAttr("Algorithm", xmlencNS+"rsa-oaep-mgf1p")
	em.CreateElement("ds:DigestMethod").CreateAttr("Algorithm", "http://www.w3.org/2000/09/xmldsig#sha1")
	ek.CreateElement("xenc:CipherData").CreateElement("xenc:CipherValue").SetText(base64.StdEncoding.EncodeToString(keySealed))
	data.CreateElement("xenc:CipherData").CreateElement("xenc:CipherValue").SetText(base64.StdEncoding.EncodeToString(sealed))
	return enc, nil
}

func testID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "_" + hex.EncodeToString(b)
}
