package relay

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"forge.axonibyte.com/axonibyte/bonemesh/go/v3/node"
)

// Pairing (docs/relays.md#pairing): an admin adds a relay on the main node,
// which gives a one-time code. The relay makes its own identity key, and
// sends the public half with the code; the main node answers with a
// BoneMesh certificate for that key and a WebSocket credential. The private
// key never leaves the relay.

// The paths a relay pairs and renews at, on the main node.
const (
	JoinPath  = "/api/v1/relays/join"
	RenewPath = "/api/v1/relays/renew"
)

// CredentialPrefix starts every relay credential, so one is told from an
// API token at a glance.
const CredentialPrefix = "bamr_"

// codeAlphabet is Crockford's base32: no I, L, O or U to misread.
const codeAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// NewCode makes a one-time enrollment code: 80 random bits, as four groups
// of four characters.
func NewCode() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	var out strings.Builder
	for i, v := range b {
		if i > 0 && i%4 == 0 {
			out.WriteByte('-')
		}
		out.WriteByte(codeAlphabet[int(v)%32])
	}
	return out.String()
}

// NormalizeCode reads a code as typed: in either case, with or without
// its hyphens and spaces, and with O, I and L read as 0, 1 and 1.
func NormalizeCode(code string) string {
	r := strings.NewReplacer("-", "", " ", "", "O", "0", "I", "1", "L", "1")
	return r.Replace(strings.ToUpper(strings.TrimSpace(code)))
}

// NewCredential makes a relay's WebSocket credential.
func NewCredential() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return CredentialPrefix + base64.RawURLEncoding.EncodeToString(b)
}

// JoinRequest is what a relay sends to pair: the code, and the public half
// of its identity key.
type JoinRequest struct {
	Code        string `json:"code"`
	IdentityKey []byte `json:"identity_key"`
	// Version is the relay's BAM! version, and Protocol the relay
	// protocol's version it speaks.
	Version  string `json:"version"`
	Protocol int    `json:"protocol"`
}

// Issued is what the main node gives a relay as it pairs or renews.
type Issued struct {
	Name string `json:"name"`
	Mesh string `json:"mesh"`
	// RootPublic is the mesh root's public key, which every node pins.
	RootPublic []byte `json:"root_public"`
	// Certificate is the relay's BoneMesh membership certificate, valid
	// until ExpiresAt.
	Certificate json.RawMessage `json:"certificate"`
	ExpiresAt   time.Time       `json:"expires_at"`
	// MainKey is the main node's identity key, which signs what it sends
	// over BoneMesh.
	MainKey []byte `json:"main_key"`
	// Credential is the relay's WebSocket credential; only joining gives
	// one.
	Credential string `json:"credential,omitempty"`
	// BoneMesh is the main node's BoneMesh address as host:port, when
	// BoneMesh is on there; WebSocket says whether it takes relays over
	// WebSocket.
	BoneMesh  string `json:"bonemesh,omitempty"`
	WebSocket bool   `json:"websocket"`
}

// RenewRequest is what a relay sends to renew its certificate.
type RenewRequest struct {
	Version  string `json:"version"`
	Protocol int    `json:"protocol"`
}

// Pairing is what a relay keeps of its pairing: everything it needs to
// link to the main node, its private key among it. It is kept in a file
// only its owner reads.
type Pairing struct {
	// Main is the main node's address, such as https://bam.example.org.
	Main string `json:"main"`
	Issued
	// IdentityPublic and IdentityPrivate are the relay's ML-DSA-65 key
	// pair; the private half never leaves the relay.
	IdentityPublic  []byte    `json:"identity_public"`
	IdentityPrivate []byte    `json:"identity_private"`
	PairedAt        time.Time `json:"paired_at"`
}

// Join pairs this node as a relay of the main node at mainURL, with the
// code an admin was given, making its identity key here.
func Join(ctx context.Context, client *http.Client, mainURL, code, version string) (*Pairing, error) {
	base, err := mainBase(mainURL)
	if err != nil {
		return nil, err
	}
	public, private := NewIdentity()
	var issued Issued
	req := JoinRequest{Code: NormalizeCode(code), IdentityKey: public, Version: version, Protocol: Version}
	if err := post(ctx, client, base+JoinPath, "", req, &issued); err != nil {
		return nil, err
	}
	p := &Pairing{Main: base, Issued: issued, IdentityPublic: public, IdentityPrivate: private, PairedAt: time.Now().UTC()}
	if err := p.check(); err != nil {
		return nil, err
	}
	return p, nil
}

// Renew asks the main node for a new certificate, with the relay's
// credential, before the one it has expires. A revoked relay is refused.
func (p *Pairing) Renew(ctx context.Context, client *http.Client, version string) error {
	var issued Issued
	if err := post(ctx, client, p.Main+RenewPath, p.Credential, RenewRequest{Version: version, Protocol: Version}, &issued); err != nil {
		return err
	}
	issued.Credential = p.Credential
	next := *p
	next.Issued = issued
	if err := next.check(); err != nil {
		return err
	}
	*p = next
	return nil
}

// check refuses a certificate that is not for this relay's key and name,
// or not from the mesh root the main node named.
func (p *Pairing) check() error {
	c, err := certificate(p.Certificate)
	if err != nil {
		return err
	}
	key, err := CertificateKey(c, p.RootPublic, p.Mesh, time.Now())
	if err != nil {
		return fmt.Errorf("the main node's certificate does not check: %w", err)
	}
	if !bytes.Equal(key, p.IdentityPublic) {
		return errors.New("the main node's certificate is for another key")
	}
	if label, _ := c["label"].(string); label != p.Name || !ValidRelayName(p.Name) {
		return fmt.Errorf("the main node's certificate is for %q, not %q", label, p.Name)
	}
	return nil
}

// certificate reads a certificate, its numbers as BoneMesh checks them.
func certificate(raw json.RawMessage) (map[string]any, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var c map[string]any
	if err := d.Decode(&c); err != nil || c == nil {
		return nil, errors.New("the certificate is not a BoneMesh certificate")
	}
	return c, nil
}

// Trust is the relay's trust, from its pairing: the main node's key, and
// its own BoneMesh identity.
func (p *Pairing) Trust() Trust { return pairedTrust{p} }

type pairedTrust struct{ p *Pairing }

// Credential refuses every credential: a relay takes no connections.
func (pairedTrust) Credential(string) (string, error) { return "", ErrUntrusted }

// IdentityKey knows the main node's key alone.
func (t pairedTrust) IdentityKey(name string) ([]byte, error) {
	if name != Main {
		return nil, ErrUntrusted
	}
	return t.p.MainKey, nil
}

// Mesh is the relay's identity, with its certificate.
func (t pairedTrust) Mesh() (node.Config, error) {
	c, err := certificate(t.p.Certificate)
	if err != nil {
		return node.Config{}, err
	}
	return node.Config{Label: t.p.Name, Mesh: t.p.Mesh, RootPublic: t.p.RootPublic, Cert: c, IDPrivate: t.p.IdentityPrivate}, nil
}

// Save writes the pairing to path, readable by its owner alone, replacing
// any there whole.
func (p *Pairing) Save(path string) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// LoadPairing reads a pairing Save wrote.
func LoadPairing(path string) (*Pairing, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p Pairing
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("%s is not a relay's pairing: %w", path, err)
	}
	return &p, nil
}

// mainBase is the main node's address, without a trailing slash.
func mainBase(mainURL string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(mainURL))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", fmt.Errorf("%q is not the main node's address, such as https://bam.example.org", mainURL)
	}
	u.RawQuery, u.Fragment = "", ""
	return strings.TrimSuffix(u.String(), "/"), nil
}

// post sends a JSON request, with a relay's credential if it has one, and
// reads the JSON answer, or the API's error.
func post(ctx context.Context, client *http.Client, u, credential string, body, out any) error {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if credential != "" {
		req.Header.Set("Authorization", CredentialScheme+" "+credential)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	answer, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(answer, &e) == nil && e.Error != "" {
			return fmt.Errorf("the main node refused (%d): %s", resp.StatusCode, e.Error)
		}
		return fmt.Errorf("the main node refused (%d)", resp.StatusCode)
	}
	return json.Unmarshal(answer, out)
}
