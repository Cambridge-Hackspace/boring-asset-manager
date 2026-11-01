package store

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
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

// HookInput is a hook to save.
type HookInput struct {
	Key, Label, Confirm string
}

func (in *HookInput) check() error {
	in.Key, in.Label, in.Confirm = strings.TrimSpace(in.Key), strings.TrimSpace(in.Label), strings.TrimSpace(in.Confirm)
	switch {
	case !hookKeys.MatchString(in.Key):
		return fmt.Errorf("%w: a key is 1-64 lowercase letters, digits, - and _, starting with a letter", ErrInvalidHook)
	case in.Label == "" || len(in.Label) > 255:
		return fmt.Errorf("%w: a label of 1-255 characters is required", ErrInvalidHook)
	case len(in.Confirm) > 255:
		return fmt.Errorf("%w: the question is at most 255 characters", ErrInvalidHook)
	}
	return nil
}

const hookColumns = "id, action_key, label, confirm_question, created_at, updated_at"

func scanHook(row interface{ Scan(...any) error }) (models.AssetHook, error) {
	var a models.AssetHook
	err := row.Scan(&a.ID, &a.Key, &a.Label, &a.Confirm, &a.CreatedAt, &a.UpdatedAt)
	return a, err
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
	now := dbTime(time.Now())
	if id == "" {
		id = uuid.NewString()
		if _, err := s.Exec("INSERT INTO asset_actions ("+hookColumns+") VALUES (?, ?, ?, ?, ?, ?)", id, in.Key, in.Label, in.Confirm, now, now); err != nil {
			return nil, fmt.Errorf("saving the hook: %w", err)
		}
		return s.GetHook(id)
	}
	res, err := s.Exec("UPDATE asset_actions SET action_key = ?, label = ?, confirm_question = ?, updated_at = ? WHERE id = ?", in.Key, in.Label, in.Confirm, now, id)
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

// offered lists the hooks offered on an asset in these categories: those
// its categories offer, and those cascading from their ancestors, by label.
func (t categoryTraits) offered(categoryIDs []string) []models.OfferedHook {
	seen := map[string]bool{}
	out := []models.OfferedHook{}
	for _, c := range categoryIDs {
		for id, d := c, 0; id != "" && d < 100; id, d = t.parents[id], d+1 {
			for _, ca := range t.offers[id] {
				def, ok := t.hooks[ca.HookID]
				if !ok || seen[ca.HookID] || (d > 0 && !ca.Cascades) {
					continue
				}
				seen[ca.HookID] = true
				out = append(out, models.OfferedHook{ID: def.ID, Key: def.Key, Label: def.Label, Confirm: def.Confirm})
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
	hook := &models.EventHook{Key: offered.Key, Label: offered.Label, Asset: thingOf(a), Location: []models.EventThing{}}
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
	tx, err := s.DB.Begin()
	if err != nil {
		return "", fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	id, err := s.recordAssetEventID(tx, models.EventAssetHook, assetID, models.EventPayload{Trigger: models.TriggerManual, Hook: hook})
	if err != nil {
		return "", err
	}
	return id, tx.Commit()
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
