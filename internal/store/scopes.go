package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/google/uuid"
)

var (
	ErrScopeNotFound = errors.New("scope not found")
	// ErrScopeReserved is returned for renaming or deleting the default or
	// public scope, or labeling a category with the default one: assets are
	// in it when they are in no other.
	ErrScopeReserved = errors.New("the default and public scopes are built in and cannot be renamed or deleted; default, where assets in no other scope are, cannot be put on categories")
	// ErrScopeInUse is returned for deleting a scope that labels categories,
	// that roles hold permissions in, or that roles are assigned in.
	ErrScopeInUse = errors.New("scope in use")
	// ErrInvalidScope is returned for a name that cannot name a scope.
	ErrInvalidScope = errors.New("a scope name is 1 to 64 lowercase letters, digits, - and _, starting with a letter or digit")
)

// ListScopes returns every scope, by name, with how many categories and
// roles use it.
func (s *Store) ListScopes() ([]models.Scope, error) {
	rows, err := s.Query("SELECT id, name, created_at FROM scopes ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("querying scopes: %w", err)
	}
	defer rows.Close()
	scopes := []models.Scope{}
	for rows.Next() {
		var sc models.Scope
		if err := rows.Scan(&sc.ID, &sc.Name, &sc.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning scope: %w", err)
		}
		sc.Reserved = reservedScope(sc.Name)
		scopes = append(scopes, sc)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	categories, err := s.countBy("SELECT scope_id, COUNT(*) FROM category_scopes GROUP BY scope_id")
	if err != nil {
		return nil, err
	}
	roles, err := s.countBy("SELECT scope_id, COUNT(DISTINCT role_id) FROM role_permission_scopes GROUP BY scope_id")
	if err != nil {
		return nil, err
	}
	// Assignments of users who have left, and of tokens revoked for good,
	// hold nothing, and go when the scope does.
	assignments := map[string]int{}
	for _, query := range []string{
		`SELECT urs.scope_id, COUNT(*) FROM user_role_scopes urs JOIN users u ON u.id = urs.user_id
			WHERE u.archived_at IS NULL GROUP BY urs.scope_id`,
		`SELECT ats.scope_id, COUNT(*) FROM api_token_role_scopes ats JOIN api_tokens t ON t.id = ats.token_id
			WHERE t.revoked_at IS NULL GROUP BY ats.scope_id`,
	} {
		counts, err := s.countBy(query)
		if err != nil {
			return nil, err
		}
		for id, n := range counts {
			assignments[id] += n
		}
	}
	for i := range scopes {
		id := scopes[i].ID
		scopes[i].CategoryCount, scopes[i].RoleCount, scopes[i].AssignmentCount = categories[id], roles[id], assignments[id]
	}
	return scopes, nil
}

// countBy reads a query of IDs and counts into a map.
func (s *Store) countBy(query string, args ...any) (map[string]int, error) {
	rows, err := s.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("counting: %w", err)
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, fmt.Errorf("scanning count: %w", err)
		}
		counts[id] = n
	}
	return counts, rows.Err()
}

// GetScope returns a scope, or ErrScopeNotFound.
func (s *Store) GetScope(id string) (*models.Scope, error) {
	scopes, err := s.ListScopes()
	if err != nil {
		return nil, err
	}
	for i := range scopes {
		if scopes[i].ID == id {
			return &scopes[i], nil
		}
	}
	return nil, ErrScopeNotFound
}

// CreateScope creates a scope. A name in use fails as a unique violation.
func (s *Store) CreateScope(name string) (*models.Scope, error) {
	name = strings.TrimSpace(name)
	if !rbac.ValidScopeName(name) {
		return nil, ErrInvalidScope
	}
	id := uuid.NewString()
	if _, err := s.Exec("INSERT INTO scopes (id, name, created_at) VALUES (?, ?, ?)", id, name, time.Now().UTC()); err != nil {
		return nil, fmt.Errorf("inserting scope: %w", err)
	}
	return s.GetScope(id)
}

// RenameScope renames a scope; roles' permissions in it, and roles
// assigned in it, follow.
func (s *Store) RenameScope(id, name string) (*models.Scope, error) {
	name = strings.TrimSpace(name)
	if !rbac.ValidScopeName(name) {
		return nil, ErrInvalidScope
	}
	if err := s.editableScope(id); err != nil {
		return nil, err
	}
	if _, err := s.Exec("UPDATE scopes SET name = ? WHERE id = ?", name, id); err != nil {
		return nil, fmt.Errorf("renaming scope: %w", err)
	}
	return s.GetScope(id)
}

// DeleteScope deletes a scope no category, role or assignment uses.
func (s *Store) DeleteScope(id string) error {
	if err := s.editableScope(id); err != nil {
		return err
	}
	sc, err := s.GetScope(id)
	if err != nil {
		return err
	}
	if sc.CategoryCount > 0 || sc.RoleCount > 0 || sc.AssignmentCount > 0 {
		return fmt.Errorf("%w: it labels %d categories, %d roles hold permissions in it, and %d roles of users and tokens are narrowed to it",
			ErrScopeInUse, sc.CategoryCount, sc.RoleCount, sc.AssignmentCount)
	}
	if _, err := s.Exec("DELETE FROM scopes WHERE id = ?", id); err != nil {
		return fmt.Errorf("deleting scope: %w", err)
	}
	return nil
}

// editableScope checks that a scope exists and is not built in.
func (s *Store) editableScope(id string) error {
	var name string
	err := s.QueryRow("SELECT name FROM scopes WHERE id = ?", id).Scan(&name)
	if err == sql.ErrNoRows {
		return ErrScopeNotFound
	}
	if err != nil {
		return fmt.Errorf("reading scope: %w", err)
	}
	if reservedScope(name) {
		return ErrScopeReserved
	}
	return nil
}

// reservedScope reports whether a scope is built in.
func reservedScope(name string) bool {
	return name == rbac.DefaultScope || name == rbac.PublicScope
}

// CategoryScopes lists the IDs of the scopes on a category itself, not
// those it has from its ancestors.
func (s *Store) CategoryScopes(categoryID string) ([]string, error) {
	if _, err := s.GetCategoryByID(categoryID); err != nil {
		return nil, err
	}
	rows, err := s.Query("SELECT scope_id FROM category_scopes WHERE category_id = ? ORDER BY scope_id", categoryID)
	if err != nil {
		return nil, fmt.Errorf("reading category scopes: %w", err)
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

// SetCategoryScopes replaces the scopes on a category.
func (s *Store) SetCategoryScopes(categoryID string, scopeIDs []string) ([]string, error) {
	if _, err := s.GetCategoryByID(categoryID); err != nil {
		return nil, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := s.TxExec(tx, "DELETE FROM category_scopes WHERE category_id = ?", categoryID); err != nil {
		return nil, fmt.Errorf("clearing category scopes: %w", err)
	}
	seen := map[string]bool{}
	for _, id := range scopeIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		var name string
		err := s.TxQueryRow(tx, "SELECT name FROM scopes WHERE id = ?", id).Scan(&name)
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("%w: %s", ErrScopeNotFound, id)
		}
		if err != nil {
			return nil, fmt.Errorf("checking scope: %w", err)
		}
		if name == rbac.DefaultScope {
			return nil, ErrScopeReserved
		}
		if _, err := s.TxExec(tx, "INSERT INTO category_scopes (category_id, scope_id) VALUES (?, ?)", categoryID, id); err != nil {
			return nil, fmt.Errorf("labeling category: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing: %w", err)
	}
	return s.CategoryScopes(categoryID)
}

// readCategoryScopes maps each category to the names of its own scopes.
func readCategoryScopes(q interface {
	Query(string, ...any) (*sql.Rows, error)
}) (map[string][]string, error) {
	rows, err := q.Query("SELECT cs.category_id, sc.name FROM category_scopes cs JOIN scopes sc ON sc.id = cs.scope_id")
	if err != nil {
		return nil, fmt.Errorf("reading category scopes: %w", err)
	}
	defer rows.Close()
	own := map[string][]string{}
	for rows.Next() {
		var cat, name string
		if err := rows.Scan(&cat, &name); err != nil {
			return nil, fmt.Errorf("scanning category scope: %w", err)
		}
		own[cat] = append(own[cat], name)
	}
	return own, rows.Err()
}

// scopesOf returns the scopes of an asset in these categories.
func (t categoryTraits) scopesOf(categoryIDs []string) []string {
	return rbac.EffectiveScopes(categoryIDs, func(id string) string { return t.parents[id] }, func(id string) []string { return t.scopes[id] })
}

// ScopesForCategories returns the scopes an asset in these categories
// would be in.
func (s *Store) ScopesForCategories(categoryIDs []string) ([]string, error) {
	scopesOf, err := s.ScopeResolver()
	if err != nil {
		return nil, err
	}
	return scopesOf(categoryIDs), nil
}

// ScopeResolver reads the categories once and returns what
// ScopesForCategories would, for any categories, as they are now.
func (s *Store) ScopeResolver() (func(categoryIDs []string) []string, error) {
	traits, err := s.categoryTraits(storeQuerier{s})
	if err != nil {
		return nil, err
	}
	return traits.scopesOf, nil
}

// AssetScopes maps each of these assets to its scopes. Unknown IDs are left
// out.
func (s *Store) AssetScopes(ids []string) (map[string][]string, error) {
	out := map[string][]string{}
	if len(ids) == 0 {
		return out, nil
	}
	traits, err := s.categoryTraits(storeQuerier{s})
	if err != nil {
		return nil, err
	}
	unique := map[string]bool{}
	for _, id := range ids {
		unique[id] = true
	}
	cats := map[string][]string{}
	query, args := "SELECT a.id, ac.category_id FROM assets a LEFT JOIN asset_categories ac ON ac.asset_id = a.id", []any{}
	// A long list reads every asset rather than naming each.
	if len(unique) <= 200 {
		keys := make([]string, 0, len(unique))
		for id := range unique {
			keys = append(keys, id)
			args = append(args, id)
		}
		query += " WHERE a.id IN (" + strings.TrimSuffix(strings.Repeat("?, ", len(keys)), ", ") + ")"
	}
	rows, err := s.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("reading asset categories: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var cat sql.NullString
		if err := rows.Scan(&id, &cat); err != nil {
			return nil, fmt.Errorf("scanning asset category: %w", err)
		}
		if !unique[id] {
			continue
		}
		if _, ok := cats[id]; !ok {
			cats[id] = nil
		}
		if cat.Valid {
			cats[id] = append(cats[id], cat.String)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for id, c := range cats {
		out[id] = traits.scopesOf(c)
	}
	return out, nil
}

// grantedKeys reads the permission keys roles grant, in every scope and
// in some, as scope:key. roles is SQL selecting the role IDs, taking args.
func grantedKeys(q interface {
	Query(string, ...any) (*sql.Rows, error)
}, roles string, args ...any) (map[string]bool, error) {
	byRole, err := roleKeys(q, roles, args...)
	if err != nil {
		return nil, err
	}
	keys := map[string]bool{}
	for _, ks := range byRole {
		for _, k := range ks {
			keys[k] = true
		}
	}
	return keys, nil
}
