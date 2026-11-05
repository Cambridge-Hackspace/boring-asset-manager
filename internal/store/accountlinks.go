package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/auth"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/textcheck"
	"github.com/google/uuid"
)

// What an account link is for (docs/access.md#account-emails), and how long
// each lasts.
const (
	LinkInvite         = "invite"
	LinkPasswordReset  = "password_reset"
	LinkFactorRecovery = "factor_recovery"

	InviteTTL         = 7 * 24 * time.Hour
	PasswordResetTTL  = 30 * time.Minute
	FactorRecoveryTTL = 30 * time.Minute
)

// Instance settings for account emails, each "on" or "off", off unless set.
const (
	// PasswordResetSetting offers Forgot password on the sign-in page.
	PasswordResetSetting = "auth.password_reset"
	// FactorRecoverySetting lets someone who has lost their second factor
	// have it removed by a link emailed to them, after their password.
	FactorRecoverySetting = "auth.factor_recovery_email"
	// FactorRecoveryRequiredSetting lets those whose roles require
	// two-factor sign-in do so too.
	FactorRecoveryRequiredSetting = "auth.factor_recovery_email_required"
)

// ErrAccountLinkNotFound is returned for a link that has expired, was used, was
// replaced by a newer one, or was never made.
var ErrAccountLinkNotFound = errors.New("the link has expired, or was used already")

// On reads an on-or-off instance setting, off unless set.
func (s *Store) On(key string) (bool, error) {
	v, err := s.Setting(key, "off")
	return v == "on", err
}

// CreateAccountLink makes a link for a user, answering the token for it;
// only its hash is kept. It replaces their links of the same purpose not
// yet used.
func (s *Store) CreateAccountLink(userID, purpose string, ttl time.Duration) (string, error) {
	token, err := auth.GenerateSessionToken()
	if err != nil {
		return "", err
	}
	now := dbTime(time.Now())
	tx, err := s.DB.Begin()
	if err != nil {
		return "", fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := s.TxExec(tx, "DELETE FROM account_links WHERE expires_at <= ? OR (user_id = ? AND purpose = ? AND used_at IS NULL)", now, userID, purpose); err != nil {
		return "", fmt.Errorf("replacing older links: %w", err)
	}
	if _, err := s.TxExec(tx, "INSERT INTO account_links (id, user_id, purpose, expires_at, created_at) VALUES (?, ?, ?, ?, ?)",
		sessionKey(token), userID, purpose, now.Add(ttl), now); err != nil {
		return "", fmt.Errorf("making the link: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("committing: %w", err)
	}
	return token, nil
}

// AccountLink is the user a link is for, while it works, without using it.
func (s *Store) AccountLink(token, purpose string) (*models.User, error) {
	if token == "" {
		return nil, ErrAccountLinkNotFound
	}
	var userID string
	err := s.QueryRow(`SELECT l.user_id FROM account_links l JOIN users u ON u.id = l.user_id
		WHERE l.id = ? AND l.purpose = ? AND l.used_at IS NULL AND l.expires_at > ? AND u.is_active = TRUE AND u.archived_at IS NULL`,
		sessionKey(token), purpose, dbTime(time.Now())).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAccountLinkNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("reading the link: %w", err)
	}
	return s.GetUserByID(userID)
}

// TakeAccountLink uses a link, answering whom it is for. It works once:
// two taking it at once, one of them is refused.
func (s *Store) TakeAccountLink(token, purpose string) (*models.User, error) {
	u, err := s.AccountLink(token, purpose)
	if err != nil {
		return nil, err
	}
	now := dbTime(time.Now())
	res, err := s.Exec("UPDATE account_links SET used_at = ? WHERE id = ? AND purpose = ? AND used_at IS NULL AND expires_at > ?",
		now, sessionKey(token), purpose, now)
	if err != nil {
		return nil, fmt.Errorf("using the link: %w", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil, ErrAccountLinkNotFound
	}
	return u, nil
}

// CreateInvitedUser makes an account with no password, for someone invited
// to set one or to sign in through a provider.
func (s *Store) CreateInvitedUser(email, name string) (*models.User, error) {
	normalized := auth.NormalizeEmail(email)
	if normalized == "" {
		return nil, fmt.Errorf("%w: email cannot be empty", ErrInvalidUser)
	}
	if err := textcheck.Check("email", normalized, textcheck.MaxName); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidUser, err)
	}
	name, err := normalizeUserName(strings.TrimSpace(name))
	if err != nil {
		return nil, err
	}
	now := dbTime(time.Now())
	id := uuid.NewString()
	if _, err := s.Exec("INSERT INTO users (id, email, name, password_hash, is_active, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		id, normalized, name, NoPassword, true, now, now); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrEmailTaken
		}
		return nil, fmt.Errorf("inserting user: %w", err)
	}
	return s.GetUserByID(id)
}
