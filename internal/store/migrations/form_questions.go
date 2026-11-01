package migrations

import (
	"database/sql"
	"fmt"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/flows"
)

// FormQuestionsVersion is the migration that rewrote every flow's
// definition from version 1 to version 2 (#174): a Form asks only its own
// questions, and a Set fields step puts values into the asset's fields.
// Archives written before it hold version 1 flows; restoring one migrates
// it forward, and importing a package converts its flows with
// flows.UpgradeV1 and a corpus read by ReadFieldCorpus.
const FormQuestionsVersion = 50

// upgradeFlows converts each version 1 flow with flows.UpgradeV1, against
// the fields and categories as they are, and drops the runs under way: a
// run's place and answers are by the steps of the definition it began
// under, and runs are dropped after 12 hours anyway. Run again, it changes
// nothing.
func upgradeFlows(db *sql.DB, dialect, _ string) error {
	corpus, err := ReadFieldCorpus(func(q string) (*sql.Rows, error) { return db.Query(q) })
	if err != nil {
		return err
	}
	changed := false
	if err := rewriteColumn(db, dialect, "SELECT id, definition FROM flows", "UPDATE flows SET definition = ? WHERE id = ?", func(def string) (string, bool, error) {
		next, did, err := flows.UpgradeV1(def, corpus.Effective)
		changed = changed || did
		return next, did, err
	}); err != nil {
		return fmt.Errorf("converting flows to version 2: %w", err)
	}
	if changed {
		if _, err := db.Exec("DELETE FROM flow_runs"); err != nil {
			return fmt.Errorf("dropping runs under way: %w", err)
		}
	}
	return nil
}

// ReadFieldCorpus reads the fields and categories a database holds, for
// converting version 1 flows.
func ReadFieldCorpus(query func(string) (*sql.Rows, error)) (*flows.FieldCorpus, error) {
	c := flows.NewFieldCorpus()
	read := func(sqlText string, each func(cols []sql.NullString)) error {
		rows, err := query(sqlText)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		names, err := rows.Columns()
		if err != nil {
			return err
		}
		for rows.Next() {
			cols := make([]sql.NullString, len(names))
			ptrs := make([]any, len(cols))
			for i := range cols {
				ptrs[i] = &cols[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				return err
			}
			each(cols)
		}
		return rows.Err()
	}
	if err := read("SELECT id, field_key, label, validation_type, options FROM fields", func(r []sql.NullString) {
		c.AddField(r[0].String, r[1].String, r[2].String, r[3].String, r[4].String)
	}); err != nil {
		return nil, fmt.Errorf("reading fields: %w", err)
	}
	if err := read("SELECT id, parent_id FROM categories", func(r []sql.NullString) {
		if r[1].Valid && r[1].String != "" {
			c.Parents[r[0].String] = r[1].String
		}
	}); err != nil {
		return nil, fmt.Errorf("reading categories: %w", err)
	}
	if err := read("SELECT category_id, field_id, is_required FROM category_fields", func(r []sql.NullString) {
		c.Assigned[r[0].String] = append(c.Assigned[r[0].String], flows.FieldAssignment{FieldID: r[1].String, Required: truthy(r[2].String)})
	}); err != nil {
		return nil, fmt.Errorf("reading categories' fields: %w", err)
	}
	return c, nil
}

// truthy reads a boolean column as each database gives it as text.
func truthy(s string) bool {
	switch s {
	case "1", "t", "true", "TRUE", "True":
		return true
	}
	return false
}
