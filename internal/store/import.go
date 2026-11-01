package store

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/google/uuid"
)

// ImportRow is one validated asset change from an import (docs/csv.md).
type ImportRow struct {
	// ID names the asset to update; empty creates one.
	ID string
	// NewID, for a row creating an asset, is the ID to give it, so links
	// in the same import can name it; empty picks one.
	NewID string
	Name  string
	// CategoryIDs replaces the asset's categories; nil leaves them as they
	// are. An empty list means Uncategorized.
	CategoryIDs []string
	// Codes sets the code for each generator type it names.
	Codes map[string]string
	// FieldValues sets the value of each field it names, by field ID.
	FieldValues map[string]string
	// Stock replaces the asset's stock, keeping its quantity when Stock has
	// none; nil leaves the stock as it is.
	Stock *StockInput
	// Icon sets the asset's own icon, "" for its categories'; nil leaves
	// it as it is.
	Icon *string
}

// ImportAssets applies validated rows in one transaction under the
// inventory lock, so either every row lands or none does, and thresholds are
// checked once for the whole import. It returns the IDs of created assets,
// in row order.
func (s *Store) ImportAssets(rows []ImportRow, links []ImportLink) ([]string, error) {
	if err := s.EnsureUncategorizedCategory(); err != nil {
		return nil, err
	}
	created := make([]string, 0, len(rows))
	err := s.withInventory(func(ic *inventoryChange) error {
		created = created[:0]
		tx, now := ic.tx, time.Now().UTC()
		ids := make([]string, len(rows))
		before := make([]*assetSnapshot, len(rows))
		recordQuantity := make([]func() error, len(rows))

		// Release every code being replaced before assigning any, so codes
		// can move between assets in one import.
		for i, row := range rows {
			ids[i] = row.ID
			if row.ID == "" {
				ids[i] = row.NewID
				if ids[i] == "" {
					ids[i] = uuid.NewString()
				}
				created = append(created, ids[i])
				continue
			}
			// importRow names a missing asset by its row.
			snap, err := s.snapshotAssetTx(tx, row.ID)
			if err != nil && !errors.Is(err, ErrAssetNotFound) {
				return err
			}
			before[i] = snap
			for _, t := range sortedKeys(row.Codes) {
				if _, err := s.TxExec(tx, "DELETE FROM asset_generator_keys WHERE asset_id = ? AND generator_type = ?", row.ID, t); err != nil {
					return fmt.Errorf("releasing code: %w", err)
				}
			}
		}

		for i, row := range rows {
			var err error
			if recordQuantity[i], err = s.importRow(tx, ids[i], row, now); err != nil {
				return fmt.Errorf("row %d: %w", i+1, err)
			}
		}

		for i, row := range rows {
			for _, t := range sortedKeys(row.Codes) {
				_, err := s.TxExec(tx, "INSERT INTO asset_generator_keys (asset_id, generator_type, assigned_code) VALUES (?, ?, ?)", ids[i], t, row.Codes[t])
				if err != nil {
					if isUniqueViolation(err) {
						return fmt.Errorf("%w (%s: %s)", ErrGeneratorCodeExists, t, row.Codes[t])
					}
					return fmt.Errorf("assigning code: %w", err)
				}
			}
		}

		for i := range rows {
			if err := s.recordAfter(tx, ids[i], before[i], models.TriggerImport, recordQuantity[i]); err != nil {
				return err
			}
		}
		return s.importLinksTx(tx, links)
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// importRow writes one row's name, categories, icon, stock and field
// values. It returns the function that records a change of the asset's
// quantity, if there was one, as setStockTx does.
func (s *Store) importRow(tx *sql.Tx, id string, row ImportRow, now time.Time) (recordQuantity func() error, err error) {
	if row.ID == "" {
		if _, err := s.TxExec(tx, "INSERT INTO assets (id, name, created_at, updated_at) VALUES (?, ?, ?, ?)", id, row.Name, now, now); err != nil {
			return nil, fmt.Errorf("inserting asset: %w", err)
		}
		if row.CategoryIDs == nil {
			row.CategoryIDs = []string{}
		}
	} else {
		res, err := s.TxExec(tx, "UPDATE assets SET name = ?, updated_at = ? WHERE id = ? AND archived_at IS NULL", row.Name, now, id)
		if err != nil {
			return nil, fmt.Errorf("updating asset: %w", err)
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return nil, fmt.Errorf("asset %s: %w or archived", id, ErrAssetNotFound)
		}
	}

	if row.Icon != nil {
		if err := s.setAssetIconTx(tx, id, models.RenditionScreen, *row.Icon); err != nil {
			return nil, err
		}
	}

	if row.CategoryIDs != nil {
		if _, err := s.TxExec(tx, "DELETE FROM asset_categories WHERE asset_id = ?", id); err != nil {
			return nil, err
		}
		cats := row.CategoryIDs
		if len(cats) == 0 {
			cats = []string{UncategorizedCategoryID}
		}
		for _, cid := range cats {
			if _, err := s.TxExec(tx, "INSERT INTO asset_categories (asset_id, category_id) VALUES (?, ?)", id, cid); err != nil {
				return nil, fmt.Errorf("attaching category %s: %w", cid, err)
			}
		}
	}

	if row.Stock != nil {
		if err := row.Stock.normalize(); err != nil {
			return nil, err
		}
		was, err := s.readStockTx(tx, id)
		if err != nil {
			return nil, err
		}
		if recordQuantity, err = s.setStockTx(tx, id, was, row.Stock); err != nil {
			return nil, err
		}
	}

	for _, fieldID := range sortedKeys(row.FieldValues) {
		if _, err := s.TxExec(tx, "DELETE FROM asset_field_values WHERE asset_id = ? AND field_id = ?", id, fieldID); err != nil {
			return nil, err
		}
		if v := row.FieldValues[fieldID]; v != "" {
			if _, err := s.TxExec(tx, "INSERT INTO asset_field_values (asset_id, field_id, value, updated_at) VALUES (?, ?, ?, ?)", id, fieldID, v, now); err != nil {
				return nil, fmt.Errorf("setting field: %w", err)
			}
		}
	}
	return recordQuantity, nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ErrImportOutdated is returned when assets or their links changed between
// checking an import and applying it; nothing is applied.
var ErrImportOutdated = errors.New("assets changed since the import was checked")

// ImportLink is a link an import adds or, with Remove, one it removes.
type ImportLink struct {
	Remove bool
	// LinkID names the link to remove.
	LinkID string
	// TypeID, From and To are the link to add, by asset ID (a new row's
	// NewID included).
	TypeID   string
	From, To string
}

// importLinksTx applies an import's links, removals first, under the links
// lock, and records them in both assets' history. A link that has gone, or
// a result that would put a thing in two places or inside itself, means
// something changed since the import was checked.
func (s *Store) importLinksTx(tx *sql.Tx, links []ImportLink) error {
	if len(links) == 0 {
		return nil
	}
	if err := s.takeLock(tx, linksLock); err != nil {
		return err
	}
	type relation struct{ name, inverse string }
	relations := map[string]relation{}
	rows, err := s.TxQuery(tx, "SELECT id, name, inverse_name FROM relation_types")
	if err != nil {
		return fmt.Errorf("reading relation types: %w", err)
	}
	for rows.Next() {
		var id string
		var r relation
		if err := rows.Scan(&id, &r.name, &r.inverse); err != nil {
			rows.Close()
			return err
		}
		relations[id] = r
	}
	rows.Close()
	names := map[string]string{}
	nameOf := func(id string) (string, error) {
		if n, ok := names[id]; ok {
			return n, nil
		}
		var n string
		if err := s.TxQueryRow(tx, "SELECT name FROM assets WHERE id = ?", id).Scan(&n); err != nil {
			return "", fmt.Errorf("%w: asset %s: %v", ErrImportOutdated, id, err)
		}
		names[id] = n
		return n, nil
	}
	changes := map[string][]models.EventChange{}
	note := func(typeID, from, to string, removed bool) error {
		fromName, err := nameOf(from)
		if err != nil {
			return err
		}
		toName, err := nameOf(to)
		if err != nil {
			return err
		}
		r := relations[typeID]
		out := models.EventChange{What: "link", Key: to, Label: r.name, To: toName}
		in := models.EventChange{What: "link", Key: from, Label: r.inverse, To: fromName}
		if removed {
			out.From, out.To, in.From, in.To = toName, "", fromName, ""
		}
		changes[from] = append(changes[from], out)
		changes[to] = append(changes[to], in)
		return nil
	}
	now := time.Now().UTC()
	for _, l := range links {
		if !l.Remove {
			continue
		}
		var typeID, from, to string
		err := s.TxQueryRow(tx, "SELECT relation_type_id, from_asset_id, to_asset_id FROM asset_links WHERE id = ?", l.LinkID).Scan(&typeID, &from, &to)
		if err == sql.ErrNoRows {
			return fmt.Errorf("%w: a link to remove has gone", ErrImportOutdated)
		}
		if err != nil {
			return fmt.Errorf("reading link: %w", err)
		}
		if _, err := s.TxExec(tx, "DELETE FROM asset_links WHERE id = ?", l.LinkID); err != nil {
			return fmt.Errorf("removing link: %w", err)
		}
		if err := note(typeID, from, to, true); err != nil {
			return err
		}
	}
	for _, l := range links {
		if l.Remove {
			continue
		}
		if _, ok := relations[l.TypeID]; !ok {
			return fmt.Errorf("%w: a relation type has gone", ErrImportOutdated)
		}
		if _, err := s.TxExec(tx, "INSERT INTO asset_links (id, relation_type_id, from_asset_id, to_asset_id, created_at) VALUES (?, ?, ?, ?, ?)",
			uuid.NewString(), l.TypeID, l.From, l.To, now); err != nil {
			if isUniqueViolation(err) {
				return fmt.Errorf("%w: a link to add already exists", ErrImportOutdated)
			}
			return fmt.Errorf("adding link: %w", err)
		}
		if err := note(l.TypeID, l.From, l.To, false); err != nil {
			return err
		}
	}
	if err := s.checkSingleLinksTx(tx); err != nil {
		return err
	}
	return s.recordLinkChanges(tx, changes, models.TriggerImport)
}

// checkSingleLinksTx checks every single relation's links: nothing is the
// far end of two, and none makes a loop.
func (s *Store) checkSingleLinksTx(tx *sql.Tx) error {
	rows, err := s.TxQuery(tx, `SELECT l.relation_type_id, l.from_asset_id, l.to_asset_id FROM asset_links l
		JOIN relation_types t ON t.id = l.relation_type_id AND t.is_single = TRUE`)
	if err != nil {
		return fmt.Errorf("reading links: %w", err)
	}
	defer rows.Close()
	parent := map[string]map[string]string{}
	for rows.Next() {
		var typeID, from, to string
		if err := rows.Scan(&typeID, &from, &to); err != nil {
			return err
		}
		if parent[typeID] == nil {
			parent[typeID] = map[string]string{}
		}
		if _, twice := parent[typeID][to]; twice {
			return fmt.Errorf("%w: something would be in two places", ErrImportOutdated)
		}
		parent[typeID][to] = from
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, up := range parent {
		for start := range up {
			at := start
			for depth := 0; depth <= len(up); depth++ {
				next, ok := up[at]
				if !ok {
					break
				}
				if next == start {
					return fmt.Errorf("%w: links would make a loop", ErrImportOutdated)
				}
				at = next
			}
		}
	}
	return nil
}
