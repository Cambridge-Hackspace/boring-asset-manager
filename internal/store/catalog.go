package store

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/money"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/textcheck"
	"github.com/google/uuid"
)

// Catalogs (docs/vendors.md#catalogs): what each vendor sells, with part
// numbers and prices. An entry linked to a category is kept in
// category_catalog_entries, and one linked to an asset in
// asset_catalog_entries; their IDs are unique across both.

const (
	// MaxCatalogDescription is the longest description of an entry.
	MaxCatalogDescription = 2000
	// MaxPackUnit is the longest pack unit: VARCHAR(64).
	MaxPackUnit = 64
	// MaxPackSize is the largest pack.
	MaxPackSize = 1_000_000_000
)

var (
	// ErrInvalidCatalogEntry is wrapped by every problem with an entry.
	ErrInvalidCatalogEntry = errors.New("invalid catalog entry")
	// ErrCatalogEntryNotFound is an entry that does not exist.
	ErrCatalogEntryNotFound = errors.New("catalog entry not found")
)

// CatalogEntryInput is everything a caller sets on an entry: an empty one
// is not recorded. It is linked to CategoryID or to AssetID, one of them.
// PriceChecked is YYYY-MM-DD; Price is a decimal in Currency, which it
// needs. Preferred makes it the one to reorder from of its category's or
// asset's, which none of the others then is.
type CatalogEntryInput struct {
	VendorID     string
	CategoryID   string
	AssetID      string
	PartNumber   string
	Description  string
	URL          string
	PackSize     *int
	PackUnit     string
	Price        string
	Currency     string
	PriceChecked string
	Preferred    bool
}

// CatalogEntryInputOf is the input that keeps an entry as it is.
func CatalogEntryInputOf(e models.CatalogEntry) CatalogEntryInput {
	return CatalogEntryInput{VendorID: e.VendorID, CategoryID: e.CategoryID, AssetID: e.AssetID, PartNumber: e.PartNumber,
		Description: e.Description, URL: e.URL, PackSize: e.PackSize, PackUnit: e.PackUnit, Price: e.Price, Currency: e.Currency,
		PriceChecked: e.PriceChecked, Preferred: e.Preferred}
}

// catalogRow is an entry checked and ready to keep.
type catalogRow struct {
	vendorID, linkID        string
	table, linkColumn       string
	partNumber, description string
	url, packUnit, currency string
	packSize                *int
	priceMinor              *int64
	priceChecked            *string
	preferred               bool
}

func invalidEntry(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidCatalogEntry, fmt.Sprintf(format, args...))
}

// check trims an entry, checks it can be kept, and gives what is kept.
func (in CatalogEntryInput) check() (*catalogRow, error) {
	row := catalogRow{
		vendorID:    strings.TrimSpace(in.VendorID),
		partNumber:  strings.TrimSpace(in.PartNumber),
		description: strings.TrimSpace(in.Description),
		url:         strings.TrimSpace(in.URL),
		packUnit:    strings.TrimSpace(in.PackUnit),
		packSize:    in.PackSize,
		preferred:   in.Preferred,
	}
	if row.vendorID == "" {
		return nil, invalidEntry("an entry needs its vendor")
	}
	category, asset := strings.TrimSpace(in.CategoryID), strings.TrimSpace(in.AssetID)
	switch {
	case category != "" && asset != "":
		return nil, invalidEntry("an entry is linked to a category or to an asset, not both")
	case category != "":
		row.table, row.linkColumn, row.linkID = "category_catalog_entries", "category_id", category
	case asset != "":
		row.table, row.linkColumn, row.linkID = "asset_catalog_entries", "asset_id", asset
	default:
		return nil, invalidEntry("an entry is linked to a category or to an asset")
	}
	if row.partNumber == "" && row.description == "" {
		return nil, invalidEntry("an entry needs a part number or a description")
	}
	for _, c := range []struct {
		what, value string
		max         int
	}{
		{"the part number", row.partNumber, textcheck.MaxName},
		{"the description", row.description, MaxCatalogDescription},
		{"the product page", row.url, MaxVendorWebsite},
		{"the pack unit", row.packUnit, MaxPackUnit},
	} {
		if err := textcheck.Check(c.what, c.value, c.max); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidCatalogEntry, err)
		}
	}
	if row.url != "" {
		u, err := url.Parse(row.url)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, invalidEntry("the product page must be an http:// or https:// address")
		}
	}
	if row.packSize != nil && (*row.packSize < 1 || *row.packSize > MaxPackSize) {
		return nil, invalidEntry("a pack holds a whole number of at least 1")
	}
	var err error
	if row.currency, err = money.NormalizeCurrency(in.Currency); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCatalogEntry, err)
	}
	if price := strings.TrimSpace(in.Price); price != "" {
		if row.currency == "" {
			return nil, invalidEntry("a price needs its currency")
		}
		minor, err := money.Parse(price, row.currency)
		if err != nil {
			return nil, invalidEntry("the price: %v", err)
		}
		row.priceMinor = &minor
	}
	if checked := strings.TrimSpace(in.PriceChecked); checked != "" {
		t, err := time.Parse(DateLayout, checked)
		if err != nil || t.Year() < 1000 {
			return nil, invalidEntry("when the price was checked must be a date written YYYY-MM-DD")
		}
		d := t.Format(DateLayout)
		row.priceChecked = &d
	}
	return &row, nil
}

// CheckCatalogEntry validates an entry without keeping it, as an import
// checks its rows first.
func CheckCatalogEntry(in CatalogEntryInput) error {
	_, err := in.check()
	return err
}

// CatalogFilter narrows a list of entries: to a vendor's, a category's or
// an asset's, each when given, and to those preferred.
type CatalogFilter struct {
	VendorID   string
	CategoryID string
	AssetID    string
	Preferred  bool
}

const catalogEntryColumns = "e.id, e.vendor_id, v.name, e.part_number, e.description, e.url, e.pack_size, e.pack_unit, e.price_minor, e.currency, e.price_checked, e.preferred, e.created_at, e.updated_at"

// catalogQuerier reads within a transaction (txQuerier) or not
// (storeQuerier).
type catalogQuerier interface {
	Query(string, ...any) (*sql.Rows, error)
}

// readCatalog reads the entries f names, by what they are linked to, then
// vendor, then part number.
func readCatalog(q catalogQuerier, f CatalogFilter, id string) ([]models.CatalogEntry, error) {
	out := []models.CatalogEntry{}
	for _, t := range []struct{ table, column, join string }{
		{"category_catalog_entries", "category_id", "categories"},
		{"asset_catalog_entries", "asset_id", "assets"},
	} {
		if f.CategoryID != "" && t.column != "category_id" || f.AssetID != "" && t.column != "asset_id" {
			continue
		}
		var where []string
		var args []any
		for _, c := range []struct{ col, value string }{{"e.id", id}, {"e.vendor_id", f.VendorID}, {"e.category_id", f.CategoryID}, {"e.asset_id", f.AssetID}} {
			if c.value != "" {
				where = append(where, c.col+" = ?")
				args = append(args, c.value)
			}
		}
		if f.Preferred {
			where = append(where, "e.preferred = ?")
			args = append(args, true)
		}
		query := "SELECT " + catalogEntryColumns + ", e." + t.column + ", l.name FROM " + t.table + " e JOIN vendors v ON v.id = e.vendor_id JOIN " +
			t.join + " l ON l.id = e." + t.column
		if len(where) > 0 {
			query += " WHERE " + strings.Join(where, " AND ")
		}
		rows, err := q.Query(query, args...)
		if err != nil {
			return nil, fmt.Errorf("reading the catalog: %w", err)
		}
		for rows.Next() {
			var (
				e       models.CatalogEntry
				pack    sql.NullInt64
				minor   sql.NullInt64
				checked sql.NullString
				link    string
			)
			if err := rows.Scan(&e.ID, &e.VendorID, &e.VendorName, &e.PartNumber, &e.Description, &e.URL, &pack, &e.PackUnit, &minor, &e.Currency,
				&checked, &e.Preferred, &e.CreatedAt, &e.UpdatedAt, &link, &e.LinkedName); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scanning a catalog entry: %w", err)
			}
			if t.column == "category_id" {
				e.CategoryID = link
			} else {
				e.AssetID = link
			}
			if pack.Valid {
				n := int(pack.Int64)
				e.PackSize = &n
			}
			if minor.Valid {
				e.Price = money.Format(minor.Int64, e.Currency)
			}
			e.PriceChecked = checked.String
			out = append(out, e)
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.LinkedName != b.LinkedName {
			return a.LinkedName < b.LinkedName
		}
		if a.VendorName != b.VendorName {
			return a.VendorName < b.VendorName
		}
		if a.PartNumber != b.PartNumber {
			return a.PartNumber < b.PartNumber
		}
		return a.ID < b.ID
	})
	return out, nil
}

// ListCatalogEntries returns the entries f names.
func (s *Store) ListCatalogEntries(f CatalogFilter) ([]models.CatalogEntry, error) {
	return readCatalog(storeQuerier{s}, f, "")
}

// GetCatalogEntry returns an entry, or ErrCatalogEntryNotFound.
func (s *Store) GetCatalogEntry(id string) (*models.CatalogEntry, error) {
	return getCatalogEntry(storeQuerier{s}, id)
}

func getCatalogEntry(q catalogQuerier, id string) (*models.CatalogEntry, error) {
	if id == "" {
		return nil, ErrCatalogEntryNotFound
	}
	list, err := readCatalog(q, CatalogFilter{}, id)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrCatalogEntryNotFound
	}
	return &list[0], nil
}

// CatalogWrite is an entry to make, with no ID, or to replace, by its ID,
// as an import writes them.
type CatalogWrite struct {
	ID string
	In CatalogEntryInput
}

// CreateCatalogEntry makes an entry, and records it in its vendor's
// history.
func (s *Store) CreateCatalogEntry(in CatalogEntryInput) (*models.CatalogEntry, error) {
	ids, err := s.WriteCatalogEntries([]CatalogWrite{{In: in}})
	if err != nil {
		return nil, err
	}
	return s.GetCatalogEntry(ids[0])
}

// UpdateCatalogEntry replaces an entry, and records what changed in its
// vendor's history: its vendor's and, when it moves to another vendor,
// that one's too.
func (s *Store) UpdateCatalogEntry(id string, in CatalogEntryInput) (*models.CatalogEntry, error) {
	if _, err := s.WriteCatalogEntries([]CatalogWrite{{ID: id, In: in}}); err != nil {
		return nil, err
	}
	return s.GetCatalogEntry(id)
}

// WriteCatalogEntries makes and replaces entries, all or none, in one
// transaction, and records the changes in their vendors' histories, one
// event for each vendor. It returns each entry's ID, in order.
func (s *Store) WriteCatalogEntries(writes []CatalogWrite) ([]string, error) {
	rows := make([]*catalogRow, len(writes))
	for i, w := range writes {
		row, err := w.In.check()
		if err != nil {
			return nil, err
		}
		rows[i] = row
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	q := txQuerier{s, tx}
	changes := map[string][]models.EventChange{}
	ids := make([]string, len(writes))
	now := dbTime(time.Now())
	for i, w := range writes {
		row := rows[i]
		if err := s.catalogLinksExistTx(tx, row); err != nil {
			return nil, err
		}
		var before *models.CatalogEntry
		created := now
		id := w.ID
		if id == "" {
			id = uuid.NewString()
		} else {
			if before, err = getCatalogEntry(q, id); err != nil {
				return nil, err
			}
			created = before.CreatedAt
			for _, table := range []string{"category_catalog_entries", "asset_catalog_entries"} {
				if _, err := s.TxExec(tx, "DELETE FROM "+table+" WHERE id = ?", id); err != nil {
					return nil, fmt.Errorf("replacing the catalog entry: %w", err)
				}
			}
		}
		ids[i] = id
		if row.preferred {
			if _, err := s.TxExec(tx, "UPDATE "+row.table+" SET preferred = ? WHERE "+row.linkColumn+" = ? AND preferred = ?", false, row.linkID, true); err != nil {
				return nil, fmt.Errorf("choosing the preferred entry: %w", err)
			}
		}
		if _, err := s.TxExec(tx, "INSERT INTO "+row.table+" (id, vendor_id, "+row.linkColumn+`, part_number, description, url, pack_size, pack_unit,
			price_minor, currency, price_checked, preferred, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, row.vendorID, row.linkID, row.partNumber, row.description, row.url, row.packSize, row.packUnit,
			row.priceMinor, row.currency, row.priceChecked, row.preferred, created, now); err != nil {
			return nil, fmt.Errorf("keeping the catalog entry: %w", err)
		}
		after, err := getCatalogEntry(q, id)
		if err != nil {
			return nil, err
		}
		switch {
		case before == nil:
			changes[after.VendorID] = append(changes[after.VendorID], models.EventChange{What: "catalog", Key: id, To: catalogLabel(after)})
		case before.VendorID != after.VendorID:
			changes[before.VendorID] = append(changes[before.VendorID], models.EventChange{What: "catalog", Key: id, From: catalogLabel(before)})
			changes[after.VendorID] = append(changes[after.VendorID], models.EventChange{What: "catalog", Key: id, To: catalogLabel(after)})
		case catalogChanged(before, after):
			changes[after.VendorID] = append(changes[after.VendorID], models.EventChange{What: "catalog", Key: id, From: catalogLabel(before), To: catalogLabel(after)})
		}
	}
	if err := s.recordCatalogChangesTx(tx, changes); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing: %w", err)
	}
	return ids, nil
}

// catalogLinksExistTx checks that an entry's vendor, and its category or
// asset, exist.
func (s *Store) catalogLinksExistTx(tx *sql.Tx, row *catalogRow) error {
	for _, c := range []struct {
		table, id string
		missing   error
	}{
		{"vendors", row.vendorID, ErrVendorNotFound},
		{map[string]string{"category_id": "categories", "asset_id": "assets"}[row.linkColumn], row.linkID,
			map[string]error{"category_id": ErrCategoryNotFound, "asset_id": ErrAssetNotFound}[row.linkColumn]},
	} {
		var n int
		if err := s.TxQueryRow(tx, "SELECT COUNT(*) FROM "+c.table+" WHERE id = ?", c.id).Scan(&n); err != nil {
			return fmt.Errorf("checking the catalog entry's links: %w", err)
		}
		if n == 0 {
			return fmt.Errorf("%w: %s", c.missing, c.id)
		}
	}
	return nil
}

// DeleteCatalogEntry deletes an entry, and records it in its vendor's
// history.
func (s *Store) DeleteCatalogEntry(id string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	e, err := getCatalogEntry(txQuerier{s, tx}, id)
	if err != nil {
		return err
	}
	for _, table := range []string{"category_catalog_entries", "asset_catalog_entries"} {
		if _, err := s.TxExec(tx, "DELETE FROM "+table+" WHERE id = ?", id); err != nil {
			return fmt.Errorf("deleting the catalog entry: %w", err)
		}
	}
	if err := s.recordCatalogChangesTx(tx, map[string][]models.EventChange{e.VendorID: {{What: "catalog", Key: id, From: catalogLabel(e)}}}); err != nil {
		return err
	}
	return tx.Commit()
}

// recordCatalogChangesTx records changes to vendors' catalogs, an edit of
// each vendor, in vendors' order of ID.
func (s *Store) recordCatalogChangesTx(tx *sql.Tx, changes map[string][]models.EventChange) error {
	vendors := make([]string, 0, len(changes))
	for v := range changes {
		vendors = append(vendors, v)
	}
	sort.Strings(vendors)
	for _, v := range vendors {
		var name string
		if err := s.TxQueryRow(tx, "SELECT name FROM vendors WHERE id = ?", v).Scan(&name); err != nil {
			return fmt.Errorf("reading the vendor: %w", err)
		}
		if err := s.recordVendorEvent(tx, models.EventVendorEdited, v, name, models.EventPayload{Trigger: models.TriggerManual, Changes: changes[v]}); err != nil {
			return err
		}
	}
	return nil
}

// catalogLabel is how history names an entry: its part number, or its
// description when it has none, and what it is for. Its price is left out,
// since history is shown to those who may not see prices.
func catalogLabel(e *models.CatalogEntry) string {
	what := e.PartNumber
	if what == "" {
		what = e.Description
		if r := []rune(what); len(r) > 60 {
			what = string(r[:60]) + "…"
		}
	}
	return what + " (" + e.LinkedName + ")"
}

// catalogChanged reports whether anything kept of an entry changed.
func catalogChanged(a, b *models.CatalogEntry) bool {
	pack := func(p *int) int {
		if p == nil {
			return 0
		}
		return *p
	}
	return a.CategoryID != b.CategoryID || a.AssetID != b.AssetID || a.PartNumber != b.PartNumber || a.Description != b.Description ||
		a.URL != b.URL || pack(a.PackSize) != pack(b.PackSize) || a.PackUnit != b.PackUnit || a.Price != b.Price || a.Currency != b.Currency ||
		a.PriceChecked != b.PriceChecked || a.Preferred != b.Preferred
}
