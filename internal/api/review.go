package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Review states (docs/review.md): moving assets between draft, confirmed
// and verified, deciding on changes an assistant proposed, categories'
// settings and the instance's.

// ReviewRequest moves an asset's review state: move is confirm, verify or
// return, and note says why, which returning needs.
type ReviewRequest struct {
	Move string `json:"move"`
	Note string `json:"note"`
}

// reviewPermission is the permission a move needs in the asset's scopes:
// assets.confirm to confirm, and assets.verify to verify or return it to
// draft.
func reviewPermission(move string) string {
	if move == models.ReviewMoveConfirm {
		return rbac.PermAssetsConfirm
	}
	return rbac.PermAssetsVerify
}

// writeReviewError answers an error from a review move or a decision on a
// proposal, or reports false when there was none.
func writeReviewError(w http.ResponseWriter, err error, action string) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, store.ErrAssetNotFound):
		writeJSONError(w, http.StatusNotFound, "Asset not found")
	case errors.Is(err, store.ErrReviewByAssistant), errors.Is(err, store.ErrReviewOwnConfirmation):
		writeJSONError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, store.ErrReviewOff), errors.Is(err, store.ErrReviewMove), errors.Is(err, store.ErrProposalPending),
		errors.Is(err, store.ErrNoProposal), errors.Is(err, store.ErrProposalNotUndoable), errors.Is(err, store.ErrAssetArchived),
		errors.Is(err, store.ErrGeneratorCodeExists), errors.Is(err, store.ErrAssetOnLoan), errors.Is(err, store.ErrContainerNotEmpty):
		writeJSONError(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrReviewNoteRequired), errors.Is(err, store.ErrInvalidSetting):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	default:
		writeStoreError(w, err, action)
	}
	return true
}

// handleReviewAsset: POST /api/v1/assets/{id}/review confirms, verifies or
// returns an asset to draft. Confirming needs assets.confirm in its scopes;
// verifying and returning, assets.verify. Verifying what one confirmed
// oneself is refused (403) unless its categories allow it.
func (s *Server) handleReviewAsset(w http.ResponseWriter, r *http.Request) {
	var req ReviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	switch req.Move {
	case models.ReviewMoveConfirm, models.ReviewMoveVerify, models.ReviewMoveReturn:
	default:
		writeJSONError(w, http.StatusBadRequest, "move must be confirm, verify or return")
		return
	}
	id := r.PathValue("id")
	if _, ok := s.assetFor(w, r, id, reviewPermission(req.Move)); !ok {
		return
	}
	asset, err := s.actingStore(r).ReviewAsset(id, req.Move, req.Note)
	if writeReviewError(w, err, "move the asset's review") {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"asset": s.shown(r, asset)})
}

// AcceptProposalRequest accepts an assistant's changes, confirming the
// asset too when confirm is set.
type AcceptProposalRequest struct {
	Confirm bool `json:"confirm"`
}

// handleAcceptProposal: POST /api/v1/assets/{id}/proposal/accept takes the
// changes an assistant made as the accepter's own (docs/review.md#assistants).
// It needs assets.write in the asset's scopes, and assets.confirm too to
// confirm it.
func (s *Server) handleAcceptProposal(w http.ResponseWriter, r *http.Request) {
	var req AcceptProposalRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "Invalid request body")
			return
		}
	}
	id := r.PathValue("id")
	a, ok := s.assetFor(w, r, id, rbac.PermAssetsWrite)
	if !ok {
		return
	}
	if req.Confirm && !Allows(r.Context(), rbac.PermAssetsConfirm, a.Scopes) {
		writeJSONError(w, http.StatusForbidden, "Confirming it needs assets.confirm")
		return
	}
	asset, err := s.actingStore(r).AcceptProposal(id, req.Confirm)
	if writeReviewError(w, err, "accept the changes") {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"asset": s.shown(r, asset)})
}

// RejectProposalRequest rejects an assistant's changes, saying why.
type RejectProposalRequest struct {
	Note string `json:"note"`
}

// handleRejectProposal: POST /api/v1/assets/{id}/proposal/reject undoes
// the changes an assistant made: an asset it made is archived, which needs
// assets.delete, and any other is put back as it was, which needs
// assets.write, in its scopes.
func (s *Server) handleRejectProposal(w http.ResponseWriter, r *http.Request) {
	var req RejectProposalRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "Invalid request body")
			return
		}
	}
	id := r.PathValue("id")
	a, ok := s.assetFor(w, r, id, rbac.PermAssetsWrite)
	if !ok {
		return
	}
	if a.Review != nil && a.Review.Proposal != nil && a.Review.Proposal.New && !Allows(r.Context(), rbac.PermAssetsDelete, a.Scopes) {
		writeJSONError(w, http.StatusForbidden, "Rejecting an asset an assistant made archives it, which needs assets.delete")
		return
	}
	asset, err := s.actingStore(r).RejectProposal(id, req.Note)
	if writeReviewError(w, err, "reject the changes") {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"asset": s.shown(r, asset)})
}

// handleSetCategoryReview: PUT /api/v1/categories/{id}/review replaces a
// category's own review settings (categories.manage); each null takes its
// parent's.
func (s *Server) handleSetCategoryReview(w http.ResponseWriter, r *http.Request) {
	var req models.CategoryReview
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	cat, err := s.actingStore(r).SetCategoryReview(r.PathValue("id"), req)
	switch {
	case errors.Is(err, store.ErrCategoryNotFound):
		writeJSONError(w, http.StatusNotFound, "Category not found")
		return
	case errors.Is(err, store.ErrInvalidSetting):
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		writeStoreError(w, err, "save the review settings")
		return
	}
	if err := s.showCategory(r, cat); err != nil {
		writeServerError(w, err, "save the review settings")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"category": cat})
}

// handleReviewSettings: GET /api/v1/settings/review (settings.manage).
func (s *Server) handleReviewSettings(w http.ResponseWriter, r *http.Request) {
	set, err := s.store.ReviewSettings()
	if err != nil {
		writeServerError(w, err, "read the review settings")
		return
	}
	writeJSON(w, http.StatusOK, set)
}

// handleSetReviewSettings: PUT /api/v1/settings/review (settings.manage).
func (s *Server) handleSetReviewSettings(w http.ResponseWriter, r *http.Request) {
	var req store.ReviewSettings
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.store.SetReviewSettings(req); errors.Is(err, store.ErrInvalidSetting) {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	} else if err != nil {
		writeServerError(w, err, "save the review settings")
		return
	}
	s.handleReviewSettings(w, r)
}

// ReviewSummary counts what waits for the requester to review: drafts and
// reviews due or overdue among the assets they may confirm, changes from
// assistants among those they may edit, and confirmed assets among those
// they may verify.
type ReviewSummary struct {
	Drafts    int `json:"drafts"`
	Due       int `json:"due"`
	Overdue   int `json:"overdue"`
	Proposals int `json:"proposals"`
	ToVerify  int `json:"to_verify"`
}

// handleReviewSummary: GET /api/v1/review, for the home page.
func (s *Server) handleReviewSummary(w http.ResponseWriter, r *http.Request) {
	assets, err := s.store.FilterAssets(store.AssetFilter{})
	if err != nil {
		writeServerError(w, err, "list assets")
		return
	}
	var out ReviewSummary
	ctx := r.Context()
	for _, a := range assets {
		rv := a.Review
		if rv == nil {
			continue
		}
		if Allows(ctx, rbac.PermAssetsConfirm, a.Scopes) {
			if rv.State == models.ReviewDraft && rv.Proposal == nil {
				out.Drafts++
			}
			if rv.Overdue {
				out.Overdue++
			} else if rv.Due {
				out.Due++
			}
		}
		if rv.Proposal != nil && Allows(ctx, rbac.PermAssetsWrite, a.Scopes) {
			out.Proposals++
		}
		if rv.State == models.ReviewConfirmed && Allows(ctx, rbac.PermAssetsVerify, a.Scopes) {
			out.ToVerify++
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// redactReview leaves out of an asset's review people's emails, and the
// people themselves, but for the requester, for those who do not track it.
func redactReview(a *models.AssetWithDetails, track bool, self string) {
	if a.Review == nil {
		return
	}
	rv := *a.Review
	hide := func(p *models.Person) *models.Person {
		if p == nil || (!track && (self == "" || p.ID != self)) {
			return nil
		}
		c := *p
		c.Email = ""
		return &c
	}
	rv.SetBy, rv.ConfirmedBy = hide(rv.SetBy), hide(rv.ConfirmedBy)
	if rv.Proposal != nil {
		p := *rv.Proposal
		p.By = hide(p.By)
		rv.Proposal = &p
	}
	a.Review = &rv
}
