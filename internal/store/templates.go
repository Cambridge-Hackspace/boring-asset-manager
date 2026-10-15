package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
	"github.com/google/uuid"
)

var ErrTemplateNotFound = errors.New("template not found")

const templateColumns = `id, name, units, width, height, dpi, layout_json, created_at, updated_at`

// templateLayout is the JSON stored in templates.layout_json.
type templateLayout struct {
	Elements []labels.Element `json:"elements"`
}

// CreateTemplate validates and stores a new template under a new ID; any ID
// t already has is ignored. Bindings must name existing fields and images
// must exist.
func (s *Store) CreateTemplate(t *labels.Template) (*labels.Template, error) {
	created := *t
	created.ID = uuid.NewString()
	if err := s.validateTemplate(&created); err != nil {
		return nil, err
	}

	layout, err := json.Marshal(templateLayout{Elements: created.Elements})
	if err != nil {
		return nil, fmt.Errorf("encoding template layout: %w", err)
	}
	now := time.Now().UTC()
	_, err = s.Exec(`INSERT INTO templates (`+templateColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		created.ID, created.Name, created.Units, created.Width, created.Height, created.DPI, string(layout), now, now)
	if err != nil {
		return nil, fmt.Errorf("inserting template: %w", err)
	}

	created.CreatedAt, created.UpdatedAt = now, now
	return &created, nil
}

// GetTemplate retrieves a template by ID.
func (s *Store) GetTemplate(id string) (*labels.Template, error) {
	t, err := scanTemplate(s.QueryRow(`SELECT `+templateColumns+` FROM templates WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrTemplateNotFound
	}
	return t, err
}

// ListTemplates returns every template ordered by name.
func (s *Store) ListTemplates() ([]labels.Template, error) {
	rows, err := s.Query(`SELECT ` + templateColumns + ` FROM templates ORDER BY name ASC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("querying templates: %w", err)
	}
	defer rows.Close()

	result := []labels.Template{}
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *t)
	}
	return result, rows.Err()
}

// UpdateTemplate replaces a template's content. The ID cannot change.
func (s *Store) UpdateTemplate(t *labels.Template) (*labels.Template, error) {
	existing, err := s.GetTemplate(t.ID)
	if err != nil {
		return nil, err
	}
	if err := s.validateTemplate(t); err != nil {
		return nil, err
	}

	layout, err := json.Marshal(templateLayout{Elements: t.Elements})
	if err != nil {
		return nil, fmt.Errorf("encoding template layout: %w", err)
	}
	now := time.Now().UTC()
	_, err = s.Exec(`UPDATE templates SET name = ?, units = ?, width = ?, height = ?, dpi = ?, layout_json = ?, updated_at = ? WHERE id = ?`,
		t.Name, t.Units, t.Width, t.Height, t.DPI, string(layout), now, t.ID)
	if err != nil {
		return nil, fmt.Errorf("updating template: %w", err)
	}

	updated := *t
	updated.CreatedAt, updated.UpdatedAt = existing.CreatedAt, now
	return &updated, nil
}

// DeleteTemplate removes a template.
func (s *Store) DeleteTemplate(id string) error {
	res, err := s.Exec(`DELETE FROM templates WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting template: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrTemplateNotFound
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTemplate(row rowScanner) (*labels.Template, error) {
	var (
		t      labels.Template
		layout string
	)
	if err := row.Scan(&t.ID, &t.Name, &t.Units, &t.Width, &t.Height, &t.DPI, &layout, &t.CreatedAt, &t.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("scanning template: %w", err)
	}
	var l templateLayout
	if err := json.Unmarshal([]byte(layout), &l); err != nil {
		return nil, fmt.Errorf("decoding layout of template %s: %w", t.ID, err)
	}
	t.Elements = l.Elements
	if t.Elements == nil {
		t.Elements = []labels.Element{}
	}
	return &t, nil
}

// validateTemplate checks the template itself, then that every field key and
// image it references exists, reporting problems at the elements that use them.
func (s *Store) validateTemplate(t *labels.Template) error {
	t.Normalize()
	if err := t.Validate(); err != nil {
		return err
	}

	var problems []labels.Problem
	knownFields := map[string]bool{}
	for i := range t.Elements {
		e := &t.Elements[i]
		path := fmt.Sprintf("elements[%d]", i)

		if e.Type == labels.ElementStaticImage {
			if _, err := s.GetImage(e.ImageID); errors.Is(err, ErrImageNotFound) {
				problems = append(problems, labels.Problem{Path: path + ".image_id", Message: "no uploaded image has this ID"})
			} else if err != nil {
				return err
			}
			continue
		}

		b, err := e.Binding()
		if err != nil {
			continue // already reported by Validate
		}
		for _, ref := range b.Refs() {
			if ref.Kind != labels.RefField {
				continue
			}
			known, checked := knownFields[ref.Arg]
			if !checked {
				_, err := s.GetFieldByKey(ref.Arg)
				if err != nil && !errors.Is(err, ErrFieldNotFound) {
					return err
				}
				known = err == nil
				knownFields[ref.Arg] = known
			}
			if !known {
				problems = append(problems, labels.Problem{
					Path:    path + ".content_binding",
					Message: fmt.Sprintf("field %q does not exist", ref.Arg),
				})
			}
		}
	}
	if len(problems) > 0 {
		return &labels.ValidationError{Problems: problems}
	}
	return nil
}
