package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net"
	"strings"

	"github.com/go-sql-driver/mysql"
	"github.com/lib/pq"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// IsDatabaseFailure reports whether err comes from the database or the
// connection to it, rather than from checking what a caller asked for: an
// error from a driver, a broken or closed connection, or a transaction
// already finished. Handlers answer these with 500, and everything else
// the store returns, which is about the request, with 4xx.
func IsDatabaseFailure(err error) bool {
	if err == nil || errors.Is(err, sql.ErrNoRows) {
		return false
	}
	var (
		pqErr     *pq.Error
		mysqlErr  *mysql.MySQLError
		sqliteErr *sqlite.Error
		netErr    net.Error
	)
	switch {
	case errors.As(err, &pqErr), errors.As(err, &mysqlErr), errors.As(err, &sqliteErr), errors.As(err, &netErr):
		return true
	case errors.Is(err, driver.ErrBadConn), errors.Is(err, sql.ErrConnDone), errors.Is(err, sql.ErrTxDone),
		errors.Is(err, mysql.ErrInvalidConn), errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return true
	}
	// database/sql does not export these.
	msg := err.Error()
	return strings.Contains(msg, "sql: database is closed") || strings.Contains(msg, "sql: connection is already closed")
}

// IsUniqueViolation reports whether err is the database refusing a row
// because another already holds the same unique value: two requests racing
// to create the same thing, the second losing. Handlers answer it with 409.
func IsUniqueViolation(err error) bool {
	var (
		pqErr     *pq.Error
		mysqlErr  *mysql.MySQLError
		sqliteErr *sqlite.Error
	)
	switch {
	case errors.As(err, &pqErr):
		return pqErr.Code == "23505"
	case errors.As(err, &mysqlErr):
		return mysqlErr.Number == 1062
	case errors.As(err, &sqliteErr):
		return sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE || sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY
	}
	return false
}
