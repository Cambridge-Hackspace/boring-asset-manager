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

// Printing from the spool (README §6.2): jobs, each sent in batches that a
// printer takes one at a time. The API's spooler claims batches, sends them
// and finishes them here; the database keeps two servers from claiming the
// same batch or sending two batches to one printer at once.

var (
	// ErrSpoolJobNotFound is returned for a job that does not exist.
	ErrSpoolJobNotFound = errors.New("spool job not found")
	// ErrSpoolJobStarted is returned for canceling a job already sending.
	ErrSpoolJobStarted = errors.New("the job has started; what is sent cannot be called back")
	// ErrSpoolBatchGiven is returned for finishing a batch the sweep gave up
	// on, its server having stopped beating.
	ErrSpoolBatchGiven = errors.New("the batch was given up as abandoned")
)

// SpoolJobNew is a job to make: entries to print with one template, on one
// printer, or with PoolID set, shared by a pool's Members in batches of
// BatchSize labels.
type SpoolJobNew struct {
	TemplateID, TemplateName string
	PrinterID, PrinterName   string
	PoolID, PoolName         string
	Members                  []PoolJobMember
	BatchSize                int
	EntryIDs                 []string
}

// SpoolBatch is a batch claimed to send: its job's template and pool, who
// asked for it, the printer, and its entries.
type SpoolBatch struct {
	ID, JobID      string
	TemplateID     *string
	TemplateName   string
	PoolID         string
	PrinterID      string
	CreatedBy      string
	CreatedByToken bool
	Entries        []SpoolBatchEntry
}

// SpoolBatchEntry is an entry of a batch: its asset and copies.
type SpoolBatchEntry struct {
	ID, AssetID string
	Copies      int
}

// actorColumns are the acting user or token, for created_by columns.
func (s *Store) actorColumns() (id any, token bool) {
	return s.actorID(), s.actor == "" && s.actorToken != ""
}

// CreateSpoolJobs makes jobs to print waiting entries, all or none: a job
// on a printer is a single batch; a pool's is cut into batches, those for
// members that do not pull given to them by their shares, and the rest left
// for whichever pulling member is free first.
func (s *Store) CreateSpoolJobs(jobs []SpoolJobNew) ([]string, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if err := s.takeLock(tx, "spool"); err != nil {
		return nil, err
	}
	now := dbTime(time.Now())
	by, byToken := s.actorColumns()
	var ids []string
	for i, j := range jobs {
		if err := s.waitingIn(tx, j.EntryIDs); err != nil {
			return nil, err
		}
		entries, err := s.entryCopies(tx, j.EntryIDs)
		if err != nil {
			return nil, err
		}
		labels := 0
		for _, e := range entries {
			labels += e.Copies
		}
		jobID := uuid.NewString()
		// A microsecond apart, so they are sent in the order made.
		at := now.Add(time.Duration(i) * time.Microsecond)
		var printerID, poolID any
		target := j.PrinterName
		if j.PoolID != "" {
			poolID, target = j.PoolID, j.PoolName
		} else {
			printerID = j.PrinterID
		}
		_, err = s.TxExec(tx, `INSERT INTO spool_jobs (id, template_id, template_name, printer_id, pool_id, target_name, state, labels, created_by, created_by_token, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, jobID, j.TemplateID, j.TemplateName, printerID, poolID, target, models.SpoolWaiting, labels, by, byToken, at)
		if err != nil {
			return nil, fmt.Errorf("making a print job: %w", err)
		}
		if j.PoolID == "" {
			if err := s.insertBatch(tx, jobID, 0, &j.PrinterID, j.PrinterName, entries); err != nil {
				return nil, err
			}
			ids = append(ids, jobID)
			continue
		}
		for _, m := range j.Members {
			if _, err := s.TxExec(tx, "INSERT INTO spool_job_members (job_id, printer_id, pulls) VALUES (?, ?, ?)", jobID, m.PrinterID, m.Pulls); err != nil {
				return nil, fmt.Errorf("making a print job: %w", err)
			}
		}
		batches := cutBatches(entries, j.BatchSize)
		picks := shareOut(j.Members, len(batches))
		for n, batch := range batches {
			m := j.Members[picks[n]]
			if m.Pulls {
				err = s.insertBatch(tx, jobID, n, nil, "", batch)
			} else {
				err = s.insertBatch(tx, jobID, n, &m.PrinterID, m.PrinterName, batch)
			}
			if err != nil {
				return nil, err
			}
		}
		ids = append(ids, jobID)
	}
	return ids, tx.Commit()
}

// entryCopies reads entries' copies, in the order of ids, within tx.
func (s *Store) entryCopies(tx *sql.Tx, ids []string) ([]SpoolBatchEntry, error) {
	byID := map[string]SpoolBatchEntry{}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := s.TxQuery(tx, "SELECT id, asset_id, copies FROM spool_entries WHERE id IN ("+placeholders(len(ids))+")", args...)
	if err != nil {
		return nil, fmt.Errorf("reading labels: %w", err)
	}
	for rows.Next() {
		var e SpoolBatchEntry
		if err := rows.Scan(&e.ID, &e.AssetID, &e.Copies); err != nil {
			rows.Close()
			return nil, err
		}
		byID[e.ID] = e
	}
	rows.Close()
	out := make([]SpoolBatchEntry, 0, len(ids))
	for _, id := range ids {
		out = append(out, byID[id])
	}
	return out, rows.Err()
}

// insertBatch makes a waiting batch of entries, for a printer or, with
// none, for a pool's members to take, within tx.
func (s *Store) insertBatch(tx *sql.Tx, jobID string, seq int, printerID *string, printerName string, entries []SpoolBatchEntry) error {
	labels := 0
	ids := make([]string, len(entries))
	for i, e := range entries {
		labels += e.Copies
		ids[i] = e.ID
	}
	batchID := uuid.NewString()
	_, err := s.TxExec(tx, `INSERT INTO spool_batches (id, job_id, seq, printer_id, printer_name, state, labels) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		batchID, jobID, seq, printerID, printerName, models.SpoolWaiting, labels)
	if err != nil {
		return fmt.Errorf("making a print job: %w", err)
	}
	return s.setBatch(tx, ids, batchID)
}

// copiesOf totals entries' copies, within tx.
func (s *Store) copiesOf(tx *sql.Tx, ids []string) (int, error) {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	var n int
	if err := s.TxQueryRow(tx, "SELECT COALESCE(SUM(copies), 0) FROM spool_entries WHERE id IN ("+placeholders(len(ids))+")", args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("counting labels: %w", err)
	}
	return n, nil
}

// setBatch puts entries in a batch, within tx.
func (s *Store) setBatch(tx *sql.Tx, ids []string, batchID string) error {
	args := []any{batchID}
	for _, id := range ids {
		args = append(args, id)
	}
	if _, err := s.TxExec(tx, "UPDATE spool_entries SET batch_id = ?, problem = '' WHERE id IN ("+placeholders(len(ids))+")", args...); err != nil {
		return fmt.Errorf("putting labels in a job: %w", err)
	}
	return nil
}

// RecordSpoolDownload records waiting entries downloaded with a template
// as a finished job, so it can be sent again, and marks them printed, all
// or none.
func (s *Store) RecordSpoolDownload(ids []string, templateID, templateName string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if err := s.waitingIn(tx, ids); err != nil {
		return err
	}
	labels, err := s.copiesOf(tx, ids)
	if err != nil {
		return err
	}
	now := dbTime(time.Now())
	by, byToken := s.actorColumns()
	jobID, batchID := uuid.NewString(), uuid.NewString()
	_, err = s.TxExec(tx, `INSERT INTO spool_jobs (id, template_id, template_name, target_name, to_download, state, labels, created_by, created_by_token, created_at, started_at, finished_at)
		VALUES (?, ?, ?, 'Download', TRUE, ?, ?, ?, ?, ?, ?, ?)`, jobID, templateID, templateName, models.SpoolDone, labels, by, byToken, now, now, now)
	if err != nil {
		return fmt.Errorf("recording a download: %w", err)
	}
	_, err = s.TxExec(tx, `INSERT INTO spool_batches (id, job_id, seq, printer_name, state, labels, started_at, finished_at) VALUES (?, ?, 0, 'Download', ?, ?, ?, ?)`,
		batchID, jobID, models.SpoolDone, labels, now, now)
	if err != nil {
		return fmt.Errorf("recording a download: %w", err)
	}
	if err := s.setBatch(tx, ids, batchID); err != nil {
		return err
	}
	if err := s.markPrintedTx(tx, ids, templateName, ""); err != nil {
		return err
	}
	return tx.Commit()
}

// markPrintedTx records entries printed, and their labels in their assets'
// histories, within tx.
func (s *Store) markPrintedTx(tx *sql.Tx, ids []string, template, printer string) error {
	on := printer
	if on == "" {
		on = "Download"
	}
	now := dbTime(time.Now())
	for _, id := range ids {
		var assetID string
		var copies int
		if err := s.TxQueryRow(tx, "SELECT asset_id, copies FROM spool_entries WHERE id = ?", id).Scan(&assetID, &copies); err != nil {
			return fmt.Errorf("reading spool entry: %w", err)
		}
		_, err := s.TxExec(tx, `UPDATE spool_entries SET printed_at = ?, printed_by = ?, printed_template = ?, printed_on = ?, problem = ''
			WHERE id = ?`, now, s.actorID(), template, on, id)
		if err != nil {
			return fmt.Errorf("marking printed: %w", err)
		}
		label := &models.EventLabel{Template: template, Printer: printer, Download: printer == "", Copies: copies}
		if err := s.recordAssetEvent(tx, models.EventAssetLabelPrinted, assetID, models.EventPayload{Trigger: models.TriggerManual, Label: label}); err != nil {
			return err
		}
	}
	return nil
}

// ClaimSpoolBatch claims the oldest waiting batch for a printer to send,
// unless one is already being sent to it, from here or another server.
// It returns nil when there is nothing to send.
func (s *Store) ClaimSpoolBatch(printerID string) (*SpoolBatch, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if err := s.takeLock(tx, "spool"); err != nil {
		return nil, err
	}
	var sending int
	if err := s.TxQueryRow(tx, "SELECT COUNT(*) FROM spool_batches WHERE printer_id = ? AND state = ?", printerID, models.SpoolSending).Scan(&sending); err != nil {
		return nil, fmt.Errorf("checking the printer: %w", err)
	}
	if sending > 0 {
		return nil, nil
	}
	b := &SpoolBatch{PrinterID: printerID}
	var template, createdBy, pool sql.NullString
	// Its own batches, or a pool's no printer holds, when it is still in
	// that job: one that pulls, or any member once no puller is left.
	err = s.TxQueryRow(tx, `SELECT b.id, b.job_id, j.template_id, j.template_name, j.created_by, j.created_by_token, j.pool_id
		FROM spool_batches b JOIN spool_jobs j ON j.id = b.job_id
		WHERE b.state = ? AND (b.printer_id = ? OR (b.printer_id IS NULL AND EXISTS (
			SELECT 1 FROM spool_job_members m WHERE m.job_id = b.job_id AND m.printer_id = ? AND m.dropped = FALSE
			AND (m.pulls = TRUE OR NOT EXISTS (SELECT 1 FROM spool_job_members p WHERE p.job_id = b.job_id AND p.pulls = TRUE AND p.dropped = FALSE)))))
		ORDER BY j.created_at, j.id, b.seq LIMIT 1`, models.SpoolWaiting, printerID, printerID).
		Scan(&b.ID, &b.JobID, &template, &b.TemplateName, &createdBy, &b.CreatedByToken, &pool)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("finding a batch to send: %w", err)
	}
	if template.Valid {
		b.TemplateID = &template.String
	}
	b.CreatedBy, b.PoolID = createdBy.String, pool.String
	now := dbTime(time.Now())
	_, err = s.TxExec(tx, `UPDATE spool_batches SET state = ?, started_at = ?, heartbeat_at = ?, printer_id = ?,
		printer_name = (SELECT name FROM printers WHERE id = ?) WHERE id = ?`, models.SpoolSending, now, now, printerID, printerID, b.ID)
	if err != nil {
		return nil, fmt.Errorf("claiming a batch: %w", err)
	}
	if err := s.updateJobState(tx, b.JobID); err != nil {
		return nil, err
	}
	rows, err := s.TxQuery(tx, "SELECT id, asset_id, copies FROM spool_entries WHERE batch_id = ? AND printed_at IS NULL ORDER BY added_at, id", b.ID)
	if err != nil {
		return nil, fmt.Errorf("reading a batch: %w", err)
	}
	for rows.Next() {
		var e SpoolBatchEntry
		if err := rows.Scan(&e.ID, &e.AssetID, &e.Copies); err != nil {
			rows.Close()
			return nil, err
		}
		b.Entries = append(b.Entries, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return b, tx.Commit()
}

// HeartbeatSpoolBatch says a batch is still being sent.
func (s *Store) HeartbeatSpoolBatch(id string) error {
	if _, err := s.Exec("UPDATE spool_batches SET heartbeat_at = ? WHERE id = ? AND state = ?", dbTime(time.Now()), id, models.SpoolSending); err != nil {
		return fmt.Errorf("recording a heartbeat: %w", err)
	}
	return nil
}

// SpoolOutcome is how sending a batch went: the entries printed, on the
// printer named; those whose labels could not be produced, with why; and,
// when the batch failed as a whole, why, which its remaining entries take
// as their problem.
type SpoolOutcome struct {
	Printed  []string
	Printer  string
	Problems map[string]string
	Failure  string
	// HandBack, with Failure, gives a pool batch back to the rest of the
	// pool, the printer dropping out of the job, rather than failing it.
	HandBack bool
	// Moves give entries that could not be produced another template, by
	// entry ID, with their problem saying so.
	Moves map[string]string
}

// FinishSpoolBatch records how sending a batch went, with the labels
// printed attributed to the acting user or token, who asked for the job.
func (s *Store) FinishSpoolBatch(b *SpoolBatch, o SpoolOutcome) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if err := s.takeLock(tx, "spool"); err != nil {
		return err
	}
	// The sweep may have given it up already, its labels back in the spool
	// as perhaps printed; they stay so.
	var state string
	if err := s.TxQueryRow(tx, "SELECT state FROM spool_batches WHERE id = ?", b.ID).Scan(&state); err != nil {
		return fmt.Errorf("reading a batch: %w", err)
	}
	if state != models.SpoolSending {
		return ErrSpoolBatchGiven
	}
	if o.HandBack && b.PoolID != "" {
		err = s.handBackTx(tx, b, o)
	} else {
		err = s.finishBatchTx(tx, b.ID, b.JobID, b.TemplateName, o)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

// recordProblemsTx records why entries could not be produced, and moves
// those the outcome moves to their other templates.
func (s *Store) recordProblemsTx(tx *sql.Tx, o SpoolOutcome) error {
	for id, problem := range o.Problems {
		if _, err := s.TxExec(tx, "UPDATE spool_entries SET problem = ? WHERE id = ? AND printed_at IS NULL", clip(problem), id); err != nil {
			return fmt.Errorf("recording a spool problem: %w", err)
		}
	}
	for id, template := range o.Moves {
		if _, err := s.TxExec(tx, "UPDATE spool_entries SET template_id = ? WHERE id = ? AND printed_at IS NULL AND template_from_category = ?", template, id, true); err != nil {
			return fmt.Errorf("moving a label to another template: %w", err)
		}
	}
	return nil
}

// handBackTx gives a pool batch its printer could not print back to the
// rest of the pool, with any other batches that printer was given, and
// drops the printer from the job. With no member left, the job's waiting
// batches fail with why the last could not print them.
func (s *Store) handBackTx(tx *sql.Tx, b *SpoolBatch, o SpoolOutcome) error {
	if err := s.recordProblemsTx(tx, o); err != nil {
		return err
	}
	if _, err := s.TxExec(tx, "UPDATE spool_job_members SET dropped = TRUE WHERE job_id = ? AND printer_id = ?", b.JobID, b.PrinterID); err != nil {
		return fmt.Errorf("dropping a printer from a job: %w", err)
	}
	_, err := s.TxExec(tx, `UPDATE spool_batches SET state = ?, printer_id = NULL, printer_name = '', started_at = NULL, heartbeat_at = NULL
		WHERE id = ? OR (job_id = ? AND state = ? AND printer_id = ?)`, models.SpoolWaiting, b.ID, b.JobID, models.SpoolWaiting, b.PrinterID)
	if err != nil {
		return fmt.Errorf("handing a batch back: %w", err)
	}
	var left int
	if err := s.TxQueryRow(tx, "SELECT COUNT(*) FROM spool_job_members WHERE job_id = ? AND dropped = FALSE", b.JobID).Scan(&left); err != nil {
		return fmt.Errorf("counting a job's printers: %w", err)
	}
	if left == 0 {
		if err := s.failWaitingTx(tx, b.JobID, "No printer of the pool could print these. The last: "+o.Failure); err != nil {
			return err
		}
	}
	return s.updateJobState(tx, b.JobID)
}

// failWaitingTx fails a job's waiting batches no printer holds, within tx.
func (s *Store) failWaitingTx(tx *sql.Tx, jobID, failure string) error {
	rows, err := s.TxQuery(tx, "SELECT id FROM spool_batches WHERE job_id = ? AND state = ? AND printer_id IS NULL", jobID, models.SpoolWaiting)
	if err != nil {
		return fmt.Errorf("finding a job's batches: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if err := s.finishBatchTx(tx, id, jobID, "", SpoolOutcome{Failure: failure}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) finishBatchTx(tx *sql.Tx, batchID, jobID, template string, o SpoolOutcome) error {
	if len(o.Printed) > 0 {
		if err := s.markPrintedTx(tx, o.Printed, template, o.Printer); err != nil {
			return err
		}
	}
	if err := s.recordProblemsTx(tx, o); err != nil {
		return err
	}
	state := models.SpoolDone
	if o.Failure != "" {
		state = models.SpoolFailed
		if _, err := s.TxExec(tx, "UPDATE spool_entries SET problem = ? WHERE batch_id = ? AND printed_at IS NULL AND problem = ''", clip(o.Failure), batchID); err != nil {
			return fmt.Errorf("recording a spool problem: %w", err)
		}
	}
	_, err := s.TxExec(tx, "UPDATE spool_batches SET state = ?, error = ?, finished_at = ? WHERE id = ?", state, clip(o.Failure), dbTime(time.Now()), batchID)
	if err != nil {
		return fmt.Errorf("finishing a batch: %w", err)
	}
	return s.updateJobState(tx, jobID)
}

// clip keeps a message within its column.
func clip(msg string) string {
	if len(msg) > 500 {
		return msg[:497] + "..."
	}
	return msg
}

// updateJobState sets a job's state from its batches': waiting until one
// starts, sending until none is left waiting or sending, then failed if
// one failed, else done (or canceled).
func (s *Store) updateJobState(tx *sql.Tx, jobID string) error {
	counts := map[string]int{}
	rows, err := s.TxQuery(tx, "SELECT state, COUNT(*) FROM spool_batches WHERE job_id = ? GROUP BY state", jobID)
	if err != nil {
		return fmt.Errorf("reading a job's batches: %w", err)
	}
	for rows.Next() {
		var st string
		var n int
		if err := rows.Scan(&st, &n); err != nil {
			rows.Close()
			return err
		}
		counts[st] = n
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	var errMsg string
	if counts[models.SpoolFailed] > 0 {
		if err := s.TxQueryRow(tx, "SELECT error FROM spool_batches WHERE job_id = ? AND state = ? ORDER BY finished_at DESC, seq LIMIT 1", jobID, models.SpoolFailed).Scan(&errMsg); err != nil {
			return fmt.Errorf("reading a job's failure: %w", err)
		}
	}
	state := models.SpoolDone
	switch {
	case counts[models.SpoolSending] > 0 || counts[models.SpoolWaiting] > 0 && (counts[models.SpoolDone] > 0 || counts[models.SpoolFailed] > 0):
		state = models.SpoolSending
	case counts[models.SpoolWaiting] > 0:
		state = models.SpoolWaiting
	case counts[models.SpoolFailed] > 0:
		state = models.SpoolFailed
	case counts[models.SpoolCanceled] > 0 && counts[models.SpoolDone] == 0:
		state = models.SpoolCanceled
	}
	now := dbTime(time.Now())
	set := "state = ?, error = ?"
	args := []any{state, errMsg}
	if state == models.SpoolSending {
		set += ", started_at = COALESCE(started_at, ?)"
		args = append(args, now)
	}
	if state == models.SpoolDone || state == models.SpoolFailed || state == models.SpoolCanceled {
		set += ", finished_at = ?"
		args = append(args, now)
	}
	if _, err := s.TxExec(tx, "UPDATE spool_jobs SET "+set+" WHERE id = ?", append(args, jobID)...); err != nil {
		return fmt.Errorf("updating a job: %w", err)
	}
	return nil
}

// CancelSpoolJob cancels a job none of whose batches has started; its
// entries wait in the spool again.
func (s *Store) CancelSpoolJob(id string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if err := s.takeLock(tx, "spool"); err != nil {
		return err
	}
	var state string
	err = s.TxQueryRow(tx, "SELECT state FROM spool_jobs WHERE id = ?", id).Scan(&state)
	if err == sql.ErrNoRows {
		return ErrSpoolJobNotFound
	}
	if err != nil {
		return fmt.Errorf("reading a job: %w", err)
	}
	if state != models.SpoolWaiting {
		return ErrSpoolJobStarted
	}
	if _, err := s.TxExec(tx, "UPDATE spool_batches SET state = ?, finished_at = ? WHERE job_id = ?", models.SpoolCanceled, dbTime(time.Now()), id); err != nil {
		return fmt.Errorf("canceling a job: %w", err)
	}
	if err := s.updateJobState(tx, id); err != nil {
		return err
	}
	return tx.Commit()
}

// SweepSpool puts back what a stopped server left: batches still sending
// whose heartbeat is older than staleBefore fail, their labels waiting
// again marked as perhaps printed; and batches waiting for a printer that
// was deleted fail too. It returns how many batches it failed.
func (s *Store) SweepSpool(staleBefore time.Time) (int, error) {
	type stale struct {
		id, job, printer string
		pool             bool
	}
	var found []stale
	rows, err := s.Query(`SELECT id, job_id, printer_name FROM spool_batches WHERE state = ? AND heartbeat_at < ?`, models.SpoolSending, dbTime(staleBefore))
	if err != nil {
		return 0, fmt.Errorf("finding stopped batches: %w", err)
	}
	for rows.Next() {
		var b stale
		if err := rows.Scan(&b.id, &b.job, &b.printer); err != nil {
			rows.Close()
			return 0, err
		}
		found = append(found, b)
	}
	rows.Close()
	var orphans []stale
	// Waiting for a printer since deleted, or for a pool none of whose
	// printers is left in the job.
	rows, err = s.Query(`SELECT b.id, b.job_id, COALESCE(NULLIF(b.printer_name, ''), j.target_name), j.pool_id IS NOT NULL FROM spool_batches b JOIN spool_jobs j ON j.id = b.job_id
		WHERE b.state = ? AND b.printer_id IS NULL AND j.to_download = FALSE AND (j.pool_id IS NULL OR NOT EXISTS (
			SELECT 1 FROM spool_job_members m WHERE m.job_id = j.id AND m.dropped = FALSE))`, models.SpoolWaiting)
	if err != nil {
		return 0, fmt.Errorf("finding orphaned batches: %w", err)
	}
	for rows.Next() {
		var b stale
		if err := rows.Scan(&b.id, &b.job, &b.printer, &b.pool); err != nil {
			rows.Close()
			return 0, err
		}
		orphans = append(orphans, b)
	}
	rows.Close()

	n := 0
	finish := func(b stale, state, failure string) error {
		tx, err := s.DB.Begin()
		if err != nil {
			return fmt.Errorf("beginning transaction: %w", err)
		}
		defer tx.Rollback()
		if err := s.takeLock(tx, "spool"); err != nil {
			return err
		}
		// Only if it is still as found: its own server may have finished it.
		var now string
		if err := s.TxQueryRow(tx, "SELECT state FROM spool_batches WHERE id = ?", b.id).Scan(&now); err != nil || now != state {
			return err
		}
		if err := s.finishBatchTx(tx, b.id, b.job, "", SpoolOutcome{Failure: failure}); err != nil {
			return err
		}
		n++
		return tx.Commit()
	}
	for _, b := range found {
		msg := fmt.Sprintf("The server stopped while sending these to %s; they may have printed. Check before printing them again", b.printer)
		if err := finish(b, models.SpoolSending, msg); err != nil {
			return n, err
		}
	}
	for _, b := range orphans {
		msg := fmt.Sprintf("%s was deleted before these were sent", b.printer)
		if b.pool {
			msg = fmt.Sprintf("No printer of %s is left to print these", b.printer)
		}
		if err := finish(b, models.SpoolWaiting, msg); err != nil {
			return n, err
		}
	}
	return n, nil
}

const spoolJobColumns = `j.id, j.template_id, j.template_name, j.printer_id, j.target_name, j.to_download, j.state, j.error, j.labels,
	(SELECT COALESCE(SUM(e.copies), 0) FROM spool_entries e JOIN spool_batches b ON b.id = e.batch_id WHERE b.job_id = j.id AND e.printed_at IS NOT NULL),
	(SELECT COUNT(*) FROM spool_entries e JOIN spool_batches b ON b.id = e.batch_id WHERE b.job_id = j.id),
	j.created_by, COALESCE(cu.name, ct.name, ''), j.created_by_token, j.created_at, j.started_at, j.finished_at, j.pool_id`

// ListSpoolJobs lists jobs made since since (all when zero), newest first;
// with IDs, only those.
func (s *Store) ListSpoolJobs(since time.Time, ids []string) ([]models.SpoolJob, error) {
	where, args := []string{"1 = 1"}, []any{}
	if !since.IsZero() {
		where, args = append(where, "j.created_at >= ?"), append(args, dbTime(since))
	}
	if ids != nil {
		if len(ids) == 0 {
			return []models.SpoolJob{}, nil
		}
		where = append(where, "j.id IN ("+placeholders(len(ids))+")")
		for _, id := range ids {
			args = append(args, id)
		}
	}
	rows, err := s.Query(`SELECT `+spoolJobColumns+` FROM spool_jobs j
		LEFT JOIN users cu ON cu.id = j.created_by LEFT JOIN api_tokens ct ON ct.id = j.created_by
		WHERE `+strings.Join(where, " AND ")+` ORDER BY j.created_at DESC, j.id`, args...)
	if err != nil {
		return nil, fmt.Errorf("listing print jobs: %w", err)
	}
	defer rows.Close()
	jobs := []models.SpoolJob{}
	for rows.Next() {
		var (
			j                 models.SpoolJob
			template, printer sql.NullString
			by                sql.NullString
			started, finished sql.NullTime
			pool              sql.NullString
		)
		err := rows.Scan(&j.ID, &template, &j.TemplateName, &printer, &j.Target, &j.Download, &j.State, &j.Error, &j.Labels, &j.Printed, &j.Entries,
			&by, &j.CreatedBy.Name, &j.CreatedBy.Token, &j.CreatedAt, &started, &finished, &pool)
		if err != nil {
			return nil, fmt.Errorf("scanning a print job: %w", err)
		}
		if template.Valid {
			j.TemplateID = &template.String
		}
		if printer.Valid {
			j.PrinterID = &printer.String
		}
		if pool.Valid {
			j.PoolID = &pool.String
		}
		j.CreatedBy.ID = by.String
		if started.Valid {
			t := started.Time.UTC()
			j.StartedAt = &t
		}
		if finished.Valid {
			t := finished.Time.UTC()
			j.FinishedAt = &t
		}
		jobs = append(jobs, j)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	return jobs, s.jobMembers(jobs)
}

// jobMembers fills in, for pool jobs, how each member is getting on.
func (s *Store) jobMembers(jobs []models.SpoolJob) error {
	for i := range jobs {
		if jobs[i].PoolID == nil {
			continue
		}
		rows, err := s.Query(`SELECT m.printer_id, COALESCE(p.name, ''), m.dropped,
			(SELECT COALESCE(SUM(e.copies), 0) FROM spool_entries e JOIN spool_batches b ON b.id = e.batch_id
				WHERE b.job_id = m.job_id AND b.printer_id = m.printer_id AND e.printed_at IS NOT NULL),
			(SELECT COUNT(*) FROM spool_batches b WHERE b.job_id = m.job_id AND b.printer_id = m.printer_id AND b.state = ?)
			FROM spool_job_members m LEFT JOIN printers p ON p.id = m.printer_id WHERE m.job_id = ? ORDER BY p.name, m.printer_id`,
			models.SpoolSending, jobs[i].ID)
		if err != nil {
			return fmt.Errorf("reading a job's printers: %w", err)
		}
		jobs[i].Members = []models.SpoolJobMember{}
		for rows.Next() {
			var m models.SpoolJobMember
			var sending int
			if err := rows.Scan(&m.PrinterID, &m.Printer, &m.Dropped, &m.Printed, &sending); err != nil {
				rows.Close()
				return err
			}
			m.Sending = sending > 0
			jobs[i].Members = append(jobs[i].Members, m)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
	}
	return nil
}

// TokenPermissions reads an API token that still works, and its
// permissions, by its ID.
func (s *Store) TokenPermissions(id string) (*models.APIToken, map[string]bool, error) {
	t, err := s.GetToken(id)
	if err != nil {
		return nil, nil, err
	}
	if t.RevokedAt != nil || (t.ExpiresAt != nil && !t.ExpiresAt.After(time.Now())) {
		return nil, nil, ErrTokenNotFound
	}
	perms, err := s.tokenKeys(id)
	if err != nil {
		return nil, nil, fmt.Errorf("reading token permissions: %w", err)
	}
	return t, perms, nil
}
