package api

import (
	"math/rand"
	"net/http"
	"strings"
	"testing"
)

// domainTables are the tables behind everything but signing in, children
// before parents so each can be dropped while foreign keys are enforced.
var domainTables = []string{
	"notification_deliveries", "events", "notification_channels",
	"asset_loans", "asset_links", "asset_photos", "category_relation_types", "category_scopes", "asset_field_values", "asset_generator_keys", "asset_categories", "category_fields",
	"assets", "categories", "relation_types", "reports", "fields", "templates", "images", "printers",
}

// sqlLeaks are fragments of database error messages, which a response
// should never pass on.
var sqlLeaks = []string{"no such table", "does not exist", "doesn't exist", "SQLSTATE", "Error 1146", "pq:"}

// TestDatabaseFailuresAreServerErrors sends every endpoint the valid request
// it answers successfully, then drops the tables behind the data and sends
// it again. A failure of the database is the server's: no endpoint may then
// blame the caller with 4xx, and none may pass the database's error on.
func TestDatabaseFailuresAreServerErrors(t *testing.T) {
	request := func(fz *fuzzer, r route) fuzzRequest {
		return fz.request(r, rand.New(rand.NewSource(1)), false)
	}
	// Which requests work is found on one server; they are then sent to
	// another, made the same way, so the deletes among them remove nothing
	// the others need.
	healthy := newFuzzer(t)
	var working []route
	for _, r := range fuzzRoutes {
		if rec, _ := healthy.serve(request(healthy, r)); rec.Code/100 == 2 {
			working = append(working, r)
		}
	}
	fz := newFuzzer(t)
	fz.filling = true // pools are not refilled once the tables are gone
	if len(working) < len(fuzzRoutes)/2 {
		t.Fatalf("only %d of %d routes work on a healthy database", len(working), len(fuzzRoutes))
	}

	for _, table := range domainTables {
		stmt := "DROP TABLE " + table
		if fz.st.Dialect == "postgres" {
			stmt += " CASCADE"
		}
		if _, err := fz.st.DB.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	failed := 0
	for _, r := range working {
		req := request(fz, r)
		rec, _ := fz.serve(req)
		body := rec.Body.String()
		switch {
		case rec.Code/100 == 4:
			t.Errorf("%s %s: %d %.300s, a failure of the database blamed on the caller", r.method, r.path, rec.Code, body)
		case rec.Code >= 500:
			failed++
			for _, leak := range sqlLeaks {
				if strings.Contains(body, leak) {
					t.Errorf("%s %s: the response passes on the database's error: %.300s", r.method, r.path, body)
				}
			}
		}
	}
	if failed == 0 {
		t.Fatal("no endpoint failed with its tables gone")
	}
}

// TestSessionFailuresAreServerErrors breaks the tables behind sessions and
// permissions in turn. A signed-in user whose session cannot be read is not
// signed out (401), and one whose permissions cannot be read is not refused
// (403): both are the server failing.
func TestSessionFailuresAreServerErrors(t *testing.T) {
	for _, table := range []string{"role_permissions", "sessions"} {
		t.Run(table, func(t *testing.T) {
			fz := newFuzzer(t)
			stmt := "DROP TABLE " + table
			if fz.st.Dialect == "postgres" {
				stmt += " CASCADE"
			}
			if _, err := fz.st.DB.Exec(stmt); err != nil {
				t.Fatalf("%s: %v", stmt, err)
			}
			for _, path := range []string{"/api/v1/auth/me", "/api/v1/assets"} {
				rec, _ := fz.serve(fuzzRequest{method: http.MethodGet, target: path})
				if rec.Code != http.StatusInternalServerError {
					t.Errorf("GET %s with %s gone: %d %s", path, table, rec.Code, rec.Body.String())
				}
			}
		})
	}
}
