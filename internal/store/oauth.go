package store

import (
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/auth"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/textcheck"
	"github.com/google/uuid"
)

// Signing assistants in by OAuth 2.1 (docs/mcp.md): clients register
// themselves, a person approves one, and it trades the code, with the PKCE
// verifier, for a personal token acting as them.

var (
	// ErrInvalidOAuthClient wraps why a registration is refused.
	ErrInvalidOAuthClient = errors.New("invalid client registration")
	// ErrOAuthClientNotFound is a client that is not registered.
	ErrOAuthClientNotFound = errors.New("client not found")
	// ErrInvalidGrant wraps why a code or refresh secret buys nothing.
	ErrInvalidGrant = errors.New("invalid grant")
)

const (
	// OAuthCodeLife is how long a person's approval waits to be redeemed.
	OAuthCodeLife = 10 * time.Minute
	// OAuthAccessLife is how long an access secret works before renewing.
	OAuthAccessLife = time.Hour
	// OAuthRefreshLife is how long a grant lasts unused.
	OAuthRefreshLife = 30 * 24 * time.Hour
	// oauthUnusedClientLife is how long a client nobody approved is kept.
	oauthUnusedClientLife = 24 * time.Hour
	// refreshPrefix starts every refresh secret, telling it from a token.
	refreshPrefix = "bamr_"
)

// validRedirect accepts where an approval may send its code: an https
// address, or http on the machine itself, as a desktop assistant listens.
func validRedirect(raw string) error {
	if len(raw) > 2000 {
		return errors.New("a redirect URI is too long")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Fragment != "" || u.Host == "" || u.User != nil {
		return fmt.Errorf("%q is not an absolute URI without a fragment", raw)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		host := u.Hostname()
		if host == "localhost" {
			return nil
		}
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return nil
		}
	}
	return fmt.Errorf("%q must be https, or http on localhost", raw)
}

// RegisterOAuthClient registers an assistant that may ask people to approve
// it. Clients no one approved within a day are dropped as others register.
func (s *Store) RegisterOAuthClient(name string, redirectURIs []string) (*models.OAuthClient, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "An assistant"
	}
	if err := textcheck.Check("client_name", name, textcheck.MaxName); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidOAuthClient, err)
	}
	if len(redirectURIs) == 0 || len(redirectURIs) > 10 {
		return nil, fmt.Errorf("%w: give 1 to 10 redirect_uris", ErrInvalidOAuthClient)
	}
	for _, u := range redirectURIs {
		if err := validRedirect(u); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidOAuthClient, err)
		}
	}
	now := time.Now().UTC()
	if _, err := s.Exec("DELETE FROM oauth_clients WHERE created_at < ? AND id NOT IN (SELECT oauth_client_id FROM api_tokens WHERE oauth_client_id IS NOT NULL)",
		now.Add(-oauthUnusedClientLife)); err != nil {
		return nil, fmt.Errorf("dropping unused clients: %w", err)
	}
	raw, _ := json.Marshal(redirectURIs)
	c := &models.OAuthClient{ID: uuid.NewString(), Name: name, RedirectURIs: redirectURIs, CreatedAt: now}
	if _, err := s.Exec("INSERT INTO oauth_clients (id, name, redirect_uris, created_at) VALUES (?, ?, ?, ?)", c.ID, c.Name, string(raw), now); err != nil {
		return nil, fmt.Errorf("registering the client: %w", err)
	}
	return c, nil
}

// GetOAuthClient returns a registered client, or ErrOAuthClientNotFound.
func (s *Store) GetOAuthClient(id string) (*models.OAuthClient, error) {
	var (
		c   models.OAuthClient
		raw string
	)
	err := s.QueryRow("SELECT id, name, redirect_uris, created_at FROM oauth_clients WHERE id = ?", id).Scan(&c.ID, &c.Name, &raw, &c.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrOAuthClientNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("reading the client: %w", err)
	}
	if err := json.Unmarshal([]byte(raw), &c.RedirectURIs); err != nil {
		return nil, fmt.Errorf("reading the client's redirect URIs: %w", err)
	}
	return &c, nil
}

// CreateOAuthCode records a person's approval of a client, to be redeemed
// once, within OAuthCodeLife, with the PKCE verifier of challenge (S256).
func (s *Store) CreateOAuthCode(clientID, userID, redirectURI, challenge string) (string, error) {
	random, err := auth.GenerateSessionToken()
	if err != nil {
		return "", err
	}
	_, err = s.Exec("INSERT INTO oauth_codes (code_hash, client_id, user_id, redirect_uri, challenge, expires_at) VALUES (?, ?, ?, ?, ?, ?)",
		sessionKey(random), clientID, userID, redirectURI, challenge, time.Now().UTC().Add(OAuthCodeLife))
	if err != nil {
		return "", fmt.Errorf("keeping the approval: %w", err)
	}
	return random, nil
}

// RedeemOAuthCode trades an approval for a grant: a personal token for the
// person who approved, named after the client, with its access secret and a
// refresh secret. A code works once: a second try, or one with the wrong
// client, redirect URI or verifier, is ErrInvalidGrant, and spends it.
func (s *Store) RedeemOAuthCode(code, clientID, redirectURI, verifier string) (*models.APIToken, string, string, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, "", "", err
	}
	defer tx.Rollback()
	var (
		gotClient, userID, gotRedirect, challenge string
		expires                                   time.Time
	)
	err = s.TxQueryRow(tx, "SELECT client_id, user_id, redirect_uri, challenge, expires_at FROM oauth_codes WHERE code_hash = ?", sessionKey(code)).
		Scan(&gotClient, &userID, &gotRedirect, &challenge, &expires)
	if err == sql.ErrNoRows {
		return nil, "", "", fmt.Errorf("%w: the code is not known, or was used", ErrInvalidGrant)
	}
	if err != nil {
		return nil, "", "", fmt.Errorf("reading the code: %w", err)
	}
	if _, err := s.TxExec(tx, "DELETE FROM oauth_codes WHERE code_hash = ?", sessionKey(code)); err != nil {
		return nil, "", "", fmt.Errorf("spending the code: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, "", "", err
	}
	switch {
	case gotClient != clientID:
		return nil, "", "", fmt.Errorf("%w: the code was given to another client", ErrInvalidGrant)
	case gotRedirect != redirectURI:
		return nil, "", "", fmt.Errorf("%w: redirect_uri is not the one approved", ErrInvalidGrant)
	case time.Now().After(expires):
		return nil, "", "", fmt.Errorf("%w: the code expired", ErrInvalidGrant)
	case !pkceMatches(verifier, challenge):
		return nil, "", "", fmt.Errorf("%w: code_verifier does not match the challenge", ErrInvalidGrant)
	}
	if err := s.activeUser(userID); err != nil {
		return nil, "", "", err
	}
	client, err := s.GetOAuthClient(clientID)
	if err != nil {
		return nil, "", "", fmt.Errorf("%w: the client is gone", ErrInvalidGrant)
	}
	access := time.Now().UTC().Add(OAuthAccessLife)
	t, secret, err := s.createToken(client.Name, nil, &access, userID, userID)
	if err != nil {
		return nil, "", "", err
	}
	refresh, err := s.newRefresh(t.ID, clientID)
	if err != nil {
		return nil, "", "", err
	}
	t, err = s.GetToken(t.ID)
	return t, secret, refresh, err
}

// RefreshOAuthGrant renews a grant with its refresh secret: a new access
// secret and a new refresh secret, the old ones no longer working.
func (s *Store) RefreshOAuthGrant(refresh, clientID string) (*models.APIToken, string, string, error) {
	var (
		id, gotClient, userID string
		until                 *time.Time
		revoked               *time.Time
	)
	err := s.QueryRow("SELECT id, oauth_client_id, user_id, refresh_expires_at, revoked_at FROM api_tokens WHERE refresh_hash = ?", sessionKey(refresh)).
		Scan(&id, &gotClient, &userID, &until, &revoked)
	switch {
	case err == sql.ErrNoRows:
		return nil, "", "", fmt.Errorf("%w: the refresh token is not known, or was replaced", ErrInvalidGrant)
	case err != nil:
		return nil, "", "", fmt.Errorf("reading the grant: %w", err)
	case gotClient != clientID:
		return nil, "", "", fmt.Errorf("%w: the grant is another client's", ErrInvalidGrant)
	case revoked != nil:
		return nil, "", "", fmt.Errorf("%w: the grant was revoked", ErrInvalidGrant)
	case until == nil || time.Now().After(*until):
		return nil, "", "", fmt.Errorf("%w: the grant expired unused", ErrInvalidGrant)
	}
	if err := s.activeUser(userID); err != nil {
		return nil, "", "", err
	}
	random, err := auth.GenerateSessionToken()
	if err != nil {
		return nil, "", "", err
	}
	secret := TokenPrefix + random
	res, err := s.Exec("UPDATE api_tokens SET token_hash = ?, expires_at = ? WHERE id = ? AND refresh_hash = ? AND revoked_at IS NULL",
		sessionKey(secret), time.Now().UTC().Add(OAuthAccessLife), id, sessionKey(refresh))
	if err != nil {
		return nil, "", "", fmt.Errorf("renewing the grant: %w", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil, "", "", fmt.Errorf("%w: the refresh token was used meanwhile", ErrInvalidGrant)
	}
	next, err := s.newRefresh(id, clientID)
	if err != nil {
		return nil, "", "", err
	}
	t, err := s.GetToken(id)
	return t, secret, next, err
}

// newRefresh gives a grant a new refresh secret, good for OAuthRefreshLife.
func (s *Store) newRefresh(tokenID, clientID string) (string, error) {
	random, err := auth.GenerateSessionToken()
	if err != nil {
		return "", err
	}
	refresh := refreshPrefix + random
	_, err = s.Exec("UPDATE api_tokens SET oauth_client_id = ?, refresh_hash = ?, refresh_expires_at = ? WHERE id = ?",
		clientID, sessionKey(refresh), time.Now().UTC().Add(OAuthRefreshLife), tokenID)
	if err != nil {
		return "", fmt.Errorf("keeping the refresh token: %w", err)
	}
	return refresh, nil
}

// activeUser is ErrInvalidGrant for a user who is deactivated or archived.
func (s *Store) activeUser(id string) error {
	u, err := s.GetUserByID(id)
	if errors.Is(err, ErrUserNotFound) || err == nil && (!u.IsActive || u.ArchivedAt != nil) {
		return fmt.Errorf("%w: the account is turned off", ErrInvalidGrant)
	}
	return err
}

// pkceMatches checks a PKCE verifier against an S256 challenge (RFC 7636):
// 43 to 128 unreserved characters, whose SHA-256, base64url without
// padding, is the challenge.
func pkceMatches(verifier, challenge string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	for _, r := range verifier {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || strings.ContainsRune("-._~", r)) {
			return false
		}
	}
	sum := sha256.Sum256([]byte(verifier))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(want), []byte(challenge)) == 1
}
