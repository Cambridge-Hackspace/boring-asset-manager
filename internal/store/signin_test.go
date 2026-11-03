package store

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// throttledEvents returns the auth.throttled events recorded so far.
func throttledEvents(t *testing.T, st *Store) []models.Event {
	t.Helper()
	events, err := st.ListEvents(500)
	if err != nil {
		t.Fatal(err)
	}
	var out []models.Event
	for _, e := range events {
		if e.Kind == models.EventAuthThrottled {
			out = append(out, e)
		}
	}
	return out
}

func TestAccountWaitsGrowAfterFiveWrongPasswordsAndStopAtFifteenMinutes(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	if _, err := st.CreateUser("ada@example.com", "Password123!", true); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	// Each failure comes once the wait before it is over, from a different
	// address each time, so only the account counts.
	want := []time.Duration{0, 0, 0, 0, time.Second, 2 * time.Second, 4 * time.Second}
	for i, w := range want {
		wait, err := st.RecordSignInFailure("Ada@Example.com", "", now)
		if err != nil {
			t.Fatal(err)
		}
		if wait != w {
			t.Errorf("failure %d: wait %v, want %v", i+1, wait, w)
		}
		now = now.Add(wait)
	}
	if wait, _ := st.SignInWait("ada@example.com", "", now.Add(-time.Second)); wait != time.Second {
		t.Errorf("one second before the wait ends: %v left, want 1s", wait)
	}
	for range 20 {
		wait, err := st.RecordSignInFailure("ada@example.com", "", now)
		if err != nil {
			t.Fatal(err)
		}
		if wait > MaxAccountWait {
			t.Fatalf("waits %v, more than %v", wait, MaxAccountWait)
		}
		now = now.Add(wait)
	}
	if wait, _ := st.RecordSignInFailure("ada@example.com", "", now); wait != MaxAccountWait {
		t.Errorf("after many failures: wait %v, want the most, %v", wait, MaxAccountWait)
	}

	// The account started waiting once, and the event names it by its user.
	events := throttledEvents(t, st)
	if len(events) != 1 {
		t.Fatalf("%d auth.throttled events, want 1", len(events))
	}
	th := events[0].Payload.Throttle
	if th == nil || th.Kind != models.ThrottleAccount || th.User == nil || th.User.Email != "" || th.Address != "" {
		t.Errorf("event names %+v, want the account's user by name only", th)
	}
	if events[0].AssetID != nil || events[0].CategoryID != nil || events[0].ActorUserID != nil {
		t.Errorf("event is about something or someone: %+v", events[0])
	}
}

func TestTheRightPasswordOrAnAdminClearsAnAccountsWait(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	now := time.Now()
	for range AccountFailuresAllowed {
		if _, err := st.RecordSignInFailure("bo@example.com", "", now); err != nil {
			t.Fatal(err)
		}
	}
	delays, err := st.SignInDelays(now)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := delays["bo@example.com"]; !ok {
		t.Fatalf("delays %v, want bo@example.com waiting", delays)
	}
	if err := st.ClearSignInFailures("BO@example.com "); err != nil {
		t.Fatal(err)
	}
	if wait, _ := st.SignInWait("bo@example.com", "", now); wait != 0 {
		t.Errorf("after clearing: wait %v, want none", wait)
	}
	// The count starts again from nothing.
	if wait, _ := st.RecordSignInFailure("bo@example.com", "", now); wait != 0 {
		t.Errorf("first failure after clearing waits %v", wait)
	}
}

func TestAnAddressWaitsAfterTwentyWrongPasswordsAcrossAccounts(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	now := time.Now()
	for i := range AddressFailuresAllowed {
		// A different email each time: one password tried on many.
		wait, err := st.RecordSignInFailure(string(rune('a'+i))+"@example.com", "203.0.113.7", now)
		if err != nil {
			t.Fatal(err)
		}
		if i < AddressFailuresAllowed-1 && wait != 0 {
			t.Fatalf("failure %d from the address waits %v", i+1, wait)
		}
		if i == AddressFailuresAllowed-1 && wait != ThrottleWindow {
			t.Fatalf("20th failure waits %v, want %v", wait, ThrottleWindow)
		}
	}
	if wait, _ := st.SignInWait("someone-new@example.com", "203.0.113.7", now); wait != ThrottleWindow {
		t.Errorf("a new email from the address waits %v, want %v", wait, ThrottleWindow)
	}
	if wait, _ := st.SignInWait("someone-new@example.com", "198.51.100.1", now); wait != 0 {
		t.Errorf("another address waits %v", wait)
	}
	// None of the emails has an account, so only the address is announced.
	events := throttledEvents(t, st)
	if len(events) != 1 || events[0].Payload.Throttle.Kind != models.ThrottleAddress || events[0].Payload.Throttle.Address != "203.0.113.7" {
		t.Errorf("events %+v, want one for the address", events)
	}
}

func TestACountLapsesAWindowAfterItsLastFailureOrWait(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	now := time.Now()
	for range AccountFailuresAllowed - 1 {
		if _, err := st.RecordSignInFailure("cy@example.com", "", now); err != nil {
			t.Fatal(err)
		}
	}
	// Past the window, the fifth failure is the first of a new count.
	now = now.Add(ThrottleWindow + time.Second)
	if wait, _ := st.RecordSignInFailure("cy@example.com", "", now); wait != 0 {
		t.Errorf("failure after the window waits %v, want none", wait)
	}
}

func TestAnEmailTooLongForAnAccountIsStillCounted(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	long := strings.Repeat("a", 2000) + "@example.com"
	now := time.Now()
	var wait time.Duration
	for range AccountFailuresAllowed {
		var err error
		if wait, err = st.RecordSignInFailure(long, "", now); err != nil {
			t.Fatal(err)
		}
	}
	if wait != time.Second {
		t.Errorf("wait %v, want 1s", wait)
	}
}

func TestServersSharingTheDatabaseCountTogether(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	// Another server: its own Store over the same database.
	other := st.As("")
	now := time.Now()
	var wg sync.WaitGroup
	errs := make(chan error, AddressFailuresAllowed)
	for i := range AddressFailuresAllowed {
		server := st
		if i%2 == 1 {
			server = other
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := server.RecordSignInFailure("x@example.com", "192.0.2.9", now)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	// Every failure counted, none lost between them: the address waits.
	if wait, _ := st.SignInWait("y@example.com", "192.0.2.9", now); wait != ThrottleWindow {
		t.Errorf("after %d failures across two servers the address waits %v, want %v", AddressFailuresAllowed, wait, ThrottleWindow)
	}
}
