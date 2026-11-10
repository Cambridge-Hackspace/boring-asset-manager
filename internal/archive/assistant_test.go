package archive

import (
	"bytes"
	"context"
	"slices"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/filestore"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// TestBackupsLeaveConversationsOut: people's conversations with the
// assistant are left out of a backup unless asked for, as authentication
// is; asked for, they are restored. Guides and settings go as always, the
// provider's key with authentication.
func TestBackupsLeaveConversationsOut(t *testing.T) {
	ctx := context.Background()
	src := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	seed(t, src, blobs)
	u, err := src.CreateUser("ada@test.org", "AdaSecret123!", true)
	if err != nil {
		t.Fatal(err)
	}
	c, err := src.CreateConversation(u.ID, "The band saw", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := src.AddConversationMessage(c.ID, store.ConversationMessage{Role: "user", Content: []byte(`[{"type":"text","text":"There's a band saw"}]`), Context: []byte(`[{"kind":"asset","label":"Shelf"}]`)}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := src.SaveGuide("", store.GuideInput{Slug: "shop", Name: "Shop", Purpose: "Record it", Enabled: true, Questions: []string{"What?"}}, ""); err != nil {
		t.Fatal(err)
	}
	set := store.DefaultAssistantSettings()
	set.Enabled, set.APIKey = true, "sk-secret"
	if err := src.SaveAssistantSettings(set, ""); err != nil {
		t.Fatal(err)
	}

	plain := backup(t, src, blobs, Options{})
	checked, err := Check(bytes.NewReader(plain))
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(checked.Header.Kinds, KindConversations) {
		t.Errorf("a plain backup holds conversations: %v", checked.Header.Kinds)
	}
	dst := emptyStore(t, envDialect())
	if _, err := Restore(ctx, dst, filestore.NewLocal(t.TempDir()), opener(plain), RestoreOptions{}); err != nil {
		t.Fatal(err)
	}
	if list, _ := dst.ListConversations(u.ID); len(list) != 0 {
		t.Errorf("restored from a plain backup: %v", list)
	}
	if g, err := dst.GetGuide("shop"); err != nil || len(g.Questions) != 1 {
		t.Errorf("the guide: %+v %v", g, err)
	}
	if got, _ := dst.AssistantSettings(); !got.Enabled || got.APIKey != "" {
		t.Errorf("settings restored without authentication: %+v", got)
	}

	full := backup(t, src, blobs, Options{Conversations: true, Secrets: true})
	dst = emptyStore(t, envDialect())
	if _, err := Restore(ctx, dst, filestore.NewLocal(t.TempDir()), opener(full), RestoreOptions{Secrets: true}); err != nil {
		t.Fatal(err)
	}
	list, err := dst.ListConversations(u.ID)
	if err != nil || len(list) != 1 || list[0].Title != "The band saw" {
		t.Fatalf("restored conversations: %v %v", list, err)
	}
	if msgs, err := dst.ConversationMessages(c.ID); err != nil || len(msgs) != 1 || string(msgs[0].Context) == "" {
		t.Errorf("restored messages: %+v %v", msgs, err)
	}
	if got, _ := dst.AssistantSettings(); got.APIKey != "sk-secret" {
		t.Errorf("the key was not restored with authentication: %+v", got)
	}
}
