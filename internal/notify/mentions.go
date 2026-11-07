package notify

import (
	"context"
	"errors"
	"log"
	"net/url"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/email"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/markdown"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Mentions (docs/comments.md#mentions) go out with the worker's turn, by
// email, to each person a comment mentions, once: each is claimed in the
// database before it is sent, so servers sharing it send each once, and
// given back if sending fails, to be tried again until GiveUpAfter has
// passed. Someone who cannot read what the comment is on, who has turned
// mentions off, or who has left, is passed over; so is a mention in a
// comment since deleted.

// SendMentions emails the people mentioned since the last turn, when
// email is set up, answering how many it emailed.
func (w *Worker) SendMentions(ctx context.Context) (int, error) {
	w.defaults()
	if w.Mailer == nil {
		return 0, nil
	}
	now := w.Now()
	due, err := w.Store.DueMentions(now.Add(-w.GiveUpAfter))
	if err != nil {
		return 0, err
	}
	sent := 0
	for _, m := range due {
		claimed, err := w.Store.ClaimMention(m.CommentID, m.Person.ID, now)
		if err != nil {
			return sent, err
		}
		if !claimed {
			continue
		}
		msg, ok, err := w.mentionMessage(m)
		if err == nil && ok {
			sctx, cancel := context.WithTimeout(ctx, w.SendTimeout)
			err = w.Mailer.Send(sctx, msg)
			cancel()
		}
		if err != nil {
			log.Printf("mentions: emailing %s about comment %s: %v", m.Person.Email, m.CommentID, err)
			if rerr := w.Store.ReleaseMention(m.CommentID, m.Person.ID); rerr != nil {
				return sent, rerr
			}
			continue
		}
		if ok {
			sent++
		}
	}
	return sent, nil
}

// mentionMessage is the email telling someone a comment mentions them,
// answering false when they are not to be told.
func (w *Worker) mentionMessage(m store.PendingMention) (email.Message, bool, error) {
	if !m.Emailed {
		return email.Message{}, false, nil
	}
	c, err := w.Store.GetComment(m.CommentID)
	if errors.Is(err, store.ErrCommentNotFound) {
		return email.Message{}, false, nil
	}
	if err != nil {
		return email.Message{}, false, err
	}
	if c.DeletedAt != nil {
		return email.Message{}, false, nil
	}
	scopes, err := w.Store.CommentScopes(c)
	if err != nil {
		return email.Message{}, false, err
	}
	reader, err := w.Store.CommentReader(m.Person.ID, c.On, scopes)
	if err != nil {
		return email.Message{}, false, err
	}
	if !reader.Reads {
		return email.Message{}, false, nil
	}
	place, err := w.Store.CommentPlaceName(c)
	if err != nil {
		return email.Message{}, false, err
	}
	var names map[string]string
	if reader.SeesWho {
		if names, err = w.Store.MentionNames(markdown.Mentions(c.Body)); err != nil {
			return email.Message{}, false, err
		}
	}
	msg, err := mentionEmail(m.Person.Email, c, place, reader.SeesWho, names, strings.TrimRight(w.BaseURL, "/"))
	return msg, err == nil, err
}

// mentionEmail lays out the email telling someone that comment c, on
// place, mentions them: who wrote it, when they may see who, and what it
// says, with the people it mentions named by names, or as someone.
func mentionEmail(to string, c *models.Comment, place string, seesWho bool, names map[string]string, base string) (email.Message, error) {
	who := "Someone"
	if seesWho && strings.TrimSpace(c.AuthorName) != "" {
		who = c.AuthorName
	}
	subject := truncate(who+" mentioned you on "+place, 200)
	text := truncate(markdown.PlainMentions(c.Body, func(id string) string { return names[id] }), 1000)
	var paragraphs []string
	for _, para := range strings.Split(text, "\n") {
		if para = strings.TrimSpace(para); para != "" {
			paragraphs = append(paragraphs, para)
		}
	}
	link := ""
	if base != "" {
		if c.On == models.CommentOnCategory {
			link = base + "/categories/" + url.PathEscape(c.OnID) + "?tab=comments#comment-" + url.PathEscape(c.ID)
		} else {
			link = base + "/assets/" + url.PathEscape(c.AssetID) + "#comment-" + url.PathEscape(c.ID)
		}
	}
	footer := "Sent to " + to + " because a comment mentioned you."
	if base != "" {
		footer += " Turn these emails off under Your account: " + base + "/account"
	}
	html, plain, err := email.Render(email.Content{Title: subject, Paragraphs: paragraphs, Link: link, LinkLabel: "See the comment", Accent: "#2563eb", Footer: footer})
	if err != nil {
		return email.Message{}, err
	}
	return email.Message{To: []string{to}, Subject: subject, HTML: html, Text: plain}, nil
}
