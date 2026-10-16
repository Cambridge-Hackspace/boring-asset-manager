package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/textcheck"
	"github.com/google/uuid"
)

var (
	ErrReportNotFound = errors.New("report not found")
	// ErrInvalidReport wraps the reason a report cannot be saved as given.
	// Its spec is checked by the caller, with the reports package.
	ErrInvalidReport = errors.New("invalid report")
)

func normalizeReportName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("%w: a name is required", ErrInvalidReport)
	}
	if err := textcheck.Check("report name", name, textcheck.MaxName); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidReport, err)
	}
	return name, nil
}

// ListReports lists every report by name.
func (s *Store) ListReports() ([]models.Report, error) {
	rows, err := s.Query("SELECT id, name, spec, created_at, updated_at FROM reports ORDER BY name, id")
	if err != nil {
		return nil, fmt.Errorf("listing reports: %w", err)
	}
	defer rows.Close()
	reports := []models.Report{}
	for rows.Next() {
		r, err := scanReport(rows)
		if err != nil {
			return nil, err
		}
		reports = append(reports, r)
	}
	return reports, rows.Err()
}

func scanReport(row interface{ Scan(...any) error }) (models.Report, error) {
	var (
		r    models.Report
		spec string
	)
	err := row.Scan(&r.ID, &r.Name, &spec, &r.CreatedAt, &r.UpdatedAt)
	r.Spec = []byte(spec)
	return r, err
}

// GetReport returns a report, or ErrReportNotFound.
func (s *Store) GetReport(id string) (*models.Report, error) {
	r, err := scanReport(s.QueryRow("SELECT id, name, spec, created_at, updated_at FROM reports WHERE id = ?", id))
	if err == sql.ErrNoRows {
		return nil, ErrReportNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("reading report: %w", err)
	}
	return &r, nil
}

// CreateReport saves a new report with a spec its caller has checked.
func (s *Store) CreateReport(name string, spec []byte) (*models.Report, error) {
	name, err := normalizeReportName(name)
	if err != nil {
		return nil, err
	}
	id, now := uuid.NewString(), time.Now().UTC()
	if _, err := s.Exec("INSERT INTO reports (id, name, spec, created_at, updated_at) VALUES (?, ?, ?, ?, ?)", id, name, string(spec), now, now); err != nil {
		return nil, fmt.Errorf("saving report: %w", err)
	}
	return s.GetReport(id)
}

// UpdateReport replaces a report's name and spec.
func (s *Store) UpdateReport(id, name string, spec []byte) (*models.Report, error) {
	name, err := normalizeReportName(name)
	if err != nil {
		return nil, err
	}
	res, err := s.Exec("UPDATE reports SET name = ?, spec = ?, updated_at = ? WHERE id = ?", name, string(spec), time.Now().UTC(), id)
	if err != nil {
		return nil, fmt.Errorf("saving report: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return nil, fmt.Errorf("saving report: %w", err)
	} else if n == 0 {
		// MariaDB counts rows changed, so a save changing nothing is 0 too.
		if _, err := s.GetReport(id); err != nil {
			return nil, err
		}
	}
	return s.GetReport(id)
}

// DeleteReport deletes a report.
func (s *Store) DeleteReport(id string) error {
	if _, err := s.GetReport(id); err != nil {
		return err
	}
	if _, err := s.Exec("DELETE FROM reports WHERE id = ?", id); err != nil {
		return fmt.Errorf("deleting report: %w", err)
	}
	return nil
}
