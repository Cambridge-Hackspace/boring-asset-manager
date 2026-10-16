package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// CheckOutRequest lends an asset. BorrowerID defaults to whoever is signed
// in; naming someone else needs assets.lend. DueAt is optional.
type CheckOutRequest struct {
	BorrowerID string     `json:"borrower_id"`
	DueAt      *time.Time `json:"due_at"`
}

// handleCheckOut lends an asset: POST /api/v1/assets/{id}/checkout
func (s *Server) handleCheckOut(w http.ResponseWriter, r *http.Request) {
	var req CheckOutRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "Invalid request body (due_at, if given, is an RFC 3339 time)")
			return
		}
	}
	self := ""
	if u := CurrentUser(r.Context()); u != nil {
		self = u.ID
	}
	if req.BorrowerID == "" {
		if self == "" {
			writeJSONError(w, http.StatusBadRequest, "borrower_id is required: an API token cannot borrow")
			return
		}
		req.BorrowerID = self
	}
	a, ok := s.assetFor(w, r, r.PathValue("id"), rbac.PermAssetsBorrow, rbac.PermAssetsLend)
	if !ok {
		return
	}
	if req.BorrowerID != self && !Allows(r.Context(), rbac.PermAssetsLend, a.Scopes) {
		writeJSONError(w, http.StatusForbidden, "Lending to someone else needs assets.lend")
		return
	}
	asset, err := s.actingStore(r).CheckOut(r.PathValue("id"), req.BorrowerID, req.DueAt)
	if err != nil {
		writeLoanError(w, err, "lend the asset")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"asset": s.shown(r, asset)})
}

// CheckInRequest ends a loan. Left, for an asset counted by quantity, is
// how much came back.
type CheckInRequest struct {
	Left *int `json:"left"`
}

// handleCheckIn ends an asset's loan: POST /api/v1/assets/{id}/checkin.
// Without assets.lend, only the borrower's own loan can be checked in.
func (s *Server) handleCheckIn(w http.ResponseWriter, r *http.Request) {
	var req CheckInRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "Invalid request body (left, if given, is a whole number)")
			return
		}
	}
	a, ok := s.assetFor(w, r, r.PathValue("id"), rbac.PermAssetsBorrow, rbac.PermAssetsLend)
	if !ok {
		return
	}
	onlyBorrower := ""
	if !Allows(r.Context(), rbac.PermAssetsLend, a.Scopes) {
		u := CurrentUser(r.Context())
		if u == nil {
			writeJSONError(w, http.StatusForbidden, "Checking in needs assets.lend")
			return
		}
		onlyBorrower = u.ID
	}
	asset, err := s.actingStore(r).CheckIn(r.PathValue("id"), onlyBorrower, req.Left)
	if err != nil {
		writeLoanError(w, err, "check the asset in")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"asset": s.shown(r, asset)})
}

// handleListLoans lists loans, newest first:
// GET /api/v1/loans[?borrower_id=...][&state=open|overdue]
func (s *Server) handleListLoans(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	loans, err := s.store.ListLoans(store.LoanFilter{BorrowerID: q.Get("borrower_id"), State: q.Get("state")})
	if errors.Is(err, store.ErrInvalidLoanFilter) {
		writeJSONError(w, http.StatusBadRequest, "state must be open or overdue")
		return
	}
	if err != nil {
		writeServerError(w, err, "list loans")
		return
	}
	// Only loans of assets the requester tracks, with names but no emails.
	ids := make([]string, len(loans))
	for i, l := range loans {
		ids[i] = l.AssetID
	}
	scopes, err := s.store.AssetScopes(ids)
	if err != nil {
		writeServerError(w, err, "read the assets' scopes")
		return
	}
	kept := loans[:0:0]
	for _, l := range loans {
		if Allows(r.Context(), rbac.PermAssetsTrack, scopes[l.AssetID]) {
			kept = append(kept, redactLoan(r, l, true))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"loans": kept})
}

// handleListPeople lists who an asset can be lent to: GET /api/v1/people
func (s *Server) handleListPeople(w http.ResponseWriter, r *http.Request) {
	people, err := s.store.ListPeople()
	if err != nil {
		writeServerError(w, err, "list people")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"people": people})
}

func writeLoanError(w http.ResponseWriter, err error, action string) {
	switch {
	case errors.Is(err, store.ErrAssetNotFound):
		writeJSONError(w, http.StatusNotFound, "Asset not found")
	case errors.Is(err, store.ErrNotYourLoan):
		writeJSONError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, store.ErrNotLendable), errors.Is(err, store.ErrLentByQuantity), errors.Is(err, store.ErrOutOfService),
		errors.Is(err, store.ErrAlreadyLent), errors.Is(err, store.ErrNotLent), errors.Is(err, store.ErrAssetArchived):
		writeJSONError(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrBorrowerNotFound), errors.Is(err, store.ErrDueInPast), errors.Is(err, store.ErrNotStocked), errors.Is(err, store.ErrQuantityOutOfRange):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	default:
		writeStoreError(w, err, action)
	}
}
