package migrations

import (
	"strings"
	"testing"
)

func TestAdaptDDL(t *testing.T) {
	create := `CREATE TABLE IF NOT EXISTS notes (
    id VARCHAR(36) PRIMARY KEY,
    body TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL
);
`
	got := adaptDDL(create, "mariadb", "utf8mb4_nopad_bin")
	for _, want := range []string{"body LONGTEXT NOT NULL", "created_at DATETIME(6) NOT NULL", ") DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if !strings.HasSuffix(got, "utf8mb4_nopad_bin") {
		t.Errorf("table options not last:\n%s", got)
	}

	// Other statements change types but take no table options.
	alter := "ALTER TABLE notes ADD COLUMN extra TEXT;"
	if got := adaptDDL(alter, "mariadb", "utf8mb4_nopad_bin"); got != "ALTER TABLE notes ADD COLUMN extra LONGTEXT;" {
		t.Errorf("alter: %q", got)
	}
	index := "CREATE INDEX idx_notes ON notes (created_at);"
	if got := adaptDDL(index, "mariadb", "utf8mb4_nopad_bin"); got != index {
		t.Errorf("index: %q", got)
	}

	for _, d := range []string{"sqlite", "postgres"} {
		if got := adaptDDL(create, d, ""); got != create {
			t.Errorf("%s changed:\n%s", d, got)
		}
	}
}
