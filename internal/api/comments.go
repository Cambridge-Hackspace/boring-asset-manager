package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/markdown"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Comments (docs/comments.md): notes in Markdown on assets, categories,
// loans and flows' filings. Reading them follows reading what they are on:
// an asset's, a loan's and a filing's go with the asset and its scopes, and
// a category's with the catalog. comments.write writes them, in the scopes
// of what they are on, and lets their authors edit and delete their own;
// comments.moderate deletes anyone's. Who wrote and deleted each shows to
// those who may see who, as history does. Their Markdown is rendered here,
// through an allowlist sanitizer.

// CommentRequest is a comment's text, in Markdown.
type CommentRequest struct {
	Body string `json:"body"`
}

// commentPlace is what a request's comments are on, once the requester is
// known to read it: the target, and the scopes that decide who may write
// there.
type commentPlace struct {
	target store.CommentTarget
	scopes []string
}

// actorKey is who the request acts as, as comments name their authors: the
// user, or else the API token.
func actorKey(r *http.Request) string {
	if u := CurrentUser(r.Context()); u != nil {
		return u.ID
	}
	if t := CurrentToken(r.Context()); t != nil {
		return t.ID
	}
	return ""
}

// commentTarget reads what a request's comments are on, from its route,
// answering 404 when the requester may not read it.
func (s *Server) commentTarget(w http.ResponseWriter, r *http.Request, on string) (*commentPlace, bool) {
	id := r.PathValue("id")
	switch on {
	case models.CommentOnCategory:
		if _, err := s.store.GetCategoryByID(id); err != nil {
			if errors.Is(err, store.ErrCategoryNotFound) {
				writeJSONError(w, http.StatusNotFound, "Category not found")
			} else {
				writeServerError(w, err, "read the category")
			}
			return nil, false
		}
		scopes, err := s.store.ScopesForCategories([]string{id})
		if err != nil {
			writeServerError(w, err, "read the category's scopes")
			return nil, false
		}
		return &commentPlace{target: store.CommentTarget{On: on, ID: id}, scopes: scopes}, true
	case models.CommentOnLoan:
		asset, err := s.store.LoanAsset(id)
		if errors.Is(err, store.ErrLoanNotFound) {
			writeJSONError(w, http.StatusNotFound, "Loan not found")
			return nil, false
		}
		if err != nil {
			writeServerError(w, err, "read the loan")
			return nil, false
		}
		a, ok := s.assetFor(w, r, asset)
		if !ok {
			return nil, false
		}
		return &commentPlace{target: store.CommentTarget{On: on, ID: id}, scopes: a.Scopes}, true
	}
	a, ok := s.assetFor(w, r, id)
	if !ok {
		return nil, false
	}
	t := store.CommentTarget{On: on, ID: a.Asset.ID}
	if on == models.CommentOnFiling {
		t.FlowID = r.PathValue("flow_id")
	}
	return &commentPlace{target: t, scopes: a.Scopes}, true
}

// commentViewer decides what a requester sees of comments, and may do with
// them, by the scopes of what each is on.
type commentViewer struct {
	r      *http.Request
	me     string
	scopes func(c *models.Comment) []string
}

// commentViewer reads what the viewer needs: the scopes of categories, for
// those of category comments, and of assets, for the rest. known are
// scopes already read, by asset ID, or by "category:" and its ID.
func (s *Server) commentViewer(r *http.Request, known map[string][]string) (*commentViewer, error) {
	resolve, err := s.store.ScopeResolver()
	if err != nil {
		return nil, err
	}
	cache := map[string][]string{}
	for k, v := range known {
		cache[k] = v
	}
	v := &commentViewer{r: r, me: actorKey(r)}
	v.scopes = func(c *models.Comment) []string {
		key := c.AssetID
		if c.On == models.CommentOnCategory {
			key = "category:" + c.OnID
		}
		if sc, ok := cache[key]; ok {
			return sc
		}
		var sc []string
		if c.On == models.CommentOnCategory {
			sc = resolve([]string{c.OnID})
		} else {
			// An asset whose scopes cannot be read is not shown.
			if got, err := s.store.AssetScopes([]string{c.AssetID}); err == nil {
				sc = got[c.AssetID]
			}
		}
		cache[key] = sc
		return sc
	}
	return v, nil
}

// sees reports whether the viewer may read a comment: one on a category
// when they read the catalog, and the rest when they read the asset.
func (v *commentViewer) sees(c *models.Comment) bool {
	if c.On == models.CommentOnCategory {
		return HasPermission(v.r.Context(), rbac.PermAssetsRead)
	}
	return Allows(v.r.Context(), rbac.PermAssetsRead, v.scopes(c))
}

// shown is a comment as the viewer sees it: rendered, with who wrote and
// deleted it only to those who may see who, and what they may do with it.
func (v *commentViewer) shown(c models.Comment) models.Comment {
	ctx := v.r.Context()
	scopes := v.scopes(&c)
	c.Mine = v.me != "" && c.AuthorID == v.me
	if c.DeletedAt == nil {
		c.HTML = markdown.Render(c.Body)
		write := Allows(ctx, rbac.PermCommentsWrite, scopes)
		c.CanEdit = c.Mine && write
		c.CanDelete = c.Mine && write || Allows(ctx, rbac.PermCommentsModerate, scopes)
	}
	if Allows(ctx, rbac.PermAssetsTrack, scopes) {
		if c.AuthorID != "" && c.AuthorName == "" {
			c.AuthorName = UnnamedUser
		}
		if c.DeletedBy != "" && c.DeletedByName == "" {
			c.DeletedByName = UnnamedUser
		}
	} else {
		c.AuthorID, c.AuthorName, c.DeletedBy, c.DeletedByName = "", "", "", ""
	}
	return c
}

func writeCommentError(w http.ResponseWriter, err error, action string) {
	switch {
	case errors.Is(err, store.ErrCommentNotFound):
		writeJSONError(w, http.StatusNotFound, "Comment not found")
	case errors.Is(err, store.ErrAssetNotFound):
		writeJSONError(w, http.StatusNotFound, "Asset not found")
	case errors.Is(err, store.ErrCategoryNotFound):
		writeJSONError(w, http.StatusNotFound, "Category not found")
	case errors.Is(err, store.ErrLoanNotFound):
		writeJSONError(w, http.StatusNotFound, "Loan not found")
	case errors.Is(err, store.ErrFilingNotFound):
		writeJSONError(w, http.StatusNotFound, "The flow did not file that asset")
	case errors.Is(err, store.ErrCommentDeleted):
		writeJSONError(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrCommentEmpty), errors.Is(err, store.ErrCommentTooLong):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	default:
		writeServerError(w, err, action)
	}
}

// knownScopes are the scopes of what a place is on, keyed as
// commentViewer caches them.
func knownScopes(p *commentPlace) map[string][]string {
	key := p.target.ID
	switch p.target.On {
	case models.CommentOnCategory:
		key = "category:" + p.target.ID
	case models.CommentOnLoan:
		return nil
	}
	return map[string][]string{key: p.scopes}
}

// handleListComments lists the comments on something, oldest first. An
// asset's and a category's are followed by those of their categories and
// their ancestors, nearest first, to those who read the catalog, unless
// ?own=1 asks for its own only:
// GET /api/v1/{assets,categories,loans}/{id}/comments,
// GET /api/v1/assets/{id}/filings/{flow_id}/comments
func (s *Server) handleListComments(on string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.commentTarget(w, r, on)
		if !ok {
			return
		}
		ancestors := r.URL.Query().Get("own") != "1" && HasPermission(r.Context(), rbac.PermAssetsRead)
		list, err := s.store.ListComments(p.target, ancestors)
		if err != nil {
			writeCommentError(w, err, "list the comments")
			return
		}
		v, err := s.commentViewer(r, knownScopes(p))
		if err != nil {
			writeServerError(w, err, "read the scopes")
			return
		}
		out := make([]models.Comment, 0, len(list))
		for _, c := range list {
			if v.sees(&c) {
				out = append(out, v.shown(c))
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"comments": out})
	}
}

// handleAddComment writes a comment, in Markdown, on something the
// requester may comment on: POST /api/v1/{assets,categories,loans}/{id}/comments,
// POST /api/v1/assets/{id}/filings/{flow_id}/comments
func (s *Server) handleAddComment(on string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.commentTarget(w, r, on)
		if !ok {
			return
		}
		if !Allows(r.Context(), rbac.PermCommentsWrite, p.scopes) {
			writeJSONError(w, http.StatusForbidden, "Forbidden: you may not comment in these scopes")
			return
		}
		var req CommentRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		c, err := s.actingStore(r).AddComment(p.target, req.Body)
		if err != nil {
			writeCommentError(w, err, "add the comment")
			return
		}
		v, err := s.commentViewer(r, knownScopes(p))
		if err != nil {
			writeServerError(w, err, "read the scopes")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"comment": v.shown(*c)})
	}
}

// commentFor reads the comment a request names, answering 404 when the
// requester may not read it.
func (s *Server) commentFor(w http.ResponseWriter, r *http.Request) (*models.Comment, *commentViewer, bool) {
	c, err := s.store.GetComment(r.PathValue("id"))
	if err != nil {
		writeCommentError(w, err, "read the comment")
		return nil, nil, false
	}
	v, err := s.commentViewer(r, nil)
	if err != nil {
		writeServerError(w, err, "read the scopes")
		return nil, nil, false
	}
	if !v.sees(c) {
		writeJSONError(w, http.StatusNotFound, "Comment not found")
		return nil, nil, false
	}
	return c, v, true
}

// handleGetComment reads one comment: GET /api/v1/comments/{id}
func (s *Server) handleGetComment(w http.ResponseWriter, r *http.Request) {
	c, v, ok := s.commentFor(w, r)
	if ok {
		writeJSON(w, http.StatusOK, map[string]any{"comment": v.shown(*c)})
	}
}

// handleEditComment replaces the text of one's own comment, keeping what
// it said in history: PUT /api/v1/comments/{id}
func (s *Server) handleEditComment(w http.ResponseWriter, r *http.Request) {
	c, v, ok := s.commentFor(w, r)
	if !ok {
		return
	}
	if !v.shown(*c).CanEdit {
		if c.DeletedAt != nil {
			writeJSONError(w, http.StatusConflict, store.ErrCommentDeleted.Error())
			return
		}
		writeJSONError(w, http.StatusForbidden, "Forbidden: only its author may edit a comment, with comments.write")
		return
	}
	var req CommentRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	edited, err := s.actingStore(r).EditComment(c.ID, req.Body)
	if err != nil {
		writeCommentError(w, err, "edit the comment")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"comment": v.shown(*edited)})
}

// handleDeleteComment deletes one's own comment, or with comments.moderate
// anyone's, leaving "deleted by" in its place and its text in history:
// DELETE /api/v1/comments/{id}
func (s *Server) handleDeleteComment(w http.ResponseWriter, r *http.Request) {
	c, v, ok := s.commentFor(w, r)
	if !ok {
		return
	}
	if c.DeletedAt != nil {
		writeJSONError(w, http.StatusConflict, store.ErrCommentDeleted.Error())
		return
	}
	if !v.shown(*c).CanDelete {
		writeJSONError(w, http.StatusForbidden, "Forbidden: only its author, or someone with comments.moderate, may delete a comment")
		return
	}
	deleted, err := s.actingStore(r).DeleteComment(c.ID)
	if err != nil {
		writeCommentError(w, err, "delete the comment")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"comment": v.shown(*deleted)})
}

// handleCommentHistory lists a comment's events, oldest first: when it was
// written, each edit with what it said before, and its deletion. People
// show by name only to those who may see who:
// GET /api/v1/comments/{id}/history
func (s *Server) handleCommentHistory(w http.ResponseWriter, r *http.Request) {
	c, v, ok := s.commentFor(w, r)
	if !ok {
		return
	}
	events, err := s.store.CommentHistory(c.ID)
	if err != nil {
		writeCommentError(w, err, "read the comment's history")
		return
	}
	track := Allows(r.Context(), rbac.PermAssetsTrack, v.scopes(c))
	for i := range events {
		events[i] = redactEvent(events[i], track)
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

// handleSearchComments finds the comments whose text holds ?q=, newest
// first, among those the requester may read: GET /api/v1/comments?q=
func (s *Server) handleSearchComments(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSONError(w, http.StatusBadRequest, "q is required: the words to find")
		return
	}
	found, err := s.store.SearchComments(q)
	if err != nil {
		writeServerError(w, err, "search the comments")
		return
	}
	v, err := s.commentViewer(r, nil)
	if err != nil {
		writeServerError(w, err, "read the scopes")
		return
	}
	out := []models.Comment{}
	for _, c := range found {
		if v.sees(&c) {
			out = append(out, v.shown(c))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"comments": out})
}

// handleListFilings lists the flows that filed an asset, which comments can
// be on: GET /api/v1/assets/{id}/filings
func (s *Server) handleListFilings(w http.ResponseWriter, r *http.Request) {
	a, ok := s.assetFor(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	filings, err := s.store.ListFilings(a.Asset.ID)
	if err != nil {
		writeServerError(w, err, "list the asset's filings")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"filings": filings})
}

// handleCommentAttachment attaches a file to what a comment is being
// written on, as the comment editor does, with comments.write in its
// scopes; it answers the attachment and the Markdown that links to it:
// POST /api/v1/{assets,categories}/{id}/comments/attachments
func (s *Server) handleCommentAttachment(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		on := models.CommentOnAsset
		if kind == models.OwnerCategory {
			on = models.CommentOnCategory
		}
		p, ok := s.commentTarget(w, r, on)
		if !ok {
			return
		}
		if !Allows(r.Context(), rbac.PermCommentsWrite, p.scopes) {
			writeJSONError(w, http.StatusForbidden, "Forbidden: you may not comment in these scopes")
			return
		}
		o := &attachmentOwner{kind: kind, id: p.target.ID, who: Allows(r.Context(), rbac.PermAssetsTrack, p.scopes)}
		_, added, ok := s.addAttachment(w, r, o)
		if !ok {
			return
		}
		if added == nil {
			writeServerError(w, errors.New("the attachment added is missing"), "add the attachment")
			return
		}
		base := "/api/v1/assets/"
		if kind == models.OwnerCategory {
			base = "/api/v1/categories/"
		}
		url := base + p.target.ID + "/attachments/" + added.ID
		writeJSON(w, http.StatusCreated, map[string]any{
			"attachment": shownAttachments(o, []models.Attachment{*added})[0],
			"url":        url,
			"markdown":   AttachmentMarkdown(added.Name, url, added.Kind == models.AttachmentImage),
		})
	}
}

// AttachmentMarkdown is the Markdown that links to an attachment: an image
// shown, anything else as a link, named by its name.
func AttachmentMarkdown(name, url string, image bool) string {
	label := strings.NewReplacer(`\`, `\\`, "[", `\[`, "]", `\]`, "\n", " ").Replace(name)
	if image {
		return "![" + label + "](" + url + ")"
	}
	return "[" + label + "](" + url + ")"
}

// handleRenderMarkdown renders Markdown as comments are, for the comment
// editor's preview: POST /api/v1/markdown
func (s *Server) handleRenderMarkdown(w http.ResponseWriter, r *http.Request) {
	var req CommentRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if len([]rune(req.Body)) > store.MaxCommentLength {
		writeJSONError(w, http.StatusBadRequest, store.ErrCommentTooLong.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"html": markdown.Render(req.Body)})
}
