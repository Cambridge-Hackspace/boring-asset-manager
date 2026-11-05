package store

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

func TestWhichReminderIsDue(t *testing.T) {
	due := time.Date(2026, 11, 10, 17, 0, 0, 0, time.UTC)
	day, week := 24*time.Hour, 7*24*time.Hour
	for _, c := range []struct {
		at   time.Duration
		days int
		want string
	}{
		{-3 * day, 1, ""},
		{-day - time.Minute, 1, ""},
		{-day, 1, "due"},
		{-time.Minute, 1, "due"},
		{-2 * day, 2, "due"},
		{-time.Hour, 0, ""}, // none before, when set so
		{0, 1, "overdue-0"}, // the moment it is due
		{6 * day, 1, "overdue-0"},
		{week, 1, "overdue-1"},
		{3*week + day, 1, "overdue-3"},
		{4 * week, 1, ""}, // four overdue reminders, and no more
		{20 * week, 1, ""},
	} {
		got, ok := ReminderDue(due, due.Add(c.at), c.days, 4)
		if got != c.want || ok != (c.want != "") {
			t.Errorf("at %v, %d days before: %q %v, want %q", c.at, c.days, got, ok, c.want)
		}
	}
	if _, ok := ReminderDue(due, due, 1, 0); ok {
		t.Error("an overdue reminder with none allowed")
	}
}

func TestLoanReminderSettings(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	set, err := st.LoanReminders()
	if err != nil || set.On || set.DaysBefore != 1 || set.OverdueTimes != 4 {
		t.Fatalf("defaults: %+v %v", set, err)
	}
	if err := st.SetLoanReminders(LoanReminderSettings{On: true, DaysBefore: 3, OverdueTimes: 2}); err != nil {
		t.Fatal(err)
	}
	if set, _ := st.LoanReminders(); set != (LoanReminderSettings{On: true, DaysBefore: 3, OverdueTimes: 2}) {
		t.Errorf("saved: %+v", set)
	}
	for _, bad := range []LoanReminderSettings{{DaysBefore: -1}, {DaysBefore: 31}, {OverdueTimes: 53}, {OverdueTimes: -2}} {
		if err := st.SetLoanReminders(bad); !errors.Is(err, ErrInvalidSetting) {
			t.Errorf("%+v: %v", bad, err)
		}
	}
}

func TestDueLoanReminders(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	_, drills, lender, bo := lending(t, st)
	ann, _ := st.CreateNamedUser("ann@hackspace.org", "Ann", "a long enough password", true)
	lent := st.As(lender.ID)
	now := time.Now().UTC()
	due := now.Add(12 * time.Hour)
	lend := func(name string, borrower string, due *time.Time, codes ...models.AssetGeneratorKey) *models.AssetWithDetails {
		a, err := st.CreateAsset(name, []string{drills.ID}, codes, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := lent.CheckOut(a.Asset.ID, borrower, due); err != nil {
			t.Fatal(err)
		}
		return a
	}
	drill := lend("Drill", bo.ID, &due, models.AssetGeneratorKey{GeneratorType: "qr", AssignedCode: "DRILL-1"})
	lend("Undated", bo.ID, nil)
	lend("Ann's saw", ann.ID, &due)
	returned := lend("Sander", bo.ID, &due)
	if _, err := lent.CheckIn(returned.Asset.ID, "", nil); err != nil {
		t.Fatal(err)
	}
	// Ann turned reminders off.
	if u, err := st.SetUserLoanReminders(ann.ID, false); err != nil || u.LoanReminders {
		t.Fatalf("opting out: %+v %v", u, err)
	}

	set := LoanReminderSettings{On: true, DaysBefore: 1, OverdueTimes: 4}
	got, err := st.DueLoanReminders(now, set)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("due: %+v", got)
	}
	r := got[0]
	if r.AssetID != drill.Asset.ID || r.Reminder != "due" || r.Code != "DRILL-1" || r.Borrower.Email != "bo@hackspace.org" ||
		r.LentBy == nil || r.LentBy.Name != "Stores" || r.Overdue() {
		t.Errorf("reminder %+v", r)
	}

	// Claimed once, however many ask at once.
	var wg sync.WaitGroup
	var mu sync.Mutex
	claims := 0
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := st.ClaimLoanReminder(r.LoanID, r.Reminder, now)
			if err != nil {
				t.Error(err)
			}
			if ok {
				mu.Lock()
				claims++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if claims != 1 {
		t.Errorf("claimed %d times", claims)
	}
	if got, _ := st.DueLoanReminders(now, set); len(got) != 0 {
		t.Errorf("a reminder sent is due again: %+v", got)
	}
	// Given back, as when sending it failed, it is due again.
	if err := st.ReleaseLoanReminder(r.LoanID, r.Reminder); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.DueLoanReminders(now, set); len(got) != 1 {
		t.Errorf("a reminder given back: %+v", got)
	}
	// Overdue, it is the next one.
	if got, _ := st.DueLoanReminders(due.Add(time.Hour), set); len(got) != 1 || got[0].Reminder != "overdue-0" || !got[0].Overdue() {
		t.Errorf("overdue: %+v", got)
	}
}
