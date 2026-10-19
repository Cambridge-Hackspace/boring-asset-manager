package render

import (
	"bytes"
	"compress/zlib"
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
)

var update = flag.Bool("update", false, "rewrite golden files in testdata/")

type memImages map[string]image.Image

func (m memImages) LoadImage(id string) (image.Image, error) {
	img, ok := m[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return img, nil
}

const logoID = "7c9e6679-7425-40de-944b-e07fc1f90ae7"

// testLogo is a small RGBA image with transparency: a dark ring on a clear
// background.
func testLogo() image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, 48, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 48; x++ {
			d := math.Hypot(float64(x)-23.5, float64(y)-15.5)
			if d > 9 && d < 15 {
				img.Set(x, y, color.NRGBA{R: 30, G: 60, B: 200, A: 255})
			} else if d <= 9 {
				img.Set(x, y, color.NRGBA{R: 200, G: 200, B: 200, A: 128})
			}
		}
	}
	return img
}

const arrowID = "0b3c5d7e-1f2a-4b6c-8d9e-0a1b2c3d4e5f"

// testArrow is an arrow pointing right, on a clear background, so that
// which way it is turned shows.
func testArrow() image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, 56, 40))
	for y := 0; y < 40; y++ {
		for x := 0; x < 56; x++ {
			shaft := x < 34 && y >= 14 && y < 26
			head := x >= 34 && math.Abs(float64(y)-19.5) < float64(56-x)*20/22
			if shaft || head {
				img.Set(x, y, color.NRGBA{R: 20, G: 20, B: 20, A: 255})
			}
		}
	}
	return img
}

var testData = labels.Data{
	AssetID:   "2f1e3c4d-5b6a-4789-8abc-def012345678",
	AssetName: "Bench Drill Press",
	BaseURL:   "https://bam.example/",
	Codes:     map[string]string{"qr": "BAM-QR-0042", "micro_qr": "BAM-0042", "code128": "TOOL-0042"},
	Fields:    map[string]string{"location": "Workshop shelf B3", "notes": "Keep the chuck key on the magnetic strip next to the machine at all times"},
}

func mustTemplate(t *testing.T, doc string) *labels.Template {
	t.Helper()
	tpl, err := labels.Parse([]byte(doc))
	if err != nil {
		t.Fatalf("template: %v", err)
	}
	return tpl
}

// goldenTemplates cover each element type, both units, two DPIs, alignment,
// wrapping, shrinking and truncation.
var goldenTemplates = map[string]string{
	"qr_label": `{"id":"qr_label","name":"QR","units":"mm","width":90,"height":29,"dpi":300,"elements":[
		{"type":"generator","generator_type":"qr","error_correction":"M","x":3,"y":3,"width":23,"height":23,
		 "content_binding":"{{base_url}}/a/{{asset.id}}"},
		{"type":"text","x":28,"y":4,"width":58,"height":7,"font_size":14,"font_weight":"bold","content_binding":"{{asset.name}}"},
		{"type":"text","x":28,"y":12,"width":58,"height":5,"font_size":9,"font_family":"mono","content_binding":"{{field.location}}"},
		{"type":"text","x":28,"y":18,"width":58,"height":8,"font_size":9,"wrap":true,"content_binding":"{{field.notes}}"}]}`,
	"micro_code128": `{"id":"micro_code128","name":"Micro","units":"mm","width":62,"height":29,"dpi":300,"elements":[
		{"type":"generator","generator_type":"micro_qr","x":2,"y":2,"width":14,"height":14},
		{"type":"text","x":18,"y":2,"width":42,"height":14,"font_size":20,"align":"center","content_binding":"{{asset.code.micro_qr}}"},
		{"type":"generator","generator_type":"code128","x":2,"y":18,"width":58,"height":9}]}`,
	"image_inches": `{"id":"image_inches","name":"Inches","units":"in","width":2,"height":1,"dpi":203,"elements":[
		{"type":"static_image","x":0.05,"y":0.05,"width":0.6,"height":0.9,"image_id":"` + logoID + `"},
		{"type":"text","x":0.7,"y":0.05,"width":1.25,"height":0.4,"font_size":30,"min_font_size":8,"align":"right","content_binding":"{{asset.name}}"},
		{"type":"text","x":0.7,"y":0.5,"width":1.25,"height":0.2,"font_size":8,"min_font_size":7,"content_binding":"{{field.notes}}"}]}`,
	// Drawn in list order: the logo over text on the left, text over the
	// logo on the right.
	"layers": `{"id":"layers","name":"Layers","units":"mm","width":50,"height":20,"dpi":203,"layering":"list","elements":[
		{"type":"text","x":1,"y":2,"width":23,"height":16,"font_size":40,"font_weight":"bold","content_binding":"BAM"},
		{"type":"static_image","x":4,"y":2,"width":18,"height":16,"image_id":"` + logoID + `"},
		{"type":"static_image","x":27,"y":2,"width":22,"height":16,"image_id":"` + logoID + `"},
		{"type":"text","x":26,"y":2,"width":23,"height":16,"font_size":40,"font_weight":"bold","content_binding":"BAM"}]}`,
	// Text, an image, a QR Code and a Code 128 code, one to a row, turned
	// 0°, 90°, 30° and 45°, one to a column.
	"turned": `{"id":"turned","name":"Turned","units":"mm","width":120,"height":104,"dpi":300,"layering":"list","elements":[
		{"type":"text","x":6.0,"y":10.0,"width":18,"height":6,"font_size":12,"font_weight":"bold","align":"center","content_binding":"BAM 42"},
		{"type":"static_image","x":8.0,"y":34.0,"width":14,"height":10,"image_id":"` + arrowID + `"},
		{"type":"generator","x":7.0,"y":57.0,"width":16,"height":16,"generator_type":"qr","content_binding":"{{asset.code.qr}}"},
		{"type":"generator","x":3.0,"y":87.0,"width":24,"height":8,"generator_type":"code128","content_binding":"1234"},
		{"type":"text","x":36.0,"y":10.0,"width":18,"height":6,"rotation":90,"font_size":12,"font_weight":"bold","align":"center","content_binding":"BAM 42"},
		{"type":"static_image","x":38.0,"y":34.0,"width":14,"height":10,"rotation":90,"image_id":"` + arrowID + `"},
		{"type":"generator","x":37.0,"y":57.0,"width":16,"height":16,"rotation":90,"generator_type":"qr","content_binding":"{{asset.code.qr}}"},
		{"type":"generator","x":33.0,"y":87.0,"width":24,"height":8,"rotation":90,"generator_type":"code128","content_binding":"1234"},
		{"type":"text","x":66.0,"y":10.0,"width":18,"height":6,"rotation":30,"font_size":12,"font_weight":"bold","align":"center","content_binding":"BAM 42"},
		{"type":"static_image","x":68.0,"y":34.0,"width":14,"height":10,"rotation":30,"image_id":"` + arrowID + `"},
		{"type":"generator","x":67.0,"y":57.0,"width":16,"height":16,"rotation":30,"generator_type":"qr","content_binding":"{{asset.code.qr}}"},
		{"type":"generator","x":63.0,"y":87.0,"width":24,"height":8,"rotation":30,"generator_type":"code128","content_binding":"1234"},
		{"type":"text","x":96.0,"y":10.0,"width":18,"height":6,"rotation":45,"font_size":12,"font_weight":"bold","align":"center","content_binding":"BAM 42"},
		{"type":"static_image","x":98.0,"y":34.0,"width":14,"height":10,"rotation":45,"image_id":"` + arrowID + `"},
		{"type":"generator","x":97.0,"y":57.0,"width":16,"height":16,"rotation":45,"generator_type":"qr","content_binding":"{{asset.code.qr}}"},
		{"type":"generator","x":93.0,"y":87.0,"width":24,"height":8,"rotation":45,"generator_type":"code128","content_binding":"1234"}]}`,
	// A QR Code in the top left corner of its box, with no margin on those
	// two sides, and a Code 128 code with margins of 2 and 3 mm.
	"margins": `{"id":"margins","name":"Margins","units":"mm","width":50,"height":25,"dpi":300,"layering":"list","elements":[
		{"type":"generator","generator_type":"qr","x":2,"y":2,"width":21,"height":21,"content_binding":"{{asset.code.qr}}","margin":{"top":0,"left":0},"align":"left","vertical_align":"top"},
		{"type":"generator","generator_type":"code128","x":25,"y":2,"width":24,"height":21,"content_binding":"1234","margin":{"top":3,"bottom":3,"left":2,"right":2},"margin_unit":"length"}]}`,
	// A QR Code at 45°, which phones still read.
	"qr45": `{"id":"qr45","name":"QR 45","units":"mm","width":40,"height":40,"dpi":300,"layering":"list","elements":[
		{"type":"generator","generator_type":"qr","x":7.5,"y":7.5,"width":25,"height":25,"rotation":45,"content_binding":"{{base_url}}/a/{{asset.id}}"}]}`,
}

// goldenPayloads lists what each golden's barcodes must decode to; see
// tools/goldens/verify_renders.py.
var goldenPayloads = map[string][]string{
	"qr_label":      {"https://bam.example/a/2f1e3c4d-5b6a-4789-8abc-def012345678"},
	"micro_code128": {"BAM-0042", "TOOL-0042"},
	"image_inches":  {},
	"layers":        {},
	"turned":        {"BAM-QR-0042", "BAM-QR-0042", "BAM-QR-0042", "BAM-QR-0042", "1234", "1234"},
	"qr45":          {"https://bam.example/a/2f1e3c4d-5b6a-4789-8abc-def012345678"},
	"margins":       {"BAM-QR-0042", "1234"},
}

func renderAll(t *testing.T, s *Scene) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	var buf bytes.Buffer
	if err := WriteSVG(&buf, s); err != nil {
		t.Fatal(err)
	}
	out["svg"] = append([]byte(nil), buf.Bytes()...)
	buf.Reset()
	if err := WritePNG(&buf, s, false); err != nil {
		t.Fatal(err)
	}
	out["png"] = append([]byte(nil), buf.Bytes()...)
	buf.Reset()
	if err := WritePNG(&buf, s, true); err != nil {
		t.Fatal(err)
	}
	out["mono.png"] = append([]byte(nil), buf.Bytes()...)
	buf.Reset()
	if err := WritePDF(&buf, s); err != nil {
		t.Fatal(err)
	}
	out["pdf"] = append([]byte(nil), buf.Bytes()...)
	return out
}

// TestGoldens pins every backend's output byte for byte. Regenerate with
// go test ./internal/render -update, inspect the files, and run
// tools/goldens/verify_renders.py to confirm the barcodes still scan.
func TestGoldens(t *testing.T) {
	images := memImages{logoID: testLogo(), arrowID: testArrow()}
	type manifestEntry struct {
		File     string   `json:"file"`
		Payloads []string `json:"payloads"`
	}
	var manifest []manifestEntry

	for _, name := range []string{"image_inches", "layers", "margins", "micro_code128", "qr45", "qr_label", "turned"} {
		s, err := Layout(mustTemplate(t, goldenTemplates[name]), testData, images)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		outputs := renderAll(t, s)
		again := renderAll(t, s)
		for ext, data := range outputs {
			if !bytes.Equal(data, again[ext]) {
				t.Errorf("%s.%s: rendering twice gave different bytes", name, ext)
			}
			path := filepath.Join("testdata", name+"."+ext)
			if *update {
				if err := os.WriteFile(path, data, 0o644); err != nil {
					t.Fatal(err)
				}
				continue
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(data, want) {
				t.Errorf("%s differs from the golden file", path)
			}
		}
		for _, ext := range []string{"png", "mono.png"} {
			manifest = append(manifest, manifestEntry{File: name + "." + ext, Payloads: goldenPayloads[name]})
		}
	}

	if *update {
		raw, _ := json.MarshalIndent(manifest, "", " ")
		if err := os.WriteFile(filepath.Join("testdata", "barcodes.json"), append(raw, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMultiPagePDF(t *testing.T) {
	var scenes []*Scene
	for _, name := range []string{"qr_label", "micro_code128"} {
		s, err := Layout(mustTemplate(t, goldenTemplates[name]), testData, nil)
		if err != nil {
			t.Fatal(err)
		}
		scenes = append(scenes, s)
	}
	var buf bytes.Buffer
	if err := WritePDF(&buf, scenes...); err != nil {
		t.Fatal(err)
	}
	pdf := buf.Bytes()
	if got := strings.Count(string(pdf), "/Type /Page "); got != 2 {
		t.Errorf("found %d pages, want 2", got)
	}
	checkXref(t, pdf)

	// 90 mm × 29 mm at 300 DPI is 1063 × 343 dots, so the page is 255.12 × 82.32 pt.
	if !strings.Contains(string(pdf), "/MediaBox [0 0 255.12 82.32]") {
		t.Error("first page is not sized to the label")
	}
}

// checkXref verifies every cross-reference offset points at its object.
func checkXref(t *testing.T, pdf []byte) {
	t.Helper()
	m := regexp.MustCompile(`startxref\n(\d+)\n%%EOF\n$`).FindSubmatch(pdf)
	if m == nil {
		t.Fatal("missing startxref trailer")
	}
	xref, _ := strconv.Atoi(string(m[1]))
	lines := strings.Split(string(pdf[xref:]), "\n")
	if lines[0] != "xref" {
		t.Fatalf("startxref does not point at the xref table")
	}
	var count int
	fmt.Sscanf(lines[1], "0 %d", &count)
	for id := 1; id < count; id++ {
		off, _ := strconv.Atoi(lines[2+id][:10])
		if want := fmt.Sprintf("%d 0 obj\n", id); !bytes.HasPrefix(pdf[off:], []byte(want)) {
			t.Errorf("xref entry for object %d points at %q", id, pdf[off:off+12])
		}
	}
}

func TestSVGIsWellFormed(t *testing.T) {
	s, err := Layout(mustTemplate(t, goldenTemplates["image_inches"]), testData, memImages{logoID: testLogo()})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := WriteSVG(&buf, s); err != nil {
		t.Fatal(err)
	}
	doc := buf.String()
	dec := xml.NewDecoder(strings.NewReader(doc))
	for {
		if _, err := dec.Token(); err == io.EOF {
			break
		} else if err != nil {
			t.Fatalf("invalid XML: %v", err)
		}
	}
	if !strings.Contains(doc, `width="2in" height="1in" viewBox="0 0 406 203"`) {
		t.Errorf("unexpected root element: %.200s", doc)
	}
}

func TestModulesSnapToWholeDots(t *testing.T) {
	// 20 mm at 300 DPI is 236 dots. A 21-module QR Code with a 4-module quiet
	// zone on each side spans 29 modules: 8 dots each.
	tpl := mustTemplate(t, `{"id":"t","name":"T","units":"mm","width":30,"height":30,"dpi":300,"elements":[
		{"type":"generator","generator_type":"qr","x":5,"y":5,"width":20,"height":20,"content_binding":"HELLO"}]}`)
	s, err := Layout(tpl, labels.Data{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	minX, minY, maxX, maxY := math.MaxInt, math.MaxInt, 0, 0
	for _, r := range s.Items[0].Rects {
		if r.H != 8 || r.W%8 != 0 {
			t.Fatalf("rect %+v is not a whole number of 8-dot modules", r)
		}
		minX, minY = min(minX, r.X), min(minY, r.Y)
		maxX, maxY = max(maxX, r.X+r.W), max(maxY, r.Y+r.H)
	}
	if maxX-minX != 168 || maxY-minY != 168 {
		t.Errorf("symbol spans %d×%d dots, want 168×168", maxX-minX, maxY-minY)
	}
	// The box starts at dot 59 and is 236 wide; the symbol is centered in it.
	if minX != 59+(236-168)/2 || minY != minX {
		t.Errorf("symbol starts at (%d,%d), want centered at %d", minX, minY, 59+(236-168)/2)
	}
}

func TestLayoutErrorsNameTheElement(t *testing.T) {
	cases := []struct {
		name, element, want string
	}{
		{"missing code", `{"type":"generator","generator_type":"micro_qr","x":0,"y":0,"width":10,"height":10}`,
			"elements[0]: asset has no micro_qr code"},
		{"empty binding", `{"type":"generator","generator_type":"qr","x":0,"y":0,"width":10,"height":10,"content_binding":"{{field.serial}}"}`,
			"elements[0]: content binding resolved to an empty payload"},
		{"box too small", `{"type":"generator","generator_type":"qr","x":0,"y":0,"width":3,"height":3,"content_binding":"HELLO"}`,
			"elements[0]: a 21×21-module symbol needs a box of at least 58×58 dots (got 35×35)"},
		{"payload too long for level", `{"type":"generator","generator_type":"micro_qr","error_correction":"Q","x":0,"y":0,"width":10,"height":10,"content_binding":"ABCDEFGHIJKLMNOPQRSTU"}`,
			"elements[0]: code length violates"},
		{"missing image", `{"type":"static_image","x":0,"y":0,"width":10,"height":10,"image_id":"` + logoID + `"}`,
			"elements[0]: loading image"},
	}
	for _, c := range cases {
		tpl := mustTemplate(t, `{"id":"t","name":"T","units":"mm","width":10,"height":10,"dpi":300,"elements":[`+c.element+`]}`)
		_, err := Layout(tpl, labels.Data{}, memImages{})
		var le *LayoutError
		if !errors.As(err, &le) || !strings.HasPrefix(err.Error(), c.want) {
			t.Errorf("%s: got %v, want %q", c.name, err, c.want)
		}
	}
}

func textElement(size, minSize float64, wrap bool) *labels.Element {
	return &labels.Element{Type: labels.ElementText, FontSize: size, MinFontSize: minSize, Wrap: wrap,
		FontFamily: labels.FamilySans, FontWeight: labels.WeightNormal, Align: labels.AlignLeft}
}

func pathBounds(p Path) (minX, minY, maxX, maxY float64) {
	minX, minY, maxX, maxY = math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, seg := range p {
		n := map[SegmentOp]int{MoveTo: 1, LineTo: 1, QuadTo: 2, CubeTo: 3}[seg.Op]
		for _, pt := range seg.Pts[:n] {
			minX, minY = math.Min(minX, pt.X), math.Min(minY, pt.Y)
			maxX, maxY = math.Max(maxX, pt.X), math.Max(maxY, pt.Y)
		}
	}
	return
}

func TestTextShrinksToFitItsBox(t *testing.T) {
	box := image.Rect(100, 50, 400, 110)
	p, err := layoutText(textElement(40, 6, false), "Bench Drill Press", box, 300)
	if err != nil {
		t.Fatal(err)
	}
	minX, minY, maxX, maxY := pathBounds(p)
	if minX < 99 || minY < 49 || maxX > 401 || maxY > 111 {
		t.Errorf("text spans (%.1f,%.1f)-(%.1f,%.1f), outside box %v", minX, minY, maxX, maxY, box)
	}
	if maxX-minX < 250 {
		t.Errorf("text shrank more than needed: only %.1f dots wide in a 300-dot box", maxX-minX)
	}
}

func TestTextTruncatesWithEllipsisAtMinimumSize(t *testing.T) {
	fonts, _ := loadFonts()
	fc, err := newFace(fonts[fontKey{labels.FamilySans, labels.WeightNormal}], 8, 300)
	if err != nil {
		t.Fatal(err)
	}
	long := "Keep the chuck key on the magnetic strip next to the machine"

	block := &textBlock{face: fc, lines: breakLines(fc, long, 200, false, func(int) bool { return false })}
	truncate(block, 200, 40)
	if len(block.lines) != 1 || !strings.HasSuffix(block.lines[0], ellipsis) || fc.width(block.lines[0]) > 200 {
		t.Errorf("single line truncation: %q", block.lines)
	}

	wrapped := &textBlock{face: fc, lines: breakLines(fc, long, 200, true, func(int) bool { return false })}
	if len(wrapped.lines) < 3 {
		t.Fatalf("expected wrapping into several lines, got %q", wrapped.lines)
	}
	for _, line := range wrapped.lines {
		if fc.width(line) > 200 {
			t.Errorf("wrapped line %q is wider than the box", line)
		}
	}
	twoLines := wrapped.lineHeight() + wrapped.ascent() + wrapped.descent()
	truncate(wrapped, 200, twoLines)
	if len(wrapped.lines) != 2 || !strings.HasSuffix(wrapped.lines[1], ellipsis) {
		t.Errorf("multi-line truncation: %q", wrapped.lines)
	}
}

func TestMonoRasterIsPureBlackAndWhite(t *testing.T) {
	s, err := Layout(mustTemplate(t, goldenTemplates["image_inches"]), testData, memImages{logoID: testLogo()})
	if err != nil {
		t.Fatal(err)
	}
	gray, mono := s.Raster(false), s.Raster(true)
	sawGray := false
	for i, v := range mono.Pix {
		if v != 0 && v != 0xFF {
			t.Fatalf("mono raster has gray value %d", v)
		}
		if g := gray.Pix[i]; g != 0 && g != 0xFF {
			sawGray = true
		}
	}
	if !sawGray {
		t.Error("grayscale raster has no anti-aliasing or image tones")
	}
}

// TestDrawsInListOrder draws an opaque white image over a code, and the code
// over the image: each backend shows whichever is listed last.
func TestDrawsInListOrder(t *testing.T) {
	white := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for i := range white.Pix {
		white.Pix[i] = 0xFF
	}
	images := memImages{logoID: white, "11111111-2222-4333-8444-555555555555": testLogo()}
	code := `{"type":"generator","generator_type":"qr","x":0,"y":0,"width":20,"height":20,"quiet_zone":0,"content_binding":"HELLO"}`
	cover := `{"type":"static_image","x":0,"y":0,"width":20,"height":20,"image_id":"` + logoID + `"}`
	for _, tc := range []struct {
		name     string
		elements string
		dark     bool
	}{
		{"image over code", code + "," + cover, false},
		{"code over image", cover + "," + code, true},
	} {
		tpl := mustTemplate(t, `{"id":"t","name":"T","units":"mm","width":20,"height":20,"dpi":300,"layering":"list","elements":[`+tc.elements+`]}`)
		s, err := Layout(tpl, labels.Data{}, images)
		if err != nil {
			t.Fatal(err)
		}
		// The QR Code's top-left finder pattern is dark at its corner.
		for _, mono := range []bool{false, true} {
			if v := s.Raster(mono).GrayAt(2, 2).Y; (v == 0) != tc.dark {
				t.Errorf("%s, mono %v: the corner is %d", tc.name, mono, v)
			}
		}
		var svg bytes.Buffer
		if err := WriteSVG(&svg, s); err != nil {
			t.Fatal(err)
		}
		if imageFirst := strings.Index(svg.String(), "<image") < strings.Index(svg.String(), "crispEdges"); imageFirst != tc.dark {
			t.Errorf("%s: the SVG draws them in the wrong order", tc.name)
		}
	}

	// An image with transparency keeps it, as a soft mask, over what is
	// drawn before it, and only then.
	for _, tc := range []struct {
		elements string
		mask     bool
	}{
		{`{"type":"static_image","x":0,"y":0,"width":20,"height":20,"image_id":"11111111-2222-4333-8444-555555555555"},` + code, false},
		{code + `,{"type":"static_image","x":0,"y":0,"width":20,"height":20,"image_id":"11111111-2222-4333-8444-555555555555"}`, true},
		{code + "," + cover, false},
	} {
		tpl := mustTemplate(t, `{"id":"t","name":"T","units":"mm","width":20,"height":20,"dpi":300,"layering":"list","elements":[`+tc.elements+`]}`)
		s, err := Layout(tpl, labels.Data{}, images)
		if err != nil {
			t.Fatal(err)
		}
		var pdf bytes.Buffer
		if err := WritePDF(&pdf, s); err != nil {
			t.Fatal(err)
		}
		if got := bytes.Contains(pdf.Bytes(), []byte("/SMask")); got != tc.mask {
			t.Errorf("%.80s...: soft mask %v, want %v", tc.elements, got, tc.mask)
		}
	}
}

// TestQuarterTurnsStayOnDots turns codes by quarter turns: their modules
// stay rectangles of whole dots, the same size, and the symbol stays where
// its box is.
func TestQuarterTurnsStayOnDots(t *testing.T) {
	for _, rotation := range []string{"0", "90", "180", "270"} {
		tpl := mustTemplate(t, `{"id":"t","name":"T","units":"mm","width":30,"height":30,"dpi":300,"layering":"list","elements":[
			{"type":"generator","generator_type":"qr","x":5,"y":5,"width":20,"height":20,"rotation":`+rotation+`,"content_binding":"HELLO"}]}`)
		s, err := Layout(tpl, labels.Data{}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(s.Items) != 1 || s.Items[0].Rects == nil {
			t.Fatalf("%s°: %d items, not modules", rotation, len(s.Items))
		}
		minX, minY, maxX, maxY := math.MaxInt, math.MaxInt, 0, 0
		for _, r := range s.Items[0].Rects {
			if r.W%8 != 0 || r.H%8 != 0 {
				t.Fatalf("%s°: rect %+v is not whole 8-dot modules", rotation, r)
			}
			minX, minY = min(minX, r.X), min(minY, r.Y)
			maxX, maxY = max(maxX, r.X+r.W), max(maxY, r.Y+r.H)
		}
		if minX != 93 || minY != 93 || maxX-minX != 168 || maxY-minY != 168 {
			t.Errorf("%s°: symbol at (%d,%d) %d×%d dots", rotation, minX, minY, maxX-minX, maxY-minY)
		}
	}

	// At other angles, modules are turned polygons.
	tpl := mustTemplate(t, `{"id":"t","name":"T","units":"mm","width":30,"height":30,"dpi":300,"layering":"list","elements":[
		{"type":"generator","generator_type":"qr","x":5,"y":5,"width":20,"height":20,"rotation":30,"content_binding":"HELLO"}]}`)
	s, err := Layout(tpl, labels.Data{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Items) != 1 || s.Items[0].Rects != nil || len(s.Items[0].Path) == 0 {
		t.Errorf("at 30°: %+v", s.Items)
	}
}

// TestTurnedImagesAgree checks that the PDF's matrix for an image turned
// 30° puts its corners where the PNG draws them.
func TestTurnedImagesAgree(t *testing.T) {
	tpl := mustTemplate(t, `{"id":"t","name":"T","units":"mm","width":40,"height":40,"dpi":300,"layering":"list","elements":[
		{"type":"static_image","x":5,"y":10,"width":30,"height":20,"rotation":30,"image_id":"`+arrowID+`"}]}`)
	s, err := Layout(tpl, labels.Data{}, memImages{arrowID: testArrow()})
	if err != nil {
		t.Fatal(err)
	}
	pi := s.Items[0].Image
	if pi == nil || pi.Rotation != 30 {
		t.Fatalf("not turned: %+v", s.Items)
	}
	var pdf bytes.Buffer
	if err := WritePDF(&pdf, s); err != nil {
		t.Fatal(err)
	}
	// The page's content is the only stream without a dictionary of its own.
	stream := pdfContent(t, pdf.Bytes())
	m := regexp.MustCompile(`q (\S+) (\S+) (\S+) (\S+) (\S+) (\S+) cm (\S+) 0 0 (\S+) (\S+) (\S+) cm /Im0 Do Q`).FindStringSubmatch(stream)
	if m == nil {
		t.Fatalf("no turned image in %s", stream)
	}
	f := make([]float64, 10)
	for i := range f {
		f[i], _ = strconv.ParseFloat(m[i+1], 64)
	}
	sin, cos := labels.SinCos(30)
	for _, c := range [][2]float64{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
		// The image's unit square, as PDF draws it, in dots.
		x, y := f[6]*c[0]+f[8], f[7]*c[1]+f[9]
		x, y = f[0]*x+f[2]*y+f[4], f[1]*x+f[3]*y+f[5]
		// The same corner as the raster turns it: (0, 1) in PDF's unit
		// square is the image's top left.
		ux, uy := float64(pi.X)+c[0]*float64(pi.W), float64(pi.Y)+(1-c[1])*float64(pi.H)
		wx := pi.Cx + (ux-pi.Cx)*cos - (uy-pi.Cy)*sin
		wy := pi.Cy + (ux-pi.Cx)*sin + (uy-pi.Cy)*cos
		if math.Abs(x-wx) > 0.01 || math.Abs(y-wy) > 0.01 {
			t.Errorf("corner %v: PDF puts it at (%.2f, %.2f), the raster at (%.2f, %.2f)", c, x, y, wx, wy)
		}
	}
}

// pdfContent inflates the first page's content stream.
func pdfContent(t *testing.T, pdf []byte) string {
	t.Helper()
	i := bytes.Index(pdf, []byte("<<  /Filter /FlateDecode"))
	if i < 0 {
		t.Fatal("no content stream")
	}
	start := bytes.Index(pdf[i:], []byte("stream\n")) + i + len("stream\n")
	end := bytes.Index(pdf[start:], []byte("\nendstream")) + start
	r, err := zlib.NewReader(bytes.NewReader(pdf[start:end]))
	if err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// TestCodeMargins lays codes out with margins side by side, in modules and
// in length, aligned in their boxes.
func TestCodeMargins(t *testing.T) {
	// A 20 mm box at 300 DPI is 236 dots; HELLO is a 21-module QR Code.
	layout := func(t *testing.T, element string) (CodePlacement, Rect) {
		t.Helper()
		tpl := mustTemplate(t, `{"id":"t","name":"T","units":"mm","width":30,"height":30,"dpi":300,"layering":"list","elements":[
			{"type":"generator","generator_type":"qr","x":5,"y":5,"width":20,"height":20,"content_binding":"HELLO",`+element+`}]}`)
		s, err := Layout(tpl, labels.Data{}, nil)
		if err != nil {
			t.Fatal(err)
		}
		minX, minY, maxX, maxY := math.MaxInt, math.MaxInt, 0, 0
		for _, r := range s.Items[0].Rects {
			minX, minY = min(minX, r.X), min(minY, r.Y)
			maxX, maxY = max(maxX, r.X+r.W), max(maxY, r.Y+r.H)
		}
		return s.Codes[0], Rect{X: minX, Y: minY, W: maxX - minX, H: maxY - minY}
	}

	// Standard: 4 modules each side, 29 in all, 8 dots each, centered.
	c, r := layout(t, `"align":"center"`)
	if c.Module != 8 || r != (Rect{X: 93, Y: 93, W: 168, H: 168}) || c.Margin != [4]int{34, 34, 34, 34} || len(c.Narrow()) != 0 {
		t.Errorf("standard: %+v %+v", c, r)
	}
	// No margin on the left and top, in the top left corner: 25 modules of 9 dots.
	c, r = layout(t, `"margin":{"left":0,"top":0},"align":"left","vertical_align":"top"`)
	if c.Module != 9 || r.X != 59 || r.Y != 59 || c.Margin[0] != 0 || c.Margin[3] != 0 || !reflect.DeepEqual(c.Narrow(), []string{"top", "left"}) {
		t.Errorf("in the corner: %+v %+v", c, r)
	}
	// Bottom right, its margins 1 mm (12 dots) whatever the module.
	c, r = layout(t, `"margin":{"top":1,"right":1,"bottom":1,"left":1},"margin_unit":"length","align":"right","vertical_align":"bottom"`)
	if c.Module != 10 || r.X+r.W != 59+236-12 || r.Y+r.H != 59+236-12 || c.Margin[1] != 12 || c.Margin[2] != 12 {
		t.Errorf("in length: %+v %+v", c, r)
	}
	// quiet_zone, from before margins, still means every side.
	c, _ = layout(t, `"quiet_zone":1`)
	if c.Module != 10 || !reflect.DeepEqual(c.Narrow(), []string{"top", "right", "bottom", "left"}) {
		t.Errorf("quiet_zone: %+v", c)
	}

	// Code 128 bars fill the height inside the top and bottom margins.
	tpl := mustTemplate(t, `{"id":"t","name":"T","units":"mm","width":40,"height":20,"dpi":300,"layering":"list","elements":[
		{"type":"generator","generator_type":"code128","x":0,"y":0,"width":40,"height":10,"content_binding":"1234","margin":{"top":2,"bottom":3},"margin_unit":"length"}]}`)
	s, err := Layout(tpl, labels.Data{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if bar := s.Items[0].Rects[0]; bar.Y != 24 || bar.H != 118-24-35 {
		t.Errorf("bars: %+v", bar)
	}
	if c := s.Codes[0]; c.Margin[0] != 24 || c.Margin[2] != 35 || len(c.Narrow()) != 0 {
		t.Errorf("code 128: %+v", c)
	}
	// Margins that leave no room are refused when laid out.
	tpl = mustTemplate(t, `{"id":"t","name":"T","units":"mm","width":40,"height":20,"dpi":300,"layering":"list","elements":[
		{"type":"generator","generator_type":"qr","x":0,"y":0,"width":10,"height":10,"content_binding":"HELLO","margin":{"left":20,"right":20}}]}`)
	if _, err := Layout(tpl, labels.Data{}, nil); err == nil || !strings.Contains(err.Error(), "needs a box") {
		t.Errorf("no room: %v", err)
	}
}
