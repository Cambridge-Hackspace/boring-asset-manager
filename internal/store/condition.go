package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/textcheck"
)

// SetAssetCondition sets an asset's condition and the note saying what is
// wrong; an ok asset keeps no note. It reports whether anything changed, and
// records asset.condition_changed when it did. Archived assets are refused
// with ErrAssetArchived.
func (s *Store) SetAssetCondition(id, condition, note string) (*models.AssetWithDetails, bool, error) {
	if !models.ValidCondition(condition) {
		return nil, false, ErrInvalidCondition
	}
	note = strings.TrimSpace(note)
	if condition == models.ConditionOK {
		note = ""
	}
	if err := textcheck.Check("note", note, textcheck.MaxName); err != nil {
		return nil, false, err
	}

	tx, err := s.DB.Begin()
	if err != nil {
		return nil, false, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	var (
		was, wasNote string
		archived     sql.NullTime
	)
	err = s.TxQueryRow(tx, "SELECT asset_condition, condition_note, archived_at FROM assets WHERE id = ?", id).Scan(&was, &wasNote, &archived)
	if err == sql.ErrNoRows {
		return nil, false, ErrAssetNotFound
	}
	if err != nil {
		return nil, false, fmt.Errorf("reading asset condition: %w", err)
	}
	if archived.Valid {
		return nil, false, ErrAssetArchived
	}
	changed := was != condition || wasNote != note
	if changed {
		if _, err := s.TxExec(tx, "UPDATE assets SET asset_condition = ?, condition_note = ?, updated_at = ? WHERE id = ?", condition, note, time.Now().UTC(), id); err != nil {
			return nil, false, fmt.Errorf("setting condition: %w", err)
		}
		err := s.recordAssetEvent(tx, models.EventAssetConditionChanged, id, models.EventPayload{
			Trigger: models.TriggerManual,
			Changes: []models.EventChange{{What: "condition", From: was, To: condition}},
			Note:    note,
		})
		if err != nil {
			return nil, false, err
		}
		if err := tx.Commit(); err != nil {
			return nil, false, fmt.Errorf("committing: %w", err)
		}
	} else {
		// Ended before reading back: SQLite has one connection, which the
		// open transaction would hold.
		_ = tx.Rollback()
	}
	a, err := s.GetAssetByID(id)
	return a, changed, err
}
