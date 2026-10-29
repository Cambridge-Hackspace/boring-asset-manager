package store

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

// ErrInvalidAssignment wraps the reason roles cannot be assigned as given.
var ErrInvalidAssignment = errors.New("invalid role assignment")

// assignmentTables are where roles are assigned to one kind of holder, users
// or API tokens: the assignments, the scopes each is narrowed to, and the
// column naming the holder in both.
type assignmentTables struct {
	roles, scopes, holder string
}

var (
	userAssignments  = assignmentTables{roles: "user_roles", scopes: "user_role_scopes", holder: "user_id"}
	tokenAssignments = assignmentTables{roles: "api_token_roles", scopes: "api_token_role_scopes", holder: "token_id"}
)

// roleKeys maps each role to its permission keys, in every scope and, as
// scope:key, in some. roles is SQL selecting the role IDs, taking args.
func roleKeys(q interface {
	Query(string, ...any) (*sql.Rows, error)
}, roles string, args ...any) (map[string][]string, error) {
	out := map[string][]string{}
	for _, query := range []string{
		`SELECT rp.role_id, '', p.permission_key FROM role_permissions rp
			JOIN permissions p ON p.id = rp.permission_id
			WHERE rp.role_id IN (` + roles + `)`,
		`SELECT rps.role_id, sc.name, p.permission_key FROM role_permission_scopes rps
			JOIN permissions p ON p.id = rps.permission_id
			JOIN scopes sc ON sc.id = rps.scope_id
			WHERE rps.role_id IN (` + roles + `)`,
	} {
		rows, err := q.Query(query, args...)
		if err != nil {
			return nil, fmt.Errorf("querying permissions: %w", err)
		}
		for rows.Next() {
			var role, scope, key string
			if err := rows.Scan(&role, &scope, &key); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scanning permission key: %w", err)
			}
			if scope != "" {
				key = scope + rbac.ScopeSeparator + key
			}
			out[role] = append(out[role], key)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// assignedKeys reads what the roles assigned to a holder grant, each
// narrowed to the scopes its assignment names (README §3.3). roles is SQL
// selecting the holder's role IDs, taking args, so a caller can require the
// holder to be active.
func assignedKeys(q interface {
	Query(string, ...any) (*sql.Rows, error)
}, t assignmentTables, holder, roles string, args ...any) (map[string]bool, error) {
	byRole, err := roleKeys(q, roles, args...)
	if err != nil {
		return nil, err
	}
	narrowed, err := assignmentScopeNames(q, t, holder)
	if err != nil {
		return nil, err
	}
	keys := map[string]bool{}
	for role, ks := range byRole {
		for _, k := range rbac.Narrow(ks, narrowed[role]) {
			keys[k] = true
		}
	}
	return keys, nil
}

// assignmentScopeNames maps each role a holder holds narrowed to the names
// of its scopes.
func assignmentScopeNames(q interface {
	Query(string, ...any) (*sql.Rows, error)
}, t assignmentTables, holder string) (map[string][]string, error) {
	rows, err := q.Query(`SELECT n.role_id, sc.name FROM `+t.scopes+` n JOIN scopes sc ON sc.id = n.scope_id
		WHERE n.`+t.holder+` = ?`, holder)
	if err != nil {
		return nil, fmt.Errorf("reading assignment scopes: %w", err)
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var role, name string
		if err := rows.Scan(&role, &name); err != nil {
			return nil, fmt.Errorf("scanning assignment scope: %w", err)
		}
		out[role] = append(out[role], name)
	}
	return out, rows.Err()
}

// readAssignments maps each holder with roles to its assignments, by role
// ID, each with the IDs of the scopes it is narrowed to, sorted.
func (s *Store) readAssignments(t assignmentTables) (map[string][]models.RoleAssignment, error) {
	narrowed := map[[2]string][]string{}
	rows, err := s.Query("SELECT " + t.holder + ", role_id, scope_id FROM " + t.scopes)
	if err != nil {
		return nil, fmt.Errorf("reading assignment scopes: %w", err)
	}
	for rows.Next() {
		var holder, role, scope string
		if err := rows.Scan(&holder, &role, &scope); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scanning assignment scope: %w", err)
		}
		narrowed[[2]string{holder, role}] = append(narrowed[[2]string{holder, role}], scope)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}

	rows, err = s.Query("SELECT " + t.holder + ", role_id FROM " + t.roles)
	if err != nil {
		return nil, fmt.Errorf("reading assignments: %w", err)
	}
	defer rows.Close()
	out := map[string][]models.RoleAssignment{}
	for rows.Next() {
		var holder, role string
		if err := rows.Scan(&holder, &role); err != nil {
			return nil, fmt.Errorf("scanning assignment: %w", err)
		}
		scopes := narrowed[[2]string{holder, role}]
		if scopes == nil {
			scopes = []string{}
		}
		sort.Strings(scopes)
		out[holder] = append(out[holder], models.RoleAssignment{RoleID: role, ScopeIDs: scopes})
	}
	for _, list := range out {
		sort.Slice(list, func(i, j int) bool { return list[i].RoleID < list[j].RoleID })
	}
	return out, rows.Err()
}

// mergeAssignments checks assignments and merges those of the same role:
// over the union of their scopes, or unrestricted when any of them is.
// Unknown roles are ErrRoleNotFound, unknown scopes ErrScopeNotFound, and
// narrowing the public role, which no one is assigned, ErrInvalidAssignment.
func (s *Store) mergeAssignments(tx *sql.Tx, assignments []models.RoleAssignment) ([]models.RoleAssignment, error) {
	var order []string
	merged := map[string]map[string]bool{}
	unrestricted := map[string]bool{}
	for _, a := range assignments {
		if _, ok := merged[a.RoleID]; !ok {
			var name string
			err := s.TxQueryRow(tx, "SELECT name FROM roles WHERE id = ?", a.RoleID).Scan(&name)
			if err == sql.ErrNoRows {
				return nil, fmt.Errorf("%w: %s", ErrRoleNotFound, a.RoleID)
			}
			if err != nil {
				return nil, fmt.Errorf("checking role: %w", err)
			}
			if name == rbac.PublicRole && len(a.ScopeIDs) > 0 {
				return nil, fmt.Errorf("%w: the public role is everyone's and cannot be narrowed", ErrInvalidAssignment)
			}
			merged[a.RoleID] = map[string]bool{}
			order = append(order, a.RoleID)
		}
		if len(a.ScopeIDs) == 0 {
			unrestricted[a.RoleID] = true
		}
		for _, id := range a.ScopeIDs {
			if merged[a.RoleID][id] {
				continue
			}
			var n int
			if err := s.TxQueryRow(tx, "SELECT COUNT(*) FROM scopes WHERE id = ?", id).Scan(&n); err != nil {
				return nil, fmt.Errorf("checking scope: %w", err)
			}
			if n == 0 {
				return nil, fmt.Errorf("%w: %s", ErrScopeNotFound, id)
			}
			merged[a.RoleID][id] = true
		}
	}
	out := make([]models.RoleAssignment, 0, len(order))
	for _, role := range order {
		scopes := []string{}
		if !unrestricted[role] {
			for id := range merged[role] {
				scopes = append(scopes, id)
			}
			sort.Strings(scopes)
		}
		out = append(out, models.RoleAssignment{RoleID: role, ScopeIDs: scopes})
	}
	return out, nil
}

// insertAssignmentsTx assigns already merged roles to a holder that has
// none.
func (s *Store) insertAssignmentsTx(tx *sql.Tx, t assignmentTables, holder string, assignments []models.RoleAssignment) error {
	for _, a := range assignments {
		if _, err := s.TxExec(tx, "INSERT INTO "+t.roles+" ("+t.holder+", role_id) VALUES (?, ?)", holder, a.RoleID); err != nil {
			return fmt.Errorf("assigning role: %w", err)
		}
		for _, scope := range a.ScopeIDs {
			if _, err := s.TxExec(tx, "INSERT INTO "+t.scopes+" ("+t.holder+", role_id, scope_id) VALUES (?, ?, ?)", holder, a.RoleID, scope); err != nil {
				return fmt.Errorf("narrowing role: %w", err)
			}
		}
	}
	return nil
}

// UserRoleAssignments maps each user with roles to their assignments.
func (s *Store) UserRoleAssignments() (map[string][]models.RoleAssignment, error) {
	return s.readAssignments(userAssignments)
}

// SetUserRoleAssignments replaces a user's roles, each in every scope or
// narrowed to some, under the anti-lockout guard: an assignment narrowed to
// scopes grants no users.manage. The same role given twice is held once,
// over the union of its scopes.
func (s *Store) SetUserRoleAssignments(userID string, assignments []models.RoleAssignment) error {
	return s.withAdminGuard(func(tx *sql.Tx) error {
		archived, err := s.userArchived(tx, userID)
		if err != nil {
			return err
		}
		if archived {
			return ErrUserNotFound
		}
		merged, err := s.mergeAssignments(tx, assignments)
		if err != nil {
			return err
		}
		// Its narrowing goes with each assignment (ON DELETE CASCADE).
		if _, err := s.TxExec(tx, "DELETE FROM user_roles WHERE user_id = ?", userID); err != nil {
			return fmt.Errorf("clearing roles: %w", err)
		}
		return s.insertAssignmentsTx(tx, userAssignments, userID, merged)
	})
}
