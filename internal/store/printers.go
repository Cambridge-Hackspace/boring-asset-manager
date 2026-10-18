package store

import (
	"bytes"
	"database/sql"
	"encoding/json"
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

const printerColumns = `id, name, driver, model, connection_type, address, media, options, created_at, updated_at`

// selectPrinters reads printers with the custom stock they name, if any.
const selectPrinters = `SELECT p.id, p.name, p.driver, p.model, p.connection_type, p.address, p.media, p.options, p.created_at, p.updated_at,
	s.id, s.name, s.width_mm, s.length_mm, s.gap_mm FROM printers p LEFT JOIN label_stock s ON s.id = p.media`

// preparePrinter normalizes and validates a printer before it is written,
// returning the options column value (NULL for driver defaults).
func preparePrinter(p *models.Printer) (*string, error) {
	p.Name = strings.TrimSpace(p.Name)
	conn, err := printers.NormalizeConnection(p.Connection)
	if err != nil {
		return nil, err
	}
	p.Connection = conn
	if trimmed := bytes.TrimSpace(p.Options); len(trimmed) == 0 || string(trimmed) == "null" {
		p.Options = nil
	}
	if err := printers.Validate(p.Name, p.Driver, p.Connection, PrinterSettings(p)); err != nil {
		return nil, err
	}
	if p.Options == nil {
		return nil, nil
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, p.Options); err != nil {
		return nil, fmt.Errorf("%w: options: %v", printers.ErrInvalidPrinter, err)
	}
	p.Options = compact.Bytes()
	options := compact.String()
	return &options, nil
}

// PrinterSettings returns the driver-specific part of a printer.
func PrinterSettings(p *models.Printer) printers.Settings {
	return printers.Settings{Model: p.Model, Media: p.Media, Stock: p.Stock, Options: p.Options}
}

// loadStock sets the custom stock a printer's media names, if any.
func (s *Store) loadStock(p *models.Printer) error {
	st, err := s.stockForMedia(p.Media)
	if err != nil {
		return err
	}
	p.Stock = st
	return nil
}

// PreviewPrinter checks a printer as CreatePrinter or UpdatePrinter would,
// and returns it as they would, without saving it.
func (s *Store) PreviewPrinter(p models.Printer) (*models.Printer, error) {
	if err := s.loadStock(&p); err != nil {
		return nil, err
	}
	if _, err := preparePrinter(&p); err != nil {
		return nil, err
	}
	p.Loaded = printers.Loaded(p.Driver, PrinterSettings(&p))
	return &p, nil
}

// CreatePrinter validates and stores a printer.
func (s *Store) CreatePrinter(p models.Printer) (*models.Printer, error) {
	if err := s.loadStock(&p); err != nil {
		return nil, err
	}
	options, err := preparePrinter(&p)
	if err != nil {
		return nil, err
	}
	p.ID = uuid.NewString()
	now := time.Now().UTC()
	p.CreatedAt, p.UpdatedAt = now, now
	_, err = s.Exec(`INSERT INTO printers (`+printerColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.Driver, p.Model, p.Connection.Type, p.Connection.Address, p.Media, options, p.CreatedAt, p.UpdatedAt)
	if err != nil {
		return nil, printerWriteError(err)
	}
	p.Loaded = printers.Loaded(p.Driver, PrinterSettings(&p))
	return &p, nil
}

// GetPrinter retrieves a printer by ID.
func (s *Store) GetPrinter(id string) (*models.Printer, error) {
	p, err := scanPrinter(s.QueryRow(selectPrinters+` WHERE p.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrPrinterNotFound
	}
	return p, err
}

// ListPrinters returns every printer ordered by name.
func (s *Store) ListPrinters() ([]models.Printer, error) {
	rows, err := s.Query(selectPrinters + ` ORDER BY p.name ASC`)
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

// UpdatePrinter replaces a printer's settings.
func (s *Store) UpdatePrinter(p models.Printer) (*models.Printer, error) {
	existing, err := s.GetPrinter(p.ID)
	if err != nil {
		return nil, err
	}
	if err := s.loadStock(&p); err != nil {
		return nil, err
	}
	options, err := preparePrinter(&p)
	if err != nil {
		return nil, err
	}
	p.CreatedAt, p.UpdatedAt = existing.CreatedAt, time.Now().UTC()
	_, err = s.Exec(`UPDATE printers SET name = ?, driver = ?, model = ?, connection_type = ?, address = ?, media = ?, options = ?, updated_at = ? WHERE id = ?`,
		p.Name, p.Driver, p.Model, p.Connection.Type, p.Connection.Address, p.Media, options, p.UpdatedAt, p.ID)
	if err != nil {
		return nil, printerWriteError(err)
	}
	p.Loaded = printers.Loaded(p.Driver, PrinterSettings(&p))
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
	var (
		p                  models.Printer
		options            sql.NullString
		stockID, stockName sql.NullString
		width, length, gap sql.NullFloat64
	)
	err := row.Scan(&p.ID, &p.Name, &p.Driver, &p.Model, &p.Connection.Type, &p.Connection.Address, &p.Media, &options, &p.CreatedAt, &p.UpdatedAt,
		&stockID, &stockName, &width, &length, &gap)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("scanning printer: %w", err)
	}
	if options.Valid && options.String != "" {
		p.Options = json.RawMessage(options.String)
	}
	if stockID.Valid {
		p.Stock = &printers.Stock{ID: stockID.String, Name: stockName.String, WidthMM: width.Float64, LengthMM: length.Float64, GapMM: gap.Float64}
	}
	p.Loaded = printers.Loaded(p.Driver, PrinterSettings(&p))
	return &p, nil
}
