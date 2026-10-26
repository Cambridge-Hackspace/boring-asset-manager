package store

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// Placements (README §3.7): where things stand in their location's scene,
// when someone has arranged them (migration 039).

var (
	// ErrNotInLocation refuses placing an asset that is in no location.
	ErrNotInLocation = errors.New("it is not in a location; put it in one first")
	// ErrInvalidPlacement refuses a position off the floor's near side, or
	// too far from it.
	ErrInvalidPlacement = errors.New("a position must be numbers of tiles from 0 to 10000")
)

// MaxPlacement is how far from a floor's corner a thing may be placed, in
// tiles.
const MaxPlacement = 10000

// Placements returns where the things in a location were placed, by
// asset ID, as x, y and z in tiles.
func (s *Store) Placements(locationID string) (map[string][3]float64, error) {
	rows, err := s.Query(`SELECT l.to_asset_id, p.x, p.y, p.z FROM asset_placements p
		JOIN asset_links l ON l.id = p.link_id
		JOIN relation_types t ON t.id = l.relation_type_id AND t.is_location = TRUE
		WHERE l.from_asset_id = ?`, locationID)
	if err != nil {
		return nil, fmt.Errorf("reading placements: %w", err)
	}
	defer rows.Close()
	out := map[string][3]float64{}
	for rows.Next() {
		var id string
		var at [3]float64
		if err := rows.Scan(&id, &at[0], &at[1], &at[2]); err != nil {
			return nil, fmt.Errorf("reading placements: %w", err)
		}
		out[id] = at
	}
	return out, rows.Err()
}

// PlaceAsset places an asset in its location's scene at x, y and z in
// tiles, or, with at nil, leaves it to be packed again: in the location
// in, or with in empty, the one it was put in first. A move is recorded in
// the asset's history; placing it where it is already changes nothing. It
// returns the location's ID.
func (s *Store) PlaceAsset(assetID, in string, at *[3]float64) (string, error) {
	if at != nil {
		for _, v := range at {
			if math.IsNaN(v) || v < 0 || v > MaxPlacement {
				return "", ErrInvalidPlacement
			}
		}
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return "", fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if err := s.takeLock(tx, linksLock); err != nil {
		return "", err
	}
	var archived sql.NullTime
	err = s.TxQueryRow(tx, "SELECT archived_at FROM assets WHERE id = ?", assetID).Scan(&archived)
	if err == sql.ErrNoRows {
		return "", ErrAssetNotFound
	}
	if err != nil {
		return "", fmt.Errorf("reading asset: %w", err)
	}
	if archived.Valid {
		return "", ErrAssetArchived
	}
	var linkID, placeID, placeName string
	err = s.TxQueryRow(tx, `SELECT l.id, a.id, a.name FROM asset_links l
		JOIN relation_types t ON t.id = l.relation_type_id AND t.is_location = TRUE
		JOIN assets a ON a.id = l.from_asset_id
		WHERE l.to_asset_id = ? AND (? = '' OR l.from_asset_id = ?) ORDER BY l.created_at, l.id`, assetID, in, in).Scan(&linkID, &placeID, &placeName)
	if err == sql.ErrNoRows {
		return "", ErrNotInLocation
	}
	if err != nil {
		return "", fmt.Errorf("reading where it is: %w", err)
	}
	var was *[3]float64
	var old [3]float64
	err = s.TxQueryRow(tx, "SELECT x, y, z FROM asset_placements WHERE link_id = ?", linkID).Scan(&old[0], &old[1], &old[2])
	switch {
	case err == nil:
		was = &old
	case err != sql.ErrNoRows:
		return "", fmt.Errorf("reading its placement: %w", err)
	}
	if (was == nil && at == nil) || (was != nil && at != nil && *was == *at) {
		return placeID, nil
	}
	if _, err := s.TxExec(tx, "DELETE FROM asset_placements WHERE link_id = ?", linkID); err != nil {
		return "", fmt.Errorf("placing: %w", err)
	}
	if at != nil {
		if _, err := s.TxExec(tx, "INSERT INTO asset_placements (link_id, x, y, z) VALUES (?, ?, ?, ?)", linkID, at[0], at[1], at[2]); err != nil {
			return "", fmt.Errorf("placing: %w", err)
		}
	}
	change := models.EventChange{What: "place", Key: placeID, Label: placeName, From: position(was), To: position(at)}
	if err := s.recordAssetEvent(tx, models.EventAssetEdited, assetID, models.EventPayload{Trigger: models.TriggerManual, Changes: []models.EventChange{change}}); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("committing: %w", err)
	}
	return placeID, nil
}

// position is a placement as a change records it, "2, 1, 0.5", or empty
// for none.
func position(at *[3]float64) string {
	if at == nil {
		return ""
	}
	parts := make([]string, 3)
	for i, v := range at {
		parts[i] = strconv.FormatFloat(v, 'f', -1, 64)
	}
	return strings.Join(parts, ", ")
}
