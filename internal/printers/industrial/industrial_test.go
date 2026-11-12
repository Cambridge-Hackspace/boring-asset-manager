package industrial

import (
	"bytes"
	"flag"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/zebra"
)

var update = flag.Bool("update", false, "rewrite testdata/*.bin")

func TestModels(t *testing.T) {
	for _, lang := range []Language{DPL, SBPL, EZPL} {
		if len(Models(lang)) < 5 {
			t.Errorf("%s has %d models", lang, len(Models(lang)))
		}
		for _, name := range Models(lang) {
			m, ok := LookupModel(lang, name)
			if !ok || (m.DPI != 203 && m.DPI != 300 && m.DPI != 600) || m.HeadDots < 800 {
				t.Errorf("%s: %+v", name, m)
			}
		}
	}
	if _, ok := LookupModel(SBPL, "Godex G500"); ok {
		t.Error("an EZPL model is found as SBPL")
	}
	// SATO's 305 and 609 DPI heads are 12 and 24 dots per mm, as others'
	// 300 and 600.
	cl, _ := LookupModel(SBPL, "SATO CL4NX 609 DPI")
	if cl.DPI != 600 || cl.HeadDots != 312*8 || cl.ReachDots() != 2496 {
		t.Errorf("CL4NX 609 DPI = %+v", cl)
	}
	// A DPL image row holds 255 bytes, so a 600 DPI head is used to 2,040
	// dots, and the rest only where a roll is wider.
	ic, _ := LookupModel(DPL, "Datamax I-Class 600 DPI")
	if ic.ReachDots() != 2040 || ic.Suits(zebra.Media{Name: "4 in", WidthMM: 101.6}) != nil {
		t.Errorf("I-Class 600 reaches %d", ic.ReachDots())
	}
	if w := Prepare(ic, image.NewGray(image.Rect(0, 0, 2400, 10)), Placement{}).Rect.Dx(); w != 2040 {
		t.Errorf("a 600 DPI DPL page is %d dots wide", w)
	}
	g500, _ := LookupModel(EZPL, "Godex G500")
	for _, c := range []struct {
		width float64
		fits  bool
	}{{101.6, true}, {110, true}, {115, false}} {
		if err := g500.Suits(zebra.Media{Name: "test", WidthMM: c.width}); (err == nil) != c.fits {
			t.Errorf("G500 with %.1f mm labels: %v", c.width, err)
		}
	}
}

// tiny is a 10 × 3 page: a dot at the left of the top row, at the right
// of the middle one, and none on the bottom.
func tiny() *image.Gray {
	page := blank(10, 3)
	page.Pix[0] = 0
	page.Pix[page.Stride+9] = 0
	return page
}

func TestRowsAreMostSignificantBitFirst(t *testing.T) {
	// A set bit is a dot, its first bit the leftmost; a row is padded with
	// clear bits to whole bytes, and rows past the page are clear.
	got := rows(tiny(), 0, 2, 4)
	want := [][]byte{{0x80, 0x00}, {0x00, 0x40}, {0x00, 0x00}, {0x00, 0x00}}
	if !slices.EqualFunc(got, want, bytes.Equal) {
		t.Errorf("rows = %X, want %X", got, want)
	}
	// Offset by 3 dots across a wider row.
	if got := rows(tiny(), 3, 3, 1); !bytes.Equal(got[0], []byte{0x10, 0x00, 0x00}) {
		t.Errorf("offset row = %X", got[0])
	}
}

func TestDPLGraphicEncoding(t *testing.T) {
	// Continuous 16-dot (2 mm) media at 203 DPI: the label is the page,
	// centered in two bytes: three dots in.
	mo, _ := LookupModel(DPL, "Generic DPL 203 DPI")
	media := zebra.Media{Name: "test", WidthMM: 2}
	stream, err := EncodeDPL(mo, media, []*image.Gray{tiny()}, DPLOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// Datamax's 7-bit records, 80 then the byte count and the row in
	// hexadecimal, a set bit printing a dot; rows from the bottom up, a
	// repeat record for none, and FFFF to end.
	want := "\x02n\r\x02c0001\r" +
		"\x02IDAFBAM\r" +
		"80020000\r" + "80020008\r" + "80021000\r" + "FFFF\r" +
		"\x02L\rD11\r1Y1100000000000BAM\rQ0001\rE\r"
	if string(stream) != want {
		t.Errorf("stream\n%q, want\n%q", stream, want)
	}

	// A die-cut label taller than the page: 21 white rows below it, sent
	// first with the page's own white bottom row, as one record and 21
	// repeats.
	tall := zebra.Media{Name: "test", WidthMM: 2, LengthMM: 3, GapMM: 3}
	heat, speed := 12, 6
	stream, err = EncodeDPL(mo, tall, []*image.Gray{tiny()}, DPLOptions{Heat: &heat, Speed: &speed, Sensor: "mark", Module: "Y"})
	if err != nil {
		t.Fatal(err)
	}
	want = "\x02c0000\r\x02r\r" +
		"\x02IYAFBAM\r" +
		"80020000\r" + "0000FF15\r" + "80020008\r" + "80021000\r" + "FFFF\r" +
		"\x02L\rD11\rH12\rPK\r1Y1100000000000BAM\rQ0001\rE\r"
	if string(stream) != want {
		t.Errorf("stream\n%q, want\n%q", stream, want)
	}
}

func TestDPLSpeedLetters(t *testing.T) {
	// The Class Series Programmer's Manual: A is 1 in/s, then half an inch
	// per letter: C 2, G 4, W 12.
	for ips, want := range map[int]byte{1: 'A', 2: 'C', 4: 'G', 6: 'K', 12: 'W'} {
		if got := dplSpeed(ips); got != want {
			t.Errorf("%d in/s is %c, want %c", ips, got, want)
		}
	}
}

func TestSBPLGraphicEncoding(t *testing.T) {
	mo, _ := LookupModel(SBPL, "Generic SBPL 203 DPI")
	media := zebra.Media{Name: "test", WidthMM: 2}
	stream, err := EncodeSBPL(mo, media, []*image.Gray{tiny()}, SBPLOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// GB: bytes across, then blocks of 8 rows down, the rows from the top
	// in binary, a set bit printing a dot, filled out to the block with
	// white. Positions count from 1, and the page is centered: 3 dots in.
	want := "\x1bA\x1bA10003" + "0016" +
		"\x1bV1\x1bH4\x1bGB002001" + "\x80\x00" + "\x00\x40" + strings.Repeat("\x00", 12) +
		"\x1bQ1\x1bZ"
	if string(stream) != want {
		t.Errorf("stream\n%q, want\n%q", stream, want)
	}
	dark, speed := 7, 5
	stream, err = EncodeSBPL(mo, media, []*image.Gray{tiny()}, SBPLOptions{Darkness: &dark, Speed: &speed, Sensor: "none", Framed: true})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(stream, []byte("\x02\x1bA\x1bIG2\x1bCS5\x1b#F7\x1bZ\x1bA")) || !bytes.HasSuffix(stream, []byte("\x1bZ\x03")) {
		t.Errorf("stream %q", stream)
	}
}

func TestEZPLGraphicEncoding(t *testing.T) {
	mo, _ := LookupModel(EZPL, "Generic EZPL 203 DPI")
	media := zebra.Media{Name: "test", WidthMM: 25.4, LengthMM: 12.7, GapMM: 2}
	stream, err := EncodeEZPL(mo, media, []*image.Gray{tiny()}, EZPLOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// Q: across in dots, down, bytes across and rows, then the rows from
	// the top in binary, a set bit printing a dot, as in the pattern
	// example of Godex's manual. Centered across the 203-dot label.
	want := "^Q13,2\r\n^W25\r\n^P1\r\n^L\r\n" +
		"Q96,0,2,3\r\n" + "\x80\x00" + "\x00\x40" + "\x00\x00" + "\r\nE\r\n"
	if string(stream) != want {
		t.Errorf("stream\n%q, want\n%q", stream, want)
	}
	dark, speed := 12, 4
	mark := zebra.Media{Name: "test", WidthMM: 50, LengthMM: 30, GapMM: 4}
	stream, err = EncodeEZPL(mo, mark, []*image.Gray{tiny()}, EZPLOptions{Darkness: &dark, Speed: &speed, MediaType: "transfer", Sensor: "mark"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(stream, []byte("^Q30,4,0+\r\n^W50\r\n^H12\r\n^S4\r\n^AT\r\n^P1\r\n^L\r\n")) {
		t.Errorf("stream %q", stream)
	}
	stream, _ = EncodeEZPL(mo, zebra.Media{Name: "test", WidthMM: 50}, []*image.Gray{tiny()}, EZPLOptions{})
	if !bytes.HasPrefix(stream, []byte("^Q0,0,0\r\n")) {
		t.Errorf("continuous %q", stream[:12])
	}
}

// testPage is a label at a model's DPI with a frame and a mark, so its
// rows are not all alike.
func testPage(mo Model, m zebra.Media) *image.Gray {
	w, h := mo.Dots(m.WidthMM)-16, mo.Dots(25.4)
	page := blank(min(w, mo.ReachDots()), h)
	for y := range h {
		for x := range page.Rect.Dx() {
			if y < 4 || y >= h-4 || x < 4 || x >= page.Rect.Dx()-4 || (x/9+y/9)%7 == 0 {
				page.Pix[y*page.Stride+x] = 0
			}
		}
	}
	return page
}

// markedLabel draws a dark square near the top-left corner of a white
// label.
func markedLabel(w, h int) *image.Gray {
	img := blank(w, h)
	for y := 20; y < 30; y++ {
		for x := 20; x < 30; x++ {
			img.Pix[y*img.Stride+x] = 0
		}
	}
	return img
}

// samePrint checks printed holds page from (x, 0), and nothing else.
func samePrint(t *testing.T, name string, printed, page *image.Gray, x int) {
	t.Helper()
	for y := range printed.Rect.Dy() {
		for px := range printed.Rect.Dx() {
			want := false
			if px >= x && px < x+page.Rect.Dx() && y < page.Rect.Dy() {
				want = page.Pix[y*page.Stride+px-x] < 0x80
			}
			if (printed.Pix[y*printed.Stride+px] < 0x80) != want {
				t.Fatalf("%s: dot (%d, %d) differs", name, px, y)
			}
		}
	}
}

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name+".bin")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to write it)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs from %s", name, path)
	}
}

// slug names a model's golden file.
func slug(name string) string {
	return strings.ToLower(strings.NewReplacer(" ", "-", "'", "").Replace(name))
}

func TestDPLJobs(t *testing.T) {
	media, _ := zebra.LookupMedia("2x1")
	heat, speed := 18, 4
	for _, c := range []struct {
		model string
		media string
		opts  DPLOptions
	}{
		{"Datamax E-Class 203 DPI", "2x1", DPLOptions{}},
		{"Honeywell PM43 300 DPI", "2x1", DPLOptions{Heat: &heat, Speed: &speed, Sensor: "mark"}},
		{"Datamax I-Class 600 DPI", "continuous-2", DPLOptions{Module: "G"}},
	} {
		mo, _ := LookupModel(DPL, c.model)
		media, _ = zebra.LookupMedia(c.media)
		page := testPage(mo, media)
		other := markedLabel(page.Rect.Dx(), page.Rect.Dy())
		stream, err := EncodeDPL(mo, media, []*image.Gray{page, page, other}, c.opts)
		if err != nil {
			t.Fatal(err)
		}
		name := "dpl-" + slug(c.model)
		golden(t, name, stream)
		job, err := DecodeDPL(stream)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(job.Labels) != 2 || job.Labels[0].Copies != 2 || job.Labels[1].Copies != 1 {
			t.Fatalf("%s: %+v", name, job.Labels)
		}
		l := job.Labels[0]
		width, length := labelSize(mo, media, page)
		if l.Image.Rect.Dx() != (width+7)/8*8 || l.Image.Rect.Dy() != length {
			t.Errorf("%s: image %v for a %d × %d label", name, l.Image.Rect, width, length)
		}
		system := strings.Join(l.System, " ")
		if media.DieCut() && system != "c0000 "+map[string]string{"": "e", "mark": "r"}[c.opts.Sensor] {
			t.Errorf("%s: system %q", name, system)
		}
		if !media.DieCut() && system != fmt.Sprintf("n c%04d", int(float64(length)/float64(mo.DPI)*100+0.5)) {
			t.Errorf("%s: system %q", name, system)
		}
		if (c.opts.Heat != nil) != slices.Contains(l.Commands, "H18") || (c.opts.Speed != nil) != slices.Contains(l.Commands, "PG") {
			t.Errorf("%s: commands %q", name, l.Commands)
		}
		if !bytes.Contains(stream, []byte("\x02I"+c.opts.module()+"AFBAM\r")) {
			t.Errorf("%s: not loaded to module %s", name, c.opts.module())
		}
		samePrint(t, name, l.Image, page, (width-page.Rect.Dx())/2)
		samePrint(t, name+" second", job.Labels[1].Image, other, (width-other.Rect.Dx())/2)
	}
}

func TestSBPLJobs(t *testing.T) {
	dark, speed := 6, 4
	for _, c := range []struct {
		model string
		media string
		opts  SBPLOptions
	}{
		{"SATO WS408", "2x1", SBPLOptions{}},
		{"SATO CL4NX 305 DPI", "2x1", SBPLOptions{Darkness: &dark, Speed: &speed, Sensor: "gap"}},
		{"SATO CL4NX 609 DPI", "continuous-2", SBPLOptions{Sensor: "none", Framed: true}},
	} {
		mo, _ := LookupModel(SBPL, c.model)
		media, _ := zebra.LookupMedia(c.media)
		page := testPage(mo, media)
		other := markedLabel(page.Rect.Dx(), page.Rect.Dy())
		stream, err := EncodeSBPL(mo, media, []*image.Gray{page, page, other}, c.opts)
		if err != nil {
			t.Fatal(err)
		}
		name := "sbpl-" + slug(c.model)
		golden(t, name, stream)
		job, err := DecodeSBPL(stream)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(job.Labels) != 2 || job.Labels[0].Copies != 2 || job.Labels[1].Copies != 1 || job.Framed != c.opts.Framed {
			t.Fatalf("%s: %+v", name, job)
		}
		l := job.Labels[0]
		width, length := labelSize(mo, media, page)
		if l.Width != width || l.Length != length {
			t.Errorf("%s: %d × %d, want %d × %d", name, l.Width, l.Length, width, length)
		}
		if c.opts.Sensor != "" && job.Setup[0] != fmt.Sprintf("IG%d", SBPLSensors[c.opts.Sensor]) {
			t.Errorf("%s: setup %q", name, job.Setup)
		}
		if c.opts.Darkness != nil && strings.Join(job.Setup, " ") != "IG1 CS4 #F6" {
			t.Errorf("%s: setup %q", name, job.Setup)
		}
		samePrint(t, name, l.Image, page, (width-page.Rect.Dx())/2)
		samePrint(t, name+" second", job.Labels[1].Image, other, (width-other.Rect.Dx())/2)
	}
}

func TestALongSBPLLabelIsSentInBands(t *testing.T) {
	mo, _ := LookupModel(SBPL, "SATO CL4NX 305 DPI")
	media, _ := zebra.LookupMedia("continuous-4")
	page := testPage(mo, media)
	tall := blank(page.Rect.Dx(), 2001)
	for y := range 2001 {
		copy(tall.Pix[y*tall.Stride:], page.Pix[(y%page.Rect.Dy())*page.Stride:(y%page.Rect.Dy()+1)*page.Stride])
	}
	stream, err := EncodeSBPL(mo, media, []*image.Gray{tall}, SBPLOptions{})
	if err != nil {
		t.Fatal(err)
	}
	job, err := DecodeSBPL(stream)
	if err != nil {
		t.Fatal(err)
	}
	// 2,001 rows are 251 blocks: bands of 100, 100 and 51.
	l := job.Labels[0]
	if l.Graphics != 3 || l.Length != 2001 || !bytes.Contains(stream, []byte("\x1bV1601\x1bH")) {
		t.Errorf("%d graphics, %d rows", l.Graphics, l.Length)
	}
	samePrint(t, "bands", l.Image, tall, (l.Width-tall.Rect.Dx())/2)

	// Longer than A1 takes in four digits, and longer than a 609 DPI head
	// prints.
	long := blank(page.Rect.Dx(), 12000)
	if stream, err := EncodeSBPL(mo, media, []*image.Gray{long}, SBPLOptions{}); err != nil || !bytes.HasPrefix(stream, []byte("\x1bA\x1bA1V12000H")) {
		t.Errorf("a 12,000-dot label: %v", err)
	}
	fine, _ := LookupModel(SBPL, "SATO CL4NX 609 DPI")
	if _, err := EncodeSBPL(fine, media, []*image.Gray{long}, SBPLOptions{}); err == nil {
		t.Error("a 12,000-dot label was taken at 609 DPI")
	}
}

func TestEZPLJobs(t *testing.T) {
	dark, speed := 14, 5
	for _, c := range []struct {
		model string
		media string
		opts  EZPLOptions
	}{
		{"Godex G500", "2x1", EZPLOptions{}},
		{"Godex RT230", "2x1", EZPLOptions{Darkness: &dark, Speed: &speed, MediaType: "direct"}},
		{"Godex EZ-1100Plus", "continuous-2", EZPLOptions{MediaType: "transfer"}},
	} {
		mo, _ := LookupModel(EZPL, c.model)
		media, _ := zebra.LookupMedia(c.media)
		page := testPage(mo, media)
		other := markedLabel(page.Rect.Dx(), page.Rect.Dy())
		stream, err := EncodeEZPL(mo, media, []*image.Gray{page, page, other}, c.opts)
		if err != nil {
			t.Fatal(err)
		}
		name := "ezpl-" + slug(c.model)
		golden(t, name, stream)
		job, err := DecodeEZPL(stream)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(job.Labels) != 2 || job.Labels[0].Copies != 2 || job.Labels[1].Copies != 1 {
			t.Fatalf("%s: %+v", name, job.Labels)
		}
		l := job.Labels[0]
		setup := strings.Join(l.Setup, " ")
		if !strings.HasPrefix(setup, map[bool]string{true: "^Q25,3 ^W51", false: "^Q25,0,0 ^W51"}[media.DieCut()]) {
			t.Errorf("%s: setup %q", name, setup)
		}
		if c.opts.MediaType != "" && !strings.Contains(setup, EZPLMediaTypes[c.opts.MediaType]) || c.opts.Speed != nil && !strings.Contains(setup, "^H14 ^S5") {
			t.Errorf("%s: setup %q", name, setup)
		}
		width, _ := labelSize(mo, media, page)
		samePrint(t, name, l.Image, page, (width-page.Rect.Dx())/2)
		samePrint(t, name+" second", job.Labels[1].Image, other, (width-other.Rect.Dx())/2)
	}
}

func TestManyCopiesAreSentInBatches(t *testing.T) {
	mo, _ := LookupModel(DPL, "Generic DPL 203 DPI")
	media, _ := zebra.LookupMedia("1x1")
	page := tiny()
	pages := make([]*image.Gray, 10000)
	for i := range pages {
		pages[i] = page
	}
	// Q9999 prints without end on most DPL printers.
	stream, err := EncodeDPL(mo, media, pages, DPLOptions{})
	if err != nil {
		t.Fatal(err)
	}
	job, err := DecodeDPL(stream)
	if err != nil || len(job.Labels) != 2 || job.Labels[0].Copies != 9998 || job.Labels[1].Copies != 2 {
		t.Errorf("%v: %d labels", err, len(job.Labels))
	}
}

func TestBadJobsAreRefused(t *testing.T) {
	media, _ := zebra.LookupMedia("4x6")
	page := image.NewGray(image.Rect(0, 0, 10, 10))
	n := func(v int) *int { return &v }
	dpl, _ := LookupModel(DPL, "Generic DPL 203 DPI")
	for _, o := range []DPLOptions{{Heat: n(31)}, {Speed: n(0)}, {Speed: n(13)}, {Sensor: "none"}, {Module: "d"}, {Module: "DD"}} {
		if _, err := EncodeDPL(dpl, media, []*image.Gray{page}, o); err == nil {
			t.Errorf("DPL %+v was taken", o)
		}
	}
	sbpl, _ := LookupModel(SBPL, "Generic SBPL 203 DPI")
	for _, o := range []SBPLOptions{{Darkness: n(0)}, {Darkness: n(11)}, {Speed: n(1)}, {Speed: n(11)}, {Sensor: "continuous"}} {
		if _, err := EncodeSBPL(sbpl, media, []*image.Gray{page}, o); err == nil {
			t.Errorf("SBPL %+v was taken", o)
		}
	}
	ezpl, _ := LookupModel(EZPL, "Generic EZPL 203 DPI")
	for _, o := range []EZPLOptions{{Darkness: n(20)}, {Speed: n(1)}, {Speed: n(8)}, {MediaType: "ribbon"}, {Sensor: "none"}} {
		if _, err := EncodeEZPL(ezpl, media, []*image.Gray{page}, o); err == nil {
			t.Errorf("EZPL %+v was taken", o)
		}
	}
	if _, err := EncodeDPL(dpl, media, nil, DPLOptions{}); err == nil {
		t.Error("a job of no pages was taken")
	}
	if _, err := EncodeSBPL(sbpl, media, []*image.Gray{image.NewGray(image.Rect(0, 0, 900, 10))}, SBPLOptions{}); err == nil {
		t.Error("a page wider than the head was taken")
	}
}
