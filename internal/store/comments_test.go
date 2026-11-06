package store

import (
	"errors"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// commentBodies lists comments as "category name: body", or "own: body"
// for those on the thing itself, "deleted" for a deleted one.
func commentBodies(list []models.Comment) []string {
	out := []string{}
	for _, c := range list {
		where := "own"
		if c.Category != nil {
			where = c.Category.Name
		}
		body := c.Body
		if c.DeletedAt != nil {
			body = "deleted"
		}
		out = append(out, where+": "+body)
	}
	return out
}

// TestAnAssetShowsItsCategoriesComments: an asset's comments come first,
// then those on its categories and their ancestors, nearest first, each
// marked with its category; a category's show its ancestors' the same way.
func TestAnAssetShowsItsCategoriesComments(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	ada, err := st.CreateNamedUser("ada@hackspace.org", "Ada", "a long enough password", true)
	if err != nil {
		t.Fatal(err)
	}
	as := st.As(ada.ID)
	tools, _ := st.CreateCategoryFrom(CategoryInput{Name: "Tools"})
	power, _ := st.CreateCategoryFrom(CategoryInput{Name: "Power tools", ParentID: &tools.ID})
	drills, _ := st.CreateCategoryFrom(CategoryInput{Name: "Drills", ParentID: &power.ID})
	loaned, _ := st.CreateCategoryFrom(CategoryInput{Name: "Club kit"})
	drill := asset(t, st, "Drill", drills.ID, loaned.ID)

	for _, c := range []struct {
		on   CommentTarget
		body string
	}{
		{CommentTarget{On: models.CommentOnCategory, ID: tools.ID}, "wear goggles"},
		{CommentTarget{On: models.CommentOnAsset, ID: drill}, "the left motor makes a noise above 50%"},
		{CommentTarget{On: models.CommentOnCategory, ID: drills.ID}, "bits are in the drawer"},
		{CommentTarget{On: models.CommentOnCategory, ID: power.ID}, "unplug first"},
		{CommentTarget{On: models.CommentOnCategory, ID: loaned.ID}, "return by Sunday"},
		{CommentTarget{On: models.CommentOnAsset, ID: drill}, "ordered a replacement belt"},
	} {
		if _, err := as.AddComment(c.on, c.body); err != nil {
			t.Fatalf("commenting %q: %v", c.body, err)
		}
	}

	list, err := st.ListComments(CommentTarget{On: models.CommentOnAsset, ID: drill}, true)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"own: the left motor makes a noise above 50%", "own: ordered a replacement belt",
		"Club kit: return by Sunday", "Drills: bits are in the drawer", "Power tools: unplug first", "Tools: wear goggles"}
	if got := commentBodies(list); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("the drill's comments:\n got %q\nwant %q", got, want)
	}
	for _, c := range list {
		if c.Category != nil && c.On != models.CommentOnCategory || c.Category != nil && c.Category.ID != c.OnID {
			t.Errorf("an inherited comment names another category: %+v", c)
		}
		if c.AuthorID != ada.ID || c.AuthorName != "Ada" {
			t.Errorf("author: %+v", c)
		}
	}

	own, err := st.ListComments(CommentTarget{On: models.CommentOnAsset, ID: drill}, false)
	if err != nil || len(own) != 2 {
		t.Errorf("its own: %v %v", commentBodies(own), err)
	}
	cat, err := st.ListComments(CommentTarget{On: models.CommentOnCategory, ID: drills.ID}, true)
	if err != nil {
		t.Fatal(err)
	}
	want = []string{"own: bits are in the drawer", "Power tools: unplug first", "Tools: wear goggles"}
	if got := commentBodies(cat); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("the category's comments:\n got %q\nwant %q", got, want)
	}

	// Commenting needs text, and something to comment on.
	if _, err := as.AddComment(CommentTarget{On: models.CommentOnAsset, ID: drill}, "  \n "); !errors.Is(err, ErrCommentEmpty) {
		t.Errorf("an empty comment: %v", err)
	}
	if _, err := as.AddComment(CommentTarget{On: models.CommentOnAsset, ID: drill}, strings.Repeat("x", MaxCommentLength+1)); !errors.Is(err, ErrCommentTooLong) {
		t.Errorf("a long comment: %v", err)
	}
	if _, err := as.AddComment(CommentTarget{On: models.CommentOnCategory, ID: "nope"}, "x"); !errors.Is(err, ErrCategoryNotFound) {
		t.Errorf("on no category: %v", err)
	}

	// Deleting the category takes its comments with it.
	if err := st.DeleteCategory(loaned.ID); err != nil {
		t.Fatal(err)
	}
	list, _ = st.ListComments(CommentTarget{On: models.CommentOnAsset, ID: drill}, true)
	for _, c := range list {
		if c.Body == "return by Sunday" {
			t.Error("a deleted category's comment is still listed")
		}
	}
}

// TestEditsAndDeletionsAreKeptInHistory: an edited comment says so, a
// deleted one who deleted it, and every version's text stays in its
// history and its asset's.
func TestEditsAndDeletionsAreKeptInHistory(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	ada, _ := st.CreateNamedUser("ada@hackspace.org", "Ada", "a long enough password", true)
	mod, _ := st.CreateNamedUser("mo@hackspace.org", "Mo", "a long enough password", true)
	cat, _ := st.CreateCategoryFrom(CategoryInput{Name: "Lasers"})
	laser := asset(t, st, "Laser", cat.ID)

	c, err := st.As(ada.ID).AddComment(CommentTarget{On: models.CommentOnAsset, ID: laser}, "first")
	if err != nil {
		t.Fatal(err)
	}
	if c.EditedAt != nil || c.DeletedAt != nil {
		t.Errorf("new: %+v", c)
	}
	same, err := st.As(ada.ID).EditComment(c.ID, "first")
	if err != nil || same.EditedAt != nil {
		t.Errorf("the same text again: %+v %v", same, err)
	}
	edited, err := st.As(ada.ID).EditComment(c.ID, "second")
	if err != nil || edited.Body != "second" || edited.EditedAt == nil {
		t.Fatalf("edited: %+v %v", edited, err)
	}
	deleted, err := st.As(mod.ID).DeleteComment(c.ID)
	if err != nil || deleted.Body != "" || deleted.DeletedAt == nil || deleted.DeletedBy != mod.ID || deleted.DeletedByName != "Mo" {
		t.Fatalf("deleted: %+v %v", deleted, err)
	}
	if _, err := st.As(ada.ID).EditComment(c.ID, "third"); !errors.Is(err, ErrCommentDeleted) {
		t.Errorf("editing a deleted comment: %v", err)
	}
	if _, err := st.As(mod.ID).DeleteComment(c.ID); !errors.Is(err, ErrCommentDeleted) {
		t.Errorf("deleting it again: %v", err)
	}

	events, err := st.CommentHistory(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || events[0].Kind != models.EventCommentAdded || events[0].Payload.Comment.Text != "first" ||
		events[1].Kind != models.EventCommentEdited || events[1].Payload.Changes[0].From != "first" || events[1].Payload.Changes[0].To != "second" ||
		events[2].Kind != models.EventCommentDeleted || events[2].Payload.Changes[0].From != "second" || events[2].Payload.Actor.ID != mod.ID {
		t.Errorf("history: %+v", events)
	}
	kinds := []string{}
	for _, e := range history(t, st, laser) {
		kinds = append(kinds, e.Kind)
	}
	if strings.Join(kinds, " ") != "comment.deleted comment.edited comment.added asset.created" {
		t.Errorf("the asset's history: %v", kinds)
	}
	if _, err := st.GetComment("nope"); !errors.Is(err, ErrCommentNotFound) {
		t.Errorf("no comment: %v", err)
	}
}

// TestLoansAndFilingsTakeComments: a loan's and a filing's comments are
// their own, are in their asset's history, and are found by search, as an
// asset's are.
func TestLoansAndFilingsTakeComments(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	_, drills, lender, bo := lending(t, st)
	drill := asset(t, st, "Drill", drills.ID)
	a, err := st.As(lender.ID).CheckOut(drill, bo.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	loan := CommentTarget{On: models.CommentOnLoan, ID: a.Loan.ID}
	c, err := st.As(lender.ID).AddComment(loan, "Lent with the **long** bits")
	if err != nil || c.AssetID != drill || c.OnID != a.Loan.ID {
		t.Fatalf("on the loan: %+v %v", c, err)
	}
	e := history(t, st, drill)[0]
	if e.Kind != models.EventCommentAdded || e.Payload.Comment == nil || e.Payload.Comment.On != models.CommentOnLoan || e.Payload.Comment.LoanID != a.Loan.ID {
		t.Errorf("in the asset's history: %+v", e.Payload)
	}
	if _, err := st.AddComment(CommentTarget{On: models.CommentOnLoan, ID: "nope"}, "x"); !errors.Is(err, ErrLoanNotFound) {
		t.Errorf("on no loan: %v", err)
	}

	f, err := st.CreateFlow(FlowInput{Slug: "leave", Name: "Leave it", Enabled: true, Definition: starter(t, st, 0, drills.ID)}, "")
	if err != nil {
		t.Fatal(err)
	}
	filing := CommentTarget{On: models.CommentOnFiling, ID: drill, FlowID: f.ID}
	if _, err := st.AddComment(filing, "x"); !errors.Is(err, ErrFilingNotFound) {
		t.Errorf("before it was filed: %v", err)
	}
	if err := st.RecordFiling(f.ID, drill, bo.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.As(bo.ID).AddComment(filing, "Left on the shelf by the door"); err != nil {
		t.Fatal(err)
	}
	filings, err := st.ListFilings(drill)
	if err != nil || len(filings) != 1 || filings[0].FlowName != "Leave it" || filings[0].Comments != 1 {
		t.Errorf("filings: %+v %v", filings, err)
	}
	e = history(t, st, drill)[0]
	if e.Payload.Comment == nil || e.Payload.Comment.Flow == nil || e.Payload.Comment.Flow.Slug != "leave" {
		t.Errorf("the filing's comment in history: %+v", e.Payload)
	}
	list, err := st.ListComments(filing, true)
	if err != nil || len(list) != 1 || list[0].FlowID != f.ID || list[0].AssetID != drill {
		t.Errorf("the filing's comments: %+v %v", list, err)
	}

	// Search finds them all by their text, ignoring case, but not deleted
	// ones.
	found, err := st.SearchComments("the **LONG** bits")
	if err != nil || len(found) != 1 || found[0].Place == nil || found[0].Place.Name != "Drill" {
		t.Errorf("search: %+v %v", found, err)
	}
	commented, err := st.CommentedAssets("shelf")
	if err != nil || !commented[drill] {
		t.Errorf("assets by their comments: %v %v", commented, err)
	}
	if _, err := st.DeleteComment(c.ID); err != nil {
		t.Fatal(err)
	}
	if found, _ := st.SearchComments("**long** bits"); len(found) != 0 {
		t.Errorf("a deleted comment is found: %+v", found)
	}
}
