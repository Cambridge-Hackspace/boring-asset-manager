package store

import (
	"database/sql"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
)

// ImportRow is one validated asset change from an import (README §6.1).
type ImportRow struct {
	// ID names the asset to update; empty creates one.
	ID   string
	Name string
	// CategoryIDs replaces the asset's categories; nil leaves them as they
	// are. An empty list means Uncategorized.
	CategoryIDs []string
	// Codes sets the code for each generator type it names.
	Codes map[string]string
	// FieldValues sets the value of each field it names, by field ID.
	FieldValues map[string]string
}

// ImportAssets applies validated rows in one transaction under the
// inventory lock, so either every row lands or none does, and thresholds are
// checked once for the whole import. It returns the IDs of created assets,
// in row order.
func (s *Store) ImportAssets(rows []ImportRow) ([]string, error) {
	if err := s.EnsureUncategorizedCategory(); err != nil {
		return nil, err
	}
	created := make([]string, 0, len(rows))
	err := s.withInventory(func(ic *inventoryChange) error {
		created = created[:0]
		tx, now := ic.tx, time.Now().UTC()
		ids := make([]string, len(rows))

		// Release every code being replaced before assigning any, so codes
		// can move between assets in one import.
		for i, row := range rows {
			ids[i] = row.ID
			if row.ID == "" {
				ids[i] = uuid.NewString()
				created = append(created, ids[i])
				continue
			}
			for _, t := range sortedKeys(row.Codes) {
				if _, err := s.TxExec(tx, "DELETE FROM asset_generator_keys WHERE asset_id = ? AND generator_type = ?", row.ID, t); err != nil {
					return fmt.Errorf("releasing code: %w", err)
				}
			}
		}

		for i, row := range rows {
			if err := s.importRow(tx, ids[i], row, now); err != nil {
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
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// importRow writes one row's name, categories and field values.
func (s *Store) importRow(tx *sql.Tx, id string, row ImportRow, now time.Time) error {
	if row.ID == "" {
		if _, err := s.TxExec(tx, "INSERT INTO assets (id, name, created_at, updated_at) VALUES (?, ?, ?, ?)", id, row.Name, now, now); err != nil {
			return fmt.Errorf("inserting asset: %w", err)
		}
		if row.CategoryIDs == nil {
			row.CategoryIDs = []string{}
		}
	} else {
		res, err := s.TxExec(tx, "UPDATE assets SET name = ?, updated_at = ? WHERE id = ? AND archived_at IS NULL", row.Name, now, id)
		if err != nil {
			return fmt.Errorf("updating asset: %w", err)
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return fmt.Errorf("asset %s: %w or archived", id, ErrAssetNotFound)
		}
	}

	if row.CategoryIDs != nil {
		if _, err := s.TxExec(tx, "DELETE FROM asset_categories WHERE asset_id = ?", id); err != nil {
			return err
		}
		cats := row.CategoryIDs
		if len(cats) == 0 {
			cats = []string{UncategorizedCategoryID}
		}
		for _, cid := range cats {
			if _, err := s.TxExec(tx, "INSERT INTO asset_categories (asset_id, category_id) VALUES (?, ?)", id, cid); err != nil {
				return fmt.Errorf("attaching category %s: %w", cid, err)
			}
		}
	}

	for _, fieldID := range sortedKeys(row.FieldValues) {
		if _, err := s.TxExec(tx, "DELETE FROM asset_field_values WHERE asset_id = ? AND field_id = ?", id, fieldID); err != nil {
			return err
		}
		if v := row.FieldValues[fieldID]; v != "" {
			if _, err := s.TxExec(tx, "INSERT INTO asset_field_values (asset_id, field_id, value, updated_at) VALUES (?, ?, ?, ?)", id, fieldID, v, now); err != nil {
				return fmt.Errorf("setting field: %w", err)
			}
		}
	}
	return nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
