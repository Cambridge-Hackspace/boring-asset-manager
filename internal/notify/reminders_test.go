package notify

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/email"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// lockedMailer is a fakeMailer two workers can share.
type lockedMailer struct {
	mu   sync.Mutex
	sent []email.Message
	err  error
}

func (m *lockedMailer) Send(_ context.Context, msg email.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.sent = append(m.sent, msg)
	return nil
}

func (m *lockedMailer) subjects() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for _, s := range m.sent {
		out = append(out, s.Subject)
	}
	return out
}

// lendDrill lends a drill with a code, due at due, to Bo, by Stores.
func lendDrill(t *testing.T, st *store.Store, due time.Time) *models.User {
	t.Helper()
	tools, err := st.CreateCategoryFrom(store.CategoryInput{Name: "Tools", IsLendable: true})
	if err != nil {
		t.Fatal(err)
	}
	lender, _ := st.CreateNamedUser("stores@hackspace.org", "Stores", "a long enough password", true)
	bo, _ := st.CreateNamedUser("bo@hackspace.org", "Bo", "a long enough password", true)
	drill, err := st.CreateAsset("Drill", []string{tools.ID}, []models.AssetGeneratorKey{{GeneratorType: "qr", AssignedCode: "DRILL 1"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.As(lender.ID).CheckOut(drill.Asset.ID, bo.ID, &due); err != nil {
		t.Fatal(err)
	}
	return bo
}

func TestLoanRemindersAreSentOnTime(t *testing.T) {
	st := openStore(t)
	due := time.Now().UTC().Add(72 * time.Hour).Truncate(time.Second)
	lendDrill(t, st, due)
	mailer := &lockedMailer{}
	now := due.Add(-48 * time.Hour)
	w := &Worker{Store: st, BaseURL: "https://bam.example/", Mailer: mailer, Now: func() time.Time { return now }}
	ctx := context.Background()
	at := func(when time.Time) int {
		t.Helper()
		now = when
		n, err := w.SendLoanReminders(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}

	// Off, as by default: nothing goes.
	if n := at(due.Add(-time.Hour)); n != 0 {
		t.Fatalf("sent %d while off", n)
	}
	if err := st.SetLoanReminders(store.LoanReminderSettings{On: true, DaysBefore: 1, OverdueTimes: 3}); err != nil {
		t.Fatal(err)
	}
	if n := at(due.Add(-48 * time.Hour)); n != 0 {
		t.Errorf("two days before: %d", n)
	}
	// The day before, once.
	if n := at(due.Add(-23 * time.Hour)); n != 1 {
		t.Errorf("the day before: %d", n)
	}
	if n := at(due.Add(-time.Hour)); n != 0 {
		t.Errorf("again the day before: %d", n)
	}
	// The due day, once it is overdue, then weekly, three in all.
	for _, c := range []struct {
		after time.Duration
		want  int
	}{{time.Minute, 1}, {3 * 24 * time.Hour, 0}, {7*24*time.Hour + time.Minute, 1}, {14*24*time.Hour + time.Minute, 1}, {21*24*time.Hour + time.Minute, 0}, {60 * 24 * time.Hour, 0}} {
		if n := at(due.Add(c.after)); n != c.want {
			t.Errorf("%v after it was due: %d, want %d", c.after, n, c.want)
		}
	}
	got := mailer.subjects()
	want := []string{"Drill is due back " + reminderDate(due), "Drill is overdue", "Drill is still overdue", "Drill is still overdue"}
	if strings.Join(got, " | ") != strings.Join(want, " | ") {
		t.Fatalf("sent %q, want %q", got, want)
	}
	// What is borrowed, when it was due, who lent it, and the links.
	first := mailer.sent[0]
	for _, s := range []string{"You borrowed Drill from Stores", "due back " + reminderDate(due), "https://bam.example/scan/DRILL%201",
		"https://bam.example/assets/", "https://bam.example/account"} {
		if !strings.Contains(first.Text, s) {
			t.Errorf("the reminder lacks %q:\n%s", s, first.Text)
		}
	}
	if first.To[0] != "bo@hackspace.org" || !strings.Contains(mailer.sent[2].Text, "days ago") {
		t.Errorf("to %v; %s", first.To, mailer.sent[2].Text)
	}
}

func TestLoanRemindersAreNotSentToThoseWhoOptedOut(t *testing.T) {
	st := openStore(t)
	due := time.Now().UTC().Add(time.Hour)
	bo := lendDrill(t, st, due)
	if err := st.SetLoanReminders(store.LoanReminderSettings{On: true, DaysBefore: 1, OverdueTimes: 4}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetUserLoanReminders(bo.ID, false); err != nil {
		t.Fatal(err)
	}
	mailer := &lockedMailer{}
	w := &Worker{Store: st, Mailer: mailer}
	if n, err := w.SendLoanReminders(context.Background()); n != 0 || err != nil {
		t.Errorf("sent %d: %v", n, err)
	}
	// Nor without email set up.
	if _, err := st.SetUserLoanReminders(bo.ID, true); err != nil {
		t.Fatal(err)
	}
	if n, err := (&Worker{Store: st}).SendLoanReminders(context.Background()); n != 0 || err != nil {
		t.Errorf("without a mailer: %d %v", n, err)
	}
}

func TestTwoServersSendAReminderOnce(t *testing.T) {
	st := openStore(t)
	due := time.Now().UTC().Add(time.Hour)
	lendDrill(t, st, due)
	if err := st.SetLoanReminders(store.LoanReminderSettings{On: true, DaysBefore: 1, OverdueTimes: 4}); err != nil {
		t.Fatal(err)
	}
	mailer := &lockedMailer{}
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := (&Worker{Store: st, Mailer: mailer}).SendLoanReminders(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if n := len(mailer.subjects()); n != 1 {
		t.Errorf("sent %d times", n)
	}
}

func TestAReminderThatFailsIsTriedAgain(t *testing.T) {
	st := openStore(t)
	lendDrill(t, st, time.Now().UTC().Add(time.Hour))
	if err := st.SetLoanReminders(store.LoanReminderSettings{On: true, DaysBefore: 1, OverdueTimes: 4}); err != nil {
		t.Fatal(err)
	}
	mailer := &lockedMailer{err: errors.New("the mail server is down")}
	w := &Worker{Store: st, Mailer: mailer}
	if n, err := w.SendLoanReminders(context.Background()); n != 0 || err != nil {
		t.Errorf("failing: %d %v", n, err)
	}
	mailer.err = nil
	if n, err := w.SendLoanReminders(context.Background()); n != 1 || err != nil {
		t.Errorf("tried again: %d %v", n, err)
	}
}
