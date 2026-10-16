package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/google/uuid"
)

// inventoryLock names the lock row that serializes changes able to move a
// category's active count or running-low flag (README §5.1).
const inventoryLock = "inventory"

// MaxLowStockThreshold bounds low_stock_threshold.
const MaxLowStockThreshold = 1_000_000

var (
	ErrThresholdRequiresConsumable = errors.New("only consumable categories can have a low stock threshold")
	ErrThresholdOutOfRange         = fmt.Errorf("low stock threshold must be between 1 and %d", MaxLowStockThreshold)
)

// As returns a view of the store whose changes are attributed to the user
// given, for the events they cause. The view shares the connection pool.
func (s *Store) As(userID string) *Store {
	c := *s
	c.actor, c.actorToken = userID, ""
	return &c
}

// takeLock holds the named lock row until tx ends.
func (s *Store) takeLock(tx *sql.Tx, name string) error {
	res, err := s.TxExec(tx, "UPDATE app_locks SET counter = counter + 1 WHERE name = ?", name)
	if err != nil {
		return fmt.Errorf("taking the %s lock: %w", name, err)
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		return fmt.Errorf("taking the %s lock: lock row missing (%d rows, %v)", name, n, err)
	}
	return nil
}

// inventoryChange is a change running under the inventory lock.
type inventoryChange struct {
	tx *sql.Tx
	// manual and manualAssets hold the categories and assets whose flag the
	// change set by hand; the threshold rule leaves them alone for this
	// change.
	manual       map[string]bool
	manualAssets map[string]bool
}

// withInventory runs change in a transaction holding the inventory lock,
// then applies the threshold rule to every category whose active count, and
// every asset whose quantity, crossed its threshold, recording events in the
// same transaction.
func (s *Store) withInventory(change func(ic *inventoryChange) error) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	if err := s.takeLock(tx, inventoryLock); err != nil {
		return err
	}
	before, err := s.stockLevels(tx)
	if err != nil {
		return err
	}
	assetsBefore, err := s.assetLevels(tx)
	if err != nil {
		return err
	}
	ic := &inventoryChange{tx: tx, manual: map[string]bool{}, manualAssets: map[string]bool{}}
	if err := change(ic); err != nil {
		return err
	}
	after, err := s.stockLevels(tx)
	if err != nil {
		return err
	}
	if err := s.applyThresholds(ic, before, after); err != nil {
		return err
	}
	assetsAfter, err := s.assetLevels(tx)
	if err != nil {
		return err
	}
	if err := s.applyAssetThresholds(ic, assetsBefore, assetsAfter); err != nil {
		return err
	}
	if s.beforeInventoryCommit != nil {
		s.beforeInventoryCommit()
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing: %w", err)
	}
	return nil
}

// stock is a thresholded category's state: its count of active assets,
// or for one with a unit, the total of their stock in it.
type stock struct {
	threshold  int
	count      int
	runningLow bool
	unit       string
}

func (s stock) below() bool { return s.count < s.threshold }

// stockLevels reads every category that has a threshold, with its active
// count.
func (s *Store) stockLevels(tx *sql.Tx) (map[string]stock, error) {
	rows, err := s.TxQuery(tx, "SELECT id, parent_id, is_running_low, low_stock_threshold, stock_unit FROM categories")
	if err != nil {
		return nil, fmt.Errorf("reading categories: %w", err)
	}
	parents := map[string]string{}
	levels := map[string]stock{}
	for rows.Next() {
		var (
			id         string
			parent     sql.NullString
			runningLow bool
			threshold  sql.NullInt64
			unit       string
		)
		if err := rows.Scan(&id, &parent, &runningLow, &threshold, &unit); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scanning category: %w", err)
		}
		if parent.Valid {
			parents[id] = parent.String
		}
		if threshold.Valid {
			levels[id] = stock{threshold: int(threshold.Int64), runningLow: runningLow, unit: unit}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(levels) == 0 {
		return levels, nil
	}
	counts, err := s.countActive(tx, parents, levels)
	if err != nil {
		return nil, err
	}
	for id, l := range levels {
		l.count = counts[id]
		levels[id] = l
	}
	return levels, nil
}

// countActive measures each target category with its descendants, given
// every category's parent: the number of distinct active assets in it, or,
// for a target with a unit, the total of their stock in that unit. An asset
// in two subcategories counts once, and one whose stock is not counted in
// the unit adds nothing to a total.
func (s *Store) countActive(tx *sql.Tx, parents map[string]string, targets map[string]stock) (map[string]int, error) {
	rows, err := s.TxQuery(tx, `
		SELECT ac.category_id, ac.asset_id, a.stock_quantity, a.stock_unit
		FROM asset_categories ac
		JOIN assets a ON a.id = ac.asset_id
		WHERE a.archived_at IS NULL
	`)
	if err != nil {
		return nil, fmt.Errorf("reading category assignments: %w", err)
	}
	defer rows.Close()

	type held struct {
		quantity sql.NullInt64
		unit     string
	}
	seen := map[string]map[string]held{}
	for rows.Next() {
		var (
			cat, asset string
			h          held
		)
		if err := rows.Scan(&cat, &asset, &h.quantity, &h.unit); err != nil {
			return nil, fmt.Errorf("scanning category assignment: %w", err)
		}
		// Credit the asset to the category and each ancestor, once each.
		for depth := 0; cat != "" && depth < 100; depth++ {
			if _, ok := targets[cat]; ok {
				if seen[cat] == nil {
					seen[cat] = map[string]held{}
				}
				seen[cat][asset] = h
			}
			cat = parents[cat]
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	counts := make(map[string]int, len(seen))
	for cat, assets := range seen {
		unit := targets[cat].unit
		if unit == "" {
			counts[cat] = len(assets)
			continue
		}
		for _, h := range assets {
			if h.quantity.Valid && h.unit == unit {
				counts[cat] += int(h.quantity.Int64)
			}
		}
	}
	return counts, nil
}

// applyThresholds flags or clears each category whose active count crossed
// its threshold during the change, unless the change set its flag by hand.
func (s *Store) applyThresholds(ic *inventoryChange, before, after map[string]stock) error {
	for _, id := range crossings(before, after, ic.manual) {
		now := after[id]
		if err := s.setRunningLowTx(ic.tx, id, now.below()); err != nil {
			return err
		}
		count, threshold := now.count, now.threshold
		kind := models.EventRestocked
		if now.below() {
			kind = models.EventRunningLow
		}
		if err := s.recordEvent(ic.tx, kind, id, models.EventPayload{
			Trigger:     models.TriggerThreshold,
			ActiveCount: &count,
			Threshold:   &threshold,
			Unit:        now.unit,
		}); err != nil {
			return err
		}
	}
	return nil
}

// crossings lists, in order, what must have its flag changed after a change:
// each thing whose level crossed its threshold, or that gained a threshold
// it is already below, whose flag disagrees, and that the change did not
// flag by hand.
func crossings(before, after map[string]stock, manual map[string]bool) []string {
	ids := make([]string, 0, len(after))
	for id := range after {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var out []string
	for _, id := range ids {
		now := after[id]
		prev, existed := before[id]
		if manual[id] || (existed && prev.below() == now.below()) || (!existed && !now.below()) {
			continue
		}
		if now.below() == now.runningLow {
			continue
		}
		out = append(out, id)
	}
	return out
}

// setRunningLowTx stores a category's flag.
func (s *Store) setRunningLowTx(tx *sql.Tx, id string, low bool) error {
	_, err := s.TxExec(tx, "UPDATE categories SET is_running_low = ?, updated_at = ? WHERE id = ?", low, time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("setting running low: %w", err)
	}
	return nil
}

// recordEvent stores an event about a category, filling in the category and
// the acting user as they are now.
func (s *Store) recordEvent(tx *sql.Tx, kind, categoryID string, payload models.EventPayload) error {
	var cat models.EventCategory
	err := s.TxQueryRow(tx, "SELECT id, name, color_hex FROM categories WHERE id = ?", categoryID).Scan(&cat.ID, &cat.Name, &cat.ColorHex)
	if err != nil {
		return fmt.Errorf("reading category for event: %w", err)
	}
	payload.Category = &cat
	return s.insertEvent(tx, kind, &categoryID, nil, payload)
}

// recordAssetEvent stores an event about an asset, filling in the asset, its
// unit and the acting user as they are now.
func (s *Store) recordAssetEvent(tx *sql.Tx, kind, assetID string, payload models.EventPayload) error {
	a := models.EventAsset{ID: assetID}
	err := s.TxQueryRow(tx, "SELECT name, stock_unit FROM assets WHERE id = ?", assetID).Scan(&a.Name, &payload.Unit)
	if err != nil {
		return fmt.Errorf("reading asset for event: %w", err)
	}
	payload.Asset = &a
	return s.insertEvent(tx, kind, nil, &assetID, payload)
}

// insertEvent stores an event about a category or an asset, filling in the
// acting user, and queues its deliveries.
func (s *Store) insertEvent(tx *sql.Tx, kind string, categoryID, assetID *string, payload models.EventPayload) error {
	var actorID *string
	if s.actor != "" {
		actor := models.EventActor{ID: s.actor}
		err := s.TxQueryRow(tx, "SELECT email, name FROM users WHERE id = ?", s.actor).Scan(&actor.Email, &actor.Name)
		if err != nil && err != sql.ErrNoRows {
			return fmt.Errorf("reading user for event: %w", err)
		}
		payload.Actor = &actor
		actorID = &actor.ID
	} else if s.actorToken != "" {
		// A token is not a user, so actor_user_id stays empty.
		actor := models.EventActor{ID: s.actorToken, Token: true}
		err := s.TxQueryRow(tx, "SELECT name FROM api_tokens WHERE id = ?", s.actorToken).Scan(&actor.Name)
		if err != nil && err != sql.ErrNoRows {
			return fmt.Errorf("reading token for event: %w", err)
		}
		payload.Actor = &actor
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encoding event: %w", err)
	}
	id, now := uuid.NewString(), dbTime(time.Now())
	_, err = s.TxExec(tx, "INSERT INTO events (id, kind, category_id, asset_id, actor_user_id, payload, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		id, kind, categoryID, assetID, actorID, string(raw), now)
	if err != nil {
		return fmt.Errorf("recording event: %w", err)
	}
	if models.HistoryOnly(kind) {
		// History only: no channel can subscribe to it.
		return nil
	}
	return s.fanOut(tx, id, kind, now)
}

// setFlagByHand records a manual change of a category's flag within ic,
// given the flag before and after. It does nothing if the flag is unchanged.
func (s *Store) setFlagByHand(ic *inventoryChange, id string, was, now bool) error {
	if was == now {
		return nil
	}
	ic.manual[id] = true
	kind := models.EventRestocked
	if now {
		kind = models.EventRunningLow
	}
	return s.recordEvent(ic.tx, kind, id, models.EventPayload{Trigger: models.TriggerManual})
}

// SetRunningLow flags or clears a consumable category by hand. It reports
// whether the flag changed; setting it to what it already is records nothing.
func (s *Store) SetRunningLow(id string, low bool) (*models.Category, bool, error) {
	changed := false
	err := s.withInventory(func(ic *inventoryChange) error {
		var consumable, was bool
		err := s.TxQueryRow(ic.tx, "SELECT is_consumable, is_running_low FROM categories WHERE id = ?", id).Scan(&consumable, &was)
		if err == sql.ErrNoRows {
			return ErrCategoryNotFound
		}
		if err != nil {
			return fmt.Errorf("reading category: %w", err)
		}
		if low && !consumable {
			return ErrRunningLowRequiresConsumable
		}
		if was == low {
			ic.manual[id] = true
			return nil
		}
		if err := s.setRunningLowTx(ic.tx, id, low); err != nil {
			return err
		}
		changed = true
		return s.setFlagByHand(ic, id, was, low)
	})
	if err != nil {
		return nil, false, err
	}
	cat, err := s.GetCategoryByID(id)
	return cat, changed, err
}

// ActiveCount counts the distinct active assets in a category or its
// descendants.
func (s *Store) ActiveCount(id string) (int, error) {
	return s.measureCategory(id, "")
}

// CategoryTotal totals the stock in unit of the active assets in a category
// and its descendants, as a category with that unit is measured.
func (s *Store) CategoryTotal(id, unit string) (int, error) {
	return s.measureCategory(id, unit)
}

func (s *Store) measureCategory(id, unit string) (int, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := s.TxQuery(tx, "SELECT id, parent_id FROM categories")
	if err != nil {
		return 0, fmt.Errorf("reading categories: %w", err)
	}
	parents := map[string]string{}
	for rows.Next() {
		var cid string
		var parent sql.NullString
		if err := rows.Scan(&cid, &parent); err != nil {
			rows.Close()
			return 0, err
		}
		if parent.Valid {
			parents[cid] = parent.String
		}
	}
	rows.Close()
	counts, err := s.countActive(tx, parents, map[string]stock{id: {unit: unit}})
	if err != nil {
		return 0, err
	}
	return counts[id], nil
}

// ListEvents returns the most recent events, newest first.
func (s *Store) ListEvents(limit int) ([]models.Event, error) {
	rows, err := s.Query(fmt.Sprintf(`SELECT `+eventColumns+`
		FROM events ORDER BY created_at DESC, id DESC LIMIT %d`, limit))
	if err != nil {
		return nil, fmt.Errorf("listing events: %w", err)
	}
	defer rows.Close()
	events := []models.Event{}
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// eventColumns lists an event's columns in scanEvent's order.
const eventColumns = "id, kind, category_id, asset_id, actor_user_id, payload, created_at"

func scanEvent(row interface{ Scan(...any) error }) (models.Event, error) {
	var (
		e             models.Event
		cat, ast, act sql.NullString
		payload       string
	)
	if err := row.Scan(&e.ID, &e.Kind, &cat, &ast, &act, &payload, &e.CreatedAt); err != nil {
		return e, err
	}
	if cat.Valid {
		e.CategoryID = &cat.String
	}
	if ast.Valid {
		e.AssetID = &ast.String
	}
	if act.Valid {
		e.ActorUserID = &act.String
	}
	if err := json.Unmarshal([]byte(payload), &e.Payload); err != nil {
		return e, fmt.Errorf("decoding event %s: %w", e.ID, err)
	}
	return e, nil
}
