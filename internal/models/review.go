package models

import "time"

// Review states (docs/review.md): an asset in a category that uses them is
// recorded but not yet checked, confirmed by someone responsible for it, or
// verified by someone else.
const (
	ReviewDraft     = "draft"
	ReviewConfirmed = "confirmed"
	ReviewVerified  = "verified"
)

// ValidReviewState reports whether s is one of the review states.
func ValidReviewState(s string) bool {
	return s == ReviewDraft || s == ReviewConfirmed || s == ReviewVerified
}

// What editing a confirmed or verified asset does (CategoryReview.OnEdit):
// returns it to draft, or keeps its state.
const (
	ReviewOnEditDraft = "draft"
	ReviewOnEditKeep  = "keep"
)

// Review moves: confirming, verifying, and returning to draft.
const (
	ReviewMoveConfirm = "confirm"
	ReviewMoveVerify  = "verify"
	ReviewMoveReturn  = "return"
)

// CategoryReview is a category's own review settings. Each nil takes its
// parent's, as subcategories take a category's fields; at the top, review
// is off, asks for no review dates, returns edited assets to draft and
// does not let whoever confirmed an asset verify it.
type CategoryReview struct {
	// Enabled turns review states on for its assets and those below it,
	// or off below a category that has them on.
	Enabled *bool `json:"enabled"`
	// Months asks for review that many months after an asset is confirmed
	// or verified; 0 asks for none.
	Months *int `json:"months"`
	// OnEdit is ReviewOnEditDraft or ReviewOnEditKeep.
	OnEdit *string `json:"on_edit"`
	// SelfVerify lets whoever confirmed an asset verify it too.
	SelfVerify *bool `json:"self_verify"`
}

// ReviewRules are review settings in effect: a category's, with what it
// takes from above, or an asset's, from those of its categories that use
// review states.
type ReviewRules struct {
	Enabled    bool   `json:"enabled"`
	Months     int    `json:"months"`
	OnEdit     string `json:"on_edit"`
	SelfVerify bool   `json:"self_verify"`
}

// AssetReview is an asset's review: set only while one of its categories
// uses review states.
type AssetReview struct {
	// State is ReviewDraft, ReviewConfirmed or ReviewVerified.
	State string `json:"state"`
	// SetBy and SetAt are who put it in its state, and when; nil for an
	// asset that has never been moved, which is a draft.
	SetBy *Person    `json:"set_by"`
	SetAt *time.Time `json:"set_at"`
	// ConfirmedBy and ConfirmedAt are who last confirmed it, and when.
	ConfirmedBy *Person    `json:"confirmed_by"`
	ConfirmedAt *time.Time `json:"confirmed_at"`
	// ReviewedAt is when it was last confirmed or verified, and DueAt when
	// it is next to be reviewed: nil when its categories ask for no review,
	// or it is a draft. Due is set from the instance's days before DueAt,
	// and Overdue from DueAt.
	ReviewedAt *time.Time `json:"reviewed_at"`
	DueAt      *time.Time `json:"due_at"`
	Due        bool       `json:"due"`
	Overdue    bool       `json:"overdue"`
	// Rules are the settings of its categories that use review states.
	Rules ReviewRules `json:"rules"`
	// Proposal is changes an assistant made, waiting to be accepted or
	// rejected; nil when there are none.
	Proposal *Proposal `json:"proposal"`
}

// Proposal is changes made through an assistant that landed as a draft
// (docs/review.md#assistants), for someone to accept or reject. New marks
// an asset the assistant made, which rejecting archives; Undoable is false
// when what it was before could not be kept, and the changes can only be
// accepted.
type Proposal struct {
	New        bool      `json:"new"`
	Undoable   bool      `json:"undoable"`
	PriorState string    `json:"prior_state"`
	By         *Person   `json:"by"`
	Via        string    `json:"via"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// EventReview is a review move, as an asset's history records it: the
// state it left and the one it took, and what moved it, one of the Review…
// causes.
type EventReview struct {
	From  string `json:"from,omitempty"`
	To    string `json:"to"`
	Cause string `json:"cause"`
	// DueAt is when its next review is due, after the move, if ever.
	DueAt *time.Time `json:"due_at,omitempty"`
}

// What moved an asset's review state (EventReview.Cause).
const (
	// ReviewByMove is a person confirming, verifying or returning it to
	// draft.
	ReviewByMove = "move"
	// ReviewByCreate is its state when it was made.
	ReviewByCreate = "create"
	// ReviewByEdit is an edit returning it to draft, as its category says.
	ReviewByEdit = "edit"
	// ReviewByAssistant is a change through an assistant landing as a draft.
	ReviewByAssistant = "assistant"
	// ReviewByAccept and ReviewByReject are changes an assistant proposed
	// being accepted, and rejected, which puts back the state it had.
	ReviewByAccept = "accept"
	ReviewByReject = "reject"
)
