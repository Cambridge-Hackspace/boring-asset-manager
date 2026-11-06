package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/release"
)

// The instance's own records of the release it runs and the update check
// (docs/installing.md), kept as settings.
const (
	// versionSetting is the release last started, and schemaSetting the
	// schema's version then.
	versionSetting = "instance.version"
	schemaSetting  = "instance.schema"
	// checkedSetting is when the update check last ran, and
	// announcedSetting the newest release it announced.
	checkedSetting   = "updates.checked_at"
	announcedSetting = "updates.announced"
)

// NoteVersion records, at startup, the release this server runs, after
// migrating. When it is not the release recorded before, it records
// instance.upgraded with it, saying so when it is older, in the same
// transaction: claimed by changing the recorded release only while it is
// still the old one, so servers starting at once on a new release record
// it once. The first start records the release alone, and a build that is
// not a release records nothing. notesURL is the page of the release's
// notes, if known. It answers what it recorded, or nil.
func (s *Store) NoteVersion(version, notesURL string) (*models.EventInstance, error) {
	if !release.Is(version) {
		return nil, nil
	}
	schema, err := s.SchemaVersion()
	if err != nil {
		return nil, err
	}
	was, err := s.Setting(versionSetting, "")
	if err != nil || was == version {
		return nil, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	// The claim comes first, so that on SQLite the transaction writes from
	// the start, rather than reading and then finding another wrote.
	claimed, err := s.swapSetting(tx, versionSetting, was, version)
	if err != nil || !claimed {
		return nil, err
	}
	schemaFrom := 0
	if was != "" {
		var v string
		err := s.TxQueryRow(tx, "SELECT value FROM settings WHERE setting_key = ?", schemaSetting).Scan(&v)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("reading setting %s: %w", schemaSetting, err)
		}
		schemaFrom, _ = strconv.Atoi(v)
	}
	if err := s.setSettingTx(tx, schemaSetting, strconv.Itoa(schema)); err != nil {
		return nil, err
	}
	if was == "" {
		return nil, tx.Commit()
	}
	inst := &models.EventInstance{From: was, To: version, SchemaFrom: schemaFrom, SchemaTo: schema,
		Downgrade: release.Newer(was, version), NotesURL: notesURL}
	if err := s.As("").insertEvent(tx, models.EventInstanceUpgraded, nil, nil, models.EventPayload{Trigger: models.TriggerSchedule, Instance: inst}); err != nil {
		return nil, err
	}
	return inst, tx.Commit()
}

// RecordUpdateFailed records instance.update_failed, for bam-update once it
// has put the release it replaced back: after restoring the database, which
// would have taken with it anything recorded before.
func (s *Store) RecordUpdateFailed(inst models.EventInstance) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if err := s.As("").insertEvent(tx, models.EventInstanceUpdateFailed, nil, nil, models.EventPayload{Trigger: models.TriggerSchedule, Instance: &inst}); err != nil {
		return err
	}
	return tx.Commit()
}

// ClaimUpdateCheck claims the update check due at now, when none has run
// for every, answering false when one has, or when another server sharing
// the database claimed it first. A check claimed counts as run, whatever
// its outcome, so one that fails is tried again only after every.
func (s *Store) ClaimUpdateCheck(now time.Time, every time.Duration) (bool, error) {
	was, err := s.Setting(checkedSetting, "")
	if err != nil {
		return false, err
	}
	if last, err := time.Parse(time.RFC3339Nano, was); err == nil && now.Sub(last) < every {
		return false, nil
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return false, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	claimed, err := s.swapSetting(tx, checkedSetting, was, now.UTC().Format(time.RFC3339Nano))
	if err != nil || !claimed {
		return false, err
	}
	return true, tx.Commit()
}

// AnnounceRelease records instance.update_available for a newer release,
// inst.To, unless it or a newer one was announced before, answering
// whether it did. A release passed over by a newer one before any check
// found it is never announced.
func (s *Store) AnnounceRelease(inst models.EventInstance) (bool, error) {
	was, err := s.Setting(announcedSetting, "")
	if err != nil {
		return false, err
	}
	if was != "" && !release.Newer(inst.To, was) {
		return false, nil
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return false, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	claimed, err := s.swapSetting(tx, announcedSetting, was, inst.To)
	if err != nil || !claimed {
		return false, err
	}
	if err := s.As("").insertEvent(tx, models.EventInstanceUpdateAvailable, nil, nil, models.EventPayload{Trigger: models.TriggerSchedule, Instance: &inst}); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// swapSetting changes a setting from was, empty for none, to value, only
// while it is still was, answering false when another changed it first:
// with a conditional update, or an insert that fails when another inserted
// it. A false answer may leave tx unusable, so it is then rolled back.
func (s *Store) swapSetting(tx *sql.Tx, key, was, value string) (bool, error) {
	now := dbTime(time.Now())
	if was == "" {
		_, err := s.TxExec(tx, "INSERT INTO settings (setting_key, value, updated_at) VALUES (?, ?, ?)", key, value, now)
		if IsUniqueViolation(err) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("writing setting %s: %w", key, err)
		}
		return true, nil
	}
	res, err := s.TxExec(tx, "UPDATE settings SET value = ?, updated_at = ? WHERE setting_key = ? AND value = ?", value, now, key, was)
	if err != nil {
		return false, fmt.Errorf("writing setting %s: %w", key, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("writing setting %s: %w", key, err)
	}
	return n == 1, nil
}
