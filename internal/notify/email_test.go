package notify

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/email"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// fakeMailer keeps what it is asked to send, or fails with err.
type fakeMailer struct {
	sent []email.Message
	err  error
}

func (f *fakeMailer) Send(_ context.Context, m email.Message) error {
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, m)
	return nil
}

func overdueDrill() models.Event {
	return models.Event{Kind: models.EventAssetOverdue, Payload: models.EventPayload{
		Asset:    &models.EventAsset{ID: "a1", Name: "Drill_<b>"},
		Trigger:  models.TriggerSchedule,
		Borrower: &models.EventActor{ID: "u1", Name: "Bo"},
	}}
}

func TestEmailSendsWhatDiscordWouldToAddressesAndRoleHolders(t *testing.T) {
	m := &fakeMailer{}
	var askedRoles []string
	n := Notification{Event: overdueDrill(), BaseURL: "https://bam.example", Mailer: m,
		Recipients: func(roles []string) ([]string, error) {
			askedRoles = roles
			return []string{"bo@example.org", "Ada@example.org"}, nil
		}}
	err := Email{}.Send(context.Background(), map[string]string{"to": "ada@example.org, cy@example.org\nada@example.org", "roles": "keepers, admin"}, n)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(askedRoles, "|") != "keepers|admin" {
		t.Errorf("asked for roles %v", askedRoles)
	}
	if len(m.sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(m.sent))
	}
	got := m.sent[0]
	// Each address once, whatever its case.
	if strings.Join(got.To, ",") != "ada@example.org,bo@example.org,cy@example.org" {
		t.Errorf("to %v", got.To)
	}
	// The same words as Discord, without Discord's escaping.
	if got.Subject != "Overdue: Drill_<b>" || !strings.Contains(got.Text, "Lent to Bo.") || !strings.Contains(got.Text, "https://bam.example/assets/a1") {
		t.Errorf("message %+v", got)
	}
	if !strings.Contains(got.HTML, "Overdue: Drill_&lt;b&gt;") || !strings.Contains(got.HTML, "#f59e0b") {
		t.Errorf("html %s", got.HTML)
	}
}

func TestEmailToNoOneSendsNothing(t *testing.T) {
	m := &fakeMailer{}
	n := Notification{Event: overdueDrill(), Mailer: m, Recipients: func([]string) ([]string, error) { return nil, nil }}
	if err := (Email{}).Send(context.Background(), map[string]string{"roles": "keepers"}, n); err != nil || len(m.sent) != 0 {
		t.Errorf("sent %d, %v; want nothing and no error", len(m.sent), err)
	}
}

func TestEmailFailures(t *testing.T) {
	opts := map[string]string{"to": "ada@example.org"}
	if err := (Email{}).Send(context.Background(), opts, Notification{Event: overdueDrill()}); !errors.Is(err, email.ErrNotSetUp) || errors.Is(err, ErrTemporary) {
		t.Errorf("no mailer: %v, want ErrNotSetUp, permanent", err)
	}
	m := &fakeMailer{err: email.ErrTemporary}
	if err := (Email{}).Send(context.Background(), opts, Notification{Event: overdueDrill(), Mailer: m}); !errors.Is(err, ErrTemporary) {
		t.Errorf("server busy: %v, want temporary", err)
	}
	m.err = errors.New("550 no such mailbox")
	if err := (Email{}).Send(context.Background(), opts, Notification{Event: overdueDrill(), Mailer: m}); err == nil || errors.Is(err, ErrTemporary) {
		t.Errorf("refused: %v, want permanent", err)
	}
}

func TestEmailOptions(t *testing.T) {
	for _, bad := range []map[string]string{{}, {"to": " , "}, {"to": "not an address"}} {
		if err := (Email{}).Validate(bad); err == nil {
			t.Errorf("accepted %v", bad)
		}
	}
	for _, good := range []map[string]string{{"to": "Ada <ada@example.org>"}, {"roles": "admin"}} {
		if err := (Email{}).Validate(good); err != nil {
			t.Errorf("refused %v: %v", good, err)
		}
	}
}

func TestEmailTestMessage(t *testing.T) {
	m := &fakeMailer{}
	n := Notification{Event: models.Event{Kind: "test"}, Test: true, Mailer: m}
	if err := (Email{}).Send(context.Background(), map[string]string{"to": "ada@example.org"}, n); err != nil {
		t.Fatal(err)
	}
	if m.sent[0].Subject != "Test message from BAM!" || !strings.Contains(m.sent[0].Text, "This channel is set up correctly.") {
		t.Errorf("test message %+v", m.sent[0])
	}
}
