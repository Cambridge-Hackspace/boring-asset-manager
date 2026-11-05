package zebra

import (
	"bytes"
	"flag"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/*.bin")

func TestModelsAndMedia(t *testing.T) {
	gk, ok := LookupModel(ZPL, "Zebra GK420d")
	if !ok || gk.DPI != 203 || gk.HeadDots != 832 {
		t.Errorf("GK420d = %+v", gk)
	}
	if _, ok := LookupModel(EPL2, "Zebra GK420d"); ok {
		t.Error("a ZPL model is found as EPL2")
	}
	lp, ok := LookupModel(EPL2, "Zebra LP 2844")
	if !ok || lp.HeadDots != 832 {
		t.Errorf("LP 2844 = %+v", lp)
	}
	for _, name := range Models(ZPL) {
		if m, _ := LookupModel(ZPL, name); m.DPI != 203 && m.DPI != 300 && m.DPI != 600 {
			t.Errorf("%s at %d DPI", name, m.DPI)
		}
	}
	shipping, _ := LookupMedia("4x6")
	if shipping.WidthMM != 101.6 || shipping.LengthMM != 152.4 || !shipping.DieCut() || gk.Suits(shipping) != nil {
		t.Errorf("4x6 = %+v", shipping)
	}
	// The fit check, for the 2-, 4- and 8-inch heads.
	two, _ := LookupModel(ZPL, "Zebra ZD410")
	eight, _ := LookupModel(ZPL, "Generic ZPL 8 in 203 DPI")
	for _, c := range []struct {
		model Model
		width float64
		fits  bool
	}{
		{two, 50.8, true}, {two, 101.6, false},
		{gk, 101.6, true}, {gk, 152.4, false},
		{eight, 203.2, true}, {eight, 228.6, false},
	} {
		if err := c.model.Suits(Media{Name: "test", WidthMM: c.width}); (err == nil) != c.fits {
			t.Errorf("%s with %.1f mm labels: %v", c.model.Name, c.width, err)
		}
	}
}

// markedLabel draws a dark square near the top-left corner of a white label.
func markedLabel(w, h int) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 0xFF
	}
	for y := 20; y < 30; y++ {
		for x := 20; x < 30; x++ {
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
	gk, _ := LookupModel(ZPL, "Zebra GK420d")
	page := Prepare(gk, markedLabel(812, 1219), Placement{})
	if page.Bounds() != image.Rect(0, 0, 812, 1219) || darkBounds(page) != image.Rect(20, 20, 30, 30) {
		t.Errorf("page %v, mark at %v", page.Bounds(), darkBounds(page))
	}
	// Counterclockwise: the top-left corner ends at the bottom-left.
	page = Prepare(gk, markedLabel(1219, 812), Placement{Rotate: true})
	if page.Bounds() != image.Rect(0, 0, 812, 1219) || darkBounds(page) != image.Rect(20, 1219-30, 30, 1219-20) {
		t.Errorf("rotated page %v, mark at %v", page.Bounds(), darkBounds(page))
	}
	// Wider than the head: the center is kept.
	if w := Prepare(gk, markedLabel(900, 100), Placement{}).Bounds().Dx(); w != 832 {
		t.Errorf("page %d dots wide, want the head's 832", w)
	}
}

func TestZPLCompressionFollowsTheGuide(t *testing.T) {
	// ZPL II Programming Guide, ^GF's ASCII compression: G to Y count 1 to
	// 19, g to z 20 to 400, combined for any count up to 419.
	for n, want := range map[int]string{1: "GA", 19: "YA", 20: "gA", 41: "hGA", 400: "zA", 419: "zYA", 420: "zYAGA"} {
		var b strings.Builder
		writeRun(&b, n, 'A')
		if b.String() != want {
			t.Errorf("%d: %q, want %q", n, b.String(), want)
		}
	}
	// A row's trailing zeros are a comma, trailing ones an exclamation
	// mark, and a row like the one before a colon.
	rows := [][]byte{
		{0xFF, 0x00, 0x00, 0x00},
		{0xFF, 0x00, 0x00, 0x00},
		{0x0A, 0xAA, 0xFF, 0xFF},
		{0x00, 0x00, 0x00, 0x00},
		{0x12, 0x34, 0x56, 0x78},
	}
	if got, want := compressZPL(rows), "FF,:0IA!,12345678"; got != want {
		t.Errorf("compressed %q, want %q", got, want)
	}
}

func TestZPLCompressionRoundTrips(t *testing.T) {
	widthBytes := 104
	var rows [][]byte
	pattern := func(f func(i int) byte) []byte {
		row := make([]byte, widthBytes)
		for i := range row {
			row[i] = f(i)
		}
		return row
	}
	rows = append(rows,
		pattern(func(int) byte { return 0 }),
		pattern(func(int) byte { return 0xFF }),
		pattern(func(int) byte { return 0xFF }),
		pattern(func(i int) byte { return byte(i * 37) }),
		pattern(func(i int) byte {
			if i < 60 {
				return 0x11
			}
			return 0
		}),
	)
	// A run longer than one count reaches: 2,000 zeros then a dot.
	wide := make([]byte, 1001)
	wide[1000] = 1
	got, err := decompressZPL(compressZPL([][]byte{wide}), len(wide), len(wide))
	if err != nil || !bytes.Equal(got, wide) {
		t.Fatalf("a long run: %v", err)
	}
	flat := bytes.Join(rows, nil)
	got, err = decompressZPL(compressZPL(rows), len(flat), widthBytes)
	if err != nil || !bytes.Equal(got, flat) {
		t.Fatalf("round trip: %v", err)
	}
	if _, err := decompressZPL(":", 4, 4); err == nil {
		t.Error("a colon with no row before was taken")
	}
}

// testPage is a label at a model's DPI with a frame and a mark, so its
// rows are not all alike.
func testPage(mo Model, m Media) *image.Gray {
	w, h := mo.Dots(m.WidthMM)-16, mo.Dots(25.4)
	page := image.NewGray(image.Rect(0, 0, min(w, mo.HeadDots), h))
	for i := range page.Pix {
		page.Pix[i] = 0xFF
	}
	for y := range h {
		for x := range page.Rect.Dx() {
			if y < 4 || y >= h-4 || x < 4 || x >= page.Rect.Dx()-4 || (x/9+y/9)%7 == 0 {
				page.Pix[y*page.Stride+x] = 0
			}
		}
	}
	return page
}

func samePrint(t *testing.T, name string, printed, page *image.Gray, x int) {
	t.Helper()
	for y := range page.Rect.Dy() {
		for px := range page.Rect.Dx() {
			if (page.Pix[y*page.Stride+px] < 0x80) != (printed.Pix[y*printed.Stride+x+px] < 0x80) {
				t.Fatalf("%s: dot (%d, %d) differs", name, px, y)
			}
		}
	}
}

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name+".bin")
	if *update {
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

func TestZPLJobsAtEachDPIAndMode(t *testing.T) {
	media, _ := LookupMedia("2x1")
	dark, speed := 18, 4
	for _, c := range []struct {
		model string
		opts  ZPLOptions
	}{
		{"Generic ZPL 203 DPI", ZPLOptions{PrintMode: "tear_off"}},
		{"Generic ZPL 203 DPI", ZPLOptions{PrintMode: "tear_off", Uncompressed: true}},
		{"Generic ZPL 300 DPI", ZPLOptions{PrintMode: "peel_off", MediaType: "direct"}},
		{"Generic ZPL 300 DPI", ZPLOptions{PrintMode: "peel_off", MediaType: "direct", Uncompressed: true}},
		{"Generic ZPL 600 DPI", ZPLOptions{PrintMode: "cutter", MediaType: "transfer", Darkness: &dark, Speed: &speed, Calibrate: true}},
		{"Generic ZPL 600 DPI", ZPLOptions{PrintMode: "cutter", Uncompressed: true}},
	} {
		mo, _ := LookupModel(ZPL, c.model)
		page := testPage(mo, media)
		stream, err := EncodeZPL(mo, media, []*image.Gray{page, page}, c.opts)
		if err != nil {
			t.Fatal(err)
		}
		name := fmt.Sprintf("zpl-%d-%s", mo.DPI, strings.ReplaceAll(c.opts.PrintMode, "_", "-"))
		if c.opts.Uncompressed {
			name += "-uncompressed"
		}
		golden(t, name, stream)

		job, err := DecodeZPL(stream)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(job.Labels) != 1 || job.Labels[0].Copies != 2 {
			t.Fatalf("%s: %d labels", name, len(job.Labels))
		}
		l := job.Labels[0]
		cmds := strings.Join(l.Commands, " ")
		if l.Width != mo.Dots(media.WidthMM) || l.Length != mo.Dots(media.LengthMM) || !strings.Contains(cmds, "^MNY") ||
			!strings.Contains(cmds, PrintModes[c.opts.PrintMode]) {
			t.Errorf("%s: %+v %q", name, l, cmds)
		}
		if c.opts.MediaType != "" && !strings.Contains(cmds, MediaTypes[c.opts.MediaType]) {
			t.Errorf("%s: no media type in %q", name, cmds)
		}
		samePrint(t, name, l.Image, page, (l.Width-page.Rect.Dx())/2)
		if c.opts.Calibrate && strings.Join(job.Setup, " ") != "~JC ~SD18" {
			t.Errorf("%s: setup %q", name, job.Setup)
		}
		if c.opts.Speed != nil && !strings.Contains(cmds, "^PR4") {
			t.Errorf("%s: no speed in %q", name, cmds)
		}
	}
}

func TestALargeZPLLabelIsSentInBands(t *testing.T) {
	mo, _ := LookupModel(ZPL, "Generic ZPL 300 DPI")
	media, _ := LookupMedia("continuous-4")
	page := testPage(mo, media)
	tall := image.NewGray(image.Rect(0, 0, page.Rect.Dx(), 1800))
	for y := range 1800 {
		copy(tall.Pix[y*tall.Stride:], page.Pix[(y%page.Rect.Dy())*page.Stride:(y%page.Rect.Dy()+1)*page.Stride])
	}
	stream, err := EncodeZPL(mo, media, []*image.Gray{tall}, ZPLOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(stream), "^GFA"); n < 3 {
		t.Errorf("%d graphics for %d bytes", n, (tall.Rect.Dx()+7)/8*1800)
	}
	job, err := DecodeZPL(stream)
	if err != nil {
		t.Fatal(err)
	}
	// Continuous: as long as the page, and not found by gaps.
	l := job.Labels[0]
	if l.Length != 1800 || !strings.Contains(strings.Join(l.Commands, " "), "^MNN") {
		t.Errorf("%+v", l.Commands)
	}
	samePrint(t, "bands", l.Image, tall, (l.Width-tall.Rect.Dx())/2)
}

func TestEPL2Jobs(t *testing.T) {
	media, _ := LookupMedia("2x1")
	density := 10
	for _, c := range []struct {
		model string
		opts  EPL2Options
	}{
		{"Zebra LP 2824", EPL2Options{}},
		{"Zebra TLP 2844", EPL2Options{Density: &density}},
	} {
		mo, _ := LookupModel(EPL2, c.model)
		page := testPage(mo, media)
		other := markedLabel(page.Rect.Dx(), page.Rect.Dy())
		stream, err := EncodeEPL2(mo, media, []*image.Gray{page, page, other}, c.opts)
		if err != nil {
			t.Fatal(err)
		}
		name := "epl2-" + strings.ToLower(strings.ReplaceAll(strings.TrimPrefix(c.model, "Zebra "), " ", "-"))
		golden(t, name, stream)
		job, err := DecodeEPL2(stream)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(job.Labels) != 2 || job.Labels[0].Copies != 2 || job.Labels[1].Copies != 1 {
			t.Fatalf("%s: %+v", name, job.Labels)
		}
		l := job.Labels[0]
		if l.Width != mo.Dots(media.WidthMM) || l.Length != mo.Dots(media.LengthMM) || l.Gap != mo.Dots(3.2) {
			t.Errorf("%s: %+v", name, l)
		}
		if (c.opts.Density != nil) != slicesContain(l.Commands, "D10") || !slicesContain(l.Commands, "ZT") {
			t.Errorf("%s: commands %q", name, l.Commands)
		}
		samePrint(t, name, l.Image, page, (l.Width-page.Rect.Dx())/2)
		samePrint(t, name+" second", job.Labels[1].Image, other, (l.Width-other.Rect.Dx())/2)
	}
}

func slicesContain(s []string, v string) bool {
	for _, e := range s {
		if e == v {
			return true
		}
	}
	return false
}

func TestBadJobsAreRefused(t *testing.T) {
	gk, _ := LookupModel(ZPL, "Zebra GK420d")
	media, _ := LookupMedia("4x6")
	page := image.NewGray(image.Rect(0, 0, 10, 10))
	bad := 31
	slow := 0
	for _, o := range []ZPLOptions{{MediaType: "ribbon"}, {PrintMode: "rewind"}, {Darkness: &bad}, {Speed: &slow}} {
		if _, err := EncodeZPL(gk, media, []*image.Gray{page}, o); err == nil {
			t.Errorf("%+v was taken", o)
		}
	}
	if _, err := EncodeZPL(gk, media, nil, ZPLOptions{}); err == nil {
		t.Error("a job of no pages was taken")
	}
	if _, err := EncodeZPL(gk, media, []*image.Gray{image.NewGray(image.Rect(0, 0, 900, 10))}, ZPLOptions{}); err == nil {
		t.Error("a page wider than the head was taken")
	}
	lp, _ := LookupModel(EPL2, "Zebra LP 2844")
	dense := 16
	if _, err := EncodeEPL2(lp, media, []*image.Gray{page}, EPL2Options{Density: &dense}); err == nil {
		t.Error("density 16 was taken")
	}
}
