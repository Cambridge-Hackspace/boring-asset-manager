package render

import (
	"bytes"
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
}

// goldenPayloads lists what each golden's barcodes must decode to; see
// tools/goldens/verify_renders.py.
var goldenPayloads = map[string][]string{
	"qr_label":      {"https://bam.example/a/2f1e3c4d-5b6a-4789-8abc-def012345678"},
	"micro_code128": {"BAM-0042", "TOOL-0042"},
	"image_inches":  {},
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
	images := memImages{logoID: testLogo()}
	type manifestEntry struct {
		File     string   `json:"file"`
		Payloads []string `json:"payloads"`
	}
	var manifest []manifestEntry

	for _, name := range []string{"image_inches", "micro_code128", "qr_label"} {
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
	for _, r := range s.Rects {
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
