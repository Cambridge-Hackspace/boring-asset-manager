package store

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/generators"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// ErrEventNotFound is returned when a history page starts after an event the
// asset does not have.
var ErrEventNotFound = errors.New("event not found")

// assetSnapshot is what an asset's history compares: the parts of an asset a
// person changes, as they read at one moment.
type assetSnapshot struct {
	name     string
	archived bool
	// categories maps category IDs to names, codes generator types to codes,
	// and fields field IDs to their values.
	categories map[string]string
	codes      map[string]string
	fields     map[string]fieldSnapshot
	stock      *models.AssetStock
	// icon is its own icon's name, "" for its categories'.
	icon string
}

type fieldSnapshot struct {
	key, label, value string
}

// snapshotAssetTx reads an asset within tx for its history.
func (s *Store) snapshotAssetTx(tx *sql.Tx, id string) (*assetSnapshot, error) {
	var (
		snap     = &assetSnapshot{categories: map[string]string{}, codes: map[string]string{}, fields: map[string]fieldSnapshot{}}
		sc       stockScan
		archived sql.NullTime
		icon     sql.NullString
	)
	err := s.TxQueryRow(tx, "SELECT name, archived_at, icon_name, "+stockColumns+" FROM assets WHERE id = ?", id).
		Scan(append([]any{&snap.name, &archived, &icon}, sc.dest()...)...)
	if err == sql.ErrNoRows {
		return nil, ErrAssetNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("reading asset for its history: %w", err)
	}
	snap.archived, snap.stock, snap.icon = archived.Valid, sc.stock(), icon.String

	err = s.scanPairs(tx, `SELECT c.id, c.name FROM asset_categories ac JOIN categories c ON ac.category_id = c.id WHERE ac.asset_id = ?`,
		id, func(k, v string) { snap.categories[k] = v })
	if err != nil {
		return nil, err
	}
	err = s.scanPairs(tx, "SELECT generator_type, assigned_code FROM asset_generator_keys WHERE asset_id = ?",
		id, func(k, v string) { snap.codes[k] = v })
	if err != nil {
		return nil, err
	}

	rows, err := s.TxQuery(tx, `SELECT f.id, f.field_key, f.label, afv.value FROM asset_field_values afv
		JOIN fields f ON afv.field_id = f.id WHERE afv.asset_id = ?`, id)
	if err != nil {
		return nil, fmt.Errorf("reading field values for history: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var fieldID string
		var f fieldSnapshot
		if err := rows.Scan(&fieldID, &f.key, &f.label, &f.value); err != nil {
			return nil, err
		}
		snap.fields[fieldID] = f
	}
	return snap, rows.Err()
}

// scanPairs runs a query for one asset that returns two text columns.
func (s *Store) scanPairs(tx *sql.Tx, query, id string, add func(k, v string)) error {
	rows, err := s.TxQuery(tx, query, id)
	if err != nil {
		return fmt.Errorf("reading asset for its history: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return err
		}
		add(k, v)
	}
	return rows.Err()
}

// diffAssets lists what changed from before to after; a nil before is an
// asset that did not exist. Changes come in a fixed order: the name, its
// icon, then categories, fields and codes, each sorted as a person would
// read them, then the stock.
func diffAssets(before, after *assetSnapshot) []models.EventChange {
	if before == nil {
		before = &assetSnapshot{}
	}
	var changes []models.EventChange
	if before.name != after.name {
		changes = append(changes, models.EventChange{What: "name", From: before.name, To: after.name})
	}
	if before.icon != after.icon {
		changes = append(changes, models.EventChange{What: "icon", From: before.icon, To: after.icon})
	}

	var cats []models.EventChange
	for id, name := range before.categories {
		if _, ok := after.categories[id]; !ok {
			cats = append(cats, models.EventChange{What: "category", Key: id, From: name})
		}
	}
	for id, name := range after.categories {
		if _, ok := before.categories[id]; !ok {
			cats = append(cats, models.EventChange{What: "category", Key: id, To: name})
		}
	}
	sort.Slice(cats, func(i, j int) bool { return cats[i].From+cats[i].To < cats[j].From+cats[j].To })
	changes = append(changes, cats...)

	var fields []models.EventChange
	for id, f := range after.fields {
		if b := before.fields[id]; b.value != f.value {
			fields = append(fields, models.EventChange{What: "field", Key: f.key, Label: f.label, From: b.value, To: f.value})
		}
	}
	for id, b := range before.fields {
		if _, ok := after.fields[id]; !ok {
			fields = append(fields, models.EventChange{What: "field", Key: b.key, Label: b.label, From: b.value})
		}
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Label < fields[j].Label })
	changes = append(changes, fields...)

	changes = append(changes, diffCodes(before.codes, after.codes)...)

	was, now := before.stock, after.stock
	switch {
	case was == nil && now != nil:
		changes = append(changes, models.EventChange{What: "stock", To: "counted"})
		was = &models.AssetStock{}
	case was != nil && now == nil:
		changes = append(changes, models.EventChange{What: "stock", From: "counted"})
	}
	if was != nil && now != nil {
		if was.Unit != now.Unit {
			changes = append(changes, models.EventChange{What: "unit", From: was.Unit, To: now.Unit})
		}
		if from, to := threshold(was), threshold(now); from != to {
			changes = append(changes, models.EventChange{What: "threshold", From: from, To: to})
		}
	}
	return changes
}

// diffCodes lists the codes that changed, by generator type.
func diffCodes(before, after map[string]string) []models.EventChange {
	names := map[string]string{}
	for _, g := range generators.Describe() {
		names[g.Type] = g.Name
	}
	types := map[string]bool{}
	for t := range before {
		types[t] = true
	}
	for t := range after {
		types[t] = true
	}
	var changes []models.EventChange
	for t := range types {
		if before[t] != after[t] {
			changes = append(changes, models.EventChange{What: "code", Key: t, Label: names[t], From: before[t], To: after[t]})
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Key < changes[j].Key })
	return changes
}

func threshold(st *models.AssetStock) string {
	if st.LowStockThreshold == nil {
		return ""
	}
	return strconv.Itoa(*st.LowStockThreshold)
}

// recordChangesTx records how an asset changed from before, in its history:
// as created when before is nil, and as edited otherwise, unless nothing did.
func (s *Store) recordChangesTx(tx *sql.Tx, id string, before *assetSnapshot, trigger string) error {
	after, err := s.snapshotAssetTx(tx, id)
	if err != nil {
		return err
	}
	changes := diffAssets(before, after)
	kind := models.EventAssetCreated
	if before != nil {
		if len(changes) == 0 {
			return nil
		}
		kind = models.EventAssetEdited
	}
	return s.recordAssetEvent(tx, kind, id, models.EventPayload{Trigger: trigger, Changes: changes})
}

// ListAssetEvents returns an asset's events, newest first: up to limit of
// them, starting after the event before when it is not empty. more reports
// whether older events remain.
func (s *Store) ListAssetEvents(id, before string, limit int) (events []models.Event, more bool, err error) {
	query, args := `SELECT `+eventColumns+` FROM events WHERE asset_id = ?`, []any{id}
	if before != "" {
		var at sql.NullTime
		err := s.QueryRow("SELECT created_at FROM events WHERE id = ? AND asset_id = ?", before, id).Scan(&at)
		if err == sql.ErrNoRows {
			return nil, false, ErrEventNotFound
		}
		if err != nil {
			return nil, false, fmt.Errorf("finding the page's first event: %w", err)
		}
		query += " AND (created_at < ? OR (created_at = ? AND id < ?))"
		args = append(args, at.Time, at.Time, before)
	}
	rows, err := s.Query(fmt.Sprintf(query+" ORDER BY created_at DESC, id DESC LIMIT %d", limit+1), args...)
	if err != nil {
		return nil, false, fmt.Errorf("listing asset events: %w", err)
	}
	defer rows.Close()
	events = []models.Event{}
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, false, err
		}
		events = append(events, e)
	}
	if len(events) > limit {
		return events[:limit], true, rows.Err()
	}
	return events, false, rows.Err()
}

// recordAfter records how an asset changed from before, then the change of
// its quantity when recordQuantity is not nil, so the history reads in the
// order a person made them.
func (s *Store) recordAfter(tx *sql.Tx, id string, before *assetSnapshot, trigger string, recordQuantity func() error) error {
	if err := s.recordChangesTx(tx, id, before, trigger); err != nil {
		return err
	}
	if recordQuantity == nil {
		return nil
	}
	return recordQuantity()
}

// AssetEventsSince lists assets' events of the kinds given recorded at or
// after since, oldest first, for charts over time.
func (s *Store) AssetEventsSince(kinds []string, since time.Time) ([]models.Event, error) {
	if len(kinds) == 0 {
		return []models.Event{}, nil
	}
	marks := strings.TrimSuffix(strings.Repeat("?, ", len(kinds)), ", ")
	args := []any{dbTime(since)}
	for _, k := range kinds {
		args = append(args, k)
	}
	rows, err := s.Query("SELECT "+eventColumns+" FROM events WHERE created_at >= ? AND asset_id IS NOT NULL AND kind IN ("+marks+") ORDER BY created_at, id", args...)
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
