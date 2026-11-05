package store

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/google/uuid"
)

// Pairing relays (docs/relays.md#pairing): the relays an admin added, their
// one-time codes, what pairing gave each, and the mesh root.

// Errors pairing ends with.
var (
	ErrRelayNotFound = errors.New("relay not found")
	// ErrRelayCodeInvalid is a code no relay waits for: mistyped, or used.
	ErrRelayCodeInvalid = errors.New("this code is not valid, or was used already")
	// ErrRelayCodeExpired is a code past its hour.
	ErrRelayCodeExpired = errors.New("this code has expired: make a new one under Admin › Relays")
	// ErrRelayRevoked is a relay that was revoked, for a new code.
	ErrRelayRevoked = errors.New("this relay was revoked: add it again once its name is free")
)

// RelayNameTakenError is a name another relay holds; a revoked relay
// holds its name until Until, when its certificate expires.
type RelayNameTakenError struct {
	Name  string
	Until *time.Time
}

func (e *RelayNameTakenError) Error() string {
	if e.Until != nil {
		return fmt.Sprintf("a revoked relay holds the name %s until its certificate expires, %s", e.Name, e.Until.UTC().Format("2 January 2006 15:04 UTC"))
	}
	return "a relay named " + e.Name + " already exists"
}

const relayColumns = "id, name, code_expires_at, identity_key IS NOT NULL, paired_at, cert_expires_at, revoked_at, online, status_at, version, address, allowlist, created_at"

func scanRelay(row interface{ Scan(...any) error }) (*models.Relay, error) {
	var r models.Relay
	var codeExpires, paired, certExpires, revoked, statusAt sql.NullTime
	var allowlist sql.NullString
	if err := row.Scan(&r.ID, &r.Name, &codeExpires, &r.Paired, &paired, &certExpires, &revoked, &r.Online, &statusAt, &r.Version, &r.Address, &allowlist, &r.CreatedAt); err != nil {
		return nil, err
	}
	at := func(t sql.NullTime) *time.Time {
		if !t.Valid {
			return nil
		}
		v := t.Time.UTC()
		return &v
	}
	r.CodeExpiresAt, r.PairedAt, r.CertExpiresAt, r.RevokedAt, r.StatusAt = at(codeExpires), at(paired), at(certExpires), at(revoked), at(statusAt)
	r.Allowlist = []string{}
	if allowlist.Valid && allowlist.String != "" {
		_ = json.Unmarshal([]byte(allowlist.String), &r.Allowlist)
	}
	return &r, nil
}

// sweepRevokedRelays forgets revoked relays whose last certificate has
// expired: nothing they hold works any more, and their names are free.
func (s *Store) sweepRevokedRelays(now time.Time) error {
	_, err := s.Exec("DELETE FROM relays WHERE revoked_at IS NOT NULL AND (cert_expires_at IS NULL OR cert_expires_at < ?)", dbTime(now))
	if err != nil {
		return fmt.Errorf("forgetting revoked relays: %w", err)
	}
	return nil
}

// ListRelays lists the relays, by name, revoked ones until their names
// are free.
func (s *Store) ListRelays() ([]models.Relay, error) {
	if err := s.sweepRevokedRelays(time.Now()); err != nil {
		return nil, err
	}
	rows, err := s.Query("SELECT " + relayColumns + " FROM relays ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("listing relays: %w", err)
	}
	defer rows.Close()
	out := []models.Relay{}
	for rows.Next() {
		r, err := scanRelay(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// GetRelay reads a relay by its ID.
func (s *Store) GetRelay(id string) (*models.Relay, error) {
	r, err := scanRelay(s.QueryRow("SELECT "+relayColumns+" FROM relays WHERE id = ?", id))
	if err == sql.ErrNoRows {
		return nil, ErrRelayNotFound
	}
	return r, err
}

// relayByName reads a relay by its name.
func (s *Store) relayByName(name string) (*models.Relay, error) {
	r, err := scanRelay(s.QueryRow("SELECT "+relayColumns+" FROM relays WHERE name = ?", name))
	if err == sql.ErrNoRows {
		return nil, ErrRelayNotFound
	}
	return r, err
}

// CreateRelay adds a relay waiting to pair with a code, kept as codeHash
// until expires. The caller checks the name is one a relay may have.
func (s *Store) CreateRelay(name, codeHash string, expires time.Time) (*models.Relay, error) {
	now := time.Now()
	if err := s.sweepRevokedRelays(now); err != nil {
		return nil, err
	}
	if r, err := s.relayByName(name); err == nil {
		taken := &RelayNameTakenError{Name: name}
		if r.RevokedAt != nil {
			taken.Until = r.CertExpiresAt
		}
		return nil, taken
	} else if !errors.Is(err, ErrRelayNotFound) {
		return nil, err
	}
	id := uuid.NewString()
	_, err := s.Exec("INSERT INTO relays (id, name, code_hash, code_expires_at, created_at) VALUES (?, ?, ?, ?, ?)",
		id, name, codeHash, dbTime(expires), dbTime(now))
	if err != nil && isUniqueViolation(err) {
		return nil, &RelayNameTakenError{Name: name}
	}
	if err != nil {
		return nil, fmt.Errorf("adding a relay: %w", err)
	}
	return s.GetRelay(id)
}

// SetRelayCode gives a relay a new code to pair with, as to pair it again
// on a new machine, or after the mesh root was rotated. What it was
// given before stays until the new code is used.
func (s *Store) SetRelayCode(id, codeHash string, expires time.Time) (*models.Relay, error) {
	r, err := s.GetRelay(id)
	if err != nil {
		return nil, err
	}
	if r.RevokedAt != nil {
		return nil, ErrRelayRevoked
	}
	if _, err := s.Exec("UPDATE relays SET code_hash = ?, code_expires_at = ? WHERE id = ?", codeHash, dbTime(expires), id); err != nil {
		return nil, fmt.Errorf("giving a relay a code: %w", err)
	}
	return s.GetRelay(id)
}

// RelayJoin is what pairing keeps of a relay that used its code.
type RelayJoin struct {
	CodeHash       string
	IdentityKey    []byte
	CredentialHash string
	CertExpiresAt  time.Time
	Version        string
	Address        string
}

// JoinRelay pairs the relay waiting with a code: once, and before the
// code expires. The code is used up, and the relay's identity key and
// credential replace any it had.
func (s *Store) JoinRelay(j RelayJoin) (*models.Relay, error) {
	var id string
	var expires sql.NullTime
	err := s.QueryRow("SELECT id, code_expires_at FROM relays WHERE code_hash = ? AND revoked_at IS NULL", j.CodeHash).Scan(&id, &expires)
	if err == sql.ErrNoRows {
		return nil, ErrRelayCodeInvalid
	}
	if err != nil {
		return nil, fmt.Errorf("finding the relay for a code: %w", err)
	}
	now := time.Now()
	if !expires.Valid || !expires.Time.After(now) {
		return nil, ErrRelayCodeExpired
	}
	// Used once: the update takes the code only while it is there.
	res, err := s.Exec("UPDATE relays SET code_hash = NULL, code_expires_at = NULL, identity_key = ?, credential_hash = ?, cert_expires_at = ?, paired_at = ?, version = ?, address = ? WHERE id = ? AND code_hash = ?",
		base64.StdEncoding.EncodeToString(j.IdentityKey), j.CredentialHash, dbTime(j.CertExpiresAt), dbTime(now), truncateText(j.Version, 64), truncateText(j.Address, 64), id, j.CodeHash)
	if err != nil {
		return nil, fmt.Errorf("pairing a relay: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return nil, ErrRelayCodeInvalid
	}
	return s.GetRelay(id)
}

// RelayByCredential is the paired relay holding a credential, by its
// SHA-256, or ErrRelayNotFound.
func (s *Store) RelayByCredential(credentialHash string) (*models.Relay, error) {
	r, err := scanRelay(s.QueryRow("SELECT "+relayColumns+" FROM relays WHERE credential_hash = ? AND revoked_at IS NULL AND identity_key IS NOT NULL", credentialHash))
	if err == sql.ErrNoRows {
		return nil, ErrRelayNotFound
	}
	return r, err
}

// RelayIdentityKey is a paired relay's identity key, or ErrRelayNotFound
// for a relay not paired or revoked.
func (s *Store) RelayIdentityKey(name string) ([]byte, error) {
	var key sql.NullString
	err := s.QueryRow("SELECT identity_key FROM relays WHERE name = ? AND revoked_at IS NULL", name).Scan(&key)
	if err == sql.ErrNoRows || (err == nil && !key.Valid) {
		return nil, ErrRelayNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("reading a relay's key: %w", err)
	}
	return base64.StdEncoding.DecodeString(key.String)
}

// RenewRelay records a paired relay's new certificate, and what it said
// of itself as it renewed.
func (s *Store) RenewRelay(id string, certExpires time.Time, version, address string) error {
	_, err := s.Exec("UPDATE relays SET cert_expires_at = ?, version = ?, address = ? WHERE id = ? AND revoked_at IS NULL",
		dbTime(certExpires), truncateText(version, 64), truncateText(address, 64), id)
	if err != nil {
		return fmt.Errorf("renewing a relay: %w", err)
	}
	return nil
}

// RevokeRelay revokes a relay. One that never paired is removed. A paired
// one loses its credential and code at once, and stays, refusing its key
// and keeping its name, until its certificate expires.
func (s *Store) RevokeRelay(id string) (*models.Relay, error) {
	r, err := s.GetRelay(id)
	if err != nil {
		return nil, err
	}
	if !r.Paired {
		if _, err := s.Exec("DELETE FROM relays WHERE id = ?", id); err != nil {
			return nil, fmt.Errorf("removing a relay: %w", err)
		}
		return r, nil
	}
	now := dbTime(time.Now())
	if r.RevokedAt == nil {
		_, err = s.Exec("UPDATE relays SET revoked_at = ?, credential_hash = NULL, code_hash = NULL, code_expires_at = NULL, online = ?, status_at = ? WHERE id = ?", now, false, now, id)
		if err != nil {
			return nil, fmt.Errorf("revoking a relay: %w", err)
		}
	}
	return s.GetRelay(id)
}

// SetRelayOnline records a relay coming online or going offline, and the
// event saying so, if it was not already. Several servers may see it at
// once; one records it.
func (s *Store) SetRelayOnline(name string, online bool) (bool, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	now := dbTime(time.Now())
	res, err := s.TxExec(tx, "UPDATE relays SET online = ?, status_at = ? WHERE name = ? AND online = ? AND revoked_at IS NULL AND identity_key IS NOT NULL", online, now, name, !online)
	if err != nil {
		return false, fmt.Errorf("recording a relay online: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return false, err
	}
	var id string
	if err := s.TxQueryRow(tx, "SELECT id FROM relays WHERE name = ?", name).Scan(&id); err != nil {
		return false, err
	}
	kind := models.EventRelayOffline
	if online {
		kind = models.EventRelayOnline
	}
	if err := s.As("").insertEvent(tx, kind, nil, nil, models.EventPayload{Trigger: models.TriggerSchedule, Relay: &models.EventRelay{ID: id, Name: name}}); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// SetRelayStatus records what a relay says of itself: its version and
// what its allowlist covers.
func (s *Store) SetRelayStatus(name, version string, allowlist []string) error {
	if allowlist == nil {
		allowlist = []string{}
	}
	list, err := json.Marshal(allowlist)
	if err != nil {
		return err
	}
	if _, err := s.Exec("UPDATE relays SET version = ?, allowlist = ? WHERE name = ?", truncateText(version, 64), string(list), name); err != nil {
		return fmt.Errorf("recording a relay's status: %w", err)
	}
	return nil
}

// RelayMesh is the mesh root and the main node's own identity, their
// private keys sealed.
type RelayMesh struct {
	Mesh        string
	RootPublic  []byte
	RootPrivate string
	MainPublic  []byte
	MainPrivate string
	CreatedAt   time.Time
}

// ErrNoRelayMesh is returned while there is no mesh root yet.
var ErrNoRelayMesh = errors.New("there is no mesh root yet")

// GetRelayMesh reads the mesh root, or ErrNoRelayMesh.
func (s *Store) GetRelayMesh() (*RelayMesh, error) {
	var m RelayMesh
	var rootPublic, mainPublic string
	err := s.QueryRow("SELECT mesh, root_public, root_private, main_public, main_private, created_at FROM relay_mesh WHERE id = 'mesh'").
		Scan(&m.Mesh, &rootPublic, &m.RootPrivate, &mainPublic, &m.MainPrivate, &m.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNoRelayMesh
	}
	if err != nil {
		return nil, fmt.Errorf("reading the mesh root: %w", err)
	}
	if m.RootPublic, err = base64.StdEncoding.DecodeString(rootPublic); err != nil {
		return nil, err
	}
	if m.MainPublic, err = base64.StdEncoding.DecodeString(mainPublic); err != nil {
		return nil, err
	}
	return &m, nil
}

// CreateRelayMesh keeps a new mesh root, unless another server made one
// first; either way it returns the one kept.
func (s *Store) CreateRelayMesh(m RelayMesh) (*RelayMesh, error) {
	_, err := s.Exec("INSERT INTO relay_mesh (id, mesh, root_public, root_private, main_public, main_private, created_at) VALUES ('mesh', ?, ?, ?, ?, ?, ?)",
		m.Mesh, base64.StdEncoding.EncodeToString(m.RootPublic), m.RootPrivate, base64.StdEncoding.EncodeToString(m.MainPublic), m.MainPrivate, dbTime(time.Now()))
	if err != nil && !isUniqueViolation(err) {
		return nil, fmt.Errorf("keeping the mesh root: %w", err)
	}
	return s.GetRelayMesh()
}

// RotateRelayMesh replaces the mesh root. Every relay's certificate is
// from the old one, so every relay is unpaired, keeping its name, to pair
// again with a new code; revoked ones are forgotten, as nothing they hold
// works any more.
func (s *Store) RotateRelayMesh(m RelayMesh) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := dbTime(time.Now())
	steps := []struct {
		q    string
		args []any
	}{
		{"DELETE FROM relay_mesh", nil},
		{"INSERT INTO relay_mesh (id, mesh, root_public, root_private, main_public, main_private, created_at) VALUES ('mesh', ?, ?, ?, ?, ?, ?)",
			[]any{m.Mesh, base64.StdEncoding.EncodeToString(m.RootPublic), m.RootPrivate, base64.StdEncoding.EncodeToString(m.MainPublic), m.MainPrivate, now}},
		{"DELETE FROM relays WHERE revoked_at IS NOT NULL", nil},
		{"UPDATE relays SET identity_key = NULL, credential_hash = NULL, cert_expires_at = NULL, paired_at = NULL, code_hash = NULL, code_expires_at = NULL, online = ?, status_at = ?", []any{false, now}},
	}
	for _, st := range steps {
		if _, err := s.TxExec(tx, st.q, st.args...); err != nil {
			return fmt.Errorf("rotating the mesh root: %w", err)
		}
	}
	return tx.Commit()
}

// RelayCodeKey is the SHA-256 a code or credential is kept as.
func RelayCodeKey(secret string) string { return sessionKey(strings.TrimSpace(secret)) }
