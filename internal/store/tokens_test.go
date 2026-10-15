package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

func TestAPITokens(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	creator, err := st.CreateUser("ada@hackspace.org", "a long enough password", true)
	if err != nil {
		t.Fatal(err)
	}
	counters, err := st.CreateRole(RoleInput{Name: "Counters", Permissions: []string{rbac.PermAssetsRead, rbac.PermStockRecord}})
	if err != nil {
		t.Fatal(err)
	}

	tok, secret, err := st.CreateToken(" Dashboard ", []string{counters.ID, counters.ID}, nil, creator.ID)
	if err != nil {
		t.Fatal(err)
	}
	if tok.Name != "Dashboard" || len(tok.RoleIDs) != 1 || tok.CreatedBy != creator.ID || tok.ExpiresAt != nil || !strings.HasPrefix(secret, TokenPrefix) || len(secret) != len(TokenPrefix)+64 {
		t.Fatalf("created %+v with %q", tok, secret)
	}
	// Only the hash is stored.
	var stored string
	if err := st.QueryRow("SELECT token_hash FROM api_tokens WHERE id = ?", tok.ID).Scan(&stored); err != nil || strings.Contains(stored, secret[len(TokenPrefix):]) {
		t.Errorf("stored %q, %v", stored, err)
	}

	for what, try := range map[string]func() error{
		"no name":     func() error { _, _, err := st.CreateToken(" ", nil, nil, ""); return err },
		"a long name": func() error { _, _, err := st.CreateToken(strings.Repeat("t", 256), nil, nil, ""); return err },
		"a past expiry": func() error {
			past := time.Now().Add(-time.Hour)
			_, _, err := st.CreateToken("Old", nil, &past, "")
			return err
		},
		"an unknown role": func() error { _, _, err := st.CreateToken("Odd", []string{"no-such-role"}, nil, ""); return err },
	} {
		if err := try(); !errors.Is(err, ErrInvalidToken) && !errors.Is(err, ErrRoleNotFound) {
			t.Errorf("%s: %v", what, err)
		}
	}

	// Authenticating gives its roles' permissions and records its use.
	got, perms, err := st.AuthenticateToken(secret)
	if err != nil || got.ID != tok.ID || !perms[rbac.PermStockRecord] || perms[rbac.PermAssetsWrite] || got.LastUsedAt == nil {
		t.Fatalf("authenticated %+v %v, %v", got, perms, err)
	}
	for _, wrong := range []string{"", "bam_", secret + "x", strings.TrimPrefix(secret, TokenPrefix), strings.ToUpper(secret)} {
		if _, _, err := st.AuthenticateToken(wrong); !errors.Is(err, ErrTokenNotFound) {
			t.Errorf("%q: %v", wrong, err)
		}
	}

	// Its changes are its own in history, by name.
	id := stocked(t, st, "Screws", 10, "", nil)
	if _, err := st.AsToken(tok.ID).AdjustQuantity(id, -1, nil); err != nil {
		t.Fatal(err)
	}
	if a := history(t, st, id)[0].Payload.Actor; a == nil || !a.Token || a.ID != tok.ID || a.Name != "Dashboard" || a.Email != "" {
		t.Errorf("actor %+v", a)
	}

	// Its permissions follow its roles.
	if _, err := st.UpdateRole(counters.ID, RoleInput{Name: "Counters", Permissions: []string{rbac.PermAssetsRead}}); err != nil {
		t.Fatal(err)
	}
	if _, perms, _ := st.AuthenticateToken(secret); perms[rbac.PermStockRecord] {
		t.Errorf("still has stock:record: %v", perms)
	}
	if err := st.DeleteRole(counters.ID); err != nil {
		t.Fatal(err)
	}
	if got, perms, err := st.AuthenticateToken(secret); err != nil || len(perms) != 0 || len(got.RoleIDs) != 0 {
		t.Errorf("after its role went: %+v %v, %v", got, perms, err)
	}

	// Revoked, it stops working but stays listed.
	revoked, err := st.RevokeToken(tok.ID)
	if err != nil || revoked.RevokedAt == nil {
		t.Fatalf("revoked %+v, %v", revoked, err)
	}
	if _, _, err := st.AuthenticateToken(secret); !errors.Is(err, ErrTokenNotFound) {
		t.Errorf("a revoked token: %v", err)
	}
	if again, err := st.RevokeToken(tok.ID); err != nil || !again.RevokedAt.Equal(*revoked.RevokedAt) {
		t.Errorf("revoking twice: %+v, %v", again, err)
	}
	if _, err := st.RevokeToken("no-such-token"); !errors.Is(err, ErrTokenNotFound) {
		t.Errorf("a missing token: %v", err)
	}
	if list, err := st.ListTokens(); err != nil || len(list) != 1 || list[0].RevokedAt == nil {
		t.Errorf("listed %+v, %v", list, err)
	}

	// An expired token stops working.
	soon := time.Now().Add(time.Hour)
	_, secret, err = st.CreateToken("Short-lived", nil, &soon, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.AuthenticateToken(secret); err != nil {
		t.Fatalf("before it expires: %v", err)
	}
	if _, err := st.Exec("UPDATE api_tokens SET expires_at = ? WHERE name = ?", time.Now().UTC().Add(-time.Second), "Short-lived"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.AuthenticateToken(secret); !errors.Is(err, ErrTokenNotFound) {
		t.Errorf("an expired token: %v", err)
	}
}
