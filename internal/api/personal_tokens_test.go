package api

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// personalToken makes the user behind the cookie a personal token, through
// the API, and returns its secret.
func personalToken(t *testing.T, srv *Server, cookie *http.Cookie, name string) (models.APIToken, string) {
	t.Helper()
	rec := doJSON(t, srv, cookie, http.MethodPost, "/api/v1/auth/tokens", `{"name":"`+name+`"}`)
	var made struct {
		Token  models.APIToken
		Secret string
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &made); err != nil || rec.Code != http.StatusCreated || !strings.HasPrefix(made.Secret, "bam_") {
		t.Fatalf("making a personal token: %d %s", rec.Code, rec.Body)
	}
	return made.Token, made.Secret
}

func TestPersonalTokensActAsTheirUser(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	adminCookie := sessionCookie(t, srv, admin.ID)
	member := userWithPermissions(t, srv, "member@hackspace.org", rbac.PermAssetsRead, rbac.PermAssetsBorrow)
	memberUser, err := srv.store.GetUserByEmail("member@hackspace.org")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.store.SetUserName(memberUser.ID, "Ada"); err != nil {
		t.Fatal(err)
	}

	// Anyone signed in makes their own, with no roles to choose.
	token, secret := personalToken(t, srv, member, "Claude")
	if token.UserID != memberUser.ID || len(token.RoleIDs) != 0 || token.CreatedBy != memberUser.ID {
		t.Errorf("token: %+v", token)
	}

	// It is answered as its user.
	rec := withBearer(t, srv, secret, http.MethodGet, "/api/v1/auth/me", "")
	var me struct {
		User        models.User
		Permissions []string
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &me)
	if rec.Code != http.StatusOK || me.User.ID != memberUser.ID || !slices.Contains(me.Permissions, rbac.PermAssetsBorrow) {
		t.Errorf("/auth/me: %d %s", rec.Code, rec.Body)
	}
	if rec := withBearer(t, srv, secret, http.MethodPut, "/api/v1/auth/name", `{"name":"Ada L."}`); rec.Code != http.StatusOK {
		t.Errorf("renaming: %d %s", rec.Code, rec.Body)
	}

	// It borrows as them, and history records them, not the token.
	tools, err := srv.store.CreateCategoryFrom(store.CategoryInput{Name: "Tools", IsLendable: true})
	if err != nil {
		t.Fatal(err)
	}
	drill, err := srv.store.CreateAsset("Drill", []string{tools.ID}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rec := withBearer(t, srv, secret, http.MethodPost, "/api/v1/assets/"+drill.Asset.ID+"/checkout", `{}`); rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
		t.Fatalf("borrowing: %d %s", rec.Code, rec.Body)
	}
	rec = doJSON(t, srv, adminCookie, http.MethodGet, "/api/v1/assets/"+drill.Asset.ID+"/history", "")
	if !strings.Contains(rec.Body.String(), `"name":"Ada L."`) || strings.Contains(rec.Body.String(), `"token":true`) {
		t.Errorf("history: %s", rec.Body)
	}

	// Never its user's credentials, nor more tokens.
	for _, r := range []struct{ method, path, body string }{
		{http.MethodPut, "/api/v1/auth/password", `{"current_password":"UserSecret123!","new_password":"another-one"}`},
		{http.MethodPost, "/api/v1/auth/tokens", `{"name":"Another"}`},
		{http.MethodGet, "/api/v1/auth/tokens", ""},
		{http.MethodPost, "/api/v1/auth/logout", ""},
	} {
		if rec := withBearer(t, srv, secret, r.method, r.path, r.body); rec.Code != http.StatusForbidden {
			t.Errorf("%s %s with a personal token: %d %s", r.method, r.path, rec.Code, rec.Body)
		}
	}

	// It holds the user's permissions as they are now: a role taken away is
	// gone at the next request.
	if rec := withBearer(t, srv, secret, http.MethodGet, "/api/v1/assets", ""); rec.Code != http.StatusOK {
		t.Fatalf("listing: %d", rec.Code)
	}
	if err := srv.store.SetUserRoles(memberUser.ID, nil); err != nil {
		t.Fatal(err)
	}
	rec = withBearer(t, srv, secret, http.MethodGet, "/api/v1/auth/me", "")
	if strings.Contains(rec.Body.String(), rbac.PermAssetsBorrow) || !strings.Contains(rec.Body.String(), "public:assets.read") {
		t.Errorf("permissions after the role went: %s", rec.Body)
	}
	if rec := withBearer(t, srv, secret, http.MethodPost, "/api/v1/assets/"+drill.Asset.ID+"/checkin", `{}`); rec.Code != http.StatusForbidden {
		t.Errorf("returning after the role went: %d %s", rec.Code, rec.Body)
	}

	// Its user sees it among their own; an admin sees whose it is.
	rec = doJSON(t, srv, member, http.MethodGet, "/api/v1/auth/tokens", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"Claude"`) {
		t.Errorf("own tokens: %d %s", rec.Code, rec.Body)
	}
	rec = doJSON(t, srv, adminCookie, http.MethodGet, "/api/v1/tokens", "")
	if !strings.Contains(rec.Body.String(), `"user_id":"`+memberUser.ID+`"`) || !strings.Contains(rec.Body.String(), `"user_name":"Ada L."`) {
		t.Errorf("all tokens: %s", rec.Body)
	}
	if rec := doJSON(t, srv, adminCookie, http.MethodGet, "/api/v1/auth/tokens", ""); strings.Contains(rec.Body.String(), "Claude") {
		t.Errorf("the admin's own tokens include the member's: %s", rec.Body)
	}
}

func TestRevokingPersonalTokens(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	adminCookie := sessionCookie(t, srv, admin.ID)
	member := userWithPermissions(t, srv, "member@hackspace.org", rbac.PermAssetsRead)
	other := userWithPermissions(t, srv, "other@hackspace.org", rbac.PermAssetsRead)
	memberUser, _ := srv.store.GetUserByEmail("member@hackspace.org")

	token, secret := personalToken(t, srv, member, "Claude")
	// Someone else's is not found; one's own is revoked, and stops at once.
	if rec := doJSON(t, srv, other, http.MethodDelete, "/api/v1/auth/tokens/"+token.ID, ""); rec.Code != http.StatusNotFound {
		t.Errorf("revoking another's: %d", rec.Code)
	}
	if rec := withBearer(t, srv, secret, http.MethodGet, "/api/v1/assets", ""); rec.Code != http.StatusOK {
		t.Fatalf("before revoking: %d", rec.Code)
	}
	if rec := doJSON(t, srv, member, http.MethodDelete, "/api/v1/auth/tokens/"+token.ID, ""); rec.Code != http.StatusOK {
		t.Errorf("revoking one's own: %d %s", rec.Code, rec.Body)
	}
	if rec := withBearer(t, srv, secret, http.MethodGet, "/api/v1/assets", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("after revoking: %d", rec.Code)
	}

	// Whoever manages users revokes anyone's.
	token, secret = personalToken(t, srv, member, "Second")
	if rec := doJSON(t, srv, adminCookie, http.MethodDelete, "/api/v1/tokens/"+token.ID, ""); rec.Code != http.StatusOK {
		t.Errorf("an admin revoking: %d", rec.Code)
	}
	if rec := withBearer(t, srv, secret, http.MethodGet, "/api/v1/assets", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("after an admin revoked it: %d", rec.Code)
	}

	// Deactivating the user stops their tokens for good: turning them back
	// on does not bring an assistant back.
	_, secret = personalToken(t, srv, member, "Third")
	if err := srv.store.UpdateUserStatus(memberUser.ID, false); err != nil {
		t.Fatal(err)
	}
	if rec := withBearer(t, srv, secret, http.MethodGet, "/api/v1/assets", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("with the user deactivated: %d", rec.Code)
	}
	if err := srv.store.UpdateUserStatus(memberUser.ID, true); err != nil {
		t.Fatal(err)
	}
	if rec := withBearer(t, srv, secret, http.MethodGet, "/api/v1/assets", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("with the user active again: %d", rec.Code)
	}

	// A token holding roles is no one: no account endpoints at all.
	roleSecret := tokenWith(t, srv, admin.ID, "Script", rbac.PermAssetsRead)
	for _, r := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/auth/me", ""},
		{http.MethodPut, "/api/v1/auth/name", `{"name":"x"}`},
	} {
		if rec := withBearer(t, srv, roleSecret, r.method, r.path, r.body); rec.Code != http.StatusForbidden {
			t.Errorf("%s %s with a role token: %d", r.method, r.path, rec.Code)
		}
	}
}

func TestMCPActsAsThePerson(t *testing.T) {
	srv, admin, _ := setupTestServer(t)
	member := userWithPermissions(t, srv, "member@hackspace.org", rbac.PermAssetsRead)
	memberUser, _ := srv.store.GetUserByEmail("member@hackspace.org")
	if _, err := srv.store.SetUserName(memberUser.ID, "Ada"); err != nil {
		t.Fatal(err)
	}
	_, secret := personalToken(t, srv, member, "Claude")
	cs := mcpSession(t, srv, secret)

	var me struct {
		Kind, ID, Name, Token string
	}
	structured(t, callTool(t, cs, "whoami", nil), &me)
	if me.Kind != "person" || me.ID != memberUser.ID || me.Name != "Ada" || me.Token != "Claude" {
		t.Errorf("whoami: %+v", me)
	}
	if init := cs.InitializeResult(); !strings.Contains(init.Instructions, `"Ada", through their personal token "Claude"`) {
		t.Errorf("instructions: %s", init.Instructions)
	}
	var named struct{ Name string }
	structured(t, callTool(t, cs, "set_my_name", map[string]any{"name": "Ada Lovelace"}), &named)
	if u, _ := srv.store.GetUserByID(memberUser.ID); named.Name != "Ada Lovelace" || u.Name != "Ada Lovelace" {
		t.Errorf("set_my_name: %+v, stored %q", named, u.Name)
	}

	// A token holding roles is no one to name.
	if names := toolNames(t, mcpSession(t, srv, tokenWith(t, srv, admin.ID, "Script", rbac.PermAssetsRead))); slices.Contains(names, "set_my_name") {
		t.Errorf("a role token lists set_my_name: %v", names)
	}
}
