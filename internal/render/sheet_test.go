package render

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"image"
	"os"
	"strings"
	"testing"
)

// sampleImage is a picture made the same each time: a gradient, or a
// checkerboard like a code's modules.
func sampleImage(w, h int, checker bool) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			v := uint8((x + y) * 255 / (w + h))
			if checker {
				v = 0xFF
				if (x/3+y/3)%2 == 0 {
					v = 0
				}
			}
			img.Pix[y*img.Stride+x] = v
		}
	}
	return img
}

// sampleSheet is an asset's sheet: its pictures, facts, and history.
func sampleSheet(t *testing.T) *Document {
	t.Helper()
	d, err := NewDocument(DocumentOptions{Size: Letter, Instance: "Cambridge Hackspace", Title: "Cordless drill", Made: "Made 2026-11-05 09:30 UTC by Ada Lovelace"})
	if err != nil {
		t.Fatal(err)
	}
	d.Heading("Cordless drill")
	d.Figures([]Figure{
		{Image: sampleImage(160, 120, false), Caption: "Photo", WidthPt: 144, HeightPt: 108},
		{Image: sampleImage(29, 29, true), Caption: "TOOL-1", WidthPt: 72, HeightPt: 72},
		{Image: nil, Caption: "left out"},
		{Image: sampleImage(90, 20, true), Caption: "TOOL-0001", WidthPt: 180, HeightPt: 48},
		{Image: sampleImage(320, 160, false), Caption: "Label", WidthPt: 144, HeightPt: 72},
	})
	d.Facts([]Fact{
		{"Categories", "Tools, Power tools"},
		{"Where", "Workshop › Cabinet 3"},
		{"Condition", "Needs maintenance: the chuck slips under load, and the battery holds a charge for about ten minutes, which is not enough for a day's work in the woodshop"},
		{"Loan", "Lent to Ada Lovelace on 2026-11-01, due 2026-11-08"},
		{"Serial number", "SN-0042"},
		{"Empty", ""},
	})
	d.Heading("Recent history")
	d.Table(Table{Columns: []string{"When", "Who", "What"}, Groups: []TableGroup{{Rows: []TableRow{
		{Cells: []string{"2026-11-01 10:00", "Ada Lovelace", "Lent"}},
		{Cells: []string{"2026-10-30 16:12", "Charles Babbage", "Condition changed"}},
		{Cells: []string{"2026-01-15 12:00", "", "Added"}},
	}}}})
	d.NewPage()
	d.Heading("Second asset")
	return d
}

// sampleChecklist is a stocktake: grouped rows to tick, long enough to run
// over pages.
func sampleChecklist(t *testing.T) *Document {
	t.Helper()
	d, err := NewDocument(DocumentOptions{Size: A4, Instance: "Cambridge Hackspace", Title: "Stocktake", Made: "Made 2026-11-05 09:30 UTC by Ada Lovelace"})
	if err != nil {
		t.Fatal(err)
	}
	d.Heading("Stocktake")
	table := Table{Columns: []string{"Asset", "Codes", "Expected", "Counted"}, Checkbox: true}
	for g, place := range []string{"Kitchen", "Workshop", "Workshop › Drawer 2", "Not in a place"} {
		group := TableGroup{Title: place}
		for r := range 20 {
			group.Rows = append(group.Rows, TableRow{Cells: []string{fmt.Sprintf("Asset %d-%d", g+1, r+1), fmt.Sprintf("BIN-%03d", g*20+r), fmt.Sprint(r + 1), ""}})
		}
		table.Groups = append(table.Groups, group)
	}
	d.Table(table)
	return d
}

// golden checks a document against testdata/<name>.pdf, and its pages,
// drawn, against testdata/<name>.pages. Run with BAM_UPDATE_FIXTURES=1 to
// write them again, after opening the PDF to check it.
func golden(t *testing.T, name string, build func(*testing.T) *Document) {
	t.Helper()
	pages := build(t).Pages()
	var pdf bytes.Buffer
	if err := WritePDF(&pdf, pages...); err != nil {
		t.Fatal(err)
	}
	var sums strings.Builder
	for i, p := range pages {
		fmt.Fprintf(&sums, "page %d %x\n", i+1, sha256.Sum256(p.Raster(false).Pix))
	}
	if os.Getenv("BAM_UPDATE_FIXTURES") != "" {
		_ = os.WriteFile("testdata/"+name+".pdf", pdf.Bytes(), 0o644)
		_ = os.WriteFile("testdata/"+name+".pages", []byte(sums.String()), 0o644)
	}
	want, _ := os.ReadFile("testdata/" + name + ".pdf")
	if !bytes.Equal(pdf.Bytes(), want) {
		t.Errorf("the PDF differs from testdata/%s.pdf", name)
	}
	wantPages, _ := os.ReadFile("testdata/" + name + ".pages")
	if got := sums.String(); got != strings.ReplaceAll(string(wantPages), "\r\n", "\n") {
		t.Errorf("the pages, drawn, differ:\n%s\nwant\n%s", got, wantPages)
	}
}

func TestTheGoldenSheet(t *testing.T) {
	golden(t, "sheet", sampleSheet)
	d := sampleSheet(t)
	log := strings.Join(d.log, "\n")
	// Four pictures, the one without an image left out; every fact with a
	// value, the empty one too; the next asset on a page of its own.
	if n := strings.Count(log, "figure"); n != 4 {
		t.Errorf("%d figures", n)
	}
	if !strings.Contains(log, "fact Condition") || !strings.Contains(log, "fact Empty") {
		t.Errorf("facts: %s", log)
	}
	if len(d.Pages()) != 2 {
		t.Errorf("%d pages", len(d.Pages()))
	}
	// A page with nothing on it yet is not left blank.
	d.NewPage()
	d.NewPage()
	if len(d.Pages()) != 3 {
		t.Errorf("%d pages: an empty page was started again", len(d.Pages()))
	}
}

func TestTheGoldenChecklist(t *testing.T) {
	golden(t, "checklist", sampleChecklist)
	d := sampleChecklist(t)
	if len(d.Pages()) < 2 {
		t.Errorf("%d pages: the checklist should run over several", len(d.Pages()))
	}
	// Each row has its box: four thin rectangles left of the text.
	boxes := 0
	for _, p := range d.Pages() {
		for _, it := range p.Items {
			if len(it.Rects) == 1 && it.Rects[0].X < int(pt(docMargin+docCheckboxW)) && it.Rects[0].W <= int(pt(docCheckbox))+1 && it.Rects[0].H <= int(pt(docCheckbox))+1 {
				boxes++
			}
		}
	}
	if boxes != 4*80 {
		t.Errorf("%d box sides for 80 rows", boxes)
	}
}
