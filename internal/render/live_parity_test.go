package render

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex/ir"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex/svg"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
)

// liveCase is one label the editor's live renderer (internal/live, compiled
// to WebAssembly) must draw dot for dot as the server does: its inputs as the
// browser hands them over, and a hash of its dots as the server draws them.
type liveCase struct {
	Name     string            `json:"name"`
	Template json.RawMessage   `json:"template"`
	Data     json.RawMessage   `json:"data"`
	Images   map[string]string `json:"images"` // id -> PNG, base64
	// Icons are what the live renderer's setIcons takes, for a template
	// that draws icons.
	Icons json.RawMessage `json:"icons,omitempty"`
	// Palette is the inks it is drawn in (docs/labels.md#inks); absent,
	// the fewest that hold the template's own.
	Palette labels.Palette `json:"palette,omitempty"`
	Width   int            `json:"width"`
	Height  int            `json:"height"`
	// SHA256 is of the dots, row by row: in black and white, one byte each
	// (0 black, 255 white); with Colors, three, the red, green and blue of
	// the ink each is drawn in.
	SHA256 string `json:"sha256"`
	Colors bool   `json:"colors,omitempty"`
}

// inkPalettes are the palettes the golden in color is drawn in, besides its
// own.
var inkPalettes = []labels.Palette{labels.PaletteBW, labels.PaletteBWR, labels.PaletteBWY, labels.PaletteBWRY, labels.PaletteBWRYGB}

// dotsHash is a liveCase's hash of the dots drawn in a palette.
func dotsHash(img *image.Paletted, p labels.Palette) (string, bool) {
	h := sha256.New()
	b := img.Bounds()
	colors := p != labels.PaletteBW
	inks := p.Colors()
	for y := 0; y < b.Dy(); y++ {
		for _, v := range img.Pix[y*img.Stride : y*img.Stride+b.Dx()] {
			c := inks[v]
			if colors {
				h.Write([]byte{c.R, c.G, c.B})
			} else {
				h.Write([]byte{c.R})
			}
		}
	}
	return hex.EncodeToString(h.Sum(nil)), colors
}

// TestLiveParityFixture keeps testdata/live_parity.json, which the web
// interface's tests render with the WebAssembly build and compare, up to
// date with every golden template: rewrite it with -update.
func TestLiveParityFixture(t *testing.T) {
	images := goldenImages()
	encoded := map[string]string{}
	for id, img := range images {
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			t.Fatal(err)
		}
		encoded[id] = base64.StdEncoding.EncodeToString(buf.Bytes())
	}
	data, err := json.Marshal(testData)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(goldenTemplates))
	for name := range goldenTemplates {
		names = append(names, name)
	}
	sort.Strings(names)

	var cases []liveCase
	for _, name := range names {
		// The editor draws the design, the right way round and level, of
		// a label that prints mirrored or turned.
		tpl := mustTemplate(t, goldenTemplates[name])
		tpl.PrintMirror, tpl.PrintRotation = "", 0
		s, err := Layout(tpl, testData, images)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		// The images the template places, by ID.
		used := map[string]string{}
		for id := range images {
			if bytes.Contains([]byte(goldenTemplates[name]), []byte(id)) {
				used[id] = encoded[id]
			}
		}
		// In its own inks, and the golden in color in others' too.
		palettes := []labels.Palette{""}
		if name == "colors" {
			palettes = append(palettes, inkPalettes...)
		}
		for _, p := range palettes {
			drawnIn := p
			if p == "" {
				drawnIn = labels.PaletteFor(tpl.Inks())
			}
			dots := s.RasterInks(drawnIn)
			sum, colors := dotsHash(dots, drawnIn)
			c := liveCase{Name: name, Template: json.RawMessage(goldenTemplates[name]), Data: data, Palette: p,
				Images: used, Width: dots.Bounds().Dx(), Height: dots.Bounds().Dy(), SHA256: sum, Colors: colors}
			if p != "" {
				c.Name = name + " in " + string(p)
			}
			if bytes.Contains([]byte(goldenTemplates[name]), []byte(`"type":"icon"`)) {
				c.Icons = liveIcons(t)
			}
			cases = append(cases, c)
		}
	}
	got, err := json.MarshalIndent(cases, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path := filepath.Join("testdata", "live_parity.json")
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n")), got) {
		t.Errorf("%s is out of date: run go test ./internal/render -run TestLiveParityFixture -update", path)
	}
}

// liveIcons are the icons memImages draws, as the live renderer takes
// them: those the golden templates use, the unknown icon, and the colors.
func liveIcons(t *testing.T) json.RawMessage {
	t.Helper()
	unknown := isolex.Unknown()
	in := map[string]any{"unknown": unknown, "colors": svg.IsolexPalette}
	var icons []*ir.Icon
	for _, name := range []string{"state-down", "switch-poe"} {
		icons = append(icons, testIcons[name])
	}
	in["icons"] = icons
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
