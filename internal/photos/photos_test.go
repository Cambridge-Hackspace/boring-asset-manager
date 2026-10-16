package photos

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

// marked is a w×h image, white but for a red block in its top-left corner.
func marked(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{255, 255, 255, 255}
			if x < w/4 && y < h/4 {
				c = color.RGBA{255, 0, 0, 255}
			}
			img.Set(x, y, c)
		}
	}
	return img
}

// withOrientation encodes a JPEG and adds an EXIF block saying how to turn
// it, with a GPS tag too, as phones write.
func withOrientation(t *testing.T, img image.Image, o uint16) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	// TIFF, little-endian: one IFD with orientation and a GPS pointer.
	tiff := []byte("II*\x00\x08\x00\x00\x00")
	tiff = binary.LittleEndian.AppendUint16(tiff, 2)
	entry := func(tag, typ uint16, count, value uint32) {
		tiff = binary.LittleEndian.AppendUint16(tiff, tag)
		tiff = binary.LittleEndian.AppendUint16(tiff, typ)
		tiff = binary.LittleEndian.AppendUint32(tiff, count)
		tiff = binary.LittleEndian.AppendUint32(tiff, value)
	}
	entry(0x0112, 3, 1, uint32(o))
	entry(0x8825, 4, 1, 0x1234)
	tiff = append(tiff, 0, 0, 0, 0)
	seg := append([]byte("Exif\x00\x00"), tiff...)
	app1 := []byte{0xFF, 0xE1}
	app1 = binary.BigEndian.AppendUint16(app1, uint16(len(seg)+2))
	app1 = append(app1, seg...)
	data := buf.Bytes()
	return append(append(append([]byte{}, data[:2]...), app1...), data[2:]...)
}

func redAt(t *testing.T, data []byte, x, y int) bool {
	t.Helper()
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := img.At(x, y).RGBA()
	return r > 0xC000 && g < 0x4000 && b < 0x4000
}

func TestPhotosAreTurnedUprightAndStripped(t *testing.T) {
	// Taken on its side: 400 wide, 200 high, shown turned a quarter right.
	data := withOrientation(t, marked(400, 200), 6)
	if orientation(data) != 6 {
		t.Fatalf("orientation %d", orientation(data))
	}
	p, err := Prepare(data)
	if err != nil {
		t.Fatal(err)
	}
	if p.ContentType != "image/jpeg" || p.Width != 200 || p.Height != 400 {
		t.Fatalf("prepared %s %d×%d", p.ContentType, p.Width, p.Height)
	}
	// The red corner, top-left as taken, is now top-right.
	if !redAt(t, p.Full, 190, 10) || redAt(t, p.Full, 10, 10) {
		t.Error("not turned a quarter right")
	}
	for _, kept := range [][]byte{p.Full, p.Thumb} {
		if bytes.Contains(kept, []byte("Exif")) {
			t.Error("EXIF kept")
		}
	}
	for o := 1; o <= 8; o++ {
		img := upright(marked(4, 2), o)
		if want := map[bool]int{true: 2, false: 4}[o >= 5]; img.Bounds().Dx() != want {
			t.Errorf("orientation %d: %v", o, img.Bounds())
		}
	}
}

func TestPhotosAreScaledWithThumbnails(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, marked(3000, 1500)); err != nil {
		t.Fatal(err)
	}
	p, err := Prepare(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if p.ContentType != "image/png" || p.Width != Largest || p.Height != Largest/2 {
		t.Errorf("full %s %d×%d", p.ContentType, p.Width, p.Height)
	}
	thumb, _, err := image.DecodeConfig(bytes.NewReader(p.Thumb))
	if err != nil || thumb.Width != Thumb || thumb.Height != Thumb/2 {
		t.Errorf("thumbnail %+v, %v", thumb, err)
	}
	// A small photo keeps its size.
	buf.Reset()
	_ = png.Encode(&buf, marked(100, 80))
	if p, err := Prepare(buf.Bytes()); err != nil || p.Width != 100 || p.Height != 80 {
		t.Errorf("small: %+v, %v", p, err)
	}
}

func TestPhotosRefuseWhatTheyCannotTake(t *testing.T) {
	var gif bytes.Buffer
	gif.WriteString("GIF89a")
	// A PNG header claiming to be far larger than it is.
	huge := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR")
	huge = binary.BigEndian.AppendUint32(huge, 20000)
	huge = binary.BigEndian.AppendUint32(huge, 20000)
	huge = append(huge, 8, 2, 0, 0, 0, 0, 0, 0, 0)
	var truncated bytes.Buffer
	_ = jpeg.Encode(&truncated, marked(64, 64), nil)
	for what, data := range map[string][]byte{
		"text":             []byte("hello"),
		"a GIF":            gif.Bytes(),
		"a giant":          huge,
		"a truncated JPEG": truncated.Bytes()[:truncated.Len()/2],
	} {
		if _, err := Prepare(data); !errors.Is(err, ErrUnsupported) {
			t.Errorf("%s: %v", what, err)
		}
	}
	// Broken EXIF is ignored, not fatal.
	if orientation([]byte{0xFF, 0xD8, 0xFF, 0xE1, 0x00, 0x08, 'E', 'x', 'i', 'f', 0, 0}) != 1 {
		t.Error("broken EXIF")
	}
}
