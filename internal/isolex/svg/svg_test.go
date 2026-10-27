package svg

import (
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex/ir"
)

// isolex's own sample, drawn here with isolex's own palette, is isolex's
// own SVG golden byte for byte: every icon, as `isolex render` writes one
// file per icon and its test strings them together in name order.
func TestSampleMatchesIsolexsGolden(t *testing.T) {
	data, err := os.ReadFile("../testdata/sample.ir.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := ir.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if missing := Missing(doc.Icons, IsolexPalette); len(missing) != 0 {
		t.Fatalf("isolex's palette lacks %v", missing)
	}
	files := map[string]string{}
	for i := range doc.Icons {
		files[doc.Icons[i].Name+".svg"] = Icon(&doc.Icons[i], Palette(IsolexPalette))
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	var got strings.Builder
	for _, n := range names {
		got.WriteString("== " + n + " ==\n" + files[n])
	}
	want, err := os.ReadFile("../testdata/sample.svg.golden")
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != string(want) {
		g, w := strings.Split(got.String(), "\n"), strings.Split(string(want), "\n")
		for i := 0; i < len(g) && i < len(w); i++ {
			if g[i] != w[i] {
				t.Fatalf("line %d differs from isolex's golden:\n got %s\nwant %s", i+1, g[i], w[i])
			}
		}
		t.Fatalf("%d lines, isolex's golden has %d", len(g), len(w))
	}
}

// isolex 0.4's goldens, IR version 2, drawn here as isolex draws them:
// its sample, whose badges are matrices, as its CLI writes one file per
// icon; and its transforms and text fixtures, groups with matrices, clips
// and fixed strokes, as its tests string each icon together in order.
func TestVersionTwoMatchesIsolexsGoldens(t *testing.T) {
	read := func(name string) []byte {
		data, err := os.ReadFile("../testdata/isolex-0.5.0/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	same := func(name, got string) {
		want := string(read(name))
		if got == want {
			return
		}
		g, w := strings.Split(got, "\n"), strings.Split(want, "\n")
		for i := 0; i < len(g) && i < len(w); i++ {
			if g[i] != w[i] {
				t.Fatalf("%s: line %d differs from isolex's golden:\n got %s\nwant %s", name, i+1, g[i], w[i])
			}
		}
		t.Fatalf("%s: %d lines, isolex's golden has %d", name, len(g), len(w))
	}

	doc, err := ir.Parse(read("sample.ir.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for i := range doc.Icons {
		files[doc.Icons[i].Name+".svg"] = Icon(&doc.Icons[i], Palette(IsolexPalette))
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	var got strings.Builder
	for _, n := range names {
		got.WriteString("== " + n + " ==\n" + files[n])
	}
	same("sample.svg.golden", got.String())

	for _, fixture := range []string{"transforms", "text"} {
		doc, err := ir.Parse(read(fixture + ".ir.golden.json"))
		if err != nil {
			t.Fatal(err)
		}
		if missing := Missing(doc.Icons, IsolexPalette); len(missing) != 0 {
			t.Fatalf("isolex's palette lacks %v", missing)
		}
		var got strings.Builder
		for i := range doc.Icons {
			got.WriteString("<!-- " + doc.Icons[i].Name + " -->\n" + Icon(&doc.Icons[i], Palette(IsolexPalette)))
		}
		same(fixture+".svg.golden", got.String())
	}
}

// isolex's renderer tests, ported: the same input must come out the same.
func TestNumbersTakeTwoDecimalsTiesToEven(t *testing.T) {
	for v, want := range map[float64]string{
		0.125: "0.12", 0.375: "0.38", 0.625: "0.62", 0.875: "0.88",
		-0.125: "-0.12", 2.5: "2.50",
		// not ties: 0.145 is 0.14499999… in binary, 0.135 is 0.13500…01
		0.145: "0.14", 0.135: "0.14",
		// one tile along x, projected
		40 * 0.8660254037844386: "34.64",
		// never -0.00
		-0.001: "0.00", -0.004999: "0.00", -0.005001: "-0.01",
	} {
		if got := Number(v); got != want {
			t.Errorf("Number(%v) = %s, want %s", v, got, want)
		}
	}
	negZero := 0.0
	negZero = -negZero
	if got := Number(negZero); got != "0.00" {
		t.Errorf("Number(-0) = %s", got)
	}
}

func TestEachShapeIsWrittenTheSameWayEveryTime(t *testing.T) {
	dash := "3 2.5"
	icon := ir.Icon{
		Name:    "t",
		Canvas:  &[2]float64{24, 24},
		Viewbox: [4]float64{0, 0, 24, 24},
		Shapes: []ir.Shape{
			{Kind: ir.KindPolygon, Points: [][2]float64{{0.125, 1}, {2, 3.375}}, Fill: "accent", FillOpacity: 0.55, Stroke: "none"},
			{Kind: ir.KindPath, D: "M1.00,2.00 L3.00,4.00", Fill: "none", FillOpacity: 1, Stroke: "ink", StrokeWidth: 1.4, Dash: &dash},
			{Kind: ir.KindGroup, TX: 10, TY: -2, Scale: 0.5, Shapes: []ir.Shape{
				{Kind: ir.KindRect, X: 3, Y: 3, W: 18, H: 18, RX: 3, Fill: "none", FillOpacity: 1, Stroke: "ink-muted", StrokeWidth: 1.5},
			}},
		},
	}
	want := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0.00 0.00 24.00 24.00" width="24.00" height="24.00">
<title>t</title>
  <polygon points="0.12,1.00 2.00,3.38" fill="#d64545" fill-opacity="0.55" stroke="none"/>
  <path d="M1.00,2.00 L3.00,4.00" fill="none" stroke="#1f2933" stroke-width="1.40" stroke-dasharray="3 2.5"/>
  <g transform="translate(10.00 -2.00) scale(0.50)">
    <rect x="3.00" y="3.00" width="18.00" height="18.00" rx="3.00" fill="none" stroke="#7b8794" stroke-width="1.50"/>
  </g>
</svg>
`
	if got := Icon(&icon, Palette(IsolexPalette)); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestColorsAreEscaped(t *testing.T) {
	icon := ir.Icon{Name: `a"<b>`, Shapes: []ir.Shape{{Kind: ir.KindCircle, R: 1, Fill: "ink", FillOpacity: 1, Stroke: "none"}}}
	got := Icon(&icon, Palette(map[string]string{"ink": `"><script>`}))
	for _, want := range []string{`<title>a&quot;&lt;b&gt;</title>`, `fill="&quot;&gt;&lt;script&gt;" stroke="none"`} {
		if !strings.Contains(got, want) {
			t.Errorf("no %s in\n%s", want, got)
		}
	}
}

// Colors are the caller's: a page that themes icons paints tokens with
// CSS variables, and a token no palette has stays a token, as in isolex.
func TestColorsAreTheCallers(t *testing.T) {
	icon := ir.Icon{Name: "i", Shapes: []ir.Shape{
		{Kind: ir.KindCircle, R: 1, Fill: "category", FillOpacity: 1, Stroke: "outline", StrokeWidth: 1.2},
		{Kind: ir.KindGroup, Scale: 1, Shapes: []ir.Shape{{Kind: ir.KindCircle, R: 1, Fill: "none", Stroke: "brand", StrokeWidth: 1}}},
	}}
	css := Icon(&icon, func(t string) string { return "var(--iso-" + t + ")" })
	if !strings.Contains(css, `fill="var(--iso-category)" stroke="var(--iso-outline)"`) {
		t.Errorf("%s", css)
	}
	if got := Missing([]ir.Icon{icon}, IsolexPalette); strings.Join(got, ",") != "brand,category" {
		t.Errorf("Missing = %v", got)
	}
	if !strings.Contains(Icon(&icon, Palette(nil)), `stroke="brand"`) {
		t.Error("a token without a color is not written as itself")
	}
}
