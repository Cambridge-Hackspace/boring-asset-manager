package migrations

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

// HooksVersion is the migration that renamed actions on assets to hooks
// (#173). Archives written before it say actions; restoring one migrates
// it forward, and importing a package converts what it holds with
// HookDefinition, HookScanSpec and HookEvent.
const HooksVersion = 49

// renameHooks carries over what was stored under the names actions had
// before they were hooks: the permission assets.act becomes assets.hook,
// with every role holding it; channels following asset.action follow
// asset.hook; events of that kind become hook events; flows whose Change
// steps pressed an action trigger the same hook; and scan templates
// offering an asset's actions offer its hooks. The tables keep their
// names. Run again, it changes nothing.
func renameHooks(db *sql.DB, dialect, _ string) error {
	var held int
	if err := db.QueryRow("SELECT COUNT(*) FROM permissions WHERE permission_key = 'assets.hook'").Scan(&held); err != nil {
		return fmt.Errorf("reading permissions: %w", err)
	}
	if held == 0 {
		if _, err := db.Exec("UPDATE permissions SET permission_key = 'assets.hook', description = 'Core permission: assets.hook' WHERE permission_key = 'assets.act'"); err != nil {
			return fmt.Errorf("renaming assets.act: %w", err)
		}
	} else if err := mergeActPermission(db); err != nil {
		return err
	}
	if _, err := db.Exec(`UPDATE notification_channels SET events = REPLACE(events, '"asset.action"', '"asset.hook"') WHERE events LIKE '%"asset.action"%'`); err != nil {
		return fmt.Errorf("renaming channels' events: %w", err)
	}
	if err := rewriteColumn(db, dialect, "SELECT id, payload FROM events WHERE kind = 'asset.action'",
		"UPDATE events SET kind = 'asset.hook', payload = ? WHERE id = ?", func(payload string) (string, bool, error) {
			_, next, err := HookEvent("asset.action", payload)
			return next, true, err
		}); err != nil {
		return fmt.Errorf("renaming action events: %w", err)
	}
	if err := rewriteColumn(db, dialect, "SELECT id, definition FROM flows", "UPDATE flows SET definition = ? WHERE id = ?", HookDefinition); err != nil {
		return fmt.Errorf("renaming flows' actions: %w", err)
	}
	if err := rewriteColumn(db, dialect, "SELECT id, spec FROM reports WHERE kind = 'scan'", "UPDATE reports SET spec = ? WHERE id = ?", HookScanSpec); err != nil {
		return fmt.Errorf("renaming scan templates' actions: %w", err)
	}
	return nil
}

// mergeActPermission moves the grants of assets.act to assets.hook, where
// both exist, and removes assets.act.
func mergeActPermission(db *sql.DB) error {
	for _, stmt := range []string{
		`INSERT INTO role_permissions (role_id, permission_id)
		SELECT rp.role_id, np.id FROM role_permissions rp
		JOIN permissions p ON p.id = rp.permission_id
		JOIN permissions np ON np.permission_key = 'assets.hook'
		WHERE p.permission_key = 'assets.act'
		AND NOT EXISTS (SELECT 1 FROM role_permissions held WHERE held.role_id = rp.role_id AND held.permission_id = np.id)`,
		`INSERT INTO role_permission_scopes (role_id, permission_id, scope_id)
		SELECT rps.role_id, np.id, rps.scope_id FROM role_permission_scopes rps
		JOIN permissions p ON p.id = rps.permission_id
		JOIN permissions np ON np.permission_key = 'assets.hook'
		WHERE p.permission_key = 'assets.act'
		AND NOT EXISTS (SELECT 1 FROM role_permission_scopes held WHERE held.role_id = rps.role_id AND held.permission_id = np.id AND held.scope_id = rps.scope_id)`,
		`DELETE FROM role_permission_scopes WHERE permission_id IN (SELECT id FROM permissions WHERE permission_key = 'assets.act')`,
		`DELETE FROM role_permissions WHERE permission_id IN (SELECT id FROM permissions WHERE permission_key = 'assets.act')`,
		`DELETE FROM permissions WHERE permission_key = 'assets.act'`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("merging assets.act into assets.hook: %w", err)
		}
	}
	return nil
}

// rewriteColumn reads id and a text column with query, and writes back
// with update the rows rewrite changes.
func rewriteColumn(db *sql.DB, dialect, query, update string, rewrite func(string) (string, bool, error)) error {
	rows, err := db.Query(query)
	if err != nil {
		return err
	}
	updated := map[string]string{}
	for rows.Next() {
		var id, value string
		if err := rows.Scan(&id, &value); err != nil {
			_ = rows.Close()
			return err
		}
		next, changed, err := rewrite(value)
		if err != nil {
			_ = rows.Close()
			return fmt.Errorf("%s: %w", id, err)
		}
		if changed {
			updated[id] = next
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for id, value := range updated {
		if _, err := db.Exec(rebind(update, dialect), value, id); err != nil {
			return fmt.Errorf("%s: %w", id, err)
		}
	}
	return nil
}

// HookEvent is an event of kind as hooks name it: an asset.action event
// becomes an asset.hook event, its payload's action its hook. Other events
// are as they were.
func HookEvent(kind, payload string) (string, string, error) {
	if kind != "asset.action" {
		return kind, payload, nil
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(payload), &doc); err != nil {
		return "", "", err
	}
	if raw, ok := doc["action"]; ok {
		doc["hook"] = raw
		delete(doc, "action")
	}
	next, err := json.Marshal(doc)
	return "asset.hook", string(next), err
}

// HookDefinition is a flow's definition with each change that pressed an
// action, {"kind": "action", "action": key}, triggering the same hook,
// {"kind": "hook", "hook": key}, and whether that changed any.
func HookDefinition(definition string) (string, bool, error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(definition), &doc); err != nil {
		return "", false, err
	}
	var steps []map[string]json.RawMessage
	if raw, ok := doc["steps"]; ok && string(raw) != "null" {
		if err := json.Unmarshal(raw, &steps); err != nil {
			return "", false, err
		}
	}
	changed := false
	var err error
	for _, step := range steps {
		raw, ok := step["changes"]
		if !ok || string(raw) == "null" {
			continue
		}
		var changes []map[string]json.RawMessage
		if err := json.Unmarshal(raw, &changes); err != nil {
			return "", false, err
		}
		hooked := false
		for _, c := range changes {
			var kind string
			if json.Unmarshal(c["kind"], &kind) != nil || kind != "action" {
				continue
			}
			c["kind"] = json.RawMessage(`"hook"`)
			if key, ok := c["action"]; ok {
				c["hook"] = key
				delete(c, "action")
			}
			hooked = true
		}
		if hooked {
			if step["changes"], err = json.Marshal(changes); err != nil {
				return "", false, err
			}
			changed = true
		}
	}
	if !changed {
		return definition, false, nil
	}
	if doc["steps"], err = json.Marshal(steps); err != nil {
		return "", false, err
	}
	next, err := json.Marshal(doc)
	return string(next), true, err
}

// HookScanSpec is a scan template's spec with the quick action that
// offered an asset's actions, "actions", offering its hooks, "hooks", and
// whether that changed any.
func HookScanSpec(spec string) (string, bool, error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(spec), &doc); err != nil {
		return "", false, err
	}
	var sections []map[string]json.RawMessage
	if raw, ok := doc["sections"]; ok && string(raw) != "null" {
		if err := json.Unmarshal(raw, &sections); err != nil {
			return "", false, err
		}
	}
	changed := false
	for _, s := range sections {
		var quick []string
		if raw, ok := s["actions"]; !ok || json.Unmarshal(raw, &quick) != nil {
			continue
		}
		hooked := false
		for i, a := range quick {
			if a == "actions" {
				quick[i], hooked = "hooks", true
			}
		}
		if hooked {
			raw, err := json.Marshal(quick)
			if err != nil {
				return "", false, err
			}
			s["actions"], changed = raw, true
		}
	}
	if !changed {
		return spec, false, nil
	}
	raw, err := json.Marshal(sections)
	if err != nil {
		return "", false, err
	}
	doc["sections"] = raw
	next, err := json.Marshal(doc)
	return string(next), true, err
}
