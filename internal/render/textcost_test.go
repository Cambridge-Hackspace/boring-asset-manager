package render

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
)

// The straightforward versions of line breaking and ellipsizing, which
// measure every candidate from scratch. Labels must come out exactly as
// they did, so the faster versions are compared with these.
func referenceBreakLines(fc *face, text string, width float64, wrap bool) []string {
	paragraphs := strings.Split(text, "\n")
	if !wrap {
		return paragraphs
	}
	var lines []string
	for _, para := range paragraphs {
		words := strings.Fields(para)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}
		line := words[0]
		for _, w := range words[1:] {
			if candidate := line + " " + w; referenceWidth(fc, candidate) <= width {
				line = candidate
			} else {
				lines = append(lines, line)
				line = w
			}
		}
		lines = append(lines, line)
	}
	return lines
}

func referenceEllipsize(fc *face, s string, width float64) string {
	runes := []rune(strings.TrimRight(s, " "))
	for len(runes) > 0 && referenceWidth(fc, string(runes)+ellipsis) > width {
		runes = []rune(strings.TrimRight(string(runes[:len(runes)-1]), " "))
	}
	if len(runes) == 0 && referenceWidth(fc, ellipsis) > width {
		return ""
	}
	return string(runes) + ellipsis
}

// referenceWidth measures s in one pass, glyph by glyph.
func referenceWidth(fc *face, s string) float64 {
	r := run{}
	for _, ch := range []rune(s) {
		r = fc.extendRune(r, ch)
	}
	return fixedToFloat(r.width)
}

// TestTextLayoutMatchesTheReference lays out random text at random sizes in
// random boxes and checks the lines, after truncation, are exactly those the
// reference gives, though breaking stops early and widths are extended.
func TestTextLayoutMatchesTheReference(t *testing.T) {
	fonts, err := loadFonts()
	if err != nil {
		t.Fatal(err)
	}
	words := []string{"a", "chuck", "key", "on", "the", "magnetic", "strip", "WWWWWWWW", "iiiii", "Ångström", "🔧", "工具箱", "x-y", "", "  "}
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 400; i++ {
		var b strings.Builder
		for n := rng.Intn(60); n > 0; n-- {
			b.WriteString(words[rng.Intn(len(words))])
			b.WriteString([]string{" ", " ", " ", "  ", "\n"}[rng.Intn(5)])
		}
		text := b.String()
		key := []fontKey{{labels.FamilySans, labels.WeightNormal}, {labels.FamilyMono, labels.WeightBold}}[rng.Intn(2)]
		fc, err := newFace(fonts[key], 4+rng.Float64()*30, []int{150, 300, 600}[rng.Intn(3)])
		if err != nil {
			t.Fatal(err)
		}
		width, height := 20+rng.Float64()*600, 10+rng.Float64()*400
		wrap := rng.Intn(4) > 0

		want := &textBlock{face: fc, lines: referenceBreakLines(fc, text, width, wrap)}
		got := &textBlock{face: fc}
		got.lines = breakLines(fc, text, width, wrap, got.enoughFor(height))
		if want.fits(width, height) != got.fits(width, height) {
			t.Fatalf("case %d: fits %v, reference %v\n%q", i, got.fits(width, height), want.fits(width, height), text)
		}
		if want.fits(width, height) && fmt.Sprint(got.lines) != fmt.Sprint(want.lines) {
			t.Fatalf("case %d: lines %q, reference %q", i, got.lines, want.lines)
		}
		if !want.fits(width, height) {
			truncate(want, width, height)
			truncate(got, width, height)
			if fmt.Sprint(got.lines) != fmt.Sprint(want.lines) {
				t.Fatalf("case %d: truncated lines %q, reference %q", i, got.lines, want.lines)
			}
		}
		for _, line := range want.lines {
			w := rng.Float64() * width
			if got, want := ellipsize(fc, line, w), referenceEllipsize(fc, line, w); got != want {
				t.Fatalf("case %d: ellipsize(%q, %g) = %q, reference %q", i, line, w, got, want)
			}
		}
		if got, want := fc.width(text), referenceWidth(fc, text); got != want {
			t.Fatalf("case %d: width %g, reference %g", i, got, want)
		}
	}
}

// TestLongTextLaysOutQuickly lays out the most text a template can ask for:
// 100 wrapped elements shrinking from 200 pt to 6 pt, each bound to a
// field value of 10,000 characters that never fits its box. Measuring every
// candidate line from scratch at every size took 25 seconds and 6 GB; it
// now takes well under a second. The bound is loose, for slow machines and
// the race detector.
func TestLongTextLaysOutQuickly(t *testing.T) {
	data := testData
	data.Fields = map[string]string{"notes": strings.Repeat("lorem ipsum dolor sit amet ", 370)}
	var elements []string
	for i := 0; i < labels.MaxElements; i++ {
		elements = append(elements, `{"type":"text","x":1,"y":1,"width":20,"height":5,"font_size":200,"min_font_size":6,"wrap":true,"content_binding":"{{field.notes}}"}`)
	}
	tpl := mustTemplate(t, `{"id":"t","name":"T","units":"mm","width":100,"height":50,"dpi":600,"elements":[`+strings.Join(elements, ",")+`]}`)
	start := time.Now()
	if _, err := Layout(tpl, data, nil); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("laying out took %v", took)
	}
}

func TestSceneParts(t *testing.T) {
	s := &Scene{
		Rects:  make([]Rect, 3),
		Images: make([]PlacedImage, 1),
		Paths:  []Path{make(Path, 5), make(Path, 2)},
	}
	if got := s.Parts(); got != 11 {
		t.Errorf("Parts() = %d, want 11", got)
	}
}
