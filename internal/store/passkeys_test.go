package store

import (
	"errors"
	"testing"
	"time"
)

func TestAPasskeyChallengeIsAnsweredOnceForItsPurpose(t *testing.T) {
	st := setupTestStore(t)
	ada, _ := st.CreateUser("ada@test.org", "AdaSecret123!", true)
	bo, _ := st.CreateUser("bo@test.org", "BoSecret123!", true)
	challenge := []byte("thirty-two bytes of challenge...")
	if err := st.CreatePasskeyChallenge(ada.ID, PasskeySignIn, challenge); err != nil {
		t.Fatal(err)
	}
	if err := st.TakePasskeyChallenge(bo.ID, PasskeySignIn, challenge); !errors.Is(err, ErrPasskeyChallengeNotFound) {
		t.Errorf("someone else's challenge: %v", err)
	}
	if err := st.TakePasskeyChallenge(ada.ID, PasskeyConfirm, challenge); !errors.Is(err, ErrPasskeyChallengeNotFound) {
		t.Errorf("a challenge for another purpose: %v", err)
	}
	if err := st.TakePasskeyChallenge(ada.ID, PasskeySignIn, challenge); err != nil {
		t.Fatalf("taking it: %v", err)
	}
	if err := st.TakePasskeyChallenge(ada.ID, PasskeySignIn, challenge); !errors.Is(err, ErrPasskeyChallengeNotFound) {
		t.Errorf("taking it again: %v", err)
	}

	if err := st.CreatePasskeyChallenge(ada.ID, PasskeyRegister, challenge); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Exec("UPDATE passkey_challenges SET expires_at = ?", dbTime(time.Now().Add(-time.Second))); err != nil {
		t.Fatal(err)
	}
	if err := st.TakePasskeyChallenge(ada.ID, PasskeyRegister, challenge); !errors.Is(err, ErrPasskeyChallengeNotFound) {
		t.Errorf("an expired challenge: %v", err)
	}
}

func TestPasskeysAreFactors(t *testing.T) {
	st := setupTestStore(t)
	u, _ := st.CreateUser("ada@test.org", "AdaSecret123!", true)
	key, err := st.AddPasskey(u.ID, "YubiKey", "bam.example.org", []byte{1, 2, 3}, []byte{0xa1, 1, 2}, 7)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddPasskey(u.ID, "Again", "bam.example.org", []byte{1, 2, 3}, []byte{0xa1, 1, 2}, 0); !errors.Is(err, ErrPasskeyRegistered) {
		t.Errorf("registering it twice: %v", err)
	}
	phone, err := st.AddPasskey(u.ID, "Phone", "old.example.org", []byte{4, 5, 6}, []byte{0xa1, 1, 2}, 0)
	if err != nil {
		t.Fatal(err)
	}
	factors, err := st.SealedFactors(u.ID)
	if err != nil || len(factors) != 2 {
		t.Fatalf("factors %+v, %v", factors, err)
	}
	f := factors[0]
	if f.ID != key.ID || f.Kind != FactorPasskey || f.Name != "YubiKey" || f.RPID != "bam.example.org" ||
		f.CredentialID != "AQID" || f.PublicKey != "oQEC" || f.LastStep != 7 || f.ConfirmedAt == nil {
		t.Errorf("read back %+v", f)
	}
	if has, _ := st.HasFactors(); !has[u.ID] {
		t.Error("a passkey does not count as a factor")
	}
	if n, err := st.PasskeysForOtherHosts("bam.example.org"); err != nil || n != 1 {
		t.Errorf("%d passkeys for other hosts, %v; want 1", n, err)
	}

	// The counter is recorded against the one checked, so a second
	// sign-in checked against the same counter is refused.
	if err := st.UsePasskey(key.ID, 7, 8); err != nil {
		t.Fatal(err)
	}
	if err := st.UsePasskey(key.ID, 7, 8); !errors.Is(err, ErrPasskeyUsed) {
		t.Errorf("using it against an old counter: %v", err)
	}

	if err := st.DeleteFactor(u.ID, phone.ID); err != nil {
		t.Fatal(err)
	}
	if factors, _ := st.Factors(u.ID); len(factors) != 1 || factors[0].Name != "YubiKey" {
		t.Errorf("after removing one: %+v", factors)
	}
}
