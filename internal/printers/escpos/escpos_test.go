package escpos

import (
	"bytes"
	"flag"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/*.bin")

func TestModelsAndMedia(t *testing.T) {
	if got := strings.Join(Models(false), ","); got != "Generic ESC/POS 58 mm,Generic ESC/POS 80 mm" {
		t.Errorf("generic models %s", got)
	}
	if got := strings.Join(Models(true), ","); got != "D30,D35,M110,M120,M200,M220,M221,Q30,T02" {
		t.Errorf("Phomemo models %s", got)
	}
	for _, c := range []struct {
		model string
		dots  int
		mm    float64
	}{
		{"Generic ESC/POS 58 mm", 384, 48}, {"Generic ESC/POS 80 mm", 576, 72},
		{"M110", 384, 48}, {"M200", 608, 76}, {"M220", 576, 72}, {"D30", 96, 12}, {"T02", 384, 48},
	} {
		m, _ := LookupModel(c.model)
		if m.DPI != 203 || m.HeadDots != c.dots || m.HeadMM(m.HeadDots) < c.mm-0.1 || m.HeadMM(m.HeadDots) > c.mm+0.1 {
			t.Errorf("%s = %+v, a %.1f mm head", c.model, m, m.HeadMM(m.HeadDots))
		}
	}
	for _, id := range append(MediaIDs(false), MediaIDs(true)...) {
		if m, ok := LookupMedia(id); !ok || m.ID != id || m.Family == "" {
			t.Errorf("%s = %+v", id, m)
		}
	}
	takes := func(model, media string) error {
		mo, _ := LookupModel(model)
		m, _ := LookupMedia(media)
		return mo.Takes(m)
	}
	for _, c := range [][2]string{
		{"Generic ESC/POS 58 mm", "58mm"}, {"Generic ESC/POS 80 mm", "80mm"}, {"M110", "50x30"}, {"M110", "50-continuous"},
		{"M220", "70x80"}, {"D30", "12x40"}, {"Q30", "15x30"}, {"T02", "53-continuous"},
	} {
		if err := takes(c[0], c[1]); err != nil {
			t.Errorf("%s refuses %s: %v", c[0], c[1], err)
		}
	}
	for _, c := range [][2]string{
		{"Generic ESC/POS 58 mm", "80mm"}, {"M110", "70x80"}, {"M220", "80mm"}, {"D30", "50x30"}, {"T02", "50x30"},
	} {
		if takes(c[0], c[1]) == nil {
			t.Errorf("%s takes %s", c[0], c[1])
		}
	}
	// Custom stock: die-cut only on the D series, continuous only on the
	// T02, and no wider than the model takes.
	d30, _ := LookupModel("D30")
	t02, _ := LookupModel("T02")
	if d30.Takes(Media{Name: "tape", WidthMM: 12}) == nil || d30.Takes(Media{Name: "wide", WidthMM: 20, LengthMM: 30}) == nil ||
		t02.Takes(Media{Name: "labels", WidthMM: 50, LengthMM: 30}) == nil || t02.Takes(Media{Name: "paper", WidthMM: 50}) != nil {
		t.Error("custom stock checks")
	}
}

// markedLabel draws a dark square near the top-left corner of a white label.
func markedLabel(w, h int) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 0xFF
	}
	for y := 2; y < min(h, 8); y++ {
		for x := 2; x < min(w, 8); x++ {
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

func TestPrepare(t *testing.T) {
	m110, _ := LookupModel("M110")
	// A 40 mm label (320 dots) is centered on the 384-dot head.
	page := Prepare(m110, 384, markedLabel(320, 240), Placement{})
	if page.Bounds() != image.Rect(0, 0, 384, 240) || darkBounds(page) != image.Rect(34, 2, 40, 8) {
		t.Errorf("page %v, mark at %v", page.Bounds(), darkBounds(page))
	}
	// The M220's roll sits against the end of its head.
	m220, _ := LookupModel("M220")
	if page := Prepare(m220, 576, markedLabel(320, 240), Placement{}); darkBounds(page) != image.Rect(258, 2, 264, 8) {
		t.Errorf("M220 mark at %v", darkBounds(page))
	}
	// Counterclockwise: a D30's landscape label, 40 × 12 mm, has its
	// top-left corner at the bottom-left.
	d30, _ := LookupModel("D30")
	page = Prepare(d30, 96, markedLabel(320, 96), Placement{Rotate: true})
	if page.Bounds() != image.Rect(0, 0, 96, 320) || darkBounds(page) != image.Rect(2, 320-8, 8, 320-2) {
		t.Errorf("rotated page %v, mark at %v", page.Bounds(), darkBounds(page))
	}
	// The other way up as well: at the top-right.
	page = Prepare(d30, 96, markedLabel(320, 96), Placement{Rotate: true, Rotate180: true})
	if darkBounds(page) != image.Rect(96-8, 2, 96-2, 8) {
		t.Errorf("rotated twice, mark at %v", darkBounds(page))
	}
	// Wider than the head: the middle is kept.
	if page := Prepare(m110, 384, markedLabel(400, 10), Placement{}); page.Bounds().Dx() != 384 || !darkBounds(page).Empty() {
		t.Errorf("page %v, mark at %v", page.Bounds(), darkBounds(page))
	}
}

// page is a prepared page of w × h dots: white but for the dots listed.
func page(w, h int, dark ...[2]int) *image.Gray {
	p := image.NewGray(image.Rect(0, 0, w, h))
	for i := range p.Pix {
		p.Pix[i] = 0xFF
	}
	for _, d := range dark {
		p.Pix[d[1]*p.Stride+d[0]] = 0
	}
	return p
}

func TestEncodeJobFollowsReferences(t *testing.T) {
	// 10-dot pages, 2 bytes a line: the first dot of the first line and
	// the last of the second.
	pg := page(10, 2, [2]int{0, 0}, [2]int{9, 1})
	rows := "\x80\x00\x00\x40"
	gsv0 := "\x1d\x76\x30\x00\x02\x00"
	for _, c := range []struct {
		name, model, media string
		opts               JobOptions
		want               string
	}{{
		// Epson: ESC @, then GS v 0 m xL xH yL yH and the data, a set bit
		// printing; 8 mm of feed is 64 blank lines; GS V 66 0 feeds to the
		// cutter and cuts partly.
		"generic", "Generic ESC/POS 58 mm", "58mm", JobOptions{Cut: CutPartial, FeedMM: 8},
		"\x1b\x40" + gsv0 + "\x42\x00" + rows + strings.Repeat("\x00", 128) + "\x1d\x56\x42\x00",
	}, {
		"generic full cut, no feed", "Generic ESC/POS 80 mm", "80mm", JobOptions{Cut: CutFull},
		"\x1b\x40" + gsv0 + "\x02\x00" + rows + "\x1d\x56\x41\x00",
	}, {
		"generic without a cutter", "Generic ESC/POS 80 mm", "80mm", JobOptions{Cut: CutNone},
		"\x1b\x40" + gsv0 + "\x02\x00" + rows,
	}, {
		// Phomemo's M series: ESC N 13 speed, ESC N 4 density, US DC1
		// media type (0x0A, gaps), the raster, and 1F F0 05 00 1F F0 03 00.
		"M110", "M110", "50x30", JobOptions{Density: 12, Speed: 3},
		"\x1b\x4e\x0d\x03\x1b\x4e\x04\x0c\x1f\x11\x0a" + gsv0 + "\x02\x00" + rows + "\x1f\xf0\x05\x00\x1f\xf0\x03\x00",
	}, {
		"M110 continuous", "M110", "50-continuous", JobOptions{Density: 10, Speed: 5},
		"\x1b\x4e\x0d\x05\x1b\x4e\x04\x0a\x1f\x11\x0b" + gsv0 + "\x02\x00" + rows + "\x1f\xf0\x05\x00\x1f\xf0\x03\x00",
	}, {
		// The D series: 1F 11 24 00, ESC @, the raster, ESC d 0.
		"D30", "D30", "12x40", JobOptions{},
		"\x1f\x11\x24\x00\x1b\x40" + gsv0 + "\x02\x00" + rows + "\x1b\x64\x00",
	}, {
		// Paper: ESC @, ESC a 1, 1F 11 02 04, the raster, ESC d 2 twice.
		"T02", "T02", "53-continuous", JobOptions{},
		"\x1b\x40\x1b\x61\x01\x1f\x11\x02\x04" + gsv0 + "\x02\x00" + rows + "\x1b\x64\x02\x1b\x64\x02",
	}} {
		t.Run(c.name, func(t *testing.T) {
			mo, _ := LookupModel(c.model)
			mo.HeadDots = 10 // as the page is prepared
			m, _ := LookupMedia(c.media)
			got, err := EncodeJob(mo, m, []*image.Gray{pg}, c.opts)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != c.want {
				t.Errorf("got  %q\nwant %q", got, c.want)
			}
		})
	}
}

func TestEncodeJobBands(t *testing.T) {
	// A label of 300 lines goes to a plain ESC/POS printer as bands of 255
	// and 45, and to an M110 whole.
	generic, _ := LookupModel("Generic ESC/POS 58 mm")
	paper, _ := LookupMedia("58mm")
	got, err := EncodeJob(generic, paper, []*image.Gray{page(384, 300)}, JobOptions{Cut: CutNone})
	if err != nil {
		t.Fatal(err)
	}
	second := 2 + 8 + 48*255
	if len(got) != second+8+48*45 || !bytes.Equal(got[2:10], []byte{0x1d, 0x76, 0x30, 0, 48, 0, 255, 0}) ||
		!bytes.Equal(got[second:second+8], []byte{0x1d, 0x76, 0x30, 0, 48, 0, 45, 0}) {
		t.Errorf("bands: %d bytes", len(got))
	}
	m110, _ := LookupModel("M110")
	labels, _ := LookupMedia("50x30")
	got, _ = EncodeJob(m110, labels, []*image.Gray{page(384, 300)}, JobOptions{Density: 10, Speed: 5})
	if !bytes.Equal(got[11:19], []byte{0x1d, 0x76, 0x30, 0, 48, 0, 0x2c, 0x01}) {
		t.Errorf("M110 raster header % x", got[11:19])
	}
}

func TestEncodeJobGoldens(t *testing.T) {
	// A copy of each model's typical label, then a different one.
	for _, c := range []struct {
		name, model, media string
		opts               JobOptions
	}{
		{"generic-58", "Generic ESC/POS 58 mm", "58mm", JobOptions{Cut: CutPartial, FeedMM: 2}},
		{"generic-80", "Generic ESC/POS 80 mm", "80mm", JobOptions{Cut: CutFull}},
		{"m110-40x30", "M110", "40x30", JobOptions{Density: 10, Speed: 5}},
		{"m120-50-continuous", "M120", "50-continuous", JobOptions{Density: 8, Speed: 3}},
		{"m200-50x30", "M200", "50x30", JobOptions{Density: 10, Speed: 5}},
		{"m220-70x80", "M220", "70x80", JobOptions{Density: 15, Speed: 1}},
		{"m221-40x30", "M221", "40x30", JobOptions{Density: 10, Speed: 5}},
		{"d30-12x40", "D30", "12x40", JobOptions{}},
		{"d35-14x30", "D35", "14x30", JobOptions{}},
		{"q30-15x30", "Q30", "15x30", JobOptions{}},
		{"t02-53-continuous", "T02", "53-continuous", JobOptions{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			mo, _ := LookupModel(c.model)
			m, _ := LookupMedia(c.media)
			// The label as designed: landscape on the D series, turned to
			// run along the tape; 20 mm long on a continuous roll.
			w, h := mo.Dots(m.WidthMM), mo.Dots(m.LengthMM)
			if !m.DieCut() {
				h = mo.Dots(20)
			}
			rotate := mo.Family == FamilyPhomemoD
			if rotate {
				w, h = h, w
			}
			first, second := markedLabel(w, h), markedLabel(w, h)
			second.Pix[len(second.Pix)-1] = 0
			p1 := Prepare(mo, mo.HeadDots, first, Placement{Rotate: rotate})
			p2 := Prepare(mo, mo.HeadDots, second, Placement{Rotate: rotate})
			stream, err := EncodeJob(mo, m, []*image.Gray{p1, p1, p2}, c.opts)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join("testdata", c.name+".bin")
			if *update {
				if err := os.WriteFile(path, stream, 0o644); err != nil {
					t.Fatal(err)
				}
			} else if want, err := os.ReadFile(path); err != nil || !bytes.Equal(stream, want) {
				t.Errorf("stream differs from %s (%v)", path, err)
			}
			// Each label is a raster as wide as the head.
			header := []byte{0x1d, 0x76, 0x30, 0x00, byte(mo.HeadDots / 8), byte(mo.HeadDots / 8 >> 8)}
			if n := bytes.Count(stream, header); n < 3 {
				t.Errorf("%d raster images for 3 labels", n)
			}
		})
	}
}

func TestEncodeJobRejectsBadInput(t *testing.T) {
	m110, _ := LookupModel("M110")
	labels, _ := LookupMedia("40x30")
	ok := JobOptions{Density: 10, Speed: 5}
	if _, err := EncodeJob(m110, labels, nil, ok); err == nil {
		t.Error("empty job accepted")
	}
	if _, err := EncodeJob(m110, labels, []*image.Gray{page(320, 10)}, ok); err == nil {
		t.Error("page narrower than the head accepted")
	}
	if _, err := EncodeJob(m110, labels, []*image.Gray{page(384, 10), page(376, 10)}, ok); err == nil {
		t.Error("pages of two widths accepted")
	}
	for _, o := range []JobOptions{{Density: 16, Speed: 5}, {Density: 0, Speed: 5}, {Density: 10, Speed: 6}} {
		if _, err := EncodeJob(m110, labels, []*image.Gray{page(384, 10)}, o); err == nil {
			t.Errorf("%+v accepted", o)
		}
	}
	generic, _ := LookupModel("Generic ESC/POS 58 mm")
	paper, _ := LookupMedia("58mm")
	for _, o := range []JobOptions{{Cut: "half"}, {Cut: CutNone, FeedMM: 101}, {Cut: CutNone, FeedMM: -1}} {
		if _, err := EncodeJob(generic, paper, []*image.Gray{page(384, 10)}, o); err == nil {
			t.Errorf("%+v accepted", o)
		}
	}
}
