package store

import (
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// ErrUploadNotFound is returned for an upload that is not there, or not
// the person's asking.
var ErrUploadNotFound = errors.New("upload not found")

const uploadColumns = "id, user_id, filename, size, sha256, part_size, received, hash_state, staging_token, complete, created_at, updated_at"

// CreateUpload records an upload begun.
func (s *Store) CreateUpload(u *models.Upload) error {
	u.CreatedAt = dbTime(time.Now())
	u.UpdatedAt = u.CreatedAt
	_, err := s.Exec("INSERT INTO uploads ("+uploadColumns+") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		u.ID, u.UserID, u.Filename, u.Size, u.SHA256, u.PartSize, u.Received, hex.EncodeToString(u.HashState), u.StagingToken, u.Complete, u.CreatedAt, u.UpdatedAt)
	if err != nil {
		return fmt.Errorf("recording an upload: %w", err)
	}
	return nil
}

// GetUpload reads an upload, which must be userID's.
func (s *Store) GetUpload(id, userID string) (*models.Upload, error) {
	var u models.Upload
	var state string
	err := s.QueryRow("SELECT "+uploadColumns+" FROM uploads WHERE id = ? AND user_id = ?", id, userID).Scan(
		&u.ID, &u.UserID, &u.Filename, &u.Size, &u.SHA256, &u.PartSize, &u.Received, &state, &u.StagingToken, &u.Complete, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUploadNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("reading an upload: %w", err)
	}
	if u.HashState, err = hex.DecodeString(state); err != nil {
		return nil, fmt.Errorf("reading an upload's hash: %w", err)
	}
	return &u, nil
}

// AdvanceUpload records a part arrived: from received bytes to the next,
// the hash of all so far, and the part's tag. It reports false, changing
// nothing, when the upload had not received exactly from: another part
// arrived first.
func (s *Store) AdvanceUpload(id string, from, to int64, hashState []byte, part int, tag string) (bool, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	res, err := s.TxExec(tx, "UPDATE uploads SET received = ?, hash_state = ?, updated_at = ? WHERE id = ? AND received = ? AND complete = ?",
		to, hex.EncodeToString(hashState), dbTime(time.Now()), id, from, false)
	if err != nil {
		return false, fmt.Errorf("recording a part: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		return false, err
	}
	if _, err := s.TxExec(tx, "DELETE FROM upload_parts WHERE upload_id = ? AND part = ?", id, part); err != nil {
		return false, err
	}
	if _, err := s.TxExec(tx, "INSERT INTO upload_parts (upload_id, part, tag) VALUES (?, ?, ?)", id, part, tag); err != nil {
		return false, fmt.Errorf("recording a part: %w", err)
	}
	return true, tx.Commit()
}

// UploadTags are an upload's parts' tags, in order.
func (s *Store) UploadTags(id string) ([]string, error) {
	rows, err := s.Query("SELECT tag FROM upload_parts WHERE upload_id = ? ORDER BY part", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// CompleteUpload records an upload whole and checked.
func (s *Store) CompleteUpload(id string) error {
	if _, err := s.Exec("UPDATE uploads SET complete = ?, updated_at = ? WHERE id = ?", true, dbTime(time.Now()), id); err != nil {
		return err
	}
	_, err := s.Exec("DELETE FROM upload_parts WHERE upload_id = ?", id)
	return err
}

// TouchUpload records an upload used, so it is kept another day.
func (s *Store) TouchUpload(id string) error {
	_, err := s.Exec("UPDATE uploads SET updated_at = ? WHERE id = ?", dbTime(time.Now()), id)
	return err
}

// DeleteUpload forgets an upload.
func (s *Store) DeleteUpload(id string) error {
	_, err := s.Exec("DELETE FROM uploads WHERE id = ?", id)
	return err
}

// StaleUploads are the uploads not added to or used since before.
func (s *Store) StaleUploads(before time.Time) ([]models.Upload, error) {
	rows, err := s.Query("SELECT id, staging_token FROM uploads WHERE updated_at < ?", dbTime(before))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Upload
	for rows.Next() {
		var u models.Upload
		if err := rows.Scan(&u.ID, &u.StagingToken); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
