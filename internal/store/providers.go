package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/auth"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/textcheck"
	"github.com/google/uuid"
)

// Identity providers (docs/access.md#signing-in-with-an-identity-provider):
// the providers people sign in through, the rules that give roles from
// what a provider says, and each person's identities at them.

var (
	ErrProviderNotFound   = errors.New("identity provider not found")
	ErrProviderKeyExists  = errors.New("another identity provider has that key")
	ErrInvalidProvider    = errors.New("invalid identity provider")
	ErrProviderFromConfig = errors.New("this identity provider is set in the configuration file; change it there")
	ErrIdentityNotFound   = errors.New("identity not found")
	// ErrIdentityTaken is returned for a provider's subject linked to
	// another account already.
	ErrIdentityTaken = errors.New("that identity is linked to another account")
	// ErrLastWayIn is returned for unlinking the last way an account has
	// to sign in.
	ErrLastWayIn = errors.New("that would leave the account with no way to sign in")
	// ErrSignInNotFound is returned for a sign-in through a provider that
	// has expired, or was finished already.
	ErrSignInNotFound = errors.New("the sign-in has expired; sign in again")
	// ErrEmailTaken is returned for making an account whose email another
	// account has.
	ErrEmailTaken = errors.New("another account has that email")
)

// ProviderSignInTTL is how long a sign-in sent to a provider may take to
// come back.
const ProviderSignInTTL = 10 * time.Minute

var providerKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

// ProviderPurpose is what a provider's secrets are sealed for: that
// provider alone.
func ProviderPurpose(id string) string { return "provider:" + id }

// ProviderRule gives a role, optionally narrowed to scopes, to whoever's
// claim holds a value.
type ProviderRule struct {
	ID       string   `json:"id"`
	Claim    string   `json:"claim"`
	Value    string   `json:"value"`
	RoleID   string   `json:"role_id"`
	ScopeIDs []string `json:"scope_ids"`
}

// Provider is an identity provider. SealedSecrets is its secrets as stored;
// a provider from the configuration has none stored.
type Provider struct {
	ID             string            `json:"id"`
	Key            string            `json:"key"`
	Name           string            `json:"name"`
	Protocol       string            `json:"protocol"`
	Settings       map[string]string `json:"settings"`
	SealedSecrets  string            `json:"-"`
	ButtonLabel    string            `json:"button_label"`
	Icon           string            `json:"icon"`
	Enabled        bool              `json:"enabled"`
	Position       int               `json:"position"`
	Provision      bool              `json:"provision"`
	DefaultRoleIDs []string          `json:"default_role_ids"`
	AllowedDomains []string          `json:"allowed_domains"`
	AllowedGroups  []string          `json:"allowed_groups"`
	LinkByEmail    bool              `json:"link_by_email"`
	TrustEmail     bool              `json:"trust_email"`
	// MFAAMR and MFAACR are the amr, and acr or AuthnContextClassRef,
	// values that make a sign-in through the provider count as two-factor
	// (docs/access.md#requiring-two-factor-sign-in).
	MFAAMR []string `json:"mfa_amr"`
	MFAACR []string `json:"mfa_acr"`
	// MFAFallback asks for BAM!'s own second factor when the provider's
	// sign-in does not count as two-factor, rather than refusing it.
	MFAFallback bool `json:"mfa_fallback"`
	// MFAAlwaysLocal asks for BAM!'s own second factor whatever the
	// provider says.
	MFAAlwaysLocal bool           `json:"mfa_always_local"`
	FromConfig     bool           `json:"from_config"`
	Rules          []ProviderRule `json:"rules"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

const providerColumns = `id, provider_key, name, protocol, settings, secrets, button_label, icon, enabled, position, provision,
	default_role_ids, allowed_domains, allowed_groups, link_by_email, trust_email, from_config, created_at, updated_at,
	mfa_amr, mfa_acr, mfa_fallback, mfa_always_local`

// DefaultMFAAMR are the amr values that count as two-factor unless a
// provider says otherwise: a second factor, a one-time code, a hardware or
// software key, or a fingerprint (RFC 8176).
var DefaultMFAAMR = []string{"mfa", "otp", "hwk", "swk", "fpt"}

func scanProvider(row interface{ Scan(...any) error }) (*Provider, error) {
	var p Provider
	var settings, roles, domains, groups string
	var amr, acr sql.NullString
	if err := row.Scan(&p.ID, &p.Key, &p.Name, &p.Protocol, &settings, &p.SealedSecrets, &p.ButtonLabel, &p.Icon, &p.Enabled,
		&p.Position, &p.Provision, &roles, &domains, &groups, &p.LinkByEmail, &p.TrustEmail, &p.FromConfig, &p.CreatedAt, &p.UpdatedAt,
		&amr, &acr, &p.MFAFallback, &p.MFAAlwaysLocal); err != nil {
		return nil, err
	}
	// A provider saved before #192 trusts the default amr values.
	p.MFAAMR, p.MFAACR = append([]string{}, DefaultMFAAMR...), []string{}
	for _, c := range []struct {
		col  sql.NullString
		into *[]string
	}{{amr, &p.MFAAMR}, {acr, &p.MFAACR}} {
		if c.col.Valid {
			if err := json.Unmarshal([]byte(c.col.String), c.into); err != nil {
				return nil, fmt.Errorf("reading provider %s: %w", p.Key, err)
			}
		}
	}
	p.Settings = map[string]string{}
	for col, into := range map[string]any{settings: &p.Settings, roles: &p.DefaultRoleIDs, domains: &p.AllowedDomains, groups: &p.AllowedGroups} {
		if err := json.Unmarshal([]byte(col), into); err != nil {
			return nil, fmt.Errorf("reading provider %s: %w", p.Key, err)
		}
	}
	for _, list := range []*[]string{&p.DefaultRoleIDs, &p.AllowedDomains, &p.AllowedGroups, &p.MFAAMR, &p.MFAACR} {
		if *list == nil {
			*list = []string{}
		}
	}
	p.Rules = []ProviderRule{}
	return &p, nil
}

// Providers lists every identity provider, in the order of their buttons,
// with their rules.
func (s *Store) Providers() ([]Provider, error) {
	rows, err := s.Query("SELECT " + providerColumns + " FROM auth_providers ORDER BY position, name, id")
	if err != nil {
		return nil, fmt.Errorf("listing identity providers: %w", err)
	}
	out := []Provider{}
	for rows.Next() {
		p, err := scanProvider(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, *p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rules, err := s.providerRules("")
	if err != nil {
		return nil, err
	}
	for i := range out {
		if r := rules[out[i].ID]; r != nil {
			out[i].Rules = r
		}
	}
	return out, nil
}

// providerRules reads the rules of one provider, or of every one when id
// is empty, by provider.
func (s *Store) providerRules(id string) (map[string][]ProviderRule, error) {
	query, args := "SELECT provider_id, id, claim, claim_value, role_id, scope_ids FROM auth_provider_rules", []any{}
	if id != "" {
		query, args = query+" WHERE provider_id = ?", append(args, id)
	}
	rows, err := s.Query(query+" ORDER BY provider_id, position, id", args...)
	if err != nil {
		return nil, fmt.Errorf("listing provider rules: %w", err)
	}
	defer rows.Close()
	out := map[string][]ProviderRule{}
	for rows.Next() {
		var provider, scopes string
		var r ProviderRule
		if err := rows.Scan(&provider, &r.ID, &r.Claim, &r.Value, &r.RoleID, &scopes); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(scopes), &r.ScopeIDs); err != nil || r.ScopeIDs == nil {
			r.ScopeIDs = []string{}
		}
		out[provider] = append(out[provider], r)
	}
	return out, rows.Err()
}

func (s *Store) getProvider(where string, arg string) (*Provider, error) {
	p, err := scanProvider(s.QueryRow("SELECT "+providerColumns+" FROM auth_providers WHERE "+where+" = ?", arg))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrProviderNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("reading identity provider: %w", err)
	}
	rules, err := s.providerRules(p.ID)
	if err != nil {
		return nil, err
	}
	if r := rules[p.ID]; r != nil {
		p.Rules = r
	}
	return p, nil
}

// GetProvider reads a provider by ID.
func (s *Store) GetProvider(id string) (*Provider, error) { return s.getProvider("id", id) }

// GetProviderByKey reads a provider by its key.
func (s *Store) GetProviderByKey(key string) (*Provider, error) {
	return s.getProvider("provider_key", key)
}

// ProviderInput is a provider as saved. Seal seals its secrets, as JSON,
// for its ID; nil keeps those stored, as for one whose secrets are not
// changing.
type ProviderInput struct {
	Key            string
	Name           string
	Protocol       string
	Settings       map[string]string
	Seal           func(id string) (string, error)
	ButtonLabel    string
	Icon           string
	Enabled        bool
	Position       int
	Provision      bool
	DefaultRoleIDs []string
	AllowedDomains []string
	AllowedGroups  []string
	LinkByEmail    bool
	TrustEmail     bool
	// MFAAMR, left nil, is DefaultMFAAMR.
	MFAAMR         []string
	MFAACR         []string
	MFAFallback    bool
	MFAAlwaysLocal bool
	Rules          []ProviderRule
}

func (in *ProviderInput) normalize() error {
	in.Key, in.Name, in.ButtonLabel, in.Icon = strings.TrimSpace(in.Key), strings.TrimSpace(in.Name), strings.TrimSpace(in.ButtonLabel), strings.TrimSpace(in.Icon)
	if !providerKeyPattern.MatchString(in.Key) {
		return fmt.Errorf("%w: a key is a lowercase letter, then up to 63 lowercase letters, digits, dashes or underscores", ErrInvalidProvider)
	}
	if in.Name == "" {
		return fmt.Errorf("%w: a name is required", ErrInvalidProvider)
	}
	if in.ButtonLabel == "" {
		in.ButtonLabel = "Sign in with " + in.Name
	}
	for what, v := range map[string]string{"name": in.Name, "button label": in.ButtonLabel, "icon": in.Icon} {
		if err := textcheck.Check(what, v, textcheck.MaxName); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidProvider, err)
		}
	}
	if in.Settings == nil {
		in.Settings = map[string]string{}
	}
	clean := func(list []string, lower bool) []string {
		out := []string{}
		for _, v := range list {
			v = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(v), "@"))
			if lower {
				v = strings.ToLower(v)
			}
			if v != "" {
				out = append(out, v)
			}
		}
		return out
	}
	in.AllowedDomains, in.AllowedGroups = clean(in.AllowedDomains, true), clean(in.AllowedGroups, false)
	if in.MFAAMR == nil {
		in.MFAAMR = DefaultMFAAMR
	}
	in.MFAAMR, in.MFAACR = clean(in.MFAAMR, false), clean(in.MFAACR, false)
	if in.DefaultRoleIDs == nil {
		in.DefaultRoleIDs = []string{}
	}
	for i := range in.Rules {
		r := &in.Rules[i]
		r.Claim, r.Value = strings.TrimSpace(r.Claim), strings.TrimSpace(r.Value)
		if r.Claim == "" || r.Value == "" || r.RoleID == "" {
			return fmt.Errorf("%w: each rule needs a claim, a value and a role", ErrInvalidProvider)
		}
		if r.ScopeIDs == nil {
			r.ScopeIDs = []string{}
		}
	}
	return nil
}

func jsonText(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// CreateProvider adds a provider set up under Admin.
func (s *Store) CreateProvider(in ProviderInput) (*Provider, error) {
	return s.saveProvider("", in, false)
}

// UpdateProvider changes one set up under Admin.
func (s *Store) UpdateProvider(id string, in ProviderInput) (*Provider, error) {
	return s.saveProvider(id, in, false)
}

func (s *Store) saveProvider(id string, in ProviderInput, fromConfig bool) (*Provider, error) {
	if err := in.normalize(); err != nil {
		return nil, err
	}
	now := dbTime(time.Now())
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if id != "" {
		var wasConfig bool
		err := s.TxQueryRow(tx, "SELECT from_config FROM auth_providers WHERE id = ?", id).Scan(&wasConfig)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrProviderNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("reading identity provider: %w", err)
		}
		if wasConfig && !fromConfig {
			return nil, ErrProviderFromConfig
		}
	}
	var n int
	if err := s.TxQueryRow(tx, "SELECT COUNT(*) FROM auth_providers WHERE provider_key = ? AND id <> ?", in.Key, id).Scan(&n); err != nil {
		return nil, fmt.Errorf("checking the key: %w", err)
	}
	if n > 0 {
		return nil, ErrProviderKeyExists
	}
	for _, roleID := range in.DefaultRoleIDs {
		if err := s.TxQueryRow(tx, "SELECT COUNT(*) FROM roles WHERE id = ?", roleID).Scan(&n); err != nil || n == 0 {
			return nil, fmt.Errorf("%w: %s", ErrRoleNotFound, roleID)
		}
	}
	for _, r := range in.Rules {
		if _, err := s.mergeAssignments(tx, []models.RoleAssignment{{RoleID: r.RoleID, ScopeIDs: r.ScopeIDs}}); err != nil {
			return nil, err
		}
	}
	created := id == ""
	if created {
		id = uuid.NewString()
	}
	sealed := "{}"
	if in.Seal != nil {
		if sealed, err = in.Seal(id); err != nil {
			return nil, err
		}
	}
	settings, roles, domains, groups := jsonText(in.Settings), jsonText(in.DefaultRoleIDs), jsonText(in.AllowedDomains), jsonText(in.AllowedGroups)
	if created {
		_, err = s.TxExec(tx, "INSERT INTO auth_providers ("+providerColumns+") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			id, in.Key, in.Name, in.Protocol, settings, sealed, in.ButtonLabel, in.Icon, in.Enabled, in.Position, in.Provision,
			roles, domains, groups, in.LinkByEmail, in.TrustEmail, fromConfig, now, now,
			jsonText(in.MFAAMR), jsonText(in.MFAACR), in.MFAFallback, in.MFAAlwaysLocal)
	} else {
		_, err = s.TxExec(tx, `UPDATE auth_providers SET provider_key = ?, name = ?, protocol = ?, settings = ?, button_label = ?, icon = ?,
			enabled = ?, position = ?, provision = ?, default_role_ids = ?, allowed_domains = ?, allowed_groups = ?, link_by_email = ?,
			trust_email = ?, updated_at = ?, mfa_amr = ?, mfa_acr = ?, mfa_fallback = ?, mfa_always_local = ? WHERE id = ?`,
			in.Key, in.Name, in.Protocol, settings, in.ButtonLabel, in.Icon, in.Enabled, in.Position, in.Provision,
			roles, domains, groups, in.LinkByEmail, in.TrustEmail, now,
			jsonText(in.MFAAMR), jsonText(in.MFAACR), in.MFAFallback, in.MFAAlwaysLocal, id)
		if err == nil && in.Seal != nil {
			_, err = s.TxExec(tx, "UPDATE auth_providers SET secrets = ? WHERE id = ?", sealed, id)
		}
	}
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrProviderKeyExists
		}
		return nil, fmt.Errorf("saving identity provider: %w", err)
	}
	if _, err := s.TxExec(tx, "DELETE FROM auth_provider_rules WHERE provider_id = ?", id); err != nil {
		return nil, fmt.Errorf("replacing rules: %w", err)
	}
	for i, r := range in.Rules {
		if _, err := s.TxExec(tx, "INSERT INTO auth_provider_rules (id, provider_id, position, claim, claim_value, role_id, scope_ids) VALUES (?, ?, ?, ?, ?, ?, ?)",
			uuid.NewString(), id, i, r.Claim, r.Value, r.RoleID, jsonText(r.ScopeIDs)); err != nil {
			return nil, fmt.Errorf("adding a rule: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing: %w", err)
	}
	return s.GetProvider(id)
}

// DeleteProvider removes a provider set up under Admin, with the
// identities at it. The roles its rules gave stay, as given by hand.
func (s *Store) DeleteProvider(id string) error {
	p, err := s.GetProvider(id)
	if err != nil {
		return err
	}
	if p.FromConfig {
		return ErrProviderFromConfig
	}
	return s.deleteProvider(id)
}

func (s *Store) deleteProvider(id string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := s.TxExec(tx, "UPDATE user_roles SET granted_by = NULL WHERE granted_by = ?", id); err != nil {
		return fmt.Errorf("keeping the roles its rules gave: %w", err)
	}
	if _, err := s.TxExec(tx, "DELETE FROM auth_providers WHERE id = ?", id); err != nil {
		return fmt.Errorf("removing identity provider: %w", err)
	}
	return tx.Commit()
}

// SyncConfigProviders makes the providers from the configuration file
// those marked from_config, by key: added, changed, or removed when gone
// from the file. A provider of the same key set up under Admin is left
// alone, and reported.
func (s *Store) SyncConfigProviders(inputs []ProviderInput) error {
	existing, err := s.Providers()
	if err != nil {
		return err
	}
	byKey := map[string]Provider{}
	for _, p := range existing {
		byKey[p.Key] = p
	}
	keep := map[string]bool{}
	for i, in := range inputs {
		if in.Position == 0 {
			in.Position = -len(inputs) + i
		}
		in.Seal = nil
		old, ok := byKey[strings.TrimSpace(in.Key)]
		switch {
		case !ok:
			_, err = s.saveProvider("", in, true)
		case !old.FromConfig:
			return fmt.Errorf("identity provider %q is set in the configuration file and under Admin; remove one", old.Key)
		default:
			_, err = s.saveProvider(old.ID, in, true)
		}
		if err != nil {
			return fmt.Errorf("identity provider %q from the configuration: %w", in.Key, err)
		}
		keep[strings.TrimSpace(in.Key)] = true
	}
	for _, p := range existing {
		if p.FromConfig && !keep[p.Key] {
			if err := s.deleteProvider(p.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// Identity links a user to a provider's subject.
type Identity struct {
	ID           string     `json:"id"`
	UserID       string     `json:"user_id"`
	ProviderID   string     `json:"provider_id"`
	ProviderKey  string     `json:"provider_key"`
	ProviderName string     `json:"provider_name"`
	Subject      string     `json:"subject"`
	Email        string     `json:"email"`
	CreatedAt    time.Time  `json:"created_at"`
	LastUsedAt   *time.Time `json:"last_used_at,omitempty"`
}

const identityQuery = `SELECT i.id, i.user_id, i.provider_id, p.provider_key, p.name, i.subject, i.email, i.created_at, i.last_used_at
	FROM user_identities i JOIN auth_providers p ON p.id = i.provider_id`

func scanIdentity(row interface{ Scan(...any) error }) (*Identity, error) {
	var i Identity
	if err := row.Scan(&i.ID, &i.UserID, &i.ProviderID, &i.ProviderKey, &i.ProviderName, &i.Subject, &i.Email, &i.CreatedAt, &i.LastUsedAt); err != nil {
		return nil, err
	}
	return &i, nil
}

// IdentityBySubject finds the identity a provider's subject is linked as.
func (s *Store) IdentityBySubject(providerID, subject string) (*Identity, error) {
	i, err := scanIdentity(s.QueryRow(identityQuery+" WHERE i.provider_id = ? AND i.subject = ?", providerID, subject))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrIdentityNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("reading identity: %w", err)
	}
	return i, nil
}

// UserIdentities lists a user's identities, by provider.
func (s *Store) UserIdentities(userID string) ([]Identity, error) {
	rows, err := s.Query(identityQuery+" WHERE i.user_id = ? ORDER BY p.position, p.name, i.created_at", userID)
	if err != nil {
		return nil, fmt.Errorf("listing identities: %w", err)
	}
	defer rows.Close()
	out := []Identity{}
	for rows.Next() {
		i, err := scanIdentity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *i)
	}
	return out, rows.Err()
}

// LinkIdentity links a provider's subject to a user.
func (s *Store) LinkIdentity(userID, providerID, subject, email string) (*Identity, error) {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return nil, fmt.Errorf("%w: a subject is required", ErrInvalidProvider)
	}
	if err := textcheck.Check("subject", subject, textcheck.MaxName); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidProvider, err)
	}
	if _, err := s.GetUserByID(userID); err != nil {
		return nil, err
	}
	if _, err := s.GetProvider(providerID); err != nil {
		return nil, err
	}
	if existing, err := s.IdentityBySubject(providerID, subject); err == nil {
		if existing.UserID == userID {
			return existing, nil
		}
		return nil, ErrIdentityTaken
	}
	id := uuid.NewString()
	if _, err := s.Exec("INSERT INTO user_identities (id, user_id, provider_id, subject, email, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		id, userID, providerID, subject, truncateText(email, 255), dbTime(time.Now())); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrIdentityTaken
		}
		return nil, fmt.Errorf("linking identity: %w", err)
	}
	return s.IdentityBySubject(providerID, subject)
}

func truncateText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// TouchIdentity records a sign-in through an identity, with the email the
// provider gave this time.
func (s *Store) TouchIdentity(id, email string) error {
	if _, err := s.Exec("UPDATE user_identities SET last_used_at = ?, email = ? WHERE id = ?", dbTime(time.Now()), truncateText(email, 255), id); err != nil {
		return fmt.Errorf("recording the sign-in: %w", err)
	}
	return nil
}

// UnlinkIdentity removes one of a user's identities, unless it is the last
// way they have to sign in: with passwordsOn, a password counts as one.
func (s *Store) UnlinkIdentity(userID, identityID string, passwordsOn bool) error {
	u, err := s.GetUserByID(userID)
	if err != nil {
		return err
	}
	identities, err := s.UserIdentities(userID)
	if err != nil {
		return err
	}
	found := false
	for _, i := range identities {
		found = found || i.ID == identityID
	}
	if !found {
		return ErrIdentityNotFound
	}
	if len(identities) == 1 && !(passwordsOn && HasPassword(u)) {
		return ErrLastWayIn
	}
	if _, err := s.Exec("DELETE FROM user_identities WHERE id = ? AND user_id = ?", identityID, userID); err != nil {
		return fmt.Errorf("unlinking identity: %w", err)
	}
	return nil
}

// NoPassword is the password hash of an account made by a provider, which
// no password matches.
const NoPassword = "!"

// HasPassword reports whether someone can sign in with a password.
func HasPassword(u *models.User) bool {
	return u.PasswordHash != "" && u.PasswordHash != NoPassword
}

// CreateProvisionedUser makes an account for someone a provider vouches
// for, with no password, and the roles given by hand.
func (s *Store) CreateProvisionedUser(email, name string, roleIDs []string) (*models.User, error) {
	normalized := auth.NormalizeEmail(email)
	if normalized == "" {
		return nil, fmt.Errorf("%w: the provider gave no email", ErrInvalidUser)
	}
	if err := textcheck.Check("email", normalized, textcheck.MaxName); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidUser, err)
	}
	if strings.TrimSpace(name) == "" {
		name = normalized
	}
	name, err := normalizeUserName(truncateText(strings.TrimSpace(name), textcheck.MaxName))
	if err != nil {
		return nil, err
	}
	now := dbTime(time.Now())
	userID := uuid.NewString()
	if _, err := s.Exec("INSERT INTO users (id, email, name, password_hash, is_active, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		userID, normalized, name, NoPassword, true, now, now); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrEmailTaken
		}
		return nil, fmt.Errorf("inserting user: %w", err)
	}
	if len(roleIDs) > 0 {
		if err := s.SetUserRoles(userID, roleIDs); err != nil {
			return nil, err
		}
	}
	return s.GetUserByID(userID)
}

// ApplyProviderRoles makes the roles a provider's rules gave a user those
// wanted: adding, narrowing again, and taking away only roles its rules
// gave, never those given by hand or by another provider. Taking away the
// last users.manage is refused by anti-lockout: the role is then kept, and
// auth.role_kept recorded. It reports the names of roles kept so.
func (s *Store) ApplyProviderRoles(userID, providerID string, wanted []models.RoleAssignment) ([]string, error) {
	kept, err := s.applyProviderRoles(userID, providerID, wanted, false)
	if !errors.Is(err, rbac.ErrAntiLockoutTriggered) {
		return nil, err
	}
	kept, err = s.applyProviderRoles(userID, providerID, wanted, true)
	if err != nil {
		return nil, err
	}
	if len(kept) > 0 {
		u, err := s.GetUserByID(userID)
		if err != nil {
			return kept, err
		}
		tx, err := s.DB.Begin()
		if err != nil {
			return kept, err
		}
		defer tx.Rollback()
		note := "Kept " + strings.Join(kept, ", ") + ": the provider no longer gives it, but it holds the last users.manage."
		if err := s.insertEvent(tx, models.EventAuthRoleKept, nil, nil, models.EventPayload{Trigger: models.TriggerManual,
			Account: &models.EventActor{ID: u.ID, Name: u.Name}, Note: note}); err != nil {
			return kept, err
		}
		return kept, tx.Commit()
	}
	return kept, nil
}

func (s *Store) applyProviderRoles(userID, providerID string, wanted []models.RoleAssignment, keepManage bool) ([]string, error) {
	var kept []string
	err := s.withAdminGuard(func(tx *sql.Tx) error {
		merged, err := s.mergeAssignments(tx, wanted)
		if err != nil {
			return err
		}
		want := map[string]models.RoleAssignment{}
		for _, a := range merged {
			want[a.RoleID] = a
		}
		rows, err := s.TxQuery(tx, "SELECT role_id, granted_by FROM user_roles WHERE user_id = ?", userID)
		if err != nil {
			return fmt.Errorf("reading roles: %w", err)
		}
		held := map[string]*string{}
		for rows.Next() {
			var role string
			var by *string
			if err := rows.Scan(&role, &by); err != nil {
				rows.Close()
				return err
			}
			held[role] = by
		}
		rows.Close()
		for role, by := range held {
			if by == nil || *by != providerID {
				continue
			}
			if _, ok := want[role]; ok {
				continue
			}
			if keepManage {
				var name string
				var n int
				if err := s.TxQueryRow(tx, `SELECT r.name, COUNT(p.id) FROM roles r
					LEFT JOIN role_permissions rp ON rp.role_id = r.id
					LEFT JOIN permissions p ON p.id = rp.permission_id AND p.permission_key = ?
					WHERE r.id = ? GROUP BY r.name`, "users.manage", role).Scan(&name, &n); err != nil {
					return fmt.Errorf("reading role: %w", err)
				}
				if n > 0 {
					kept = append(kept, name)
					continue
				}
			}
			if _, err := s.TxExec(tx, "DELETE FROM user_roles WHERE user_id = ? AND role_id = ?", userID, role); err != nil {
				return fmt.Errorf("taking a role away: %w", err)
			}
		}
		for role, a := range want {
			by, has := held[role]
			if has && (by == nil || *by != providerID) {
				continue // given by hand, or by another provider: left alone
			}
			if has {
				if _, err := s.TxExec(tx, "DELETE FROM user_roles WHERE user_id = ? AND role_id = ?", userID, role); err != nil {
					return fmt.Errorf("narrowing a role again: %w", err)
				}
			}
			if err := s.insertAssignmentsTx(tx, userAssignments, userID, []models.RoleAssignment{a}); err != nil {
				return err
			}
			if _, err := s.TxExec(tx, "UPDATE user_roles SET granted_by = ? WHERE user_id = ? AND role_id = ?", providerID, userID, role); err != nil {
				return fmt.Errorf("recording who gave a role: %w", err)
			}
		}
		return nil
	})
	return kept, err
}

// RoleGrants maps each of a user's roles given by a provider's rule to the
// provider; roles given by hand are left out.
func (s *Store) RoleGrants(userID string) (map[string]string, error) {
	rows, err := s.Query("SELECT role_id, granted_by FROM user_roles WHERE user_id = ? AND granted_by IS NOT NULL", userID)
	if err != nil {
		return nil, fmt.Errorf("reading who gave roles: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var role, by string
		if err := rows.Scan(&role, &by); err != nil {
			return nil, err
		}
		out[role] = by
	}
	return out, rows.Err()
}

// ProviderSignIn is a sign-in sent to a provider, as it comes back.
type ProviderSignIn struct {
	ProviderID string
	State      map[string]string
	Next       string
	LinkUserID string
}

// CreateProviderSignIn records a sign-in sent to a provider, answering the
// token for the browser's cookie; only its hash is stored.
func (s *Store) CreateProviderSignIn(in ProviderSignIn) (string, error) {
	token, err := auth.GenerateSessionToken()
	if err != nil {
		return "", err
	}
	now := dbTime(time.Now())
	if _, err := s.Exec("DELETE FROM provider_sign_ins WHERE expires_at <= ?", now); err != nil {
		return "", fmt.Errorf("forgetting expired sign-ins: %w", err)
	}
	var link *string
	if in.LinkUserID != "" {
		link = &in.LinkUserID
	}
	if _, err := s.Exec("INSERT INTO provider_sign_ins (id, provider_id, state, next_path, link_user_id, expires_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		sessionKey(token), in.ProviderID, jsonText(in.State), in.Next, link, now.Add(ProviderSignInTTL), now); err != nil {
		return "", fmt.Errorf("starting the sign-in: %w", err)
	}
	return token, nil
}

// TakeProviderSignIn reads a sign-in back, once: a second answer for it is
// refused.
func (s *Store) TakeProviderSignIn(token string) (*ProviderSignIn, error) {
	if token == "" {
		return nil, ErrSignInNotFound
	}
	key := sessionKey(token)
	var in ProviderSignIn
	var state string
	var link *string
	err := s.QueryRow("SELECT provider_id, state, next_path, link_user_id FROM provider_sign_ins WHERE id = ? AND expires_at > ?",
		key, dbTime(time.Now())).Scan(&in.ProviderID, &state, &in.Next, &link)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSignInNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("reading the sign-in: %w", err)
	}
	res, err := s.Exec("DELETE FROM provider_sign_ins WHERE id = ?", key)
	if err != nil {
		return nil, fmt.Errorf("ending the sign-in: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrSignInNotFound
	}
	if link != nil {
		in.LinkUserID = *link
	}
	if err := json.Unmarshal([]byte(state), &in.State); err != nil {
		return nil, fmt.Errorf("reading the sign-in: %w", err)
	}
	return &in, nil
}

// LocalPasswordsSetting turns signing in with a password off ("off") or on.
const LocalPasswordsSetting = "auth.local_passwords"

// Setting reads an instance setting, or def when it is not set.
func (s *Store) Setting(key, def string) (string, error) {
	var v string
	err := s.QueryRow("SELECT value FROM settings WHERE setting_key = ?", key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return def, nil
	}
	if err != nil {
		return "", fmt.Errorf("reading setting %s: %w", key, err)
	}
	return v, nil
}

// SetSetting writes an instance setting.
func (s *Store) SetSetting(key, value string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if err := s.setSettingTx(tx, key, value); err != nil {
		return err
	}
	return tx.Commit()
}

// setSettingTx writes an instance setting within tx.
func (s *Store) setSettingTx(tx *sql.Tx, key, value string) error {
	if _, err := s.TxExec(tx, "DELETE FROM settings WHERE setting_key = ?", key); err != nil {
		return fmt.Errorf("writing setting %s: %w", key, err)
	}
	if _, err := s.TxExec(tx, "INSERT INTO settings (setting_key, value, updated_at) VALUES (?, ?, ?)", key, value, dbTime(time.Now())); err != nil {
		return fmt.Errorf("writing setting %s: %w", key, err)
	}
	return nil
}

// LocalPasswords reports whether signing in with a password is on.
func (s *Store) LocalPasswords() (bool, error) {
	v, err := s.Setting(LocalPasswordsSetting, "on")
	return v != "off", err
}

// RecordBreakGlass records someone signing in with a password while
// passwords are off.
func (s *Store) RecordBreakGlass(u *models.User) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if err := s.As("").insertEvent(tx, models.EventAuthBreakGlass, nil, nil, models.EventPayload{Trigger: models.TriggerManual,
		Account: &models.EventActor{ID: u.ID, Name: u.Name}}); err != nil {
		return err
	}
	return tx.Commit()
}
