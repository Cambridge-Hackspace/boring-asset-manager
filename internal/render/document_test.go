package render

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"image"
	"image/color"
	"os"
	"strings"
	"testing"
)

// sampleDocument is a report's worth of everything a document draws: a
// heading, text, a grouped table long enough to run over pages, and each
// kind of chart.
func sampleDocument(t *testing.T, opts DocumentOptions) *Document {
	t.Helper()
	d, err := NewDocument(opts)
	if err != nil {
		t.Fatal(err)
	}
	d.Heading("Running low")
	d.Paragraph("Everything in a category flagged as running low, with where it is, so that the shopping list is one page to take to the shop.", Black)
	table := Table{Columns: []string{"Asset", "In stock", "Where"}}
	for g := 0; g < 3; g++ {
		group := TableGroup{Title: fmt.Sprintf("Category %d", g+1)}
		for r := 0; r < 25; r++ {
			group.Rows = append(group.Rows, TableRow{Cells: []string{
				fmt.Sprintf("Asset %d-%d with a name long enough to be cut short at the column's edge, as some are", g+1, r+1),
				fmt.Sprint(r * 7),
				"Shelf " + string(rune('A'+r%5)),
			}})
		}
		table.Groups = append(table.Groups, group)
	}
	d.Table(table)
	series := []ChartSeries{
		{Label: "Used", Points: []ChartPoint{{"Jan", 4}, {"Feb", 9}, {"Mar", 2}, {"Apr", 12}}},
		{Label: "Restocked", Points: []ChartPoint{{"Jan", 10}, {"Feb", 0}, {"Mar", 8}, {"Apr", 3}}},
	}
	for _, kind := range []string{ChartBar, ChartDonut, ChartLine} {
		if err := d.Chart(Chart{Title: "A " + kind + " chart", Kind: kind, Series: series}); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

var sampleOptions = DocumentOptions{Size: A4, Instance: "Cambridge Hackspace", Title: "Running low", Made: "Made 2026-11-04 09:30 UTC by Ada Lovelace"}

// TestTheGoldenDocument checks a document against testdata/document.pdf,
// and each of its pages, drawn, against testdata/document.pages, which
// holds each page's pixels' SHA-256. Run with BAM_UPDATE_FIXTURES=1 to
// write them again, after opening the PDF to check it.
func TestTheGoldenDocument(t *testing.T) {
	pages := sampleDocument(t, sampleOptions).Pages()
	var pdf bytes.Buffer
	if err := WritePDF(&pdf, pages...); err != nil {
		t.Fatal(err)
	}
	var sums strings.Builder
	for i, p := range pages {
		fmt.Fprintf(&sums, "page %d %x\n", i+1, sha256.Sum256(p.Raster(false).Pix))
	}
	if os.Getenv("BAM_UPDATE_FIXTURES") != "" {
		_ = os.WriteFile("testdata/document.pdf", pdf.Bytes(), 0o644)
		_ = os.WriteFile("testdata/document.pages", []byte(sums.String()), 0o644)
	}
	want, _ := os.ReadFile("testdata/document.pdf")
	if !bytes.Equal(pdf.Bytes(), want) {
		t.Error("the PDF differs from testdata/document.pdf")
	}
	wantPages, _ := os.ReadFile("testdata/document.pages")
	if got := sums.String(); got != strings.ReplaceAll(string(wantPages), "\r\n", "\n") {
		t.Errorf("the pages, drawn, differ:\n%s\nwant\n%s", got, wantPages)
	}

	// Made again, it is the same to the byte.
	var again bytes.Buffer
	_ = WritePDF(&again, sampleDocument(t, sampleOptions).Pages()...)
	if !bytes.Equal(pdf.Bytes(), again.Bytes()) {
		t.Error("the same document differs when made again")
	}
}

func TestLongTablesRepeatTheirHeaderRowAndKeepGroupTitlesWithRows(t *testing.T) {
	d := sampleDocument(t, DocumentOptions{Size: Letter, Title: "Long"})
	pages := d.Pages()
	if len(pages) < 3 {
		t.Fatalf("%d pages: the table should run over several", len(pages))
	}
	rowsOnPage := 0
	for i, e := range d.log {
		switch {
		case e == "page":
			rowsOnPage = 0
		case e == "row":
			rowsOnPage++
			// A row on a page is under that page's heading row.
			last := ""
			for j := i - 1; j >= 0 && d.log[j] != "page"; j-- {
				if d.log[j] == "header" {
					last = "header"
					break
				}
			}
			if last != "header" {
				t.Fatalf("a row with no heading row above it on its page, at %d: %v", i, d.log[:i+1])
			}
		case strings.HasPrefix(e, "title ") && !strings.HasSuffix(e, "(continued)"):
			// A group's title is followed by two of its rows before any
			// page breaks.
			if i+2 >= len(d.log) || d.log[i+1] != "row" || d.log[i+2] != "row" {
				t.Errorf("%q is not kept with its first rows: %v", e, d.log[i:min(i+4, len(d.log))])
			}
		}
	}
	headers := strings.Count(strings.Join(d.log, "\n"), "header")
	if headers < 2 {
		t.Errorf("the heading row was drawn %d times", headers)
	}
}

func TestPicturesAreDrawnOnlyWhereThereAreSome(t *testing.T) {
	photo := image.NewRGBA(image.Rect(0, 0, 40, 30))
	for i := range photo.Pix {
		photo.Pix[i] = 0x80
	}
	images := func(withPictures bool) int {
		d, _ := NewDocument(DocumentOptions{})
		tbl := Table{Columns: []string{"Asset"}, Groups: []TableGroup{{Rows: []TableRow{{Cells: []string{"Drill"}}, {Cells: []string{"Saw"}}}}}}
		if withPictures {
			tbl.PictureColumns = []string{"Photo", "Label"}
			tbl.Groups[0].Rows[0].Pictures = []image.Image{photo, nil}
		}
		d.Table(tbl)
		n := 0
		for _, it := range d.Pages()[0].Items {
			if it.Image != nil {
				n++
			}
		}
		return n
	}
	if n := images(false); n != 0 {
		t.Errorf("without pictures: %d images", n)
	}
	if n := images(true); n != 1 {
		t.Errorf("with one picture: %d images", n)
	}
}

func TestShadesOfGray(t *testing.T) {
	s := &Scene{Width: 4, Height: 1, DPI: 72, Items: []Item{
		{Rects: []Rect{{0, 0, 1, 1}}, Paint: Gray90},
		{Rects: []Rect{{1, 0, 1, 1}}, Paint: Gray50},
		{Rects: []Rect{{2, 0, 1, 1}}, Paint: Black},
	}}
	img := s.Raster(false)
	if got := []uint8{img.GrayAt(0, 0).Y, img.GrayAt(1, 0).Y, img.GrayAt(2, 0).Y, img.GrayAt(3, 0).Y}; got[0] != 230 || got[1] != 128 || got[2] != 0 || got[3] != 0xFF {
		t.Errorf("raster: %v", got)
	}
	var pdf bytes.Buffer
	if err := WritePDF(&pdf, s); err != nil {
		t.Fatal(err)
	}
	var svg bytes.Buffer
	if err := WriteSVG(&svg, s); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(svg.String(), `fill="#e6e6e6"`) || !strings.Contains(svg.String(), `fill="#808080"`) {
		t.Errorf("svg: %s", svg.String())
	}
	_ = color.Gray{}
}

func TestPageSizes(t *testing.T) {
	for _, c := range []struct {
		size      PageSize
		landscape bool
		w, h      int
	}{{Letter, false, 6120, 7920}, {Letter, true, 7920, 6120}, {A4, false, 5953, 8419}} {
		d, _ := NewDocument(DocumentOptions{Size: c.size, Landscape: c.landscape})
		if p := d.Pages()[0]; p.Width != c.w || p.Height != c.h {
			t.Errorf("%s landscape=%v: %dx%d", c.size.Name, c.landscape, p.Width, p.Height)
		}
	}
	if _, ok := PageSizeNamed("A4"); !ok {
		t.Error("A4 not found")
	}
	if _, ok := PageSizeNamed("legal"); ok {
		t.Error("legal found")
	}
}
