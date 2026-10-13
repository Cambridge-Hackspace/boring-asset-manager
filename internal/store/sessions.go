package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/auth"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

var (
	ErrSessionNotFound = errors.New("session not found or expired")
)

// CreateSession generates a new secure session token for an active user and persists it.
func (s *Store) CreateSession(userID string, ttl time.Duration) (*models.Session, error) {
	token, err := auth.GenerateSessionToken()
	if err != nil {
		return nil, fmt.Errorf("generating session token: %w", err)
	}

	now := time.Now().UTC()
	expiresAt := now.Add(ttl)

	query := `INSERT INTO sessions (id, user_id, expires_at, created_at) VALUES (?, ?, ?, ?)`
	_, err = s.DB.Exec(query, token, userID, expiresAt, now)
	if err != nil {
		return nil, fmt.Errorf("inserting session: %w", err)
	}

	return &models.Session{
		ID:        token,
		UserID:    userID,
		ExpiresAt: expiresAt,
		CreatedAt: now,
	}, nil
}

// GetSession retrieves an active session by token, validating expiration and active user status.
func (s *Store) GetSession(token string) (*models.Session, *models.User, error) {
	if token == "" {
		return nil, nil, ErrSessionNotFound
	}

	now := time.Now().UTC()
	query := `
		SELECT 
			s.id, s.user_id, s.expires_at, s.created_at,
			u.id, u.email, u.password_hash, u.totp_secret, u.is_active, u.created_at, u.updated_at, u.archived_at
		FROM sessions s
		JOIN users u ON s.user_id = u.id
		WHERE s.id = ? AND s.expires_at > ? AND u.is_active = 1 AND u.archived_at IS NULL
	`

	var (
		sess models.Session
		u    models.User
	)

	err := s.DB.QueryRow(query, token, now).Scan(
		&sess.ID, &sess.UserID, &sess.ExpiresAt, &sess.CreatedAt,
		&u.ID, &u.Email, &u.PasswordHash, &u.TOTPSecret, &u.IsActive, &u.CreatedAt, &u.UpdatedAt, &u.ArchivedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("querying session: %w", err)
	}

	return &sess, &u, nil
}

// DeleteSession revokes a session by its token.
func (s *Store) DeleteSession(token string) error {
	query := `DELETE FROM sessions WHERE id = ?`
	_, err := s.DB.Exec(query, token)
	if err != nil {
		return fmt.Errorf("deleting session: %w", err)
	}
	return nil
}

// DeleteUserSessions revokes all sessions for a specific user (e.g., upon password reset or deactivation).
func (s *Store) DeleteUserSessions(userID string) error {
	query := `DELETE FROM sessions WHERE user_id = ?`
	_, err := s.DB.Exec(query, userID)
	if err != nil {
		return fmt.Errorf("deleting user sessions: %w", err)
	}
	return nil
}

// CleanExpiredSessions removes all sessions past their expiry timestamp.
func (s *Store) CleanExpiredSessions() (int64, error) {
	now := time.Now().UTC()
	res, err := s.DB.Exec("DELETE FROM sessions WHERE expires_at <= ?", now)
	if err != nil {
		return 0, fmt.Errorf("cleaning expired sessions: %w", err)
	}
	return res.RowsAffected()
}
