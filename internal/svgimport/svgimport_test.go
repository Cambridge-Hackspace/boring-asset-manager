package svgimport

import (
	"math"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
)

func mustImport(t *testing.T, svg string) *Result {
	t.Helper()
	r, err := Import([]byte(svg))
	if err != nil {
		t.Fatalf("%v: %s", err, svg)
	}
	return r
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

// checkTemplate puts the shapes on a template, which must validate.
func checkTemplate(t *testing.T, r *Result) {
	t.Helper()
	tpl := &labels.Template{Name: "T", Units: labels.UnitsMM, Width: 100, Height: 100, DPI: 300, Layering: labels.LayeringList, Elements: r.Place(0, 0, 100, 100)}
	tpl.Normalize()
	if err := tpl.Validate(); err != nil {
		t.Errorf("placed, the shapes are invalid: %v", err)
	}
}

// TestElements reads each kind of element as a path in its box.
func TestElements(t *testing.T) {
	r := mustImport(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 50">
		<rect x="10" y="10" width="20" height="10"/>
		<rect x="40" y="10" width="20" height="10" rx="3"/>
		<circle cx="80" cy="15" r="5"/>
		<ellipse cx="20" cy="35" rx="8" ry="4"/>
		<line x1="40" y1="30" x2="60" y2="30" stroke="black" stroke-width="2"/>
		<polyline points="70,30 80,40 90,30" fill="none" stroke="#000"/>
		<polygon points="10,45 20,48 15,49"/>
		<path d="m70 42 h10 a2 2 0 0 1 2 2 v2 h-12 z"/>
	</svg>`)
	if r.Width != 100 || r.Height != 50 || len(r.Elements) != 8 || len(r.Skipped) != 0 {
		t.Fatalf("%v × %v, %d shapes, left out %v", r.Width, r.Height, len(r.Elements), r.Skipped)
	}
	rect := r.Elements[0]
	if rect.Shape != labels.ShapePath || rect.X != 10 || rect.Y != 10 || rect.Width != 20 || rect.Height != 10 || rect.D != "M0 0 L1 0 L1 1 L0 1 Z" || rect.Fill != "black" || rect.Stroke != "none" {
		t.Errorf("the rectangle: %+v", rect)
	}
	if c := r.Elements[2]; !near(c.Width, 10) || !near(c.X, 75) || !strings.Contains(c.D, "C") {
		t.Errorf("the circle: %+v", c)
	}
	// A level line's box is as deep as its stroke; it has no fill.
	if l := r.Elements[4]; l.Height != 2 || l.Y != 29 || l.Fill != "none" || l.Stroke != "black" || l.StrokeWidth != 2 {
		t.Errorf("the line: %+v", l)
	}
	if p := r.Elements[5]; p.Fill != "none" || strings.Contains(p.D, "Z") {
		t.Errorf("the polyline: %+v", p)
	}
	checkTemplate(t, r)
}

// TestTransformsAndStyles follows transforms down groups and the viewBox,
// and styles from attributes, style attributes and the groups they are in.
func TestTransformsAndStyles(t *testing.T) {
	r := mustImport(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="-10 -10 100 100">
		<g transform="translate(10 20) scale(2)" fill="white" stroke="#333" stroke-width="1.5">
			<rect width="5" height="5"/>
			<rect x="10" width="5" height="5" style="fill: rgb(30, 30, 30); stroke: none"/>
			<path d="M0 10 h10 v10 h-10z M2 12 h6 v6 h-6z" fill-rule="evenodd" transform="rotate(90 5 15)"/>
		</g>
		<rect width="5" height="5" fill="transparent" stroke="black" opacity="0.4"/>
		<rect width="5" height="5" fill="black" display="none"/>
	</svg>`)
	if len(r.Elements) != 3 {
		t.Fatalf("%d shapes: %+v", len(r.Elements), r.Elements)
	}
	// translate(10 20) scale(2), and the viewBox's (-10, -10): (20, 30), 10 across.
	a := r.Elements[0]
	if a.X != 20 || a.Y != 30 || a.Width != 10 || a.Fill != "white" || a.Stroke != "black" || a.StrokeWidth != 3 {
		t.Errorf("styled from its group: %+v", a)
	}
	if b := r.Elements[1]; b.Fill != "black" || b.Stroke != "none" {
		t.Errorf("styled itself: %+v", b)
	}
	if c := r.Elements[2]; c.FillRule != labels.FillEvenOdd || !near(c.Width, 20) {
		t.Errorf("turned, even-odd: %+v", c)
	}
	checkTemplate(t, r)
}

// TestColors reduces colors to black or white by how light they look, and
// see-through paint to none.
func TestColors(t *testing.T) {
	for _, tc := range []struct{ paint, want string }{
		{"#000", "black"}, {"#fff", "white"}, {"#ffff00", "white"}, {"navy", "black"}, {"#808080", "white"}, {"#606060", "black"},
		{"rgb(255, 255, 255)", "white"}, {"rgba(0,0,0,0.2)", "none"}, {"#0008", "black"}, {"none", "none"}, {"currentColor", "black"},
	} {
		r, err := Import([]byte(`<svg viewBox="0 0 10 10"><rect width="5" height="5" fill="` + tc.paint + `" stroke="black"/></svg>`))
		if err != nil || r.Elements[0].Fill != tc.want {
			t.Errorf("%s: %+v %v", tc.paint, r, err)
		}
	}
}

// TestLeftOut says what it does not read, once each, and draws the rest.
func TestLeftOut(t *testing.T) {
	r := mustImport(t, `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" viewBox="0 0 10 10">
		<title>Logo</title>
		<style>rect { fill: red }</style>
		<script>alert(1)</script>
		<defs><linearGradient id="g"><stop offset="0" stop-color="#fff"/></linearGradient></defs>
		<text x="1" y="5">Hi</text><text x="1" y="9">Again</text>
		<image xlink:href="https://example.com/x.png" width="5" height="5"/>
		<use xlink:href="#a"/>
		<rect width="5" height="5" fill="url(#g)" filter="url(#f)"/>
		<filter id="f"/>
	</svg>`)
	if len(r.Elements) != 1 {
		t.Errorf("%d shapes", len(r.Elements))
	}
	joined := strings.Join(r.Skipped, " | ")
	for _, want := range []string{"text, which needs", "images", "scripts", "style sheets", "links to other files", "use elements", "gradients", "filters"} {
		if !strings.Contains(joined, want) {
			t.Errorf("not reported: %s, in %s", want, joined)
		}
	}
	if strings.Count(joined, "text, which") != 1 {
		t.Errorf("reported twice: %s", joined)
	}
}

// TestRefused refuses what is not SVG, a document type, which could
// define entities, and files past its limits.
func TestRefused(t *testing.T) {
	laughs := `<?xml version="1.0"?><!DOCTYPE lolz [<!ENTITY lol "lol"><!ENTITY lol2 "&lol;&lol;&lol;&lol;">]><svg>&lol2;</svg>`
	deep := "<svg>" + strings.Repeat("<g>", MaxDepth+1) + strings.Repeat("</g>", MaxDepth+1) + "</svg>"
	many := "<svg>" + strings.Repeat("<g/>", MaxElements+1) + "</svg>"
	for name, svg := range map[string]string{
		"entities":     laughs,
		"an entity":    `<svg><rect width="1" height="&x;"/></svg>`,
		"not SVG":      `<html><body/></html>`,
		"not XML":      `<svg><rect></svg>`,
		"too deep":     deep,
		"too many":     many,
		"too big":      "<svg>" + strings.Repeat(" ", MaxBytes) + "</svg>",
		"nothing":      `<svg viewBox="0 0 1 1"><text>Hi</text></svg>`,
		"not UTF-8":    `<?xml version="1.0" encoding="ISO-8859-1"?><svg/>`,
		"bad path":     `<svg viewBox="0 0 1 1"><path d="M0 0 X"/></svg>`,
		"no transform": `<svg viewBox="0 0 1 1"><rect width="1" height="1" transform="spin(3)"/></svg>`,
	} {
		if _, err := Import([]byte(svg)); err == nil {
			t.Errorf("%s: imported", name)
		}
	}
}

// TestPlace fits artwork into a box, keeping its proportions, centered,
// its strokes scaled with it.
func TestPlace(t *testing.T) {
	r := mustImport(t, `<svg viewBox="0 0 100 50"><rect width="100" height="50" fill="none" stroke="black" stroke-width="4"/></svg>`)
	e := r.Place(10, 10, 20, 20)[0]
	// A fifth as big: 20 × 10, in the middle of the box, its stroke too.
	if !near(e.X, 10) || !near(e.Y, 15) || !near(e.Width, 20) || !near(e.Height, 10) || !near(e.StrokeWidth, 0.8) {
		t.Errorf("placed: %+v", e)
	}
}

// FuzzImport never panics, and keeps within its limits.
func FuzzImport(f *testing.F) {
	for _, seed := range []string{
		`<svg viewBox="0 0 10 10"><path d="M1 1 L9 9 A3 4 30 1 0 2 8 Q1 1 5 5 T 9 1 S 2 2 3 3 Z"/></svg>`,
		`<svg width="10" height="10"><g transform="matrix(1 2 3 4 5 6) skewX(10)"><circle r="3" fill="#abc"/></g></svg>`,
		`<svg><rect width="-1" height="2"/><polygon points="1"/><ellipse rx="0"/></svg>`,
		`<svg><path d="M0 0 a0 0 0 0 0 1 1 z m 1e400 2"/></svg>`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		r, err := Import(data)
		if err != nil {
			return
		}
		segments := 0
		for _, e := range r.Elements {
			segs, err := labels.ParsePathData(e.D)
			if err != nil {
				t.Fatalf("it wrote path data it cannot read: %q: %v", e.D, err)
			}
			segments += len(segs)
		}
		if segments > labels.MaxTemplatePathSegments {
			t.Fatalf("%d segments", segments)
		}
	})
}
