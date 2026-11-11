package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/google/uuid"
)

// Hooks on assets (docs/hooks.md): buttons categories offer on their
// assets, such as Identify, which record asset.hook events for devices
// and notification channels to act on.

var (
	ErrHookNotFound   = errors.New("hook not found")
	ErrHookKeyExists  = errors.New("a hook with that key already exists")
	ErrInvalidHook    = errors.New("invalid hook")
	ErrHookNotOffered = errors.New("that hook is not offered on this asset")
)

// hookKeys are lowercase letters, digits, hyphens and underscores,
// starting with a letter.
var hookKeys = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

// HookInput is a hook to save. Triggers nil or empty is a button alone.
// The API checks each trigger's details (docs/hooks.md); here only that
// there are not too many.
type HookInput struct {
	Key, Label, Confirm string
	Triggers            []models.HookTrigger
	// Actions are what BAM! does itself when the hook fires, from
	// models.HookActions.
	Actions []string
}

// MaxHookTriggers is the most triggers a hook has.
const MaxHookTriggers = 10

func (in *HookInput) check() error {
	in.Key, in.Label, in.Confirm = strings.TrimSpace(in.Key), strings.TrimSpace(in.Label), strings.TrimSpace(in.Confirm)
	switch {
	case !hookKeys.MatchString(in.Key):
		return fmt.Errorf("%w: a key is 1-64 lowercase letters, digits, - and _, starting with a letter", ErrInvalidHook)
	case in.Label == "" || len(in.Label) > 255:
		return fmt.Errorf("%w: a label of 1-255 characters is required", ErrInvalidHook)
	case len(in.Confirm) > 255:
		return fmt.Errorf("%w: the question is at most 255 characters", ErrInvalidHook)
	case len(in.Triggers) > MaxHookTriggers:
		return fmt.Errorf("%w: at most %d triggers", ErrInvalidHook, MaxHookTriggers)
	}
	seen := map[string]bool{}
	for _, a := range in.Actions {
		if !slices.Contains(models.HookActions, a) {
			return fmt.Errorf("%w: %q is not an action; a hook's actions are %s", ErrInvalidHook, a, strings.Join(models.HookActions, ", "))
		}
		if seen[a] {
			return fmt.Errorf("%w: %s is listed twice", ErrInvalidHook, a)
		}
		seen[a] = true
	}
	return nil
}

// storedActions is how actions are stored: NULL for none.
func storedActions(actions []string) any {
	if len(actions) == 0 {
		return nil
	}
	raw, _ := json.Marshal(actions)
	return string(raw)
}

// buttonOnly is the triggers of a hook stored without any.
func buttonOnly() []models.HookTrigger {
	return []models.HookTrigger{{Type: models.HookTriggerButton}}
}

// storedTriggers is how triggers are stored: NULL for a button alone.
func storedTriggers(triggers []models.HookTrigger) (any, error) {
	if len(triggers) == 0 || (len(triggers) == 1 && triggers[0].Type == models.HookTriggerButton) {
		return nil, nil
	}
	raw, err := json.Marshal(triggers)
	if err != nil {
		return nil, fmt.Errorf("encoding triggers: %w", err)
	}
	return string(raw), nil
}

const hookColumns = "id, action_key, label, confirm_question, triggers, created_at, updated_at, actions"

func scanHook(row interface{ Scan(...any) error }) (models.AssetHook, error) {
	var a models.AssetHook
	var triggers, actions sql.NullString
	if err := row.Scan(&a.ID, &a.Key, &a.Label, &a.Confirm, &triggers, &a.CreatedAt, &a.UpdatedAt, &actions); err != nil {
		return a, err
	}
	a.Actions = []string{}
	if actions.Valid && actions.String != "" {
		if err := json.Unmarshal([]byte(actions.String), &a.Actions); err != nil {
			return a, fmt.Errorf("reading hook %s's actions: %w", a.Key, err)
		}
	}
	a.Triggers = buttonOnly()
	if triggers.Valid && triggers.String != "" {
		if err := json.Unmarshal([]byte(triggers.String), &a.Triggers); err != nil {
			return a, fmt.Errorf("reading hook %s's triggers: %w", a.Key, err)
		}
	}
	return a, nil
}

// ListHooks lists the hooks by label.
func (s *Store) ListHooks() ([]models.AssetHook, error) {
	rows, err := s.Query("SELECT " + hookColumns + " FROM asset_actions ORDER BY label, action_key")
	if err != nil {
		return nil, fmt.Errorf("listing hooks: %w", err)
	}
	defer rows.Close()
	out := []models.AssetHook{}
	for rows.Next() {
		a, err := scanHook(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning a hook: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// GetHook reads a hook.
func (s *Store) GetHook(id string) (*models.AssetHook, error) {
	a, err := scanHook(s.QueryRow("SELECT "+hookColumns+" FROM asset_actions WHERE id = ?", id))
	if err == sql.ErrNoRows {
		return nil, ErrHookNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("reading a hook: %w", err)
	}
	return &a, nil
}

// SaveHook creates a hook, or with an ID replaces one.
func (s *Store) SaveHook(id string, in HookInput) (*models.AssetHook, error) {
	if err := in.check(); err != nil {
		return nil, err
	}
	var taken int
	if err := s.QueryRow("SELECT COUNT(*) FROM asset_actions WHERE action_key = ? AND id <> ?", in.Key, id).Scan(&taken); err != nil {
		return nil, fmt.Errorf("checking the hook's key: %w", err)
	}
	if taken > 0 {
		return nil, ErrHookKeyExists
	}
	triggers, err := storedTriggers(in.Triggers)
	if err != nil {
		return nil, err
	}
	now := dbTime(time.Now())
	if id == "" {
		id = uuid.NewString()
		if _, err := s.Exec("INSERT INTO asset_actions ("+hookColumns+") VALUES (?, ?, ?, ?, ?, ?, ?, ?)", id, in.Key, in.Label, in.Confirm, triggers, now, now, storedActions(in.Actions)); err != nil {
			return nil, fmt.Errorf("saving the hook: %w", err)
		}
		return s.GetHook(id)
	}
	res, err := s.Exec("UPDATE asset_actions SET action_key = ?, label = ?, confirm_question = ?, triggers = ?, actions = ?, updated_at = ? WHERE id = ?", in.Key, in.Label, in.Confirm, triggers, storedActions(in.Actions), now, id)
	if err != nil {
		return nil, fmt.Errorf("saving the hook: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrHookNotFound
	}
	return s.GetHook(id)
}

// DeleteHook removes a hook, and every category's offer of it.
func (s *Store) DeleteHook(id string) error {
	res, err := s.Exec("DELETE FROM asset_actions WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("deleting the hook: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrHookNotFound
	}
	return nil
}

// CategoryHooks lists the hooks a category offers itself.
func (s *Store) CategoryHooks(categoryID string) ([]models.CategoryHook, error) {
	if _, err := s.GetCategoryByID(categoryID); err != nil {
		return nil, err
	}
	rows, err := s.Query(`SELECT ca.action_id, ca.cascades FROM category_actions ca JOIN asset_actions a ON a.id = ca.action_id
		WHERE ca.category_id = ? ORDER BY a.label, a.action_key`, categoryID)
	if err != nil {
		return nil, fmt.Errorf("listing the category's hooks: %w", err)
	}
	defer rows.Close()
	out := []models.CategoryHook{}
	for rows.Next() {
		var ca models.CategoryHook
		if err := rows.Scan(&ca.HookID, &ca.Cascades); err != nil {
			return nil, err
		}
		out = append(out, ca)
	}
	return out, rows.Err()
}

// SetCategoryHooks replaces the hooks a category offers.
func (s *Store) SetCategoryHooks(categoryID string, hooks []models.CategoryHook) ([]models.CategoryHook, error) {
	if _, err := s.GetCategoryByID(categoryID); err != nil {
		return nil, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := s.TxExec(tx, "DELETE FROM category_actions WHERE category_id = ?", categoryID); err != nil {
		return nil, fmt.Errorf("replacing the category's hooks: %w", err)
	}
	seen := map[string]bool{}
	for _, ca := range hooks {
		if seen[ca.HookID] {
			return nil, fmt.Errorf("%w: a hook is listed more than once", ErrInvalidHook)
		}
		seen[ca.HookID] = true
		var n int
		if err := s.TxQueryRow(tx, "SELECT COUNT(*) FROM asset_actions WHERE id = ?", ca.HookID).Scan(&n); err != nil {
			return nil, fmt.Errorf("finding the hook: %w", err)
		}
		if n == 0 {
			return nil, ErrHookNotFound
		}
		if _, err := s.TxExec(tx, "INSERT INTO category_actions (category_id, action_id, cascades) VALUES (?, ?, ?)", categoryID, ca.HookID, ca.Cascades); err != nil {
			return nil, fmt.Errorf("replacing the category's hooks: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing: %w", err)
	}
	return s.CategoryHooks(categoryID)
}

// readHookTraits reads the hooks and which categories offer them, for
// categoryTraits.
func readHookTraits(q interface {
	Query(string, ...any) (*sql.Rows, error)
}) (map[string]models.AssetHook, map[string][]models.CategoryHook, error) {
	defs := map[string]models.AssetHook{}
	rows, err := q.Query("SELECT " + hookColumns + " FROM asset_actions")
	if err != nil {
		return nil, nil, fmt.Errorf("reading hooks: %w", err)
	}
	for rows.Next() {
		a, err := scanHook(rows)
		if err != nil {
			rows.Close()
			return nil, nil, fmt.Errorf("scanning a hook: %w", err)
		}
		defs[a.ID] = a
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	offers := map[string][]models.CategoryHook{}
	rows, err = q.Query("SELECT category_id, action_id, cascades FROM category_actions")
	if err != nil {
		return nil, nil, fmt.Errorf("reading category hooks: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cat string
		var ca models.CategoryHook
		if err := rows.Scan(&cat, &ca.HookID, &ca.Cascades); err != nil {
			return nil, nil, err
		}
		offers[cat] = append(offers[cat], ca)
	}
	return defs, offers, rows.Err()
}

// offered lists the hooks offered as buttons on an asset in these
// categories: those its categories offer, and those cascading from their
// ancestors, by label. Hooks that only events or a schedule fire are left
// out, as nobody presses them.
func (t categoryTraits) offered(categoryIDs []string) []models.OfferedHook {
	out := []models.OfferedHook{}
	for _, def := range t.offeredHooks(categoryIDs) {
		if def.Button() {
			out = append(out, models.OfferedHook{ID: def.ID, Key: def.Key, Label: def.Label, Confirm: def.Confirm})
		}
	}
	return out
}

// offeredHooks lists every hook offered on an asset in these categories,
// whatever fires it, by label.
func (t categoryTraits) offeredHooks(categoryIDs []string) []models.AssetHook {
	seen := map[string]bool{}
	out := []models.AssetHook{}
	for _, c := range categoryIDs {
		for id, d := c, 0; id != "" && d < 100; id, d = t.parents[id], d+1 {
			for _, ca := range t.offers[id] {
				def, ok := t.hooks[ca.HookID]
				if !ok || seen[ca.HookID] || (d > 0 && !ca.Cascades) {
					continue
				}
				seen[ca.HookID] = true
				out = append(out, def)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Label != out[j].Label {
			return out[i].Label < out[j].Label
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// RecordAssetHook triggers a hook offered on an active asset,
// recording an asset.hook event attributed to the acting user or token,
// with the asset and the containers it is in, each with its codes and
// field values. It returns the event's ID.
func (s *Store) RecordAssetHook(assetID, key string) (string, error) {
	a, err := s.GetAssetByID(assetID)
	if err != nil {
		return "", err
	}
	if a.Asset.ArchivedAt != nil {
		return "", ErrAssetArchived
	}
	var offered *models.OfferedHook
	for i := range a.Hooks {
		if a.Hooks[i].Key == key {
			offered = &a.Hooks[i]
		}
	}
	if offered == nil {
		return "", ErrHookNotOffered
	}
	hook, err := s.GetHook(offered.ID)
	if err != nil {
		return "", err
	}
	return s.recordHook(a, *hook, models.TriggerManual, models.HookTriggerButton, nil)
}

// recordHook records an asset.hook event for hook h, fired on asset a by
// trigger, with the asset and the containers it is in, and takes the
// hook's actions with it.
func (s *Store) recordHook(a *models.AssetWithDetails, h models.AssetHook, payloadTrigger, trigger string, cause *models.EventCause) (string, error) {
	hook := &models.EventHook{Key: h.Key, Label: h.Label, Asset: thingOf(a), Location: []models.EventThing{}, Trigger: trigger, Cause: cause}
	for _, place := range a.Location {
		container, err := s.GetAssetByID(place.ID)
		if errors.Is(err, ErrAssetNotFound) {
			continue
		}
		if err != nil {
			return "", err
		}
		hook.Location = append(hook.Location, thingOf(container))
	}
	// The displays to refresh, and when, read before the transaction:
	// SQLite has one connection.
	var refreshing []string
	var due time.Time
	if slices.Contains(h.Actions, models.HookActionRefreshDisplays) {
		var err error
		if refreshing, err = s.displaysShowing(a.Asset.ID); err != nil {
			return "", err
		}
		if due, err = s.displayRefreshDue(time.Now()); err != nil {
			return "", err
		}
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return "", fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	id, err := s.recordAssetEventID(tx, models.EventAssetHook, a.Asset.ID, models.EventPayload{Trigger: payloadTrigger, Hook: hook})
	if err != nil {
		return "", err
	}
	if err := s.refreshDisplaysTx(tx, refreshing, due); err != nil {
		return "", err
	}
	return id, tx.Commit()
}

// HooksOffered reads which categories offer hooks once, and returns what
// is offered on an asset in given categories, whatever fires it, as it is
// now.
func (s *Store) HooksOffered() (func(categoryIDs []string) []models.AssetHook, error) {
	traits, err := s.categoryTraits(storeQuerier{s})
	if err != nil {
		return nil, err
	}
	return traits.offeredHooks, nil
}

// RecordTriggeredHook records hook firing on an active asset by an event
// or a schedule (trigger), and returns the event's ID. It is recorded by
// nobody: the hook fired by itself. An archived asset is skipped, with
// ErrAssetArchived.
func (s *Store) RecordTriggeredHook(assetID string, hook models.AssetHook, trigger string, cause *models.EventCause) (string, error) {
	a, err := s.GetAssetByID(assetID)
	if err != nil {
		return "", err
	}
	if a.Asset.ArchivedAt != nil {
		return "", ErrAssetArchived
	}
	payloadTrigger := models.TriggerSchedule
	if trigger == models.HookTriggerEvent {
		payloadTrigger = models.TriggerEvent
	}
	return s.As("").recordHook(a, hook, payloadTrigger, trigger, cause)
}

// ClaimHookEvent claims an event for firing hooks on, so that of several
// servers sharing the database only one acts on it: false when one already
// has.
func (s *Store) ClaimHookEvent(eventID string, now time.Time) (bool, error) {
	_, err := s.Exec("INSERT INTO hook_trigger_seen (event_id, seen_at) VALUES (?, ?)", eventID, dbTime(now))
	if IsUniqueViolation(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claiming an event for hooks: %w", err)
	}
	return true, nil
}

// PruneHookEvents forgets events claimed before before, which are too old
// to be looked at again.
func (s *Store) PruneHookEvents(before time.Time) error {
	if _, err := s.Exec("DELETE FROM hook_trigger_seen WHERE seen_at < ?", dbTime(before)); err != nil {
		return fmt.Errorf("pruning hook events: %w", err)
	}
	return nil
}

// ClaimHookFiring claims firing a hook on an asset at now, unless it fired
// on it less than interval ago: false then, or when another server claimed
// it first.
func (s *Store) ClaimHookFiring(hookID, assetID string, interval time.Duration, now time.Time) (bool, error) {
	now = dbTime(now)
	res, err := s.Exec("UPDATE hook_trigger_fired SET fired_at = ? WHERE hook_id = ? AND asset_id = ? AND fired_at <= ?", now, hookID, assetID, now.Add(-interval))
	if err != nil {
		return false, fmt.Errorf("claiming a hook's firing: %w", err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return true, nil
	}
	_, err = s.Exec("INSERT INTO hook_trigger_fired (hook_id, asset_id, fired_at) VALUES (?, ?, ?)", hookID, assetID, now)
	if IsUniqueViolation(err) {
		// It fired within the interval, or another server just claimed it.
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claiming a hook's firing: %w", err)
	}
	return true, nil
}

// HookScheduleRan is when a hook's index-th trigger, a schedule, last ran:
// false when it never has.
func (s *Store) HookScheduleRan(hookID string, index int) (time.Time, bool, error) {
	var at time.Time
	err := s.QueryRow("SELECT ran_at FROM hook_schedule_runs WHERE hook_id = ? AND trigger_index = ?", hookID, index).Scan(&at)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("reading a schedule's last run: %w", err)
	}
	return at, true, nil
}

// ClaimHookSchedule claims a schedule's run at at, from the last run was
// (zero when it never ran), so that of several servers only one runs it:
// false when another already moved it on.
func (s *Store) ClaimHookSchedule(hookID string, index int, was, at time.Time) (bool, error) {
	if was.IsZero() {
		_, err := s.Exec("INSERT INTO hook_schedule_runs (hook_id, trigger_index, ran_at) VALUES (?, ?, ?)", hookID, index, dbTime(at))
		if IsUniqueViolation(err) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("claiming a schedule's run: %w", err)
		}
		return true, nil
	}
	res, err := s.Exec("UPDATE hook_schedule_runs SET ran_at = ? WHERE hook_id = ? AND trigger_index = ? AND ran_at = ?", dbTime(at), hookID, index, dbTime(was))
	if err != nil {
		return false, fmt.Errorf("claiming a schedule's run: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// thingOf is an asset as a hook event carries it.
func thingOf(a *models.AssetWithDetails) models.EventThing {
	t := models.EventThing{ID: a.Asset.ID, Name: a.Asset.Name, Codes: map[string]string{}, Fields: map[string]string{}}
	for _, k := range a.GeneratorKeys {
		t.Codes[k.GeneratorType] = k.AssignedCode
	}
	for k, v := range a.FieldValues {
		t.Fields[k] = v
	}
	return t
}
