package store

import (
	"errors"
	"testing"
	"time"
)

func TestAccountLinksWorkOnceUntilTheyExpire(t *testing.T) {
	st := setupTestStore(t)
	ada, err := st.CreateInvitedUser("Ada@Test.org", "Ada")
	if err != nil || ada.Email != "ada@test.org" || HasPassword(ada) {
		t.Fatalf("invited: %+v, %v", ada, err)
	}
	if _, err := st.CreateInvitedUser("ada@test.org", "Ada again"); !errors.Is(err, ErrEmailTaken) {
		t.Errorf("the same email: %v", err)
	}

	token, err := st.CreateAccountLink(ada.ID, LinkInvite, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	// Read as often as liked; of another purpose, it is not there.
	for i := 0; i < 2; i++ {
		if u, err := st.AccountLink(token, LinkInvite); err != nil || u.ID != ada.ID {
			t.Fatalf("reading it: %+v, %v", u, err)
		}
	}
	if _, err := st.AccountLink(token, LinkPasswordReset); !errors.Is(err, ErrAccountLinkNotFound) {
		t.Errorf("as a reset: %v", err)
	}
	if u, err := st.TakeAccountLink(token, LinkInvite); err != nil || u.ID != ada.ID {
		t.Fatalf("using it: %+v, %v", u, err)
	}
	if _, err := st.TakeAccountLink(token, LinkInvite); !errors.Is(err, ErrAccountLinkNotFound) {
		t.Errorf("using it again: %v", err)
	}

	// A new one replaces the one not yet used.
	first, _ := st.CreateAccountLink(ada.ID, LinkPasswordReset, time.Hour)
	second, _ := st.CreateAccountLink(ada.ID, LinkPasswordReset, time.Hour)
	if _, err := st.AccountLink(first, LinkPasswordReset); !errors.Is(err, ErrAccountLinkNotFound) {
		t.Errorf("the first: %v", err)
	}
	if _, err := st.AccountLink(second, LinkPasswordReset); err != nil {
		t.Errorf("the second: %v", err)
	}
	// Expired, or for a disabled account, it does not work.
	gone, _ := st.CreateAccountLink(ada.ID, LinkFactorRecovery, -time.Second)
	if _, err := st.TakeAccountLink(gone, LinkFactorRecovery); !errors.Is(err, ErrAccountLinkNotFound) {
		t.Errorf("expired: %v", err)
	}
	if _, err := st.Exec("UPDATE users SET is_active = FALSE WHERE id = ?", ada.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AccountLink(second, LinkPasswordReset); !errors.Is(err, ErrAccountLinkNotFound) {
		t.Errorf("a disabled account: %v", err)
	}
	if _, err := st.AccountLink("", LinkInvite); !errors.Is(err, ErrAccountLinkNotFound) {
		t.Errorf("no token: %v", err)
	}
}

func TestOnOffSettings(t *testing.T) {
	st := setupTestStore(t)
	if on, err := st.On(PasswordResetSetting); err != nil || on {
		t.Errorf("unset: %v, %v", on, err)
	}
	_ = st.SetSetting(PasswordResetSetting, "on")
	if on, _ := st.On(PasswordResetSetting); !on {
		t.Error("set on")
	}
}
