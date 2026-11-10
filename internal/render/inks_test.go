package render

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
)

const rainbowID = "5d1f0e3c-8a7b-4c6d-9e2f-3a4b5c6d7e8f"

// testRainbow is a photograph's worth of color: every hue across, from
// full brightness at the top to dark at the bottom, clear in one corner.
func testRainbow() image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, 48, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 48; x++ {
			if x+y < 6 {
				continue
			}
			h := float64(x) / 48 * 6
			v := 1 - float64(y)/48
			f := h - math.Floor(h)
			at := func(k float64) uint8 { return uint8(math.Round(255 * v * k)) }
			var r, g, b uint8
			switch int(h) {
			case 0:
				r, g, b = at(1), at(f), 0
			case 1:
				r, g, b = at(1-f), at(1), 0
			case 2:
				r, g, b = 0, at(1), at(f)
			case 3:
				r, g, b = 0, at(1-f), at(1)
			case 4:
				r, g, b = at(f), 0, at(1)
			default:
				r, g, b = at(1), 0, at(1-f)
			}
			img.Set(x, y, color.NRGBA{r, g, b, 255})
		}
	}
	return img
}

// goldenImages are the images the golden templates place.
func goldenImages() memImages {
	return memImages{logoID: testLogo(), arrowID: testArrow(), rainbowID: testRainbow()}
}

// The "colors" golden: an image in color, level and turned; a code, text
// and shapes in each ink; and white text over red.
func init() {
	goldenTemplates["colors"] = `{"id":"colors","name":"Colors","units":"mm","width":60,"height":30,"dpi":300,"layering":"list","elements":[
		{"type":"static_image","x":1,"y":1,"width":20,"height":14,"image_id":"` + rainbowID + `"},
		{"type":"static_image","x":3,"y":17,"width":16,"height":10,"rotation":20,"image_id":"` + rainbowID + `"},
		{"type":"generator","generator_type":"qr","x":23,"y":1,"width":14,"height":14,"fill":"blue","content_binding":"{{asset.code.qr}}"},
		{"type":"text","x":38,"y":1,"width":21,"height":6,"font_size":12,"font_weight":"bold","fill":"red","content_binding":"RED"},
		{"type":"text","x":38,"y":8,"width":21,"height":6,"font_size":12,"font_weight":"bold","fill":"yellow","content_binding":"YELLOW"},
		{"type":"shape","shape":"rectangle","x":23,"y":17,"width":16,"height":12,"stroke":"orange","stroke_width":0.8,"fill":"green"},
		{"type":"shape","shape":"ellipse","x":41,"y":16,"width":18,"height":13,"stroke":"none","fill":"red"},
		{"type":"text","x":42,"y":19,"width":16,"height":7,"font_size":11,"align":"center","fill":"white","content_binding":"BAM"}]}`
	goldenPayloads["colors"] = []string{"BAM-QR-0042"}
}

// TestInkGoldens pins the "colors" golden drawn in each palette, as a
// printer or display with its inks prints it. Regenerate with -update.
func TestInkGoldens(t *testing.T) {
	s, err := Layout(mustTemplate(t, goldenTemplates["colors"]), testData, goldenImages())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range labels.Palettes {
		var buf bytes.Buffer
		if err := WriteInksPNG(&buf, s, p); err != nil {
			t.Fatal(err)
		}
		var again bytes.Buffer
		if err := WriteInksPNG(&again, s, p); err != nil || !bytes.Equal(buf.Bytes(), again.Bytes()) {
			t.Errorf("%s: drawing twice gave different dots", p)
		}
		path := filepath.Join("testdata", "colors."+string(p)+".png")
		if *update {
			if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(buf.Bytes(), want) {
			t.Errorf("%s differs from the golden file", path)
		}
		// Every ink of the palette is drawn, and nothing else.
		img, err := png.Decode(bytes.NewReader(buf.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		seen := map[color.RGBA]bool{}
		for _, v := range img.(*image.Paletted).Pix {
			seen[p.Colors()[v]] = true
		}
		for _, ink := range p.Inks() {
			if !seen[labels.InkColor(ink)] {
				t.Errorf("%s: nothing drawn in %s", p, ink)
			}
		}
	}
}

// TestBlackAndWhiteInksAreMono draws every golden in black and white dot
// for dot as Raster does in mono mode, which printers of black alone print.
func TestBlackAndWhiteInksAreMono(t *testing.T) {
	images := goldenImages()
	for name, doc := range goldenTemplates {
		s, err := Layout(mustTemplate(t, doc), testData, images)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		mono, inks := s.Raster(true), s.RasterInks(labels.PaletteBW)
		for i, v := range mono.Pix {
			if (v == 0) != (inks.Pix[i] == 0) {
				t.Errorf("%s: dot %d is %d in mono, ink %d in black and white", name, i, v, inks.Pix[i])
				break
			}
		}
	}
}

// TestMissingInksPrintBlack draws inks a palette lacks in black, where
// they would be drawn, and those it has in themselves.
func TestMissingInksPrintBlack(t *testing.T) {
	tpl := mustTemplate(t, `{"name":"Red","units":"mm","width":30,"height":10,"dpi":300,"layering":"list","elements":[
		{"type":"shape","shape":"rectangle","x":1,"y":1,"width":12,"height":8,"stroke":"none","fill":"red"},
		{"type":"text","x":15,"y":1,"width":14,"height":8,"font_size":14,"font_weight":"bold","fill":"yellow","content_binding":"Y"}]}`)
	s, err := Layout(tpl, testData, nil)
	if err != nil {
		t.Fatal(err)
	}
	count := func(img *image.Paletted, ink string) int {
		n := 0
		for _, v := range img.Pix {
			if img.Palette[v] == color.Color(labels.InkColor(ink)) {
				n++
			}
		}
		return n
	}
	red, yellow := s.RasterInks(labels.PaletteBWR), s.RasterInks(labels.PaletteBW)
	both := s.RasterInks(labels.PaletteBWRY)
	if count(red, labels.InkRed) == 0 || count(red, labels.InkRed) != count(both, labels.InkRed) {
		t.Errorf("red drawn red: %d in bwr, %d in bwry", count(red, labels.InkRed), count(both, labels.InkRed))
	}
	// In bwr, yellow is black; in bw, both are.
	if count(red, labels.InkBlack) != count(both, labels.InkYellow) {
		t.Errorf("yellow in black: %d, as yellow: %d", count(red, labels.InkBlack), count(both, labels.InkYellow))
	}
	if count(yellow, labels.InkBlack) != count(both, labels.InkRed)+count(both, labels.InkYellow) {
		t.Errorf("in black and white, %d black dots for %d in color", count(yellow, labels.InkBlack), count(both, labels.InkRed)+count(both, labels.InkYellow))
	}
	// In gray, as printers of black alone print it, the inks are black.
	gray := s.Raster(true)
	for i, v := range gray.Pix {
		if (v == 0) != (yellow.Pix[i] == 0) {
			t.Fatalf("dot %d: %d in mono, ink %d in black and white", i, v, yellow.Pix[i])
		}
	}
}

// TestImagesDitherIntoInks dithers an image into a palette the same way
// every time, to the palette's inks alone, and leaves an image already in
// them as it is.
func TestImagesDitherIntoInks(t *testing.T) {
	solid := image.NewNRGBA(image.Rect(0, 0, 10, 10))
	for i := 0; i < len(solid.Pix); i += 4 {
		c := labels.InkColor(labels.InkGreen)
		solid.Pix[i], solid.Pix[i+1], solid.Pix[i+2], solid.Pix[i+3] = c.R, c.G, c.B, 0xFF
	}
	const solidID = "0f0e0d0c-0b0a-4908-8706-050403020100"
	images := memImages{rainbowID: testRainbow(), solidID: solid}
	tpl := mustTemplate(t, `{"name":"Dither","units":"mm","width":40,"height":20,"dpi":300,"layering":"list","elements":[
		{"type":"static_image","x":0,"y":0,"width":20,"height":20,"image_id":"`+rainbowID+`"},
		{"type":"static_image","x":22,"y":2,"width":16,"height":16,"image_id":"`+solidID+`"}]}`)
	s, err := Layout(tpl, testData, images)
	if err != nil {
		t.Fatal(err)
	}
	a, b := s.RasterInks(labels.PaletteBWRYGB), s.RasterInks(labels.PaletteBWRYGB)
	if !bytes.Equal(a.Pix, b.Pix) {
		t.Fatal("dithered differently twice")
	}
	used := map[uint8]int{}
	for _, v := range a.Pix {
		used[v]++
	}
	if len(used) != 6 {
		t.Errorf("the rainbow dithers into all six inks, used %d", len(used))
	}
	// The solid green image, its box from (260, 24) to (449, 213) dots.
	green := uint8(4)
	for y := 40; y < 200; y++ {
		for x := 280; x < 430; x++ {
			if a.Pix[y*a.Stride+x] != green {
				t.Fatalf("dot (%d, %d) of solid green dithered to ink %d", x, y, a.Pix[y*a.Stride+x])
			}
		}
	}
}

// TestColorPNG writes a label in color inks as a color preview, and one
// in black and white as gray, as before.
func TestColorPNG(t *testing.T) {
	images := goldenImages()
	for name, wantColor := range map[string]bool{"colors": true, "shapes": false} {
		s, err := Layout(mustTemplate(t, goldenTemplates[name]), testData, images)
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		if err := WritePNG(&buf, s, false); err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(&buf)
		if err != nil {
			t.Fatal(err)
		}
		_, gray := img.(*image.Gray)
		if gray == wantColor {
			t.Errorf("%s: decoded as %T", name, img)
		}
	}
}
