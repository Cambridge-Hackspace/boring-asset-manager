package dymolw

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/*.bin")

// The expectations below are worked out from DYMO's LabelWriter 450 Series
// Technical Reference Manual.

func TestCompressionMatchesManualExamples(t *testing.T) {
	// Appendix A: the top bit is the color, the rest the run length less
	// one; 00h is 1 white dot, 80h 1 printed dot, 0Fh 16 white dots and FFh
	// 128 printed dots.
	for _, c := range []struct {
		line, rle []byte
	}{
		{[]byte{0x00, 0x00}, []byte{0x0F}},
		{bytes.Repeat([]byte{0xFF}, 16), []byte{0xFF}},
		{bytes.Repeat([]byte{0xFF}, 17), []byte{0xFF, 0x87}},
		{[]byte{0x80}, []byte{0x80, 0x06}},
		{[]byte{0x7F}, []byte{0x00, 0x86}},
	} {
		if got := compressLine(c.line); !bytes.Equal(got, c.rle) {
			t.Errorf("compressLine(% X) = % X, want % X", c.line, got, c.rle)
		}
	}
}

func TestLineWriterFollowsManual(t *testing.T) {
	rows := [][]byte{
		{0x00, 0xFF, 0xFF, 0x00}, // dot tab 1, 2 bytes, 16 printed dots
		{0x00, 0xFF, 0xFF, 0x00}, // same settings: nothing to resend
	}
	for i := 0; i < 300; i++ {
		rows = append(rows, make([]byte, 4)) // skipped, at most 255 at a time
	}
	rows = append(rows,
		[]byte{0x00, 0xAA, 0x55, 0x00}, // compressing would lengthen it
		[]byte{0x0F, 0x00, 0x00, 0x00}, // new dot tab and bytes per line
		make([]byte, 4), make([]byte, 4),
	)
	want := []byte{
		esc, 'B', 1, esc, 'D', 2, etb, 0x8F,
		etb, 0x8F,
		esc, 'f', 1, 255, esc, 'f', 1, 45,
		syn, 0xAA, 0x55,
		esc, 'B', 0, esc, 'D', 1, syn, 0x0F,
	}
	for _, keep := range []bool{false, true} {
		var buf bytes.Buffer
		lw := lineWriter{buf: &buf, dotTab: -1, bytesPerLine: -1}
		lw.page(rows, keep)
		w := want
		if keep {
			w = concat(want, []byte{esc, 'f', 1, 2})
		}
		if !bytes.Equal(buf.Bytes(), w) {
			t.Errorf("keep trailing blanks %v:\ngot  % X\nwant % X", keep, buf.Bytes(), w)
		}
	}
}

func TestV1JobFrameFollowsManual(t *testing.T) {
	// Resynchronize with one ESC more than the longest line, choose text
	// mode and density (and the roll on twin models), set the label length
	// a little past the label, then a short form feed between labels and a
	// form feed after the last.
	m := Media{ID: "x", Name: "x", WidthMM: 1, LengthMM: 1, WidthDots: 8, LengthDots: 0x0800}
	blankPage := image.NewGray(image.Rect(0, 0, 8, 0x0800))
	for i := range blankPage.Pix {
		blankPage.Pix[i] = 0xFF
	}
	length := []byte{esc, 'L', 0x08, 0x24} // 0x0800 + 36 lines
	for _, c := range []struct {
		model string
		pages int
		want  []byte
	}{
		{"LabelWriter 400", 2, concat(bytes.Repeat([]byte{esc}, 85), []byte{esc, 'h', esc, 'e'}, length, []byte{esc, 'G', esc, 'E'})},
		{"LabelWriter Twin Turbo", 1, concat(bytes.Repeat([]byte{esc}, 85), []byte{esc, 'h', esc, 'e', esc, 'q', '0'}, length, []byte{esc, 'E'})},
		{"LabelWriter 4XL", 1, concat(bytes.Repeat([]byte{esc}, 157), []byte{esc, 'h', esc, 'e'}, length, []byte{esc, 'E'})},
	} {
		mo, _ := LookupModel(c.model)
		pages := make([]*image.Gray, c.pages)
		for i := range pages {
			pages[i] = blankPage
		}
		got, err := EncodeJob(mo, m, pages, JobOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, c.want) {
			t.Errorf("%s:\ngot  % X\nwant % X", c.model, got, c.want)
		}
	}
}

func concat(parts ...[]byte) []byte { return bytes.Join(parts, nil) }

func TestCompressLineRoundTrip(t *testing.T) {
	for _, line := range [][]byte{{0x00}, {0xFF}, {0x0F, 0xF0}, bytes.Repeat([]byte{0xFF}, 40), {0x80, 0x01, 0x55}} {
		rle := compressLine(line)
		var bits []bool
		for _, c := range rle {
			for k := 0; k <= int(c&0x7F); k++ {
				bits = append(bits, c&0x80 != 0)
			}
		}
		if len(bits) != len(line)*8 {
			t.Fatalf("% X: %d dots back", line, len(bits))
		}
		for i, b := range bits {
			if b != (line[i/8]&(0x80>>uint(i%8)) != 0) {
				t.Fatalf("% X: dot %d differs", line, i)
			}
		}
	}
}

func TestModelsAndMedia(t *testing.T) {
	lw450, _ := LookupModel("LabelWriter 450")
	xl, _ := LookupModel("LabelWriter 5XL")
	big, _ := LookupMedia("1744907")
	shipping, _ := LookupMedia("30256")
	if lw450.Suits(big) == nil {
		t.Error("4-inch labels accepted on a 56 mm head")
	}
	if xl.Suits(big) != nil || lw450.Suits(shipping) != nil {
		t.Error("a roll the printer takes was refused")
	}
	if address, _ := LookupMedia("30252"); address.WidthDots != 331 || address.LengthDots != 1051 {
		t.Errorf("30252 is %d × %d dots, want 331 × 1051", address.WidthDots, address.LengthDots)
	}
	for _, name := range Models() {
		if mo, _ := LookupModel(name); mo.Protocol != ProtocolV1 && mo.Protocol != ProtocolV2 {
			t.Errorf("%s has no protocol", name)
		}
	}
}

func TestFit(t *testing.T) {
	address, _ := LookupMedia("30252")
	continuous, _ := LookupMedia("30270")
	cases := []struct {
		name          string
		media         Media
		width, height float64
		dpi           int
		rotate, fits  bool
	}{
		{"portrait", address, 28, 89, 300, false, true},
		{"landscape rotates", address, 89, 28, 300, true, true},
		{"within tolerance", address, 28.4, 88.6, 300, false, true},
		{"wrong length", address, 28, 60, 300, false, false},
		{"continuous", continuous, 57, 40, 300, false, true},
		{"continuous landscape", continuous, 100, 57, 300, true, true},
		{"continuous too short", continuous, 57, 5, 300, false, false},
		{"wrong width", continuous, 62, 40, 300, false, false},
		{"wrong dpi", address, 28, 89, 203, false, false},
	}
	for _, c := range cases {
		p, err := c.media.Fit(c.width, c.height, c.dpi)
		if c.fits != (err == nil) {
			t.Errorf("%s: err = %v, want fits=%v", c.name, err, c.fits)
			continue
		}
		if err != nil && !errors.Is(err, ErrDoesNotFit) {
			t.Errorf("%s: error does not wrap ErrDoesNotFit: %v", c.name, err)
		}
		if err == nil && p.Rotate != c.rotate {
			t.Errorf("%s: rotate = %v, want %v", c.name, p.Rotate, c.rotate)
		}
	}
}

// markedLabel draws a dark square near the top-left corner of a white label
// so orientation can be checked.
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

func TestPrepare(t *testing.T) {
	lw450, _ := LookupModel("LabelWriter 450")
	lw550, _ := LookupModel("LabelWriter 550")
	address, _ := LookupMedia("30252")

	// A template 0.2 mm over 28 × 89 mm renders as 333 × 1053 dots; the
	// 331 × 1051 label takes its center, a (1, 1) offset.
	page := address.Prepare(lw450, markedLabel(333, 1053), Placement{})
	if b := page.Bounds(); b.Dx() != 331 || b.Dy() != 1051 {
		t.Fatalf("page is %v, want 331×1051", b)
	}
	if got, want := darkBounds(page), image.Rect(39, 39, 49, 49); got != want {
		t.Errorf("mark at %v, want %v", got, want)
	}

	// Landscape turns counterclockwise: the top-left corner ends at the
	// bottom-left.
	page = address.Prepare(lw450, markedLabel(1053, 333), Placement{Rotate: true})
	if got, want := darkBounds(page), image.Rect(39, 1053-50-1, 49, 1053-40-1); got != want {
		t.Errorf("rotated mark at %v, want %v", got, want)
	}

	// A 59 mm roll is wider than the 56 mm head: version 1 models get what
	// the head takes, version 2 models the whole label.
	shipping, _ := LookupMedia("30256")
	if w := shipping.Prepare(lw450, markedLabel(697, 1205), Placement{}).Bounds().Dx(); w != 672 {
		t.Errorf("450 page is %d dots wide, want the head's 672", w)
	}
	if w := shipping.Prepare(lw550, markedLabel(697, 1205), Placement{}).Bounds().Dx(); w != 697 {
		t.Errorf("550 page is %d dots wide, want the label's 697", w)
	}
}

func checkerPage(w, h int) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if (x/16+y/16)%2 != 0 {
				img.Pix[y*img.Stride+x] = 0xFF
			}
		}
	}
	// Blank bands at the top, middle and bottom exercise line skipping and
	// the trailing-blank rules.
	for y := 0; y < h; y++ {
		if y < 30 || (y >= h/2 && y < h/2+300) || y >= h-20 {
			for x := 0; x < w; x++ {
				img.Pix[y*img.Stride+x] = 0xFF
			}
		}
	}
	return img
}

// TestEncodeJobGoldens pins the byte stream for representative printers and
// checks that decoding it gives back the pages that went in.
func TestEncodeJobGoldens(t *testing.T) {
	cases := []struct {
		model, media string
		opts         JobOptions
	}{
		{"LabelWriter 450", "30252", JobOptions{}},
		{"LabelWriter 450 Twin Turbo", "30334", JobOptions{Density: DensityDark, Roll: RollRight}},
		{"LabelWriter 400", "30270", JobOptions{Density: DensityLight}},
		{"LabelWriter 4XL", "1744907", JobOptions{}},
		{"LabelWriter 550", "30252", JobOptions{JobID: 0x01020304}},
		{"LabelWriter 5XL", "30334", JobOptions{Density: DensityMedium, JobID: 7}},
		{"LabelWriter 550 Turbo", "30270", JobOptions{JobID: 9}},
	}
	for _, c := range cases {
		mo, _ := LookupModel(c.model)
		m, _ := LookupMedia(c.media)
		name := fmt.Sprintf("%s-%s", strings.ReplaceAll(strings.ToLower(c.model), " ", "-"), c.media)
		t.Run(name, func(t *testing.T) {
			lines := m.LengthDots
			if !m.DieCut() {
				lines = 700
			}
			w := m.PageDots(mo)
			pages := []*image.Gray{checkerPage(w, lines), markedLabel(w, lines)}
			stream, err := EncodeJob(mo, m, pages, c.opts)
			if err != nil {
				t.Fatal(err)
			}

			path := filepath.Join("testdata", name+".bin")
			if *update {
				if err := os.WriteFile(path, stream, 0o644); err != nil {
					t.Fatal(err)
				}
			} else if want, err := os.ReadFile(path); err != nil || !bytes.Equal(stream, want) {
				t.Errorf("stream differs from %s (%v)", path, err)
			}

			job, err := DecodeJob(mo.Protocol, stream, w)
			if err != nil {
				t.Fatalf("decoding: %v", err)
			}
			wantDensity := densityCommands[c.opts.Density]
			if c.opts.Density == "" {
				wantDensity = 'e'
			}
			if job.Density != wantDensity || job.JobID != c.opts.JobID || len(job.Pages) != 2 {
				t.Fatalf("decoded density %q, job %d, %d pages", job.Density, job.JobID, len(job.Pages))
			}
			if mo.TwinRoll != (job.Roll != 0) || (mo.TwinRoll && job.Roll != rollCommands[c.opts.Roll]) {
				t.Errorf("roll command %q", job.Roll)
			}
			for i, p := range job.Pages {
				wantLength := uint16(m.LengthDots + lengthSlack)
				switch {
				case !m.DieCut():
					wantLength = 0xFFFF
				case mo.Protocol == ProtocolV2:
					wantLength = 0
				}
				if p.Length != wantLength {
					t.Errorf("page %d: label length %#x, want %#x", i, p.Length, wantLength)
				}
				if mo.Protocol == ProtocolV2 && p.Index != uint16(i+1) {
					t.Errorf("page %d: label index %d", i, p.Index)
				}
				// Die-cut pages lose their trailing blank lines; the form
				// feed covers them.
				want := pages[i]
				if m.DieCut() {
					n := want.Rect.Dy()
					for n > 0 && blankRow(want, n-1) {
						n--
					}
					want = want.SubImage(image.Rect(0, 0, w, n)).(*image.Gray)
				}
				if !sameImage(p.Image, want) {
					t.Errorf("page %d: decoded %v differs from the %v input", i, p.Image.Bounds(), want.Bounds())
				}
			}
		})
	}
}

func sameImage(a, b *image.Gray) bool {
	if a.Bounds().Dx() != b.Bounds().Dx() || a.Bounds().Dy() != b.Bounds().Dy() {
		return false
	}
	for y := 0; y < a.Bounds().Dy(); y++ {
		for x := 0; x < a.Bounds().Dx(); x++ {
			if (a.Pix[y*a.Stride+x] < 0x80) != (b.Pix[y*b.Stride+x] < 0x80) {
				return false
			}
		}
	}
	return true
}

func TestEncodeJobRejectsBadInput(t *testing.T) {
	lw450, _ := LookupModel("LabelWriter 450")
	address, _ := LookupMedia("30252")
	good := markedLabel(331, 1051)
	if _, err := EncodeJob(lw450, address, nil, JobOptions{}); err == nil {
		t.Error("empty job accepted")
	}
	if _, err := EncodeJob(lw450, address, []*image.Gray{markedLabel(300, 1051)}, JobOptions{}); err == nil {
		t.Error("wrong width accepted")
	}
	if _, err := EncodeJob(lw450, address, []*image.Gray{markedLabel(331, 900)}, JobOptions{}); err == nil {
		t.Error("wrong die-cut length accepted")
	}
	if _, err := EncodeJob(lw450, address, []*image.Gray{good}, JobOptions{Density: "extra"}); err == nil {
		t.Error("unknown density accepted")
	}
	if _, err := EncodeJob(lw450, address, []*image.Gray{good}, JobOptions{Roll: "middle"}); err == nil {
		t.Error("unknown roll accepted")
	}
}

func TestDecodeJobRejectsDamagedStreams(t *testing.T) {
	address, _ := LookupMedia("30252")
	for _, name := range []string{"LabelWriter 450", "LabelWriter 550"} {
		mo, _ := LookupModel(name)
		stream, err := EncodeJob(mo, address, []*image.Gray{checkerPage(331, 1051)}, JobOptions{})
		if err != nil {
			t.Fatal(err)
		}
		for what, damaged := range map[string][]byte{
			"truncated":      stream[:len(stream)-40],
			"no ending":      stream[:len(stream)-2],
			"trailing bytes": append(append([]byte{}, stream...), 0x00),
		} {
			if _, err := DecodeJob(mo.Protocol, damaged, 331); err == nil {
				t.Errorf("%s: %s stream accepted", name, what)
			}
		}
	}
}
