package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/auth"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/google/uuid"
)

// Second factors (docs/access.md): each person's ways of proving it is them
// beyond their password, and the step between the two at sign-in.

// FactorTOTP is an authenticator app's codes.
const FactorTOTP = "totp"

// SignInChallengeTTL is how long the second step of a sign-in lasts.
const SignInChallengeTTL = 5 * time.Minute

var (
	ErrFactorNotFound    = errors.New("second factor not found")
	ErrChallengeNotFound = errors.New("the sign-in has expired; sign in again")
	// ErrCodeUsed is returned for a code whose step was accepted already,
	// or by another server at the same moment.
	ErrCodeUsed = errors.New("that code was used already; wait for the next one")
)

// Factor is a second factor as it is listed: never its secret.
type Factor struct {
	ID          string     `json:"id"`
	Kind        string     `json:"kind"`
	CreatedAt   time.Time  `json:"created_at"`
	ConfirmedAt *time.Time `json:"confirmed_at,omitempty"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
}

// SealedFactor is a factor with its secret, still sealed, and the last
// step its codes were accepted at.
type SealedFactor struct {
	Factor
	Secret   string
	LastStep int64
}

const factorColumns = "id, kind, created_at, confirmed_at, last_used_at, secret, last_step"

func scanFactor(row interface{ Scan(...any) error }) (*SealedFactor, error) {
	var f SealedFactor
	if err := row.Scan(&f.ID, &f.Kind, &f.CreatedAt, &f.ConfirmedAt, &f.LastUsedAt, &f.Secret, &f.LastStep); err != nil {
		return nil, err
	}
	return &f, nil
}

// FactorPurpose is what a factor's secret is sealed for: that factor alone.
func FactorPurpose(id string) string { return "factor:" + id }

// StartTOTP adds a TOTP factor being set up, its secret sealed by seal,
// replacing any of the user's not confirmed.
func (s *Store) StartTOTP(userID string, seal func(id string) (string, error)) (*Factor, error) {
	id := uuid.NewString()
	sealed, err := seal(id)
	if err != nil {
		return nil, err
	}
	now := dbTime(time.Now())
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := s.TxExec(tx, "DELETE FROM user_factors WHERE user_id = ? AND confirmed_at IS NULL", userID); err != nil {
		return nil, fmt.Errorf("dropping a factor not set up: %w", err)
	}
	if _, err := s.TxExec(tx, "INSERT INTO user_factors (id, user_id, kind, secret, last_step, created_at) VALUES (?, ?, ?, ?, 0, ?)",
		id, userID, FactorTOTP, sealed, now); err != nil {
		return nil, fmt.Errorf("adding the factor: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing: %w", err)
	}
	return &Factor{ID: id, Kind: FactorTOTP, CreatedAt: now}, nil
}

// PendingTOTP is the user's TOTP factor being set up.
func (s *Store) PendingTOTP(userID string) (*SealedFactor, error) {
	f, err := scanFactor(s.QueryRow("SELECT "+factorColumns+" FROM user_factors WHERE user_id = ? AND kind = ? AND confirmed_at IS NULL",
		userID, FactorTOTP))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrFactorNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("reading the factor: %w", err)
	}
	return f, nil
}

// ConfirmTOTP finishes setting a factor up, with the step of the code that
// confirmed it, which cannot be used again.
func (s *Store) ConfirmTOTP(userID, id string, step int64) (*Factor, error) {
	now := dbTime(time.Now())
	res, err := s.Exec("UPDATE user_factors SET confirmed_at = ?, last_used_at = ?, last_step = ? WHERE id = ? AND user_id = ? AND confirmed_at IS NULL",
		now, now, step, id, userID)
	if err != nil {
		return nil, fmt.Errorf("confirming the factor: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrFactorNotFound
	}
	return &Factor{ID: id, Kind: FactorTOTP, CreatedAt: now, ConfirmedAt: &now, LastUsedAt: &now}, nil
}

// Factors lists a user's confirmed second factors, oldest first.
func (s *Store) Factors(userID string) ([]Factor, error) {
	sealed, err := s.SealedFactors(userID)
	if err != nil {
		return nil, err
	}
	out := make([]Factor, len(sealed))
	for i, f := range sealed {
		out[i] = f.Factor
	}
	return out, nil
}

// SealedFactors lists a user's confirmed second factors with their sealed
// secrets, for checking a code.
func (s *Store) SealedFactors(userID string) ([]SealedFactor, error) {
	rows, err := s.Query("SELECT "+factorColumns+" FROM user_factors WHERE user_id = ? AND confirmed_at IS NOT NULL ORDER BY created_at, id", userID)
	if err != nil {
		return nil, fmt.Errorf("listing factors: %w", err)
	}
	defer rows.Close()
	out := []SealedFactor{}
	for rows.Next() {
		f, err := scanFactor(rows)
		if err != nil {
			return nil, fmt.Errorf("reading a factor: %w", err)
		}
		out = append(out, *f)
	}
	return out, rows.Err()
}

// HasFactors reports, by user ID, who has a confirmed second factor.
func (s *Store) HasFactors() (map[string]bool, error) {
	rows, err := s.Query("SELECT DISTINCT user_id FROM user_factors WHERE confirmed_at IS NOT NULL")
	if err != nil {
		return nil, fmt.Errorf("listing who has factors: %w", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// UseTOTPStep records that a factor's code at step was accepted. Only a
// step later than the last one counts, checked as it is written, so that
// two servers given the same code at once accept it once.
func (s *Store) UseTOTPStep(id string, step int64) error {
	now := dbTime(time.Now())
	res, err := s.Exec("UPDATE user_factors SET last_step = ?, last_used_at = ? WHERE id = ? AND last_step < ?", step, now, id, step)
	if err != nil {
		return fmt.Errorf("recording the code: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrCodeUsed
	}
	return nil
}

// DeleteFactor removes one of a user's own factors.
func (s *Store) DeleteFactor(userID, id string) error {
	res, err := s.Exec("DELETE FROM user_factors WHERE id = ? AND user_id = ?", id, userID)
	if err != nil {
		return fmt.Errorf("removing the factor: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrFactorNotFound
	}
	return nil
}

// ResetFactors removes all of a user's second factors and signs them out
// everywhere, as an admin does for someone who has lost theirs, recording
// auth.factors_reset in history as done by the store's actor. It reports
// how many factors there were.
func (s *Store) ResetFactors(userID string) (int, error) {
	u, err := s.GetUserByID(userID)
	if err != nil {
		return 0, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	res, err := s.TxExec(tx, "DELETE FROM user_factors WHERE user_id = ?", userID)
	if err != nil {
		return 0, fmt.Errorf("removing factors: %w", err)
	}
	n, _ := res.RowsAffected()
	if _, err := s.TxExec(tx, "DELETE FROM sessions WHERE user_id = ?", userID); err != nil {
		return 0, fmt.Errorf("signing them out: %w", err)
	}
	if _, err := s.TxExec(tx, "DELETE FROM sign_in_challenges WHERE user_id = ?", userID); err != nil {
		return 0, fmt.Errorf("ending sign-ins under way: %w", err)
	}
	account := &models.EventActor{ID: u.ID, Name: u.Name}
	if err := s.insertEvent(tx, models.EventAuthFactorsReset, nil, nil, models.EventPayload{Trigger: models.TriggerManual, Account: account}); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("committing: %w", err)
	}
	return int(n), nil
}

// CreateSignInChallenge starts the second step of a sign-in for a user whose
// password was accepted. The token returned is for the browser's cookie;
// only its hash is stored.
func (s *Store) CreateSignInChallenge(userID string) (string, error) {
	token, err := auth.GenerateSessionToken()
	if err != nil {
		return "", err
	}
	now := dbTime(time.Now())
	if _, err := s.Exec("DELETE FROM sign_in_challenges WHERE expires_at <= ?", now); err != nil {
		return "", fmt.Errorf("forgetting expired sign-ins: %w", err)
	}
	if _, err := s.Exec("INSERT INTO sign_in_challenges (id, user_id, expires_at, created_at) VALUES (?, ?, ?, ?)",
		sessionKey(token), userID, now.Add(SignInChallengeTTL), now); err != nil {
		return "", fmt.Errorf("starting the second step: %w", err)
	}
	return token, nil
}

// SignInChallenge returns the user a second step is for, while it lasts and
// they may still sign in.
func (s *Store) SignInChallenge(token string) (*models.User, error) {
	if token == "" {
		return nil, ErrChallengeNotFound
	}
	var userID string
	err := s.QueryRow("SELECT c.user_id FROM sign_in_challenges c JOIN users u ON u.id = c.user_id WHERE c.id = ? AND c.expires_at > ? AND u.is_active = TRUE AND u.archived_at IS NULL",
		sessionKey(token), dbTime(time.Now())).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrChallengeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("reading the sign-in: %w", err)
	}
	return s.GetUserByID(userID)
}

// EndSignInChallenge forgets a second step, once it has led to a session.
func (s *Store) EndSignInChallenge(token string) error {
	if _, err := s.Exec("DELETE FROM sign_in_challenges WHERE id = ?", sessionKey(token)); err != nil {
		return fmt.Errorf("ending the sign-in: %w", err)
	}
	return nil
}
