package integration

import (
	"bytes"
	"encoding/json"
	"flag"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

// roundTripTemplate uses every element type, both alignments of text that
// wraps and shrinks, and lengths that are awkward in binary floating point,
// so storage that rounds, reorders or re-encodes anything shows up.
const roundTripTemplate = `{
  "id": "roundtrip", "name": "Round trip", "units": "in", "width": 2.44, "height": 1.15, "dpi": 300,
  "elements": [
    {"type": "generator", "generator_type": "qr", "error_correction": "Q", "x": 0.05, "y": 0.07, "width": 0.93, "height": 0.93,
     "content_binding": "{{asset.code.qr}}"},
    {"type": "generator", "generator_type": "code128", "x": 1.02, "y": 0.83, "width": 1.35, "height": 0.27,
     "content_binding": "{{asset.code.code128}}"},
    {"type": "text", "x": 1.02, "y": 0.07, "width": 1.37, "height": 0.33, "font_size": 13.5, "font_weight": "bold",
     "align": "right", "content_binding": "{{asset.name}}"},
    {"type": "text", "x": 1.02, "y": 0.41, "width": 1.37, "height": 0.39, "font_size": 9.3, "min_font_size": 6.1,
     "font_family": "mono", "wrap": true, "content_binding": "S/N {{field.serial}} · {{field.notes}}"},
    {"type": "static_image", "image_id": "IMAGE", "x": 0.05, "y": 1.01, "width": 0.11, "height": 0.11}
  ]
}`

// TestTemplateVectorsSurviveTheDatabase renders a template before it is
// saved and after it comes back out of the database, as SVG, and from the
// database as PDF. The outputs must match each other and the goldens, which
// were written on SQLite, on every database.
func TestTemplateVectorsSurviveTheDatabase(t *testing.T) {
	e := newEnv(t)
	c := e.admin("ada@hackspace.org")

	// A small checkerboard image for the static image element.
	img := image.NewGray(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			if (x+y)%2 == 0 {
				img.SetGray(x, y, color.Gray{Y: 255})
			}
		}
	}
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		t.Fatal(err)
	}
	var uploaded struct{ Image idOnly }
	c.mustJSON(http.MethodPost, "/images", fileForm("check.png", pngBuf.Bytes()), &uploaded, http.StatusCreated)
	doc := bytes.Replace([]byte(roundTripTemplate), []byte("IMAGE"), []byte(uploaded.Image.ID), 1)

	for _, f := range [][2]string{{"serial", "text"}, {"notes", "text"}} {
		c.mustJSON(http.MethodPost, "/fields", map[string]string{"key": f[0], "label": f[0], "validation_type": f[1]}, nil, http.StatusCreated)
	}
	asset := c.newAsset(map[string]any{
		"name": "Cordless drill, 18 V",
		"generator_keys": []map[string]string{
			{"generator_type": "qr", "assigned_code": "BAM-0042"},
			{"generator_type": "code128", "assigned_code": "DRL-0042"},
		},
		"field_values": map[string]string{"serial": "SN-77-0042", "notes": "Battery in the blue case; charger on shelf 3"},
	})

	before := c.mustJSON(http.MethodPost, "/templates/preview", map[string]any{"template": json.RawMessage(doc), "asset_id": asset}, nil, http.StatusOK).body
	c.mustJSON(http.MethodPost, "/templates", json.RawMessage(doc), nil, http.StatusCreated)
	label := "/assets/" + asset + "/label?template_id=roundtrip&format="
	after := c.mustJSON(http.MethodGet, label+"svg", nil, nil, http.StatusOK).body
	pdf := c.mustJSON(http.MethodGet, label+"pdf", nil, nil, http.StatusOK).body

	if !bytes.Equal(before, after) {
		t.Errorf("the SVG changed on its way through the database (%d bytes before, %d after)", len(before), len(after))
	}
	// The image's ID is random, and so appears nowhere in the output; check.
	if bytes.Contains(after, []byte(uploaded.Image.ID)) {
		t.Fatal("the output names the image's ID; the goldens cannot be stable")
	}
	golden(t, "roundtrip.svg", after)
	golden(t, "roundtrip.pdf", pdf)
}

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs from the golden (%d bytes, want %d)", name, len(got), len(want))
	}
}
