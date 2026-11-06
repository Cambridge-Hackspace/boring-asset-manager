package notify

import (
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// TestCommentAddedMessages: a comment's event says what it is on and what
// it says, escaped for Discord, and links to the comment itself, on
// Discord, by email and over MQTT and webhooks.
func TestCommentAddedMessages(t *testing.T) {
	asset := "a1"
	n := Notification{BaseURL: "https://bam.example", Event: models.Event{
		Kind:    models.EventCommentAdded,
		AssetID: &asset,
		Payload: models.EventPayload{
			Asset:   &models.EventAsset{ID: "a1", Name: "Drill"},
			Actor:   &models.EventActor{ID: "u1", Email: "ada@hackspace.org", Name: "Ada"},
			Comment: &models.EventComment{ID: "c1", On: models.CommentOnAsset, Text: "The **left** motor_whines"},
		},
	}}
	e := discordPayload(n).Embeds[0]
	if e.Title != "Comment on Drill" || e.Description != "The \\*\\*left\\*\\* motor\\_whines\nBy Ada." || e.URL != "https://bam.example/assets/a1#comment-c1" {
		t.Errorf("Discord: %+v", e)
	}
	m, err := emailMessage(n)
	if err != nil {
		t.Fatal(err)
	}
	if m.Subject != "Comment on Drill" || !strings.Contains(m.Text, "The **left** motor_whines") || strings.Contains(m.Text, "ada@hackspace.org") {
		t.Errorf("email: %+v", m)
	}
	if out := outbound(n); out.URL != "https://bam.example/assets/a1#comment-c1" || out.Payload.Comment.Text == "" || out.Payload.Actor.Email != "" {
		t.Errorf("outbound: %+v", out)
	}

	// A loan's and a filing's say so.
	n.Event.Payload.Comment = &models.EventComment{ID: "c2", On: models.CommentOnLoan, LoanID: "l1", Text: "x"}
	if e := discordPayload(n).Embeds[0]; e.Title != "Comment on the loan of Drill" {
		t.Errorf("a loan's: %+v", e)
	}
	n.Event.Payload.Comment = &models.EventComment{ID: "c3", On: models.CommentOnFiling, Flow: &models.EventFlow{Slug: "leave", Name: "Leave it"}, Text: "x"}
	if e := discordPayload(n).Embeds[0]; e.Title != "Comment on Drill, as filed by Leave it" {
		t.Errorf("a filing's: %+v", e)
	}
	found := false
	for _, k := range EventKinds {
		found = found || k.Kind == models.EventCommentAdded
	}
	if !found || models.HistoryOnly(models.EventCommentAdded) || !models.HistoryOnly(models.EventCommentEdited) {
		t.Error("comment.added is not one channels can subscribe to")
	}
}
