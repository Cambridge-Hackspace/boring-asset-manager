package store

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/auth"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/textcheck"
	"github.com/google/uuid"
)

var (
	// ErrTokenNotFound is returned for a token that does not exist, or, when
	// authenticating, one that is revoked or expired.
	ErrTokenNotFound = errors.New("token not found")
	// ErrInvalidToken wraps the reason a token cannot be created as given.
	ErrInvalidToken = errors.New("invalid token")
)

// TokenPrefix starts every API token's secret, so one is easy to recognize
// in a script or a leak report.
const TokenPrefix = "bam_"

// tokenUseInterval is how often a token's last use is written: at most once
// a minute, so a busy script does not write on every request.
const tokenUseInterval = time.Minute

const tokenColumns = "id, name, created_by, created_at, expires_at, last_used_at, revoked_at"

func scanToken(row interface{ Scan(...any) error }) (models.APIToken, error) {
	var (
		t         models.APIToken
		createdBy sql.NullString
	)
	err := row.Scan(&t.ID, &t.Name, &createdBy, &t.CreatedAt, &t.ExpiresAt, &t.LastUsedAt, &t.RevokedAt)
	t.CreatedBy = createdBy.String
	t.RoleIDs = []string{}
	return t, err
}

// CreateToken creates a token holding the roles given, made by createdBy,
// and returns it with its secret, which is shown once and not stored. A nil
// expiresAt never expires.
func (s *Store) CreateToken(name string, roleIDs []string, expiresAt *time.Time, createdBy string) (*models.APIToken, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, "", fmt.Errorf("%w: a name is required", ErrInvalidToken)
	}
	if err := textcheck.Check("token name", name, textcheck.MaxName); err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	now := time.Now().UTC()
	if expiresAt != nil {
		if !expiresAt.After(now) {
			return nil, "", fmt.Errorf("%w: it would expire before it is made", ErrInvalidToken)
		}
		at := expiresAt.UTC()
		expiresAt = &at
	}
	random, err := auth.GenerateSessionToken()
	if err != nil {
		return nil, "", err
	}
	secret := TokenPrefix + random
	id := uuid.NewString()

	tx, err := s.DB.Begin()
	if err != nil {
		return nil, "", fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	var by any
	if createdBy != "" {
		by = createdBy
	}
	_, err = s.TxExec(tx, "INSERT INTO api_tokens (id, name, token_hash, created_by, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?)",
		id, name, sessionKey(secret), by, now, expiresAt)
	if err != nil {
		return nil, "", fmt.Errorf("inserting token: %w", err)
	}
	seen := map[string]bool{}
	for _, roleID := range roleIDs {
		if seen[roleID] {
			continue
		}
		seen[roleID] = true
		var n int
		if err := s.TxQueryRow(tx, "SELECT COUNT(*) FROM roles WHERE id = ?", roleID).Scan(&n); err != nil {
			return nil, "", fmt.Errorf("checking role: %w", err)
		}
		if n == 0 {
			return nil, "", fmt.Errorf("%w: %s", ErrRoleNotFound, roleID)
		}
		if _, err := s.TxExec(tx, "INSERT INTO api_token_roles (token_id, role_id) VALUES (?, ?)", id, roleID); err != nil {
			return nil, "", fmt.Errorf("assigning role: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, "", fmt.Errorf("committing: %w", err)
	}
	t, err := s.GetToken(id)
	return t, secret, err
}

// ListTokens returns every token, revoked ones included, newest first.
func (s *Store) ListTokens() ([]models.APIToken, error) {
	rows, err := s.Query("SELECT " + tokenColumns + " FROM api_tokens ORDER BY created_at DESC, id")
	if err != nil {
		return nil, fmt.Errorf("listing tokens: %w", err)
	}
	tokens := []models.APIToken{}
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			rows.Close()
			return nil, fmt.Errorf("scanning token: %w", err)
		}
		tokens = append(tokens, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	roles, err := s.tokenRoleIDs()
	if err != nil {
		return nil, err
	}
	for i := range tokens {
		if ids := roles[tokens[i].ID]; ids != nil {
			tokens[i].RoleIDs = ids
		}
	}
	return tokens, nil
}

// GetToken returns a token, or ErrTokenNotFound.
func (s *Store) GetToken(id string) (*models.APIToken, error) {
	t, err := scanToken(s.QueryRow("SELECT "+tokenColumns+" FROM api_tokens WHERE id = ?", id))
	if err == sql.ErrNoRows {
		return nil, ErrTokenNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("reading token: %w", err)
	}
	roles, err := s.tokenRoleIDs()
	if err != nil {
		return nil, err
	}
	if ids := roles[id]; ids != nil {
		t.RoleIDs = ids
	}
	return &t, nil
}

// tokenRoleIDs maps each token with roles to their IDs, sorted.
func (s *Store) tokenRoleIDs() (map[string][]string, error) {
	rows, err := s.Query("SELECT token_id, role_id FROM api_token_roles")
	if err != nil {
		return nil, fmt.Errorf("reading token roles: %w", err)
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var tokenID, roleID string
		if err := rows.Scan(&tokenID, &roleID); err != nil {
			return nil, fmt.Errorf("scanning token role: %w", err)
		}
		out[tokenID] = append(out[tokenID], roleID)
	}
	for _, ids := range out {
		sort.Strings(ids)
	}
	return out, rows.Err()
}

// RevokeToken stops a token working for good. Revoking one already revoked
// changes nothing.
func (s *Store) RevokeToken(id string) (*models.APIToken, error) {
	if _, err := s.Exec("UPDATE api_tokens SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL", time.Now().UTC(), id); err != nil {
		return nil, fmt.Errorf("revoking token: %w", err)
	}
	return s.GetToken(id)
}

// AuthenticateToken finds the token with this secret and the permissions
// its roles grant. A token that is unknown, revoked or expired is
// ErrTokenNotFound. Its last use is recorded, at most once a minute.
func (s *Store) AuthenticateToken(secret string) (*models.APIToken, map[string]bool, error) {
	if !strings.HasPrefix(secret, TokenPrefix) {
		return nil, nil, ErrTokenNotFound
	}
	now := time.Now().UTC()
	t, err := scanToken(s.QueryRow("SELECT "+tokenColumns+" FROM api_tokens WHERE token_hash = ? AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?)",
		sessionKey(secret), now))
	if err == sql.ErrNoRows {
		return nil, nil, ErrTokenNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("reading token: %w", err)
	}
	if t.LastUsedAt == nil || now.Sub(*t.LastUsedAt) >= tokenUseInterval {
		if _, err := s.Exec("UPDATE api_tokens SET last_used_at = ? WHERE id = ?", now, t.ID); err != nil {
			return nil, nil, fmt.Errorf("recording token use: %w", err)
		}
		t.LastUsedAt = &now
	}
	perms, err := grantedKeys(storeQuerier{s}, "SELECT role_id FROM api_token_roles WHERE token_id = ?", t.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("reading token permissions: %w", err)
	}
	return &t, perms, nil
}

// AsToken returns a view of the store whose changes are attributed to an
// API token, as As attributes them to a user.
func (s *Store) AsToken(tokenID string) *Store {
	c := *s
	c.actor, c.actorToken = "", tokenID
	return &c
}
