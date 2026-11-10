package store

import (
	"errors"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// TestAssistantSettingsKeepTheirKeyApart: settings read their defaults
// until saved, keep the key they are given, and refuse what is out of
// range.
func TestAssistantSettingsKeepTheirKeyApart(t *testing.T) {
	st := setupTestStore(t)
	set, err := st.AssistantSettings()
	if err != nil || set.Enabled || set.KeepDays != DefaultAssistantKeepDays || set.Model != DefaultAssistantModel[AssistantAnthropic] || set.KeySet {
		t.Fatalf("defaults %+v, %v", set, err)
	}
	set.Enabled, set.APIKey, set.Provider, set.Endpoint, set.Model = true, "k", AssistantOpenAI, "", ""
	if err := st.SaveAssistantSettings(set, ""); err != nil {
		t.Fatal(err)
	}
	got, err := st.AssistantSettings()
	if err != nil || !got.Enabled || got.APIKey != "k" || !got.KeySet || got.Endpoint != DefaultAssistantEndpoint[AssistantOpenAI] || got.Model != DefaultAssistantModel[AssistantOpenAI] {
		t.Errorf("saved %+v, %v", got, err)
	}
	for _, bad := range []func(*AssistantSettings){
		func(s *AssistantSettings) { s.Provider = "other" },
		func(s *AssistantSettings) { s.KeepDays = 0 },
		func(s *AssistantSettings) { s.PersonTokens = -1 },
		func(s *AssistantSettings) { s.Endpoint = "ftp://x" },
	} {
		s := got
		bad(&s)
		if err := st.SaveAssistantSettings(s, ""); !errors.Is(err, ErrInvalidSetting) {
			t.Errorf("%+v saved: %v", s, err)
		}
	}
}

// TestConversationsBelongToTheirPerson: a conversation is read, listed and
// deleted by its person only; old ones are deleted, and archiving an
// account deletes its person's.
func TestConversationsBelongToTheirPerson(t *testing.T) {
	st := setupTestStore(t)
	alice, err := st.CreateUser("alice@test.org", "AliceSecret123!", true)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := st.CreateUser("bob@test.org", "BobSecret123!", true)
	if err != nil {
		t.Fatal(err)
	}
	c, err := st.CreateConversation(alice.ID, "", "")
	if err != nil || c.Title != UntitledConversation {
		t.Fatalf("%+v %v", c, err)
	}
	if _, err := st.AddConversationMessage(c.ID, ConversationMessage{Role: "user", Content: []byte(`[{"type":"text","text":"hi"}]`)}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddConversationMessage(c.ID, ConversationMessage{Role: "assistant", Content: []byte(`[]`), Raw: []byte(`[{"type":"text"}]`), RawProvider: "anthropic", Tokens: 5}, "call"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetConversation(bob.ID, c.ID); !errors.Is(err, ErrConversationNotFound) {
		t.Errorf("bob read alice's: %v", err)
	}
	if err := st.DeleteConversation(bob.ID, c.ID); !errors.Is(err, ErrConversationNotFound) {
		t.Errorf("bob deleted alice's: %v", err)
	}
	if list, _ := st.ListConversations(bob.ID); len(list) != 0 {
		t.Errorf("bob lists %v", list)
	}
	got, err := st.GetConversation(alice.ID, c.ID)
	if err != nil || got.PendingCall != "call" {
		t.Errorf("%+v %v", got, err)
	}
	msgs, err := st.ConversationMessages(c.ID)
	if err != nil || len(msgs) != 2 || msgs[1].Seq != 2 || string(msgs[1].Raw) != `[{"type":"text"}]` || msgs[1].RawProvider != "anthropic" {
		t.Errorf("messages %+v %v", msgs, err)
	}
	if n, err := st.DeleteConversationsBefore(time.Now().Add(-time.Hour)); err != nil || n != 0 {
		t.Errorf("deleted a new one: %d %v", n, err)
	}
	if n, err := st.DeleteConversationsBefore(time.Now().Add(time.Hour)); err != nil || n != 1 {
		t.Errorf("deleted %d, %v", n, err)
	}
	if msgs, _ := st.ConversationMessages(c.ID); len(msgs) != 0 {
		t.Errorf("messages outlived their conversation: %v", msgs)
	}
	c, _ = st.CreateConversation(bob.ID, "Shelves", "")
	if err := st.DeleteUser(bob.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetConversation(bob.ID, c.ID); !errors.Is(err, ErrConversationNotFound) {
		t.Errorf("an archived account's conversation was kept: %v", err)
	}
}

// TestAssistantUsageCountsADay counts each person's use a day, and the
// instance's as theirs together.
func TestAssistantUsageCountsADay(t *testing.T) {
	st := setupTestStore(t)
	day := AssistantDay(time.Now())
	for _, u := range []struct {
		who      string
		messages int
		tokens   int64
	}{{"a", 1, 0}, {"a", 0, 100}, {"b", 1, 50}} {
		if err := st.AddAssistantUsage(u.who, day, u.messages, u.tokens); err != nil {
			t.Fatal(err)
		}
	}
	person, instance, err := st.AssistantUsage("a", day)
	if err != nil || person != (AssistantUse{1, 100}) || instance != (AssistantUse{2, 150}) {
		t.Errorf("person %+v, instance %+v, %v", person, instance, err)
	}
	if other, _, _ := st.AssistantUsage("a", "2000-01-01"); other != (AssistantUse{}) {
		t.Errorf("another day: %+v", other)
	}
}

// TestGuidesHaveAnAddress: a guide is saved, read at its address, checked,
// and one address holds one guide.
func TestGuidesHaveAnAddress(t *testing.T) {
	st := setupTestStore(t)
	in := GuideInput{Slug: "workshop", Name: "Workshop", Purpose: "Record it", Enabled: true, Questions: []string{" What? ", ""}, Category: []string{"c", "c"}}
	g, err := st.SaveGuide("", in, "")
	if err != nil || len(g.Questions) != 1 || g.Questions[0] != "What?" || len(g.Category) != 1 || len(g.LinkType) != 0 {
		t.Fatalf("%+v %v", g, err)
	}
	if _, err := st.SaveGuide("", in, ""); !IsUniqueViolation(err) {
		t.Errorf("a second at the address: %v", err)
	}
	if _, err := st.SaveGuide("", GuideInput{Slug: "Bad Slug", Name: "x", Purpose: "y"}, ""); !errors.Is(err, ErrInvalidGuide) {
		t.Errorf("a bad address: %v", err)
	}
	in.Name = "The workshop"
	if g, err = st.SaveGuide(g.ID, in, ""); err != nil || g.Name != "The workshop" {
		t.Errorf("%+v %v", g, err)
	}
	if got, err := st.GetGuide("workshop"); err != nil || got.ID != g.ID {
		t.Errorf("%+v %v", got, err)
	}
	if err := st.DeleteGuide(g.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetGuide("workshop"); !errors.Is(err, ErrGuideNotFound) {
		t.Errorf("after deleting: %v", err)
	}
}

// TestAssistantDraftsWhereReviewIsOn: an assistant's change lands as a
// draft only where the instance says so and every asset named is in a
// category using review states.
func TestAssistantDraftsWhereReviewIsOn(t *testing.T) {
	st := setupTestStore(t)
	on, err := st.CreateCategory("Tools", nil, "#336699", false, false)
	if err != nil {
		t.Fatal(err)
	}
	off, err := st.CreateCategory("Kitchen", nil, "#336699", false, false)
	if err != nil {
		t.Fatal(err)
	}
	yes := true
	reviewing(t, st, on.ID, models.CategoryReview{Enabled: &yes})
	saw, _ := st.CreateAsset("Saw", []string{on.ID}, nil, nil)
	kettle, _ := st.CreateAsset("Kettle", []string{off.ID}, nil, nil)
	check := func(name string, got bool, err error, want bool) {
		t.Helper()
		if err != nil || got != want {
			t.Errorf("%s: %v %v, want %v", name, got, err, want)
		}
	}
	got, err := st.AssistantDraftsForAssets([]string{saw.Asset.ID})
	check("saw", got, err, true)
	got, err = st.AssistantDraftsForAssets([]string{saw.Asset.ID, kettle.Asset.ID})
	check("saw and kettle", got, err, false)
	got, err = st.AssistantDraftsForAssets([]string{"nothing"})
	check("nothing", got, err, false)
	got, err = st.AssistantDraftsForCategories([]string{on.ID})
	check("new in tools", got, err, true)
	got, err = st.AssistantDraftsForCategories(nil)
	check("new in none", got, err, false)
	if err := st.SetReviewSettings(ReviewSettings{AssistantDrafts: false, DueDays: DefaultReviewDueDays}); err != nil {
		t.Fatal(err)
	}
	got, err = st.AssistantDraftsForAssets([]string{saw.Asset.ID})
	check("saw, drafts off", got, err, false)
}
