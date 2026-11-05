package store

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// RelayPrinters lists, by relay name, the printers reached through each
// relay, with the last print job sent to each.
func (s *Store) RelayPrinters() (map[string][]models.RelayPrinter, error) {
	rows, err := s.Query("SELECT id, name, relay FROM printers WHERE relay <> '' ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("listing printers through relays: %w", err)
	}
	type printer struct{ id, name, relay string }
	var list []printer
	for rows.Next() {
		var p printer
		if err := rows.Scan(&p.id, &p.name, &p.relay); err != nil {
			rows.Close()
			return nil, err
		}
		list = append(list, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := map[string][]models.RelayPrinter{}
	for _, p := range list {
		rp := models.RelayPrinter{ID: p.id, Name: p.name}
		var job models.RelayPrinterJob
		var finished sql.NullTime
		var created time.Time
		err := s.QueryRow("SELECT state, error, created_at, finished_at FROM spool_jobs WHERE printer_id = ? ORDER BY created_at DESC, id DESC LIMIT 1", p.id).
			Scan(&job.State, &job.Error, &created, &finished)
		switch {
		case err == sql.ErrNoRows:
		case err != nil:
			return nil, fmt.Errorf("reading a printer's last job: %w", err)
		default:
			job.At = created.UTC()
			if finished.Valid {
				job.At = finished.Time.UTC()
			}
			rp.LastJob = &job
		}
		out[p.relay] = append(out[p.relay], rp)
	}
	return out, nil
}
