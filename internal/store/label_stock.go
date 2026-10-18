package store

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers"
	"github.com/google/uuid"
)

var (
	ErrLabelStockNotFound   = errors.New("label stock not found")
	ErrLabelStockNameExists = errors.New("label stock with this name already exists")
	// ErrLabelStockInUse wraps the refusal to delete stock a printer has loaded.
	ErrLabelStockInUse = errors.New("label stock is loaded in a printer")
)

const labelStockColumns = `id, name, width_mm, length_mm, gap_mm, created_at, updated_at`

// ListLabelStock returns every custom stock by name, with the printers that
// have each loaded.
func (s *Store) ListLabelStock() ([]models.LabelStock, error) {
	rows, err := s.Query(`SELECT ` + labelStockColumns + ` FROM label_stock ORDER BY name, id`)
	if err != nil {
		return nil, fmt.Errorf("querying label stock: %w", err)
	}
	defer rows.Close()
	list := []models.LabelStock{}
	byID := map[string]int{}
	for rows.Next() {
		st, err := scanLabelStock(rows)
		if err != nil {
			return nil, err
		}
		byID[st.ID] = len(list)
		list = append(list, *st)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	users, err := s.labelStockUsers("")
	if err != nil {
		return nil, err
	}
	for id, names := range users {
		if i, ok := byID[id]; ok {
			list[i].Printers = names
		}
	}
	return list, nil
}

// GetLabelStock reads one custom stock.
func (s *Store) GetLabelStock(id string) (*models.LabelStock, error) {
	st, err := scanLabelStock(s.QueryRow(`SELECT `+labelStockColumns+` FROM label_stock WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrLabelStockNotFound
	}
	if err != nil {
		return nil, err
	}
	users, err := s.labelStockUsers(id)
	if err != nil {
		return nil, err
	}
	if names := users[id]; names != nil {
		st.Printers = names
	}
	return st, nil
}

// CreateLabelStock saves new custom stock.
func (s *Store) CreateLabelStock(in printers.Stock) (*models.LabelStock, error) {
	in.Name = strings.TrimSpace(in.Name)
	if err := printers.ValidateStock(in); err != nil {
		return nil, err
	}
	in.ID = uuid.NewString()
	now := dbTime(time.Now())
	_, err := s.Exec(`INSERT INTO label_stock (`+labelStockColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		in.ID, in.Name, in.WidthMM, in.LengthMM, in.GapMM, now, now)
	if err != nil {
		return nil, labelStockWriteError(err)
	}
	return s.GetLabelStock(in.ID)
}

// UpdateLabelStock changes custom stock's name and size. Every printer with
// it loaded must still take it, or nothing changes and the error names the
// printer that would not.
func (s *Store) UpdateLabelStock(in printers.Stock) (*models.LabelStock, error) {
	in.Name = strings.TrimSpace(in.Name)
	if err := printers.ValidateStock(in); err != nil {
		return nil, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	res, err := s.TxExec(tx, `UPDATE label_stock SET name = ?, width_mm = ?, length_mm = ?, gap_mm = ?, updated_at = ? WHERE id = ?`,
		in.Name, in.WidthMM, in.LengthMM, in.GapMM, dbTime(time.Now()), in.ID)
	if err != nil {
		return nil, labelStockWriteError(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrLabelStockNotFound
	}
	rows, err := s.TxQuery(tx, `SELECT name, driver, model, connection_type, address, options FROM printers WHERE media = ? ORDER BY name`, in.ID)
	if err != nil {
		return nil, fmt.Errorf("finding printers with the stock loaded: %w", err)
	}
	type loaded struct {
		p       models.Printer
		options sql.NullString
	}
	var users []loaded
	for rows.Next() {
		var l loaded
		if err := rows.Scan(&l.p.Name, &l.p.Driver, &l.p.Model, &l.p.Connection.Type, &l.p.Connection.Address, &l.options); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scanning a printer: %w", err)
		}
		users = append(users, l)
	}
	rows.Close()
	for _, l := range users {
		settings := printers.Settings{Model: l.p.Model, Media: in.ID, Stock: &in}
		if l.options.Valid && l.options.String != "" {
			settings.Options = []byte(l.options.String)
		}
		if err := printers.Validate(l.p.Name, l.p.Driver, l.p.Connection, settings); err != nil {
			return nil, fmt.Errorf("%w (loaded in %s)", err, l.p.Name)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing: %w", err)
	}
	return s.GetLabelStock(in.ID)
}

// DeleteLabelStock removes custom stock no printer has loaded.
func (s *Store) DeleteLabelStock(id string) error {
	users, err := s.labelStockUsers(id)
	if err != nil {
		return err
	}
	if names := users[id]; len(names) > 0 {
		return fmt.Errorf("%w: %s", ErrLabelStockInUse, strings.Join(names, ", "))
	}
	res, err := s.Exec(`DELETE FROM label_stock WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting label stock: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrLabelStockNotFound
	}
	return nil
}

// stockForMedia returns the custom stock a printer's media names, or nil
// when it names a built-in roll.
func (s *Store) stockForMedia(media string) (*printers.Stock, error) {
	if uuid.Validate(media) != nil {
		return nil, nil
	}
	st, err := s.GetLabelStock(media)
	if errors.Is(err, ErrLabelStockNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &st.Stock, nil
}

// labelStockUsers maps stock IDs to the names of printers with each loaded: all
// stock, or only id.
func (s *Store) labelStockUsers(id string) (map[string][]string, error) {
	query := `SELECT p.media, p.name FROM printers p JOIN label_stock s ON s.id = p.media`
	var args []any
	if id != "" {
		query += ` WHERE s.id = ?`
		args = append(args, id)
	}
	rows, err := s.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("finding printers with stock loaded: %w", err)
	}
	defer rows.Close()
	users := map[string][]string{}
	for rows.Next() {
		var media, name string
		if err := rows.Scan(&media, &name); err != nil {
			return nil, fmt.Errorf("scanning a printer: %w", err)
		}
		users[media] = append(users[media], name)
	}
	for _, names := range users {
		sort.Strings(names)
	}
	return users, rows.Err()
}

func scanLabelStock(row rowScanner) (*models.LabelStock, error) {
	var st models.LabelStock
	err := row.Scan(&st.ID, &st.Name, &st.WidthMM, &st.LengthMM, &st.GapMM, &st.CreatedAt, &st.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("scanning label stock: %w", err)
	}
	st.Printers = []string{}
	return &st, nil
}

func labelStockWriteError(err error) error {
	if msg := strings.ToLower(err.Error()); strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate") {
		return ErrLabelStockNameExists
	}
	return fmt.Errorf("writing label stock: %w", err)
}
