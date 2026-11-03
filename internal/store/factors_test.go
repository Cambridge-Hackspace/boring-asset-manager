package store

import (
	"errors"
	"sync"
	"testing"
)

func TestFactorsAreSetUpOnceConfirmed(t *testing.T) {
	st := setupTestStore(t)
	u, err := st.CreateUser("ada@test.org", "AdaSecret123!", true)
	if err != nil {
		t.Fatal(err)
	}
	seal := func(string) (string, error) { return "v1:sealed", nil }
	first, err := st.StartTOTP(u.ID, seal)
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.StartTOTP(u.ID, seal)
	if err != nil {
		t.Fatal(err)
	}
	// Starting again replaces the one not confirmed.
	pending, err := st.PendingTOTP(u.ID)
	if err != nil || pending.ID != second.ID {
		t.Fatalf("pending %+v, %v; want %s, not %s", pending, err, second.ID, first.ID)
	}
	if has, _ := st.HasFactors(); has[u.ID] {
		t.Error("a factor not confirmed counts")
	}
	if _, err := st.ConfirmTOTP(u.ID, first.ID, 10); !errors.Is(err, ErrFactorNotFound) {
		t.Errorf("confirming the replaced one: %v", err)
	}
	if _, err := st.ConfirmTOTP(u.ID, second.ID, 10); err != nil {
		t.Fatal(err)
	}
	factors, err := st.SealedFactors(u.ID)
	if err != nil || len(factors) != 1 || factors[0].LastStep != 10 || factors[0].Secret != "v1:sealed" {
		t.Fatalf("factors %+v, %v", factors, err)
	}
	if has, _ := st.HasFactors(); !has[u.ID] {
		t.Error("the confirmed factor does not count")
	}
	if _, err := st.PendingTOTP(u.ID); !errors.Is(err, ErrFactorNotFound) {
		t.Errorf("still pending: %v", err)
	}
}

func TestACodesStepIsAcceptedOnceAcrossServers(t *testing.T) {
	st := setupTestStore(t)
	u, _ := st.CreateUser("ada@test.org", "AdaSecret123!", true)
	f, _ := st.StartTOTP(u.ID, func(string) (string, error) { return "v1:sealed", nil })
	if _, err := st.ConfirmTOTP(u.ID, f.ID, 10); err != nil {
		t.Fatal(err)
	}
	if err := st.UseTOTPStep(f.ID, 10); !errors.Is(err, ErrCodeUsed) {
		t.Errorf("the confirming step again: %v", err)
	}
	// Several servers given the same code at once: one accepts it.
	var wg sync.WaitGroup
	var mu sync.Mutex
	accepted := 0
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := st.UseTOTPStep(f.ID, 11); err == nil {
				mu.Lock()
				accepted++
				mu.Unlock()
			} else if !errors.Is(err, ErrCodeUsed) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if accepted != 1 {
		t.Errorf("step 11 accepted %d times", accepted)
	}
	if err := st.UseTOTPStep(f.ID, 9); !errors.Is(err, ErrCodeUsed) {
		t.Errorf("an earlier step: %v", err)
	}
}

func TestAChallengeEndsWithItsUsersSignIn(t *testing.T) {
	st := setupTestStore(t)
	u, _ := st.CreateUser("ada@test.org", "AdaSecret123!", true)
	token, err := st.CreateSignInChallenge(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := st.SignInChallenge(token); err != nil || got.ID != u.ID {
		t.Fatalf("challenge: %+v, %v", got, err)
	}
	if _, err := st.SignInChallenge("other"); !errors.Is(err, ErrChallengeNotFound) {
		t.Errorf("a token never given: %v", err)
	}
	// Turned off while the second step waits, the sign-in goes no further.
	if err := st.UpdateUserStatus(u.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SignInChallenge(token); !errors.Is(err, ErrChallengeNotFound) {
		t.Errorf("a challenge of someone turned off: %v", err)
	}
	if err := st.UpdateUserStatus(u.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := st.EndSignInChallenge(token); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SignInChallenge(token); !errors.Is(err, ErrChallengeNotFound) {
		t.Errorf("an ended challenge: %v", err)
	}
}
