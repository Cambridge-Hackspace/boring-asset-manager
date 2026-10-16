package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/google/uuid"
)

var (
	ErrNotLendable = errors.New("the asset is in no lendable category")
	// ErrLentByQuantity refuses lending part of a pooled quantity.
	ErrLentByQuantity   = errors.New("an asset counted by quantity cannot be lent; label each item to lend it")
	ErrOutOfService     = errors.New("the asset is out of service")
	ErrAlreadyLent      = errors.New("the asset is already lent out")
	ErrNotLent          = errors.New("the asset is not lent out")
	ErrNotYourLoan      = errors.New("the asset is lent to someone else")
	ErrAssetOnLoan      = errors.New("the asset is lent out; check it in first")
	ErrBorrowerNotFound = errors.New("the borrower must be an active user")
	ErrDueInPast        = errors.New("a loan cannot be due before it starts")
	// ErrInvalidLoanFilter is returned for a loan state that is not one of
	// the Loans constants.
	ErrInvalidLoanFilter = errors.New("loan must be open or overdue")
)

// LoanFilter chooses which loans a list holds.
type LoanFilter struct {
	// BorrowerID limits the list to one person's loans.
	BorrowerID string
	// State is LoansOpen, LoansOverdue or LoansAll (the default).
	State string
}

// Loan states, for LoanFilter.State and AssetFilter.Loan.
const (
	LoansAll     = ""
	LoansOpen    = "open"
	LoansOverdue = "overdue"
)

// loanColumns are the columns scanLoan reads, from asset_loans l, assets a
// and users u.
const loanColumns = `l.id, l.asset_id, a.name, u.id, u.email, u.name, u.archived_at,
	l.checked_out_at, l.due_at, l.checked_in_at`

const loanJoins = ` FROM asset_loans l JOIN assets a ON a.id = l.asset_id JOIN users u ON u.id = l.borrower_id`

func scanLoan(row interface{ Scan(...any) error }, now time.Time) (models.Loan, error) {
	var (
		l        models.Loan
		archived sql.NullTime
	)
	err := row.Scan(&l.ID, &l.AssetID, &l.AssetName, &l.Borrower.ID, &l.Borrower.Email, &l.Borrower.Name, &archived,
		&l.CheckedOutAt, &l.DueAt, &l.CheckedInAt)
	l.Borrower.Archived = archived.Valid
	l.Overdue = l.CheckedInAt == nil && l.DueAt != nil && l.DueAt.Before(now)
	return l, err
}

// lendableCategories returns the categories whose assets may be lent: the
// lendable ones and every category below one.
func (s *Store) lendableCategories(q interface {
	Query(string, ...any) (*sql.Rows, error)
}) (map[string]bool, error) {
	rows, err := q.Query("SELECT id, parent_id, is_lendable FROM categories")
	if err != nil {
		return nil, fmt.Errorf("reading categories: %w", err)
	}
	defer rows.Close()
	parents, own := map[string]string{}, map[string]bool{}
	for rows.Next() {
		var (
			id       string
			parent   sql.NullString
			lendable bool
		)
		if err := rows.Scan(&id, &parent, &lendable); err != nil {
			return nil, fmt.Errorf("scanning category: %w", err)
		}
		parents[id], own[id] = parent.String, lendable
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for id := range own {
		for c, depth := id, 0; c != "" && depth < 100; c, depth = parents[c], depth+1 {
			if own[c] {
				out[id] = true
				break
			}
		}
	}
	return out, nil
}

// storeQuerier runs queries outside a transaction.
type storeQuerier struct{ s *Store }

func (q storeQuerier) Query(query string, args ...any) (*sql.Rows, error) {
	return q.s.Query(query, args...)
}

// txQuerier runs queries in a transaction.
type txQuerier struct {
	s  *Store
	tx *sql.Tx
}

func (q txQuerier) Query(query string, args ...any) (*sql.Rows, error) {
	return q.s.TxQuery(q.tx, query, args...)
}

// loanDetails fills in whether an asset may be lent and its open loan.
func (s *Store) loanDetails(a *models.AssetWithDetails) error {
	lendable, err := s.lendableCategories(storeQuerier{s})
	if err != nil {
		return err
	}
	for _, c := range a.Categories {
		a.Lendable = a.Lendable || lendable[c.ID]
	}
	l, err := scanLoan(s.QueryRow("SELECT "+loanColumns+loanJoins+" WHERE l.asset_id = ? AND l.checked_in_at IS NULL", a.Asset.ID), time.Now())
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading loan: %w", err)
	}
	a.Loan = &l
	return nil
}

// actorID is who acts: the user or the API token.
func (s *Store) actorID() any {
	switch {
	case s.actor != "":
		return s.actor
	case s.actorToken != "":
		return s.actorToken
	}
	return nil
}

// personTx reads a user as a loan event names them.
func (s *Store) personTx(tx *sql.Tx, id string) (*models.EventActor, error) {
	p := &models.EventActor{ID: id}
	if err := s.TxQueryRow(tx, "SELECT email, name FROM users WHERE id = ?", id).Scan(&p.Email, &p.Name); err != nil {
		return nil, fmt.Errorf("reading borrower: %w", err)
	}
	return p, nil
}

// CheckOut lends an asset to a borrower, an active user, until dueAt (nil
// for no due date). The asset must be active, in a lendable category, not
// counted by quantity, not out of service and not already lent.
func (s *Store) CheckOut(assetID, borrowerID string, dueAt *time.Time) (*models.AssetWithDetails, error) {
	now := time.Now().UTC()
	if dueAt != nil {
		if !dueAt.After(now) {
			return nil, ErrDueInPast
		}
		at := dueAt.UTC()
		dueAt = &at
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if err := s.lockAssetTx(tx, assetID); err != nil {
		return nil, err
	}
	var (
		archived  sql.NullTime
		quantity  sql.NullInt64
		condition string
	)
	if err := s.TxQueryRow(tx, "SELECT archived_at, stock_quantity, asset_condition FROM assets WHERE id = ?", assetID).Scan(&archived, &quantity, &condition); err != nil {
		return nil, fmt.Errorf("reading asset: %w", err)
	}
	switch {
	case archived.Valid:
		return nil, ErrAssetArchived
	case quantity.Valid:
		return nil, ErrLentByQuantity
	case condition == models.ConditionOutOfService:
		return nil, ErrOutOfService
	}
	lendable, err := s.lendableCategories(txQuerier{s, tx})
	if err != nil {
		return nil, err
	}
	ok := false
	err = s.scanPairs(tx, "SELECT category_id, category_id FROM asset_categories WHERE asset_id = ?", assetID, func(id, _ string) { ok = ok || lendable[id] })
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotLendable
	}
	var open int
	if err := s.TxQueryRow(tx, "SELECT COUNT(*) FROM asset_loans WHERE asset_id = ? AND checked_in_at IS NULL", assetID).Scan(&open); err != nil {
		return nil, fmt.Errorf("checking loans: %w", err)
	}
	if open > 0 {
		return nil, ErrAlreadyLent
	}
	var active int
	if err := s.TxQueryRow(tx, "SELECT COUNT(*) FROM users WHERE id = ? AND is_active = TRUE AND archived_at IS NULL", borrowerID).Scan(&active); err != nil {
		return nil, fmt.Errorf("checking borrower: %w", err)
	}
	if active == 0 {
		return nil, ErrBorrowerNotFound
	}
	_, err = s.TxExec(tx, "INSERT INTO asset_loans (id, asset_id, borrower_id, lent_by, checked_out_at, due_at) VALUES (?, ?, ?, ?, ?, ?)",
		uuid.NewString(), assetID, borrowerID, s.actorID(), now, dueAt)
	if err != nil {
		return nil, fmt.Errorf("recording loan: %w", err)
	}
	borrower, err := s.personTx(tx, borrowerID)
	if err != nil {
		return nil, err
	}
	err = s.recordAssetEvent(tx, models.EventAssetCheckedOut, assetID, models.EventPayload{Trigger: models.TriggerManual, Borrower: borrower, DueAt: dueAt})
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing: %w", err)
	}
	return s.GetAssetByID(assetID)
}

// CheckIn ends an asset's open loan. When onlyBorrower is set, the loan must
// be theirs: people who may only borrow check in their own loans.
func (s *Store) CheckIn(assetID, onlyBorrower string) (*models.AssetWithDetails, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if err := s.lockAssetTx(tx, assetID); err != nil {
		return nil, err
	}
	var loanID, borrowerID string
	err = s.TxQueryRow(tx, "SELECT id, borrower_id FROM asset_loans WHERE asset_id = ? AND checked_in_at IS NULL", assetID).Scan(&loanID, &borrowerID)
	if err == sql.ErrNoRows {
		return nil, ErrNotLent
	}
	if err != nil {
		return nil, fmt.Errorf("reading loan: %w", err)
	}
	if onlyBorrower != "" && onlyBorrower != borrowerID {
		return nil, ErrNotYourLoan
	}
	if _, err := s.TxExec(tx, "UPDATE asset_loans SET checked_in_at = ?, returned_by = ? WHERE id = ?", time.Now().UTC(), s.actorID(), loanID); err != nil {
		return nil, fmt.Errorf("checking in: %w", err)
	}
	borrower, err := s.personTx(tx, borrowerID)
	if err != nil {
		return nil, err
	}
	if err := s.recordAssetEvent(tx, models.EventAssetCheckedIn, assetID, models.EventPayload{Trigger: models.TriggerManual, Borrower: borrower}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing: %w", err)
	}
	return s.GetAssetByID(assetID)
}

// lockAssetTx holds an asset's row until tx ends, so two loans of it are
// decided one after the other. A missing asset is ErrAssetNotFound.
func (s *Store) lockAssetTx(tx *sql.Tx, assetID string) error {
	res, err := s.TxExec(tx, "UPDATE assets SET updated_at = updated_at WHERE id = ?", assetID)
	if err != nil {
		return fmt.Errorf("locking asset: %w", err)
	}
	// MariaDB counts rows changed, not matched, so check the asset exists.
	if n, _ := res.RowsAffected(); n == 0 {
		var exists int
		if err := s.TxQueryRow(tx, "SELECT COUNT(*) FROM assets WHERE id = ?", assetID).Scan(&exists); err != nil {
			return fmt.Errorf("reading asset: %w", err)
		}
		if exists == 0 {
			return ErrAssetNotFound
		}
	}
	return nil
}

// ListLoans lists loans, newest first.
func (s *Store) ListLoans(f LoanFilter) ([]models.Loan, error) {
	now := time.Now().UTC()
	query, args := "SELECT "+loanColumns+loanJoins+" WHERE 1 = 1", []any{}
	if f.BorrowerID != "" {
		query += " AND l.borrower_id = ?"
		args = append(args, f.BorrowerID)
	}
	switch f.State {
	case LoansAll:
	case LoansOpen:
		query += " AND l.checked_in_at IS NULL"
	case LoansOverdue:
		query += " AND l.checked_in_at IS NULL AND l.due_at < ?"
		args = append(args, now)
	default:
		return nil, ErrInvalidLoanFilter
	}
	rows, err := s.Query(query+" ORDER BY l.checked_out_at DESC, l.id", args...)
	if err != nil {
		return nil, fmt.Errorf("listing loans: %w", err)
	}
	defer rows.Close()
	loans := []models.Loan{}
	for rows.Next() {
		l, err := scanLoan(rows, now)
		if err != nil {
			return nil, fmt.Errorf("scanning loan: %w", err)
		}
		loans = append(loans, l)
	}
	return loans, rows.Err()
}

// RecordOverdueLoans records asset.overdue for each open loan that has
// passed its due date and has not been announced, and returns how many it
// recorded. Each loan is announced once, however many servers sweep at
// once: a loan is marked in the same transaction, and only by the first.
func (s *Store) RecordOverdueLoans(now time.Time) (int, error) {
	now = now.UTC()
	rows, err := s.Query("SELECT id, asset_id, borrower_id, due_at FROM asset_loans WHERE checked_in_at IS NULL AND overdue_notified_at IS NULL AND due_at < ?", now)
	if err != nil {
		return 0, fmt.Errorf("finding overdue loans: %w", err)
	}
	type due struct {
		id, asset, borrower string
		at                  time.Time
	}
	var loans []due
	for rows.Next() {
		var d due
		if err := rows.Scan(&d.id, &d.asset, &d.borrower, &d.at); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scanning overdue loan: %w", err)
		}
		loans = append(loans, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	recorded := 0
	for _, d := range loans {
		ok, err := s.recordOverdue(d.id, d.asset, d.borrower, d.at, now)
		if err != nil {
			return recorded, err
		}
		if ok {
			recorded++
		}
	}
	return recorded, nil
}

func (s *Store) recordOverdue(loanID, assetID, borrowerID string, dueAt, now time.Time) (bool, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return false, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	res, err := s.TxExec(tx, "UPDATE asset_loans SET overdue_notified_at = ? WHERE id = ? AND overdue_notified_at IS NULL AND checked_in_at IS NULL", now, loanID)
	if err != nil {
		return false, fmt.Errorf("marking loan overdue: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		// Another server announced it, or it came back meanwhile.
		return false, err
	}
	borrower, err := s.personTx(tx, borrowerID)
	if err != nil {
		return false, err
	}
	at := dueAt.UTC()
	if err := s.recordAssetEvent(tx, models.EventAssetOverdue, assetID, models.EventPayload{Trigger: models.TriggerSchedule, Borrower: borrower, DueAt: &at}); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// ListPeople lists the active users a loan can name, by name then email.
func (s *Store) ListPeople() ([]models.Person, error) {
	rows, err := s.Query("SELECT id, email, name FROM users WHERE is_active = TRUE AND archived_at IS NULL ORDER BY name, email")
	if err != nil {
		return nil, fmt.Errorf("listing people: %w", err)
	}
	defer rows.Close()
	people := []models.Person{}
	for rows.Next() {
		var p models.Person
		if err := rows.Scan(&p.ID, &p.Email, &p.Name); err != nil {
			return nil, fmt.Errorf("scanning person: %w", err)
		}
		people = append(people, p)
	}
	return people, rows.Err()
}
