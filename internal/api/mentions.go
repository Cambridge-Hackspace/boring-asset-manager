package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/markdown"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Mentions (docs/comments.md#mentions): a comment mentions a person with
// <@id>, their user ID, so that it follows them when they are renamed. It
// reads as their name now to those who may see who, assets.track in the
// scopes of what it is on, as a comment's author does, and as "someone" to
// others. The people a comment can mention are offered only to those who
// may both comment there and see who, and only those who may read what it
// is on are offered, and told.

// namesOf gives the names, now, of the people with these IDs, reading
// those not yet read; an ID naming no one has no name.
func (v *commentViewer) namesOf(ids []string) map[string]string {
	var missing []string
	for _, id := range ids {
		if _, ok := v.names[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		// Names that cannot be read show as someone.
		got, _ := v.store.MentionNames(missing)
		for _, id := range missing {
			v.names[id] = got[id]
		}
	}
	out := map[string]string{}
	for _, id := range ids {
		if n := v.names[id]; n != "" {
			out[id] = n
		}
	}
	return out
}

// render renders a comment's text, naming those it mentions to a viewer
// who sees who, and lists them.
func (v *commentViewer) render(c *models.Comment, seesWho bool) {
	if !seesWho {
		c.HTML = markdown.Render(c.Body)
		return
	}
	ids := markdown.Mentions(c.Body)
	names := v.namesOf(ids)
	for _, id := range ids {
		if n, ok := names[id]; ok {
			c.Mentions = append(c.Mentions, models.CommentMention{ID: id, Name: n})
		}
	}
	c.HTML = markdown.RenderWith(c.Body, func(id string) string { return names[id] })
}

// handleCommentPeople lists the people a comment on something may
// mention: the active people who may read it, by name, but the requester.
// It takes comments.write and assets.track in its scopes, so that only
// those who already see the people around it learn who they are:
// GET /api/v1/{assets,categories,loans}/{id}/comments/people,
// GET /api/v1/assets/{id}/filings/{flow_id}/comments/people
func (s *Server) handleCommentPeople(on string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.commentTarget(w, r, on)
		if !ok {
			return
		}
		ctx := r.Context()
		if !Allows(ctx, rbac.PermCommentsWrite, p.scopes) || !Allows(ctx, rbac.PermAssetsTrack, p.scopes) {
			writeJSONError(w, http.StatusForbidden, "Forbidden: mentioning people takes comments.write and assets.track in these scopes")
			return
		}
		people, err := s.store.PeopleWhoRead(on, p.scopes)
		if err != nil {
			writeServerError(w, err, "list the people who may read it")
			return
		}
		me := actorKey(r)
		out := make([]models.CommentMention, 0, len(people))
		for _, person := range people {
			if person.ID != me {
				out = append(out, person)
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"people": out})
	}
}

// MentionEmailsRequest turns one's own mention emails on or off.
type MentionEmailsRequest struct {
	Mentions *bool `json:"mentions"`
}

// handleSetOwnMentionEmails sets whether the signed-in person is emailed
// when a comment mentions them: PUT /api/v1/auth/mention-emails with
// {"mentions"}.
func (s *Server) handleSetOwnMentionEmails(w http.ResponseWriter, r *http.Request) {
	var req MentionEmailsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Mentions == nil {
		writeJSONError(w, http.StatusBadRequest, "mentions is required: true or false")
		return
	}
	user, err := s.store.SetUserMentionEmails(CurrentUser(r.Context()).ID, *req.Mentions)
	switch {
	case errors.Is(err, store.ErrUserNotFound):
		writeJSONError(w, http.StatusNotFound, "User not found")
	case err != nil:
		writeStoreError(w, err, "set mention emails")
	default:
		writeJSON(w, http.StatusOK, map[string]any{"user": user})
	}
}
