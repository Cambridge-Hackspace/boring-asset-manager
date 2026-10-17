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

// Actions on assets (README §5.4): buttons categories offer on their
// assets, such as Identify, which record asset.action events for devices
// and notification channels to act on.

var (
	ErrActionNotFound   = errors.New("action not found")
	ErrActionKeyExists  = errors.New("an action with that key already exists")
	ErrInvalidAction    = errors.New("invalid action")
	ErrActionNotOffered = errors.New("that action is not offered on this asset")
)

// actionKeys are lowercase letters, digits, hyphens and underscores,
// starting with a letter.
var actionKeys = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

// ActionInput is an action to save.
type ActionInput struct {
	Key, Label, Confirm string
}

func (in *ActionInput) check() error {
	in.Key, in.Label, in.Confirm = strings.TrimSpace(in.Key), strings.TrimSpace(in.Label), strings.TrimSpace(in.Confirm)
	switch {
	case !actionKeys.MatchString(in.Key):
		return fmt.Errorf("%w: a key is 1-64 lowercase letters, digits, - and _, starting with a letter", ErrInvalidAction)
	case in.Label == "" || len(in.Label) > 255:
		return fmt.Errorf("%w: a label of 1-255 characters is required", ErrInvalidAction)
	case len(in.Confirm) > 255:
		return fmt.Errorf("%w: the question is at most 255 characters", ErrInvalidAction)
	}
	return nil
}

const actionColumns = "id, action_key, label, confirm_question, created_at, updated_at"

func scanAction(row interface{ Scan(...any) error }) (models.AssetAction, error) {
	var a models.AssetAction
	err := row.Scan(&a.ID, &a.Key, &a.Label, &a.Confirm, &a.CreatedAt, &a.UpdatedAt)
	return a, err
}

// ListActions lists the actions by label.
func (s *Store) ListActions() ([]models.AssetAction, error) {
	rows, err := s.Query("SELECT " + actionColumns + " FROM asset_actions ORDER BY label, action_key")
	if err != nil {
		return nil, fmt.Errorf("listing actions: %w", err)
	}
	defer rows.Close()
	out := []models.AssetAction{}
	for rows.Next() {
		a, err := scanAction(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning an action: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// GetAction reads an action.
func (s *Store) GetAction(id string) (*models.AssetAction, error) {
	a, err := scanAction(s.QueryRow("SELECT "+actionColumns+" FROM asset_actions WHERE id = ?", id))
	if err == sql.ErrNoRows {
		return nil, ErrActionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("reading an action: %w", err)
	}
	return &a, nil
}

// SaveAction creates an action, or with an ID replaces one.
func (s *Store) SaveAction(id string, in ActionInput) (*models.AssetAction, error) {
	if err := in.check(); err != nil {
		return nil, err
	}
	var taken int
	if err := s.QueryRow("SELECT COUNT(*) FROM asset_actions WHERE action_key = ? AND id <> ?", in.Key, id).Scan(&taken); err != nil {
		return nil, fmt.Errorf("checking the action's key: %w", err)
	}
	if taken > 0 {
		return nil, ErrActionKeyExists
	}
	now := dbTime(time.Now())
	if id == "" {
		id = uuid.NewString()
		if _, err := s.Exec("INSERT INTO asset_actions ("+actionColumns+") VALUES (?, ?, ?, ?, ?, ?)", id, in.Key, in.Label, in.Confirm, now, now); err != nil {
			return nil, fmt.Errorf("saving the action: %w", err)
		}
		return s.GetAction(id)
	}
	res, err := s.Exec("UPDATE asset_actions SET action_key = ?, label = ?, confirm_question = ?, updated_at = ? WHERE id = ?", in.Key, in.Label, in.Confirm, now, id)
	if err != nil {
		return nil, fmt.Errorf("saving the action: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrActionNotFound
	}
	return s.GetAction(id)
}

// DeleteAction removes an action, and every category's offer of it.
func (s *Store) DeleteAction(id string) error {
	res, err := s.Exec("DELETE FROM asset_actions WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("deleting the action: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrActionNotFound
	}
	return nil
}

// CategoryActions lists the actions a category offers itself.
func (s *Store) CategoryActions(categoryID string) ([]models.CategoryAction, error) {
	if _, err := s.GetCategoryByID(categoryID); err != nil {
		return nil, err
	}
	rows, err := s.Query(`SELECT ca.action_id, ca.cascades FROM category_actions ca JOIN asset_actions a ON a.id = ca.action_id
		WHERE ca.category_id = ? ORDER BY a.label, a.action_key`, categoryID)
	if err != nil {
		return nil, fmt.Errorf("listing the category's actions: %w", err)
	}
	defer rows.Close()
	out := []models.CategoryAction{}
	for rows.Next() {
		var ca models.CategoryAction
		if err := rows.Scan(&ca.ActionID, &ca.Cascades); err != nil {
			return nil, err
		}
		out = append(out, ca)
	}
	return out, rows.Err()
}

// SetCategoryActions replaces the actions a category offers.
func (s *Store) SetCategoryActions(categoryID string, actions []models.CategoryAction) ([]models.CategoryAction, error) {
	if _, err := s.GetCategoryByID(categoryID); err != nil {
		return nil, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := s.TxExec(tx, "DELETE FROM category_actions WHERE category_id = ?", categoryID); err != nil {
		return nil, fmt.Errorf("replacing the category's actions: %w", err)
	}
	seen := map[string]bool{}
	for _, ca := range actions {
		if seen[ca.ActionID] {
			return nil, fmt.Errorf("%w: an action is listed more than once", ErrInvalidAction)
		}
		seen[ca.ActionID] = true
		var n int
		if err := s.TxQueryRow(tx, "SELECT COUNT(*) FROM asset_actions WHERE id = ?", ca.ActionID).Scan(&n); err != nil {
			return nil, fmt.Errorf("finding the action: %w", err)
		}
		if n == 0 {
			return nil, ErrActionNotFound
		}
		if _, err := s.TxExec(tx, "INSERT INTO category_actions (category_id, action_id, cascades) VALUES (?, ?, ?)", categoryID, ca.ActionID, ca.Cascades); err != nil {
			return nil, fmt.Errorf("replacing the category's actions: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing: %w", err)
	}
	return s.CategoryActions(categoryID)
}

// readActionTraits reads the actions and which categories offer them, for
// categoryTraits.
func readActionTraits(q interface {
	Query(string, ...any) (*sql.Rows, error)
}) (map[string]models.AssetAction, map[string][]models.CategoryAction, error) {
	defs := map[string]models.AssetAction{}
	rows, err := q.Query("SELECT " + actionColumns + " FROM asset_actions")
	if err != nil {
		return nil, nil, fmt.Errorf("reading actions: %w", err)
	}
	for rows.Next() {
		a, err := scanAction(rows)
		if err != nil {
			rows.Close()
			return nil, nil, fmt.Errorf("scanning an action: %w", err)
		}
		defs[a.ID] = a
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	offers := map[string][]models.CategoryAction{}
	rows, err = q.Query("SELECT category_id, action_id, cascades FROM category_actions")
	if err != nil {
		return nil, nil, fmt.Errorf("reading category actions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cat string
		var ca models.CategoryAction
		if err := rows.Scan(&cat, &ca.ActionID, &ca.Cascades); err != nil {
			return nil, nil, err
		}
		offers[cat] = append(offers[cat], ca)
	}
	return defs, offers, rows.Err()
}

// offered lists the actions offered on an asset in these categories: those
// its categories offer, and those cascading from their ancestors, by label.
func (t categoryTraits) offered(categoryIDs []string) []models.OfferedAction {
	seen := map[string]bool{}
	out := []models.OfferedAction{}
	for _, c := range categoryIDs {
		for id, d := c, 0; id != "" && d < 100; id, d = t.parents[id], d+1 {
			for _, ca := range t.offers[id] {
				def, ok := t.actions[ca.ActionID]
				if !ok || seen[ca.ActionID] || (d > 0 && !ca.Cascades) {
					continue
				}
				seen[ca.ActionID] = true
				out = append(out, models.OfferedAction{ID: def.ID, Key: def.Key, Label: def.Label, Confirm: def.Confirm})
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

// RecordAssetAction presses an action offered on an active asset,
// recording an asset.action event attributed to the acting user or token,
// with the asset and the containers it is in, each with its codes and
// field values. It returns the event's ID.
func (s *Store) RecordAssetAction(assetID, key string) (string, error) {
	a, err := s.GetAssetByID(assetID)
	if err != nil {
		return "", err
	}
	if a.Asset.ArchivedAt != nil {
		return "", ErrAssetArchived
	}
	var offered *models.OfferedAction
	for i := range a.Actions {
		if a.Actions[i].Key == key {
			offered = &a.Actions[i]
		}
	}
	if offered == nil {
		return "", ErrActionNotOffered
	}
	action := &models.EventAction{Key: offered.Key, Label: offered.Label, Asset: thingOf(a), Location: []models.EventThing{}}
	for _, place := range a.Location {
		container, err := s.GetAssetByID(place.ID)
		if errors.Is(err, ErrAssetNotFound) {
			continue
		}
		if err != nil {
			return "", err
		}
		action.Location = append(action.Location, thingOf(container))
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return "", fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	id, err := s.recordAssetEventID(tx, models.EventAssetAction, assetID, models.EventPayload{Trigger: models.TriggerManual, Action: action})
	if err != nil {
		return "", err
	}
	return id, tx.Commit()
}

// thingOf is an asset as an action event carries it.
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
