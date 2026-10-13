package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers"
	"github.com/google/uuid"
)

var (
	ErrPrinterNotFound   = errors.New("printer not found")
	ErrPrinterNameExists = errors.New("a printer with this name already exists")
)

const printerColumns = `id, name, driver, model, host, port, media, created_at, updated_at`

// CreatePrinter validates and stores a printer. A zero port means 9100.
func (s *Store) CreatePrinter(p models.Printer) (*models.Printer, error) {
	p.Name = strings.TrimSpace(p.Name)
	if p.Port == 0 {
		p.Port = printers.DefaultPort
	}
	if err := printers.Validate(p.Name, p.Driver, p.Model, p.Media, p.Host, p.Port); err != nil {
		return nil, err
	}
	p.ID = uuid.NewString()
	now := time.Now().UTC()
	p.CreatedAt, p.UpdatedAt = now, now
	_, err := s.Exec(`INSERT INTO printers (`+printerColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.Driver, p.Model, p.Host, p.Port, p.Media, p.CreatedAt, p.UpdatedAt)
	if err != nil {
		return nil, printerWriteError(err)
	}
	return &p, nil
}

// GetPrinter retrieves a printer by ID.
func (s *Store) GetPrinter(id string) (*models.Printer, error) {
	p, err := scanPrinter(s.QueryRow(`SELECT `+printerColumns+` FROM printers WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrPrinterNotFound
	}
	return p, err
}

// ListPrinters returns every printer ordered by name.
func (s *Store) ListPrinters() ([]models.Printer, error) {
	rows, err := s.Query(`SELECT ` + printerColumns + ` FROM printers ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("querying printers: %w", err)
	}
	defer rows.Close()
	result := []models.Printer{}
	for rows.Next() {
		p, err := scanPrinter(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *p)
	}
	return result, rows.Err()
}

// UpdatePrinter replaces a printer's settings. A zero port means 9100.
func (s *Store) UpdatePrinter(p models.Printer) (*models.Printer, error) {
	existing, err := s.GetPrinter(p.ID)
	if err != nil {
		return nil, err
	}
	p.Name = strings.TrimSpace(p.Name)
	if p.Port == 0 {
		p.Port = printers.DefaultPort
	}
	if err := printers.Validate(p.Name, p.Driver, p.Model, p.Media, p.Host, p.Port); err != nil {
		return nil, err
	}
	p.CreatedAt, p.UpdatedAt = existing.CreatedAt, time.Now().UTC()
	_, err = s.Exec(`UPDATE printers SET name = ?, driver = ?, model = ?, host = ?, port = ?, media = ?, updated_at = ? WHERE id = ?`,
		p.Name, p.Driver, p.Model, p.Host, p.Port, p.Media, p.UpdatedAt, p.ID)
	if err != nil {
		return nil, printerWriteError(err)
	}
	return &p, nil
}

// DeletePrinter removes a printer.
func (s *Store) DeletePrinter(id string) error {
	res, err := s.Exec(`DELETE FROM printers WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting printer: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrPrinterNotFound
	}
	return nil
}

func printerWriteError(err error) error {
	if msg := strings.ToLower(err.Error()); strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate") {
		return ErrPrinterNameExists
	}
	return fmt.Errorf("writing printer: %w", err)
}

func scanPrinter(row rowScanner) (*models.Printer, error) {
	var p models.Printer
	err := row.Scan(&p.ID, &p.Name, &p.Driver, &p.Model, &p.Host, &p.Port, &p.Media, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("scanning printer: %w", err)
	}
	return &p, nil
}
