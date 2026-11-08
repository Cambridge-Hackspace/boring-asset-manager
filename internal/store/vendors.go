package store

import (
	"database/sql"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/textcheck"
	"github.com/google/uuid"
)

// Vendors (docs/vendors.md): the suppliers a space buys from, and how to
// reach them. A vendor's scopes decide who sees it, as a category's decide
// who sees its assets; the API checks them.

const (
	// MaxVendorWebsite is the longest website: VARCHAR(2048).
	MaxVendorWebsite = 2048
	// MaxVendorAddress is the longest address.
	MaxVendorAddress = 1000
	// MaxVendorNotes is the longest notes, in Markdown.
	MaxVendorNotes = 20000
	// MaxVendorContacts is how many contacts a vendor may have.
	MaxVendorContacts = 50
	// MaxContactEmail and MaxContactPhone are the longest email and phone:
	// VARCHAR(320) and VARCHAR(64).
	MaxContactEmail = 320
	MaxContactPhone = 64
)

var (
	ErrVendorNotFound = errors.New("vendor not found")
	// ErrVendorWebsite refuses a website that is not a web address, so
	// that a link to it can only open a page.
	ErrVendorWebsite = errors.New("a vendor's website must be an http:// or https:// address")
)

// VendorInput is everything a caller sets on a vendor. Contacts replace the
// vendor's: one naming the ID of one it has keeps that ID, and the rest are
// new. ScopeIDs replace its scopes.
type VendorInput struct {
	Name          string
	Website       string
	AccountNumber string
	Address       string
	Notes         string
	Contacts      []models.VendorContact
	ScopeIDs      []string
}

// clean trims a vendor's text and checks it can be stored.
func (in *VendorInput) clean() error {
	in.Name = strings.TrimSpace(in.Name)
	in.Website = strings.TrimSpace(in.Website)
	in.AccountNumber = strings.TrimSpace(in.AccountNumber)
	in.Address = strings.TrimSpace(in.Address)
	if in.Name == "" {
		return errors.New("vendor name cannot be empty")
	}
	for _, c := range []struct {
		what, value string
		max         int
	}{
		{"vendor name", in.Name, textcheck.MaxName},
		{"website", in.Website, MaxVendorWebsite},
		{"account number", in.AccountNumber, textcheck.MaxName},
		{"address", in.Address, MaxVendorAddress},
		{"notes", in.Notes, MaxVendorNotes},
	} {
		if err := textcheck.Check(c.what, c.value, c.max); err != nil {
			return err
		}
	}
	if in.Website != "" {
		u, err := url.Parse(in.Website)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return ErrVendorWebsite
		}
	}
	if len(in.Contacts) > MaxVendorContacts {
		return fmt.Errorf("a vendor can have at most %d contacts", MaxVendorContacts)
	}
	for i := range in.Contacts {
		c := &in.Contacts[i]
		c.Name, c.Role, c.Email, c.Phone = strings.TrimSpace(c.Name), strings.TrimSpace(c.Role), strings.TrimSpace(c.Email), strings.TrimSpace(c.Phone)
		if c.Name == "" && c.Email == "" && c.Phone == "" {
			return errors.New("a contact needs a name, an email or a phone")
		}
		for _, f := range []struct {
			what, value string
			max         int
		}{
			{"contact name", c.Name, textcheck.MaxName},
			{"contact role", c.Role, textcheck.MaxName},
			{"contact email", c.Email, MaxContactEmail},
			{"contact phone", c.Phone, MaxContactPhone},
		} {
			if err := textcheck.Check(f.what, f.value, f.max); err != nil {
				return err
			}
		}
		if c.Email != "" {
			if a, err := mail.ParseAddress(c.Email); err != nil || a.Address != c.Email {
				return fmt.Errorf("contact email %q is not an email address", c.Email)
			}
		}
	}
	return nil
}

// vendorColumns are a vendor's own columns, in scanVendor's order.
const vendorColumns = "id, name, website, account_number, address, notes, created_at, updated_at"

func scanVendor(row interface{ Scan(...any) error }) (models.Vendor, error) {
	var v models.Vendor
	err := row.Scan(&v.ID, &v.Name, &v.Website, &v.AccountNumber, &v.Address, &v.Notes, &v.CreatedAt, &v.UpdatedAt)
	v.Contacts, v.ScopeIDs = []models.VendorContact{}, []string{}
	return v, err
}

// vendorQuerier reads within a transaction (txQuerier) or not
// (storeQuerier).
type vendorQuerier interface {
	Query(string, ...any) (*sql.Rows, error)
}

// readVendors reads vendors, with their contacts and scopes, by name. where
// narrows them, with args.
func readVendors(q vendorQuerier, where string, args ...any) ([]models.Vendor, error) {
	rows, err := q.Query("SELECT "+vendorColumns+" FROM vendors"+where+" ORDER BY name, id", args...)
	if err != nil {
		return nil, fmt.Errorf("reading vendors: %w", err)
	}
	vendors := []models.Vendor{}
	byID := map[string]int{}
	for rows.Next() {
		v, err := scanVendor(rows)
		if err != nil {
			rows.Close()
			return nil, fmt.Errorf("scanning vendor: %w", err)
		}
		byID[v.ID] = len(vendors)
		vendors = append(vendors, v)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if len(vendors) == 0 {
		return vendors, nil
	}
	rows, err = q.Query("SELECT vendor_id, id, name, role, email, phone FROM vendor_contacts ORDER BY vendor_id, position")
	if err != nil {
		return nil, fmt.Errorf("reading vendor contacts: %w", err)
	}
	for rows.Next() {
		var vendor string
		var c models.VendorContact
		if err := rows.Scan(&vendor, &c.ID, &c.Name, &c.Role, &c.Email, &c.Phone); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scanning vendor contact: %w", err)
		}
		if i, ok := byID[vendor]; ok {
			vendors[i].Contacts = append(vendors[i].Contacts, c)
		}
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	rows, err = q.Query("SELECT vendor_id, scope_id FROM vendor_scopes ORDER BY vendor_id, scope_id")
	if err != nil {
		return nil, fmt.Errorf("reading vendor scopes: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var vendor, scope string
		if err := rows.Scan(&vendor, &scope); err != nil {
			return nil, fmt.Errorf("scanning vendor scope: %w", err)
		}
		if i, ok := byID[vendor]; ok {
			vendors[i].ScopeIDs = append(vendors[i].ScopeIDs, scope)
		}
	}
	return vendors, rows.Err()
}

// ListVendors returns every vendor, by name, with its contacts and scopes.
func (s *Store) ListVendors() ([]models.Vendor, error) {
	return readVendors(storeQuerier{s}, "")
}

// GetVendor returns a vendor, or ErrVendorNotFound.
func (s *Store) GetVendor(id string) (*models.Vendor, error) {
	return getVendor(storeQuerier{s}, id)
}

func getVendor(q vendorQuerier, id string) (*models.Vendor, error) {
	vs, err := readVendors(q, " WHERE id = ?", id)
	if err != nil {
		return nil, err
	}
	if len(vs) == 0 {
		return nil, ErrVendorNotFound
	}
	return &vs[0], nil
}

// ScopeNames maps every scope's ID to its name.
func (s *Store) ScopeNames() (map[string]string, error) {
	rows, err := s.Query("SELECT id, name FROM scopes")
	if err != nil {
		return nil, fmt.Errorf("reading scopes: %w", err)
	}
	defer rows.Close()
	names := map[string]string{}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, fmt.Errorf("scanning scope: %w", err)
		}
		names[id] = name
	}
	return names, rows.Err()
}

// vendorScopesOK checks that scope IDs name scopes a vendor may be in:
// any but default, which is where assets in no other scope are. It returns
// them without repeats, sorted.
func (s *Store) vendorScopesOK(tx *sql.Tx, ids []string) ([]string, error) {
	seen := map[string]bool{}
	out := []string{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		var name string
		err := s.TxQueryRow(tx, "SELECT name FROM scopes WHERE id = ?", id).Scan(&name)
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("%w: %s", ErrScopeNotFound, id)
		}
		if err != nil {
			return nil, fmt.Errorf("checking scope: %w", err)
		}
		if name == rbac.DefaultScope {
			return nil, ErrScopeReserved
		}
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}

// CreateVendor makes a vendor, with its contacts and scopes, and records it
// in its history.
func (s *Store) CreateVendor(in VendorInput) (*models.Vendor, error) {
	if err := in.clean(); err != nil {
		return nil, err
	}
	id := uuid.NewString()
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	scopes, err := s.vendorScopesOK(tx, in.ScopeIDs)
	if err != nil {
		return nil, err
	}
	now := dbTime(time.Now())
	if _, err := s.TxExec(tx, "INSERT INTO vendors ("+vendorColumns+") VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		id, in.Name, in.Website, in.AccountNumber, in.Address, in.Notes, now, now); err != nil {
		return nil, fmt.Errorf("inserting vendor: %w", err)
	}
	if err := s.writeVendorListsTx(tx, id, in.Contacts, nil, scopes); err != nil {
		return nil, err
	}
	after, err := getVendor(txQuerier{s, tx}, id)
	if err != nil {
		return nil, err
	}
	if err := s.recordVendorEventTx(tx, models.EventVendorCreated, after, diffVendors(nil, after, nil)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing: %w", err)
	}
	return s.GetVendor(id)
}

// UpdateVendor replaces a vendor's details, contacts and scopes, and
// records what changed in its history.
func (s *Store) UpdateVendor(id string, in VendorInput) (*models.Vendor, error) {
	if err := in.clean(); err != nil {
		return nil, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	// Lock it, so that two changes at once record what each changed.
	res, err := s.TxExec(tx, "UPDATE vendors SET updated_at = updated_at WHERE id = ?", id)
	if err != nil {
		return nil, fmt.Errorf("locking vendor: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// MariaDB counts rows changed, not matched.
		if _, err := getVendor(txQuerier{s, tx}, id); err != nil {
			return nil, err
		}
	}
	before, err := getVendor(txQuerier{s, tx}, id)
	if err != nil {
		return nil, err
	}
	scopes, err := s.vendorScopesOK(tx, in.ScopeIDs)
	if err != nil {
		return nil, err
	}
	if _, err := s.TxExec(tx, "UPDATE vendors SET name = ?, website = ?, account_number = ?, address = ?, notes = ?, updated_at = ? WHERE id = ?",
		in.Name, in.Website, in.AccountNumber, in.Address, in.Notes, dbTime(time.Now()), id); err != nil {
		return nil, fmt.Errorf("updating vendor: %w", err)
	}
	if err := s.writeVendorListsTx(tx, id, in.Contacts, before.Contacts, scopes); err != nil {
		return nil, err
	}
	after, err := getVendor(txQuerier{s, tx}, id)
	if err != nil {
		return nil, err
	}
	names, err := scopeNamesTx(s, tx)
	if err != nil {
		return nil, err
	}
	if changes := diffVendors(before, after, names); len(changes) > 0 {
		if err := s.recordVendorEventTx(tx, models.EventVendorEdited, after, changes); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing: %w", err)
	}
	return s.GetVendor(id)
}

// scopeNamesTx maps scopes' IDs to their names, within tx.
func scopeNamesTx(s *Store, tx *sql.Tx) (map[string]string, error) {
	rows, err := s.TxQuery(tx, "SELECT id, name FROM scopes")
	if err != nil {
		return nil, fmt.Errorf("reading scopes: %w", err)
	}
	defer rows.Close()
	names := map[string]string{}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		names[id] = name
	}
	return names, rows.Err()
}

// writeVendorListsTx replaces a vendor's contacts and scopes. A contact
// naming the ID of one in had keeps it.
func (s *Store) writeVendorListsTx(tx *sql.Tx, id string, contacts, had []models.VendorContact, scopes []string) error {
	kept := map[string]bool{}
	for _, c := range had {
		kept[c.ID] = true
	}
	if _, err := s.TxExec(tx, "DELETE FROM vendor_contacts WHERE vendor_id = ?", id); err != nil {
		return fmt.Errorf("clearing vendor contacts: %w", err)
	}
	used := map[string]bool{}
	for i, c := range contacts {
		cid := c.ID
		if !kept[cid] || used[cid] {
			cid = uuid.NewString()
		}
		used[cid] = true
		if _, err := s.TxExec(tx, "INSERT INTO vendor_contacts (id, vendor_id, position, name, role, email, phone) VALUES (?, ?, ?, ?, ?, ?, ?)",
			cid, id, i, c.Name, c.Role, c.Email, c.Phone); err != nil {
			return fmt.Errorf("inserting vendor contact: %w", err)
		}
	}
	if _, err := s.TxExec(tx, "DELETE FROM vendor_scopes WHERE vendor_id = ?", id); err != nil {
		return fmt.Errorf("clearing vendor scopes: %w", err)
	}
	for _, sc := range scopes {
		if _, err := s.TxExec(tx, "INSERT INTO vendor_scopes (vendor_id, scope_id) VALUES (?, ?)", id, sc); err != nil {
			return fmt.Errorf("putting vendor in scope: %w", err)
		}
	}
	return nil
}

// DeleteVendor deletes a vendor, with its contacts and files' rows;
// categories stop naming it. Its files stay until bam storage sweep, as
// other attachments' do. A vendor that contracts or purchase orders are
// with is not deleted: that would delete them too.
func (s *Store) DeleteVendor(id string) error {
	if has, err := s.vendorHasContracts(id); err != nil {
		return err
	} else if has {
		return ErrVendorHasContracts
	}
	if has, err := s.vendorHasOrders(id); err != nil {
		return err
	} else if has {
		return ErrVendorHasOrders
	}
	res, err := s.Exec("DELETE FROM vendors WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("deleting vendor: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrVendorNotFound
	}
	return nil
}

// diffVendors lists what changed from before to after, nil before being a
// vendor just made: its details, then its contacts by name, then its
// scopes by name, as names gives them.
func diffVendors(before, after *models.Vendor, names map[string]string) []models.EventChange {
	if before == nil {
		before = &models.Vendor{}
	}
	var changes []models.EventChange
	for _, f := range []struct{ what, from, to string }{
		{"name", before.Name, after.Name},
		{"website", before.Website, after.Website},
		{"account_number", before.AccountNumber, after.AccountNumber},
		{"address", before.Address, after.Address},
	} {
		if f.from != f.to {
			changes = append(changes, models.EventChange{What: f.what, From: f.from, To: f.to})
		}
	}
	// Notes may be long: history says they changed, not how.
	if before.Notes != after.Notes {
		changes = append(changes, models.EventChange{What: "notes"})
	}
	was := map[string]models.VendorContact{}
	for _, c := range before.Contacts {
		was[c.ID] = c
	}
	now := map[string]bool{}
	for _, c := range after.Contacts {
		now[c.ID] = true
		old, ok := was[c.ID]
		switch {
		case !ok:
			changes = append(changes, models.EventChange{What: "contact", Key: c.ID, To: contactLabel(c)})
		case old != c:
			changes = append(changes, models.EventChange{What: "contact", Key: c.ID, From: contactLabel(old), To: contactLabel(c)})
		}
	}
	for _, c := range before.Contacts {
		if !now[c.ID] {
			changes = append(changes, models.EventChange{What: "contact", Key: c.ID, From: contactLabel(c)})
		}
	}
	had := map[string]bool{}
	for _, id := range before.ScopeIDs {
		had[id] = true
	}
	has := map[string]bool{}
	for _, id := range after.ScopeIDs {
		has[id] = true
		if !had[id] {
			changes = append(changes, models.EventChange{What: "scope", Key: id, To: nameOr(names, id)})
		}
	}
	for _, id := range before.ScopeIDs {
		if !has[id] {
			changes = append(changes, models.EventChange{What: "scope", Key: id, From: nameOr(names, id)})
		}
	}
	return changes
}

// contactLabel is how history names a contact: by name and role, or by
// how to reach them when they have no name.
func contactLabel(c models.VendorContact) string {
	label := c.Name
	if label == "" {
		label = c.Email
	}
	if label == "" {
		label = c.Phone
	}
	if c.Role != "" {
		label += " (" + c.Role + ")"
	}
	return label
}

func nameOr(names map[string]string, id string) string {
	if n, ok := names[id]; ok {
		return n
	}
	return id
}

// recordVendorEventTx stores an event about a vendor, as it is now, in its
// history.
func (s *Store) recordVendorEventTx(tx *sql.Tx, kind string, v *models.Vendor, changes []models.EventChange) error {
	return s.recordVendorEvent(tx, kind, v.ID, v.Name, models.EventPayload{Trigger: models.TriggerManual, Changes: changes})
}

// recordVendorEvent stores an event about a vendor, by its ID and name.
func (s *Store) recordVendorEvent(tx *sql.Tx, kind, id, name string, payload models.EventPayload) error {
	payload.Vendor = &models.EventVendor{ID: id, Name: name}
	eventID, err := s.insertEventID(tx, kind, nil, nil, payload)
	if err != nil {
		return err
	}
	if _, err := s.TxExec(tx, "UPDATE events SET vendor_id = ? WHERE id = ?", id, eventID); err != nil {
		return fmt.Errorf("recording vendor event: %w", err)
	}
	return nil
}

// ListVendorEvents returns a vendor's events, newest first: up to limit of
// them, starting after the event before when it is not empty. more reports
// whether older events remain.
func (s *Store) ListVendorEvents(id, before string, limit int) (events []models.Event, more bool, err error) {
	query, args := `SELECT `+eventColumns+` FROM events WHERE vendor_id = ?`, []any{id}
	if before != "" {
		var at sql.NullTime
		err := s.QueryRow("SELECT created_at FROM events WHERE id = ? AND vendor_id = ?", before, id).Scan(&at)
		if err == sql.ErrNoRows {
			return nil, false, ErrEventNotFound
		}
		if err != nil {
			return nil, false, fmt.Errorf("finding the page's first event: %w", err)
		}
		query += " AND (created_at < ? OR (created_at = ? AND id < ?))"
		args = append(args, at.Time, at.Time, before)
	}
	rows, err := s.Query(fmt.Sprintf(query+" ORDER BY created_at DESC, id DESC LIMIT %d", limit+1), args...)
	if err != nil {
		return nil, false, fmt.Errorf("listing vendor events: %w", err)
	}
	defer rows.Close()
	events = []models.Event{}
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, false, err
		}
		events = append(events, e)
	}
	if len(events) > limit {
		return events[:limit], true, rows.Err()
	}
	return events, false, rows.Err()
}

// SetCategoryVendors replaces the vendors a category is bought from, in
// order of preference.
func (s *Store) SetCategoryVendors(categoryID string, vendorIDs []string) (*models.Category, error) {
	if _, err := s.GetCategoryByID(categoryID); err != nil {
		return nil, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := s.TxExec(tx, "DELETE FROM category_vendors WHERE category_id = ?", categoryID); err != nil {
		return nil, fmt.Errorf("clearing category vendors: %w", err)
	}
	seen := map[string]bool{}
	for _, id := range vendorIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		var n int
		if err := s.TxQueryRow(tx, "SELECT COUNT(*) FROM vendors WHERE id = ?", id).Scan(&n); err != nil {
			return nil, fmt.Errorf("checking vendor: %w", err)
		}
		if n == 0 {
			return nil, fmt.Errorf("%w: %s", ErrVendorNotFound, id)
		}
		if _, err := s.TxExec(tx, "INSERT INTO category_vendors (category_id, vendor_id, sort_order) VALUES (?, ?, ?)", categoryID, id, len(seen)-1); err != nil {
			return nil, fmt.Errorf("naming category vendor: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing: %w", err)
	}
	cat, err := s.GetCategoryByID(categoryID)
	if err != nil {
		return nil, err
	}
	list := []models.Category{*cat}
	if err := s.WithCategoryVendors(list); err != nil {
		return nil, err
	}
	return &list[0], nil
}

// CategoryVendorIDs are the vendors a category is bought from, in order of
// preference.
func (s *Store) CategoryVendorIDs(categoryID string) ([]string, error) {
	list := []models.Category{{ID: categoryID}}
	if err := s.WithCategoryVendors(list); err != nil {
		return nil, err
	}
	return list[0].VendorIDs, nil
}

// WithCategoryVendors sets the vendors categories are bought from. Reading
// categories leaves them out, so that categories read the same on a schema
// from before vendors, as migrations and their tests read them; the API
// sets them.
func (s *Store) WithCategoryVendors(categories []models.Category) error {
	lists, err := categoryVendorLists(storeQuerier{s})
	if err != nil {
		return err
	}
	for i := range categories {
		categories[i].VendorIDs = lists[categories[i].ID]
		if categories[i].VendorIDs == nil {
			categories[i].VendorIDs = []string{}
		}
	}
	return nil
}

// categoryVendorLists maps each category naming vendors to them, in order
// of preference.
func categoryVendorLists(q interface {
	Query(string, ...any) (*sql.Rows, error)
}) (map[string][]string, error) {
	rows, err := q.Query(`SELECT category_id, vendor_id FROM category_vendors ORDER BY category_id, sort_order`)
	if err != nil {
		return nil, fmt.Errorf("reading category vendors: %w", err)
	}
	defer rows.Close()
	lists := map[string][]string{}
	for rows.Next() {
		var category, vendor string
		if err := rows.Scan(&category, &vendor); err != nil {
			return nil, fmt.Errorf("scanning a category vendor: %w", err)
		}
		lists[category] = append(lists[category], vendor)
	}
	return lists, rows.Err()
}
