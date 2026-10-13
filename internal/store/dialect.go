package store

import (
	"database/sql"
	"strconv"
	"strings"
)

// Rebind adapts parameter placeholders in a SQL query according to the target database dialect.
// Queries are authored using standard '?' placeholders.
// For PostgreSQL, '?' is converted to positional '$1', '$2', '$3', etc.
// For SQLite and MariaDB/MySQL, '?' is preserved untouched.
func Rebind(query string, dialect string) string {
	if dialect != "postgres" {
		return query
	}

	var b strings.Builder
	b.Grow(len(query) + 16)
	paramIdx := 1
	inString := false

	for i := 0; i < len(query); i++ {
		ch := query[i]
		if ch == '\'' {
			inString = !inString
			b.WriteByte(ch)
			continue
		}
		if ch == '?' && !inString {
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(paramIdx))
			paramIdx++
		} else {
			b.WriteByte(ch)
		}
	}
	return b.String()
}

// Rebind adapts query placeholders for this store's dialect.
func (s *Store) Rebind(query string) string {
	return Rebind(query, s.Dialect)
}

// Exec executes a query with dialect parameter rebinding.
func (s *Store) Exec(query string, args ...any) (sql.Result, error) {
	return s.DB.Exec(s.Rebind(query), args...)
}

// Query executes a query returning rows with dialect parameter rebinding.
func (s *Store) Query(query string, args ...any) (*sql.Rows, error) {
	return s.DB.Query(s.Rebind(query), args...)
}

// QueryRow executes a query returning a single row with dialect parameter rebinding.
func (s *Store) QueryRow(query string, args ...any) *sql.Row {
	return s.DB.QueryRow(s.Rebind(query), args...)
}

// TxExec executes a query within a transaction with dialect parameter rebinding.
func (s *Store) TxExec(tx *sql.Tx, query string, args ...any) (sql.Result, error) {
	return tx.Exec(s.Rebind(query), args...)
}

// TxQuery executes a query returning rows within a transaction with dialect parameter rebinding.
func (s *Store) TxQuery(tx *sql.Tx, query string, args ...any) (*sql.Rows, error) {
	return tx.Query(s.Rebind(query), args...)
}

// TxQueryRow executes a query returning a single row within a transaction with dialect parameter rebinding.
func (s *Store) TxQueryRow(tx *sql.Tx, query string, args ...any) *sql.Row {
	return tx.QueryRow(s.Rebind(query), args...)
}
