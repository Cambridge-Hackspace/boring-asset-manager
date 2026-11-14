package archive

import (
	"context"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/filestore"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// TestBackupsCarryTerms: a backup holds the related terms, their links and
// the imports they came in, with who added each, and a restore puts them
// back as they were, still expanding searches.
func TestBackupsCarryTerms(t *testing.T) {
	src := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	s := seed(t, src, blobs)
	as := src.As(s.user.ID)
	flex, err := as.Via("Claude Desktop").CreateTerm(store.TermInput{Label: "Flexible filament", AltLabels: []string{"flex filament"}, Note: "Bends."})
	if err != nil {
		t.Fatal(err)
	}
	imp, err := as.ImportTerms("Filament words", []store.TermImportItem{
		{TermInput: store.TermInput{Label: "TPU"}, Broader: []string{"Flexible filament"}, Related: []string{"TPE"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	b := backup(t, src, blobs, Options{})
	r := readArchive(t, b)
	for dir, want := range map[string]int{"terms": 3, "term_links": 2, "term_imports": 1} {
		if got := len(r.rows(t, DataDir+dir+"/")); got != want {
			t.Errorf("%s: %d rows, want %d", dir, got, want)
		}
	}

	dst := emptyStore(t, envDialect())
	if _, err := Restore(context.Background(), dst, filestore.NewLocal(t.TempDir()), opener(b), RestoreOptions{}); err != nil {
		t.Fatal(err)
	}
	got, err := dst.GetTerm(flex.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Label != flex.Label || len(got.AltLabels) != 1 || got.AltLabels[0] != "flex filament" || got.Note != "Bends." ||
		got.Via != "Claude Desktop" || got.AddedBy != s.user.ID || !got.CreatedAt.Equal(flex.CreatedAt) {
		t.Errorf("restored %+v, want %+v", got, flex)
	}
	imports, err := dst.ListTermImports()
	if err != nil || len(imports) != 1 || imports[0].ID != imp.ID || imports[0].Terms != 2 || imports[0].Links != 2 {
		t.Errorf("imports, restored: %+v %v", imports, err)
	}
	g, err := dst.TermGraph()
	if err != nil {
		t.Fatal(err)
	}
	if x := g.Expand("flex filament"); len(x) != 2 || x[1].Label != "TPU" {
		t.Errorf("restored terms expand to %+v", x)
	}
	if rel := g.Related("TPU"); len(rel) != 1 || rel[0] != "TPE" {
		t.Errorf("restored related terms: %v", rel)
	}
}
