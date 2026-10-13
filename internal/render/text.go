package render

import (
	"fmt"
	"image"
	"math"
	"strings"
	"sync"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

const (
	fontSizeStep = 0.5 // points between shrink-to-fit attempts
	ellipsis     = "…"
)

type fontKey struct{ family, weight string }

var (
	fontsOnce sync.Once
	fonts     map[fontKey]*sfnt.Font
	fontsErr  error
)

// loadFonts parses the embedded Go fonts once. They are BSD-licensed and
// ship inside golang.org/x/image.
func loadFonts() (map[fontKey]*sfnt.Font, error) {
	fontsOnce.Do(func() {
		sources := map[fontKey][]byte{
			{labels.FamilySans, labels.WeightNormal}: goregular.TTF,
			{labels.FamilySans, labels.WeightBold}:   gobold.TTF,
			{labels.FamilyMono, labels.WeightNormal}: gomono.TTF,
			{labels.FamilyMono, labels.WeightBold}:   gomonobold.TTF,
		}
		fonts = make(map[fontKey]*sfnt.Font, len(sources))
		for key, ttf := range sources {
			f, err := sfnt.Parse(ttf)
			if err != nil {
				fontsErr = fmt.Errorf("parsing embedded font %s %s: %w", key.family, key.weight, err)
				return
			}
			fonts[key] = f
		}
	})
	return fonts, fontsErr
}

// face measures and outlines text in one font at one size. It is not safe
// for concurrent use.
type face struct {
	font    *sfnt.Font
	buf     sfnt.Buffer
	ppem    fixed.Int26_6
	metrics font.Metrics
}

func newFace(f *sfnt.Font, points float64, dpi int) (*face, error) {
	ppem := fixed.Int26_6(math.Round(points * float64(dpi) / 72 * 64))
	fc := &face{font: f, ppem: ppem}
	m, err := f.Metrics(&fc.buf, ppem, font.HintingNone)
	if err != nil {
		return nil, err
	}
	fc.metrics = m
	return fc, nil
}

func (fc *face) glyph(r rune) sfnt.GlyphIndex {
	idx, err := fc.font.GlyphIndex(&fc.buf, r)
	if err != nil {
		return 0
	}
	return idx // 0 is .notdef, drawn as a box
}

// width returns the advance width of s in dots, including kerning.
func (fc *face) width(s string) float64 {
	var total fixed.Int26_6
	prev := sfnt.GlyphIndex(0)
	for i, r := range []rune(s) {
		idx := fc.glyph(r)
		if i > 0 {
			if k, err := fc.font.Kern(&fc.buf, prev, idx, fc.ppem, font.HintingNone); err == nil {
				total += k
			}
		}
		if adv, err := fc.font.GlyphAdvance(&fc.buf, idx, fc.ppem, font.HintingNone); err == nil {
			total += adv
		}
		prev = idx
	}
	return fixedToFloat(total)
}

// outline appends the glyph outlines of s with its baseline starting at
// (x, y) in dots.
func (fc *face) outline(p Path, s string, x, y float64) Path {
	prev := sfnt.GlyphIndex(0)
	for i, r := range []rune(s) {
		idx := fc.glyph(r)
		if i > 0 {
			if k, err := fc.font.Kern(&fc.buf, prev, idx, fc.ppem, font.HintingNone); err == nil {
				x += fixedToFloat(k)
			}
		}
		segments, err := fc.font.LoadGlyph(&fc.buf, idx, fc.ppem, nil)
		if err == nil {
			for _, seg := range segments {
				out := Segment{Op: segmentOps[seg.Op]}
				for j, pt := range seg.Args {
					out.Pts[j] = Point{X: x + fixedToFloat(pt.X), Y: y + fixedToFloat(pt.Y)}
				}
				p = append(p, out)
			}
		}
		if adv, err := fc.font.GlyphAdvance(&fc.buf, idx, fc.ppem, font.HintingNone); err == nil {
			x += fixedToFloat(adv)
		}
		prev = idx
	}
	return p
}

var segmentOps = map[sfnt.SegmentOp]SegmentOp{
	sfnt.SegmentOpMoveTo: MoveTo,
	sfnt.SegmentOpLineTo: LineTo,
	sfnt.SegmentOpQuadTo: QuadTo,
	sfnt.SegmentOpCubeTo: CubeTo,
}

func fixedToFloat(v fixed.Int26_6) float64 {
	return float64(v) / 64
}

// textBlock is text broken into lines at one font size.
type textBlock struct {
	face  *face
	lines []string
}

func (b *textBlock) lineHeight() float64 { return fixedToFloat(b.face.metrics.Height) }
func (b *textBlock) ascent() float64     { return fixedToFloat(b.face.metrics.Ascent) }
func (b *textBlock) descent() float64    { return fixedToFloat(b.face.metrics.Descent) }

// height is the block's extent from the first line's ascent to the last
// line's descent.
func (b *textBlock) height() float64 {
	if len(b.lines) == 0 {
		return 0
	}
	return float64(len(b.lines)-1)*b.lineHeight() + b.ascent() + b.descent()
}

func (b *textBlock) fits(width, height float64) bool {
	if b.height() > height {
		return false
	}
	for _, line := range b.lines {
		if b.face.width(line) > width {
			return false
		}
	}
	return true
}

// layoutText sets text in box following README §4.1: break lines at "\n"
// (and at spaces when wrapping), shrink in 0.5 pt steps until it fits, and at
// the minimum size cut it short with an ellipsis. The block is centered
// vertically.
func layoutText(e *labels.Element, text string, box image.Rectangle, dpi int) (Path, error) {
	all, err := loadFonts()
	if err != nil {
		return nil, err
	}
	f := all[fontKey{e.FontFamily, e.FontWeight}]
	if f == nil {
		return nil, fmt.Errorf("no font for %s %s", e.FontFamily, e.FontWeight)
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	bw, bh := float64(box.Dx()), float64(box.Dy())

	var sizes []float64
	for size := e.FontSize; size > e.MinFontSize; size -= fontSizeStep {
		sizes = append(sizes, size)
	}
	sizes = append(sizes, e.MinFontSize)

	var block *textBlock
	for _, size := range sizes {
		fc, err := newFace(f, size, dpi)
		if err != nil {
			return nil, err
		}
		block = &textBlock{face: fc, lines: breakLines(fc, text, bw, e.Wrap)}
		if block.fits(bw, bh) {
			break
		}
	}
	if !block.fits(bw, bh) {
		truncate(block, bw, bh)
	}

	var p Path
	top := float64(box.Min.Y) + (bh-block.height())/2
	for i, line := range block.lines {
		w := block.face.width(line)
		x := float64(box.Min.X)
		switch e.Align {
		case labels.AlignCenter:
			x += (bw - w) / 2
		case labels.AlignRight:
			x += bw - w
		}
		baseline := top + block.ascent() + float64(i)*block.lineHeight()
		p = block.face.outline(p, line, x, baseline)
	}
	return p, nil
}

// breakLines splits text at newlines and, when wrapping, between words so no
// line is wider than width unless a single word is.
func breakLines(fc *face, text string, width float64, wrap bool) []string {
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
			if candidate := line + " " + w; fc.width(candidate) <= width {
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

// truncate drops lines that do not fit vertically and ellipsizes the last
// kept line and any line wider than width.
func truncate(b *textBlock, width, height float64) {
	keep := 1
	for keep < len(b.lines) && float64(keep)*b.lineHeight()+b.ascent()+b.descent() <= height {
		keep++
	}
	cut := keep < len(b.lines)
	b.lines = b.lines[:keep]
	for i, line := range b.lines {
		last := i == len(b.lines)-1
		if b.face.width(line) > width || (last && cut) {
			b.lines[i] = ellipsize(b.face, line, width)
		}
	}
}

// ellipsize shortens s until it and a trailing ellipsis fit in width.
func ellipsize(fc *face, s string, width float64) string {
	runes := []rune(strings.TrimRight(s, " "))
	for len(runes) > 0 && fc.width(string(runes)+ellipsis) > width {
		runes = []rune(strings.TrimRight(string(runes[:len(runes)-1]), " "))
	}
	if len(runes) == 0 && fc.width(ellipsis) > width {
		return ""
	}
	return string(runes) + ellipsis
}
