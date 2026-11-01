package store

import (
	"errors"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

func TestLiveConnectionsAndReplies(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	admin, _ := st.CreateAdminUser("admin@test.org", "AdminSecret123!")
	tok, _, err := st.CreateToken("Cabinet A", nil, nil, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	a, err := st.OpenLiveConnection(tok.ID, "", "10.0.0.7:5555")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := st.OpenLiveConnection("", admin.ID, "10.0.0.8:5555")
	if mine, _ := st.ListLiveConnections(time.Now().Add(-time.Minute), tok.ID, ""); len(mine) != 1 || mine[0].ID != a || *mine[0].TokenID != tok.ID || mine[0].Remote != "10.0.0.7:5555" {
		t.Errorf("the token's: %+v", mine)
	}
	if all, _ := st.ListLiveConnections(time.Now().Add(-time.Minute), "", ""); len(all) != 2 {
		t.Errorf("all: %+v", all)
	}
	// One not seen lately was a stopped server's: it is not counted, and
	// goes when another closes.
	if _, err := st.Exec("UPDATE live_connections SET seen_at = ? WHERE id = ?", time.Now().Add(-time.Hour).UTC(), b); err != nil {
		t.Fatal(err)
	}
	if err := st.TouchLiveConnections([]string{a}); err != nil {
		t.Fatal(err)
	}
	if all, _ := st.ListLiveConnections(time.Now().Add(-time.Minute), "", ""); len(all) != 1 {
		t.Errorf("with one stale: %+v", all)
	}
	if err := st.CloseLiveConnection(a, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if left, _ := st.ListLiveConnections(time.Time{}, "", ""); len(left) != 0 {
		t.Errorf("after closing: %+v", left)
	}

	// Answers to hooks, and only to hooks.
	identify, _ := st.SaveHook("", HookInput{Key: "identify", Label: "Identify"})
	cat, _ := st.CreateCategory("Parts", nil, "", false, false)
	_, _ = st.SetCategoryHooks(cat.ID, []models.CategoryHook{{HookID: identify.ID}})
	asset, _ := st.CreateAsset("Drawer", []string{cat.ID}, nil, nil)
	eventID, err := st.RecordAssetHook(asset.Asset.ID, "identify")
	if err != nil {
		t.Fatal(err)
	}
	long := ""
	for len(long) < 300 {
		long += "lit "
	}
	r, err := st.AsToken(tok.ID).RecordHookReply(eventID, true, long)
	if err != nil || r.Name != "Cabinet A" || !r.Token || len(r.Message) != MaxReplyMessage {
		t.Errorf("an answer: %+v, %v", r, err)
	}
	if replies, _ := st.HookReplies(eventID); len(replies) != 1 || !replies[0].OK || !replies[0].Token {
		t.Errorf("answers: %+v", replies)
	}
	events, _, _ := st.ListAssetEvents(asset.Asset.ID, "", 10)
	var created string
	for _, e := range events {
		if e.Kind == models.EventAssetCreated {
			created = e.ID
		}
	}
	if _, err := st.RecordHookReply(created, true, ""); !errors.Is(err, ErrNotAHook) {
		t.Errorf("answering something else: %v", err)
	}
	if _, err := st.RecordHookReply("nope", true, ""); !errors.Is(err, ErrEventNotFound) {
		t.Errorf("answering nothing: %v", err)
	}
	if got, err := st.EventAsset(eventID); err != nil || got != asset.Asset.ID {
		t.Errorf("the event's asset: %q, %v", got, err)
	}

	// Recent events, and where they are.
	recent, err := st.EventsSince(time.Now().Add(-time.Minute), 100)
	if err != nil || len(recent) < 2 || recent[len(recent)-1].ID != eventID {
		t.Fatalf("recent: %d, %v", len(recent), err)
	}
	cats, scopes, err := st.EventPlaces(recent)
	if err != nil || !cats[asset.Asset.ID][cat.ID] || len(scopes[asset.Asset.ID]) == 0 {
		t.Errorf("places: %v %v %v", cats, scopes, err)
	}
}
