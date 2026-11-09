package store

import (
	"errors"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/fields"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

func boolp(b bool) *bool    { return &b }
func strp(s string) *string { return &s }
func reviewed(t *testing.T, st *Store, id string) *models.AssetReview {
	t.Helper()
	a, err := st.GetAssetByID(id)
	if err != nil {
		t.Fatal(err)
	}
	return a.Review
}

// reviewing turns review states on for a category, with the settings given.
func reviewing(t *testing.T, st *Store, id string, in models.CategoryReview) {
	t.Helper()
	if _, err := st.SetCategoryReview(id, in); err != nil {
		t.Fatalf("setting review on %s: %v", id, err)
	}
}

func person(t *testing.T, st *Store, email string) string {
	t.Helper()
	u, err := st.CreateUser(email, "Secret-password-1", true)
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}

func reviewEvents(t *testing.T, st *Store, assetID, kind string) []models.Event {
	t.Helper()
	all, _, err := st.ListAssetEvents(assetID, "", 200)
	if err != nil {
		t.Fatal(err)
	}
	var out []models.Event
	for _, e := range all {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

// Settings pass down the tree as fields do, each nearest first, and an
// asset in several categories takes the strictest of those that use review
// states.
func TestReviewSettingsPassDownTheTree(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	shop := plainCategory(t, st, "Shop", nil)
	tools := plainCategory(t, st, "Tools", &shop.ID)
	saws := plainCategory(t, st, "Saws", &tools.ID)
	loose := plainCategory(t, st, "Loose", &tools.ID)
	reviewing(t, st, shop.ID, models.CategoryReview{Enabled: boolp(true), Months: intp(12)})
	reviewing(t, st, saws.ID, models.CategoryReview{Months: intp(6), OnEdit: strp(models.ReviewOnEditKeep)})
	reviewing(t, st, loose.ID, models.CategoryReview{Enabled: boolp(false)})

	got, err := st.GetCategoryByID(saws.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := models.ReviewRules{Enabled: true, Months: 6, OnEdit: models.ReviewOnEditKeep}
	if got.EffectiveReview != want {
		t.Errorf("saws take %+v, want %+v", got.EffectiveReview, want)
	}
	if got.Review.Enabled != nil || *got.Review.Months != 6 {
		t.Errorf("saws' own settings: %+v", got.Review)
	}
	if c, _ := st.GetCategoryByID(tools.ID); c.EffectiveReview != (models.ReviewRules{Enabled: true, Months: 12, OnEdit: models.ReviewOnEditDraft}) {
		t.Errorf("tools take %+v", c.EffectiveReview)
	}

	a, err := st.CreateAsset("Band saw", []string{saws.ID, tools.ID}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := a.Review
	if r == nil || r.State != models.ReviewDraft || r.Rules.Months != 6 || r.Rules.OnEdit != models.ReviewOnEditDraft {
		t.Errorf("an asset in saws and tools reviews as %+v", r)
	}
	if b, _ := st.CreateAsset("Bucket of offcuts", []string{loose.ID}, nil, nil); b.Review != nil {
		t.Errorf("an asset where review is turned off below has a review: %+v", b.Review)
	}
}

// Confirming and verifying move an asset forward, a person cannot verify
// what they confirmed unless the category allows it, returning needs a
// note, and each move is in the history with who made it.
func TestReviewMoves(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	tools := plainCategory(t, st, "Tools", nil)
	reviewing(t, st, tools.ID, models.CategoryReview{Enabled: boolp(true), Months: intp(12)})
	ada, bea := person(t, st, "ada@example.org"), person(t, st, "bea@example.org")
	a, err := st.CreateAsset("Drill", []string{tools.ID}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	id := a.Asset.ID

	if _, err := st.As(ada).ReviewAsset(id, models.ReviewMoveVerify, ""); !errors.Is(err, ErrReviewMove) {
		t.Errorf("verifying a draft: %v", err)
	}
	if _, err := st.As(ada).ReviewAsset(id, models.ReviewMoveReturn, "wrong"); !errors.Is(err, ErrReviewMove) {
		t.Errorf("returning a draft: %v", err)
	}
	before := time.Now().UTC().Add(-time.Second)
	got, err := st.As(ada).ReviewAsset(id, models.ReviewMoveConfirm, "looked it over")
	if err != nil {
		t.Fatal(err)
	}
	r := got.Review
	if r.State != models.ReviewConfirmed || r.ConfirmedBy == nil || r.ConfirmedBy.ID != ada || r.DueAt == nil {
		t.Fatalf("confirmed: %+v", r)
	}
	if want := r.ReviewedAt.AddDate(1, 0, 0); !r.DueAt.Equal(want) || r.ReviewedAt.Before(before) {
		t.Errorf("due %v, want a year after %v", r.DueAt, r.ReviewedAt)
	}
	if _, err := st.As(ada).ReviewAsset(id, models.ReviewMoveVerify, ""); !errors.Is(err, ErrReviewOwnConfirmation) {
		t.Errorf("verifying one's own confirmation: %v", err)
	}
	if got, err = st.As(bea).ReviewAsset(id, models.ReviewMoveVerify, ""); err != nil || got.Review.State != models.ReviewVerified || got.Review.SetBy.ID != bea {
		t.Fatalf("verified by someone else: %+v %v", got.Review, err)
	}
	if _, err := st.As(bea).ReviewAsset(id, models.ReviewMoveReturn, ""); !errors.Is(err, ErrReviewNoteRequired) {
		t.Errorf("returning without a note: %v", err)
	}
	if got, err = st.As(bea).ReviewAsset(id, models.ReviewMoveReturn, "the chuck is missing"); err != nil || got.Review.State != models.ReviewDraft || got.Review.DueAt != nil {
		t.Fatalf("returned: %+v %v", got.Review, err)
	}

	moves := reviewEvents(t, st, id, models.EventAssetReviewChanged)
	if len(moves) != 3 {
		t.Fatalf("history holds %d moves, want 3", len(moves))
	}
	last := moves[0]
	if last.Payload.Review.From != models.ReviewVerified || last.Payload.Review.To != models.ReviewDraft || last.Payload.Note != "the chuck is missing" ||
		last.Payload.Actor == nil || last.Payload.Actor.ID != bea {
		t.Errorf("the return reads %+v, note %q, by %+v", last.Payload.Review, last.Payload.Note, last.Payload.Actor)
	}

	// The category lets people verify their own confirmation.
	reviewing(t, st, tools.ID, models.CategoryReview{Enabled: boolp(true), SelfVerify: boolp(true)})
	if _, err := st.As(ada).ReviewAsset(id, models.ReviewMoveConfirm, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.As(ada).ReviewAsset(id, models.ReviewMoveVerify, ""); err != nil {
		t.Errorf("verifying one's own where allowed: %v", err)
	}

	plain := plainCategory(t, st, "Plain", nil)
	b, _ := st.CreateAsset("Broom", []string{plain.ID}, nil, nil)
	if _, err := st.As(ada).ReviewAsset(b.Asset.ID, models.ReviewMoveConfirm, ""); !errors.Is(err, ErrReviewOff) {
		t.Errorf("confirming where review is off: %v", err)
	}
}

// Editing a confirmed asset returns it to draft, or keeps its state, as
// its category says; links count as edits.
func TestEditingReturnsToDraftAsTheCategorySays(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	draft := plainCategory(t, st, "Returns", nil)
	keep := plainCategory(t, st, "Keeps", nil)
	reviewing(t, st, draft.ID, models.CategoryReview{Enabled: boolp(true)})
	reviewing(t, st, keep.ID, models.CategoryReview{Enabled: boolp(true), OnEdit: strp(models.ReviewOnEditKeep)})
	ada := person(t, st, "ada@example.org")
	note, err := st.CreateField("note", "Note", fields.TypeText, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{draft.ID, keep.ID} {
		if err := st.AssignCategoryField(c, note.ID, false); err != nil {
			t.Fatal(err)
		}
	}
	a, _ := st.As(ada).Confirming().CreateAsset("Lathe", []string{draft.ID}, nil, nil)
	b, _ := st.As(ada).Confirming().CreateAsset("Mill", []string{keep.ID}, nil, nil)
	shelf, _ := st.CreateAsset("Shelf", nil, nil, nil)
	if a.Review.State != models.ReviewConfirmed || b.Review.State != models.ReviewConfirmed {
		t.Fatalf("filed as confirmed: %+v %+v", a.Review, b.Review)
	}

	for _, id := range []string{a.Asset.ID, b.Asset.ID} {
		if _, err := st.As(ada).UpdateAsset(id, "", nil, nil, map[string]string{"note": "oiled"}); err != nil {
			t.Fatal(err)
		}
	}
	if r := reviewed(t, st, a.Asset.ID); r.State != models.ReviewDraft {
		t.Errorf("an edit where the category returns to draft leaves %s", r.State)
	}
	if r := reviewed(t, st, b.Asset.ID); r.State != models.ReviewConfirmed {
		t.Errorf("an edit where the category keeps the state leaves %s", r.State)
	}
	edits := reviewEvents(t, st, a.Asset.ID, models.EventAssetReviewChanged)
	if len(edits) == 0 || edits[0].Payload.Review.Cause != models.ReviewByEdit {
		t.Errorf("the return to draft is not in the history as an edit: %+v", edits)
	}

	// A link is an edit too.
	if _, err := st.As(ada).ReviewAsset(a.Asset.ID, models.ReviewMoveConfirm, ""); err != nil {
		t.Fatal(err)
	}
	loc := locationType(t, st)
	if _, err := st.As(ada).AddLink(shelf.Asset.ID, loc, a.Asset.ID, true); err != nil {
		t.Fatal(err)
	}
	if r := reviewed(t, st, a.Asset.ID); r.State != models.ReviewDraft {
		t.Errorf("a link leaves %s", r.State)
	}
}

func locationType(t *testing.T, st *Store) string {
	t.Helper()
	types, err := st.ListRelationTypes()
	if err != nil {
		t.Fatal(err)
	}
	for _, rt := range types {
		if rt.IsLocation {
			return rt.ID
		}
	}
	t.Fatal("no location type")
	return ""
}

// Through an assistant, with the instance landing its changes as drafts,
// a change returns the asset to draft with a proposal; accepting keeps
// the changes, rejecting puts the asset back as it was, links included,
// and an asset the assistant made is archived when rejected.
func TestAssistantChangesLandAsDrafts(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	tools := plainCategory(t, st, "Tools", nil)
	reviewing(t, st, tools.ID, models.CategoryReview{Enabled: boolp(true), OnEdit: strp(models.ReviewOnEditKeep)})
	ada := person(t, st, "ada@example.org")
	note, _ := st.CreateField("note", "Note", fields.TypeText, nil)
	if err := st.AssignCategoryField(tools.ID, note.ID, false); err != nil {
		t.Fatal(err)
	}
	a, err := st.As(ada).Confirming().CreateAsset("Planer", []string{tools.ID}, nil, map[string]string{"note": "sharp"})
	if err != nil {
		t.Fatal(err)
	}
	id := a.Asset.ID
	shelf, _ := st.CreateAsset("Shelf", nil, nil, nil)
	loc := locationType(t, st)
	claude := st.As(ada).Via("Claude Desktop")

	// Confirming through the assistant is refused; the change lands.
	if _, err := claude.ReviewAsset(id, models.ReviewMoveConfirm, ""); !errors.Is(err, ErrReviewByAssistant) {
		t.Errorf("confirming through an assistant: %v", err)
	}
	if _, err := claude.UpdateAsset(id, "Thickness planer", nil, nil, map[string]string{"note": "blunt"}); err != nil {
		t.Fatal(err)
	}
	if _, err := claude.AddLink(shelf.Asset.ID, loc, id, true); err != nil {
		t.Fatal(err)
	}
	r := reviewed(t, st, id)
	if r.State != models.ReviewDraft || r.Proposal == nil || !r.Proposal.Undoable || r.Proposal.New || r.Proposal.Via != "Claude Desktop" ||
		r.Proposal.PriorState != models.ReviewConfirmed || r.Proposal.By == nil || r.Proposal.By.ID != ada {
		t.Fatalf("after the assistant's changes: %+v %+v", r, r.Proposal)
	}
	if _, err := claude.AcceptProposal(id, false); !errors.Is(err, ErrReviewByAssistant) {
		t.Errorf("accepting through an assistant: %v", err)
	}
	if _, err := st.As(ada).ReviewAsset(id, models.ReviewMoveConfirm, ""); !errors.Is(err, ErrProposalPending) {
		t.Errorf("confirming with changes waiting: %v", err)
	}

	got, err := st.As(ada).RejectProposal(id, "not this one")
	if err != nil {
		t.Fatal(err)
	}
	if got.Asset.Name != "Planer" || got.FieldValues["note"] != "sharp" || len(got.Location) != 0 {
		t.Errorf("rejected, it reads %q, note %q, in %v", got.Asset.Name, got.FieldValues["note"], got.Location)
	}
	if got.Review.State != models.ReviewConfirmed || got.Review.Proposal != nil {
		t.Errorf("rejected, its review is %+v", got.Review)
	}
	if s, _ := st.GetAssetByID(shelf.Asset.ID); len(s.Links) != 0 {
		t.Errorf("the shelf keeps the link: %+v", s.Links)
	}

	// Accepted, the changes stay, and the category keeps the state it had.
	if _, err := claude.UpdateAsset(id, "Thickness planer", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got, err = st.As(ada).AcceptProposal(id, false); err != nil {
		t.Fatal(err)
	}
	if got.Asset.Name != "Thickness planer" || got.Review.State != models.ReviewConfirmed || got.Review.Proposal != nil {
		t.Errorf("accepted: %q %+v", got.Asset.Name, got.Review)
	}

	// A person's own edit takes the changes waiting as theirs.
	if _, err := claude.UpdateAsset(id, "Planer 2", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := st.As(ada).UpdateAsset(id, "Planer 3", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if r := reviewed(t, st, id); r.Proposal != nil {
		t.Errorf("a person's edit leaves the proposal: %+v", r.Proposal)
	}

	// An asset the assistant makes is a draft even if asked to confirm it,
	// and rejecting it archives it.
	made, err := claude.Confirming().CreateAsset("Router", []string{tools.ID}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if made.Review.State != models.ReviewDraft || made.Review.Proposal == nil || !made.Review.Proposal.New {
		t.Fatalf("made by the assistant: %+v", made.Review)
	}
	if got, err = st.As(ada).RejectProposal(made.Asset.ID, ""); err != nil || got.Asset.ArchivedAt == nil {
		t.Errorf("rejecting a new asset: %v, archived %v", err, got)
	}

	// Turned off per instance, an assistant's changes are the person's.
	if err := st.SetReviewSettings(ReviewSettings{AssistantDrafts: false, DueDays: DefaultReviewDueDays}); err != nil {
		t.Fatal(err)
	}
	if _, err := claude.ReviewAsset(id, models.ReviewMoveConfirm, ""); err != nil {
		t.Errorf("confirming through an assistant when it is off: %v", err)
	}
	if _, err := claude.UpdateAsset(id, "Planer 4", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if r := reviewed(t, st, id); r.Proposal != nil || r.State != models.ReviewConfirmed {
		t.Errorf("with drafts off, the assistant's edit leaves %+v", r)
	}
}

// A review falls due its category's months after it was confirmed, shows
// as due from the instance's days before and overdue from the date, and
// asset.review_due is recorded once for each date, even with two servers
// polling at once.
func TestReviewsFallDueAndAnnounceThemselvesOnce(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	tools := plainCategory(t, st, "Tools", nil)
	reviewing(t, st, tools.ID, models.CategoryReview{Enabled: boolp(true), Months: intp(12)})
	ada := person(t, st, "ada@example.org")
	a, err := st.As(ada).Confirming().CreateAsset("Saw", []string{tools.ID}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	id := a.Asset.ID
	due := *a.Review.DueAt
	if a.Review.Due || a.Review.Overdue {
		t.Errorf("a new confirmation is already due: %+v", a.Review)
	}
	if n, err := st.RecordDueReviews(time.Now()); err != nil || n != 0 {
		t.Errorf("recorded %d a year early (%v)", n, err)
	}
	soon := due.AddDate(0, 0, -DefaultReviewDueDays+1)
	results := make(chan int, 2)
	for range 2 {
		go func() {
			n, err := st.RecordDueReviews(soon)
			if err != nil {
				t.Error(err)
			}
			results <- n
		}()
	}
	if total := <-results + <-results; total != 1 {
		t.Errorf("two servers recorded %d, want 1", total)
	}
	if n, _ := st.RecordDueReviews(due.AddDate(0, 0, 1)); n != 0 {
		t.Errorf("recorded again for the same date: %d", n)
	}
	got := reviewEvents(t, st, id, models.EventAssetReviewDue)
	if len(got) != 1 || got[0].Payload.DueAt == nil || !got[0].Payload.DueAt.Equal(due) {
		t.Fatalf("review_due events: %+v", got)
	}

	// Its due date moves back a year to make it overdue, as if confirmed
	// long ago.
	past := time.Now().UTC().AddDate(-1, 0, -1)
	if _, err := st.Exec("UPDATE assets SET reviewed_at = ?, review_due = ? WHERE id = ?", dbTime(past), dbTime(past.AddDate(1, 0, 0)), id); err != nil {
		t.Fatal(err)
	}
	if r := reviewed(t, st, id); !r.Due || !r.Overdue {
		t.Errorf("a year and a day on: %+v", r)
	}
	if list, err := st.FilterAssets(AssetFilter{Review: ReviewOverdue}); err != nil || len(list) != 1 {
		t.Errorf("the overdue filter finds %d (%v)", len(list), err)
	}
	if n, _ := st.RecordDueReviews(time.Now()); n != 1 {
		t.Errorf("a new date records again: %d", n)
	}

	// Confirmed again, the next review is a year on, and not due.
	if _, err := st.As(ada).ReviewAsset(id, models.ReviewMoveConfirm, ""); err != nil {
		t.Fatal(err)
	}
	if r := reviewed(t, st, id); r.Due || r.Overdue || r.DueAt == nil || r.DueAt.Before(time.Now().AddDate(0, 11, 0)) {
		t.Errorf("confirmed again: %+v", r)
	}

	// The category asking for review less often moves the date.
	reviewing(t, st, tools.ID, models.CategoryReview{Enabled: boolp(true), Months: intp(24)})
	r := reviewed(t, st, id)
	if want := r.ReviewedAt.AddDate(2, 0, 0); r.DueAt == nil || !r.DueAt.Equal(want) {
		t.Errorf("with 24 months, due %v, want %v", r.DueAt, want)
	}
	reviewing(t, st, tools.ID, models.CategoryReview{Enabled: boolp(true)})
	if r := reviewed(t, st, id); r.DueAt != nil {
		t.Errorf("asking for no review leaves a date: %v", r.DueAt)
	}
}

// Categories without review states behave as before: no review, no
// events, and edits and links as they were.
func TestCategoriesWithoutReviewBehaveAsBefore(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	plain := plainCategory(t, st, "Plain", nil)
	ada := person(t, st, "ada@example.org")
	a, err := st.As(ada).Via("Claude Desktop").CreateAsset("Hammer", []string{plain.ID}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.As(ada).Via("Claude Desktop").UpdateAsset(a.Asset.ID, "Claw hammer", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetAssetByID(a.Asset.ID)
	if got.Review != nil || got.Asset.Name != "Claw hammer" {
		t.Errorf("without review: %+v %q", got.Review, got.Asset.Name)
	}
	if ev := reviewEvents(t, st, a.Asset.ID, models.EventAssetReviewChanged); len(ev) != 0 {
		t.Errorf("review events without review: %+v", ev)
	}
	var state, proposals int
	if err := st.QueryRow("SELECT COUNT(*) FROM assets WHERE review_state IS NOT NULL").Scan(&state); err != nil {
		t.Fatal(err)
	}
	if err := st.QueryRow("SELECT COUNT(*) FROM asset_proposals").Scan(&proposals); err != nil {
		t.Fatal(err)
	}
	if state != 0 || proposals != 0 {
		t.Errorf("%d states and %d proposals stored without review", state, proposals)
	}
	if list, err := st.FilterAssets(AssetFilter{Review: models.ReviewDraft}); err != nil || len(list) != 0 {
		t.Errorf("the draft filter finds %d (%v)", len(list), err)
	}
	if _, err := st.FilterAssets(AssetFilter{Review: "pending"}); !errors.Is(err, ErrInvalidReviewFilter) {
		t.Errorf("an unknown filter: %v", err)
	}
}
