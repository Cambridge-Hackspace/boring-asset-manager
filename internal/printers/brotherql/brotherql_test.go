package brotherql

import (
	"bytes"
	"errors"
	"flag"
	"image"
	"math/rand"
	"os"
	"path/filepath"
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
	// A blank raster line compresses to two bytes.
	if got := packBits(make([]byte, bytesPerLine)); len(got) != 2 {
		t.Errorf("blank line packed to % X", got)
	}
}

func TestFit(t *testing.T) {
	dk22210, _ := LookupMedia("dk-22210")
	dk11201, _ := LookupMedia("dk-11201")
	dk22205, _ := LookupMedia("dk-22205")
	dk11207, _ := LookupMedia("dk-11207")
	dk22214, _ := LookupMedia("dk-22214")

	cases := []struct {
		name          string
		media         Media
		width, height float64
		dpi           int
		rotate        bool
		fits          bool
	}{
		{"die-cut portrait", dk11201, 29, 90, 300, false, true},
		{"die-cut landscape rotates", dk11201, 90, 29, 300, true, true},
		{"die-cut within tolerance", dk11201, 29.2, 89.7, 300, false, true},
		{"die-cut wrong length", dk11201, 29, 60, 300, false, false},
		{"continuous portrait", dk22210, 29, 50, 300, false, true},
		{"continuous landscape rotates", dk22205, 100, 62, 300, true, true},
		{"continuous too short", dk22210, 29, 20, 300, false, false},
		{"continuous too long", dk22210, 29, 1100, 300, false, false},
		{"wrong width", dk22205, 50, 29, 300, false, false},
		{"wrong dpi", dk22210, 29, 50, 203, false, false},
		{"round", dk11207, 58, 58, 300, false, true},
		{"narrow tape", dk22214, 12, 40, 300, false, true},
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

// TestCatalogFillsTheHead checks the catalog against the column of Brother's
// pin tables (Raster Command Reference, section 2.3.5) it does not store:
// the pins left of the tape. With the printable and right-hand pins, they
// must make up the 720 pins of the head.
func TestCatalogFillsTheHead(t *testing.T) {
	left := map[string]int{
		"dk-22214": 585, "dk-22210": 408, "dk-22225": 295, "dk-22223": 154, "dk-n55224": 130, "dk-22205": 12,
		"dk-11204": 555, "dk-11203": 555, "dk-11221": 442, "brother-29x42": 408, "dk-11201": 408,
		"dk-11208": 295, "brother-39x48": 289, "brother-52x29": 142, "brother-54x29": 59, "brother-60x86": 24,
		"brother-62x29": 12, "brother-62x60": 12, "brother-62x75": 12, "dk-11202": 12,
		"dk-11219": 513, "dk-11218": 442, "dk-11207": 51,
	}
	if len(left) != len(MediaIDs()) {
		t.Errorf("the catalog has %d rolls, the table %d", len(MediaIDs()), len(left))
	}
	for id, l := range left {
		m, ok := LookupMedia(id)
		if !ok {
			t.Errorf("%s is missing", id)
			continue
		}
		if sum := l + m.PrintableDots + m.RightMarginDots; sum != headPins {
			t.Errorf("%s: %d + %d + %d pins = %d, not %d", id, l, m.PrintableDots, m.RightMarginDots, sum, headPins)
		}
		if m.DieCut() != (m.PrintableLines > 0) || m.DieCut() == (m.FeedMarginDots > 0) {
			t.Errorf("%s: die-cut %v with %d printable lines and a %d-dot feed", id, m.DieCut(), m.PrintableLines, m.FeedMarginDots)
		}
	}
	// Printers saved before the catalog grew name these.
	for _, id := range []string{"dk-22210", "dk-22205", "dk-11201"} {
		if _, ok := LookupMedia(id); !ok {
			t.Errorf("%s, which printers may already name, is missing", id)
		}
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
}

func checkerPage(m Media, lines int) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, m.PrintableDots, lines))
	for y := 0; y < lines; y++ {
		for x := 0; x < m.PrintableDots; x++ {
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

// TestEncodeJobGoldens pins the byte stream for each roll and checks that
// decoding it gives back exactly the pages that went in.
func TestEncodeJobGoldens(t *testing.T) {
	for _, id := range MediaIDs() {
		m, _ := LookupMedia(id)
		lines := m.PrintableLines
		if lines == 0 {
			lines = 400
		}
		pages := []*image.Gray{checkerPage(m, lines), markedLabel(m.PrintableDots, lines)}
		stream, err := EncodeJob(m, pages, JobOptions{Cut: true})
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}

		path := filepath.Join("testdata", id+".bin")
		if *update {
			if err := os.WriteFile(path, stream, 0o644); err != nil {
				t.Fatal(err)
			}
		} else if want, err := os.ReadFile(path); err != nil || !bytes.Equal(stream, want) {
			t.Errorf("%s: stream differs from %s (%v)", id, path, err)
		}

		decoded, err := DecodeJob(stream)
		if err != nil {
			t.Fatalf("%s: decoding: %v", id, err)
		}
		if len(decoded) != 2 || decoded[0].Last || !decoded[1].Last {
			t.Fatalf("%s: decoded %d pages with wrong print commands", id, len(decoded))
		}
		wantType := byte(mediaContinuous)
		if m.DieCut() {
			wantType = mediaDieCut
		}
		for i, p := range decoded {
			if p.MediaType != wantType || p.WidthMM != byte(m.WidthMM) || p.LengthMM != byte(m.LengthMM) || !p.AutoCut {
				t.Errorf("%s page %d: print information %+v", id, i, p)
			}
			if got := PageImage(m, p); !bytes.Equal(got.Pix, pages[i].Pix) {
				t.Errorf("%s page %d: decoded bitmap differs from the input", id, i)
			}
		}

		// Pin 0 sits at the right of the tape: the page's leftmost column must
		// land on the last printable pin, just inside the right margin.
		lastPin := m.RightMarginDots + m.PrintableDots - 1
		if decoded[0].Lines[0][lastPin/8]&(0x80>>uint(lastPin%8)) == 0 {
			t.Errorf("%s: leftmost column is not on pin %d", id, lastPin)
		}
		for pin := 0; pin < m.RightMarginDots; pin++ {
			if decoded[0].Lines[0][pin/8]&(0x80>>uint(pin%8)) != 0 {
				t.Errorf("%s: pin %d beside the tape is set", id, pin)
			}
		}
	}
}

func TestEncodeJobRejectsMisshapenPages(t *testing.T) {
	dk11201, _ := LookupMedia("dk-11201")
	if _, err := EncodeJob(dk11201, nil, JobOptions{}); err == nil {
		t.Error("empty job accepted")
	}
	if _, err := EncodeJob(dk11201, []*image.Gray{markedLabel(300, 991)}, JobOptions{}); err == nil {
		t.Error("wrong width accepted")
	}
	if _, err := EncodeJob(dk11201, []*image.Gray{markedLabel(306, 500)}, JobOptions{}); err == nil {
		t.Error("wrong die-cut length accepted")
	}
}

func TestDecodeJobRejectsDamagedStreams(t *testing.T) {
	m, _ := LookupMedia("dk-22210")
	stream, err := EncodeJob(m, []*image.Gray{checkerPage(m, 300)}, JobOptions{Cut: true})
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
	m, _ := LookupMedia("dk-22210")
	page := checkerPage(m, 300)
	cut, _ := EncodeJob(m, []*image.Gray{page}, JobOptions{Cut: true})
	uncut, err := EncodeJob(m, []*image.Gray{page}, JobOptions{Cut: false})
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
	if decoded[0].AutoCut || !bytes.Equal(PageImage(m, decoded[0]).Pix, page.Pix) {
		t.Error("uncut job decoded wrongly")
	}
	if c, _ := DecodeJob(cut); !c[0].AutoCut {
		t.Error("cut job decoded without auto-cut")
	}
}
