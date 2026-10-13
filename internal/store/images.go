package store

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

var ErrImageNotFound = errors.New("image not found")

// ImageInUseError is returned when deleting an image that templates still use.
type ImageInUseError struct {
	TemplateIDs []string
}

func (e *ImageInUseError) Error() string {
	return "image is used by templates: " + strings.Join(e.TemplateIDs, ", ")
}

const imageColumns = `id, filename, content_type, width_px, height_px, byte_size, sha256, created_at`

// CreateImage records an uploaded image's metadata. The caller stores the
// bytes and sets every field.
func (s *Store) CreateImage(img *models.Image) error {
	_, err := s.Exec(`INSERT INTO images (`+imageColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		img.ID, img.Filename, img.ContentType, img.WidthPx, img.HeightPx, img.ByteSize, img.SHA256, img.CreatedAt)
	if err != nil {
		return fmt.Errorf("inserting image: %w", err)
	}
	return nil
}

// GetImage retrieves an image's metadata.
func (s *Store) GetImage(id string) (*models.Image, error) {
	img, err := scanImage(s.QueryRow(`SELECT `+imageColumns+` FROM images WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrImageNotFound
	}
	return img, err
}

// ListImages returns every image, newest first.
func (s *Store) ListImages() ([]models.Image, error) {
	rows, err := s.Query(`SELECT ` + imageColumns + ` FROM images ORDER BY created_at DESC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("querying images: %w", err)
	}
	defer rows.Close()

	result := []models.Image{}
	for rows.Next() {
		img, err := scanImage(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *img)
	}
	return result, rows.Err()
}

// DeleteImage removes an image's metadata, refusing with *ImageInUseError
// while any template places it.
func (s *Store) DeleteImage(id string) error {
	if _, err := s.GetImage(id); err != nil {
		return err
	}

	templates, err := s.ListTemplates()
	if err != nil {
		return err
	}
	var users []string
	for i := range templates {
		for _, imageID := range templates[i].ImageIDs() {
			if imageID == id {
				users = append(users, templates[i].ID)
				break
			}
		}
	}
	if len(users) > 0 {
		sort.Strings(users)
		return &ImageInUseError{TemplateIDs: users}
	}

	if _, err := s.Exec(`DELETE FROM images WHERE id = ?`, id); err != nil {
		return fmt.Errorf("deleting image: %w", err)
	}
	return nil
}

func scanImage(row rowScanner) (*models.Image, error) {
	var img models.Image
	err := row.Scan(&img.ID, &img.Filename, &img.ContentType, &img.WidthPx, &img.HeightPx, &img.ByteSize, &img.SHA256, &img.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("scanning image: %w", err)
	}
	return &img, nil
}
