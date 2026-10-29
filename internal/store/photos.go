package store

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// MaxPhotos is how many photos an asset may have.
const MaxPhotos = 10

var (
	ErrPhotoNotFound = errors.New("photo not found")
	ErrTooManyPhotos = fmt.Errorf("an asset can have at most %d photos", MaxPhotos)
	// ErrPhotoOrder refuses an order that is not exactly the asset's photos.
	ErrPhotoOrder = errors.New("the order must name each of the asset's photos once")
)

const photoColumns = "id, asset_id, thumb_id, filename, content_type, width_px, height_px, byte_size, created_at"

func scanPhoto(row interface{ Scan(...any) error }) (models.AssetPhoto, string, error) {
	var (
		p       models.AssetPhoto
		assetID string
	)
	err := row.Scan(&p.ID, &assetID, &p.ThumbID, &p.Filename, &p.ContentType, &p.WidthPx, &p.HeightPx, &p.ByteSize, &p.CreatedAt)
	return p, assetID, err
}

// readPhotos reads photos in order, by asset.
func (s *Store) readPhotos(where string, args ...any) (map[string][]models.AssetPhoto, error) {
	rows, err := s.Query("SELECT "+photoColumns+" FROM asset_photos"+where+" ORDER BY asset_id, position", args...)
	if err != nil {
		return nil, fmt.Errorf("reading photos: %w", err)
	}
	defer rows.Close()
	out := map[string][]models.AssetPhoto{}
	for rows.Next() {
		p, assetID, err := scanPhoto(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning photo: %w", err)
		}
		out[assetID] = append(out[assetID], p)
	}
	return out, rows.Err()
}

// photoDetails fills in an asset's photos: from every asset's, read once,
// when ctx serves a list, and otherwise its own.
func (s *Store) photoDetails(a *models.AssetWithDetails, ctx *assetContext) error {
	var photos map[string][]models.AssetPhoto
	if ctx.list {
		if ctx.photos == nil {
			all, err := s.readPhotos("")
			if err != nil {
				return err
			}
			ctx.photos = all
		}
		photos = ctx.photos
	} else {
		own, err := s.readPhotos(" WHERE asset_id = ?", a.Asset.ID)
		if err != nil {
			return err
		}
		photos = own
	}
	a.Photos = photos[a.Asset.ID]
	if a.Photos == nil {
		a.Photos = []models.AssetPhoto{}
	}
	return nil
}

// GetPhoto returns one of an asset's photos, or ErrPhotoNotFound.
func (s *Store) GetPhoto(assetID, photoID string) (*models.AssetPhoto, error) {
	p, _, err := scanPhoto(s.QueryRow("SELECT "+photoColumns+" FROM asset_photos WHERE id = ? AND asset_id = ?", photoID, assetID))
	if err == sql.ErrNoRows {
		return nil, ErrPhotoNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("reading photo: %w", err)
	}
	return &p, nil
}

// editablePhotosTx locks an active asset and returns its photo IDs in order.
func (s *Store) editablePhotosTx(tx *sql.Tx, assetID string) ([]string, error) {
	if err := s.lockAssetTx(tx, assetID); err != nil {
		return nil, err
	}
	var archived sql.NullTime
	if err := s.TxQueryRow(tx, "SELECT archived_at FROM assets WHERE id = ?", assetID).Scan(&archived); err != nil {
		return nil, fmt.Errorf("reading asset: %w", err)
	}
	if archived.Valid {
		return nil, ErrAssetArchived
	}
	rows, err := s.TxQuery(tx, "SELECT id FROM asset_photos WHERE asset_id = ? ORDER BY position", assetID)
	if err != nil {
		return nil, fmt.Errorf("reading photos: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// AddPhoto adds a photo, whose files are already stored, after an asset's
// others, and records it in the asset's history.
func (s *Store) AddPhoto(assetID string, p models.AssetPhoto) (*models.AssetWithDetails, error) {
	return s.AddPhotos(assetID, []models.AssetPhoto{p})
}

// AddPhotos adds photos, whose files are already stored, after an asset's
// others, in order, and records them in the asset's history: all of them,
// or none if that would take it past MaxPhotos.
func (s *Store) AddPhotos(assetID string, ps []models.AssetPhoto) (*models.AssetWithDetails, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	ids, err := s.editablePhotosTx(tx, assetID)
	if err != nil {
		return nil, err
	}
	if len(ids)+len(ps) > MaxPhotos {
		return nil, ErrTooManyPhotos
	}
	changes := make([]models.EventChange, 0, len(ps))
	for i, p := range ps {
		_, err = s.TxExec(tx, "INSERT INTO asset_photos ("+photoColumns+", position) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			p.ID, assetID, p.ThumbID, p.Filename, p.ContentType, p.WidthPx, p.HeightPx, p.ByteSize, dbTime(p.CreatedAt), len(ids)+i)
		if err != nil {
			return nil, fmt.Errorf("adding photo: %w", err)
		}
		changes = append(changes, models.EventChange{What: "photo", Key: p.ID, To: p.Filename})
	}
	if len(changes) > 0 {
		if err := s.recordAssetEvent(tx, models.EventAssetEdited, assetID, models.EventPayload{
			Trigger: models.TriggerManual,
			Changes: changes,
		}); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing: %w", err)
	}
	return s.GetAssetByID(assetID)
}

// RemovePhoto removes one of an asset's photos and records it, returning
// the photo so its caller can remove its files.
func (s *Store) RemovePhoto(assetID, photoID string) (*models.AssetWithDetails, *models.AssetPhoto, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	ids, err := s.editablePhotosTx(tx, assetID)
	if err != nil {
		return nil, nil, err
	}
	p, _, err := scanPhoto(s.TxQueryRow(tx, "SELECT "+photoColumns+" FROM asset_photos WHERE id = ? AND asset_id = ?", photoID, assetID))
	if err == sql.ErrNoRows {
		return nil, nil, ErrPhotoNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("reading photo: %w", err)
	}
	if _, err := s.TxExec(tx, "DELETE FROM asset_photos WHERE id = ?", photoID); err != nil {
		return nil, nil, fmt.Errorf("removing photo: %w", err)
	}
	rest := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != photoID {
			rest = append(rest, id)
		}
	}
	if err := s.positionPhotosTx(tx, rest); err != nil {
		return nil, nil, err
	}
	if err := s.recordAssetEvent(tx, models.EventAssetEdited, assetID, models.EventPayload{
		Trigger: models.TriggerManual,
		Changes: []models.EventChange{{What: "photo", Key: p.ID, From: p.Filename}},
	}); err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("committing: %w", err)
	}
	a, err := s.GetAssetByID(assetID)
	return a, &p, err
}

// ReorderPhotos puts an asset's photos in the order given, which must name
// each of them once. The first is the one lists show.
func (s *Store) ReorderPhotos(assetID string, order []string) (*models.AssetWithDetails, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	ids, err := s.editablePhotosTx(tx, assetID)
	if err != nil {
		return nil, err
	}
	have, want := append([]string{}, ids...), append([]string{}, order...)
	sort.Strings(have)
	sort.Strings(want)
	if len(have) != len(want) {
		return nil, ErrPhotoOrder
	}
	for i := range have {
		if have[i] != want[i] {
			return nil, ErrPhotoOrder
		}
	}
	if err := s.positionPhotosTx(tx, order); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing: %w", err)
	}
	return s.GetAssetByID(assetID)
}

func (s *Store) positionPhotosTx(tx *sql.Tx, ids []string) error {
	for i, id := range ids {
		if _, err := s.TxExec(tx, "UPDATE asset_photos SET position = ? WHERE id = ?", i, id); err != nil {
			return fmt.Errorf("ordering photos: %w", err)
		}
	}
	return nil
}

// StoredFiles lists the keys of the files rows name in the file store
// (filestore.Images): each photo and its thumbnail, as photos/<id>, and
// each label image, as images/<id>.
func (s *Store) StoredFiles() (map[string]bool, error) {
	out := map[string]bool{}
	for query, prefixes := range map[string][]string{
		"SELECT id, thumb_id FROM asset_photos": {"photos/", "photos/"},
		"SELECT id, id FROM images":             {"images/", "images/"},
	} {
		rows, err := s.Query(query)
		if err != nil {
			return nil, fmt.Errorf("reading stored files: %w", err)
		}
		for rows.Next() {
			var a, b string
			if err := rows.Scan(&a, &b); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scanning a stored file: %w", err)
			}
			out[prefixes[0]+a], out[prefixes[1]+b] = true, true
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
