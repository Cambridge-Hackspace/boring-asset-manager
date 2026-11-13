package render

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
)

// nfcMark reads the NFC mark's path data from the editor, which adds it to
// templates (web/src/lib/template.ts, NFC_MARK_D), so that what is tested
// here is what labels carry.
func nfcMark(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "lib", "template.ts"))
	if err != nil {
		t.Fatal(err)
	}
	decl := regexp.MustCompile(`(?s)export const NFC_MARK_D =(.*?);`).FindSubmatch(src)
	if decl == nil {
		t.Fatal("template.ts declares no NFC_MARK_D")
	}
	var d strings.Builder
	for _, part := range regexp.MustCompile(`'([^']*)'`).FindAllSubmatch(decl[1], -1) {
		d.Write(part[1])
	}
	return d.String()
}

// TestTheNFCMarkPrints draws the mark as the editor adds it, filled black
// in a square, and finds its dot and its three arcs inked, with the gaps
// between them blank.
func TestTheNFCMarkPrints(t *testing.T) {
	mark, err := json.Marshal(map[string]any{
		"type": "shape", "shape": "path", "name": "NFC mark", "x": 0, "y": 0, "width": 10, "height": 10,
		"d": nfcMark(t), "stroke": "none", "fill": "black",
	})
	if err != nil {
		t.Fatal(err)
	}
	tpl, err := labels.Parse([]byte(`{"name":"Tagged","units":"mm","width":10,"height":10,"dpi":300,"elements":[` + string(mark) + `]}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := tpl.Validate(); err != nil {
		t.Fatalf("the mark is not a valid shape: %v", err)
	}
	scene, err := Layout(tpl, labels.Data{AssetName: "Drill"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	img := scene.Raster(true)
	size := img.Bounds().Dx()
	// Along the middle row, as fractions of the box: the dot about 0.2,
	// the arcs' bands at 0.2 plus 0.24–0.33, 0.43–0.52 and 0.62–0.71.
	at := func(x float64) bool { return img.GrayAt(int(x*float64(size)), size/2).Y < 128 }
	for _, x := range []float64{0.2, 0.485, 0.675, 0.865} {
		if !at(x) {
			t.Errorf("nothing inked at %v of the way across", x)
		}
	}
	for _, x := range []float64{0.05, 0.36, 0.575, 0.765, 0.95} {
		if at(x) {
			t.Errorf("ink in a gap, %v of the way across", x)
		}
	}
	// Above and below the dot, beside the arcs' ends, stays blank.
	if img.GrayAt(int(0.2*float64(size)), size/10).Y < 128 {
		t.Error("ink above the dot")
	}
}
