package ptouch

import (
	"bytes"
	"errors"
	"flag"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/*.bin")

func model(t *testing.T, name string) Model {
	t.Helper()
	m, ok := LookupModel(name)
	if !ok {
		t.Fatalf("no model %s", name)
	}
	return m
}

func layout(t *testing.T, modelName, mediaID string) Layout {
	t.Helper()
	m, ok := LookupMedia(mediaID)
	if !ok {
		t.Fatalf("no cassette %s", mediaID)
	}
	l, err := model(t, modelName).Layout(m)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// TestPinTablesFillTheHead checks the catalog against the column of
// Brother's tables of pins (section 2.3.5) it does not store: the pins left
// of the tape. With the printable and right-hand pins, they must make up
// the head.
func TestPinTablesFillTheHead(t *testing.T) {
	left := map[head]map[string]int{
		head128: {
			"tze-3.5": 52, "tze-6": 48, "tze-9": 39, "tze-12": 29, "tze-18": 8, "tze-24": 0,
			"hse-5.8": 50, "hse-8.8": 40, "hse-11.7": 31, "hse-17.7": 11, "hse-23.6": 0,
			"hse-5.2": 54, "hse-9": 42, "hse-11.2": 39, "hse-21": 4,
		},
		head560: {
			"tze-3.5": 248, "tze-6": 240, "tze-9": 219, "tze-12": 197, "tze-18": 155, "tze-24": 112, "tze-36": 45,
			"hse-5.8": 244, "hse-8.8": 224, "hse-11.7": 206, "hse-17.7": 166, "hse-23.6": 144,
			"hse-5.2": 252, "hse-9": 228, "hse-11.2": 222, "hse-21": 152, "hse-31": 92,
		},
	}
	total := map[head]int{head128: 128, head560: 560}
	for h, rolls := range left {
		placed := 0
		for _, id := range MediaIDs() {
			p := placements[id][h]
			l, ok := rolls[id]
			if p == nil {
				if ok {
					t.Errorf("head %d: Brother places %s, the catalog does not", h, id)
				}
				continue
			}
			placed++
			if !ok {
				t.Errorf("head %d: the catalog places %s, Brother does not", h, id)
				continue
			}
			if sum := l + p.printable + p.right; sum != total[h] {
				t.Errorf("head %d: %s: %d + %d + %d pins = %d, not %d", h, id, l, p.printable, p.right, sum, total[h])
			}
		}
		if placed != len(rolls) {
			t.Errorf("head %d: %d cassettes placed, Brother has %d", h, placed, len(rolls))
		}
	}
}

func TestWhichModelsTakeWhichCassettes(t *testing.T) {
	cases := []struct {
		model, media string
		takes        bool
	}{
		{"PT-P750W", "tze-24", true},
		{"PT-P750W", "tze-36", false},
		{"PT-P700", "hse-21", true},
		{"PT-P700", "hse-31", false},
		{"PT-P950NW", "tze-36", true},
		{"PT-P950NW", "hse-31", true},
		{"PT-P910BT", "tze-36", true},
		{"PT-P910BT", "hse-5.8", false},
	}
	for _, c := range cases {
		m, _ := LookupMedia(c.media)
		if err := model(t, c.model).Takes(m); (err == nil) != c.takes {
			t.Errorf("%s takes %s: %v", c.model, c.media, err)
		}
	}
}

func checkerPage(dots, lines int) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, dots, lines))
	for y := 0; y < lines; y++ {
		for x := 0; x < dots; x++ {
			if (x/8+y/8)%2 == 0 && x != 0 {
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

func defaultOptions(mo Model) JobOptions {
	return JobOptions{Cut: CutEach, MarginDots: mo.MinMarginDots}
}

// TestEncodeJobGoldens pins the byte stream for every cassette on a model
// of each head, and checks that decoding it gives back exactly the pages
// that went in, placed on the tape's printable pins.
func TestEncodeJobGoldens(t *testing.T) {
	n := 0
	for _, name := range []string{"PT-P750W", "PT-P950NW"} {
		mo := model(t, name)
		for _, id := range MediaIDs() {
			m, _ := LookupMedia(id)
			l, err := mo.Layout(m)
			if err != nil {
				continue
			}
			n++
			pages := []*image.Gray{checkerPage(l.PrintableDots, 120), checkerPage(l.PrintableDots, 80)}
			stream, err := EncodeJob(l, pages, defaultOptions(mo))
			if err != nil {
				t.Fatalf("%s %s: %v", name, id, err)
			}
			golden := strings.ToLower(name) + "-" + id
			checkGolden(t, golden, stream)

			decoded, err := mo.DecodeJob(stream)
			if err != nil {
				t.Fatalf("%s: %v", golden, err)
			}
			if len(decoded) != 2 || decoded[0].Last || !decoded[1].Last {
				t.Fatalf("%s: decoded %d pages with wrong print commands", golden, len(decoded))
			}
			for i, p := range decoded {
				if p.WidthMM != byte(m.infoWidth) || p.MediaType != m.mediaType() || len(p.Lines[0]) != mo.Pins/8 {
					t.Errorf("%s page %d: %+v", golden, i, p)
				}
				if got := l.PageImage(p); !bytes.Equal(got.Pix, pages[i].Pix) {
					t.Errorf("%s page %d: decoded bitmap differs from the input", golden, i)
				}
			}
			// Pin 0 is at the right of the tape: the page's leftmost column
			// lands on the last printable pin, and no pin beside the tape is
			// set.
			last := l.RightMarginDots + l.PrintableDots - 1
			line := decoded[0].Lines[0]
			if line[last/8]&(0x80>>uint(last%8)) == 0 {
				t.Errorf("%s: leftmost column is not on pin %d", golden, last)
			}
			for pin := 0; pin < mo.Pins; pin++ {
				if (pin < l.RightMarginDots || pin > last) && line[pin/8]&(0x80>>uint(pin%8)) != 0 {
					t.Errorf("%s: pin %d beside the tape is set", golden, pin)
				}
			}
		}
	}
	if n != 15+17 {
		t.Errorf("%d goldens, want 32", n)
	}
	// The PT-P700, whose reference spells raster lines "g".
	l := layout(t, "PT-P700", "tze-12")
	stream, err := EncodeJob(l, []*image.Gray{checkerPage(l.PrintableDots, 60)}, defaultOptions(l.Model))
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "pt-p700-tze-12", stream)
	if _, err := l.Model.DecodeJob(stream); err != nil {
		t.Error(err)
	}
	if i := bytes.Index(stream, cmdCompressTIFF); i < 0 || stream[i+2] != 'g' {
		t.Error("PT-P700 raster lines are not sent with g")
	}
}

func checkGolden(t *testing.T, name string, stream []byte) {
	t.Helper()
	path := filepath.Join("testdata", name+".bin")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, stream, 0o644); err != nil {
			t.Fatal(err)
		}
	} else if want, err := os.ReadFile(path); err != nil || !bytes.Equal(stream, want) {
		t.Errorf("%s: stream differs from %s (%v)", name, path, err)
	}
}

// TestStreamHeader checks the commands before the raster against the
// references' sections 2.1 and 4.
func TestStreamHeader(t *testing.T) {
	l := layout(t, "PT-P750W", "tze-24")
	stream, err := EncodeJob(l, []*image.Gray{checkerPage(128, 100)}, defaultOptions(l.Model))
	if err != nil {
		t.Fatal(err)
	}
	want := append(make([]byte, 100), 0x1B, '@',
		0x1B, 'i', 'a', 0x01,
		// Valid width, recovery on; tape type 0, 24 mm, 100 lines, first page.
		0x1B, 'i', 'z', 0x84, 0x00, 24, 0x00, 100, 0, 0, 0, 0, 0,
		0x1B, 'i', 'M', 0x40,
		0x1B, 'i', 'A', 0x01,
		0x1B, 'i', 'K', 0x08,
		0x1B, 'i', 'd', 14, 0,
		'M', 0x02, 'G')
	if !bytes.HasPrefix(stream, want) {
		t.Errorf("stream starts\n% X\nwant\n% X", stream[:len(want)], want)
	}

	// 3.5 mm tape is sent as 4 mm, as the printer reports it.
	l = layout(t, "PT-P950NW", "tze-3.5")
	stream, _ = EncodeJob(l, []*image.Gray{checkerPage(l.PrintableDots, 100)}, defaultOptions(l.Model))
	if p, err := l.Model.DecodeJob(stream); err != nil || p[0].WidthMM != 4 || p[0].InfoFlags != 0x84 {
		t.Errorf("3.5 mm tape: %+v, %v", p, err)
	}
	// Tube is checked by its type.
	for id, typ := range map[string]byte{"hse-11.7": 0x11, "hse-11.2": 0x17} {
		l = layout(t, "PT-P750W", id)
		stream, _ = EncodeJob(l, []*image.Gray{checkerPage(l.PrintableDots, 100)}, defaultOptions(l.Model))
		if p, err := l.Model.DecodeJob(stream); err != nil || p[0].MediaType != typ || p[0].InfoFlags != 0x82 {
			t.Errorf("%s: %+v, %v", id, p, err)
		}
	}
}

// TestPageNumbers checks the print information's page numbers: the
// PT-P900 series marks the last page 2, and a job of one page with it.
func TestPageNumbers(t *testing.T) {
	for _, c := range []struct {
		model string
		pages int
		want  []byte
	}{
		{"PT-P750W", 3, []byte{0, 1, 1}},
		{"PT-P750W", 1, []byte{0}},
		{"PT-P900W", 3, []byte{0, 1, 2}},
		{"PT-P900W", 1, []byte{2}},
	} {
		l := layout(t, c.model, "tze-12")
		pages := make([]*image.Gray, c.pages)
		for i := range pages {
			pages[i] = checkerPage(l.PrintableDots, 100)
		}
		stream, err := EncodeJob(l, pages, defaultOptions(l.Model))
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := l.Model.DecodeJob(stream)
		if err != nil {
			t.Fatal(err)
		}
		for i, p := range decoded {
			if p.PageFlag != c.want[i] {
				t.Errorf("%s, %d pages: page %d numbered %d, want %d", c.model, c.pages, i, p.PageFlag, c.want[i])
			}
		}
	}
}

// TestCutOptionsSetModeBits checks each cut option's bits in various mode
// (auto cut 40h, mirror 80h), "cut each * labels", and advanced mode (half
// cut 04h, no chain printing 08h).
func TestCutOptionsSetModeBits(t *testing.T) {
	cases := []struct {
		name, model       string
		opts              JobOptions
		various, advanced byte
		cutEvery          byte
	}{
		{"cut each label", "PT-P750W", JobOptions{Cut: CutEach}, 0x40, 0x08, 1},
		{"cut at the end", "PT-P750W", JobOptions{Cut: CutEnd}, 0x00, 0x08, 0},
		{"half cut", "PT-P750W", JobOptions{Cut: CutEach, HalfCut: true}, 0x40, 0x0C, 1},
		{"chain", "PT-P750W", JobOptions{Cut: CutEach, Chain: true}, 0x40, 0x00, 1},
		{"half cut, chained, at the end", "PT-P950NW", JobOptions{Cut: CutEnd, HalfCut: true, Chain: true}, 0x00, 0x04, 0},
		{"mirror", "PT-P950NW", JobOptions{Cut: CutEach, Mirror: true}, 0xC0, 0x08, 1},
		{"mirror, cut at the end", "PT-P700", JobOptions{Cut: CutEnd, Mirror: true}, 0x80, 0x08, 0},
		// Neither takes "cut each * labels": auto cut alone cuts each.
		{"PT-P700 cuts each", "PT-P700", JobOptions{Cut: CutEach}, 0x40, 0x08, 0},
		{"PT-P710BT cuts each", "PT-P710BT", JobOptions{Cut: CutEach}, 0x40, 0x08, 0},
	}
	for _, c := range cases {
		l := layout(t, c.model, "tze-12")
		c.opts.MarginDots = l.Model.MinMarginDots
		stream, err := EncodeJob(l, []*image.Gray{checkerPage(l.PrintableDots, 100), checkerPage(l.PrintableDots, 100)}, c.opts)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		pages, err := l.Model.DecodeJob(stream)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		for i, p := range pages {
			if p.Various != c.various || p.Advanced != c.advanced || p.CutEvery != c.cutEvery {
				t.Errorf("%s page %d: various %02X, advanced %02X, cut every %d; want %02X, %02X, %d",
					c.name, i, p.Various, p.Advanced, p.CutEvery, c.various, c.advanced, c.cutEvery)
			}
		}
	}
}

func TestOptionsAreChecked(t *testing.T) {
	cases := []struct {
		name, model string
		opts        JobOptions
	}{
		{"half cut on the PT-P700", "PT-P700", JobOptions{Cut: CutEach, HalfCut: true, MarginDots: 14}},
		{"half cut on the PT-P710BT", "PT-P710BT", JobOptions{Cut: CutEach, HalfCut: true, MarginDots: 14}},
		{"unknown cut", "PT-P750W", JobOptions{Cut: "sometimes", MarginDots: 14}},
		{"margin under 2 mm", "PT-P750W", JobOptions{Cut: CutEach, MarginDots: 7}},
		{"margin over 127 mm", "PT-P950NW", JobOptions{Cut: CutEach, MarginDots: 1801}},
	}
	for _, c := range cases {
		l := layout(t, c.model, "tze-12")
		if _, err := EncodeJob(l, []*image.Gray{checkerPage(l.PrintableDots, 100)}, c.opts); err == nil {
			t.Errorf("%s accepted", c.name)
		}
	}
	// The margin goes to the printer in dots: 5 mm at 360 DPI is 71.
	l := layout(t, "PT-P950NW", "tze-12")
	stream, err := EncodeJob(l, []*image.Gray{checkerPage(l.PrintableDots, 100)}, JobOptions{Cut: CutEach, MarginDots: l.Model.Dots(5)})
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := l.Model.DecodeJob(stream); p[0].MarginDots != 71 {
		t.Errorf("margin = %d dots", p[0].MarginDots)
	}
}

func TestEncodeJobRejectsMisshapenPages(t *testing.T) {
	l := layout(t, "PT-P750W", "tze-12")
	opts := defaultOptions(l.Model)
	if _, err := EncodeJob(l, nil, opts); err == nil {
		t.Error("empty job accepted")
	}
	if _, err := EncodeJob(l, []*image.Gray{checkerPage(69, 100)}, opts); err == nil {
		t.Error("wrong width accepted")
	}
	if _, err := EncodeJob(l, []*image.Gray{checkerPage(70, 30)}, opts); err == nil {
		t.Error("too short accepted")
	}
	if _, err := EncodeJob(l, []*image.Gray{checkerPage(70, 7087)}, opts); err == nil {
		t.Error("too long accepted")
	}
	tube := layout(t, "PT-P750W", "hse-11.7")
	if _, err := EncodeJob(tube, []*image.Gray{checkerPage(66, 3544)}, opts); err == nil {
		t.Error("tube over 500 mm accepted")
	}
}

func TestDecodeJobRejectsDamagedStreams(t *testing.T) {
	l := layout(t, "PT-P750W", "tze-12")
	stream, err := EncodeJob(l, []*image.Gray{checkerPage(70, 100)}, defaultOptions(l.Model))
	if err != nil {
		t.Fatal(err)
	}
	for name, damaged := range map[string][]byte{
		"truncated":      stream[:len(stream)-10],
		"no final print": stream[:len(stream)-1],
		"no invalidate":  stream[50:],
		"trailing bytes": append(append([]byte{}, stream...), 0x00),
	} {
		if _, err := l.Model.DecodeJob(damaged); err == nil {
			t.Errorf("%s stream accepted", name)
		}
	}
	// The PT-P700 spells raster lines "g", and refuses "G".
	if _, err := model(t, "PT-P700").DecodeJob(stream); err == nil {
		t.Error("the PT-P700 decoded a PT-P750W stream")
	}
}

func status(width, kind, err1, err2 byte) []byte {
	b := make([]byte, 32)
	b[0], b[1], b[2], b[3], b[4] = 0x80, 0x20, 'B', '0', 'h'
	b[8], b[9], b[10], b[11] = err1, err2, width, kind
	return b
}

func TestStatusChecksTheCassette(t *testing.T) {
	tze12, _ := LookupMedia("tze-12")
	tze35, _ := LookupMedia("tze-3.5")
	hs, _ := LookupMedia("hse-11.7")
	hs31, _ := LookupMedia("hse-11.2")
	cases := []struct {
		name  string
		reply []byte
		media Media
		ok    bool
	}{
		{"12 mm laminated tape", status(12, 0x01, 0, 0), tze12, true},
		{"12 mm non-laminated tape", status(12, 0x03, 0, 0), tze12, true},
		{"3.5 mm tape, reported as 4", status(4, 0x01, 0, 0), tze35, true},
		{"24 mm tape for a 12 mm label", status(24, 0x01, 0, 0), tze12, false},
		{"tube for a tape label", status(12, 0x11, 0, 0), tze12, false},
		{"2:1 tube", status(12, 0x11, 0, 0), hs, true},
		{"3:1 tube for a 2:1 label", status(12, 0x17, 0, 0), hs, false},
		{"3:1 tube, width unknown", status(11, 0x17, 0, 0), hs31, true},
		{"no tape", status(0, 0x00, 0x01, 0), tze12, false},
		{"cover open", status(12, 0x01, 0, 0x10), tze12, false},
		{"incompatible cassette", status(12, 0xFF, 0, 0), tze12, false},
	}
	for _, c := range cases {
		s, err := ParseStatus(c.reply)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if err := s.Check(c.media); (err == nil) != c.ok || (err != nil && !errors.Is(err, ErrStatus)) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	if _, err := ParseStatus(make([]byte, 32)); err == nil {
		t.Error("parsed a reply without the print head mark")
	}
	if _, err := ParseStatus(status(12, 1, 0, 0)[:20]); err == nil {
		t.Error("parsed a short reply")
	}

	// The fit check can take the cassette loaded from a status.
	s, _ := ParseStatus(status(18, 0x01, 0, 0))
	if m, ok := s.LoadedMedia(model(t, "PT-P750W")); !ok || m.ID != "tze-18" {
		t.Errorf("loaded %+v, %v", m, ok)
	}
	s, _ = ParseStatus(status(36, 0x01, 0, 0))
	if _, ok := s.LoadedMedia(model(t, "PT-P750W")); ok {
		t.Error("the PT-P750W found 36 mm tape loaded")
	}
}
