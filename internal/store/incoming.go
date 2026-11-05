package store

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/textcheck"
)

// Incoming hooks (docs/hooks.md#incoming-hooks): URLs that let something
// outside BAM! act on it, as a role narrowed to scopes, as tokens do.

var (
	// ErrIncomingNotFound is returned for an incoming hook that does not
	// exist.
	ErrIncomingNotFound = errors.New("incoming hook not found")
	// ErrInvalidIncoming wraps the reason an incoming hook cannot be saved
	// as given.
	ErrInvalidIncoming = errors.New("invalid incoming hook")
	// ErrDeliveryUsed is returned for a signed request whose delivery ID
	// was taken already.
	ErrDeliveryUsed = errors.New("this delivery was received already")
)

// IncomingSecretPrefix starts every incoming hook's secret, so one is easy
// to recognize in a script or a leak report, and never taken for an API
// token's.
const IncomingSecretPrefix = "bamin_"

// Limits on how many requests a minute an incoming hook takes.
const (
	DefaultIncomingRate = 60
	MaxIncomingRate     = 600
)

// incomingAssignments are where incoming hooks' roles are kept.
var incomingAssignments = assignmentTables{roles: "incoming_hook_roles", scopes: "incoming_hook_role_scopes", holder: "hook_id"}

// IncomingInput makes or changes an incoming hook. Its action's settings
// are checked by the API, which knows the hooks, flows and fields they
// name; the store checks the rest.
type IncomingInput struct {
	Name      string
	Enabled   bool
	Auth      string
	Action    string
	Settings  models.IncomingSettings
	RateLimit int
	Roles     []models.RoleAssignment
	// Seal seals a signing secret for the hook with this ID, for
	// IncomingHMAC: checking a signature needs the secret itself.
	Seal func(id, secret string) (string, error)
}

func (in *IncomingInput) check() error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return fmt.Errorf("%w: a name is required", ErrInvalidIncoming)
	}
	if err := textcheck.Check("name", in.Name, textcheck.MaxName); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidIncoming, err)
	}
	switch in.Auth {
	case models.IncomingBearer, models.IncomingHMAC:
	default:
		return fmt.Errorf("%w: auth is %s or %s, not %q", ErrInvalidIncoming, models.IncomingBearer, models.IncomingHMAC, in.Auth)
	}
	if in.Auth == models.IncomingHMAC && in.Seal == nil {
		return fmt.Errorf("%w: a signing secret cannot be kept", ErrInvalidIncoming)
	}
	switch in.Action {
	case models.IncomingTriggerHook, models.IncomingQuantity, models.IncomingFields, models.IncomingFlow:
	default:
		return fmt.Errorf("%w: the action is hook, quantity, fields or flow, not %q", ErrInvalidIncoming, in.Action)
	}
	if in.RateLimit == 0 {
		in.RateLimit = DefaultIncomingRate
	}
	if in.RateLimit < 1 || in.RateLimit > MaxIncomingRate {
		return fmt.Errorf("%w: the rate limit is 1 to %d requests a minute", ErrInvalidIncoming, MaxIncomingRate)
	}
	if len(in.Roles) == 0 {
		return fmt.Errorf("%w: give it a role to act as", ErrInvalidIncoming)
	}
	return nil
}

// newIncomingID makes an incoming hook's ID, its address: long and random,
// so it cannot be guessed.
func newIncomingID() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("reading random bytes for an incoming hook: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// newIncomingSecret makes an incoming hook's secret.
func newIncomingSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("reading random bytes for a secret: %w", err)
	}
	return IncomingSecretPrefix + hex.EncodeToString(b), nil
}

// incomingSecretHash is how a bearer secret is kept: its SHA-256 hash.
func incomingSecretHash(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// keepSecret makes a new secret for a hook and says how it is kept: hashed
// for a bearer secret, sealed for a signing secret.
func keepSecret(id string, in IncomingInput) (secret string, hash, sealed any, err error) {
	if secret, err = newIncomingSecret(); err != nil {
		return "", nil, nil, err
	}
	if in.Auth == models.IncomingBearer {
		return secret, incomingSecretHash(secret), nil, nil
	}
	s, err := in.Seal(id, secret)
	if err != nil {
		return "", nil, nil, err
	}
	return secret, nil, s, nil
}

const incomingColumns = "id, name, enabled, auth, secret_hash IS NOT NULL OR sealed_secret IS NOT NULL, action, settings, rate_limit, created_by, created_at, updated_at, last_used_at"

func scanIncoming(row interface{ Scan(...any) error }) (models.IncomingHook, error) {
	var (
		h        models.IncomingHook
		settings string
		by       sql.NullString
	)
	if err := row.Scan(&h.ID, &h.Name, &h.Enabled, &h.Auth, &h.HasSecret, &h.Action, &settings, &h.RateLimit, &by, &h.CreatedAt, &h.UpdatedAt, &h.LastUsedAt); err != nil {
		return h, err
	}
	h.CreatedBy = by.String
	if err := json.Unmarshal([]byte(settings), &h.IncomingSettings); err != nil {
		return h, fmt.Errorf("reading incoming hook %s's settings: %w", h.ID, err)
	}
	h.Roles = []models.RoleAssignment{}
	return h, nil
}

// ListIncomingHooks returns every incoming hook, by name.
func (s *Store) ListIncomingHooks() ([]models.IncomingHook, error) {
	rows, err := s.Query("SELECT " + incomingColumns + " FROM incoming_hooks ORDER BY name, id")
	if err != nil {
		return nil, fmt.Errorf("listing incoming hooks: %w", err)
	}
	hooks := []models.IncomingHook{}
	for rows.Next() {
		h, err := scanIncoming(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		hooks = append(hooks, h)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	roles, err := s.readAssignments(incomingAssignments)
	if err != nil {
		return nil, err
	}
	for i := range hooks {
		if r := roles[hooks[i].ID]; r != nil {
			hooks[i].Roles = r
		}
	}
	return hooks, nil
}

// GetIncomingHook returns an incoming hook, or ErrIncomingNotFound.
func (s *Store) GetIncomingHook(id string) (*models.IncomingHook, error) {
	h, err := scanIncoming(s.QueryRow("SELECT "+incomingColumns+" FROM incoming_hooks WHERE id = ?", id))
	if err == sql.ErrNoRows {
		return nil, ErrIncomingNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("reading incoming hook: %w", err)
	}
	roles, err := s.readAssignments(incomingAssignments)
	if err != nil {
		return nil, err
	}
	if r := roles[id]; r != nil {
		h.Roles = r
	}
	return &h, nil
}

// CreateIncomingHook makes an incoming hook, made by createdBy, and returns
// it with its secret, which is shown once.
func (s *Store) CreateIncomingHook(in IncomingInput, createdBy string) (*models.IncomingHook, string, error) {
	if err := in.check(); err != nil {
		return nil, "", err
	}
	id, err := newIncomingID()
	if err != nil {
		return nil, "", err
	}
	secret, hash, sealed, err := keepSecret(id, in)
	if err != nil {
		return nil, "", err
	}
	settings, err := json.Marshal(in.Settings)
	if err != nil {
		return nil, "", err
	}
	var by any
	if createdBy != "" {
		by = createdBy
	}
	now := dbTime(time.Now())
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, "", fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := s.TxExec(tx, `INSERT INTO incoming_hooks (id, name, enabled, auth, secret_hash, sealed_secret, action, settings, rate_limit, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, in.Name, in.Enabled, in.Auth, hash, sealed, in.Action, string(settings), in.RateLimit, by, now, now); err != nil {
		return nil, "", fmt.Errorf("inserting incoming hook: %w", err)
	}
	merged, err := s.mergeAssignments(tx, in.Roles)
	if err != nil {
		return nil, "", err
	}
	if err := s.insertAssignmentsTx(tx, incomingAssignments, id, merged); err != nil {
		return nil, "", err
	}
	if err := tx.Commit(); err != nil {
		return nil, "", fmt.Errorf("committing: %w", err)
	}
	h, err := s.GetIncomingHook(id)
	return h, secret, err
}

// UpdateIncomingHook changes an incoming hook. With newSecret, or a change
// of how it authenticates, it is given a new secret, returned to be shown
// once, and the old one stops working; otherwise the secret is "" and kept.
func (s *Store) UpdateIncomingHook(id string, in IncomingInput, newSecret bool) (*models.IncomingHook, string, error) {
	if err := in.check(); err != nil {
		return nil, "", err
	}
	was, err := s.GetIncomingHook(id)
	if err != nil {
		return nil, "", err
	}
	settings, err := json.Marshal(in.Settings)
	if err != nil {
		return nil, "", err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, "", fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := s.TxExec(tx, "UPDATE incoming_hooks SET name = ?, enabled = ?, auth = ?, action = ?, settings = ?, rate_limit = ?, updated_at = ? WHERE id = ?",
		in.Name, in.Enabled, in.Auth, in.Action, string(settings), in.RateLimit, dbTime(time.Now()), id); err != nil {
		return nil, "", fmt.Errorf("updating incoming hook: %w", err)
	}
	var secret string
	if newSecret || in.Auth != was.Auth {
		var hash, sealed any
		if secret, hash, sealed, err = keepSecret(id, in); err != nil {
			return nil, "", err
		}
		if _, err := s.TxExec(tx, "UPDATE incoming_hooks SET secret_hash = ?, sealed_secret = ? WHERE id = ?", hash, sealed, id); err != nil {
			return nil, "", fmt.Errorf("replacing the secret: %w", err)
		}
	}
	merged, err := s.mergeAssignments(tx, in.Roles)
	if err != nil {
		return nil, "", err
	}
	if _, err := s.TxExec(tx, "DELETE FROM incoming_hook_roles WHERE hook_id = ?", id); err != nil {
		return nil, "", fmt.Errorf("replacing roles: %w", err)
	}
	if err := s.insertAssignmentsTx(tx, incomingAssignments, id, merged); err != nil {
		return nil, "", err
	}
	if err := tx.Commit(); err != nil {
		return nil, "", fmt.Errorf("committing: %w", err)
	}
	h, err := s.GetIncomingHook(id)
	return h, secret, err
}

// DeleteIncomingHook deletes an incoming hook; its address stops working.
func (s *Store) DeleteIncomingHook(id string) error {
	res, err := s.Exec("DELETE FROM incoming_hooks WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("deleting incoming hook: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrIncomingNotFound
	}
	return nil
}

// IncomingCredential is how an incoming hook's secret is kept, for checking
// a request: a bearer secret's hash, or a signing secret, sealed.
type IncomingCredential struct {
	Hash   string
	Sealed string
}

// IncomingHookCredential reads how an incoming hook's secret is kept.
func (s *Store) IncomingHookCredential(id string) (IncomingCredential, error) {
	var hash, sealed sql.NullString
	err := s.QueryRow("SELECT secret_hash, sealed_secret FROM incoming_hooks WHERE id = ?", id).Scan(&hash, &sealed)
	if err == sql.ErrNoRows {
		return IncomingCredential{}, ErrIncomingNotFound
	}
	if err != nil {
		return IncomingCredential{}, fmt.Errorf("reading incoming hook: %w", err)
	}
	return IncomingCredential{Hash: hash.String, Sealed: sealed.String}, nil
}

// MatchesBearer reports, in constant time, whether secret is the bearer
// secret a credential keeps.
func (c IncomingCredential) MatchesBearer(secret string) bool {
	if c.Hash == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(incomingSecretHash(secret)), []byte(c.Hash)) == 1
}

// IncomingHookKeys reads what an incoming hook's roles grant, each narrowed
// to the scopes its assignment names.
func (s *Store) IncomingHookKeys(id string) (map[string]bool, error) {
	return assignedKeys(storeQuerier{s}, incomingAssignments, id, "SELECT role_id FROM incoming_hook_roles WHERE hook_id = ?", id)
}

// AssignmentKeys reads what roles would grant, each narrowed to the scopes
// its assignment names, without assigning them: what an incoming hook
// given them would hold.
func (s *Store) AssignmentKeys(assignments []models.RoleAssignment) (map[string]bool, error) {
	keys := map[string]bool{}
	scopes, err := s.ListScopes()
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	for _, sc := range scopes {
		names[sc.ID] = sc.Name
	}
	for _, a := range assignments {
		byRole, err := roleKeys(storeQuerier{s}, "?", a.RoleID)
		if err != nil {
			return nil, err
		}
		var narrowed []string
		for _, id := range a.ScopeIDs {
			name, ok := names[id]
			if !ok {
				return nil, fmt.Errorf("%w: %s", ErrScopeNotFound, id)
			}
			narrowed = append(narrowed, name)
		}
		for _, k := range rbac.Narrow(byRole[a.RoleID], narrowed) {
			keys[k] = true
		}
	}
	return keys, nil
}

// ClaimIncomingDelivery records that a signed request to an incoming hook
// with this delivery ID was taken, until it would be refused as too old
// anyway, forgetting those past that. It returns ErrDeliveryUsed for one
// taken already, so each is taken once, on any server.
func (s *Store) ClaimIncomingDelivery(hookID, deliveryID string, until time.Time) error {
	if _, err := s.Exec("DELETE FROM incoming_deliveries WHERE expires_at <= ?", dbTime(time.Now())); err != nil {
		return fmt.Errorf("forgetting old deliveries: %w", err)
	}
	if _, err := s.Exec("INSERT INTO incoming_deliveries (hook_id, delivery_id, expires_at) VALUES (?, ?, ?)",
		hookID, deliveryID, dbTime(until)); err != nil {
		if IsUniqueViolation(err) {
			return ErrDeliveryUsed
		}
		return fmt.Errorf("recording the delivery: %w", err)
	}
	return nil
}

// TouchIncomingHook records that an incoming hook was used, at most once a
// minute, as tokens' last use is.
func (s *Store) TouchIncomingHook(h *models.IncomingHook) error {
	now := time.Now().UTC()
	if h.LastUsedAt != nil && now.Sub(*h.LastUsedAt) < tokenUseInterval {
		return nil
	}
	if _, err := s.Exec("UPDATE incoming_hooks SET last_used_at = ? WHERE id = ?", dbTime(now), h.ID); err != nil {
		return fmt.Errorf("recording the incoming hook's use: %w", err)
	}
	return nil
}

// AsIncoming returns a view of the store whose changes are attributed to an
// incoming hook, as As attributes them to a user.
func (s *Store) AsIncoming(hookID string) *Store {
	c := *s
	c.actor, c.actorToken, c.actorIncoming = "", "", hookID
	return &c
}

// EventLog collects the IDs of the events a view of the store records, in
// order, for an incoming hook to answer with.
type EventLog struct {
	mu  sync.Mutex
	ids []string
}

func (l *EventLog) add(id string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ids = append(l.ids, id)
}

// IDs are the events recorded, oldest first.
func (l *EventLog) IDs() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string{}, l.ids...)
}

// Logging returns a view of the store that adds each event it records to
// log.
func (s *Store) Logging(log *EventLog) *Store {
	c := *s
	c.events = log
	return &c
}

// AssetsByFieldValue lists the active assets whose field keyed fieldKey
// holds exactly value, for an incoming hook finding an asset by a field.
func (s *Store) AssetsByFieldValue(fieldKey, value string) ([]string, error) {
	rows, err := s.Query(`SELECT DISTINCT v.asset_id FROM asset_field_values v
		JOIN fields f ON f.id = v.field_id
		JOIN assets a ON a.id = v.asset_id
		WHERE f.field_key = ? AND v.value = ? AND a.archived_at IS NULL ORDER BY v.asset_id`, fieldKey, value)
	if err != nil {
		return nil, fmt.Errorf("finding the field's value: %w", err)
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// IncomingPurpose is what an incoming hook's signing secret is sealed for,
// so one hook's sealed secret cannot be copied onto another.
func IncomingPurpose(id string) string { return "incoming:" + id }
