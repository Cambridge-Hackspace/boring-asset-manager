package store

import (
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/money"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/textcheck"
	"github.com/google/uuid"
)

// Contracts (docs/contracts.md): warranties, service and maintenance
// agreements, leases and licenses, each with a vendor, whose scopes decide
// who sees it; the API checks them. A contract covers the assets it names,
// and every asset in the categories it names or below them.

const (
	// MaxContractNotes is the longest notes, in Markdown.
	MaxContractNotes = 20000
	// MaxNoticeDays is the longest notice period, in days: ten years.
	MaxNoticeDays = 3650
	// MaxContractCoverage is how many assets, and how many categories, a
	// contract may name.
	MaxContractCoverage = 5000
	// DefaultContractReminderDays is how many days before a contract ends,
	// or before its notice deadline, its contract.expiring event is
	// recorded, unless the configuration says otherwise.
	DefaultContractReminderDays = 30
)

// Contract notices, by what they say (contract_notices.kind).
const (
	noticeExpiring = "expiring"
	noticeExpired  = "expired"
)

var (
	ErrContractNotFound = errors.New("contract not found")
	// ErrInvalidContract is wrapped by every problem with a contract's
	// details.
	ErrInvalidContract = errors.New("invalid contract")
	// ErrVendorHasContracts refuses deleting a vendor that contracts are
	// with, which would delete them too.
	ErrVendorHasContracts = errors.New("the vendor has contracts; delete them, or move them to another vendor, first")
)

// ContractInput is everything a caller sets on a contract. Dates are
// YYYY-MM-DD; Cost is a decimal in Currency, which it needs. AssetIDs and
// CategoryIDs replace what it covers.
type ContractInput struct {
	VendorID    string
	Kind        string
	Name        string
	Reference   string
	StartDate   string
	EndDate     string
	AutoRenews  bool
	NoticeDays  int
	Cost        string
	Currency    string
	Billing     string
	Notes       string
	AssetIDs    []string
	CategoryIDs []string
}

// ContractInputOf is the input that keeps a contract as it is.
func ContractInputOf(c models.Contract) ContractInput {
	return ContractInput{VendorID: c.VendorID, Kind: c.Kind, Name: c.Name, Reference: c.Reference, StartDate: c.StartDate, EndDate: c.EndDate,
		AutoRenews: c.AutoRenews, NoticeDays: c.NoticeDays, Cost: c.Cost, Currency: c.Currency, Billing: c.Billing, Notes: c.Notes,
		AssetIDs: slices.Clone(c.AssetIDs), CategoryIDs: slices.Clone(c.CategoryIDs)}
}

// contractRow is what a contract keeps that differs from its input.
type contractRow struct {
	startDate, endDate *string
	costMinor          *int64
	currency           string
}

func invalidContract(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrInvalidContract}, args...)...)
}

// contractDate reads a date written YYYY-MM-DD, trimmed; empty stays
// empty.
func contractDate(what, s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	t, err := time.Parse(DateLayout, s)
	if err != nil || t.Year() < 1000 {
		return "", invalidContract("%s must be a date written YYYY-MM-DD", what)
	}
	return t.Format(DateLayout), nil
}

// dedupe keeps the first of each ID, trimmed, leaving out empty ones.
func dedupe(ids []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// clean trims a contract's details and checks them, giving what is kept.
func (in *ContractInput) clean() (*contractRow, error) {
	in.VendorID = strings.TrimSpace(in.VendorID)
	in.Kind = strings.ToLower(strings.TrimSpace(in.Kind))
	in.Name = strings.TrimSpace(in.Name)
	in.Reference = strings.TrimSpace(in.Reference)
	in.Billing = strings.ToLower(strings.TrimSpace(in.Billing))
	if in.Name == "" {
		return nil, invalidContract("a contract needs a name")
	}
	if in.VendorID == "" {
		return nil, invalidContract("a contract needs a vendor")
	}
	if !slices.Contains(models.ContractKinds, in.Kind) {
		return nil, invalidContract("its kind must be one of %s", strings.Join(models.ContractKinds, ", "))
	}
	for _, c := range []struct {
		what, value string
		max         int
	}{
		{"the contract's name", in.Name, textcheck.MaxName},
		{"the reference", in.Reference, textcheck.MaxName},
		{"the notes", in.Notes, MaxContractNotes},
	} {
		if err := textcheck.Check(c.what, c.value, c.max); err != nil {
			return nil, invalidContract("%v", err)
		}
	}
	var row contractRow
	start, err := contractDate("the start date", in.StartDate)
	if err != nil {
		return nil, err
	}
	end, err := contractDate("the end date", in.EndDate)
	if err != nil {
		return nil, err
	}
	if start != "" && end != "" && end < start {
		return nil, invalidContract("it cannot end before it starts")
	}
	in.StartDate, in.EndDate = start, end
	row.startDate, row.endDate = nonEmpty(start), nonEmpty(end)
	if in.NoticeDays < 0 || in.NoticeDays > MaxNoticeDays {
		return nil, invalidContract("its notice period must be from 0 to %d days", MaxNoticeDays)
	}
	if in.AutoRenews && end == "" {
		return nil, invalidContract("a contract that renews automatically needs an end date, when it renews")
	}
	if row.currency, err = money.NormalizeCurrency(in.Currency); err != nil {
		return nil, invalidContract("%v", err)
	}
	if cost := strings.TrimSpace(in.Cost); cost != "" {
		if row.currency == "" {
			return nil, invalidContract("a cost needs its currency")
		}
		minor, err := money.Parse(cost, row.currency)
		if err != nil {
			return nil, invalidContract("the cost: %v", err)
		}
		row.costMinor = &minor
	}
	if !slices.Contains(models.Billings, in.Billing) {
		return nil, invalidContract("how often it is paid must be once, monthly, quarterly, yearly, or not said")
	}
	in.AssetIDs, in.CategoryIDs = dedupe(in.AssetIDs), dedupe(in.CategoryIDs)
	if len(in.AssetIDs) > MaxContractCoverage || len(in.CategoryIDs) > MaxContractCoverage {
		return nil, invalidContract("a contract can name at most %d assets and %d categories", MaxContractCoverage, MaxContractCoverage)
	}
	return &row, nil
}

// NoticeDeadline is when notice must be given to stop a contract renewing
// automatically: notice days before it ends. It is empty for one that does
// not renew.
func NoticeDeadline(end string, autoRenews bool, noticeDays int) string {
	if !autoRenews || end == "" {
		return ""
	}
	t, err := time.Parse(DateLayout, end)
	if err != nil {
		return ""
	}
	return t.AddDate(0, 0, -noticeDays).Format(DateLayout)
}

// reminderDate is the date a contract's contract.expiring warns of: its
// notice deadline when it renews automatically, its end otherwise.
func reminderDate(end string, autoRenews bool, noticeDays int) string {
	if d := NoticeDeadline(end, autoRenews, noticeDays); d != "" {
		return d
	}
	return end
}

const contractSelect = `SELECT c.id, c.vendor_id, v.name, c.kind, c.name, c.reference, c.start_date, c.end_date, c.auto_renews, c.notice_days,
	c.cost_minor, c.currency, c.billing, c.notes, c.created_at, c.updated_at
	FROM contracts c JOIN vendors v ON v.id = c.vendor_id`

// readContracts reads contracts, with what they cover, by when they end,
// those with no end last. where narrows them, with args, naming the
// contracts table c.
func readContracts(q vendorQuerier, where string, args ...any) ([]models.Contract, error) {
	rows, err := q.Query(contractSelect+where, args...)
	if err != nil {
		return nil, fmt.Errorf("reading contracts: %w", err)
	}
	out := []models.Contract{}
	byID := map[string]int{}
	for rows.Next() {
		var (
			c          models.Contract
			start, end sql.NullString
			minor      sql.NullInt64
		)
		if err := rows.Scan(&c.ID, &c.VendorID, &c.VendorName, &c.Kind, &c.Name, &c.Reference, &start, &end, &c.AutoRenews, &c.NoticeDays,
			&minor, &c.Currency, &c.Billing, &c.Notes, &c.CreatedAt, &c.UpdatedAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scanning contract: %w", err)
		}
		c.StartDate, c.EndDate = start.String, end.String
		c.NoticeDeadline = NoticeDeadline(c.EndDate, c.AutoRenews, c.NoticeDays)
		if minor.Valid {
			c.Cost = money.Format(minor.Int64, c.Currency)
		}
		c.AssetIDs, c.CategoryIDs, c.Assets, c.Categories = []string{}, []string{}, []models.NamedThing{}, []models.NamedThing{}
		byID[c.ID] = len(out)
		out = append(out, c)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return out, nil
	}
	// What they cover, by name: only those of the contracts read.
	within := " WHERE x.contract_id IN (SELECT c.id FROM contracts c" + where + ")"
	for _, cov := range []struct {
		query string
		add   func(*models.Contract, models.NamedThing)
	}{
		{"SELECT x.contract_id, a.id, a.name FROM contract_assets x JOIN assets a ON a.id = x.asset_id" + within + " ORDER BY a.name, a.id",
			func(c *models.Contract, n models.NamedThing) {
				c.AssetIDs, c.Assets = append(c.AssetIDs, n.ID), append(c.Assets, n)
			}},
		{"SELECT x.contract_id, k.id, k.name FROM contract_categories x JOIN categories k ON k.id = x.category_id" + within + " ORDER BY k.name, k.id",
			func(c *models.Contract, n models.NamedThing) {
				c.CategoryIDs, c.Categories = append(c.CategoryIDs, n.ID), append(c.Categories, n)
			}},
	} {
		rows, err := q.Query(cov.query, args...)
		if err != nil {
			return nil, fmt.Errorf("reading what contracts cover: %w", err)
		}
		for rows.Next() {
			var contract string
			var n models.NamedThing
			if err := rows.Scan(&contract, &n.ID, &n.Name); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scanning what a contract covers: %w", err)
			}
			if i, ok := byID[contract]; ok {
				cov.add(&out[i], n)
			}
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	SortContracts(out)
	return out, nil
}

// SortContracts orders contracts by when they end, soonest first and those
// with no end last, then by name.
func SortContracts(cs []models.Contract) {
	sort.SliceStable(cs, func(i, j int) bool {
		return endsBefore(cs[i].EndDate, cs[i].Name, cs[i].ID, cs[j].EndDate, cs[j].Name, cs[j].ID)
	})
}

// endsBefore orders by end, soonest first and none last, then by name and
// ID.
func endsBefore(endA, nameA, idA, endB, nameB, idB string) bool {
	if endA != endB {
		switch {
		case endA == "":
			return false
		case endB == "":
			return true
		}
		return endA < endB
	}
	if nameA != nameB {
		return nameA < nameB
	}
	return idA < idB
}

// ListContracts returns every contract, by when it ends, with what it
// covers.
func (s *Store) ListContracts() ([]models.Contract, error) {
	return readContracts(storeQuerier{s}, "")
}

// GetContract returns a contract, or ErrContractNotFound.
func (s *Store) GetContract(id string) (*models.Contract, error) {
	return getContract(storeQuerier{s}, id)
}

func getContract(q vendorQuerier, id string) (*models.Contract, error) {
	cs, err := readContracts(q, " WHERE c.id = ?", id)
	if err != nil {
		return nil, err
	}
	if len(cs) == 0 {
		return nil, ErrContractNotFound
	}
	return &cs[0], nil
}

// writeContractTx writes a contract's own columns, inserting it when
// insert is set, and replaces what it covers. The vendor, the assets and
// the categories must exist.
func (s *Store) writeContractTx(tx *sql.Tx, id string, in ContractInput, row *contractRow, insert bool) error {
	var n int
	if err := s.TxQueryRow(tx, "SELECT COUNT(*) FROM vendors WHERE id = ?", in.VendorID).Scan(&n); err != nil {
		return fmt.Errorf("reading the vendor: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%w: %s", ErrVendorNotFound, in.VendorID)
	}
	now := dbTime(time.Now())
	var err error
	if insert {
		_, err = s.TxExec(tx, `INSERT INTO contracts (id, vendor_id, kind, name, reference, start_date, end_date, auto_renews, notice_days, cost_minor, currency, billing, notes, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, in.VendorID, in.Kind, in.Name, in.Reference, row.startDate, row.endDate, in.AutoRenews, in.NoticeDays, row.costMinor, row.currency, in.Billing, in.Notes, now, now)
	} else {
		_, err = s.TxExec(tx, `UPDATE contracts SET vendor_id = ?, kind = ?, name = ?, reference = ?, start_date = ?, end_date = ?, auto_renews = ?, notice_days = ?,
			cost_minor = ?, currency = ?, billing = ?, notes = ?, updated_at = ? WHERE id = ?`,
			in.VendorID, in.Kind, in.Name, in.Reference, row.startDate, row.endDate, in.AutoRenews, in.NoticeDays, row.costMinor, row.currency, in.Billing, in.Notes, now, id)
	}
	if err != nil {
		return fmt.Errorf("keeping the contract: %w", err)
	}
	for _, cov := range []struct {
		table, column, parent string
		ids                   []string
		missing               error
	}{
		{"contract_assets", "asset_id", "assets", in.AssetIDs, ErrAssetNotFound},
		{"contract_categories", "category_id", "categories", in.CategoryIDs, ErrCategoryNotFound},
	} {
		if _, err := s.TxExec(tx, "DELETE FROM "+cov.table+" WHERE contract_id = ?", id); err != nil {
			return fmt.Errorf("clearing what the contract covers: %w", err)
		}
		for _, other := range cov.ids {
			if err := s.TxQueryRow(tx, "SELECT COUNT(*) FROM "+cov.parent+" WHERE id = ?", other).Scan(&n); err != nil {
				return fmt.Errorf("checking what the contract covers: %w", err)
			}
			if n == 0 {
				return fmt.Errorf("%w: %s", cov.missing, other)
			}
			if _, err := s.TxExec(tx, "INSERT INTO "+cov.table+" (contract_id, "+cov.column+") VALUES (?, ?)", id, other); err != nil {
				return fmt.Errorf("naming what the contract covers: %w", err)
			}
		}
	}
	return nil
}

// CreateContract makes a contract, with what it covers, and records it in
// its history.
func (s *Store) CreateContract(in ContractInput) (*models.Contract, error) {
	row, err := in.clean()
	if err != nil {
		return nil, err
	}
	id := uuid.NewString()
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if err := s.writeContractTx(tx, id, in, row, true); err != nil {
		return nil, err
	}
	after, err := getContract(txQuerier{s, tx}, id)
	if err != nil {
		return nil, err
	}
	if err := s.recordContractEvent(tx, models.EventContractCreated, after, models.EventPayload{Trigger: models.TriggerManual, Changes: diffContracts(nil, after)}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing: %w", err)
	}
	return s.GetContract(id)
}

// UpdateContract replaces a contract's details and what it covers, and
// records what changed in its history.
func (s *Store) UpdateContract(id string, in ContractInput) (*models.Contract, error) {
	row, err := in.clean()
	if err != nil {
		return nil, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	// Lock it, so that two changes at once record what each changed.
	if _, err := s.TxExec(tx, "UPDATE contracts SET updated_at = updated_at WHERE id = ?", id); err != nil {
		return nil, fmt.Errorf("locking contract: %w", err)
	}
	before, err := getContract(txQuerier{s, tx}, id)
	if err != nil {
		return nil, err
	}
	if err := s.writeContractTx(tx, id, in, row, false); err != nil {
		return nil, err
	}
	after, err := getContract(txQuerier{s, tx}, id)
	if err != nil {
		return nil, err
	}
	if changes := diffContracts(before, after); len(changes) > 0 {
		if err := s.recordContractEvent(tx, models.EventContractEdited, after, models.EventPayload{Trigger: models.TriggerManual, Changes: changes}); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing: %w", err)
	}
	return s.GetContract(id)
}

// DeleteContract deletes a contract, with what it covers and its files'
// rows. Its files stay until bam storage sweep, as other attachments' do.
func (s *Store) DeleteContract(id string) error {
	res, err := s.Exec("DELETE FROM contracts WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("deleting contract: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrContractNotFound
	}
	return nil
}

// vendorHasContracts reports whether any contract is with a vendor.
func (s *Store) vendorHasContracts(id string) (bool, error) {
	var n int
	if err := s.QueryRow("SELECT COUNT(*) FROM contracts WHERE vendor_id = ?", id).Scan(&n); err != nil {
		return false, fmt.Errorf("reading the vendor's contracts: %w", err)
	}
	return n > 0, nil
}

// costText is a contract's cost with its currency, as history shows it.
func costText(c *models.Contract) string {
	return strings.TrimSpace(c.Cost + " " + c.Currency)
}

// yesNo is a flag as history shows it.
func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// diffContracts lists what changed from before to after, nil before being
// a contract just made: its details, then what it covers, by name.
func diffContracts(before, after *models.Contract) []models.EventChange {
	// A contract just made differs from none in what it has: one that
	// does not renew, with no notice period, says nothing of either.
	if before == nil {
		before = &models.Contract{}
	}
	var changes []models.EventChange
	if before.VendorID != after.VendorID {
		changes = append(changes, models.EventChange{What: "vendor", Key: after.VendorID, From: before.VendorName, To: after.VendorName})
	}
	for _, f := range []struct{ what, from, to string }{
		{"kind", before.Kind, after.Kind},
		{"name", before.Name, after.Name},
		{"reference", before.Reference, after.Reference},
		{"start_date", before.StartDate, after.StartDate},
		{"end_date", before.EndDate, after.EndDate},
		{"auto_renews", yesNo(before.AutoRenews), yesNo(after.AutoRenews)},
		{"notice_days", strconv.Itoa(before.NoticeDays), strconv.Itoa(after.NoticeDays)},
		{"cost", costText(before), costText(after)},
		{"billing", before.Billing, after.Billing},
	} {
		if f.from != f.to {
			changes = append(changes, models.EventChange{What: f.what, From: f.from, To: f.to})
		}
	}
	// Notes may be long: history says they changed, not how.
	if before.Notes != after.Notes {
		changes = append(changes, models.EventChange{What: "notes"})
	}
	for _, cov := range []struct {
		what     string
		from, to []models.NamedThing
	}{
		{"asset", before.Assets, after.Assets},
		{"category", before.Categories, after.Categories},
	} {
		had := map[string]bool{}
		for _, n := range cov.from {
			had[n.ID] = true
		}
		has := map[string]bool{}
		for _, n := range cov.to {
			has[n.ID] = true
			if !had[n.ID] {
				changes = append(changes, models.EventChange{What: cov.what, Key: n.ID, To: n.Name})
			}
		}
		for _, n := range cov.from {
			if !has[n.ID] {
				changes = append(changes, models.EventChange{What: cov.what, Key: n.ID, From: n.Name})
			}
		}
	}
	return changes
}

// ContractChanges are the kinds of change in a contract's history that
// only purchases.read sees.
var ContractChanges = map[string]bool{"cost": true, "billing": true}

// eventContract is a contract as its events name it.
func eventContract(c *models.Contract) *models.EventContract {
	return &models.EventContract{ID: c.ID, Name: c.Name, Kind: c.Kind, VendorID: c.VendorID, VendorName: c.VendorName,
		EndDate: c.EndDate, AutoRenews: c.AutoRenews, NoticeDays: c.NoticeDays}
}

// recordContractEvent stores an event about a contract, as it is now, in
// its history.
func (s *Store) recordContractEvent(tx *sql.Tx, kind string, c *models.Contract, payload models.EventPayload) error {
	payload.Contract = eventContract(c)
	eventID, err := s.insertEventID(tx, kind, nil, nil, payload)
	if err != nil {
		return err
	}
	if _, err := s.TxExec(tx, "UPDATE events SET contract_id = ? WHERE id = ?", c.ID, eventID); err != nil {
		return fmt.Errorf("recording contract event: %w", err)
	}
	return nil
}

// recordContractEditedTx records changes to a contract's attachments in
// its history.
func (s *Store) recordContractEditedTx(tx *sql.Tx, id string, payload models.EventPayload) error {
	c, err := getContract(txQuerier{s, tx}, id)
	if err != nil {
		return err
	}
	return s.recordContractEvent(tx, models.EventContractEdited, c, payload)
}

// ListContractEvents returns a contract's events, newest first: up to
// limit of them, starting after the event before when it is not empty.
// more reports whether older events remain.
func (s *Store) ListContractEvents(id, before string, limit int) (events []models.Event, more bool, err error) {
	query, args := `SELECT `+eventColumns+` FROM events WHERE contract_id = ?`, []any{id}
	if before != "" {
		var at sql.NullTime
		err := s.QueryRow("SELECT created_at FROM events WHERE id = ? AND contract_id = ?", before, id).Scan(&at)
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
		return nil, false, fmt.Errorf("listing contract events: %w", err)
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

// contractCoverage is every contract, and what each covers, read once for
// the assets an assetContext serves.
type contractCoverage struct {
	contracts  []models.Contract
	byAsset    map[string][]int
	byCategory map[string][]int
}

func (s *Store) readCoverage() (*contractCoverage, error) {
	all, err := readContracts(storeQuerier{s}, "")
	if err != nil {
		return nil, err
	}
	cov := &contractCoverage{contracts: all, byAsset: map[string][]int{}, byCategory: map[string][]int{}}
	for i, c := range all {
		for _, a := range c.AssetIDs {
			cov.byAsset[a] = append(cov.byAsset[a], i)
		}
		for _, k := range c.CategoryIDs {
			cov.byCategory[k] = append(cov.byCategory[k], i)
		}
	}
	return cov, nil
}

// coverOf is a contract as an asset it covers shows it.
func coverOf(c models.Contract, categoryID string) models.ContractCover {
	return models.ContractCover{ID: c.ID, Name: c.Name, Kind: c.Kind, VendorID: c.VendorID, VendorName: c.VendorName, Reference: c.Reference,
		StartDate: c.StartDate, EndDate: c.EndDate, AutoRenews: c.AutoRenews, CategoryID: categoryID, Cost: c.Cost, Currency: c.Currency, Billing: c.Billing}
}

// covers lists the contracts covering an asset in categories catIDs, whose
// parents parents gives: those naming it, then those naming one of its
// categories or one above, each once, by when they end.
func (cov *contractCoverage) covers(assetID string, catIDs []string, parents map[string]string) []models.ContractCover {
	out := []models.ContractCover{}
	if len(cov.contracts) == 0 {
		return out
	}
	seen := map[int]bool{}
	for _, i := range cov.byAsset[assetID] {
		seen[i] = true
		out = append(out, coverOf(cov.contracts[i], ""))
	}
	for _, cat := range catIDs {
		// Up from each category, guarding against a loop.
		for at, steps := cat, 0; at != "" && steps < 1000; at, steps = parents[at], steps+1 {
			for _, i := range cov.byCategory[at] {
				if !seen[i] {
					seen[i] = true
					out = append(out, coverOf(cov.contracts[i], at))
				}
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return endsBefore(out[i].EndDate, out[i].Name, out[i].ID, out[j].EndDate, out[j].Name, out[j].ID)
	})
	return out
}

// contractDetails fills in the contracts covering an asset, reading every
// contract once for the context.
func (s *Store) contractDetails(a *models.AssetWithDetails, ctx *assetContext, catIDs []string, parents map[string]string) error {
	if ctx.contracts == nil {
		cov, err := s.readCoverage()
		if err != nil {
			return err
		}
		ctx.contracts = cov
	}
	a.Contracts = ctx.contracts.covers(a.Asset.ID, catIDs, parents)
	return nil
}

// RecordContractReminders records, for each contract with an end:
// contract.expiring once, from days before the date it warns of (its
// notice deadline when it renews automatically, its end otherwise) up to
// that date; and contract.expired once, on the day it ends. Each is
// claimed by inserting it into contract_notices, so servers sharing the
// database record it once, and new dates are new notices. A date already
// past when it is first seen is not announced. It returns how many it
// recorded.
func (s *Store) RecordContractReminders(now time.Time, days int) (int, error) {
	if days < 0 {
		days = 0
	}
	today := now.UTC().Format(DateLayout)
	rows, err := s.Query("SELECT id, end_date, auto_renews, notice_days FROM contracts WHERE end_date IS NOT NULL ORDER BY end_date, id")
	if err != nil {
		return 0, fmt.Errorf("finding contracts ending: %w", err)
	}
	type reminder struct{ contract, kind, date string }
	var due []reminder
	for rows.Next() {
		var (
			id, end    string
			renews     bool
			noticeDays int
		)
		if err := rows.Scan(&id, &end, &renews, &noticeDays); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scanning a contract ending: %w", err)
		}
		warn := reminderDate(end, renews, noticeDays)
		at, err := time.Parse(DateLayout, warn)
		if err != nil {
			continue
		}
		if from := at.AddDate(0, 0, -days).Format(DateLayout); today >= from && today <= warn {
			due = append(due, reminder{id, noticeExpiring, warn})
		}
		if end == today {
			due = append(due, reminder{id, noticeExpired, end})
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	recorded := 0
	for _, n := range due {
		ok, err := s.recordContractNotice(n.contract, n.kind, n.date, now)
		if err != nil {
			return recorded, err
		}
		if ok {
			recorded++
		}
	}
	return recorded, nil
}

// recordContractNotice claims and records one contract's reminder,
// answering false when another server claimed it first.
func (s *Store) recordContractNotice(id, kind, date string, now time.Time) (bool, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return false, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := s.TxExec(tx, "INSERT INTO contract_notices (contract_id, kind, due_date, sent_at) VALUES (?, ?, ?, ?)", id, kind, date, dbTime(now)); err != nil {
		if IsUniqueViolation(err) {
			return false, nil
		}
		return false, fmt.Errorf("claiming the contract notice: %w", err)
	}
	c, err := getContract(txQuerier{s, tx}, id)
	if err != nil {
		return false, err
	}
	at, err := time.Parse(DateLayout, date)
	if err != nil {
		return false, fmt.Errorf("reading the contract's date: %w", err)
	}
	event := models.EventContractExpiring
	if kind == noticeExpired {
		event = models.EventContractExpired
	}
	if err := s.recordContractEvent(tx, event, c, models.EventPayload{Trigger: models.TriggerSchedule, DueAt: &at}); err != nil {
		return false, err
	}
	return true, tx.Commit()
}
