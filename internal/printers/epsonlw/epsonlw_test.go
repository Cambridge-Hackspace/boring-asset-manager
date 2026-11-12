package epsonlw

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
		t.Fatalf("no cartridge %s", mediaID)
	}
	l, err := model(t, modelName).Layout(m)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// TestTapes checks which tapes each model takes, by Epson's capability
// table, and that each tape's printable dots fit on it: 72 on 12 mm tape,
// the one width measured on a printer.
func TestTapes(t *testing.T) {
	takes := map[string]string{
		"LW-600P":  "lk-6 lk-9 lk-12 lk-18 lk-24",
		"LW-PX400": "lk-4 lk-6 lk-9 lk-12 lk-18 lk-24",
		"LW-C410":  "lk-4 lk-6 lk-9 lk-12 lk-18",
	}
	for name, want := range takes {
		mo := model(t, name)
		var got []string
		for _, id := range MediaIDs() {
			m, _ := LookupMedia(id)
			l, err := mo.Layout(m)
			if err != nil {
				continue
			}
			got = append(got, id)
			if l.PrintableDots < 1 || l.PrintableDots > mo.Dots(m.WidthMM) {
				t.Errorf("%s, %s: %d dots on a tape %d wide", name, id, l.PrintableDots, mo.Dots(m.WidthMM))
			}
		}
		if strings.Join(got, " ") != want {
			t.Errorf("%s takes %v, want %s", name, got, want)
		}
		if mo.DPI != 180 {
			t.Errorf("%s at %d DPI", name, mo.DPI)
		}
	}
	if l := layout(t, "LW-600P", "lk-12"); l.PrintableDots != 72 {
		t.Errorf("12 mm tape prints %d dots, want 72", l.PrintableDots)
	}
	if got := len(Models()); got != 3 {
		t.Errorf("%d models", got)
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
	// A blank stretch inside the label and a dark first column, so that
	// blank lines and the bit order show in the stream.
	for y := 20; y < 30 && y < lines; y++ {
		for x := 0; x < dots; x++ {
			img.Pix[y*img.Stride+x] = 0xFF
		}
	}
	for y := 0; y < lines; y++ {
		if y < 20 || y >= 30 {
			img.Pix[y*img.Stride] = 0
		}
	}
	return img
}

func letters(cmds []Command) string {
	var b strings.Builder
	for _, c := range cmds {
		b.WriteByte(c.Letter)
	}
	return b.String()
}

// TestEncodeJobGoldens pins the byte stream for every tape on every model,
// and checks that decoding it gives back the pages that went in, with each
// model's commands and the margins around each label.
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
			golden := strings.ToLower(name) + "-" + id
			checkGolden(t, golden, stream)

			job, err := l.DecodeJob(stream)
			if err != nil {
				t.Fatalf("%s: %v", golden, err)
			}
			if got := letters(job.Setup); got != "!Q@{CDG" {
				t.Errorf("%s: job setup %q", golden, got)
			}
			pageSetup := "QLT"
			if mo.Level == 3 {
				pageSetup = "QLTHs"
			}
			if len(job.Pages) != len(pages) {
				t.Fatalf("%s: %d pages", golden, len(job.Pages))
			}
			margin := opts.MarginLines
			for i, p := range job.Pages {
				want := pages[i].Rect.Dy()
				if got := letters(p.Setup); got != pageSetup {
					t.Errorf("%s page %d: setup %q, want %q", golden, i, got, pageSetup)
				}
				if got := binary.LittleEndian.Uint32(p.Setup[1].Params); int(got) != want+2*margin || len(p.Lines) != want+2*margin {
					t.Errorf("%s page %d: length %d, %d lines, want %d", golden, i, got, len(p.Lines), want+2*margin)
				}
				if got := binary.LittleEndian.Uint16(p.Setup[2].Params); int(got) != margin {
					t.Errorf("%s page %d: margin %d, want %d", golden, i, got, margin)
				}
				blank := l.PageImage(append(append([][]byte{}, p.Lines[:margin]...), p.Lines[len(p.Lines)-margin:]...))
				for _, v := range blank.Pix {
					if v != 0xFF {
						t.Errorf("%s page %d: the margins print", golden, i)
						break
					}
				}
				if got := l.PageImage(p.Lines[margin : margin+want]); !bytes.Equal(got.Pix, pages[i].Pix) {
					t.Errorf("%s page %d: decoded bitmap differs from the input", golden, i)
				}
			}
		}
	}
	if n != 5+6+5 {
		t.Errorf("%d goldens, want 16", n)
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

// TestStreamCommands checks a short job byte by byte against the framing
// the community descriptions agree on: each ESC { command's length counts
// its letter through its closing brace, and its checksum sums its letter
// and parameters; each raster line gives its dots, little-endian.
func TestStreamCommands(t *testing.T) {
	l := layout(t, "LW-PX400", "lk-4")
	// 22 dots across: line 0 the first two, line 1 the last.
	page := image.NewGray(image.Rect(0, 0, 22, 2))
	for i := range page.Pix {
		page.Pix[i] = 0xFF
	}
	page.Pix[0], page.Pix[1] = 0, 0
	page.Pix[page.Stride+21] = 0
	stream, err := EncodeJob(l, []*image.Gray{page}, JobOptions{Cut: CutEnd, Density: -2, MarginLines: 1})
	if err != nil {
		t.Fatal(err)
	}
	line := func(b ...byte) []byte { return append([]byte{0x1B, 0x2E, 0, 0, 0, 1, 22, 0}, b...) }
	var want []byte
	for _, b := range [][]byte{
		{0x1B, 0x7B, 3, 0x21, 0x21, 0x7D},                   // reset
		{0x1B, 0x7B, 5, 0x51, 5, 0, 0x56, 0x7D},             // status on
		{0x1B, 0x7B, 3, 0x40, 0x40, 0x7D},                   // print end
		{0x1B, 0x7B, 7, 0x7B, 0, 0, 0x53, 0x54, 0x22, 0x7D}, // signature
		{0x1B, 0x7B, 7, 0x43, 1, 0, 1, 1, 0x46, 0x7D},       // cut at the end
		{0x1B, 0x7B, 4, 0x44, 3, 0x47, 0x7D},                // density -2
		{0x1B, 0x7B, 3, 0x47, 0x47, 0x7D},                   // G
		{0x1B, 0x7B, 5, 0x51, 5, 0, 0x56, 0x7D},             // status on
		{0x1B, 0x7B, 7, 0x4C, 4, 0, 0, 0, 0x50, 0x7D},       // 4 lines
		{0x1B, 0x7B, 5, 0x54, 1, 0, 0x55, 0x7D},             // margin 1
		{0x1B, 0x7B, 4, 0x48, 5, 0x4D, 0x7D},                // H, level 3
		{0x1B, 0x7B, 4, 0x73, 1, 0x74, 0x7D},                // high speed
		// The margin, the two lines (dot 21 is byte 2, 04h) and the margin.
		line(0, 0, 0), line(0xC0, 0, 0), line(0, 0, 0x04), line(0, 0, 0),
		{0x0C},
		{0x1B, 0x7B, 3, 0x40, 0x40, 0x7D}, // print end
	} {
		want = append(want, b...)
	}
	if !bytes.Equal(stream, want) {
		t.Errorf("stream\n% X\nwant\n% X", stream, want)
	}
}

// TestCuts checks the cut command's bytes for each choice.
func TestCuts(t *testing.T) {
	l := layout(t, "LW-C410", "lk-12")
	for cut, want := range map[Cut][]byte{CutEach: {1, 1, 1, 1}, CutEnd: {1, 0, 1, 1}, CutNever: {0, 0, 0, 0}} {
		opts := DefaultOptions(l.Model)
		opts.Cut = cut
		stream, err := EncodeJob(l, []*image.Gray{checkerPage(72, 40)}, opts)
		if err != nil {
			t.Fatal(err)
		}
		job, err := l.DecodeJob(stream)
		if err != nil {
			t.Fatal(err)
		}
		if c := job.Setup[4]; c.Letter != 'C' || !bytes.Equal(c.Params, want) {
			t.Errorf("%s: %q % X, want C % X", cut, c.Letter, c.Params, want)
		}
	}
}

func TestOptionsAreChecked(t *testing.T) {
	l := layout(t, "LW-600P", "lk-12")
	for name, opts := range map[string]JobOptions{
		"unknown cut":        {Cut: "sometimes", MarginLines: 14},
		"too dark":           {Cut: CutEach, Density: 6, MarginLines: 14},
		"too light":          {Cut: CutEach, Density: -6, MarginLines: 14},
		"negative margin":    {Cut: CutEach, MarginLines: -1},
		"margin over 127 mm": {Cut: CutEach, MarginLines: 901},
	} {
		if _, err := EncodeJob(l, []*image.Gray{checkerPage(72, 100)}, opts); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	for _, opts := range []JobOptions{{Cut: CutNever, Density: 5}, {Cut: CutEach, Density: -5, MarginLines: 900}} {
		if _, err := EncodeJob(l, []*image.Gray{checkerPage(72, 100)}, opts); err != nil {
			t.Errorf("%+v: %v", opts, err)
		}
	}
	if o := DefaultOptions(l.Model); o.Cut != CutEach || o.Density != 0 || o.MarginLines != 14 {
		t.Errorf("defaults %+v", o)
	}
}

func TestEncodeJobRejectsMisshapenPages(t *testing.T) {
	l := layout(t, "LW-PX400", "lk-9")
	opts := DefaultOptions(l.Model)
	if _, err := EncodeJob(l, nil, opts); err == nil {
		t.Error("empty job accepted")
	}
	if _, err := EncodeJob(l, []*image.Gray{checkerPage(53, 100)}, opts); err == nil {
		t.Error("wrong width accepted")
	}
	if _, err := EncodeJob(l, []*image.Gray{checkerPage(54, l.Model.MaxLines()+1)}, opts); err == nil {
		t.Error("too long accepted")
	}
	if _, err := model(t, "LW-C410").Layout(Media{ID: "lk-24", Name: "LK tape 24 mm", WidthMM: 24}); err == nil {
		t.Error("the LW-C410 took 24 mm tape")
	}
	if _, err := model(t, "LW-600P").Layout(Media{ID: "lk-4", Name: "LK tape 4 mm", WidthMM: 4}); err == nil {
		t.Error("the LW-600P took 4 mm tape")
	}
}

func TestDecodeJobRejectsDamagedStreams(t *testing.T) {
	l := layout(t, "LW-PX400", "lk-12")
	stream, err := EncodeJob(l, []*image.Gray{checkerPage(72, 50)}, DefaultOptions(l.Model))
	if err != nil {
		t.Fatal(err)
	}
	bad := append([]byte{}, stream...)
	bad[4]++ // the reset's checksum
	for name, damaged := range map[string][]byte{
		"bad checksum":   bad,
		"no print end":   stream[:len(stream)-6],
		"no form feed":   append(append([]byte{}, stream[:len(stream)-7]...), stream[len(stream)-6:]...),
		"unknown bytes":  append(append([]byte{}, stream...), 0x1B, 'Z'),
		"truncated line": stream[:len(stream)-10],
	} {
		if _, err := l.DecodeJob(damaged); err == nil {
			t.Errorf("%s stream accepted", name)
		}
	}
	// Lines of another tape's width are refused.
	if _, err := layout(t, "LW-PX400", "lk-18").DecodeJob(stream); err == nil {
		t.Error("decoded 12 mm lines as 18 mm tape")
	}
}

// TestPrepareTurnsTheLabel checks where a label turned to run along the
// tape lands: its left edge sent first, its bottom edge first in each line.
func TestPrepareTurnsTheLabel(t *testing.T) {
	l := layout(t, "LW-600P", "lk-12")
	// 100 dots along the tape, 72 across, with one dark dot at its
	// bottom-left corner.
	label := image.NewGray(image.Rect(0, 0, 100, 72))
	for i := range label.Pix {
		label.Pix[i] = 0xFF
	}
	label.Pix[71*label.Stride] = 0
	page := l.Prepare(label, Placement{Rotate: true})
	if b := page.Bounds(); b.Dx() != 72 || b.Dy() != 100 {
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
