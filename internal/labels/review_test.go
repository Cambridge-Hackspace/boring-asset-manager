package labels

import "testing"

// An asset's review binds as its state, when it is next due, and when it
// was last confirmed and by whom (docs/review.md).
func TestReviewBindings(t *testing.T) {
	b, err := ParseBinding("{{asset.review_state}} until {{ asset.review_due }}, confirmed {{asset.confirmed_at}} by {{asset.confirmed_by}}")
	if err != nil {
		t.Fatal(err)
	}
	d := Data{Review: ReviewData{State: "Verified", Due: "2027-11-08", ConfirmedAt: "2026-11-08", ConfirmedBy: "Ada"}}
	if got, want := b.Resolve(d), "Verified until 2027-11-08, confirmed 2026-11-08 by Ada"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := b.Resolve(Data{}); got != " until , confirmed  by " {
		t.Errorf("without review: %q", got)
	}
}
