// Package render lays out a label template for one asset on a grid of whole
// printer dots, then draws that scene as SVG, PNG or PDF (README §4).
//
// Every backend draws the same scene, item by item, in the order the
// template lists its elements, each over those before it. Text is drawn as
// glyph outlines, so output never depends on the fonts installed where it is
// viewed.
package render

import (
	"fmt"
	"image"
	"math"
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
}

// Item is one thing drawn: exactly one of an image, a path or barcode
// modules.
type Item struct {
	Image *PlacedImage
	// Path is glyph outlines, filled with the nonzero winding rule.
	Path Path
	// Rects are dark barcode modules, merged into runs.
	Rects []Rect
}

// isPath reports whether the item is a filled path.
func (it *Item) isPath() bool { return it.Image == nil && it.Rects == nil }

// Rect is a dark rectangle in dots.
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
	}
	return n
}
