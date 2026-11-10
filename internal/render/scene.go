// Package render lays out a label template for one asset on a grid of whole
// printer dots, then draws that scene as SVG, PNG or PDF (docs/labels.md).
//
// Every backend draws the same scene, item by item, in the order the
// template lists its elements, each over those before it. Text is drawn as
// glyph outlines, so output never depends on the fonts installed where it is
// viewed.
package render

import (
	"fmt"
	"image"
	"image/color"
	"math"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
)

// Scene is a laid-out label, measured in dots at DPI.
type Scene struct {
	Width, Height int
	DPI           int
	// Units is the template's unit ("mm" or "in"), used for the physical size
	// written into SVG output.
	Units string

	// Items are drawn in order, the first at the back.
	Items []Item
	// Codes says how each code was laid out, by element index, before it
	// was turned.
	Codes map[int]CodePlacement
	// TextSizes is the size each text was drawn at, in points, by element
	// index: its font_size, or smaller where it shrank, or as it filled its
	// box.
	TextSizes map[int]float64
}

// Item is one thing drawn: exactly one of an image, a path, rectangles of
// whole dots, such as barcode modules, or, in documents, text.
type Item struct {
	Image *PlacedImage
	// Text is a run of text, in documents (see TextRun).
	Text *TextRun
	// Path is outlines, such as glyphs, filled with the nonzero winding
	// rule.
	Path Path
	// Rects are rectangles of whole dots, such as barcode modules merged
	// into runs.
	Rects []Rect
	// Paint is what a path or rectangles are filled with.
	Paint Paint
	// EvenOdd fills a path by the even-odd rule: where its outlines cross
	// it an odd number of times.
	EvenOdd bool
}

// Paint is black, or white, which paints over what is beneath it, or
// another of a label's inks (docs/labels.md#inks), or, in documents
// (docs/reports.md#pdf), a shade of gray.
type Paint uint8

const (
	Black Paint = iota
	White
	// Gray90 is the lightest gray, for shading; Gray25 the darkest.
	Gray90
	Gray75
	Gray50
	Gray25
	// The inks besides black and white.
	Red
	Yellow
	Green
	Blue
	Orange
)

// inkPaints are the paints of inks, by name.
var inkPaints = map[string]Paint{
	labels.InkBlack: Black, labels.InkWhite: White, labels.InkRed: Red, labels.InkYellow: Yellow,
	labels.InkGreen: Green, labels.InkBlue: Blue, labels.InkOrange: Orange,
}

// Ink is the ink a paint is, or "" for a gray.
func (p Paint) Ink() string {
	for name, paint := range inkPaints {
		if paint == p {
			return name
		}
	}
	return ""
}

// Color is the color a paint is shown in: its ink's, or its gray.
func (p Paint) Color() color.RGBA {
	if ink := p.Ink(); ink != "" {
		return labels.InkColor(ink)
	}
	l := p.Level()
	return color.RGBA{l, l, l, 0xFF}
}

// In is the paint as a device with the palette draws it: an ink it lacks
// is drawn black, and grays stay as they are.
func (p Paint) In(palette labels.Palette) Paint {
	if ink := p.Ink(); ink != "" && !palette.Has(ink) {
		return Black
	}
	return p
}

// Level is how light a paint is, from 0, black, to 255, white. In gray,
// every ink but white is drawn black, as a device of black and white
// draws it.
func (p Paint) Level() uint8 {
	switch p {
	case White:
		return 0xFF
	case Gray90:
		return 230
	case Gray75:
		return 191
	case Gray50:
		return 128
	case Gray25:
		return 64
	}
	return 0
}

// isPath reports whether the item is a filled path.
func (it *Item) isPath() bool { return it.Image == nil && it.Rects == nil && it.Text == nil }

// Rect is a rectangle in dots.
type Rect struct {
	X, Y, W, H int
}

// PlacedImage is a bitmap scaled into a rectangle of dots, and turned
// clockwise by Rotation degrees about (Cx, Cy) when that is not 0. An image
// turned a whole number of quarter turns is turned dot for dot instead, and
// has no Rotation.
type PlacedImage struct {
	X, Y, W, H int
	Image      image.Image
	Rotation   float64
	Cx, Cy     float64
}

// Point is a position in dots; fractional values come from glyph outlines.
type Point struct {
	X, Y float64
}

// SegmentOp is a path drawing operation.
type SegmentOp int

const (
	MoveTo SegmentOp = iota
	LineTo
	QuadTo
	CubeTo
)

// Segment is one path operation. MoveTo and LineTo use Pts[0]; QuadTo uses
// Pts[0] (control) and Pts[1]; CubeTo uses all three.
type Segment struct {
	Op  SegmentOp
	Pts [3]Point
}

// Path is a sequence of closed contours, each starting with MoveTo.
type Path []Segment

// LayoutError reports why an element could not be laid out for an asset.
type LayoutError struct {
	// Path names the element, such as "elements[0]".
	Path    string
	Message string
}

func (e *LayoutError) Error() string {
	return fmt.Sprintf("%s: %s", e.Path, e.Message)
}

// PhysicalSize returns the scene's size in its template units, derived from
// whole dots so that every backend agrees.
func (s *Scene) PhysicalSize() (w, h float64) {
	perDot := 1 / float64(s.DPI)
	if s.Units == "mm" {
		perDot *= 25.4
	}
	return float64(s.Width) * perDot, float64(s.Height) * perDot
}

func round(v float64) int {
	return int(math.Round(v))
}

// Parts counts what the scene holds: path segments, barcode rectangles and
// images. Memory and the time to write the scene out grow with it.
func (s *Scene) Parts() int {
	n := 0
	for _, it := range s.Items {
		n += len(it.Rects) + len(it.Path)
		if it.Image != nil {
			n++
		}
		if it.Text != nil {
			n += len(it.Text.Text)
		}
	}
	return n
}
