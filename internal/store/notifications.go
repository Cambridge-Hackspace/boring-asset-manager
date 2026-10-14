package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/google/uuid"
)

var (
	ErrChannelNotFound   = errors.New("notification channel not found")
	ErrChannelNameExists = errors.New("a notification channel with this name already exists")
)

// dbTime rounds a time to what every dialect stores (microseconds), in UTC.
func dbTime(t time.Time) time.Time { return t.UTC().Truncate(time.Microsecond) }

const channelColumns = "id, name, driver, options, enabled, events, created_at, updated_at"

func scanChannel(row interface{ Scan(...any) error }) (models.NotificationChannel, error) {
	var (
		c               models.NotificationChannel
		options, events string
	)
	if err := row.Scan(&c.ID, &c.Name, &c.Driver, &options, &c.Enabled, &events, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return c, err
	}
	if err := json.Unmarshal([]byte(options), &c.Options); err != nil {
		return c, fmt.Errorf("decoding options of channel %s: %w", c.ID, err)
	}
	if err := json.Unmarshal([]byte(events), &c.Events); err != nil {
		return c, fmt.Errorf("decoding events of channel %s: %w", c.ID, err)
	}
	if c.Options == nil {
		c.Options = map[string]string{}
	}
	if c.Events == nil {
		c.Events = []string{}
	}
	return c, nil
}

// channelJSON encodes a channel's options and events for storage.
func channelJSON(c *models.NotificationChannel) (string, string, error) {
	if c.Options == nil {
		c.Options = map[string]string{}
	}
	if c.Events == nil {
		c.Events = []string{}
	}
	options, err := json.Marshal(c.Options)
	if err != nil {
		return "", "", err
	}
	events, err := json.Marshal(c.Events)
	return string(options), string(events), err
}

func isUniqueViolation(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate")
}

// CreateChannel stores a new notification channel. The caller validates the
// driver and its options.
func (s *Store) CreateChannel(c models.NotificationChannel) (*models.NotificationChannel, error) {
	c.ID = uuid.NewString()
	c.CreatedAt = dbTime(time.Now())
	c.UpdatedAt = c.CreatedAt
	options, events, err := channelJSON(&c)
	if err != nil {
		return nil, err
	}
	_, err = s.Exec(`INSERT INTO notification_channels (`+channelColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.Name, c.Driver, options, c.Enabled, events, c.CreatedAt, c.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrChannelNameExists
		}
		return nil, fmt.Errorf("inserting channel: %w", err)
	}
	return &c, nil
}

// GetChannel reads a notification channel.
func (s *Store) GetChannel(id string) (*models.NotificationChannel, error) {
	c, err := scanChannel(s.QueryRow(`SELECT `+channelColumns+` FROM notification_channels WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, ErrChannelNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// ListChannels lists notification channels by name.
func (s *Store) ListChannels() ([]models.NotificationChannel, error) {
	rows, err := s.Query(`SELECT ` + channelColumns + ` FROM notification_channels ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("listing channels: %w", err)
	}
	defer rows.Close()
	list := []models.NotificationChannel{}
	for rows.Next() {
		c, err := scanChannel(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, c)
	}
	return list, rows.Err()
}

// UpdateChannel replaces a channel's name, driver, options, switch and
// subscriptions.
func (s *Store) UpdateChannel(c models.NotificationChannel) (*models.NotificationChannel, error) {
	existing, err := s.GetChannel(c.ID)
	if err != nil {
		return nil, err
	}
	c.CreatedAt = existing.CreatedAt
	c.UpdatedAt = dbTime(time.Now())
	options, events, err := channelJSON(&c)
	if err != nil {
		return nil, err
	}
	_, err = s.Exec(`UPDATE notification_channels SET name = ?, driver = ?, options = ?, enabled = ?, events = ?, updated_at = ? WHERE id = ?`,
		c.Name, c.Driver, options, c.Enabled, events, c.UpdatedAt, c.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrChannelNameExists
		}
		return nil, fmt.Errorf("updating channel: %w", err)
	}
	return &c, nil
}

// DeleteChannel deletes a channel and its deliveries.
func (s *Store) DeleteChannel(id string) error {
	res, err := s.Exec(`DELETE FROM notification_channels WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting channel: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrChannelNotFound
	}
	return nil
}

// fanOut queues an event for every enabled channel subscribed to its kind,
// within the transaction that records it.
func (s *Store) fanOut(tx *sql.Tx, eventID, kind string, now time.Time) error {
	rows, err := s.TxQuery(tx, `SELECT id, events FROM notification_channels WHERE enabled = ?`, true)
	if err != nil {
		return fmt.Errorf("reading channels: %w", err)
	}
	var targets []string
	for rows.Next() {
		var id, events string
		if err := rows.Scan(&id, &events); err != nil {
			rows.Close()
			return err
		}
		var kinds []string
		if err := json.Unmarshal([]byte(events), &kinds); err != nil {
			rows.Close()
			return fmt.Errorf("decoding events of channel %s: %w", id, err)
		}
		for _, k := range kinds {
			if k == kind {
				targets = append(targets, id)
				break
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, channelID := range targets {
		_, err := s.TxExec(tx, `INSERT INTO notification_deliveries (id, event_id, channel_id, status, attempts, next_attempt_at, created_at) VALUES (?, ?, ?, ?, 0, ?, ?)`,
			uuid.NewString(), eventID, channelID, models.DeliveryPending, now, now)
		if err != nil {
			return fmt.Errorf("queueing delivery: %w", err)
		}
	}
	return nil
}

// Claim is a delivery a worker holds the lease on, with what it needs to
// send it.
type Claim struct {
	Delivery models.Delivery
	Event    models.Event
	Channel  models.NotificationChannel
	// Token identifies the lease; FinishDelivery needs it.
	Token string
}

// ClaimDeliveries leases up to limit pending deliveries that are due at now
// and not leased by anyone else. Each is claimed with a conditional update,
// so two workers sharing a database never hold the same delivery.
func (s *Store) ClaimDeliveries(now time.Time, lease time.Duration, limit int) ([]Claim, error) {
	now = dbTime(now)
	rows, err := s.Query(fmt.Sprintf(`SELECT id FROM notification_deliveries
		WHERE status = ? AND next_attempt_at <= ? AND (lease_until IS NULL OR lease_until < ?)
		ORDER BY next_attempt_at ASC, id ASC LIMIT %d`, limit), models.DeliveryPending, now, now)
	if err != nil {
		return nil, fmt.Errorf("finding due deliveries: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var claims []Claim
	until := dbTime(now.Add(lease))
	for _, id := range ids {
		token := uuid.NewString()
		res, err := s.Exec(`UPDATE notification_deliveries SET lease_token = ?, lease_until = ?
			WHERE id = ? AND status = ? AND (lease_until IS NULL OR lease_until < ?)`,
			token, until, id, models.DeliveryPending, now)
		if err != nil {
			return nil, fmt.Errorf("claiming delivery: %w", err)
		}
		if n, _ := res.RowsAffected(); n != 1 {
			continue // another worker got there first
		}
		claim, err := s.loadClaim(id)
		if err != nil {
			return nil, err
		}
		claim.Token = token
		claims = append(claims, *claim)
	}
	return claims, nil
}

func (s *Store) loadClaim(id string) (*Claim, error) {
	var (
		c        Claim
		lastErr  sql.NullString
		finished sql.NullTime
	)
	d := &c.Delivery
	err := s.QueryRow(`SELECT id, event_id, channel_id, status, attempts, next_attempt_at, last_error, created_at, finished_at
		FROM notification_deliveries WHERE id = ?`, id).Scan(
		&d.ID, &d.EventID, &d.ChannelID, &d.Status, &d.Attempts, &d.NextAttemptAt, &lastErr, &d.CreatedAt, &finished)
	if err != nil {
		return nil, fmt.Errorf("reading delivery: %w", err)
	}
	d.LastError = lastErr.String
	if finished.Valid {
		d.FinishedAt = &finished.Time
	}
	event, err := s.GetEvent(d.EventID)
	if err != nil {
		return nil, err
	}
	c.Event = *event
	channel, err := s.GetChannel(d.ChannelID)
	if err != nil {
		return nil, err
	}
	c.Channel = *channel
	return &c, nil
}

// GetEvent reads an event.
func (s *Store) GetEvent(id string) (*models.Event, error) {
	e, err := scanEvent(s.QueryRow(`SELECT `+eventColumns+` FROM events WHERE id = ?`, id))
	if err != nil {
		return nil, fmt.Errorf("reading event %s: %w", id, err)
	}
	return &e, nil
}

// Outcome is what became of one attempt at a delivery.
type Outcome struct {
	// Status is DeliverySent, DeliveryFailed, or DeliveryPending to retry.
	Status string
	// Error describes a failed attempt.
	Error string
	// NextAttemptAt is when to retry a pending delivery.
	NextAttemptAt time.Time
}

// FinishDelivery records an attempt and releases the lease. It reports false,
// changing nothing, if the lease was lost (it expired and another worker
// claimed the delivery).
func (s *Store) FinishDelivery(id, token string, now time.Time, o Outcome) (bool, error) {
	now = dbTime(now)
	var finished any
	if o.Status != models.DeliveryPending {
		finished = now
	}
	next := dbTime(o.NextAttemptAt)
	if o.Status != models.DeliveryPending {
		next = now
	}
	var lastErr any
	if o.Error != "" {
		lastErr = o.Error
	}
	res, err := s.Exec(`UPDATE notification_deliveries
		SET status = ?, attempts = attempts + 1, next_attempt_at = ?, last_error = ?, finished_at = ?, lease_token = NULL, lease_until = NULL
		WHERE id = ? AND lease_token = ?`,
		o.Status, next, lastErr, finished, id, token)
	if err != nil {
		return false, fmt.Errorf("recording delivery: %w", err)
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// DeliveryView is a delivery with what the delivery log shows about it.
type DeliveryView struct {
	models.Delivery
	ChannelName  string `json:"channel_name"`
	EventKind    string `json:"event_kind"`
	CategoryName string `json:"category_name,omitempty"`
}

// ListDeliveries returns the most recent deliveries, newest first.
func (s *Store) ListDeliveries(limit int) ([]DeliveryView, error) {
	rows, err := s.Query(fmt.Sprintf(`SELECT d.id, d.event_id, d.channel_id, d.status, d.attempts, d.next_attempt_at, d.last_error, d.created_at, d.finished_at,
			c.name, e.kind, e.payload
		FROM notification_deliveries d
		JOIN notification_channels c ON c.id = d.channel_id
		JOIN events e ON e.id = d.event_id
		ORDER BY d.created_at DESC, d.id DESC LIMIT %d`, limit))
	if err != nil {
		return nil, fmt.Errorf("listing deliveries: %w", err)
	}
	defer rows.Close()
	list := []DeliveryView{}
	for rows.Next() {
		var (
			v        DeliveryView
			lastErr  sql.NullString
			finished sql.NullTime
			payload  string
		)
		d := &v.Delivery
		if err := rows.Scan(&d.ID, &d.EventID, &d.ChannelID, &d.Status, &d.Attempts, &d.NextAttemptAt, &lastErr, &d.CreatedAt, &finished,
			&v.ChannelName, &v.EventKind, &payload); err != nil {
			return nil, err
		}
		d.LastError = lastErr.String
		if finished.Valid {
			d.FinishedAt = &finished.Time
		}
		var p models.EventPayload
		if json.Unmarshal([]byte(payload), &p) == nil && p.Category != nil {
			v.CategoryName = p.Category.Name
		}
		list = append(list, v)
	}
	return list, rows.Err()
}
