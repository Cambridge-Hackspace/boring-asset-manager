package brotherql

import (
	"bytes"
	"flag"
	"image"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/*.bin")

func TestPackBitsKnownVector(t *testing.T) {
	// Apple Technical Note TN1023.
	raw := []byte{0xAA, 0xAA, 0xAA, 0x80, 0x00, 0x2A, 0xAA, 0xAA, 0xAA, 0xAA, 0x80, 0x00, 0x2A, 0x22,
		0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA}
	packed := []byte{0xFE, 0xAA, 0x02, 0x80, 0x00, 0x2A, 0xFD, 0xAA, 0x03, 0x80, 0x00, 0x2A, 0x22, 0xF7, 0xAA}
	if got := packBits(raw); !bytes.Equal(got, packed) {
		t.Errorf("packBits = % X, want % X", got, packed)
	}
	if got, err := unpackBits(packed); err != nil || !bytes.Equal(got, raw) {
		t.Errorf("unpackBits = % X, %v", got, err)
	}
}

func TestPackBitsRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for n := 0; n < 400; n++ {
		data := make([]byte, rng.Intn(300))
		for i := range data {
			// Small alphabet so runs of every length appear.
			data[i] = byte(rng.Intn(3))
		}
		packed := packBits(data)
		got, err := unpackBits(packed)
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("round trip of % X failed: % X, %v", data, got, err)
		}
		if len(packed) > len(data)+(len(data)+127)/128 {
			t.Fatalf("%d bytes packed to %d", len(data), len(packed))
		}
	}
	// A blank raster line compresses to two bytes for each run of up to
	// 128: two on a 720-pin head, four on a 1,296-pin one.
	for n, want := range map[int]int{56: 2, 90: 2, 162: 4} {
		if got := packBits(make([]byte, n)); len(got) != want {
			t.Errorf("blank %d-byte line packed to % X", n, got)
		}
	}
}

func model(t *testing.T, name string) Model {
	t.Helper()
	m, ok := LookupModel(name)
	if !ok {
		t.Fatalf("no model %s", name)
	}
	return m
}

func layout(t *testing.T, modelName string, s Series, mediaID string) Layout {
	t.Helper()
	m, ok := LookupSeriesMedia(s, mediaID)
	if !ok {
		t.Fatalf("no roll %s", mediaID)
	}
	l, err := model(t, modelName).Layout(m)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// TestPinTablesFillTheHead checks the catalog against the column of
// Brother's pin tables it does not store: the pins left of the tape. With
// the printable and right-hand pins, they must make up the head. The
// figures are the references' own, but for the QL-1100's 62 mm die-cut
// labels, given 554 there (see dkCatalog).
func TestPinTablesFillTheHead(t *testing.T) {
	left := map[pinTable]map[string]int{
		// QL-800 series reference, section 2.3.5, which covers every roll
		// the other 720-pin references do.
		pins720: {
			"dk-22214": 585, "dk-22210": 408, "dk-22225": 295, "dk-22223": 154, "dk-n55224": 130, "dk-22205": 12,
			"dk-11204": 555, "dk-11203": 555, "dk-11221": 442, "brother-29x42": 408, "dk-11201": 408,
			"dk-11208": 295, "brother-39x48": 289, "brother-52x29": 142, "brother-54x29": 59, "brother-60x86": 24,
			"brother-62x29": 12, "brother-62x60": 12, "brother-62x75": 12, "dk-11202": 12, "dk-22251": 12,
			"dk-11219": 513, "dk-11218": 442, "dk-11207": 51,
		},
		// QL-500 to QL-1060N reference, section 3.2.5.
		pins1296QL1050: {
			"dk-22214": 1116, "dk-22210": 940, "dk-22225": 827, "dk-22223": 686, "dk-n55224": 662, "dk-22205": 544, "dk-22243": 76,
			"dk-11204": 1087, "dk-11203": 1087, "dk-11221": 976, "dk-11201": 940, "dk-11208": 827, "brother-39x48": 821,
			"brother-52x29": 674, "brother-62x29": 544, "dk-11202": 544, "dk-11240": 76, "dk-11241": 76,
			"dk-11219": 1046, "dk-11218": 975, "dk-11207": 584,
		},
		// QL-1100 series reference, section 2.3.5.
		pins1296QL1100: {
			"dk-22214": 1116, "dk-22210": 940, "dk-22225": 827, "dk-22223": 686, "dk-n55224": 662, "dk-22205": 544,
			"dk-22243": 76, "dk-22246": 58,
			"dk-11204": 1087, "dk-11203": 1087, "dk-11221": 975, "brother-29x42": 940, "dk-11201": 940, "dk-11208": 827,
			"brother-39x48": 821, "brother-52x29": 674, "brother-60x86": 556, "brother-62x29": 544, "dk-11202": 544,
			"dk-11240": 76, "dk-11241": 76, "dk-11247": 58,
			"dk-11219": 1046, "dk-11218": 975, "dk-11207": 584,
		},
	}
	heads := map[pinTable]int{pins720: 720, pins1296QL1050: 1296, pins1296QL1100: 1296}
	for table, rolls := range left {
		listed := 0
		for _, id := range MediaIDs() {
			m, _ := LookupMedia(id)
			l, ok := rolls[id]
			if m.right[table] == none {
				if ok {
					t.Errorf("table %d: %s is in Brother's table but not the catalog's", table, id)
				}
				continue
			}
			listed++
			if !ok {
				t.Errorf("table %d: %s has a right margin but Brother does not list it", table, id)
				continue
			}
			if sum := l + m.PrintableDots + m.right[table]; sum != heads[table] {
				t.Errorf("table %d: %s: %d + %d + %d pins = %d, not %d", table, id, l, m.PrintableDots, m.right[table], sum, heads[table])
			}
		}
		if listed != len(rolls) {
			t.Errorf("table %d: the catalog places %d rolls, Brother %d", table, listed, len(rolls))
		}
	}
	for _, id := range MediaIDs() {
		m, _ := LookupMedia(id)
		if m.DieCut() != (m.PrintableLines > 0) {
			t.Errorf("%s: die-cut %v with %d printable lines", id, m.DieCut(), m.PrintableLines)
		}
	}
	// Printers saved before the catalog grew name these.
	for _, id := range []string{"dk-22210", "dk-22205", "dk-11201"} {
		if _, ok := LookupMedia(id); !ok {
			t.Errorf("%s, which printers may already name, is missing", id)
		}
	}
}

// TestEveryQLModelPlacesItsRolls checks that every roll a model's
// reference lists is in its head's table.
func TestEveryQLModelPlacesItsRolls(t *testing.T) {
	for _, name := range Models(SeriesQL) {
		mo := model(t, name)
		taken := 0
		for _, id := range MediaIDs() {
			m, _ := LookupMedia(id)
			if m.lines&mo.lines == 0 {
				continue
			}
			taken++
			l, err := mo.Layout(m)
			if err != nil {
				t.Errorf("%s: %v", name, err)
				continue
			}
			if l.RightMarginDots+l.PrintableDots > mo.Pins {
				t.Errorf("%s: %s overhangs the head", name, id)
			}
		}
		if taken < 17 {
			t.Errorf("%s takes only %d rolls", name, taken)
		}
	}
}

func TestWhichModelsTakeWhichRolls(t *testing.T) {
	cases := []struct {
		model, media string
		takes        bool
	}{
		{"QL-810W", "dk-22205", true},
		{"QL-810W", "brother-54x29", true},
		{"QL-810W", "dk-22243", false},
		{"QL-820NWB", "dk-11241", false},
		{"QL-500", "brother-29x42", false},
		{"QL-500", "brother-60x86", false},
		{"QL-700", "dk-22205", true},
		{"QL-700", "dk-11240", false},
		{"QL-710W", "brother-60x86", true},
		{"QL-710W", "brother-54x29", false},
		{"QL-1050", "dk-22243", true},
		{"QL-1060N", "dk-11241", true},
		{"QL-1050", "dk-22246", false},
		{"QL-1050", "brother-29x42", false},
		{"QL-1100", "dk-11241", true},
		{"QL-1100", "dk-22246", true},
		{"QL-1110NWB", "dk-11247", true},
		{"QL-1115NWB", "dk-11241", true},
		{"QL-1115NWB", "dk-22246", false},
		{"QL-1115NWB", "dk-11247", false},
		{"QL-1100", "brother-62x60", false},
	}
	for _, c := range cases {
		m, _ := LookupMedia(c.media)
		if err := model(t, c.model).Takes(m); (err == nil) != c.takes {
			t.Errorf("%s takes %s: %v", c.model, c.media, err)
		}
	}
	// A QL takes no roll known only by size.
	if err := model(t, "QL-1100").Takes(SizedMedia("x", "Custom", 62, 0)); err == nil {
		t.Error("the QL-1100 took custom stock")
	}
}

// TestTDLayoutsMatchBrother checks the TD layouts, worked out from a
// roll's size, against the printable pins and lines the TD references give
// each RD roll (TD-2000 series sections 2.3.2 and 2.3.5, TD-4000 series
// sections 4.3.2 and 4.3.5), to within a dot.
func TestTDLayoutsMatchBrother(t *testing.T) {
	cases := []struct {
		model, media            string
		printable, right, lines int
	}{
		{"TD-2020", "rd-57", 432, 8, 0},
		{"TD-2020", "rd-58", 440, 4, 0},
		{"TD-2120N", "rd-51x26", 382, 33, 157},
		{"TD-2125N", "rd-30x30", 216, 116, 192},
		{"TD-2020", "rd-40x60", 296, 76, 432},
		{"TD-2020", "rd-50x30", 376, 36, 192},
		{"TD-2020", "rd-60x60", 448, 0, 432},
		{"TD-2130N", "rd-57", 638, 17, 0},
		{"TD-2135N", "rd-51x26", 564, 54, 231},
		{"TD-2130N", "rd-40x40", 436, 118, 401},
		{"TD-4410D", "rd-102", 788, 22, 0},
		{"TD-4420DN", "rd-90", 695, 68, 0},
		{"TD-4420DN", "rd-76", 583, 124, 0},
		{"TD-4410D", "rd-102x152", 788, 22, 1170},
		{"TD-4410D", "rd-76x26", 585, 123, 156},
		{"TD-4550DNWB", "rd-102", 1164, 58, 0},
		{"TD-4550DNWB", "rd-90", 1027, 126, 0},
		{"TD-4550DNWB", "rd-58", 649, 315, 0},
		{"TD-4550DNWB", "rd-102x152", 1164, 58, 1728},
		{"TD-4520DN", "rd-102x50", 1164, 58, 519},
		{"TD-4520DN", "rd-76x26", 864, 208, 232},
	}
	near := func(a, b int) bool { return a-b <= 2 && b-a <= 2 }
	for _, c := range cases {
		l := layout(t, c.model, SeriesTD, c.media)
		if !near(l.PrintableDots, c.printable) || !near(l.RightMarginDots, c.right) || !near(l.PrintableLines, c.lines) {
			t.Errorf("%s on %s: %d pins from %d, %d lines; Brother has %d from %d, %d lines",
				c.media, c.model, l.PrintableDots, l.RightMarginDots, l.PrintableLines, c.printable, c.right, c.lines)
		}
		if l.RightMarginDots+l.PrintableDots > l.Model.Pins {
			t.Errorf("%s on %s overhangs the head", c.media, c.model)
		}
	}
	// The one figure the offsets miss: a 60 mm label is wider than a
	// 300 DPI TD-2's head, which is used whole.
	if l := layout(t, "TD-2135N", SeriesTD, "rd-60x60"); l.PrintableDots != 672 || l.RightMarginDots != 0 {
		t.Errorf("60 mm on the TD-2135N: %d pins from %d", l.PrintableDots, l.RightMarginDots)
	}
	// Continuous rolls are fed Brother's 3 mm, die-cut labels nothing.
	if l := layout(t, "TD-2020", SeriesTD, "rd-58"); l.FeedDots != 24 {
		t.Errorf("TD-2020 feed = %d", l.FeedDots)
	}
	if l := layout(t, "TD-4550DNWB", SeriesTD, "rd-102"); l.FeedDots != 35 {
		t.Errorf("TD-4550DNWB feed = %d", l.FeedDots)
	}
}

func TestTDTakesRollsByWidth(t *testing.T) {
	cases := []struct {
		model         string
		width, length float64
		takes         bool
	}{
		{"TD-2020", 50, 30, true},
		{"TD-2020", 63, 0, true},
		{"TD-2020", 76, 0, false},
		{"TD-2020", 15, 0, false},
		{"TD-4410D", 118, 0, true},
		{"TD-4410D", 120, 0, false},
		{"TD-4410D", 100, 5, false},
		{"TD-4410D", 100, 3001, false},
		{"TD-4550DNWB", 100, 2900, true},
	}
	for _, c := range cases {
		if err := model(t, c.model).Takes(SizedMedia("x", "Custom", c.width, c.length)); (err == nil) != c.takes {
			t.Errorf("%s with %g × %g mm: %v", c.model, c.width, c.length, err)
		}
	}
	// The RD rolls go to the series whose reference sizes them.
	rd102, _ := LookupSeriesMedia(SeriesTD, "rd-102")
	if err := model(t, "TD-2020").Takes(rd102); err == nil {
		t.Error("the TD-2020 took 102 mm tape")
	}
	rd57, _ := LookupSeriesMedia(SeriesTD, "rd-57")
	if err := model(t, "TD-4410D").Takes(rd57); err == nil {
		t.Error("the TD-4410D took the TD-2's 57 mm tape")
	}
	if err := model(t, "TD-4410D").Takes(rd102); err != nil {
		t.Error(err)
	}
	// A TD takes no DK roll.
	dk, _ := LookupMedia("dk-22205")
	if err := model(t, "TD-4410D").Takes(dk); err == nil {
		t.Error("the TD-4410D took a DK roll")
	}
}

func TestSmallRoundLabelsFeedOnTheOlderModels(t *testing.T) {
	// QL-500 to QL-1060N reference, section 3.2.3.
	for name, feed := range map[string]int{"QL-500": 35, "QL-700": 35, "QL-580N": 35, "QL-650TD": 0, "QL-1050": 0, "QL-810W": 0} {
		if l := layout(t, name, SeriesQL, "dk-11219"); l.FeedDots != feed {
			t.Errorf("%s feeds 12 mm round labels %d dots, want %d", name, l.FeedDots, feed)
		}
	}
	if l := layout(t, "QL-500", SeriesQL, "dk-11201"); l.FeedDots != 0 {
		t.Errorf("QL-500 feeds DK-11201 %d dots", l.FeedDots)
	}
}

// markedLabel draws a dark dot near the top-left corner of a white label so
// orientation can be checked.
func markedLabel(w, h int) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 0xFF
	}
	for y := 40; y < 50; y++ {
		for x := 40; x < 50; x++ {
			img.Pix[y*img.Stride+x] = 0
		}
	}
	return img
}

func darkBounds(img *image.Gray) image.Rectangle {
	var r image.Rectangle
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			if img.Pix[y*img.Stride+x] < 0x80 {
				r = r.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	return r
}

func TestPrepareCropsAndRotates(t *testing.T) {
	dk11201, _ := LookupMedia("dk-11201")

	// 29 × 90 mm at 300 DPI renders as 343 × 1063 dots; the printable window
	// is 306 × 991 around the center, so the crop offset is (18, 36).
	page := dk11201.Prepare(markedLabel(343, 1063), Placement{})
	if b := page.Bounds(); b.Dx() != 306 || b.Dy() != 991 {
		t.Fatalf("page is %v, want 306×991", b)
	}
	if got, want := darkBounds(page), image.Rect(22, 4, 32, 14); got != want {
		t.Errorf("mark at %v, want %v", got, want)
	}

	// A 90 × 29 mm label turns counterclockwise: its top-left corner ends up
	// at the bottom-left, so the mark moves from (40,40) to (40, 1063-50).
	page = dk11201.Prepare(markedLabel(1063, 343), Placement{Rotate: true})
	if b := page.Bounds(); b.Dx() != 306 || b.Dy() != 991 {
		t.Fatalf("rotated page is %v, want 306×991", b)
	}
	if got, want := darkBounds(page), image.Rect(40-18, 1063-50-36, 50-18, 1063-40-36); got != want {
		t.Errorf("rotated mark at %v, want %v", got, want)
	}

	dk22210, _ := LookupMedia("dk-22210")
	if b := dk22210.Prepare(markedLabel(343, 600), Placement{}).Bounds(); b.Dx() != 306 || b.Dy() != 600 {
		t.Errorf("continuous page is %v, want every line kept", b)
	}

	// A TD layout crops the same way, to its own figures: 51 × 26 mm at
	// 203 DPI renders as 408 × 208 dots, cropped to 384 × 160.
	l := layout(t, "TD-2020", SeriesTD, "rd-51x26")
	if b := l.Prepare(markedLabel(408, 208), Placement{}).Bounds(); b.Dx() != l.PrintableDots || b.Dy() != l.PrintableLines {
		t.Errorf("TD page is %v, want %d × %d", b, l.PrintableDots, l.PrintableLines)
	}
}

func checkerPage(dots, lines int) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, dots, lines))
	for y := 0; y < lines; y++ {
		for x := 0; x < dots; x++ {
			if (x/16+y/16)%2 == 0 && x != 0 {
				continue
			}
			img.Pix[y*img.Stride+x] = 0xFF
		}
	}
	// The leftmost column is dark on every line, so the right margin and
	// mirroring are visible in the stream.
	for y := 0; y < lines; y++ {
		img.Pix[y*img.Stride] = 0
	}
	return img
}

// checkStream pins a stream to its golden file, then checks that decoding
// it for the layout's model gives back exactly the pages that went in.
func checkStream(t *testing.T, name string, l Layout, pages []*image.Gray, stream []byte, cut bool) []Page {
	t.Helper()
	path := filepath.Join("testdata", name+".bin")
	if *update {
		if err := os.WriteFile(path, stream, 0o644); err != nil {
			t.Fatal(err)
		}
	} else if want, err := os.ReadFile(path); err != nil || !bytes.Equal(stream, want) {
		t.Errorf("%s: stream differs from %s (%v)", name, path, err)
	}

	decoded, err := l.Model.DecodeJob(stream)
	if err != nil {
		t.Fatalf("%s: decoding: %v", name, err)
	}
	if len(decoded) != len(pages) || !decoded[len(decoded)-1].Last {
		t.Fatalf("%s: decoded %d pages with wrong print commands", name, len(decoded))
	}
	m := l.Media
	wantType := byte(mediaContinuous)
	if m.DieCut() {
		wantType = mediaDieCut
	}
	for i, p := range decoded {
		if p.Last != (i == len(decoded)-1) {
			t.Errorf("%s page %d: last %v", name, i, p.Last)
		}
		if p.MediaType != wantType || p.LengthMM != byte(m.LengthMM+0.5) || p.AutoCut != cut || p.InfoFlags != l.Model.InfoFlags || p.FeedDots != l.FeedDots {
			t.Errorf("%s page %d: print information %+v", name, i, p)
		}
		if got := l.PageImage(p); !bytes.Equal(got.Pix, pages[i].Pix) {
			t.Errorf("%s page %d: decoded bitmap differs from the input", name, i)
		}
	}

	// Pin 0 sits at the right of the tape: the page's leftmost column must
	// land on the last printable pin, just inside the right margin.
	lastPin := l.RightMarginDots + l.PrintableDots - 1
	if decoded[0].Lines[0][lastPin/8]&(0x80>>uint(lastPin%8)) == 0 {
		t.Errorf("%s: leftmost column is not on pin %d", name, lastPin)
	}
	for pin := 0; pin < l.RightMarginDots; pin++ {
		if decoded[0].Lines[0][pin/8]&(0x80>>uint(pin%8)) != 0 {
			t.Errorf("%s: pin %d beside the tape is set", name, pin)
		}
	}
	return decoded
}

// TestEncodeJobGoldens pins the QL-810W's byte stream for each roll it
// takes. These files predate the other models: the QL-800 series' streams
// must not change.
func TestEncodeJobGoldens(t *testing.T) {
	mo := model(t, "QL-810W")
	n := 0
	for _, id := range MediaIDs() {
		m, _ := LookupMedia(id)
		l, err := mo.Layout(m)
		if err != nil {
			continue
		}
		n++
		lines := l.PrintableLines
		if lines == 0 {
			lines = 400
		}
		pages := []*image.Gray{checkerPage(l.PrintableDots, lines), markedLabel(l.PrintableDots, lines)}
		stream, err := EncodeJob(l, pages, JobOptions{Cut: true})
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		decoded := checkStream(t, id, l, pages, stream, true)
		for i, p := range decoded {
			if p.WidthMM != byte(m.WidthMM) {
				t.Errorf("%s page %d: width %d", id, i, p.WidthMM)
			}
		}
		// The QL-810W's own figures, as the catalog had them.
		if mo.Invalidate != 200 || !bytes.HasPrefix(stream[200:], []byte{0x1B, '@', 0x1B, 'i', 'a', 1, 0x1B, 'i', 'z', 0xCE}) {
			t.Errorf("%s: stream starts % X", id, stream[195:215])
		}
	}
	// The 23 it always has, and DK-22251 in red and black.
	if n != 24 {
		t.Errorf("the QL-810W takes %d rolls, not 24", n)
	}
}

// TestEncodeJobGoldensPerFamily pins a stream for a model of each family.
func TestEncodeJobGoldensPerFamily(t *testing.T) {
	cases := []struct {
		golden, model string
		series        Series
		media         string
		cut           bool
		// commands that must, or must not, be in the stream.
		has, lacks [][]byte
		width      byte
	}{
		// No cutter, no mode switching, no compression: 90-byte lines.
		{"ql-500-dk-22205", "QL-500", SeriesQL, "dk-22205", false,
			nil, [][]byte{cmdRasterMode, cmdVariousMode, cmdCutEvery, cmdExpandedMode, cmdCompressTIFF}, 62},
		// A cutter, but no mode switching or compression.
		{"ql-700-dk-11201", "QL-700", SeriesQL, "dk-11201", true,
			[][]byte{cmdVariousMode, cmdCutEvery, cmdExpandedMode}, [][]byte{cmdRasterMode, cmdCompressTIFF}, 29},
		// The 1,296-pin head with a 4-inch roll, compressed, 350 invalidate bytes.
		{"ql-1100-dk-11241", "QL-1100", SeriesQL, "dk-11241", true,
			[][]byte{cmdRasterMode, cmdCompressTIFF}, nil, 102},
		// The 103 mm roll is sent as 104 mm.
		{"ql-1110nwb-dk-22246", "QL-1110NWB", SeriesQL, "dk-22246", false,
			[][]byte{cmdRasterMode, cmdCompressTIFF}, [][]byte{cmdCutEvery}, 104},
		{"ql-1050-dk-22243", "QL-1050", SeriesQL, "dk-22243", true,
			[][]byte{cmdRasterMode, cmdCompressTIFF}, nil, 102},
		// A TD-2: 203 DPI, 448 pins, no cut commands.
		{"td-2020-rd-51x26", "TD-2020", SeriesTD, "rd-51x26", false,
			[][]byte{cmdRasterMode, append(append([]byte{}, cmdVariousMode...), 0), cmdCompressTIFF}, [][]byte{cmdCutEvery, cmdExpandedMode}, 51},
		// A TD-4 at 300 DPI, 1,280 pins, with its cutter.
		{"td-4550dnwb-rd-102x152", "TD-4550DNWB", SeriesTD, "rd-102x152", true,
			[][]byte{cmdRasterMode, cmdCutEvery, cmdExpandedMode, cmdCompressTIFF}, nil, 102},
		{"td-4420dn-rd-102", "TD-4420DN", SeriesTD, "rd-102", false,
			[][]byte{cmdRasterMode, cmdExpandedMode, cmdCompressTIFF}, [][]byte{cmdCutEvery}, 102},
	}
	for _, c := range cases {
		l := layout(t, c.model, c.series, c.media)
		lines := l.PrintableLines
		if lines == 0 {
			lines = 200
		}
		pages := []*image.Gray{checkerPage(l.PrintableDots, lines), markedLabel(l.PrintableDots, lines)}
		stream, err := EncodeJob(l, pages, JobOptions{Cut: c.cut})
		if err != nil {
			t.Fatalf("%s: %v", c.golden, err)
		}
		decoded := checkStream(t, c.golden, l, pages, stream, c.cut)
		if decoded[0].WidthMM != c.width {
			t.Errorf("%s: sent width %d, want %d", c.golden, decoded[0].WidthMM, c.width)
		}
		if !bytes.Equal(stream[:l.Model.Invalidate], make([]byte, l.Model.Invalidate)) || stream[l.Model.Invalidate] != 0x1B {
			t.Errorf("%s: not %d invalidate bytes", c.golden, l.Model.Invalidate)
		}
		if got := len(decoded[0].Lines[0]); got != l.Model.Pins/8 {
			t.Errorf("%s: %d-byte lines", c.golden, got)
		}
		head := stream[:bytes.IndexByte(stream, cmdRasterLine)]
		for _, cmd := range c.has {
			if !bytes.Contains(head, cmd) {
				t.Errorf("%s: no % X before the raster", c.golden, cmd)
			}
		}
		for _, cmd := range c.lacks {
			if bytes.Contains(head, cmd) {
				t.Errorf("%s: % X, which the %s does not take", c.golden, cmd, c.model)
			}
		}
	}
}

func TestUncompressedLinesSpanTheHead(t *testing.T) {
	l := layout(t, "QL-500", SeriesQL, "dk-22205")
	stream, err := EncodeJob(l, []*image.Gray{checkerPage(l.PrintableDots, 3)}, JobOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// g 00 5A and 90 bytes, as in the QL-500 reference's example (3.1).
	if !bytes.Contains(stream, []byte{'g', 0x00, 90}) || bytes.Contains(stream, cmdCompressTIFF) {
		t.Error("QL-500 lines are not 90 uncompressed bytes")
	}
}

func TestEncodeJobRefusesCuttingWithoutACutter(t *testing.T) {
	for _, c := range []struct {
		model  string
		series Series
		media  string
	}{{"QL-500", SeriesQL, "dk-22205"}, {"TD-2130N", SeriesTD, "rd-58"}} {
		l := layout(t, c.model, c.series, c.media)
		_, err := EncodeJob(l, []*image.Gray{checkerPage(l.PrintableDots, 300)}, JobOptions{Cut: true})
		if err == nil || !strings.Contains(err.Error(), "no cutter") {
			t.Errorf("%s: cut job gave %v", c.model, err)
		}
	}
}

func TestDecodeJobRefusesWhatTheModelDoesNotTake(t *testing.T) {
	l := layout(t, "QL-810W", SeriesQL, "dk-22205")
	stream, err := EncodeJob(l, []*image.Gray{checkerPage(l.PrintableDots, 300)}, JobOptions{Cut: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"QL-500", "QL-700", "QL-800"} {
		if _, err := model(t, name).DecodeJob(stream); err == nil {
			t.Errorf("the %s accepted a QL-810W stream", name)
		}
	}
}

func TestEncodeJobRejectsMisshapenPages(t *testing.T) {
	l := layout(t, "QL-810W", SeriesQL, "dk-11201")
	if _, err := EncodeJob(l, nil, JobOptions{}); err == nil {
		t.Error("empty job accepted")
	}
	if _, err := EncodeJob(l, []*image.Gray{markedLabel(300, 991)}, JobOptions{}); err == nil {
		t.Error("wrong width accepted")
	}
	if _, err := EncodeJob(l, []*image.Gray{markedLabel(306, 500)}, JobOptions{}); err == nil {
		t.Error("wrong die-cut length accepted")
	}
}

func TestDecodeJobRejectsDamagedStreams(t *testing.T) {
	l := layout(t, "QL-810W", SeriesQL, "dk-22210")
	stream, err := EncodeJob(l, []*image.Gray{checkerPage(l.PrintableDots, 300)}, JobOptions{Cut: true})
	if err != nil {
		t.Fatal(err)
	}
	for name, damaged := range map[string][]byte{
		"truncated":      stream[:len(stream)-40],
		"no final print": stream[:len(stream)-1],
		"no invalidate":  stream[100:],
		"trailing bytes": append(append([]byte{}, stream...), 0x00),
	} {
		if _, err := DecodeJob(damaged); err == nil {
			t.Errorf("%s stream accepted", name)
		}
	}
}

func TestEncodeJobWithoutCutting(t *testing.T) {
	l := layout(t, "QL-810W", SeriesQL, "dk-22210")
	page := checkerPage(l.PrintableDots, 300)
	cut, _ := EncodeJob(l, []*image.Gray{page}, JobOptions{Cut: true})
	uncut, err := EncodeJob(l, []*image.Gray{page}, JobOptions{Cut: false})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(uncut, cmdCutEvery) {
		t.Error("uncut job still sets a cut interval")
	}
	if !bytes.Contains(uncut, append(append([]byte{}, cmdVariousMode...), 0)) ||
		!bytes.Contains(uncut, append(append([]byte{}, cmdExpandedMode...), 0)) {
		t.Error("uncut job does not switch auto-cut and cut-at-end off")
	}
	decoded, err := DecodeJob(uncut)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := LookupMedia("dk-22210")
	if decoded[0].AutoCut || !bytes.Equal(PageImage(m, decoded[0]).Pix, page.Pix) {
		t.Error("uncut job decoded wrongly")
	}
	if c, _ := DecodeJob(cut); !c[0].AutoCut {
		t.Error("cut job decoded without auto-cut")
	}
}

// TestTwoColorJob prints black and red on DK-22251 in the QL-800 series,
// each line of the label as a line of black and a line of red, red only
// where black is not; and in black alone on any other roll or model.
func TestTwoColorJob(t *testing.T) {
	red, _ := LookupMedia("dk-22251")
	for _, c := range []struct {
		model string
		takes bool
	}{{"QL-800", true}, {"QL-810W", true}, {"QL-820NWB", true}, {"QL-700", false}, {"QL-1100", false}} {
		if err := model(t, c.model).Takes(red); (err == nil) != c.takes {
			t.Errorf("%s takes DK-22251: %v", c.model, err)
		}
	}
	for _, name := range []string{"QL-800", "QL-810W"} {
		mo := model(t, name)
		l, err := mo.Layout(red)
		if err != nil || !l.TwoColor() {
			t.Fatalf("%s: %v, two colors %v", name, err, l.TwoColor())
		}
		black := image.NewGray(image.Rect(0, 0, l.PrintableDots, 300))
		reds := image.NewGray(black.Rect)
		for i := range black.Pix {
			black.Pix[i], reds.Pix[i] = 0xFF, 0xFF
		}
		for y := 0; y < 300; y++ {
			for x := 0; x < 100; x++ {
				black.Pix[y*black.Stride+x] = 0  // black at the left
				reds.Pix[y*reds.Stride+x+50] = 0 // red overlapping it, then alone
			}
		}
		job, err := EncodeTwoColorJob(l, []*image.Gray{black}, []*image.Gray{reds}, JobOptions{Cut: true})
		if err != nil {
			t.Fatal(err)
		}
		pages, err := mo.DecodeJob(job)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		p := pages[0]
		if len(pages) != 1 || !p.TwoColor || len(p.Lines) != 300 || len(p.RedLines) != 300 || !p.AutoCut {
			t.Fatalf("%s: %d pages, two colors %v, %d and %d lines", name, len(pages), p.TwoColor, len(p.Lines), len(p.RedLines))
		}
		gotBlack := l.PageImage(p)
		gotRed := l.PageImage(Page{Lines: p.RedLines})
		for x := 0; x < l.PrintableDots; x++ {
			wantBlack, wantRed := x < 100, x >= 100 && x < 150
			if (gotBlack.Pix[x] == 0) != wantBlack || (gotRed.Pix[x] == 0) != wantRed {
				t.Fatalf("%s: dot %d black %v red %v", name, x, gotBlack.Pix[x] == 0, gotRed.Pix[x] == 0)
			}
		}
		// A label in black alone is still sent in two colors, red blank.
		job, err = EncodeJob(l, []*image.Gray{black}, JobOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if pages, err := mo.DecodeJob(job); err != nil || !pages[0].TwoColor || len(pages[0].RedLines) != 300 {
			t.Errorf("%s: black alone on DK-22251: %v", name, err)
		}
	}
	// On black-and-white tape the series prints black alone.
	l, err := model(t, "QL-810W").Layout(mustMedia(t, "dk-22205"))
	if err != nil {
		t.Fatal(err)
	}
	page := image.NewGray(image.Rect(0, 0, l.PrintableDots, 300))
	if _, err := EncodeTwoColorJob(l, []*image.Gray{page}, []*image.Gray{page}, JobOptions{}); err == nil {
		t.Error("two colors on black-and-white tape")
	}
	job, err := EncodeJob(l, []*image.Gray{page}, JobOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if pages, err := DecodeJob(job); err != nil || pages[0].TwoColor {
		t.Errorf("black-and-white tape sent in two colors: %v", err)
	}
	// A model that prints in black alone refuses two-color streams.
	two, _ := model(t, "QL-810W").Layout(red)
	job, err = EncodeTwoColorJob(two, []*image.Gray{page}, []*image.Gray{page}, JobOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ql710 := model(t, "QL-710W")
	if _, err := ql710.DecodeJob(job); err == nil || !strings.Contains(err.Error(), "two-color") {
		t.Errorf("the QL-710W took a two-color job: %v", err)
	}
}

func mustMedia(t *testing.T, id string) Media {
	t.Helper()
	m, ok := LookupMedia(id)
	if !ok {
		t.Fatalf("no roll %s", id)
	}
	return m
}
