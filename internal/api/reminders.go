package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Loan reminders (docs/stock-and-lending.md#loan-reminders): the instance's
// settings, and each borrower's choice.

// loanReminderSettings are the instance's settings, with whether email can
// carry them.
type loanReminderSettings struct {
	store.LoanReminderSettings
	CanEmail bool `json:"can_email"`
}

// handleLoanReminderSettings reads them: GET /api/v1/settings/loan-reminders
// (settings.manage).
func (s *Server) handleLoanReminderSettings(w http.ResponseWriter, r *http.Request) {
	set, err := s.store.LoanReminders()
	if err != nil {
		writeServerError(w, err, "read the loan reminder settings")
		return
	}
	writeJSON(w, http.StatusOK, loanReminderSettings{LoanReminderSettings: set, CanEmail: s.canEmailLinks()})
}

// handleSetLoanReminderSettings saves them: PUT
// /api/v1/settings/loan-reminders (settings.manage). Reminders cannot be
// turned on where email cannot carry their links.
func (s *Server) handleSetLoanReminderSettings(w http.ResponseWriter, r *http.Request) {
	var req store.LoanReminderSettings
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.On && !s.canEmailLinks() {
		noEmailForLinks(w)
		return
	}
	if err := s.store.SetLoanReminders(req); errors.Is(err, store.ErrInvalidSetting) {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	} else if err != nil {
		writeServerError(w, err, "save the loan reminder settings")
		return
	}
	s.handleLoanReminderSettings(w, r)
}

// LoanRemindersRequest turns one's own loan reminders on or off.
type LoanRemindersRequest struct {
	Reminders *bool `json:"reminders"`
}

// handleSetOwnLoanReminders sets whether the signed-in person is reminded
// of their loans: PUT /api/v1/auth/loan-reminders with {"reminders"}.
func (s *Server) handleSetOwnLoanReminders(w http.ResponseWriter, r *http.Request) {
	var req LoanRemindersRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Reminders == nil {
		writeJSONError(w, http.StatusBadRequest, "reminders is required: true or false")
		return
	}
	user, err := s.store.SetUserLoanReminders(CurrentUser(r.Context()).ID, *req.Reminders)
	switch {
	case errors.Is(err, store.ErrUserNotFound):
		writeJSONError(w, http.StatusNotFound, "User not found")
	case err != nil:
		writeStoreError(w, err, "set loan reminders")
	default:
		writeJSON(w, http.StatusOK, map[string]any{"user": user})
	}
}
