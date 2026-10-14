package assetcsv

import (
	"bytes"
	"encoding/json"
	"errors"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"testing"
)

// checkImport plans a file and holds the planner to its word: an error is
// only ever an unreadable file, problems point at lines in the file, and a
// plan without problems must import, after which exporting everything and
// planning that export again changes nothing.
func checkImport(t testing.TB, w *world, data []byte, mapping []string) (imported bool) {
	t.Helper()
	plan, err := Prepare(w.st, data, mapping)
	if err != nil {
		if !errors.Is(err, ErrUnreadable) {
			t.Fatalf("Prepare: %v\nfile %q\nmapping %q", err, data, mapping)
		}
		return false
	}
	// Problems give the line a record starts on, which a quoted line break
	// can push past the record count.
	lines := bytes.Count(data, []byte("\n")) + 1
	for _, p := range plan.Problems {
		if p.Row < 1 || p.Row > lines {
			t.Fatalf("problem on line %d of %d: %+v\nfile %q", p.Row, lines, p, data)
		}
	}
	if plan.ProblemCount > 0 {
		if plan.Changes != nil {
			t.Fatalf("a plan with problems has changes\nfile %q", data)
		}
		return false
	}
	if _, err := w.st.ImportAssets(plan.Changes); err != nil {
		t.Fatalf("the planner approved what the store refused: %v\nfile %q\nmapping %q", err, data, mapping)
	}

	assets, err := w.st.ListAssets("", "")
	if err != nil {
		t.Fatal(err)
	}
	cats, err := w.st.ListAllCategories()
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := Export(&b, assets, NewCategories(cats), []string{"qr", "micro_qr", "code128"}); err != nil {
		t.Fatal(err)
	}
	again, err := Prepare(w.st, b.Bytes(), nil)
	if err != nil {
		t.Fatalf("planning the export: %v", err)
	}
	if again.ProblemCount != 0 || again.Create+again.Update != 0 || again.Unchanged != len(assets) {
		t.Fatalf("after importing %q, the export does not plan as unchanged: %d problems %+v, %d to create, %d to update",
			data, again.ProblemCount, again.Problems, again.Create, again.Update)
	}
	return true
}

// importTargets are the columns a generated file maps to.
var importTargets = []string{"id", "name", "categories", "code.qr", "code.code128", "code.micro_qr", "field.serial", "field.condition", "field.weight", "ignore"}

// importValues are cell values to draw from, besides IDs and codes.
var importValues = []string{
	"", " ", "Drill", "drill", "Tools", "Tools > Drills", "Tape", "Tools; Tape", "Nope", "Tools > Nope", ";",
	"SN1", "good", "broken", "bad", "-5", "1e3", "0.5", "x", "D1", "H1", "ABC123", "12345", "abc",
	"=1+1", "'=1+1", "+1", "@x", "Quote \"me\"", "line\nbreak", "tab\there", "Ångström", "🔧", "工具",
	"a" + string(rune(0)) + "b", string([]byte{0xff}), strings.Repeat("x", 256), strings.Repeat("9", 40),
}

// generateImport makes a file that is often valid and often nearly so.
func generateImport(rng *rand.Rand, w *world, n int) ([]byte, []string) {
	cols := make([]string, 1+rng.Intn(5))
	for i := range cols {
		cols[i] = importTargets[rng.Intn(len(importTargets))]
	}
	if rng.Intn(3) > 0 {
		cols[0] = "name"
	}
	assets, _ := w.st.ListAssets("", "")
	cell := func(target string) string {
		switch {
		case target == "id" && len(assets) > 0 && rng.Intn(4) > 0:
			return assets[rng.Intn(len(assets))].Asset.ID
		case strings.HasPrefix(target, "code.") && rng.Intn(2) == 0:
			n++
			return "C" + strconv.Itoa(n)
		case target == "name" && rng.Intn(2) == 0:
			n++
			return "Thing " + strconv.Itoa(n)
		}
		return importValues[rng.Intn(len(importValues))]
	}
	var b bytes.Buffer
	quote := func(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
	row := func(cells []string) {
		for i, c := range cells {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(quote(c))
		}
		b.WriteString("\r\n")
	}
	row(cols)
	for r := rng.Intn(5); r >= 0; r-- {
		cells := make([]string, len(cols))
		for i, c := range cols {
			cells[i] = cell(c)
		}
		row(cells)
	}
	mapping := append([]string(nil), cols...)
	if rng.Intn(10) == 0 {
		mapping = nil // the suggested mapping
	}
	return b.Bytes(), mapping
}

// TestSeededImports plans and imports generated files, holding the planner
// to its word (see checkImport). BAM_FUZZ_SEED and BAM_FUZZ_IMPORTS choose
// another seed and number of files.
func TestSeededImports(t *testing.T) {
	seed, files := int64(1), 300
	if n, err := strconv.Atoi(os.Getenv("BAM_FUZZ_SEED")); err == nil {
		seed = int64(n)
	}
	if n, err := strconv.Atoi(os.Getenv("BAM_FUZZ_IMPORTS")); err == nil && n > 0 {
		files = n
	}
	if testing.Short() {
		files = 50
	}
	w := newWorld(t)
	rng := rand.New(rand.NewSource(seed))
	imported := 0
	for i := 0; i < files; i++ {
		data, mapping := generateImport(rng, w, i*100)
		if checkImport(t, w, data, mapping) {
			imported++
		}
	}
	t.Logf("%d of %d files imported; the rest had problems", imported, files)
}

// FuzzImport lets Go's fuzzer write import files and mappings, for longer
// runs than TestSeededImports: go test -fuzz=FuzzImport ./internal/assetcsv/
func FuzzImport(f *testing.F) {
	w := newWorld(f)
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 20; i++ {
		data, mapping := generateImport(rng, w, i*100)
		raw, _ := json.Marshal(mapping)
		f.Add(data, string(raw))
	}
	f.Fuzz(func(t *testing.T, data []byte, mappingJSON string) {
		var mapping []string
		if json.Unmarshal([]byte(mappingJSON), &mapping) != nil {
			mapping = nil
		}
		checkImport(t, w, data, mapping)
	})
}
