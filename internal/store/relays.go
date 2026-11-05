package store

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// The relay bus, when several servers share the database
// (docs/relays.md#several-servers): which server holds each relay over each
// transport, and the messages servers leave each other for the relays they
// hold.

// HoldRelay records that server holds node over transport, taking it from
// any server that held it before: a relay that dials again reaches
// whichever server it reaches.
func (s *Store) HoldRelay(node, transport, server string) error {
	now := dbTime(time.Now())
	for range 2 {
		res, err := s.Exec("UPDATE relay_holds SET server = ?, seen_at = ? WHERE node = ? AND transport = ?", server, now, node, transport)
		if err != nil {
			return fmt.Errorf("claiming a relay: %w", err)
		}
		if n, err := res.RowsAffected(); err == nil && n > 0 {
			return nil
		}
		_, err = s.Exec("INSERT INTO relay_holds (node, transport, server, seen_at) VALUES (?, ?, ?, ?)", node, transport, server, now)
		if err == nil {
			return nil
		}
		// Another server claimed it in between: take it from that one.
		if !isUniqueViolation(err) {
			return fmt.Errorf("claiming a relay: %w", err)
		}
	}
	return fmt.Errorf("claiming relay %s: other servers keep claiming it", node)
}

// KeepRelay says server still holds node over transport. It reports false
// when another server has taken it since.
func (s *Store) KeepRelay(node, transport, server string) (bool, error) {
	res, err := s.Exec("UPDATE relay_holds SET seen_at = ? WHERE node = ? AND transport = ? AND server = ?", dbTime(time.Now()), node, transport, server)
	if err != nil {
		return false, fmt.Errorf("keeping a relay: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// ReleaseRelay lets go of node over transport, if server holds it.
func (s *Store) ReleaseRelay(node, transport, server string) error {
	if _, err := s.Exec("DELETE FROM relay_holds WHERE node = ? AND transport = ? AND server = ?", node, transport, server); err != nil {
		return fmt.Errorf("releasing a relay: %w", err)
	}
	return nil
}

// RelayHolder is the server holding node, seen since staleBefore, over
// BoneMesh if one does and otherwise over WebSocket; "" when none does.
func (s *Store) RelayHolder(node string, staleBefore time.Time) (string, error) {
	rows, err := s.Query("SELECT transport, server FROM relay_holds WHERE node = ? AND seen_at >= ?", node, dbTime(staleBefore))
	if err != nil {
		return "", fmt.Errorf("finding a relay's server: %w", err)
	}
	defer rows.Close()
	holder := ""
	for rows.Next() {
		var transport, server string
		if err := rows.Scan(&transport, &server); err != nil {
			return "", err
		}
		if holder == "" || transport == "bonemesh" {
			holder = server
		}
	}
	return holder, rows.Err()
}

// PostRelayMail leaves a message for server to route.
func (s *Store) PostRelayMail(server string, message []byte) error {
	_, err := s.Exec("INSERT INTO relay_mail (id, server, message, created_at) VALUES (?, ?, ?, ?)",
		uuid.NewString(), server, string(message), dbTime(time.Now()))
	if err != nil {
		return fmt.Errorf("leaving a relay message for another server: %w", err)
	}
	return nil
}

// TakeRelayMail takes the messages left for server, oldest first, at most
// 256 at a time, and drops any left before staleBefore for a server that
// never took them.
func (s *Store) TakeRelayMail(server string, staleBefore time.Time) ([][]byte, error) {
	if _, err := s.Exec("DELETE FROM relay_mail WHERE created_at < ?", dbTime(staleBefore)); err != nil {
		return nil, fmt.Errorf("dropping stale relay messages: %w", err)
	}
	rows, err := s.Query("SELECT id, message FROM relay_mail WHERE server = ? ORDER BY created_at, id LIMIT 256", server)
	if err != nil {
		return nil, fmt.Errorf("reading relay messages: %w", err)
	}
	var ids []any
	var out [][]byte
	for rows.Next() {
		var id, message string
		if err := rows.Scan(&id, &message); err != nil {
			rows.Close()
			return nil, err
		}
		ids, out = append(ids, id), append(out, []byte(message))
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	if _, err := s.Exec("DELETE FROM relay_mail WHERE id IN ("+placeholders(len(ids))+")", ids...); err != nil {
		return nil, fmt.Errorf("taking relay messages: %w", err)
	}
	return out, nil
}
