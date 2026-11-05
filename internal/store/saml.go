package store

import (
	"errors"
	"fmt"
	"time"
)

// ErrAssertionUsed is returned for a SAML assertion used to sign in
// already.
var ErrAssertionUsed = errors.New("the assertion was used already")

// UseSAMLAssertion records that providerID's assertion assertionID signed
// someone in, until it expires, forgetting those expired. It returns
// ErrAssertionUsed for one used already, so it signs in once
// (docs/access.md#saml-20).
func (s *Store) UseSAMLAssertion(providerID, assertionID string, until time.Time) error {
	now := dbTime(time.Now())
	if _, err := s.Exec("DELETE FROM saml_assertions WHERE expires_at <= ?", now); err != nil {
		return fmt.Errorf("forgetting expired assertions: %w", err)
	}
	if _, err := s.Exec("INSERT INTO saml_assertions (provider_id, assertion_id, expires_at) VALUES (?, ?, ?)",
		providerID, assertionID, dbTime(until)); err != nil {
		if IsUniqueViolation(err) {
			return ErrAssertionUsed
		}
		return fmt.Errorf("recording the assertion: %w", err)
	}
	return nil
}
