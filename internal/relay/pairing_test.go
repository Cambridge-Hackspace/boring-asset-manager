package relay

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCodes(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		code := NewCode()
		if !regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{4}(-[0-9A-HJKMNP-TV-Z]{4}){3}$`).MatchString(code) {
			t.Fatalf("code %q", code)
		}
		if seen[code] {
			t.Fatalf("code %q came twice", code)
		}
		seen[code] = true
	}
	// Typed loosely, it reads the same.
	if NormalizeCode(" ab12-cd34-ef56-gh7o ") != NormalizeCode("AB12CD34EF56GH70") {
		t.Fatal("a code typed in lowercase, with an O for a zero, reads differently")
	}
	if c := NewCredential(); !strings.HasPrefix(c, CredentialPrefix) || len(c) < 40 {
		t.Fatalf("credential %q", c)
	}
}

// pairing makes a relay's pairing as the main node would issue it.
func pairing(t *testing.T, w *world, name string) *Pairing {
	t.Helper()
	cfg, key := w.identity(name, time.Now().Add(-time.Hour), time.Now().Add(time.Hour), w.rootPriv)
	raw, _ := json.Marshal(cfg.Cert)
	return &Pairing{Main: "https://bam.example.org", Issued: Issued{Name: name, Mesh: testMesh, RootPublic: w.rootPublic, Certificate: raw,
		ExpiresAt: time.Now().Add(time.Hour), MainKey: w.mainKey, Credential: NewCredential(), WebSocket: true},
		IdentityPublic: key, IdentityPrivate: cfg.IDPrivate}
}

func TestPairingIsKeptAndChecked(t *testing.T) {
	w := newWorld(t)
	p := pairing(t, w, "front-desk")
	if err := p.check(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "relay", "relay.json")
	if err := p.Save(path); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
		t.Fatalf("kept as %v, %v", info.Mode(), err)
	}
	got, err := LoadPairing(path)
	if err != nil || got.Name != "front-desk" || got.Credential != p.Credential {
		t.Fatalf("read back: %+v %v", got, err)
	}

	// Its trust is the main node's key and its own identity, the
	// certificate's numbers read as BoneMesh checks them.
	tr := got.Trust()
	if key, err := tr.IdentityKey(Main); err != nil || string(key) != string(w.mainKey) {
		t.Fatal("the main node's key is not trusted")
	}
	if _, err := tr.IdentityKey("workshop"); err == nil {
		t.Fatal("another relay's key is trusted")
	}
	cfg, err := tr.Mesh()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CertificateKey(cfg.Cert, cfg.RootPublic, cfg.Mesh, time.Now()); err != nil || cfg.Label != "front-desk" {
		t.Fatalf("the certificate read back does not check: %v", err)
	}

	// A certificate for another key, or another name, is refused.
	other := pairing(t, w, "front-desk")
	bad := *p
	bad.Certificate = other.Certificate
	if err := bad.check(); err == nil {
		t.Fatal("a certificate for another key was taken")
	}
	bad = *p
	bad.Name = "workshop"
	if err := bad.check(); err == nil {
		t.Fatal("a certificate for another name was taken")
	}
}

func TestIdentityKeys(t *testing.T) {
	public, _ := NewIdentity()
	if !ValidIdentityKey(public) || ValidIdentityKey(public[:100]) || ValidIdentityKey(nil) {
		t.Fatal("identity keys are not told apart")
	}
}
