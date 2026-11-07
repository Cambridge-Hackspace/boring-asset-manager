package notify

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/markdown"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// TestMentionsAreEmailedOnce: a mention emails the person once, by the
// worker, naming who wrote it and those it mentions to someone who may see
// who; an edit emails only those it mentions first. No one who cannot read
// what the comment is on, or has turned mentions off, is emailed, nor
// anyone about a deleted comment; a failed email is tried again.
func TestMentionsAreEmailedOnce(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	members, err := st.CreateRole(store.RoleInput{Name: "Members", Permissions: []string{"assets.read", "assets.track", "comments.write"}})
	if err != nil {
		t.Fatal(err)
	}
	woodshop, _ := st.CreateScope("woodshop")
	saws, _ := st.CreateCategory("Saws", nil, "#000000", false, false)
	if _, err := st.SetCategoryScopes(saws.ID, []string{woodshop.ID}); err != nil {
		t.Fatal(err)
	}
	person := func(name string, roles ...models.RoleAssignment) *models.User {
		u, err := st.CreateNamedUser(strings.ToLower(name)+"@hackspace.org", name, "a long enough password", true)
		if err != nil {
			t.Fatal(err)
		}
		if err := st.SetUserRoleAssignments(u.ID, roles); err != nil {
			t.Fatal(err)
		}
		return u
	}
	ada := person("Ada", models.RoleAssignment{RoleID: members.ID})
	bob := person("Bob", models.RoleAssignment{RoleID: members.ID})
	cy := person("Cy", models.RoleAssignment{RoleID: members.ID})
	quiet := person("Quiet", models.RoleAssignment{RoleID: members.ID})
	wood := person("Wood", models.RoleAssignment{RoleID: members.ID, ScopeIDs: []string{woodshop.ID}})
	if _, err := st.SetUserMentionEmails(quiet.ID, false); err != nil {
		t.Fatal(err)
	}
	drill, err := st.CreateAsset("Drill", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	as := st.As(ada.ID)

	now := time.Now()
	mailer := &lockedMailer{}
	w := &Worker{Store: st, BaseURL: "https://bam.example/", Now: func() time.Time { return now }}
	on := store.CommentTarget{On: models.CommentOnAsset, ID: drill.Asset.ID}
	c, err := as.AddComment(on, "Can "+markdown.Mention(bob.ID)+" look? Not "+markdown.Mention(wood.ID)+" nor "+markdown.Mention(quiet.ID)+".")
	if err != nil {
		t.Fatal(err)
	}

	// Without email set up, nothing is sent, nor passed over.
	if n, err := w.SendMentions(ctx); err != nil || n != 0 {
		t.Fatalf("without email: %d %v", n, err)
	}
	w.Mailer = mailer
	if n, err := w.SendMentions(ctx); err != nil || n != 1 {
		t.Fatalf("sent %d: %v", n, err)
	}
	m := mailer.sent[0]
	if m.To[0] != "bob@hackspace.org" || m.Subject != "Ada mentioned you on Drill" {
		t.Errorf("the email: %v %q", m.To, m.Subject)
	}
	if !strings.Contains(m.Text, "Can @Bob look? Not @Wood nor @Quiet.") || !strings.Contains(m.Text, "https://bam.example/assets/"+drill.Asset.ID+"#comment-"+c.ID) {
		t.Errorf("the email's text: %s", m.Text)
	}
	if n, _ := w.SendMentions(ctx); n != 0 {
		t.Errorf("sent again: %d", n)
	}

	// An edit tells only whoever it mentions first, and a failed email is
	// tried again.
	if _, err := as.EditComment(c.ID, markdown.Mention(bob.ID)+" and "+markdown.Mention(cy.ID)+", please."); err != nil {
		t.Fatal(err)
	}
	mailer.err = errors.New("the server is down")
	if n, _ := w.SendMentions(ctx); n != 0 {
		t.Errorf("sent while failing: %d", n)
	}
	mailer.err = nil
	if n, _ := w.SendMentions(ctx); n != 1 || mailer.subjects()[1] != "Ada mentioned you on Drill" || mailer.sent[1].To[0] != "cy@hackspace.org" {
		t.Errorf("after the edit: %d %v", n, mailer.subjects())
	}

	// Someone who reads it but not who did what is not told who wrote it.
	saw, _ := st.CreateAsset("Saw", []string{saws.ID}, nil, nil)
	if _, err := as.AddComment(store.CommentTarget{On: models.CommentOnAsset, ID: saw.Asset.ID}, "Thanks "+markdown.Mention(wood.ID)+" and "+markdown.Mention(bob.ID)); err != nil {
		t.Fatal(err)
	}
	if err := st.SetUserRoleAssignments(wood.ID, nil); err != nil {
		t.Fatal(err)
	}
	readers, _ := st.CreateRole(store.RoleInput{Name: "Readers", Permissions: []string{"assets.read"}})
	if err := st.SetUserRoleAssignments(wood.ID, []models.RoleAssignment{{RoleID: readers.ID, ScopeIDs: []string{woodshop.ID}}}); err != nil {
		t.Fatal(err)
	}
	if n, _ := w.SendMentions(ctx); n != 2 {
		t.Fatalf("on the saw: %d", n)
	}
	for _, m := range mailer.sent[2:] {
		switch m.To[0] {
		case "wood@hackspace.org":
			if m.Subject != "Someone mentioned you on Saw" || !strings.Contains(m.Text, "Thanks @someone and @someone") {
				t.Errorf("to Wood: %q %s", m.Subject, m.Text)
			}
		case "bob@hackspace.org":
			if m.Subject != "Ada mentioned you on Saw" || !strings.Contains(m.Text, "Thanks @Wood and @Bob") {
				t.Errorf("to Bob: %q %s", m.Subject, m.Text)
			}
		default:
			t.Errorf("emailed %v", m.To)
		}
	}

	// A deleted comment's mentions, and old ones, are passed over.
	d, _ := as.AddComment(on, "Hey "+markdown.Mention(bob.ID))
	if _, err := as.DeleteComment(d.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := as.AddComment(on, "Later, "+markdown.Mention(cy.ID)); err != nil {
		t.Fatal(err)
	}
	now = now.Add(48 * time.Hour)
	if n, _ := w.SendMentions(ctx); n != 0 || len(mailer.sent) != 4 {
		t.Errorf("deleted or old: %d, %v", n, mailer.subjects())
	}
}

// TestMentionsReadWithNamesInChannels: a comment's event, as channels
// tell it, names whoever it mentions, as they were named then.
func TestMentionsReadWithNamesInChannels(t *testing.T) {
	asset := "a1"
	bob, cy := "0b0b0b0b-0000-4000-8000-00000000000b", "0c0c0c0c-0000-4000-8000-00000000000c"
	n := Notification{BaseURL: "https://bam.example", Event: models.Event{
		Kind:    models.EventCommentAdded,
		AssetID: &asset,
		Payload: models.EventPayload{
			Asset: &models.EventAsset{ID: "a1", Name: "Drill"},
			Comment: &models.EventComment{ID: "c1", On: models.CommentOnAsset, Text: "Ask " + markdown.Mention(bob) + " or " + markdown.Mention(cy),
				Mentions: []models.EventMention{{ID: bob, Name: "Bob"}}},
		},
	}}
	if e := discordPayload(n).Embeds[0]; e.Description != "Ask @Bob or @someone" {
		t.Errorf("Discord: %q", e.Description)
	}
}
