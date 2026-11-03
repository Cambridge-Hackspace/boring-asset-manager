package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/auth"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// Sign-in throttling (#246, docs/access.md). Failed passwords are counted
// against the account (by email, whether or not it exists, so throttling
// says nothing about which do) and against the client address. An account
// past AccountFailuresAllowed waits longer after each further failure, up to
// MaxAccountWait, rather than being locked, so nobody can lock an admin out
// by guessing badly. An address past AddressFailuresAllowed, across any
// accounts, waits out ThrottleWindow.
const (
	AccountFailuresAllowed = 5
	AddressFailuresAllowed = 20
	ThrottleWindow         = 15 * time.Minute
	MaxAccountWait         = 15 * time.Minute
	// throttleRowsKept is how long a lapsed row stays before it is pruned.
	throttleRowsKept = 24 * time.Hour
)

// signInLock serializes counting failures, so servers sharing the database
// count together.
const signInLock = "sign_in"

// accountKey is an account's row. An email too long to be one, which can
// have no account, is counted by its hash, so it still fits the column.
func accountKey(email string) string {
	email = auth.NormalizeEmail(email)
	if len(email) > maxEmailKey {
		sum := sha256.Sum256([]byte(email))
		email = "#" + hex.EncodeToString(sum[:])
	}
	return models.ThrottleAccount + ":" + email
}

// maxEmailKey is the longest email kept as it is in a key: 254 bytes, the
// longest an address can be.
const maxEmailKey = 254

func addressKey(address string) string { return models.ThrottleAddress + ":" + address }

// throttleKeys are the rows a sign-in by email from address counts against.
// An address is left out when it is unknown.
func throttleKeys(email, address string) []string {
	keys := []string{accountKey(email)}
	if address != "" {
		keys = append(keys, addressKey(address))
	}
	return keys
}

// SignInWait returns how long a sign-in by email from address must wait
// before its password is checked: zero when it may go ahead.
func (s *Store) SignInWait(email, address string, now time.Time) (time.Duration, error) {
	// To the microsecond, as times are stored.
	now = dbTime(now)
	var wait time.Duration
	for _, key := range throttleKeys(email, address) {
		var until *time.Time
		err := s.QueryRow("SELECT blocked_until FROM sign_in_throttles WHERE throttle_key = ?", key).Scan(&until)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return 0, fmt.Errorf("reading sign-in throttle: %w", err)
		}
		if until != nil && until.After(now) && until.Sub(now) > wait {
			wait = until.Sub(now)
		}
	}
	return wait, nil
}

// accountWait is how long an account waits after its failures-th failure in
// a window: nothing up to AccountFailuresAllowed, then 1s, doubling each
// time, up to MaxAccountWait.
func accountWait(failures int) time.Duration {
	if failures < AccountFailuresAllowed {
		return 0
	}
	n := failures - AccountFailuresAllowed
	if n >= 20 {
		return MaxAccountWait
	}
	return min(time.Second<<n, MaxAccountWait)
}

// RecordSignInFailure counts a failed sign-in by email from address at now.
// It records auth.throttled when the account (if it exists) or the address
// first starts waiting in a window, and returns how long the next attempt
// must wait.
func (s *Store) RecordSignInFailure(email, address string, now time.Time) (time.Duration, error) {
	now = dbTime(now)
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if err := s.takeLock(tx, signInLock); err != nil {
		return 0, err
	}
	// Lapsed rows are pruned as failures come, so the table stays small.
	if _, err := s.TxExec(tx, "DELETE FROM sign_in_throttles WHERE last_failure_at < ? AND (blocked_until IS NULL OR blocked_until < ?)",
		now.Add(-throttleRowsKept), now.Add(-throttleRowsKept)); err != nil {
		return 0, fmt.Errorf("pruning sign-in throttles: %w", err)
	}

	var wait time.Duration
	for _, key := range throttleKeys(email, address) {
		var (
			failures int
			last     time.Time
			until    *time.Time
		)
		err := s.TxQueryRow(tx, "SELECT failures, last_failure_at, blocked_until FROM sign_in_throttles WHERE throttle_key = ?", key).Scan(&failures, &last, &until)
		exists := err == nil
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("reading sign-in throttle: %w", err)
		}
		// A window runs from the last failure, or the end of the wait it
		// set, whichever is later.
		end := last
		if until != nil && until.After(end) {
			end = *until
		}
		if !exists || now.Sub(end) > ThrottleWindow {
			failures = 0
		}
		failures++

		account := strings.HasPrefix(key, models.ThrottleAccount+":")
		var blocked *time.Time
		var started bool
		if account {
			if w := accountWait(failures); w > 0 {
				t := now.Add(w)
				blocked, started, wait = &t, failures == AccountFailuresAllowed, max(wait, w)
			}
		} else if failures >= AddressFailuresAllowed {
			t := now.Add(ThrottleWindow)
			blocked, started, wait = &t, failures == AddressFailuresAllowed, max(wait, ThrottleWindow)
		}

		if exists {
			_, err = s.TxExec(tx, "UPDATE sign_in_throttles SET failures = ?, last_failure_at = ?, blocked_until = ? WHERE throttle_key = ?", failures, now, blocked, key)
		} else {
			_, err = s.TxExec(tx, "INSERT INTO sign_in_throttles (throttle_key, failures, last_failure_at, blocked_until) VALUES (?, ?, ?, ?)", key, failures, now, blocked)
		}
		if err != nil {
			return 0, fmt.Errorf("counting a failed sign-in: %w", err)
		}
		if started {
			if err := s.recordThrottled(tx, account, email, address, *blocked); err != nil {
				return 0, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("committing: %w", err)
	}
	return wait, nil
}

// recordThrottled records auth.throttled for an account that exists, named
// by its user, or for an address.
func (s *Store) recordThrottled(tx *sql.Tx, account bool, email, address string, until time.Time) error {
	t := &models.EventThrottle{Kind: models.ThrottleAddress, Address: address, Until: until}
	if account {
		var u models.EventActor
		err := s.TxQueryRow(tx, "SELECT id, name FROM users WHERE email = ? AND archived_at IS NULL", auth.NormalizeEmail(email)).Scan(&u.ID, &u.Name)
		if errors.Is(err, sql.ErrNoRows) {
			// An email with no account: nobody to warn about.
			return nil
		}
		if err != nil {
			return fmt.Errorf("reading the throttled account: %w", err)
		}
		t = &models.EventThrottle{Kind: models.ThrottleAccount, User: &u, Until: until}
	}
	// Recorded by nobody: a failed sign-in is no one's action.
	return s.As("").insertEvent(tx, models.EventAuthThrottled, nil, nil, models.EventPayload{Trigger: models.TriggerThreshold, Throttle: t})
}

// ClearSignInFailures forgets an account's failed sign-ins, after a correct
// password or when an admin clears them.
func (s *Store) ClearSignInFailures(email string) error {
	if _, err := s.Exec("DELETE FROM sign_in_throttles WHERE throttle_key = ?", accountKey(email)); err != nil {
		return fmt.Errorf("clearing sign-in throttle: %w", err)
	}
	return nil
}

// SignInDelays returns, by normalized email, the accounts that must wait to
// sign in at now, and until when.
func (s *Store) SignInDelays(now time.Time) (map[string]time.Time, error) {
	rows, err := s.Query("SELECT throttle_key, blocked_until FROM sign_in_throttles WHERE throttle_key LIKE ? AND blocked_until > ?",
		models.ThrottleAccount+":%", dbTime(now))
	if err != nil {
		return nil, fmt.Errorf("listing sign-in delays: %w", err)
	}
	defer rows.Close()
	out := map[string]time.Time{}
	for rows.Next() {
		var (
			key   string
			until time.Time
		)
		if err := rows.Scan(&key, &until); err != nil {
			return nil, fmt.Errorf("listing sign-in delays: %w", err)
		}
		out[strings.TrimPrefix(key, models.ThrottleAccount+":")] = until
	}
	return out, rows.Err()
}
