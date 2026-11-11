package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers"
	"github.com/google/uuid"
)

// Lasers (docs/printing.md#lasers): one-shot printers' materials, as their
// admins tuned them, and the history of their jobs.

var (
	// ErrNotOneShot is returned for a laser's operation on a printer
	// whose driver is not a one-shot printer's.
	ErrNotOneShot = errors.New("that printer is not a laser")
	// ErrMaterialNotFound is returned for a material a printer does not
	// have.
	ErrMaterialNotFound = errors.New("material not found")
	// ErrLaserJobNotFound is returned for a laser job that is not there.
	ErrLaserJobNotFound = errors.New("laser job not found")
	// ErrTooManyMaterials is returned for a material past MaxMaterials.
	ErrTooManyMaterials = fmt.Errorf("%w: a printer has at most %d materials", printers.ErrInvalidMaterial, printers.MaxMaterials)
)

// oneShotPrinter reads a printer, and the model its driver says it is.
func (s *Store) oneShotPrinter(printerID string) (*models.Printer, printers.OneShotModel, error) {
	p, err := s.GetPrinter(printerID)
	if err != nil {
		return nil, printers.OneShotModel{}, err
	}
	if !printers.IsOneShot(p.Driver) {
		return nil, printers.OneShotModel{}, ErrNotOneShot
	}
	m, err := printers.OneShotModelOf(p.Driver, PrinterSettings(p))
	return p, m, err
}

// ListMaterials lists a one-shot printer's materials: its driver's presets
// first, in the driver's order, as they are or as tuned, then the
// printer's own, by name.
func (s *Store) ListMaterials(printerID string) ([]models.LaserMaterial, error) {
	_, model, err := s.oneShotPrinter(printerID)
	if err != nil {
		return nil, err
	}
	saved, err := s.savedMaterials(printerID)
	if err != nil {
		return nil, err
	}
	out := []models.LaserMaterial{}
	for _, preset := range model.Materials {
		if m, ok := saved[preset.ID]; ok {
			m.Preset, m.Tuned = true, true
			out = append(out, m)
			delete(saved, preset.ID)
			continue
		}
		out = append(out, models.LaserMaterial{Material: preset, Preset: true})
	}
	own := make([]models.LaserMaterial, 0, len(saved))
	for _, m := range saved {
		own = append(own, m)
	}
	sort.Slice(own, func(i, j int) bool {
		if own[i].Name != own[j].Name {
			return own[i].Name < own[j].Name
		}
		return own[i].ID < own[j].ID
	})
	return append(out, own...), nil
}

// Material reads one of a one-shot printer's materials.
func (s *Store) Material(printerID, materialID string) (models.LaserMaterial, error) {
	list, err := s.ListMaterials(printerID)
	if err != nil {
		return models.LaserMaterial{}, err
	}
	for _, m := range list {
		if m.ID == materialID {
			return m, nil
		}
	}
	return models.LaserMaterial{}, ErrMaterialNotFound
}

const materialColumns = `material_id, name, engrave_power, engrave_speed, engrave_passes, engrave_lpi, cut_power, cut_speed, cut_passes, from_back, updated_at`

// savedMaterials reads the materials saved for a printer, by ID.
func (s *Store) savedMaterials(printerID string) (map[string]models.LaserMaterial, error) {
	rows, err := s.Query("SELECT "+materialColumns+" FROM laser_materials WHERE printer_id = ?", printerID)
	if err != nil {
		return nil, fmt.Errorf("reading materials: %w", err)
	}
	defer rows.Close()
	out := map[string]models.LaserMaterial{}
	for rows.Next() {
		var m models.LaserMaterial
		var cutPower, cutSpeed sql.NullFloat64
		var cutPasses sql.NullInt64
		var updated time.Time
		if err := rows.Scan(&m.ID, &m.Name, &m.Engrave.Power, &m.Engrave.Speed, &m.Engrave.Passes, &m.Engrave.LPI,
			&cutPower, &cutSpeed, &cutPasses, &m.FromBack, &updated); err != nil {
			return nil, fmt.Errorf("reading materials: %w", err)
		}
		if cutPower.Valid && cutSpeed.Valid && cutPasses.Valid {
			m.Cut = &printers.CutSetting{Power: cutPower.Float64, Speed: cutSpeed.Float64, Passes: int(cutPasses.Int64)}
		}
		updated = updated.UTC()
		m.UpdatedAt = &updated
		out[m.ID] = m
	}
	return out, rows.Err()
}

// SaveMaterial saves one of a one-shot printer's materials: a preset
// tuned, under its ID, or one of the printer's own, made or changed.
// Errors wrap printers.ErrInvalidMaterial for one that is not valid.
func (s *Store) SaveMaterial(printerID string, m printers.Material) (models.LaserMaterial, error) {
	m, err := printers.ValidateMaterial(m)
	if err != nil {
		return models.LaserMaterial{}, err
	}
	list, err := s.ListMaterials(printerID)
	if err != nil {
		return models.LaserMaterial{}, err
	}
	preset, known := false, false
	for _, have := range list {
		if have.ID == m.ID {
			preset, known = have.Preset, true
		}
	}
	if !known && len(list) >= printers.MaxMaterials {
		return models.LaserMaterial{}, ErrTooManyMaterials
	}
	now := dbTime(time.Now())
	var cutPower, cutSpeed, cutPasses any
	if m.Cut != nil {
		cutPower, cutSpeed, cutPasses = m.Cut.Power, m.Cut.Speed, m.Cut.Passes
	}
	err = s.withTx(func(tx *sql.Tx) error {
		if _, err := s.TxExec(tx, "DELETE FROM laser_materials WHERE printer_id = ? AND material_id = ?", printerID, m.ID); err != nil {
			return err
		}
		_, err := s.TxExec(tx, "INSERT INTO laser_materials (printer_id, "+materialColumns+") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			printerID, m.ID, m.Name, m.Engrave.Power, m.Engrave.Speed, m.Engrave.Passes, m.Engrave.LPI, cutPower, cutSpeed, cutPasses, m.FromBack, now)
		return err
	})
	if err != nil {
		return models.LaserMaterial{}, fmt.Errorf("saving the material: %w", err)
	}
	return models.LaserMaterial{Material: m, Preset: preset, Tuned: preset, UpdatedAt: &now}, nil
}

// DeleteMaterial deletes one of a printer's own materials, or puts a
// tuned preset back as its driver has it.
func (s *Store) DeleteMaterial(printerID, materialID string) error {
	if _, err := s.Material(printerID, materialID); err != nil {
		return err
	}
	if _, err := s.Exec("DELETE FROM laser_materials WHERE printer_id = ? AND material_id = ?", printerID, materialID); err != nil {
		return fmt.Errorf("deleting the material: %w", err)
	}
	return nil
}

// withTx runs fn in a transaction, committing when it succeeds.
func (s *Store) withTx(fn func(tx *sql.Tx) error) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

const laserJobColumns = `id, printer_id, printer_name, template_id, template_name, asset_id, asset_name, material, placement, state, message,
	prepared_by, prepared_by_token, started_by, started_by_token, created_at, started_at, ended_at`

func scanLaserJob(row interface{ Scan(...any) error }) (*models.LaserJob, error) {
	var j models.LaserJob
	var printer, template, asset, message, preparedBy, startedBy sql.NullString
	var material, placement string
	var started, ended sql.NullTime
	if err := row.Scan(&j.ID, &printer, &j.PrinterName, &template, &j.TemplateName, &asset, &j.AssetName, &material, &placement, &j.State, &message,
		&preparedBy, &j.PreparedByToken, &startedBy, &j.StartedByToken, &j.CreatedAt, &started, &ended); err != nil {
		return nil, err
	}
	for _, v := range []struct {
		from sql.NullString
		to   **string
	}{{printer, &j.PrinterID}, {template, &j.TemplateID}, {asset, &j.AssetID}} {
		if v.from.Valid {
			id := v.from.String
			*v.to = &id
		}
	}
	j.Message, j.PreparedBy, j.StartedBy = message.String, preparedBy.String, startedBy.String
	if err := json.Unmarshal([]byte(material), &j.Material); err != nil {
		return nil, fmt.Errorf("reading laser job %s's material: %w", j.ID, err)
	}
	if err := json.Unmarshal([]byte(placement), &j.Placement); err != nil {
		return nil, fmt.Errorf("reading laser job %s's placement: %w", j.ID, err)
	}
	j.CreatedAt = j.CreatedAt.UTC()
	for _, t := range []struct {
		from sql.NullTime
		to   **time.Time
	}{{started, &j.StartedAt}, {ended, &j.EndedAt}} {
		if t.from.Valid {
			v := t.from.Time.UTC()
			*t.to = &v
		}
	}
	return &j, nil
}

// CreateLaserJob records a job prepared on a one-shot printer, by whoever
// the store acts as. A job prepared before on the same printer and never
// started is discarded: a printer holds one prepared job at a time.
func (s *Store) CreateLaserJob(j models.LaserJob) (*models.LaserJob, error) {
	material, err := json.Marshal(j.Material)
	if err != nil {
		return nil, err
	}
	placement, err := json.Marshal(j.Placement)
	if err != nil {
		return nil, err
	}
	j.ID, j.State, j.CreatedAt = uuid.NewString(), models.LaserJobPrepared, dbTime(time.Now())
	if id, ok := s.actorID().(string); ok {
		j.PreparedBy, j.PreparedByToken = id, s.actor == ""
	}
	var preparedBy any
	if j.PreparedBy != "" {
		preparedBy = j.PreparedBy
	}
	err = s.withTx(func(tx *sql.Tx) error {
		if j.PrinterID != nil {
			if _, err := s.TxExec(tx, "UPDATE laser_jobs SET state = ?, ended_at = ? WHERE printer_id = ? AND state = ?",
				models.LaserJobDiscarded, j.CreatedAt, *j.PrinterID, models.LaserJobPrepared); err != nil {
				return err
			}
		}
		_, err := s.TxExec(tx, "INSERT INTO laser_jobs (id, printer_id, printer_name, template_id, template_name, asset_id, asset_name, material, placement, state, prepared_by, prepared_by_token, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			j.ID, j.PrinterID, j.PrinterName, j.TemplateID, j.TemplateName, j.AssetID, j.AssetName, string(material), string(placement), j.State, preparedBy, j.PreparedByToken, j.CreatedAt)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("recording the laser job: %w", err)
	}
	return &j, nil
}

// GetLaserJob reads a laser job.
func (s *Store) GetLaserJob(id string) (*models.LaserJob, error) {
	j, err := scanLaserJob(s.QueryRow("SELECT "+laserJobColumns+" FROM laser_jobs WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrLaserJobNotFound
	}
	return j, err
}

// MaxLaserJobsListed bounds a list of laser jobs.
const MaxLaserJobsListed = 200

// ListLaserJobs lists a printer's jobs, or an asset's when assetID is set,
// newest first, at most limit of them.
func (s *Store) ListLaserJobs(printerID, assetID string, limit int) ([]models.LaserJob, error) {
	if limit <= 0 || limit > MaxLaserJobsListed {
		limit = MaxLaserJobsListed
	}
	where, args := "printer_id = ?", []any{printerID}
	if assetID != "" {
		where, args = "asset_id = ?", []any{assetID}
	}
	rows, err := s.Query("SELECT "+laserJobColumns+" FROM laser_jobs WHERE "+where+" ORDER BY created_at DESC, id LIMIT "+fmt.Sprint(limit), args...)
	if err != nil {
		return nil, fmt.Errorf("listing laser jobs: %w", err)
	}
	defer rows.Close()
	out := []models.LaserJob{}
	for rows.Next() {
		j, err := scanLaserJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *j)
	}
	return out, rows.Err()
}

// ErrLaserJobState is returned when a laser job is not in a state that
// allows what was asked: started twice, or changed after it ended.
var ErrLaserJobState = errors.New("the laser job is not in a state that allows that")

// StartLaserJob records a prepared job as started, by whoever the store
// acts as, in state ready or running. It fails with ErrLaserJobState
// unless the job is prepared, so a job starts once.
func (s *Store) StartLaserJob(id, state string) (*models.LaserJob, error) {
	now := dbTime(time.Now())
	var by any
	token := false
	if id, ok := s.actorID().(string); ok {
		by, token = id, s.actor == ""
	}
	res, err := s.Exec("UPDATE laser_jobs SET state = ?, started_by = ?, started_by_token = ?, started_at = ? WHERE id = ? AND state = ?",
		state, by, token, now, id, models.LaserJobPrepared)
	if err != nil {
		return nil, fmt.Errorf("recording the laser job started: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return nil, err
	} else if n == 0 {
		if _, err := s.GetLaserJob(id); err != nil {
			return nil, err
		}
		return nil, ErrLaserJobState
	}
	return s.GetLaserJob(id)
}

// SetLaserJobState records a job's state as its machine reports it, with
// a message, the time it ended when it has. A job that has ended stays as
// it ended: it reports false, and changes nothing.
func (s *Store) SetLaserJobState(id, state, message string) (bool, error) {
	var ended any
	if models.LaserJobEnded(state) {
		ended = dbTime(time.Now())
	}
	var msg any
	if message != "" {
		msg = message
	}
	res, err := s.Exec("UPDATE laser_jobs SET state = ?, message = COALESCE(?, message), ended_at = ? WHERE id = ? AND state NOT IN (?, ?, ?, ?)",
		state, msg, ended, id, models.LaserJobDone, models.LaserJobCancelled, models.LaserJobFailed, models.LaserJobDiscarded)
	if err != nil {
		return false, fmt.Errorf("recording the laser job's state: %w", err)
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// RecordLabelEngraved records in an asset's history that a laser engraved
// its label, on a material.
func (s *Store) RecordLabelEngraved(assetID, template, printer, material string) error {
	return s.withTx(func(tx *sql.Tx) error {
		label := &models.EventLabel{Template: template, Printer: printer, Material: material, Copies: 1}
		return s.recordAssetEvent(tx, models.EventAssetLabelPrinted, assetID, models.EventPayload{Trigger: models.TriggerManual, Label: label})
	})
}
