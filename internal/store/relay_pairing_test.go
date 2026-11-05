package store

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

func TestRelayCodePairsOnce(t *testing.T) {
	st := setupTestStore(t)
	r, err := st.CreateRelay("front-desk", "code-1", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if r.Paired || r.CodeExpiresAt == nil {
		t.Fatalf("a new relay: %+v", r)
	}
	var taken *RelayNameTakenError
	if _, err := st.CreateRelay("front-desk", "code-2", time.Now().Add(time.Hour)); !errors.As(err, &taken) || taken.Until != nil {
		t.Fatalf("a second relay of the same name: %v", err)
	}

	key := bytes.Repeat([]byte{7}, 32)
	join := RelayJoin{CodeHash: "code-1", IdentityKey: key, CredentialHash: "cred-1", CertExpiresAt: time.Now().Add(7 * 24 * time.Hour), Version: "v1.2.0", Address: "192.0.2.7"}
	r, err = st.JoinRelay(join)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Paired || r.CodeExpiresAt != nil || r.Version != "v1.2.0" || r.Address != "192.0.2.7" || r.PairedAt == nil {
		t.Fatalf("paired: %+v", r)
	}
	// Once.
	if _, err := st.JoinRelay(join); !errors.Is(err, ErrRelayCodeInvalid) {
		t.Fatalf("a used code: %v", err)
	}
	if got, err := st.RelayIdentityKey("front-desk"); err != nil || !bytes.Equal(got, key) {
		t.Fatalf("its key: %v %v", got, err)
	}
	if got, err := st.RelayByCredential("cred-1"); err != nil || got.Name != "front-desk" {
		t.Fatalf("by its credential: %v %v", got, err)
	}

	// And within its hour.
	if _, err := st.CreateRelay("workshop", "code-3", time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	join.CodeHash = "code-3"
	if _, err := st.JoinRelay(join); !errors.Is(err, ErrRelayCodeExpired) {
		t.Fatalf("an expired code: %v", err)
	}
}

func TestRevokedRelaysAreDeniedUntilTheirCertificatesExpire(t *testing.T) {
	st := setupTestStore(t)
	never, _ := st.CreateRelay("never-paired", "code-0", time.Now().Add(time.Hour))
	if _, err := st.RevokeRelay(never.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetRelay(never.ID); !errors.Is(err, ErrRelayNotFound) {
		t.Fatalf("a relay that never paired is removed, not %v", err)
	}

	r, _ := st.CreateRelay("front-desk", "code-1", time.Now().Add(time.Hour))
	expires := time.Now().Add(7 * 24 * time.Hour)
	if _, err := st.JoinRelay(RelayJoin{CodeHash: "code-1", IdentityKey: []byte{1}, CredentialHash: "cred-1", CertExpiresAt: expires}); err != nil {
		t.Fatal(err)
	}
	r, err := st.RevokeRelay(r.ID)
	if err != nil || r.RevokedAt == nil {
		t.Fatalf("revoked: %+v %v", r, err)
	}
	if _, err := st.RelayByCredential("cred-1"); !errors.Is(err, ErrRelayNotFound) {
		t.Fatalf("a revoked credential: %v", err)
	}
	if _, err := st.RelayIdentityKey("front-desk"); !errors.Is(err, ErrRelayNotFound) {
		t.Fatalf("a revoked key: %v", err)
	}
	if _, err := st.SetRelayCode(r.ID, "code-2", time.Now().Add(time.Hour)); !errors.Is(err, ErrRelayRevoked) {
		t.Fatalf("a code for a revoked relay: %v", err)
	}
	// Its name is held until its certificate expires.
	var taken *RelayNameTakenError
	if _, err := st.CreateRelay("front-desk", "code-2", time.Now().Add(time.Hour)); !errors.As(err, &taken) || taken.Until == nil {
		t.Fatalf("the name of a revoked relay: %v", err)
	}
	if _, err := st.Exec("UPDATE relays SET cert_expires_at = ? WHERE id = ?", dbTime(time.Now().Add(-time.Minute)), r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateRelay("front-desk", "code-2", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("once its certificate expired: %v", err)
	}
}

func TestRelayOnlineEvents(t *testing.T) {
	st := setupTestStore(t)
	r, _ := st.CreateRelay("front-desk", "code-1", time.Now().Add(time.Hour))
	// Not paired: nothing to say.
	if changed, err := st.SetRelayOnline("front-desk", true); err != nil || changed {
		t.Fatalf("an unpaired relay came online: %v %v", changed, err)
	}
	if _, err := st.JoinRelay(RelayJoin{CodeHash: "code-1", IdentityKey: []byte{1}, CredentialHash: "cred-1", CertExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	for i, step := range []struct{ online, changed bool }{{true, true}, {true, false}, {false, true}, {false, false}} {
		if changed, err := st.SetRelayOnline("front-desk", step.online); err != nil || changed != step.changed {
			t.Fatalf("step %d: %v %v", i, changed, err)
		}
	}
	events, err := st.EventsSince(time.Now().Add(-time.Hour), 100)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, e := range events {
		if e.Payload.Relay != nil && e.Payload.Relay.ID == r.ID {
			kinds = append(kinds, e.Kind)
		}
	}
	if len(kinds) != 2 || kinds[0] != models.EventRelayOnline || kinds[1] != models.EventRelayOffline {
		t.Fatalf("events: %v", kinds)
	}
	got, _ := st.GetRelay(r.ID)
	if got.Online || got.StatusAt == nil {
		t.Fatalf("after: %+v", got)
	}

	if err := st.SetRelayStatus("front-desk", "v1.3.0", []string{"192.168.1.0/24:9100"}); err != nil {
		t.Fatal(err)
	}
	got, _ = st.GetRelay(r.ID)
	if got.Version != "v1.3.0" || len(got.Allowlist) != 1 {
		t.Fatalf("status: %+v", got)
	}
}

func TestRelayMeshRotation(t *testing.T) {
	st := setupTestStore(t)
	if _, err := st.GetRelayMesh(); !errors.Is(err, ErrNoRelayMesh) {
		t.Fatalf("before any: %v", err)
	}
	first := RelayMesh{Mesh: "bam-1", RootPublic: []byte{1}, RootPrivate: "sealed-root", MainPublic: []byte{2}, MainPrivate: "sealed-main"}
	m, err := st.CreateRelayMesh(first)
	if err != nil || m.Mesh != "bam-1" || !bytes.Equal(m.MainPublic, []byte{2}) {
		t.Fatalf("made: %+v %v", m, err)
	}
	// Another server's root, made at the same time, does not replace it.
	if m, err := st.CreateRelayMesh(RelayMesh{Mesh: "bam-2", RootPublic: []byte{3}, MainPublic: []byte{4}}); err != nil || m.Mesh != "bam-1" {
		t.Fatalf("made twice: %+v %v", m, err)
	}

	paired, _ := st.CreateRelay("front-desk", "code-1", time.Now().Add(time.Hour))
	if _, err := st.JoinRelay(RelayJoin{CodeHash: "code-1", IdentityKey: []byte{1}, CredentialHash: "cred-1", CertExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	revoked, _ := st.CreateRelay("old", "code-2", time.Now().Add(time.Hour))
	if _, err := st.JoinRelay(RelayJoin{CodeHash: "code-2", IdentityKey: []byte{1}, CredentialHash: "cred-2", CertExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RevokeRelay(revoked.ID); err != nil {
		t.Fatal(err)
	}

	if err := st.RotateRelayMesh(RelayMesh{Mesh: "bam-3", RootPublic: []byte{5}, RootPrivate: "r", MainPublic: []byte{6}, MainPrivate: "m"}); err != nil {
		t.Fatal(err)
	}
	if m, _ := st.GetRelayMesh(); m.Mesh != "bam-3" {
		t.Fatalf("after rotating: %+v", m)
	}
	r, err := st.GetRelay(paired.ID)
	if err != nil || r.Paired {
		t.Fatalf("a relay paired under the old root: %+v %v", r, err)
	}
	if _, err := st.RelayByCredential("cred-1"); !errors.Is(err, ErrRelayNotFound) {
		t.Fatalf("its credential: %v", err)
	}
	if _, err := st.GetRelay(revoked.ID); !errors.Is(err, ErrRelayNotFound) {
		t.Fatalf("a revoked relay outlived the old root: %v", err)
	}
}
