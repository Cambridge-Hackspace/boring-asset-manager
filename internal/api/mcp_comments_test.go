package api

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// TestCommentsOverMCP: an assistant reads an asset's comments with its
// categories', writes one with a file attached, edits it, finds it,
// reads its history and deletes it; without comments.write, it only
// reads.
func TestCommentsOverMCP(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	tools, _ := srv.store.CreateCategoryFrom(store.CategoryInput{Name: "Tools"})
	a, err := srv.store.CreateAsset("Drill", []string{tools.ID}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.store.As(admin.ID).AddComment(store.CommentTarget{On: "category", ID: tools.ID}, "Wear goggles"); err != nil {
		t.Fatal(err)
	}
	secret := tokenWith(t, srv, admin.ID, "assistant", rbac.PermAssetsRead, rbac.PermAssetsTrack, rbac.PermCommentsWrite)
	cs := mcpSession(t, srv, secret)

	var attached struct{ Markdown string }
	structured(t, callTool(t, cs, "attach_to_comment", map[string]any{"asset": a.Asset.ID, "filename": "belt.png",
		"data": base64.StdEncoding.EncodeToString(pngBytes(t, 4, 4))}), &attached)
	if !strings.HasPrefix(attached.Markdown, "![belt.png](/api/v1/assets/") {
		t.Errorf("attached: %+v", attached)
	}
	var added struct {
		Comment struct {
			ID, HTML string
			Mine     bool
		}
	}
	structured(t, callTool(t, cs, "add_comment", map[string]any{"asset": a.Asset.ID, "body": "Belt ordered\n\n" + attached.Markdown}), &added)
	if added.Comment.ID == "" || !strings.Contains(added.Comment.HTML, "<img") || !added.Comment.Mine {
		t.Fatalf("added: %+v", added)
	}
	id := added.Comment.ID

	var list struct {
		Comments []struct {
			ID       string
			Category *struct{ Name string }
		}
	}
	structured(t, callTool(t, cs, "list_comments", map[string]any{"asset": a.Asset.ID}), &list)
	if len(list.Comments) != 2 || list.Comments[0].ID != id || list.Comments[1].Category == nil || list.Comments[1].Category.Name != "Tools" {
		t.Errorf("listed: %+v", list)
	}
	structured(t, callTool(t, cs, "list_comments", map[string]any{"asset": a.Asset.ID, "own": true}), &list)
	if len(list.Comments) != 1 {
		t.Errorf("its own: %+v", list)
	}
	if res := callTool(t, cs, "list_comments", map[string]any{"asset": a.Asset.ID, "category_id": tools.ID}); !res.IsError {
		t.Errorf("two things at once: %s", toolText(res))
	}
	if res := callTool(t, cs, "edit_comment", map[string]any{"id": id, "body": "Belt **fitted**"}); res.IsError {
		t.Errorf("editing: %s", toolText(res))
	}
	var found struct{ Comments []struct{ ID string } }
	structured(t, callTool(t, cs, "search_comments", map[string]any{"query": "fitted"}), &found)
	if len(found.Comments) != 1 || found.Comments[0].ID != id {
		t.Errorf("found: %+v", found)
	}
	var got struct{ Comment struct{ Body string } }
	structured(t, callTool(t, cs, "get_comment", map[string]any{"id": id}), &got)
	if got.Comment.Body != "Belt **fitted**" {
		t.Errorf("got: %+v", got)
	}
	var filings struct{ Filings []any }
	structured(t, callTool(t, cs, "list_filings", map[string]any{"id": a.Asset.ID}), &filings)
	if len(filings.Filings) != 0 {
		t.Errorf("filings: %+v", filings)
	}
	if res := callTool(t, cs, "delete_comment", map[string]any{"id": id}); res.IsError {
		t.Errorf("deleting: %s", toolText(res))
	}
	var history struct{ Events []struct{ Kind string } }
	structured(t, callTool(t, cs, "comment_history", map[string]any{"id": id}), &history)
	if len(history.Events) != 3 || history.Events[2].Kind != "comment.deleted" {
		t.Errorf("history: %+v", history)
	}

	reader := mcpSession(t, srv, tokenWith(t, srv, admin.ID, "reader", rbac.PermAssetsRead))
	names := strings.Join(toolNames(t, reader), " ")
	if !strings.Contains(names, "list_comments") || strings.Contains(names, "add_comment") || strings.Contains(names, "delete_comment") {
		t.Errorf("a reader's tools: %s", names)
	}
}
