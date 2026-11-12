package dymolm

import (
	"bytes"
	"encoding/binary"
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

// TestPrintableDots checks each tape's printable dots against the width of
// DYMO's imageable area for it, in points, from the model's PPD, at the
// model's resolution; and that they fit on the head.
func TestPrintableDots(t *testing.T) {
	// lmpnp.ppd, lm450.ppd (a 128-dot head at 180 DPI, for the 420P) and
	// lmwpnp.ppd: each tape's ImageableArea, left to right.
	pnp := map[string][2]float64{"d1-6": {2.8, 14.8}, "d1-9": {2.8, 22.8}, "d1-12": {5.6, 28.8}}
	lm450 := map[string][2]float64{"d1-6": {2.8, 14.8}, "d1-9": {2.8, 22.8}, "d1-12": {2.8, 31.6}, "d1-19": {7.2, 47.2}}
	wireless := map[string][2]float64{"d1-6": {1.6, 16.0}, "d1-9": {2.8, 22.8}, "d1-12": {4.4, 30.0}, "d1-19": {7.2, 47.2}, "d1-24": {8.4, 59.6}}
	for name, areas := range map[string]map[string][2]float64{
		"LabelManager PnP": pnp, "LabelManager 280": pnp, "LabelManager 420P": lm450,
		"LabelManager Wireless PnP": wireless, "LabelManager 500TS": wireless,
	} {
		mo := model(t, name)
		taken := 0
		for _, id := range MediaIDs() {
			m, _ := LookupMedia(id)
			area, ok := areas[id]
			if err := mo.Takes(m); (err == nil) != ok {
				t.Errorf("%s takes %s: %v", name, id, err)
			}
			if !ok {
				continue
			}
			taken++
			l, _ := mo.Layout(m)
			want := int((area[1]-area[0])/72*float64(mo.DPI) + 1e-6)
			if l.PrintableDots != want {
				t.Errorf("%s, %s: %d dots, DYMO's area is %d", name, id, l.PrintableDots, want)
			}
			if l.FirstDot < 0 || l.FirstDot+l.PrintableDots > mo.HeadDots {
				t.Errorf("%s, %s: dots %d-%d are off the %d-dot head", name, id, l.FirstDot, l.FirstDot+l.PrintableDots, mo.HeadDots)
			}
		}
		if taken != len(areas) {
			t.Errorf("%s takes %d tapes, want %d", name, taken, len(areas))
		}
	}
	// Centered on the head, but for the 500TS's 12 and 19 mm tapes, which
	// DYMO's driver moves.
	for _, c := range []struct {
		model, media string
		first        int
	}{
		{"LabelManager PnP", "d1-12", 3},
		{"LabelManager 420P", "d1-19", 14},
		{"LabelManager Wireless PnP", "d1-12", 75},
		{"LabelManager 500TS", "d1-12", 77},
		{"LabelManager 500TS", "d1-19", 41},
		{"LabelManager 500TS", "d1-24", 21},
	} {
		if l := layout(t, c.model, c.media); l.FirstDot != c.first {
			t.Errorf("%s, %s: first dot %d, want %d", c.model, c.media, l.FirstDot, c.first)
		}
	}
}

func checkerPage(dots, lines int) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, dots, lines))
	for y := 0; y < lines; y++ {
		for x := 0; x < dots; x++ {
			if (x/8+y/8)%2 == 0 {
				continue
			}
			img.Pix[y*img.Stride+x] = 0xFF
		}
	}
	// Blank lines inside the label and at its end, and a dark first
	// column, so skips, trimming and placement show in the stream.
	for y := 20; y < 30 && y < lines; y++ {
		for x := 0; x < dots; x++ {
			img.Pix[y*img.Stride+x] = 0xFF
		}
	}
	for y := max(0, lines-12); y < lines; y++ {
		for x := 0; x < dots; x++ {
			img.Pix[y*img.Stride+x] = 0xFF
		}
	}
	for y := 0; y < lines-12; y++ {
		if y < 20 || y >= 30 {
			img.Pix[y*img.Stride] = 0
		}
	}
	return img
}

// TestEncodeJobGoldens pins the byte stream for every tape on every model,
// and checks that decoding it gives back the pages that went in, with the
// margins and cuts DYMO's driver gives them.
func TestEncodeJobGoldens(t *testing.T) {
	n := 0
	for _, name := range Models() {
		mo := model(t, name)
		for _, id := range MediaIDs() {
			m, _ := LookupMedia(id)
			l, err := mo.Layout(m)
			if err != nil {
				continue
			}
			n++
			pages := []*image.Gray{checkerPage(l.PrintableDots, 300), checkerPage(l.PrintableDots, 240)}
			opts := DefaultOptions(mo)
			stream, err := EncodeJob(l, pages, opts)
			if err != nil {
				t.Fatalf("%s %s: %v", name, id, err)
			}
			golden := strings.ToLower(strings.ReplaceAll(name, " ", "-")) + "-" + id
			checkGolden(t, golden, stream)

			job, err := mo.DecodeJob(stream)
			if err != nil {
				t.Fatalf("%s: %v", golden, err)
			}
			// The first label after the margin, less the tape already
			// between head and cutter; between labels, both margins.
			at := opts.MarginLines - mo.CutterLines
			for i, p := range pages {
				if i > 0 {
					at += 2 * opts.MarginLines
					if !mo.AutoCut {
						at++ // the cut mark
					}
				}
				got := l.PageImage(job.Lines[at : at+p.Rect.Dy()])
				if !bytes.Equal(got.Pix, p.Pix) {
					t.Errorf("%s page %d: decoded bitmap differs from the input", golden, i)
				}
				at += p.Rect.Dy()
			}
			if want := at + opts.MarginLines + mo.CutterLines; len(job.Lines) != want {
				t.Errorf("%s: %d lines fed, want %d", golden, len(job.Lines), want)
			}
			if mo.AutoCut {
				// Each label is cut off where its trailing margin ends.
				first := opts.MarginLines - mo.CutterLines + 300 + opts.MarginLines + mo.CutterLines
				if len(job.Cuts) != 2 || job.Cuts[0] != first || job.Cuts[1] != len(job.Lines) {
					t.Errorf("%s: cuts after %v lines, want %d and %d", golden, job.Cuts, first, len(job.Lines))
				}
			} else {
				mark := opts.MarginLines - mo.CutterLines + 300 + opts.MarginLines
				if !bytes.Equal(job.Lines[mark], bytes.Repeat([]byte{chainMark}, mo.HeadDots/8)) {
					t.Errorf("%s: no cut mark between the labels", golden)
				}
			}
		}
	}
	if n != 3+3+4+5+5 {
		t.Errorf("%d goldens, want 20", n)
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

// TestStreamCommands checks a short job byte by byte against Appendix B of
// the LabelWriter 450 Series Technical Reference Manual: ESC C, ESC B and
// ESC D with SYN lines, blank lines as SYNs with no bytes per line, and
// ESC E to cut.
func TestStreamCommands(t *testing.T) {
	l := layout(t, "LabelManager Wireless PnP", "d1-6")
	page := image.NewGray(image.Rect(0, 0, 60, 3))
	for i := range page.Pix {
		page.Pix[i] = 0xFF
	}
	// Line 0: the page's first two dots; line 1 blank; line 2 its last.
	page.Pix[0], page.Pix[1] = 0, 0
	page.Pix[2*page.Stride+59] = 0
	opts := JobOptions{Tape: "white_on_black", Cut: CutEach, MarginLines: 100}
	stream, err := EncodeJob(l, []*image.Gray{page}, opts)
	if err != nil {
		t.Fatal(err)
	}
	// 60 dots on the 256-dot head from dot 98: the first two are dots 98
	// and 99, the third and fourth bits of byte 12 (0x30); the last, dot
	// 157, the sixth of byte 19 (0x04).
	syns := func(n int) []byte { return bytes.Repeat([]byte{0x16}, n) }
	var want []byte
	want = append(want, make([]byte, 32)...)
	want = append(want, 0x1B, 'C', 10)
	want = append(want, 0x1B, 'D', 0)
	want = append(want, syns(100-92)...)
	want = append(want, 0x1B, 'B', 12, 0x1B, 'D', 1, 0x16, 0x30)
	want = append(want, 0x1B, 'D', 0, 0x16)
	want = append(want, 0x1B, 'B', 19, 0x1B, 'D', 1, 0x16, 0x04)
	// The margin after, lengthened to the shortest label (222 lines),
	// then on to the cutter, and the cut.
	want = append(want, 0x1B, 'D', 0)
	want = append(want, syns(100+222-3)...)
	want = append(want, 0x1B, 'D', 0)
	want = append(want, syns(92)...)
	want = append(want, 0x1B, 'E')
	if !bytes.Equal(stream, want) {
		t.Errorf("stream\n% X\nwant\n% X", stream, want)
	}
}

// TestModelsWithoutACutter print a cut mark between labels, cut nothing,
// and feed the last label to the cutter.
func TestModelsWithoutACutter(t *testing.T) {
	l := layout(t, "LabelManager PnP", "d1-12")
	stream, err := EncodeJob(l, []*image.Gray{checkerPage(58, 40), checkerPage(58, 40)}, DefaultOptions(l.Model))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stream, []byte{0x1B, 'E'}) {
		t.Error("the PnP was told to cut")
	}
	// The mark: dot tab 0, a full line of 99h.
	if !bytes.Contains(stream, append([]byte{0x1B, 'B', 0, 0x1B, 'D', 8, 0x16}, bytes.Repeat([]byte{0x99}, 8)...)) {
		t.Error("no cut mark")
	}
	if !bytes.HasSuffix(stream, append([]byte{0x1B, 'D', 0}, bytes.Repeat([]byte{0x16}, 58)...)) {
		t.Error("the last label is not fed to the cutter")
	}
	if _, err := EncodeJob(l, []*image.Gray{checkerPage(58, 40)}, JobOptions{Tape: TapeBlackOnWhite, Cut: CutEnd, MarginLines: 75}); err == nil {
		t.Error("the PnP took cut_after last_label")
	}
}

// TestCutAtTheEnd cuts a job on a model with a cutter only after its last
// label, with cut marks between.
func TestCutAtTheEnd(t *testing.T) {
	l := layout(t, "LabelManager Wireless PnP", "d1-24")
	opts := DefaultOptions(l.Model)
	opts.Cut = CutEnd
	pages := []*image.Gray{checkerPage(213, 300), checkerPage(213, 300), checkerPage(213, 300)}
	stream, err := EncodeJob(l, pages, opts)
	if err != nil {
		t.Fatal(err)
	}
	job, err := l.Model.DecodeJob(stream)
	if err != nil {
		t.Fatal(err)
	}
	if len(job.Cuts) != 1 {
		t.Errorf("cuts after %v lines, want one at the end", job.Cuts)
	}
	marks := 0
	for _, line := range job.Lines {
		if line[0] == chainMark {
			marks++
		}
	}
	if marks != 2 {
		t.Errorf("%d cut marks, want 2", marks)
	}
}

// TestFramed500TS checks the 500TS's blocks: ESC Y 1 and a big-endian
// length before each, at most 4096 bytes, and ESC Y 0 after the last; and
// that the stream inside is the Wireless PnP's.
func TestFramed500TS(t *testing.T) {
	ts := layout(t, "LabelManager 500TS", "d1-24")
	w := layout(t, "LabelManager Wireless PnP", "d1-24")
	pages := []*image.Gray{checkerPage(213, 400)}
	framed, err := EncodeJob(ts, pages, DefaultOptions(ts.Model))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := EncodeJob(w, pages, DefaultOptions(w.Model))
	if err != nil {
		t.Fatal(err)
	}
	var inner []byte
	s := framed
	blocks := 0
	for {
		if s[0] != 0x1B || s[1] != 'Y' {
			t.Fatalf("block %d starts % X", blocks, s[:3])
		}
		n := int(binary.BigEndian.Uint32(s[3:7]))
		if s[2] == 0 {
			if n != 0 || len(s) != 7 {
				t.Errorf("end of job % X", s)
			}
			break
		}
		if n > 4096 {
			t.Errorf("block %d holds %d bytes", blocks, n)
		}
		inner = append(inner, s[7:7+n]...)
		s = s[7+n:]
		blocks++
	}
	if blocks < 2 || !bytes.Equal(inner, plain) {
		t.Errorf("%d blocks; the stream inside differs from the Wireless PnP's", blocks)
	}
	if _, err := ts.Model.DecodeJob(framed); err != nil {
		t.Error(err)
	}
	if _, err := ts.Model.DecodeJob(plain); err == nil {
		t.Error("the 500TS decoded a stream without blocks")
	}
}

func TestOptionsAreChecked(t *testing.T) {
	l := layout(t, "LabelManager 420P", "d1-12")
	for name, opts := range map[string]JobOptions{
		"unknown tape":              {Tape: "gold_on_black", Cut: CutEach, MarginLines: 75},
		"unknown cut":               {Tape: TapeBlackOnWhite, Cut: "never", MarginLines: 75},
		"margin short of cutter":    {Tape: TapeBlackOnWhite, Cut: CutEach, MarginLines: 57},
		"margin over 127 mm":        {Tape: TapeBlackOnWhite, Cut: CutEach, MarginLines: 901},
		"cut at the end, no cutter": {Tape: TapeBlackOnWhite, Cut: CutEnd, MarginLines: 75},
	} {
		if _, err := EncodeJob(l, []*image.Gray{checkerPage(72, 100)}, opts); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if _, err := EncodeJob(l, []*image.Gray{checkerPage(72, 100)}, JobOptions{Tape: "red_on_white", Cut: CutEach, MarginLines: 58}); err != nil {
		t.Errorf("a margin of the cutter's distance: %v", err)
	}
	if got := len(TapeTypes()); got != 13 {
		t.Errorf("%d tape types, the manual has 13", got)
	}
}

func TestEncodeJobRejectsMisshapenPages(t *testing.T) {
	l := layout(t, "LabelManager PnP", "d1-9")
	opts := DefaultOptions(l.Model)
	if _, err := EncodeJob(l, nil, opts); err == nil {
		t.Error("empty job accepted")
	}
	if _, err := EncodeJob(l, []*image.Gray{checkerPage(49, 100)}, opts); err == nil {
		t.Error("wrong width accepted")
	}
	if _, err := EncodeJob(l, []*image.Gray{checkerPage(50, l.Model.MaxLines()+1)}, opts); err == nil {
		t.Error("too long accepted")
	}
	if _, err := model(t, "LabelManager PnP").Layout(Media{ID: "d1-24", Name: "D1 tape 24 mm", WidthMM: 24}); err == nil {
		t.Error("the PnP took 24 mm tape")
	}
}

func TestDecodeJobRejectsDamagedStreams(t *testing.T) {
	l := layout(t, "LabelManager Wireless PnP", "d1-12")
	stream, err := EncodeJob(l, []*image.Gray{checkerPage(106, 100)}, DefaultOptions(l.Model))
	if err != nil {
		t.Fatal(err)
	}
	for name, damaged := range map[string][]byte{
		"no final cut":  stream[:len(stream)-2],
		"no NUL bytes":  stream[32:],
		"unknown bytes": append(append([]byte{}, stream...), 0x1B, 'Z'),
	} {
		if _, err := l.Model.DecodeJob(damaged); err == nil {
			t.Errorf("%s stream accepted", name)
		}
	}
	if _, err := l.Model.DecodeJob(stream[:32]); err == nil {
		t.Error("decoded a stream without a tape type")
	}
	// The PnP has no cutter, and refuses a stream that cuts.
	if _, err := model(t, "LabelManager PnP").DecodeJob(append(append(make([]byte, 8), 0x1B, 'C', 0), 0x1B, 'E')); err == nil {
		t.Error("the PnP decoded a cut")
	}
}

// TestPrepareTurnsTheLabel checks where a label turned to run along the
// tape lands: its left edge sent first, its bottom edge on the head's first
// printable dot.
func TestPrepareTurnsTheLabel(t *testing.T) {
	l := layout(t, "LabelManager PnP", "d1-12")
	// 100 dots along the tape, 58 across, with one dark dot at its
	// bottom-left corner.
	label := image.NewGray(image.Rect(0, 0, 100, 58))
	for i := range label.Pix {
		label.Pix[i] = 0xFF
	}
	label.Pix[57*label.Stride] = 0
	// Placed upright the label is 58 wide; turned, its rows run along.
	page := l.Prepare(label, Placement{Rotate: true})
	if b := page.Bounds(); b.Dx() != 58 || b.Dy() != 100 {
		t.Fatalf("page is %v", b)
	}
	if page.Pix[0] != 0 {
		t.Error("the label's bottom-left corner is not the first dot of the first line")
	}
	dark := 0
	for _, v := range page.Pix {
		if v < 0x80 {
			dark++
		}
	}
	if dark != 1 {
		t.Errorf("%d dark dots, want 1", dark)
	}
}
