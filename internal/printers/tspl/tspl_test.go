package tspl

import (
	"bytes"
	"errors"
	"flag"
	"image"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/*.bin")

func TestModelsAndMedia(t *testing.T) {
	rollo, _ := LookupModel("Rollo X1038")
	if rollo.DPI != 203 || rollo.HeadDots != 832 {
		t.Errorf("Rollo X1038 = %+v", rollo)
	}
	shipping, _ := LookupMedia("4x6")
	if shipping.WidthMM != 101.6 || shipping.LengthMM != 152.4 || !shipping.DieCut() || rollo.Suits(shipping) != nil {
		t.Errorf("4x6 = %+v", shipping)
	}
	if wide := (Media{Name: "6 in", WidthMM: 152.4}); rollo.Suits(wide) == nil {
		t.Error("6 in labels accepted on a 4.1 in head")
	}
}

func TestFit(t *testing.T) {
	rollo, _ := LookupModel("Rollo X1038")
	generic300, _ := LookupModel("Generic TSPL 300 DPI")
	shipping, _ := LookupMedia("4x6")
	small, _ := LookupMedia("2x1")
	continuous, _ := LookupMedia("continuous-4")
	cases := []struct {
		name          string
		model         Model
		media         Media
		width, height float64
		dpi           int
		rotate, fits  bool
	}{
		{"portrait", rollo, shipping, 101.6, 152.4, 203, false, true},
		{"landscape rotates", rollo, shipping, 152.4, 101.6, 203, true, true},
		{"small", rollo, small, 50.8, 25.4, 203, false, true},
		{"within tolerance", rollo, small, 51, 25, 203, false, true},
		{"wrong size", rollo, small, 40, 25.4, 203, false, false},
		{"continuous", rollo, continuous, 101.6, 30, 203, false, true},
		{"continuous too long", rollo, continuous, 101.6, 1200, 203, false, false},
		{"300 DPI template on a 203 DPI printer", rollo, shipping, 101.6, 152.4, 300, false, false},
		{"300 DPI printer", generic300, shipping, 101.6, 152.4, 300, false, true},
	}
	for _, c := range cases {
		p, err := c.media.Fit(c.model, c.width, c.height, c.dpi)
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
	rollo, _ := LookupModel("Rollo X1038")
	page := Prepare(rollo, markedLabel(812, 1219), Placement{})
	if page.Bounds() != image.Rect(0, 0, 812, 1219) || darkBounds(page) != image.Rect(20, 20, 30, 30) {
		t.Errorf("page %v, mark at %v", page.Bounds(), darkBounds(page))
	}
	// Counterclockwise: the top-left corner ends at the bottom-left.
	page = Prepare(rollo, markedLabel(1219, 812), Placement{Rotate: true})
	if page.Bounds() != image.Rect(0, 0, 812, 1219) || darkBounds(page) != image.Rect(20, 1219-30, 30, 1219-20) {
		t.Errorf("rotated page %v, mark at %v", page.Bounds(), darkBounds(page))
	}
	// Wider than the head: the center is kept.
	if w := Prepare(rollo, markedLabel(900, 100), Placement{}).Bounds().Dx(); w != 832 {
		t.Errorf("page %d dots wide, want the head's 832", w)
	}
}

func TestEncodeJobFollowsManual(t *testing.T) {
	// TSPL/TSPL2 Programming Manual: setup (SIZE, GAP, DIRECTION,
	// REFERENCE, DENSITY), then per label CLS, BITMAP x,y,width in bytes,
	// height,mode then the data, and PRINT sets,copies. In bitmap data a 0
	// bit prints a dot.
	rollo, _ := LookupModel("Rollo X1038")
	small, _ := LookupMedia("2x1")
	page := image.NewGray(image.Rect(0, 0, 10, 2)) // first dot printed, rest white
	for i := range page.Pix {
		page.Pix[i] = 0xFF
	}
	page.Pix[0], page.Pix[page.Stride+9] = 0, 0
	other := image.NewGray(image.Rect(0, 0, 10, 2)) // all printed

	density := 12
	got, err := EncodeJob(rollo, small, []*image.Gray{page, page, other}, JobOptions{Density: &density, Rotate180: true})
	if err != nil {
		t.Fatal(err)
	}
	// 2 in at 203 DPI is 406 dots, so a 10-dot page sits at x = 198.
	want := "SIZE 50.8 mm,25.4 mm\r\nGAP 3.2 mm,0 mm\r\nDIRECTION 1,0\r\nREFERENCE 0,0\r\nDENSITY 12\r\n" +
		"CLS\r\nBITMAP 198,0,2,2,0,\x7F\xFF\xFF\xBF\r\nPRINT 1,2\r\n" +
		"CLS\r\nBITMAP 198,0,2,2,0,\x00\x3F\x00\x3F\r\nPRINT 1,1\r\n"
	if string(got) != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestEncodeJobGoldens(t *testing.T) {
	for _, c := range []struct {
		name, model, media string
		lines              int
	}{
		{"rollo-x1038-4x6", "Rollo X1038", "4x6", 300},
		{"generic-300-continuous-2", "Generic TSPL 300 DPI", "continuous-2", 400},
	} {
		t.Run(c.name, func(t *testing.T) {
			mo, _ := LookupModel(c.model)
			m, _ := LookupMedia(c.media)
			w := int(m.WidthMM / 25.4 * float64(mo.DPI))
			pages := []*image.Gray{markedLabel(w, c.lines), markedLabel(w, c.lines)}
			pages[1].Pix[len(pages[1].Pix)-1] = 0 // a different second label
			stream, err := EncodeJob(mo, m, pages, JobOptions{})
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

			job, err := DecodeJob(stream)
			if err != nil {
				t.Fatal(err)
			}
			if len(job.Labels) != 2 || job.Labels[0].Copies != 1 {
				t.Fatalf("decoded %d labels", len(job.Labels))
			}
			for i, l := range job.Labels {
				if !sameImage(l.Image, pages[i]) {
					t.Errorf("label %d differs from its page", i)
				}
			}
		})
	}
}

// sameImage compares a decoded bitmap, padded to whole bytes, with a page.
func sameImage(decoded, page *image.Gray) bool {
	if decoded.Rect.Dy() != page.Rect.Dy() || decoded.Rect.Dx() < page.Rect.Dx() {
		return false
	}
	for y := 0; y < page.Rect.Dy(); y++ {
		for x := 0; x < decoded.Rect.Dx(); x++ {
			want := x >= page.Rect.Dx() || page.Pix[y*page.Stride+x] >= 0x80
			if (decoded.Pix[y*decoded.Stride+x] >= 0x80) != want {
				return false
			}
		}
	}
	return true
}

func TestEncodeJobRejectsBadInput(t *testing.T) {
	rollo, _ := LookupModel("Rollo X1038")
	shipping, _ := LookupMedia("4x6")
	if _, err := EncodeJob(rollo, shipping, nil, JobOptions{}); err == nil {
		t.Error("empty job accepted")
	}
	if _, err := EncodeJob(rollo, shipping, []*image.Gray{markedLabel(900, 40)}, JobOptions{}); err == nil {
		t.Error("page wider than the head accepted")
	}
	tooDark := 16
	if _, err := EncodeJob(rollo, shipping, []*image.Gray{markedLabel(812, 40)}, JobOptions{Density: &tooDark}); err == nil {
		t.Error("density 16 accepted")
	}
}

func TestDecodeJobRejectsDamagedPrograms(t *testing.T) {
	rollo, _ := LookupModel("Rollo X1038")
	small, _ := LookupMedia("2x1")
	program, err := EncodeJob(rollo, small, []*image.Gray{markedLabel(406, 203)}, JobOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for what, damaged := range map[string][]byte{
		"truncated bitmap": program[:len(program)-40],
		"no print":         program[:len(program)-len("PRINT 1,1\r\n")],
		"trailing bytes":   append(append([]byte{}, program...), 'X'),
	} {
		if _, err := DecodeJob(damaged); err == nil {
			t.Errorf("%s program accepted", what)
		}
	}
}
