package store

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestPKCE(t *testing.T) {
	// RFC 7636, appendix B.
	if !pkceMatches("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk", "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM") {
		t.Error("the RFC's example does not match")
	}
	for _, bad := range []string{"short", strings.Repeat("a", 129), strings.Repeat("ä", 43)} {
		sum := sha256.Sum256([]byte(bad))
		if pkceMatches(bad, base64.RawURLEncoding.EncodeToString(sum[:])) {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestOAuthGrants(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	u, err := st.CreateUser("ada@hackspace.org", "a long enough password", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]string{nil, {"http://evil.example/cb"}, {"ftp://x.example/"}, {"https://x.example/cb#f"}} {
		if _, err := st.RegisterOAuthClient("x", bad); !errors.Is(err, ErrInvalidOAuthClient) {
			t.Errorf("%v: %v", bad, err)
		}
	}
	c, err := st.RegisterOAuthClient("  ", []string{"http://localhost:9/cb", "https://claude.ai/api/mcp/auth_callback"})
	if err != nil || c.Name != "An assistant" {
		t.Fatalf("%v %+v", err, c)
	}
	verifier := strings.Repeat("v", 43)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	code, err := st.CreateOAuthCode(c.ID, u.ID, "http://localhost:9/cb", challenge)
	if err != nil {
		t.Fatal(err)
	}
	tok, access, refresh, err := st.RedeemOAuthCode(code, c.ID, "http://localhost:9/cb", verifier)
	if err != nil || tok.UserID != u.ID || tok.OAuthClient != "An assistant" || tok.RefreshExpiresAt == nil || tok.ExpiresAt == nil || time.Until(*tok.ExpiresAt) > OAuthAccessLife {
		t.Fatalf("redeemed: %v %+v", err, tok)
	}
	if _, _, _, err := st.RedeemOAuthCode(code, c.ID, "http://localhost:9/cb", verifier); !errors.Is(err, ErrInvalidGrant) {
		t.Errorf("twice: %v", err)
	}
	if got, _, err := st.AuthenticateToken(access); err != nil || got.ID != tok.ID {
		t.Errorf("access: %v", err)
	}
	_, access2, refresh2, err := st.RefreshOAuthGrant(refresh, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.AuthenticateToken(access); !errors.Is(err, ErrTokenNotFound) {
		t.Errorf("the old access: %v", err)
	}
	if _, _, err := st.AuthenticateToken(access2); err != nil {
		t.Errorf("the new access: %v", err)
	}
	if _, _, _, err := st.RefreshOAuthGrant(refresh, c.ID); !errors.Is(err, ErrInvalidGrant) {
		t.Errorf("the old refresh: %v", err)
	}
	if _, _, _, err := st.RefreshOAuthGrant(refresh2, "another"); !errors.Is(err, ErrInvalidGrant) {
		t.Errorf("another client: %v", err)
	}
	if err := st.UpdateUserStatus(u.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := st.RefreshOAuthGrant(refresh2, c.ID); !errors.Is(err, ErrInvalidGrant) {
		t.Errorf("for a user turned off: %v", err)
	}

	// A client no one approved goes after a day; one approved stays.
	unused, err := st.RegisterOAuthClient("Unused", []string{"https://x.example/cb"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Exec("UPDATE oauth_clients SET created_at = ?", time.Now().UTC().Add(-48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RegisterOAuthClient("Next", []string{"https://x.example/cb"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetOAuthClient(unused.ID); !errors.Is(err, ErrOAuthClientNotFound) {
		t.Errorf("an unused client stayed: %v", err)
	}
	if _, err := st.GetOAuthClient(c.ID); err != nil {
		t.Errorf("an approved client went: %v", err)
	}
}
