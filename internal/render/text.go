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
	// size is in points, as asked for.
	size float64
}

func newFace(f *sfnt.Font, points float64, dpi int) (*face, error) {
	ppem := fixed.Int26_6(math.Round(points * float64(dpi) / 72 * 64))
	fc := &face{font: f, ppem: ppem, size: points}
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
	return fixedToFloat(fc.extend(run{}, s).width)
}

// run is the measured advance of some text, kept so that more can be
// appended without measuring it again. Advances and kerning are whole
// 1/64ths of a dot, so a run extended piece by piece has exactly the width
// of the whole text measured at once.
type run struct {
	width    fixed.Int26_6
	last     sfnt.GlyphIndex
	nonEmpty bool
}

// extend returns r with s appended.
func (fc *face) extend(r run, s string) run {
	for _, ch := range s {
		r = fc.extendRune(r, ch)
	}
	return r
}

func (fc *face) extendRune(r run, ch rune) run {
	idx := fc.glyph(ch)
	if r.nonEmpty {
		if k, err := fc.font.Kern(&fc.buf, r.last, idx, fc.ppem, font.HintingNone); err == nil {
			r.width += k
		}
	}
	if adv, err := fc.font.GlyphAdvance(&fc.buf, idx, fc.ppem, font.HintingNone); err == nil {
		r.width += adv
	}
	r.last, r.nonEmpty = idx, true
	return r
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
	return b.heightOf(len(b.lines))
}

// enoughFor says when breaking text for a box height can stop: one line
// after the lines that fit, which is enough to know the text does not fit,
// and for truncate to know there was more.
func (b *textBlock) enoughFor(height float64) func(n int) bool {
	return func(n int) bool { return n > 1 && b.heightOf(n-1) > height }
}

// heightOf is the height of a block of n lines.
func (b *textBlock) heightOf(n int) float64 {
	if n == 0 {
		return 0
	}
	return float64(n-1)*b.lineHeight() + b.ascent() + b.descent()
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

// layoutText sets text in box following docs/labels.md: break lines at "\n"
// (and at spaces when wrapping), shrink in 0.5 pt steps until it fits, and at
// the minimum size cut it short with an ellipsis. The block is centered
// vertically.
func layoutText(e *labels.Element, text string, box image.Rectangle, dpi int) (Path, float64, error) {
	all, err := loadFonts()
	if err != nil {
		return nil, 0, err
	}
	f := all[fontKey{e.FontFamily, e.FontWeight}]
	if f == nil {
		return nil, 0, fmt.Errorf("no font for %s %s", e.FontFamily, e.FontWeight)
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if strings.TrimSpace(text) == "" {
		return nil, e.FontSize, nil
	}
	bw, bh := float64(box.Dx()), float64(box.Dy())

	paras := splitParagraphs(text, e.Wrap)
	at := func(size float64) (*textBlock, error) {
		fc, err := newFace(f, size, dpi)
		if err != nil {
			return nil, err
		}
		b := &textBlock{face: fc}
		// Lines past the box's height cannot be shown; see enoughFor.
		b.lines = paras.lines(fc, bw, b.enoughFor(bh))
		return b, nil
	}

	var block *textBlock
	if e.Fit == labels.FitFill {
		// The largest size, in 0.5 pt steps, from the least to the most,
		// that fits: found by halving, as a larger size never fits where a
		// smaller one does not.
		most := e.MaxFontSize
		if most == 0 {
			most = labels.MaxFontSize
		}
		lo, hi := 0, int(math.Floor((most-e.MinFontSize)/fontSizeStep))
		size := func(k int) float64 { return e.MinFontSize + float64(k)*fontSizeStep }
		if block, err = at(size(lo)); err != nil {
			return nil, 0, err
		}
		for lo < hi {
			mid := (lo + hi + 1) / 2
			b, err := at(size(mid))
			if err != nil {
				return nil, 0, err
			}
			if b.fits(bw, bh) {
				lo, block = mid, b
			} else {
				hi = mid - 1
			}
		}
	} else {
		var sizes []float64
		for size := e.FontSize; size > e.MinFontSize; size -= fontSizeStep {
			sizes = append(sizes, size)
		}
		sizes = append(sizes, e.MinFontSize)
		for _, size := range sizes {
			if block, err = at(size); err != nil {
				return nil, 0, err
			}
			if block.fits(bw, bh) {
				break
			}
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
	return p, block.face.size, nil
}

// breakLines splits text at newlines and, when wrapping, between words so no
// line is wider than width unless a single word is. It stops early once
// enough(n) says n lines are enough.
func breakLines(fc *face, text string, width float64, wrap bool, enough func(n int) bool) []string {
	return splitParagraphs(text, wrap).lines(fc, width, enough)
}

// paragraphs is text split at newlines and, for wrapping, into words: once,
// for every font size it is tried at.
type paragraphs struct {
	text  []string
	words [][]string // nil when not wrapping
}

func splitParagraphs(text string, wrap bool) paragraphs {
	p := paragraphs{text: strings.Split(text, "\n")}
	if wrap {
		p.words = make([][]string, len(p.text))
		for i, para := range p.text {
			p.words[i] = strings.Fields(para)
		}
	}
	return p
}

// lines breaks the paragraphs into lines at fc's size, as breakLines does.
func (p paragraphs) lines(fc *face, width float64, enough func(n int) bool) []string {
	if p.words == nil {
		return p.text
	}
	var lines []string
	for _, words := range p.words {
		if len(words) == 0 {
			lines = append(lines, "")
			if enough(len(lines)) {
				return lines
			}
			continue
		}
		var line strings.Builder
		line.WriteString(words[0])
		measured := fc.extend(run{}, words[0])
		for _, w := range words[1:] {
			if candidate := fc.extend(fc.extendRune(measured, ' '), w); fixedToFloat(candidate.width) <= width {
				line.WriteByte(' ')
				line.WriteString(w)
				measured = candidate
				continue
			}
			lines = append(lines, line.String())
			if enough(len(lines)) {
				return lines
			}
			line.Reset()
			line.WriteString(w)
			measured = fc.extend(run{}, w)
		}
		lines = append(lines, line.String())
		if enough(len(lines)) {
			return lines
		}
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

// ellipsize shortens s until it and a trailing ellipsis fit in width,
// dropping spaces left at the end. Each prefix is measured once.
func ellipsize(fc *face, s string, width float64) string {
	runes := []rune(strings.TrimRight(s, " "))
	prefix := make([]run, len(runes)+1)
	for i, r := range runes {
		prefix[i+1] = fc.extendRune(prefix[i], r)
	}
	for n := len(runes); n > 0; n-- {
		if runes[n-1] == ' ' {
			continue
		}
		if fixedToFloat(fc.extend(prefix[n], ellipsis).width) <= width {
			return string(runes[:n]) + ellipsis
		}
	}
	if fc.width(ellipsis) > width {
		return ""
	}
	return ellipsis
}
