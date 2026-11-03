package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// The delivery log and failing channels (#196, docs/notifications.md).

var (
	ErrDeliveryNotFound = errors.New("delivery not found")
	// ErrDeliveryNotFailed is returned for retrying a delivery that has not
	// failed: one sent, or still being tried.
	ErrDeliveryNotFailed = errors.New("only a failed delivery can be sent again")
	// ErrChannelOff is returned for retrying a delivery whose channel is off.
	ErrChannelOff = errors.New("the channel is off; turn it on first")
)

// ChannelPolicy says when a channel that keeps failing is turned off: when
// every attempt has failed for FailingFor, or, if Failures is set, after
// that many failed attempts in a row. Zero values turn neither off.
type ChannelPolicy struct {
	FailingFor time.Duration
	Failures   int
}

// RecordChannelResult counts an attempt at one of a channel's deliveries:
// a success starts the count again, and a failure adds to it. A channel
// failing past the policy is turned off, saying why, and notifier.disabled
// is recorded, once, however many servers see it fail at the same moment.
// It reports whether this call turned the channel off.
func (s *Store) RecordChannelResult(channelID string, ok bool, now time.Time, policy ChannelPolicy) (bool, error) {
	now = dbTime(now)
	if ok {
		_, err := s.Exec(`UPDATE notification_channels SET failures = 0, failing_since = NULL WHERE id = ? AND failures <> 0`, channelID)
		if err != nil {
			return false, fmt.Errorf("counting the channel's success: %w", err)
		}
		return false, nil
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return false, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := s.TxExec(tx, `UPDATE notification_channels SET failures = failures + 1, failing_since = COALESCE(failing_since, ?) WHERE id = ?`,
		now, channelID); err != nil {
		return false, fmt.Errorf("counting the channel's failure: %w", err)
	}
	var (
		c       models.EventChannel
		enabled bool
		count   int
		since   sql.NullTime
	)
	if err := s.TxQueryRow(tx, `SELECT id, name, driver, enabled, failures, failing_since FROM notification_channels WHERE id = ?`, channelID).
		Scan(&c.ID, &c.Name, &c.Driver, &enabled, &count, &since); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("reading the channel: %w", err)
	}
	reason := ""
	switch {
	case !enabled:
	case policy.Failures > 0 && count >= policy.Failures:
		reason = "Turned off after " + strconv.Itoa(count) + " failed attempts in a row."
	case policy.FailingFor > 0 && since.Valid && !now.Before(since.Time.Add(policy.FailingFor)):
		reason = "Turned off after failing every attempt for " + hoursOf(policy.FailingFor) + ", since " + since.Time.UTC().Format("15:04 on 2 January 2006") + " (UTC)."
	}
	turnedOff := false
	if reason != "" {
		res, err := s.TxExec(tx, `UPDATE notification_channels SET enabled = ?, disabled_reason = ?, updated_at = ? WHERE id = ? AND enabled = ?`,
			false, reason, now, channelID, true)
		if err != nil {
			return false, fmt.Errorf("turning the channel off: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 1 {
			turnedOff = true
			// Recorded by nobody: BAM! turned it off itself.
			if err := s.As("").insertEvent(tx, models.EventNotifierDisabled, nil, nil, models.EventPayload{
				Trigger: models.TriggerThreshold, Channel: &c, Note: reason,
			}); err != nil {
				return false, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("committing: %w", err)
	}
	return turnedOff, nil
}

// hoursOf says a duration in whole hours, or minutes when shorter.
func hoursOf(d time.Duration) string {
	if d >= time.Hour && d%time.Hour == 0 {
		h := int(d / time.Hour)
		if h == 1 {
			return "1 hour"
		}
		return strconv.Itoa(h) + " hours"
	}
	m := int(d.Round(time.Minute) / time.Minute)
	if m == 1 {
		return "1 minute"
	}
	return strconv.Itoa(m) + " minutes"
}

// RetryDelivery queues a failed delivery to be sent again at once, by hand.
// Its attempts start again; its record of the last attempt stays until the
// next one replaces it.
func (s *Store) RetryDelivery(id string, now time.Time) error {
	var status string
	var enabled bool
	err := s.QueryRow(`SELECT d.status, c.enabled FROM notification_deliveries d JOIN notification_channels c ON c.id = d.channel_id WHERE d.id = ?`, id).
		Scan(&status, &enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrDeliveryNotFound
	}
	if err != nil {
		return fmt.Errorf("reading the delivery: %w", err)
	}
	if !enabled {
		return ErrChannelOff
	}
	res, err := s.Exec(`UPDATE notification_deliveries SET status = ?, attempts = 0, next_attempt_at = ?, finished_at = NULL WHERE id = ? AND status = ?`,
		models.DeliveryPending, dbTime(now), id, models.DeliveryFailed)
	if err != nil {
		return fmt.Errorf("queuing the delivery again: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrDeliveryNotFailed
	}
	return nil
}

// PruneDeliveries removes deliveries finished, sent or failed, before
// before, reporting how many. Those still being tried are kept.
func (s *Store) PruneDeliveries(before time.Time) (int64, error) {
	res, err := s.Exec(`DELETE FROM notification_deliveries WHERE status <> ? AND finished_at < ?`, models.DeliveryPending, dbTime(before))
	if err != nil {
		return 0, fmt.Errorf("pruning deliveries: %w", err)
	}
	return res.RowsAffected()
}
