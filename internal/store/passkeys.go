package store

import (
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Passkeys (docs/access.md#passkeys): second factors registered from a
// browser, and the challenges sent to it for them.

// Passkey challenges are each for one purpose.
const (
	PasskeyRegister = "register"
	PasskeySignIn   = "sign_in"
	PasskeyConfirm  = "confirm"
)

// PasskeyChallengeTTL is how long a challenge waits for its answer.
const PasskeyChallengeTTL = 5 * time.Minute

var (
	// ErrPasskeyChallengeNotFound is returned for an answer to a challenge
	// not sent, answered already, or expired.
	ErrPasskeyChallengeNotFound = errors.New("the passkey was too slow, or answered something else; try again")
	// ErrPasskeyRegistered is returned for a passkey registered already.
	ErrPasskeyRegistered = errors.New("that passkey is registered already")
	// ErrPasskeyUsed is returned when another sign-in used the passkey at
	// the same moment.
	ErrPasskeyUsed = errors.New("that passkey was just used; try again")
)

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// CreatePasskeyChallenge records a challenge sent to userID's browser for
// purpose, forgetting those expired.
func (s *Store) CreatePasskeyChallenge(userID, purpose string, challenge []byte) error {
	now := dbTime(time.Now())
	if _, err := s.Exec("DELETE FROM passkey_challenges WHERE expires_at <= ?", now); err != nil {
		return fmt.Errorf("forgetting expired challenges: %w", err)
	}
	if _, err := s.Exec("INSERT INTO passkey_challenges (id, user_id, purpose, challenge, expires_at, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		uuid.NewString(), userID, purpose, b64(challenge), now.Add(PasskeyChallengeTTL), now); err != nil {
		return fmt.Errorf("recording the challenge: %w", err)
	}
	return nil
}

// TakePasskeyChallenge uses up a challenge sent to userID for purpose, as
// an answer to it arrives. Taken as it is checked, it is answered once,
// however many servers are given the answer.
func (s *Store) TakePasskeyChallenge(userID, purpose string, challenge []byte) error {
	res, err := s.Exec("DELETE FROM passkey_challenges WHERE user_id = ? AND purpose = ? AND challenge = ? AND expires_at > ?",
		userID, purpose, b64(challenge), dbTime(time.Now()))
	if err != nil {
		return fmt.Errorf("taking the challenge: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrPasskeyChallengeNotFound
	}
	return nil
}

// AddPasskey registers a passkey for userID, named, for the relying party
// rpID: its credential ID, COSE public key and signature counter.
func (s *Store) AddPasskey(userID, name, rpID string, credentialID, publicKey []byte, signCount uint32) (*Factor, error) {
	var n int
	if err := s.QueryRow("SELECT COUNT(*) FROM user_factors WHERE kind = ? AND credential_id = ?", FactorPasskey, b64(credentialID)).Scan(&n); err != nil {
		return nil, fmt.Errorf("looking for the passkey: %w", err)
	}
	if n > 0 {
		return nil, ErrPasskeyRegistered
	}
	id, now := uuid.NewString(), dbTime(time.Now())
	if _, err := s.Exec(`INSERT INTO user_factors (id, user_id, kind, secret, last_step, created_at, confirmed_at, name, credential_id, public_key, rp_id)
		VALUES (?, ?, ?, '', ?, ?, ?, ?, ?, ?, ?)`,
		id, userID, FactorPasskey, int64(signCount), now, now, name, b64(credentialID), b64(publicKey), rpID); err != nil {
		return nil, fmt.Errorf("adding the passkey: %w", err)
	}
	// With a factor, sessions held to setting one up are free.
	if err := s.clearFactorSetup(userID); err != nil {
		return nil, err
	}
	return &Factor{ID: id, Kind: FactorPasskey, Name: name, RPID: rpID, CreatedAt: now, ConfirmedAt: &now}, nil
}

// UsePasskey records a passkey used, with its signature counter now, given
// the one it was checked against.
func (s *Store) UsePasskey(id string, was, now uint32) error {
	res, err := s.Exec("UPDATE user_factors SET last_step = ?, last_used_at = ? WHERE id = ? AND kind = ? AND last_step = ?",
		int64(now), dbTime(time.Now()), id, FactorPasskey, int64(was))
	if err != nil {
		return fmt.Errorf("recording the passkey used: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrPasskeyUsed
	}
	return nil
}

// PasskeysForOtherHosts counts the passkeys registered for a relying party
// other than rpID, which cannot be used while base_url's host is rpID.
func (s *Store) PasskeysForOtherHosts(rpID string) (int, error) {
	var n int
	if err := s.QueryRow("SELECT COUNT(*) FROM user_factors WHERE kind = ? AND rp_id <> ?", FactorPasskey, rpID).Scan(&n); err != nil {
		return 0, fmt.Errorf("counting passkeys: %w", err)
	}
	return n, nil
}
