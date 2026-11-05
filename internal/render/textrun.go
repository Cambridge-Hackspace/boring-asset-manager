package render

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
	"golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// TextRun is text on one baseline in one of the embedded fonts. A PDF draws
// it as text in a Type 3 font made of the same glyph outlines, defined once
// however often they are used, so that a document of many pages stays small
// and its text can be searched and copied; SVG and PNG draw it as the
// outlines themselves. Labels draw their text as paths.
type TextRun struct {
	Bold bool
	// Size is in points; X and Y, the start of the baseline, in dots.
	Size float64
	X, Y float64
	Text string
}

func (t *TextRun) key() fontKey {
	if t.Bold {
		return fontKey{labels.FamilySans, labels.WeightBold}
	}
	return fontKey{labels.FamilySans, labels.WeightNormal}
}

// outlined is the scene with its text runs as paths, for the backends that
// draw paths.
func (s *Scene) outlined() *Scene {
	has := false
	for _, it := range s.Items {
		has = has || it.Text != nil
	}
	if !has {
		return s
	}
	fonts, _ := loadFonts()
	faces := map[faceKey]*face{}
	out := *s
	out.Items = make([]Item, 0, len(s.Items))
	for _, it := range s.Items {
		if it.Text == nil {
			out.Items = append(out.Items, it)
			continue
		}
		t := it.Text
		k := faceKey{t.Bold, t.Size}
		fc := faces[k]
		if fc == nil {
			var err error
			if fc, err = newFace(fonts[t.key()], t.Size, s.DPI); err != nil {
				continue
			}
			faces[k] = fc
		}
		if p := fc.outline(nil, t.Text, t.X, t.Y); len(p) > 0 {
			out.Items = append(out.Items, Item{Path: p, Paint: it.Paint})
		}
	}
	return &out
}

// type3Units is a Type 3 glyph's units to the em.
const type3Units = 1000

// type3Font is the glyphs of one embedded font a PDF uses, in subsets of
// up to 255, each a Type 3 font of its own, as a code is one byte.
type type3Font struct {
	font    *sfnt.Font
	buf     sfnt.Buffer
	subsets []*type3Subset
	// where finds the subset and code a glyph was given.
	where map[sfnt.GlyphIndex]glyphCode
}

type type3Subset struct {
	id     int
	glyphs []sfnt.GlyphIndex // by code - 1
	runes  []rune
}

type glyphCode struct {
	subset int
	code   byte
}

func (f *type3Font) glyph(pw *pdfWriter, r rune) (sfnt.GlyphIndex, glyphCode) {
	idx, err := f.font.GlyphIndex(&f.buf, r)
	if err != nil {
		idx = 0
	}
	if c, ok := f.where[idx]; ok {
		return idx, c
	}
	if len(f.subsets) == 0 || len(f.subsets[len(f.subsets)-1].glyphs) == 255 {
		f.subsets = append(f.subsets, &type3Subset{id: pw.reserve()})
	}
	n := len(f.subsets) - 1
	s := f.subsets[n]
	s.glyphs = append(s.glyphs, idx)
	s.runes = append(s.runes, r)
	c := glyphCode{subset: n, code: byte(len(s.glyphs))}
	f.where[idx] = c
	return idx, c
}

// fontFor is the Type 3 font of key, made as it is first used.
func (pw *pdfWriter) fontFor(key fontKey) *type3Font {
	if pw.type3 == nil {
		pw.type3 = map[fontKey]*type3Font{}
	}
	f := pw.type3[key]
	if f == nil {
		fonts, _ := loadFonts()
		f = &type3Font{font: fonts[key], where: map[sfnt.GlyphIndex]glyphCode{}}
		pw.type3[key] = f
	}
	return f
}

// writeText writes a text run's operators: the codes of its glyphs, with
// kerning between them, switching subset as the glyphs need. used gathers
// the fonts' resource names.
func (pw *pdfWriter) writeText(content *bytes.Buffer, t *TextRun, dpi int, used map[string]int) {
	f := pw.fontFor(t.key())
	size := t.Size * float64(dpi) / 72
	ppem := fixed.I(type3Units)
	fmt.Fprintf(content, "BT 1 0 0 1 %s %s Tm\n", num(t.X), num(t.Y))
	current := -1
	var run strings.Builder
	flush := func() {
		if run.Len() > 0 {
			fmt.Fprintf(content, "[%s] TJ\n", run.String())
			run.Reset()
		}
	}
	prev, first := sfnt.GlyphIndex(0), true
	for _, r := range t.Text {
		idx, c := f.glyph(pw, r)
		if c.subset != current {
			flush()
			name := fmt.Sprintf("T%d", f.subsets[c.subset].id)
			used[name] = f.subsets[c.subset].id
			fmt.Fprintf(content, "/%s %s Tf\n", name, num(size))
			current = c.subset
		}
		if !first {
			if k, err := f.font.Kern(&f.buf, prev, idx, ppem, font.HintingNone); err == nil && k != 0 {
				fmt.Fprintf(&run, "%d", -k.Round())
			}
		}
		fmt.Fprintf(&run, "<%02X>", c.code)
		prev, first = idx, false
	}
	flush()
	content.WriteString("ET\n")
}

// writeFonts writes each Type 3 subset used: its glyphs' outlines, drawn in
// whatever paint the text is, their advances, and a map back to the text,
// so that it can be searched and copied.
func (pw *pdfWriter) writeFonts() error {
	keys := make([]fontKey, 0, len(pw.type3))
	for k := range pw.type3 {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].family+keys[i].weight < keys[j].family+keys[j].weight
	})
	ppem := fixed.I(type3Units)
	for _, k := range keys {
		f := pw.type3[k]
		for _, s := range f.subsets {
			var procs, diffs, widths, cmap strings.Builder
			for i, idx := range s.glyphs {
				adv, err := f.font.GlyphAdvance(&f.buf, idx, ppem, font.HintingNone)
				if err != nil {
					return err
				}
				var glyph bytes.Buffer
				fmt.Fprintf(&glyph, "%d 0 d0\n", adv.Round())
				if segments, err := f.font.LoadGlyph(&f.buf, idx, ppem, nil); err == nil && len(segments) > 0 {
					var p Path
					for _, seg := range segments {
						out := Segment{Op: segmentOps[seg.Op]}
						for j, pt := range seg.Args {
							out.Pts[j] = Point{X: fixedToFloat(pt.X), Y: fixedToFloat(pt.Y)}
						}
						p = append(p, out)
					}
					pdfPath(&glyph, p)
					glyph.WriteString("f\n")
				}
				id := pw.writeStream("", deflate(glyph.Bytes()))
				fmt.Fprintf(&procs, "/g%d %d 0 R ", i+1, id)
				fmt.Fprintf(&diffs, "/g%d ", i+1)
				fmt.Fprintf(&widths, "%d ", adv.Round())
				fmt.Fprintf(&cmap, "<%02X> <%04X>\n", i+1, utf16Unit(s.runes[i]))
			}
			toUnicode := pw.writeStream("", deflate([]byte(fmt.Sprintf(
				"/CIDInit /ProcSet findresource begin 12 dict begin begincmap\n/CMapName /BAM%d def /CMapType 2 def\n"+
					"1 begincodespacerange <01> <FF> endcodespacerange\n%d beginbfchar\n%sendbfchar\nendcmap CMapName currentdict /CMap defineresource pop end end\n",
				s.id, len(s.glyphs), cmap.String()))))
			// The glyphs' outlines run down the page, as dots do, so that
			// under the page's matrix, which turns everything over, they
			// stand upright.
			pw.writeObjectAt(s.id, fmt.Sprintf("<< /Type /Font /Subtype /Type3 /FontBBox [0 0 0 0] /FontMatrix [0.001 0 0 0.001 0 0] "+
				"/CharProcs << %s>> /Encoding << /Type /Encoding /Differences [1 %s] >> /FirstChar 1 /LastChar %d /Widths [%s] /ToUnicode %d 0 R >>",
				procs.String(), strings.TrimSpace(diffs.String()), len(s.glyphs), strings.TrimSpace(widths.String()), toUnicode))
		}
	}
	return nil
}

// utf16Unit is r as one UTF-16 unit, or the replacement character for one
// outside the Basic Multilingual Plane, which takes two.
func utf16Unit(r rune) rune {
	if r > 0xFFFF {
		return 0xFFFD
	}
	return r
}
