package store

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/auth"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
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

func TestRecoveryCodesAreUsedOnceAndKeptAsHashes(t *testing.T) {
	st := setupTestStore(t)
	u, err := st.CreateUser("ada@test.org", "AdaSecret123!", true)
	if err != nil {
		t.Fatal(err)
	}
	app, err := st.StartTOTP(u.ID, func(string) (string, error) { return "v1:sealed", nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ConfirmTOTP(u.ID, app.ID, 10); err != nil {
		t.Fatal(err)
	}
	old, _ := auth.NewRecoveryCodes(RecoveryCodeCount)
	if err := st.ReplaceRecoveryCodes(u.ID, old); err != nil {
		t.Fatal(err)
	}
	left := func() int {
		n, err := st.RecoveryCodesLeft(u.ID)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if left() != 10 {
		t.Errorf("made: %d", left())
	}
	// They are not factors of their own.
	if factors, _ := st.Factors(u.ID); len(factors) != 1 || factors[0].Kind != FactorTOTP {
		t.Errorf("listed: %+v", factors)
	}
	// Not readable from the database: nothing stored holds a code.
	rows, err := st.Query("SELECT secret FROM user_factors WHERE kind = ?", FactorRecovery)
	if err != nil {
		t.Fatal(err)
	}
	stored := 0
	for rows.Next() {
		var secret string
		_ = rows.Scan(&secret)
		stored++
		for _, c := range old {
			n := auth.NormalizeRecoveryCode(c)
			if strings.Contains(secret, n) || strings.Contains(secret, c) || len(secret) != 64 {
				t.Errorf("stored as %q", secret)
			}
		}
	}
	rows.Close()
	if stored != 10 {
		t.Errorf("stored %d", stored)
	}

	// Typed loosely, once; again, refused.
	if err := st.UseRecoveryCode(u.ID, " "+strings.ToUpper(old[0])+" "); err != nil {
		t.Fatal(err)
	}
	if err := st.UseRecoveryCode(u.ID, old[0]); !errors.Is(err, ErrRecoveryCodeNotFound) {
		t.Errorf("used twice: %v", err)
	}
	if left() != 9 {
		t.Errorf("after one: %d", left())
	}
	events, _ := st.ListEvents(1)
	if len(events) != 1 || events[0].Kind != models.EventAuthRecoveryCodeUsed || events[0].Payload.Account == nil || events[0].Payload.Account.ID != u.ID {
		t.Errorf("history: %+v", events)
	}
	// Someone else's code is not theirs.
	bob, _ := st.CreateUser("bob@test.org", "BobSecret123!", true)
	if err := st.UseRecoveryCode(bob.ID, old[1]); !errors.Is(err, ErrRecoveryCodeNotFound) {
		t.Errorf("another's: %v", err)
	}

	// New ones replace them all.
	fresh, _ := auth.NewRecoveryCodes(RecoveryCodeCount)
	if err := st.ReplaceRecoveryCodes(u.ID, fresh); err != nil {
		t.Fatal(err)
	}
	if err := st.UseRecoveryCode(u.ID, old[1]); !errors.Is(err, ErrRecoveryCodeNotFound) {
		t.Errorf("an old one after replacing: %v", err)
	}
	if left() != 10 {
		t.Errorf("after replacing: %d", left())
	}

	// They go with the last factor.
	if err := st.DeleteFactor(u.ID, app.ID); err != nil {
		t.Fatal(err)
	}
	if left() != 0 {
		t.Errorf("after the last factor: %d", left())
	}
}

func TestRecoveryCodesUsedAtOnceCountOnce(t *testing.T) {
	st := setupTestStore(t)
	u, err := st.CreateUser("ada@test.org", "AdaSecret123!", true)
	if err != nil {
		t.Fatal(err)
	}
	codes, _ := auth.NewRecoveryCodes(1)
	if err := st.ReplaceRecoveryCodes(u.ID, codes); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	accepted := 0
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := st.UseRecoveryCode(u.ID, codes[0]); err == nil {
				mu.Lock()
				accepted++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if accepted != 1 {
		t.Errorf("accepted %d times", accepted)
	}
}
