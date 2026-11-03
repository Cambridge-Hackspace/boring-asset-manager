package store

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// EmailRecipients returns the addresses email about e reaches through the
// roles named (by name, ignoring case): those of active users holding one,
// who have not turned email off (docs/notifications.md). A role narrowed to
// scopes reaches its holder only for events about something in one of
// them, and an event about no asset or category only those holding it
// everywhere. With everyone, as for a channel's test message, narrowing is
// ignored. Addresses come sorted, each once.
func (s *Store) EmailRecipients(roleNames []string, e models.Event, everyone bool) ([]string, error) {
	if len(roleNames) == 0 {
		return nil, nil
	}
	marks := make([]string, len(roleNames))
	args := make([]any, len(roleNames))
	for i, n := range roleNames {
		marks[i], args[i] = "?", strings.ToLower(strings.TrimSpace(n))
	}
	rows, err := s.Query(`SELECT u.email, ur.user_id, ur.role_id
		FROM user_roles ur
		JOIN roles r ON r.id = ur.role_id
		JOIN users u ON u.id = ur.user_id
		WHERE u.is_active = TRUE AND u.archived_at IS NULL AND u.email_notifications = TRUE
		AND LOWER(r.name) IN (`+strings.Join(marks, ", ")+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("finding email recipients: %w", err)
	}
	type holding struct{ email, user, role string }
	var held []holding
	for rows.Next() {
		var h holding
		if err := rows.Scan(&h.email, &h.user, &h.role); err != nil {
			rows.Close()
			return nil, fmt.Errorf("finding email recipients: %w", err)
		}
		held = append(held, h)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("finding email recipients: %w", err)
	}

	// Each narrowed assignment's scopes; an assignment with none holds
	// everywhere.
	narrowed := map[string][]string{}
	if !everyone && len(held) > 0 {
		// By name, as EventPlaces gives the scopes of places.
		rows, err := s.Query("SELECT urs.user_id, urs.role_id, sc.name FROM user_role_scopes urs JOIN scopes sc ON sc.id = urs.scope_id")
		if err != nil {
			return nil, fmt.Errorf("reading narrowed roles: %w", err)
		}
		for rows.Next() {
			var user, role, scope string
			if err := rows.Scan(&user, &role, &scope); err != nil {
				rows.Close()
				return nil, fmt.Errorf("reading narrowed roles: %w", err)
			}
			narrowed[user+"/"+role] = append(narrowed[user+"/"+role], scope)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("reading narrowed roles: %w", err)
		}
	}
	var place []string
	if !everyone && (e.AssetID != nil || e.CategoryID != nil) {
		_, scopes, err := s.EventPlaces([]models.Event{e})
		if err != nil {
			return nil, err
		}
		if e.AssetID != nil {
			place = scopes[*e.AssetID]
		} else {
			place = scopes[*e.CategoryID]
		}
	}
	inPlace := map[string]bool{}
	for _, sc := range place {
		inPlace[sc] = true
	}

	seen := map[string]bool{}
	var out []string
	for _, h := range held {
		if only := narrowed[h.user+"/"+h.role]; len(only) > 0 {
			reached := false
			for _, sc := range only {
				reached = reached || inPlace[sc]
			}
			if !reached {
				continue
			}
		}
		if !seen[h.email] {
			seen[h.email] = true
			out = append(out, h.email)
		}
	}
	sort.Strings(out)
	return out, nil
}
