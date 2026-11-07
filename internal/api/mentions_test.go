package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/markdown"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// namedHolder makes a named person holding a role of these permissions,
// narrowed to scopes if any are given, signed in.
func namedHolder(t *testing.T, srv *Server, name string, scopes []string, perms ...string) (*models.User, *http.Cookie) {
	t.Helper()
	role, err := srv.store.CreateRole(store.RoleInput{Name: "Role of " + name, Permissions: perms})
	if err != nil {
		t.Fatal(err)
	}
	u, err := srv.store.CreateNamedUser(strings.ToLower(name)+"@hackspace.org", name, "UserSecret123!", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.store.SetUserRoleAssignments(u.ID, []models.RoleAssignment{{RoleID: role.ID, ScopeIDs: scopes}}); err != nil {
		t.Fatal(err)
	}
	return u, sessionCookie(t, srv, u.ID)
}

// peopleOffered lists the names GET .../comments/people offers, sorted,
// failing unless it answers 200.
func peopleOffered(t *testing.T, srv *Server, who *http.Cookie, path string) []string {
	t.Helper()
	rec := doJSON(t, srv, who, http.MethodGet, path, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
	}
	var out struct{ People []models.CommentMention }
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, p := range out.People {
		names = append(names, p.Name)
	}
	sort.Strings(names)
	return names
}

func holds(list []string, name string) bool {
	for _, n := range list {
		if n == name {
			return true
		}
	}
	return false
}

// TestMentionsThroughTheAPI: typing @ offers only the people who may read
// what the comment is on, and only to those who may comment there and see
// who; a mention reads as the person's name now to those who see who, and
// as someone to others, and is recorded by ID; previews and history name
// people as comments do; each person turns their mention emails off.
func TestMentionsThroughTheAPI(t *testing.T) {
	srv, _, _ := setupTestServer(t)
	woodshop, _ := srv.store.CreateScope("woodshop")
	saws, _ := srv.store.CreateCategoryFrom(store.CategoryInput{Name: "Saws"})
	if _, err := srv.store.SetCategoryScopes(saws.ID, []string{woodshop.ID}); err != nil {
		t.Fatal(err)
	}
	drill, _ := srv.store.CreateAsset("Drill", nil, nil, nil)
	saw, _ := srv.store.CreateAsset("Saw", []string{saws.ID}, nil, nil)
	member := []string{rbac.PermAssetsRead, rbac.PermAssetsTrack, rbac.PermCommentsWrite}
	ada, adaC := namedHolder(t, srv, "Ada", nil, member...)
	bob, _ := namedHolder(t, srv, "Bob", nil, member...)
	namedHolder(t, srv, "Wood", []string{woodshop.ID}, member...)
	_, readerC := namedHolder(t, srv, "Reader", nil, rbac.PermAssetsRead)
	_, blindC := namedHolder(t, srv, "Blind", nil, rbac.PermAssetsRead, rbac.PermCommentsWrite)

	drillPath := "/api/v1/assets/" + drill.Asset.ID + "/comments"
	onDrill := peopleOffered(t, srv, adaC, drillPath+"/people")
	if !holds(onDrill, "Bob") || !holds(onDrill, "Reader") || holds(onDrill, "Ada") || holds(onDrill, "Wood") {
		t.Errorf("offered on the drill: %v", onDrill)
	}
	if onSaw := peopleOffered(t, srv, adaC, "/api/v1/assets/"+saw.Asset.ID+"/comments/people"); !holds(onSaw, "Wood") {
		t.Errorf("offered on the saw: %v", onSaw)
	}
	if onSaws := peopleOffered(t, srv, adaC, "/api/v1/categories/"+saws.ID+"/comments/people"); !holds(onSaws, "Wood") || !holds(onSaws, "Bob") {
		t.Errorf("offered on the category: %v", onSaws)
	}
	// Without seeing who, or commenting, no one is offered: who exists is
	// not told.
	for _, who := range []*http.Cookie{blindC, readerC} {
		if rec := doJSON(t, srv, who, http.MethodGet, drillPath+"/people", ""); rec.Code != http.StatusForbidden {
			t.Errorf("offered without assets.track or comments.write: %d %s", rec.Code, rec.Body)
		}
	}
	if rec := doJSON(t, srv, adaC, http.MethodGet, "/api/v1/loans/nope/comments/people", ""); rec.Code != http.StatusNotFound {
		t.Errorf("a loan that is not: %d", rec.Code)
	}

	c := comment(t, srv, adaC, drillPath, "Ask "+markdown.Mention(bob.ID)+", <b>now</b>")
	if !strings.Contains(c.HTML, `<span class="mention">@Bob</span>`) || len(c.Mentions) != 1 || c.Mentions[0].Name != "Bob" || strings.Contains(c.HTML, "<b>") {
		t.Errorf("as written: %s %+v", c.HTML, c.Mentions)
	}
	if got, _ := srv.store.CommentMentions(c.ID); len(got) != 1 || got[0] != bob.ID {
		t.Errorf("recorded: %v", got)
	}

	// Renamed, Bob's mention reads with his new name; to someone who may
	// not see who, as someone.
	if _, err := srv.store.SetUserName(bob.ID, "Robert"); err != nil {
		t.Fatal(err)
	}
	list := decodeComments(t, doJSON(t, srv, adaC, http.MethodGet, drillPath, "")).Comments
	if len(list) != 1 || !strings.Contains(list[0].HTML, "@Robert") || list[0].Mentions[0].Name != "Robert" {
		t.Errorf("after renaming: %+v", list)
	}
	list = decodeComments(t, doJSON(t, srv, readerC, http.MethodGet, drillPath, "")).Comments
	if len(list) != 1 || !strings.Contains(list[0].HTML, "@someone") || strings.Contains(list[0].HTML, "Robert") || list[0].Mentions != nil {
		t.Errorf("to the reader: %+v", list)
	}
	rec := doJSON(t, srv, readerC, http.MethodGet, "/api/v1/comments/"+c.ID+"/history", "")
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "Robert") || strings.Contains(rec.Body.String(), `"mentions"`) {
		t.Errorf("history to the reader: %d %s", rec.Code, rec.Body)
	}

	body, _ := json.Marshal(map[string]string{"body": "Hi " + markdown.Mention(bob.ID)})
	for who, want := range map[*http.Cookie]string{adaC: "@Robert", readerC: "@someone"} {
		rec := doJSON(t, srv, who, http.MethodPost, "/api/v1/markdown", string(body))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), want) {
			t.Errorf("preview, wanting %s: %d %s", want, rec.Code, rec.Body)
		}
	}

	rec = doJSON(t, srv, adaC, http.MethodPut, "/api/v1/auth/mention-emails", `{"mentions":false}`)
	var out struct{ User models.User }
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil || out.User.MentionEmails {
		t.Errorf("turning mentions off: %d %s", rec.Code, rec.Body)
	}
	if u, _ := srv.store.GetUserByID(ada.ID); u.MentionEmails {
		t.Error("still on")
	}
	if rec := doJSON(t, srv, adaC, http.MethodPut, "/api/v1/auth/mention-emails", `{}`); rec.Code != http.StatusBadRequest {
		t.Errorf("without mentions: %d", rec.Code)
	}
}

// TestMentionsOverMCP: an assistant lists the people it may mention and
// mentions one; a person's assistant turns their mention emails off.
func TestMentionsOverMCP(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	drill, _ := srv.store.CreateAsset("Drill", nil, nil, nil)
	bob, _ := namedHolder(t, srv, "Bob", nil, rbac.PermAssetsRead)
	secret := tokenWith(t, srv, admin.ID, "assistant", rbac.PermAssetsRead, rbac.PermAssetsTrack, rbac.PermCommentsWrite)
	cs := mcpSession(t, srv, secret)

	var people struct{ People []models.CommentMention }
	structured(t, callTool(t, cs, "list_comment_people", map[string]any{"asset": drill.Asset.ID}), &people)
	var id string
	for _, p := range people.People {
		if p.Name == "Bob" {
			id = p.ID
		}
	}
	if id != bob.ID {
		t.Fatalf("people: %+v", people)
	}
	var added struct{ Comment models.Comment }
	structured(t, callTool(t, cs, "add_comment", map[string]any{"asset": drill.Asset.ID, "body": "Ask " + markdown.Mention(id)}), &added)
	if !strings.Contains(added.Comment.HTML, "@Bob") {
		t.Errorf("added: %+v", added.Comment)
	}

	_, personal, err := srv.store.CreatePersonalToken(bob.ID, "Bob's assistant", nil)
	if err != nil {
		t.Fatal(err)
	}
	var set struct{ Mentions bool }
	structured(t, callTool(t, mcpSession(t, srv, personal), "set_my_mention_emails", map[string]any{"mentions": false}), &set)
	if u, _ := srv.store.GetUserByID(bob.ID); set.Mentions || u.MentionEmails {
		t.Errorf("turned off: %+v %v", set, u.MentionEmails)
	}
}
