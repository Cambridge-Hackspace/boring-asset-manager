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

// Live events (README §5.4): what the servers holding devices' connections
// read to send them events, and keep about those connections and the
// devices' answers to actions.

// ErrNotAnAction is returned for answering an event that is no action.
var ErrNotAnAction = errors.New("that event is not an action")

// MaxReplyMessage bounds a device's answer.
const MaxReplyMessage = 255

// EventsSince lists events recorded at or after since, oldest first, at
// most limit of them.
func (s *Store) EventsSince(since time.Time, limit int) ([]models.Event, error) {
	rows, err := s.Query(fmt.Sprintf("SELECT "+eventColumns+" FROM events WHERE created_at >= ? ORDER BY created_at, id LIMIT %d", limit), dbTime(since))
	if err != nil {
		return nil, fmt.Errorf("listing events: %w", err)
	}
	defer rows.Close()
	out := []models.Event{}
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// EventPlaces says, for events about assets and categories, every category
// each is in, with all their ancestors, and the scopes it is in.
func (s *Store) EventPlaces(events []models.Event) (categories map[string]map[string]bool, scopes map[string][]string, err error) {
	traits, err := s.categoryTraits(storeQuerier{s})
	if err != nil {
		return nil, nil, err
	}
	up := func(set map[string]bool, id string) {
		for c, d := id, 0; c != "" && d < 100; c, d = traits.parents[c], d+1 {
			set[c] = true
		}
	}
	categories, scopes = map[string]map[string]bool{}, map[string][]string{}
	var assetIDs []string
	for _, e := range events {
		switch {
		case e.AssetID != nil:
			if _, ok := categories[*e.AssetID]; !ok {
				categories[*e.AssetID] = map[string]bool{}
				assetIDs = append(assetIDs, *e.AssetID)
			}
		case e.CategoryID != nil:
			set := map[string]bool{}
			up(set, *e.CategoryID)
			categories[*e.CategoryID] = set
			scopes[*e.CategoryID] = traits.scopesOf([]string{*e.CategoryID})
		}
	}
	if len(assetIDs) == 0 {
		return categories, scopes, nil
	}
	args := make([]any, len(assetIDs))
	for i, id := range assetIDs {
		args[i] = id
	}
	rows, err := s.Query("SELECT asset_id, category_id FROM asset_categories WHERE asset_id IN ("+placeholders(len(assetIDs))+")", args...)
	if err != nil {
		return nil, nil, fmt.Errorf("reading assets' categories: %w", err)
	}
	direct := map[string][]string{}
	for rows.Next() {
		var a, c string
		if err := rows.Scan(&a, &c); err != nil {
			rows.Close()
			return nil, nil, err
		}
		direct[a] = append(direct[a], c)
		up(categories[a], c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	for _, a := range assetIDs {
		scopes[a] = traits.scopesOf(direct[a])
	}
	return categories, scopes, nil
}

// OpenLiveConnection lists a live connection this server holds, for a
// token or a user.
func (s *Store) OpenLiveConnection(tokenID, userID, remote string) (string, error) {
	id, now := uuid.NewString(), dbTime(time.Now())
	if len(remote) > 64 {
		remote = remote[:64]
	}
	_, err := s.Exec("INSERT INTO live_connections (id, token_id, user_id, remote, opened_at, seen_at) VALUES (?, ?, ?, ?, ?, ?)",
		id, nullable(tokenID), nullable(userID), remote, now, now)
	if err != nil {
		return "", fmt.Errorf("listing a live connection: %w", err)
	}
	return id, nil
}

// nullable is nil for an empty string.
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// TouchLiveConnections says connections are still open.
func (s *Store) TouchLiveConnections(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	args := []any{dbTime(time.Now())}
	for _, id := range ids {
		args = append(args, id)
	}
	if _, err := s.Exec("UPDATE live_connections SET seen_at = ? WHERE id IN ("+placeholders(len(ids))+")", args...); err != nil {
		return fmt.Errorf("touching live connections: %w", err)
	}
	return nil
}

// CloseLiveConnection takes a closed connection off the list, and with it
// any a stopped server left that have not been seen since staleBefore.
func (s *Store) CloseLiveConnection(id string, staleBefore time.Time) error {
	if _, err := s.Exec("DELETE FROM live_connections WHERE id = ? OR seen_at < ?", id, dbTime(staleBefore)); err != nil {
		return fmt.Errorf("closing a live connection: %w", err)
	}
	return nil
}

// ListLiveConnections lists the connections seen since staleBefore, newest
// first; with tokenID or userID, only theirs.
func (s *Store) ListLiveConnections(staleBefore time.Time, tokenID, userID string) ([]models.LiveConnection, error) {
	where, args := []string{"seen_at >= ?"}, []any{dbTime(staleBefore)}
	if tokenID != "" {
		where, args = append(where, "token_id = ?"), append(args, tokenID)
	}
	if userID != "" {
		where, args = append(where, "user_id = ?"), append(args, userID)
	}
	rows, err := s.Query("SELECT id, token_id, user_id, remote, opened_at, seen_at FROM live_connections WHERE "+strings.Join(where, " AND ")+" ORDER BY opened_at DESC, id", args...)
	if err != nil {
		return nil, fmt.Errorf("listing live connections: %w", err)
	}
	defer rows.Close()
	out := []models.LiveConnection{}
	for rows.Next() {
		var c models.LiveConnection
		var token, user sql.NullString
		if err := rows.Scan(&c.ID, &token, &user, &c.Remote, &c.OpenedAt, &c.SeenAt); err != nil {
			return nil, err
		}
		if token.Valid {
			c.TokenID = &token.String
		}
		if user.Valid {
			c.UserID = &user.String
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// RecordActionReply keeps a device's answer to an action event, from the
// acting token or user.
func (s *Store) RecordActionReply(eventID string, ok bool, message string) (*models.ActionReply, error) {
	var kind string
	err := s.QueryRow("SELECT kind FROM events WHERE id = ?", eventID).Scan(&kind)
	if err == sql.ErrNoRows {
		return nil, ErrEventNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("reading the event: %w", err)
	}
	if kind != models.EventAssetAction {
		return nil, ErrNotAnAction
	}
	message = strings.TrimSpace(message)
	if len(message) > MaxReplyMessage {
		message = message[:MaxReplyMessage]
	}
	r := &models.ActionReply{ID: uuid.NewString(), EventID: eventID, OK: ok, Message: message, CreatedAt: dbTime(time.Now())}
	var tokenID, userID any
	switch {
	case s.actor != "":
		userID = s.actor
		if err := s.QueryRow("SELECT name FROM users WHERE id = ?", s.actor).Scan(&r.Name); err != nil && err != sql.ErrNoRows {
			return nil, fmt.Errorf("reading the user: %w", err)
		}
	case s.actorToken != "":
		tokenID, r.Token = s.actorToken, true
		if err := s.QueryRow("SELECT name FROM api_tokens WHERE id = ?", s.actorToken).Scan(&r.Name); err != nil && err != sql.ErrNoRows {
			return nil, fmt.Errorf("reading the token: %w", err)
		}
	}
	_, err = s.Exec("INSERT INTO action_replies (id, event_id, token_id, user_id, name, ok, message, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		r.ID, eventID, tokenID, userID, r.Name, ok, message, r.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("keeping the answer: %w", err)
	}
	return r, nil
}

// ActionReplies lists the answers to an action event, oldest first.
func (s *Store) ActionReplies(eventID string) ([]models.ActionReply, error) {
	rows, err := s.Query("SELECT id, event_id, name, token_id IS NOT NULL, ok, message, created_at FROM action_replies WHERE event_id = ? ORDER BY created_at, id", eventID)
	if err != nil {
		return nil, fmt.Errorf("listing answers: %w", err)
	}
	defer rows.Close()
	out := []models.ActionReply{}
	for rows.Next() {
		var r models.ActionReply
		if err := rows.Scan(&r.ID, &r.EventID, &r.Name, &r.Token, &r.OK, &r.Message, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// EventAsset says which asset an event is about, if any.
func (s *Store) EventAsset(eventID string) (string, error) {
	var asset sql.NullString
	err := s.QueryRow("SELECT asset_id FROM events WHERE id = ?", eventID).Scan(&asset)
	if err == sql.ErrNoRows {
		return "", ErrEventNotFound
	}
	if err != nil {
		return "", fmt.Errorf("reading the event: %w", err)
	}
	return asset.String, nil
}
