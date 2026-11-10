package appicon

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func decode(t *testing.T, data []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func same(a, b color.Color) bool {
	r1, g1, b1, a1 := a.RGBA()
	r2, g2, b2, a2 := b.RGBA()
	return r1 == r2 && g1 == g2 && b1 == b2 && a1 == a2
}

// TestDrawsEveryIconAtItsSize draws each icon the manifest lists from BAM!'s
// logo: square, at its size, the corners the background and the middle the
// logo.
func TestDrawsEveryIconAtItsSize(t *testing.T) {
	paper := color.RGBA{0xfa, 0xf7, 0xf0, 0xff}
	for _, icon := range Icons {
		data, err := Draw(BAM(), icon, paper)
		if err != nil {
			t.Fatal(err)
		}
		img := decode(t, data)
		if b := img.Bounds(); b.Dx() != icon.Size || b.Dy() != icon.Size {
			t.Errorf("%s: %v", icon.File, b)
		}
		if !same(img.At(0, 0), paper) || !same(img.At(icon.Size-1, icon.Size-1), paper) {
			t.Errorf("%s: corners %v %v", icon.File, img.At(0, 0), img.At(icon.Size-1, icon.Size-1))
		}
		if same(img.At(icon.Size/2, icon.Size/2), paper) {
			t.Errorf("%s: no logo in the middle", icon.File)
		}
	}
}

// TestMaskableKeepsToTheSafeZone keeps a maskable icon's logo inside the
// circle every launcher's mask leaves, and lets one shown whole fill more.
func TestMaskableKeepsToTheSafeZone(t *testing.T) {
	// A solid square logo, so its extent is easy to find.
	logo := image.NewRGBA(image.Rect(0, 0, 100, 100))
	for i := range logo.Pix {
		logo.Pix[i] = 0xff
	}
	black := color.RGBA{0, 0, 0, 0xff}
	extent := func(icon Icon) int {
		img := decode(t, must(Draw(logo, icon, black)))
		n := 0
		for x := 0; x < icon.Size; x++ {
			if !same(img.At(x, icon.Size/2), black) {
				n++
			}
		}
		return n
	}
	whole, _ := Named("icon-512.png")
	masked, _ := Named("maskable-512.png")
	w, m := extent(whole), extent(masked)
	// The square's corners lie within 40% of the side from the middle.
	if half := float64(m) / 2; half*1.4143 > 0.4*512 {
		t.Errorf("maskable logo %d px wide reaches outside the safe zone", m)
	}
	if w <= m || w > 512*9/10 {
		t.Errorf("whole logo %d px wide, maskable %d", w, m)
	}
}

func must(data []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return data
}

func TestParseColor(t *testing.T) {
	for s, want := range map[string]bool{"#faf7f0": true, "#FAF7F0": true, "#0b0b0a": true, "faf7f0": false, "#fff": false, "#gggggg": false, "#+12345": false, "": false} {
		if _, ok := ParseColor(s); ok != want {
			t.Errorf("%q: %v", s, ok)
		}
	}
	if c, _ := ParseColor("#d41920"); c != (color.RGBA{0xd4, 0x19, 0x20, 0xff}) {
		t.Errorf("%v", c)
	}
}

func TestNamed(t *testing.T) {
	if ic, ok := Named("apple-touch-icon.png"); !ok || ic.Size != 180 || ic.Maskable {
		t.Errorf("%+v %v", ic, ok)
	}
	if _, ok := Named("../icon-512.png"); ok {
		t.Error("found a name not listed")
	}
}
