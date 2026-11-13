package brady

import (
	"bytes"
	"encoding/binary"
	"image"
	"math/rand"
	"strings"
	"testing"
	"time"
)

func TestLZ4BlocksRoundTrip(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	random := make([]byte, 5000)
	r.Read(random)
	runs := bytes.Repeat([]byte{0xFF}, 70000) // a match longer than 15 + 255
	mixed := append(bytes.Repeat([]byte("abcdefgh"), 300), random[:300]...)
	for name, src := range map[string][]byte{
		"empty": {}, "short": []byte("hello"), "twelve": []byte("aaaaaaaaaaaa"), "random": random, "runs": runs, "mixed": mixed,
		"far": append(append(append([]byte{}, random[:100]...), make([]byte, 70000)...), random[:100]...),
	} {
		packed := compressBlock(src)
		got, err := decompressBlock(packed)
		if err != nil || !bytes.Equal(got, src) {
			t.Errorf("%s: %v, %d bytes back of %d", name, err, len(got), len(src))
		}
		if name == "runs" && len(packed) > 400 {
			t.Errorf("runs packed to %d bytes", len(packed))
		}
	}
	// A block written by hand from the format: "abc", a match of 10 at
	// offset 3, then 5 literals.
	known := []byte{0x36, 'a', 'b', 'c', 0x03, 0x00, 0x50, 'b', 'c', 'a', 'b', 'c'}
	if got, err := decompressBlock(known); err != nil || string(got) != "abcabcabcabcabcabc" {
		t.Errorf("known block: %q %v", got, err)
	}
	for _, bad := range [][]byte{{}, {0xF0}, {0x10}, {0x14, 'a', 0x05, 0x00}, {0x10, 'a', 0x00}} {
		if _, err := decompressBlock(bad); err == nil {
			t.Errorf("%x decompressed", bad)
		}
	}
}

func page(w, h int) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 0xFF
	}
	// A black square at the top left, and a dot at the bottom right.
	for y := 0; y < min(10, h); y++ {
		for x := 0; x < min(10, w); x++ {
			img.Pix[y*img.Stride+x] = 0
		}
	}
	img.Pix[(h-1)*img.Stride+w-1] = 0x40
	return img
}

func TestAJobIsAHeaderThenAPacketPerPage(t *testing.T) {
	when := time.Date(2026, 11, 13, 9, 30, 5, 0, time.Local)
	now = func() time.Time { return when }
	defer func() { now = time.Now }()
	mo, _ := LookupModel("M611")
	// A 1.5 × 0.75 in die-cut label, at 300 DPI.
	media := Media{ID: "m6", Name: "M6 1.5 × 0.75 in", WidthMM: 38.1, LengthMM: 19.05}
	a, b := page(450, 225), page(450, 225)
	b.Pix[100] = 0
	job, err := EncodeJob(mo, media, []*image.Gray{a, a, b}, JobOptions{Cut: CutEnd, Part: "M6-11-427"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(job, []byte{0x7F, 0x42, 0xEE, 0x41}) {
		t.Fatalf("starts %x", job[:4])
	}
	n := binary.LittleEndian.Uint32(job[16:])
	if !strings.HasPrefix(string(job[20:20+n]), `{"JobID":"`) {
		t.Fatalf("header %s", job[20:20+n])
	}
	got, err := DecodeJob(job)
	if err != nil {
		t.Fatal(err)
	}
	h := got.Header
	if h.NumberOfPages != 3 || h.SubstratePart != "M6-11-427" || h.PostPrint != "Cut;0" || h.JobTime != "20261113093005" ||
		h.JobType != "Print" || len(h.JobID) != 32 || len(h.JobClientID) != 32 {
		t.Fatalf("header %+v", h)
	}
	for i, p := range got.Pages {
		if p.JobID != h.JobID || p.PageNumber != i || p.LabelWidth != 1500 || p.LabelHeight != 750 || p.PrintFileName != "Page"+string(rune('1'+i))+".prn" {
			t.Errorf("page %d: %+v", i, p)
		}
		cut := len(p.Pages.PostPrintOperations) == 1 && p.Pages.PostPrintOperations[0]["Cut"] == "Shear"
		if cut != (i == 2) {
			t.Errorf("page %d cut %v", i, p.Pages.PostPrintOperations)
		}
	}
	// Dark is below 50% gray; the rest is paper.
	for i, img := range got.Images {
		want := []*image.Gray{a, a, b}[i]
		if img.Rect != want.Rect {
			t.Fatalf("page %d is %v", i, img.Rect)
		}
		for k := range want.Pix {
			if (img.Pix[k] == 0) != (want.Pix[k] < 0x80) {
				t.Fatalf("page %d dot %d", i, k)
			}
		}
	}
	// The same pages sent later are another job.
	when = when.Add(time.Second)
	again, _ := EncodeJob(mo, media, []*image.Gray{a, a, b}, JobOptions{Cut: CutEnd, Part: "M6-11-427"})
	if j, _ := DecodeJob(again); j.Header.JobID == h.JobID {
		t.Error("the same job ID twice")
	}
}

func TestBMPsAreTopDownWithBlackFirst(t *testing.T) {
	b := writeBMP(page(13, 3), 300)
	le := binary.LittleEndian
	if string(b[:2]) != "BM" || int(le.Uint32(b[2:])) != len(b) || le.Uint32(b[10:]) != 62 || int32(le.Uint32(b[22:])) != -3 ||
		le.Uint16(b[28:]) != 1 || le.Uint32(b[38:]) != 11811 || len(b) != 62+4*3 {
		t.Fatalf("BMP % x", b[:62])
	}
	// Row 0: ten black dots, then three white, padded with zeros.
	if !bytes.Equal(b[62:66], []byte{0x00, 0x38, 0, 0}) || !bytes.Equal(b[54:62], []byte{0, 0, 0, 0, 0xFF, 0xFF, 0xFF, 0}) {
		t.Fatalf("row 0 % x, palette % x", b[62:66], b[54:62])
	}
}

func TestCutsAndContinuousLabels(t *testing.T) {
	mo, _ := LookupModel("M611")
	tape := Media{ID: "c", Name: "1 in continuous", WidthMM: 25.4}
	for cut, want := range map[Cut]string{CutEach: "Cut;1", CutPrinter: "SetByPrinter"} {
		job, err := EncodeJob(mo, tape, []*image.Gray{page(300, 600), page(300, 900)}, JobOptions{Cut: cut})
		if err != nil {
			t.Fatal(err)
		}
		got, _ := DecodeJob(job)
		if got.Header.PostPrint != want || got.Header.SubstratePart != "" {
			t.Errorf("%s: %+v", cut, got.Header)
		}
		// A continuous label is as long as its image.
		if p := got.Pages[1]; p.LabelWidth != 1000 || p.LabelHeight != 3000 || len(p.Pages.PostPrintOperations) != 1 {
			t.Errorf("%s: %+v", cut, p)
		}
		if cut == CutPrinter && got.Pages[0].Pages.PostPrintOperations[0]["SetByPrinter"] != "SetByPrinter" {
			t.Errorf("printer's cut: %v", got.Pages[0].Pages.PostPrintOperations)
		}
	}
	if _, err := EncodeJob(mo, tape, nil, JobOptions{Cut: CutEach}); err == nil {
		t.Error("a job of no pages")
	}
}

func TestModelsTakeTheirCartridges(t *testing.T) {
	mo, ok := LookupModel("M611")
	if !ok || mo.HeadDots() != 600 || strings.Join(Models(), ",") != "M611" {
		t.Fatalf("%+v", mo)
	}
	for _, m := range []Media{{Name: "wide", WidthMM: 57}, {Name: "short", WidthMM: 25, LengthMM: 8}, {Name: "long", WidthMM: 25, LengthMM: 1100}} {
		if mo.Suits(m) == nil {
			t.Errorf("%s suits", m.Name)
		}
	}
	if err := mo.Suits(Media{Name: "2 in", WidthMM: 50.8, LengthMM: 25.4}); err != nil {
		t.Error(err)
	}
	for _, o := range []JobOptions{{Cut: "sometimes"}, {Cut: CutEach, Part: "M6;11"}, {Cut: CutEach, Part: strings.Repeat("A", 65)}} {
		if o.Check() == nil {
			t.Errorf("%+v passed", o)
		}
	}
	if err := (JobOptions{Cut: CutEach, Part: "M6C-1000-595-WT"}).Check(); err != nil {
		t.Error(err)
	}
	// A wider label is cut to the head, around its center; a turned one
	// runs its height across.
	wide := page(700, 50)
	if p := Prepare(mo, wide, Placement{}); p.Rect.Dx() != 600 || p.Pix[0] != 0xFF {
		t.Errorf("cut to %v", p.Rect)
	}
	if p := Prepare(mo, page(100, 40), Placement{Rotate: true}); p.Rect.Dx() != 40 || p.Rect.Dy() != 100 || p.Pix[99*p.Stride] != 0 {
		t.Errorf("turned to %v", p.Rect)
	}
}
