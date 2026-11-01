package store

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/google/uuid"
)

// Printer pools (docs/printing.md): printers loaded alike that share print
// jobs from the spool.

const (
	// MaxPoolBatch bounds a pool's batch size.
	MaxPoolBatch = 100
	// MaxPoolShare bounds a member's share.
	MaxPoolShare = 100
)

var (
	ErrPoolNotFound   = errors.New("printer pool not found")
	ErrPoolNameExists = errors.New("a printer pool with that name already exists")
	// ErrInvalidPool is wrapped with what is wrong with a pool.
	ErrInvalidPool = errors.New("invalid printer pool")
)

// PoolInput is a pool to save.
type PoolInput struct {
	Name        string
	BatchSize   int
	FixedShares bool
	Members     []models.PoolMember
}

func (in *PoolInput) check() error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 255 {
		return fmt.Errorf("%w: a name of 1-255 characters is required", ErrInvalidPool)
	}
	if in.BatchSize == 0 {
		in.BatchSize = 10
	}
	if in.BatchSize < 1 || in.BatchSize > MaxPoolBatch {
		return fmt.Errorf("%w: batch_size must be 1-%d", ErrInvalidPool, MaxPoolBatch)
	}
	if len(in.Members) == 0 {
		return fmt.Errorf("%w: a pool needs at least one printer", ErrInvalidPool)
	}
	seen := map[string]bool{}
	for i, m := range in.Members {
		if seen[m.PrinterID] {
			return fmt.Errorf("%w: a printer is listed more than once", ErrInvalidPool)
		}
		seen[m.PrinterID] = true
		if m.Share == 0 {
			in.Members[i].Share = 1
		} else if m.Share < 1 || m.Share > MaxPoolShare {
			return fmt.Errorf("%w: shares must be 1-%d", ErrInvalidPool, MaxPoolShare)
		}
	}
	return nil
}

// ListPools lists the printer pools by name, with their members.
func (s *Store) ListPools() ([]models.PrinterPool, error) {
	rows, err := s.Query("SELECT id, name, batch_size, fixed_shares, created_at, updated_at FROM printer_pools ORDER BY name, id")
	if err != nil {
		return nil, fmt.Errorf("listing printer pools: %w", err)
	}
	pools := []models.PrinterPool{}
	byID := map[string]int{}
	for rows.Next() {
		var p models.PrinterPool
		if err := rows.Scan(&p.ID, &p.Name, &p.BatchSize, &p.FixedShares, &p.CreatedAt, &p.UpdatedAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scanning a printer pool: %w", err)
		}
		p.Members = []models.PoolMember{}
		byID[p.ID] = len(pools)
		pools = append(pools, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = s.Query(`SELECT m.pool_id, m.printer_id, m.share FROM printer_pool_members m JOIN printers p ON p.id = m.printer_id ORDER BY p.name, p.id`)
	if err != nil {
		return nil, fmt.Errorf("listing pool members: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var pool string
		var m models.PoolMember
		if err := rows.Scan(&pool, &m.PrinterID, &m.Share); err != nil {
			return nil, fmt.Errorf("scanning a pool member: %w", err)
		}
		if i, ok := byID[pool]; ok {
			pools[i].Members = append(pools[i].Members, m)
		}
	}
	return pools, rows.Err()
}

// GetPool reads a printer pool.
func (s *Store) GetPool(id string) (*models.PrinterPool, error) {
	pools, err := s.ListPools()
	if err != nil {
		return nil, err
	}
	for i := range pools {
		if pools[i].ID == id {
			return &pools[i], nil
		}
	}
	return nil, ErrPoolNotFound
}

// CreatePool saves a new printer pool.
func (s *Store) CreatePool(in PoolInput) (*models.PrinterPool, error) {
	return s.savePool("", in)
}

// UpdatePool replaces a printer pool's settings and members. Jobs already
// made keep the members they had.
func (s *Store) UpdatePool(id string, in PoolInput) (*models.PrinterPool, error) {
	return s.savePool(id, in)
}

func (s *Store) savePool(id string, in PoolInput) (*models.PrinterPool, error) {
	if err := in.check(); err != nil {
		return nil, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	var taken int
	if err := s.TxQueryRow(tx, "SELECT COUNT(*) FROM printer_pools WHERE name = ? AND id <> ?", in.Name, id).Scan(&taken); err != nil {
		return nil, fmt.Errorf("checking the pool's name: %w", err)
	}
	if taken > 0 {
		return nil, ErrPoolNameExists
	}
	for _, m := range in.Members {
		var n int
		if err := s.TxQueryRow(tx, "SELECT COUNT(*) FROM printers WHERE id = ?", m.PrinterID).Scan(&n); err != nil {
			return nil, fmt.Errorf("finding the printer: %w", err)
		}
		if n == 0 {
			return nil, ErrPrinterNotFound
		}
	}
	now := dbTime(time.Now())
	if id == "" {
		id = uuid.NewString()
		_, err = s.TxExec(tx, "INSERT INTO printer_pools (id, name, batch_size, fixed_shares, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
			id, in.Name, in.BatchSize, in.FixedShares, now, now)
	} else {
		var res sql.Result
		res, err = s.TxExec(tx, "UPDATE printer_pools SET name = ?, batch_size = ?, fixed_shares = ?, updated_at = ? WHERE id = ?",
			in.Name, in.BatchSize, in.FixedShares, now, id)
		if err == nil {
			if n, _ := res.RowsAffected(); n == 0 {
				return nil, ErrPoolNotFound
			}
		}
	}
	if err != nil {
		return nil, fmt.Errorf("saving the pool: %w", err)
	}
	if _, err := s.TxExec(tx, "DELETE FROM printer_pool_members WHERE pool_id = ?", id); err != nil {
		return nil, fmt.Errorf("saving the pool's members: %w", err)
	}
	for _, m := range in.Members {
		if _, err := s.TxExec(tx, "INSERT INTO printer_pool_members (pool_id, printer_id, share) VALUES (?, ?, ?)", id, m.PrinterID, m.Share); err != nil {
			return nil, fmt.Errorf("saving the pool's members: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing: %w", err)
	}
	return s.GetPool(id)
}

// DeletePool removes a printer pool. Labels waiting for it are left with
// nowhere to go, and its jobs' batches no printer has taken fail; those
// already given to a printer still print.
func (s *Store) DeletePool(id string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if err := s.takeLock(tx, "spool"); err != nil {
		return err
	}
	var name string
	err = s.TxQueryRow(tx, "SELECT name FROM printer_pools WHERE id = ?", id).Scan(&name)
	if err == sql.ErrNoRows {
		return ErrPoolNotFound
	}
	if err != nil {
		return fmt.Errorf("reading the pool: %w", err)
	}
	if _, err := s.TxExec(tx, "UPDATE spool_entries SET pool_id = NULL WHERE pool_id = ? AND printed_at IS NULL", id); err != nil {
		return fmt.Errorf("clearing the pool from the spool: %w", err)
	}
	type orphan struct{ id, job string }
	var orphans []orphan
	rows, err := s.TxQuery(tx, `SELECT b.id, b.job_id FROM spool_batches b JOIN spool_jobs j ON j.id = b.job_id
		WHERE j.pool_id = ? AND b.state = ? AND b.printer_id IS NULL`, id, models.SpoolWaiting)
	if err != nil {
		return fmt.Errorf("finding the pool's batches: %w", err)
	}
	for rows.Next() {
		var o orphan
		if err := rows.Scan(&o.id, &o.job); err != nil {
			rows.Close()
			return err
		}
		orphans = append(orphans, o)
	}
	rows.Close()
	for _, o := range orphans {
		if err := s.finishBatchTx(tx, o.id, o.job, "", SpoolOutcome{Failure: name + " was deleted before these were printed"}); err != nil {
			return err
		}
	}
	if _, err := s.TxExec(tx, "DELETE FROM printer_pools WHERE id = ?", id); err != nil {
		return fmt.Errorf("deleting the pool: %w", err)
	}
	return tx.Commit()
}

// PoolJobMember is a member of a pool job when it is made.
type PoolJobMember struct {
	PrinterID, PrinterName string
	Share                  int
	// Pulls is set for a member that takes batches as it frees up; one
	// that does not is given a fixed share.
	Pulls bool
}

// cutBatches cuts entries, with their copies, into batches of about size
// labels, keeping each entry's copies together.
func cutBatches(entries []SpoolBatchEntry, size int) [][]SpoolBatchEntry {
	var batches [][]SpoolBatchEntry
	var cur []SpoolBatchEntry
	labels := 0
	for _, e := range entries {
		if len(cur) > 0 && labels+e.Copies > size {
			batches = append(batches, cur)
			cur, labels = nil, 0
		}
		cur = append(cur, e)
		labels += e.Copies
	}
	if len(cur) > 0 {
		batches = append(batches, cur)
	}
	return batches
}

// shareOut decides which member would take each of n batches by their
// shares, interleaved (smooth weighted round-robin), so a member with
// twice the share takes every other batch rather than a run of them.
func shareOut(members []PoolJobMember, n int) []int {
	order := make([]int, len(members))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return members[order[a]].PrinterName < members[order[b]].PrinterName })
	total := 0
	for _, m := range members {
		total += m.Share
	}
	current := make([]int, len(members))
	picks := make([]int, n)
	for b := 0; b < n; b++ {
		best := -1
		for _, i := range order {
			current[i] += members[i].Share
			if best < 0 || current[i] > current[best] {
				best = i
			}
		}
		current[best] -= total
		picks[b] = best
	}
	return picks
}
