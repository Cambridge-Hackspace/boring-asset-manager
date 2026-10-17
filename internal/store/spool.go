package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/google/uuid"
)

// The spool (README §6.2): labels waiting to be printed, gathered over time,
// sorted onto templates and printers, and printed or downloaded together.
// Printed entries stay, as the Printed list.

// MaxSpoolWaiting bounds how many labels may wait in the spool at once. It
// is a variable so tests can lower it.
var MaxSpoolWaiting = 5000

// MaxSpoolCopies bounds the copies of one entry, as for direct printing.
const MaxSpoolCopies = 20

var (
	// ErrSpoolFull is returned for adding more than MaxSpoolWaiting labels.
	ErrSpoolFull = errors.New("the spool is full")
	// ErrSpoolEntryNotFound is returned for an entry that is not in the
	// spool, or not in the state the change needs.
	ErrSpoolEntryNotFound = errors.New("spool entry not found")
	// ErrSpoolEntryBusy is returned for changing an entry a job is
	// printing.
	ErrSpoolEntryBusy = errors.New("a job is printing that label")
	// ErrInvalidDestination is returned for a destination that is neither
	// a download nor a printer's.
	ErrInvalidDestination = errors.New(`destination must be "download", "printer:<id>", "pool:<id>" or empty`)
	// ErrInvalidCopies is returned for copies out of range.
	ErrInvalidCopies = fmt.Errorf("copies must be 1-%d", MaxSpoolCopies)
	// ErrTemplateInUse is returned for deleting a label template that
	// categories name.
	ErrTemplateInUse = errors.New("label template in use")
)

// SpoolAdd is a label to add to the spool.
type SpoolAdd struct {
	AssetID     string
	TemplateID  *string
	Destination string
	Copies      int
}

// SpoolFilter selects spool entries: those waiting, or with Printed those
// printed since PrintedSince (all of them when it is zero); with AddedBy,
// only those that user or token added; with IDs, only those; with JobID,
// only those last in that job.
type SpoolFilter struct {
	Printed      bool
	PrintedSince time.Time
	AddedBy      string
	IDs          []string
	JobID        string
}

// SpoolUpdate changes waiting entries: the template when SetTemplate is
// set (nil for none), the destination when SetDestination is, and copies
// when Copies is not nil.
type SpoolUpdate struct {
	SetTemplate    bool
	TemplateID     *string
	SetDestination bool
	Destination    string
	Copies         *int
}

const spoolColumns = `e.id, e.asset_id, a.name, a.archived_at, e.template_id, e.printer_id, e.to_download, e.copies, e.problem,
	e.added_by, COALESCE(adu.name, adt.name, ''), adt.id IS NOT NULL, e.added_at,
	e.printed_at, e.printed_by, COALESCE(pru.name, prt.name, ''), prt.id IS NOT NULL, e.printed_template, e.printed_on,
	b.state, j.id, j.target_name, e.pool_id`

const spoolJoins = ` FROM spool_entries e JOIN assets a ON a.id = e.asset_id
	LEFT JOIN users adu ON adu.id = e.added_by LEFT JOIN api_tokens adt ON adt.id = e.added_by
	LEFT JOIN users pru ON pru.id = e.printed_by LEFT JOIN api_tokens prt ON prt.id = e.printed_by
	LEFT JOIN spool_batches b ON b.id = e.batch_id LEFT JOIN spool_jobs j ON j.id = b.job_id`

func scanSpoolEntry(row interface{ Scan(...any) error }) (models.SpoolEntry, error) {
	var (
		e                     models.SpoolEntry
		archived, printed     sql.NullTime
		template, printer     sql.NullString
		download              bool
		addedBy, printedBy    sql.NullString
		printedName           string
		addedToken, prntToken bool
		batchState, jobID     sql.NullString
		jobTarget, pool       sql.NullString
	)
	err := row.Scan(&e.ID, &e.AssetID, &e.AssetName, &archived, &template, &printer, &download, &e.Copies, &e.Problem,
		&addedBy, &e.AddedBy.Name, &addedToken, &e.AddedAt,
		&printed, &printedBy, &printedName, &prntToken, &e.PrintedTemplate, &e.PrintedOn,
		&batchState, &jobID, &jobTarget, &pool)
	if err != nil {
		return e, err
	}
	e.AssetArchived = archived.Valid
	if template.Valid && template.String != "" {
		e.TemplateID = &template.String
	}
	switch {
	case download:
		e.Destination = models.DestinationDownload
	case printer.Valid && printer.String != "":
		e.Destination = models.DestinationPrinterPrefix + printer.String
	case pool.Valid && pool.String != "":
		e.Destination = models.DestinationPoolPrefix + pool.String
	}
	e.AddedBy.ID, e.AddedBy.Token = addedBy.String, addedToken
	if !printed.Valid && (batchState.String == models.SpoolWaiting || batchState.String == models.SpoolSending) {
		e.Job = &models.SpoolJobRef{ID: jobID.String, State: batchState.String, Target: jobTarget.String}
	}
	if printed.Valid {
		at := printed.Time.UTC()
		e.PrintedAt = &at
		e.PrintedBy = &models.EventActor{ID: printedBy.String, Name: printedName, Token: prntToken}
	}
	return e, nil
}

// placeholders returns n comma-separated placeholders.
func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}

// ListSpool lists the spool entries a filter selects with their assets'
// scopes: waiting ones in the order they were added, printed ones most
// recently printed first.
func (s *Store) ListSpool(f SpoolFilter) ([]models.SpoolEntry, error) {
	where, args := []string{"e.printed_at IS NULL"}, []any{}
	order := "e.added_at, e.id"
	if f.Printed {
		where[0], order = "e.printed_at IS NOT NULL", "e.printed_at DESC, e.id"
		if !f.PrintedSince.IsZero() {
			where, args = append(where, "e.printed_at >= ?"), append(args, dbTime(f.PrintedSince))
		}
	}
	if f.AddedBy != "" {
		where, args = append(where, "e.added_by = ?"), append(args, f.AddedBy)
	}
	if f.JobID != "" {
		where, args = append(where, "e.batch_id IN (SELECT id FROM spool_batches WHERE job_id = ?)"), append(args, f.JobID)
	}
	if f.IDs != nil {
		if len(f.IDs) == 0 {
			return []models.SpoolEntry{}, nil
		}
		where = append(where, "e.id IN ("+placeholders(len(f.IDs))+")")
		for _, id := range f.IDs {
			args = append(args, id)
		}
	}
	rows, err := s.Query("SELECT "+spoolColumns+spoolJoins+" WHERE "+strings.Join(where, " AND ")+" ORDER BY "+order, args...)
	if err != nil {
		return nil, fmt.Errorf("listing the spool: %w", err)
	}
	defer rows.Close()
	entries := []models.SpoolEntry{}
	var assetIDs []string
	for rows.Next() {
		e, err := scanSpoolEntry(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning spool entry: %w", err)
		}
		entries = append(entries, e)
		assetIDs = append(assetIDs, e.AssetID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	scopes, err := s.AssetScopes(assetIDs)
	if err != nil {
		return nil, err
	}
	for i := range entries {
		entries[i].AssetScopes = scopes[entries[i].AssetID]
	}
	return entries, nil
}

// destinationColumns turns a destination into its printer_id, pool_id and
// to_download columns, checking the printer or pool exists.
func (s *Store) destinationColumns(q interface {
	QueryRow(string, ...any) *sql.Row
}, destination string) (printerID, poolID *string, download bool, err error) {
	switch {
	case destination == "":
		return nil, nil, false, nil
	case destination == models.DestinationDownload:
		return nil, nil, true, nil
	case strings.HasPrefix(destination, models.DestinationPoolPrefix):
		id := strings.TrimPrefix(destination, models.DestinationPoolPrefix)
		var n int
		if err := q.QueryRow("SELECT COUNT(*) FROM printer_pools WHERE id = ?", id).Scan(&n); err != nil {
			return nil, nil, false, fmt.Errorf("finding the pool: %w", err)
		}
		if n == 0 {
			return nil, nil, false, ErrPoolNotFound
		}
		return nil, &id, false, nil
	case strings.HasPrefix(destination, models.DestinationPrinterPrefix):
		id := strings.TrimPrefix(destination, models.DestinationPrinterPrefix)
		var n int
		if err := q.QueryRow("SELECT COUNT(*) FROM printers WHERE id = ?", id).Scan(&n); err != nil {
			return nil, nil, false, fmt.Errorf("finding the printer: %w", err)
		}
		if n == 0 {
			return nil, nil, false, ErrPrinterNotFound
		}
		return &id, nil, false, nil
	}
	return nil, nil, false, ErrInvalidDestination
}

// templateExists checks a label template exists.
func templateExists(q interface {
	QueryRow(string, ...any) *sql.Row
}, id string) error {
	var n int
	if err := q.QueryRow("SELECT COUNT(*) FROM templates WHERE id = ?", id).Scan(&n); err != nil {
		return fmt.Errorf("finding the template: %w", err)
	}
	if n == 0 {
		return ErrTemplateNotFound
	}
	return nil
}

// txRow runs QueryRow within a transaction, for helpers shared with
// queries outside one.
type txRow struct {
	s  *Store
	tx *sql.Tx
}

func (t txRow) QueryRow(query string, args ...any) *sql.Row {
	return t.s.TxQueryRow(t.tx, query, args...)
}

// AddToSpool adds labels to the spool, attributed to the acting user or
// token. A label already waiting for the same asset and template is not
// added again: its asset is listed in already. Adding fails, with nothing
// added, when the spool would hold more than MaxSpoolWaiting labels.
func (s *Store) AddToSpool(adds []SpoolAdd) (added []models.SpoolEntry, already []string, err error) {
	for _, a := range adds {
		if a.Copies < 1 || a.Copies > MaxSpoolCopies {
			return nil, nil, ErrInvalidCopies
		}
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if err := s.takeLock(tx, "spool"); err != nil {
		return nil, nil, err
	}
	var waiting int
	if err := s.TxQueryRow(tx, "SELECT COUNT(*) FROM spool_entries WHERE printed_at IS NULL").Scan(&waiting); err != nil {
		return nil, nil, fmt.Errorf("counting the spool: %w", err)
	}
	q := txRow{s, tx}
	now := dbTime(time.Now())
	var ids []string
	seen := map[string]bool{}
	for _, a := range adds {
		if a.TemplateID != nil {
			if err := templateExists(q, *a.TemplateID); err != nil {
				return nil, nil, err
			}
		}
		printerID, poolID, download, err := s.destinationColumns(q, a.Destination)
		if err != nil {
			return nil, nil, err
		}
		var n int
		if a.TemplateID == nil {
			err = s.TxQueryRow(tx, "SELECT COUNT(*) FROM spool_entries WHERE asset_id = ? AND printed_at IS NULL AND template_id IS NULL", a.AssetID).Scan(&n)
		} else {
			err = s.TxQueryRow(tx, "SELECT COUNT(*) FROM spool_entries WHERE asset_id = ? AND printed_at IS NULL AND template_id = ?", a.AssetID, *a.TemplateID).Scan(&n)
		}
		if err != nil {
			return nil, nil, fmt.Errorf("checking the spool: %w", err)
		}
		key := a.AssetID + "|"
		if a.TemplateID != nil {
			key += *a.TemplateID
		}
		if n > 0 || seen[key] {
			already = append(already, a.AssetID)
			continue
		}
		seen[key] = true
		if waiting++; waiting > MaxSpoolWaiting {
			return nil, nil, ErrSpoolFull
		}
		if err := s.assetExists(tx, a.AssetID); err != nil {
			return nil, nil, err
		}
		// A microsecond apart, so the spool lists them in the order given.
		id := uuid.NewString()
		_, err = s.TxExec(tx, `INSERT INTO spool_entries (id, asset_id, template_id, printer_id, pool_id, to_download, copies, added_by, added_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, a.AssetID, a.TemplateID, printerID, poolID, download, a.Copies, s.actorID(), now.Add(time.Duration(len(ids))*time.Microsecond))
		if err != nil {
			return nil, nil, fmt.Errorf("adding to the spool: %w", err)
		}
		ids = append(ids, id)
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("committing: %w", err)
	}
	if added, err = s.spoolInOrder(ids); err != nil {
		return nil, nil, err
	}
	if already == nil {
		already = []string{}
	}
	return added, already, nil
}

// assetExists checks an asset exists, within tx.
func (s *Store) assetExists(tx *sql.Tx, id string) error {
	var n int
	if err := s.TxQueryRow(tx, "SELECT COUNT(*) FROM assets WHERE id = ?", id).Scan(&n); err != nil {
		return fmt.Errorf("finding the asset: %w", err)
	}
	if n == 0 {
		return ErrAssetNotFound
	}
	return nil
}

// waitingIn checks every ID names a waiting entry that no job is printing,
// within tx.
func (s *Store) waitingIn(tx *sql.Tx, ids []string) error {
	if err := s.waitingOrBusyIn(tx, ids); err != nil {
		return err
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	var busy int
	err := s.TxQueryRow(tx, `SELECT COUNT(*) FROM spool_entries WHERE id IN (`+placeholders(len(ids))+`)
		AND batch_id IN (SELECT id FROM spool_batches WHERE state IN ('waiting', 'sending'))`, args...).Scan(&busy)
	if err != nil {
		return fmt.Errorf("finding spool entries being printed: %w", err)
	}
	if busy > 0 {
		return ErrSpoolEntryBusy
	}
	return nil
}

// waitingOrBusyIn checks every ID names a waiting entry, within tx.
func (s *Store) waitingOrBusyIn(tx *sql.Tx, ids []string) error {
	if len(ids) == 0 {
		return ErrSpoolEntryNotFound
	}
	args := make([]any, len(ids))
	seen := map[string]bool{}
	for i, id := range ids {
		if seen[id] {
			return fmt.Errorf("%w: %s is listed more than once", ErrSpoolEntryNotFound, id)
		}
		seen[id], args[i] = true, id
	}
	var n int
	err := s.TxQueryRow(tx, "SELECT COUNT(*) FROM spool_entries WHERE printed_at IS NULL AND id IN ("+placeholders(len(ids))+")", args...).Scan(&n)
	if err != nil {
		return fmt.Errorf("finding spool entries: %w", err)
	}
	if n != len(ids) {
		return ErrSpoolEntryNotFound
	}
	return nil
}

// UpdateSpool changes waiting entries, all or none, and clears their
// problems: they are to be tried again.
func (s *Store) UpdateSpool(ids []string, u SpoolUpdate) error {
	if u.Copies != nil && (*u.Copies < 1 || *u.Copies > MaxSpoolCopies) {
		return ErrInvalidCopies
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if err := s.waitingIn(tx, ids); err != nil {
		return err
	}
	q := txRow{s, tx}
	set, args := []string{"problem = ''"}, []any{}
	if u.SetTemplate {
		if u.TemplateID != nil {
			if err := templateExists(q, *u.TemplateID); err != nil {
				return err
			}
		}
		set, args = append(set, "template_id = ?"), append(args, u.TemplateID)
	}
	if u.SetDestination {
		printerID, poolID, download, err := s.destinationColumns(q, u.Destination)
		if err != nil {
			return err
		}
		set, args = append(set, "printer_id = ?", "pool_id = ?", "to_download = ?"), append(args, printerID, poolID, download)
	}
	if u.Copies != nil {
		set, args = append(set, "copies = ?"), append(args, *u.Copies)
	}
	for _, id := range ids {
		args = append(args, id)
	}
	if _, err := s.TxExec(tx, "UPDATE spool_entries SET "+strings.Join(set, ", ")+" WHERE id IN ("+placeholders(len(ids))+")", args...); err != nil {
		return fmt.Errorf("updating the spool: %w", err)
	}
	return tx.Commit()
}

// RemoveFromSpool takes waiting entries out of the spool, all or none.
func (s *Store) RemoveFromSpool(ids []string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if err := s.waitingIn(tx, ids); err != nil {
		return err
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	if _, err := s.TxExec(tx, "DELETE FROM spool_entries WHERE id IN ("+placeholders(len(ids))+")", args...); err != nil {
		return fmt.Errorf("removing from the spool: %w", err)
	}
	return tx.Commit()
}

// SetSpoolProblems records why waiting entries' labels could not be
// produced, by entry ID.
func (s *Store) SetSpoolProblems(problems map[string]string) error {
	for id, problem := range problems {
		if len(problem) > 500 {
			problem = problem[:497] + "..."
		}
		if _, err := s.Exec("UPDATE spool_entries SET problem = ? WHERE id = ? AND printed_at IS NULL", problem, id); err != nil {
			return fmt.Errorf("recording a spool problem: %w", err)
		}
	}
	return nil
}

// RecordLabelsPrinted records labels printed outside the spool in their
// assets' histories: copies of each, with the template and on the printer
// named.
func (s *Store) RecordLabelsPrinted(assetIDs []string, template, printer string, copies int) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	for _, id := range assetIDs {
		label := &models.EventLabel{Template: template, Printer: printer, Copies: copies}
		if err := s.recordAssetEvent(tx, models.EventAssetLabelPrinted, id, models.EventPayload{Trigger: models.TriggerManual, Label: label}); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetCategoryLabelTemplate names the label template a category's assets
// join the spool with, or with nil, none, so they take their parent's.
func (s *Store) SetCategoryLabelTemplate(categoryID string, templateID *string) (*models.Category, error) {
	if _, err := s.GetCategoryByID(categoryID); err != nil {
		return nil, err
	}
	if templateID != nil {
		if err := templateExists(s, *templateID); err != nil {
			return nil, err
		}
	}
	if _, err := s.Exec("UPDATE categories SET label_template_id = ?, updated_at = ? WHERE id = ?", templateID, time.Now().UTC(), categoryID); err != nil {
		return nil, fmt.Errorf("setting the label template: %w", err)
	}
	return s.GetCategoryByID(categoryID)
}

// CategoryAssetIDs lists the active assets in a category, and with
// subcategories in the categories below it too, oldest first.
func (s *Store) CategoryAssetIDs(categoryID string, subcategories bool) ([]string, error) {
	if _, err := s.GetCategoryByID(categoryID); err != nil {
		return nil, err
	}
	cats := []string{categoryID}
	if subcategories {
		traits, err := s.categoryTraits(storeQuerier{s})
		if err != nil {
			return nil, err
		}
		for id := range traits.parents {
			for p, d := traits.parents[id], 0; p != "" && d < 100; p, d = traits.parents[p], d+1 {
				if p == categoryID {
					cats = append(cats, id)
					break
				}
			}
		}
	}
	args := make([]any, len(cats))
	for i, c := range cats {
		args[i] = c
	}
	rows, err := s.Query(`SELECT a.id FROM assets a WHERE a.archived_at IS NULL AND a.id IN
		(SELECT asset_id FROM asset_categories WHERE category_id IN (`+placeholders(len(cats))+`)) ORDER BY a.created_at, a.id`, args...)
	if err != nil {
		return nil, fmt.Errorf("listing the category's assets: %w", err)
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// AssetsByIDs reads assets as GetAssetByID does, sharing what they need
// from whole tables; those that do not exist are left out.
func (s *Store) AssetsByIDs(ids []string) ([]models.AssetWithDetails, error) {
	ctx := &assetContext{list: true}
	out := make([]models.AssetWithDetails, 0, len(ids))
	for _, id := range ids {
		a, err := s.readAsset(id, ctx)
		if errors.Is(err, ErrAssetNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, nil
}

// spoolInOrder reads entries in the order of ids, which may be empty.
func (s *Store) spoolInOrder(ids []string) ([]models.SpoolEntry, error) {
	if ids == nil {
		ids = []string{}
	}
	entries, err := s.ListSpool(SpoolFilter{IDs: ids})
	if err != nil {
		return nil, err
	}
	byID := make(map[string]models.SpoolEntry, len(entries))
	for _, e := range entries {
		byID[e.ID] = e
	}
	out := make([]models.SpoolEntry, 0, len(ids))
	for _, id := range ids {
		if e, ok := byID[id]; ok {
			out = append(out, e)
		}
	}
	return out, nil
}
