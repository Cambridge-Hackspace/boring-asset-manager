package printers

import (
	"image"
	"math"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
)

// Placing a label on a workpiece (docs/printing.md#placing-the-label): by
// the template's anchor, centered or in a corner, and turned with the
// workpiece when it lies turned on the bed, as the camera measures it.

// AnchorAt is where a label's top left corner goes on a workpiece, from the
// workpiece's top left corner, by an anchor: centered, or in a corner,
// inset from both its sides.
func AnchorAt(a labels.WorkpieceAnchor, pieceW, pieceH, labelW, labelH float64) PointMM {
	right, bottom := pieceW-labelW-a.InsetMM, pieceH-labelH-a.InsetMM
	switch a.Corner {
	case labels.CornerTopLeft:
		return PointMM{X: a.InsetMM, Y: a.InsetMM}
	case labels.CornerTopRight:
		return PointMM{X: right, Y: a.InsetMM}
	case labels.CornerBottomLeft:
		return PointMM{X: a.InsetMM, Y: bottom}
	case labels.CornerBottomRight:
		return PointMM{X: right, Y: bottom}
	}
	return PointMM{X: (pieceW - labelW) / 2, Y: (pieceH - labelH) / 2}
}

// Turn turns points on the bed clockwise (as it is drawn, Y down) about a
// center, by degrees.
type Turn struct {
	Center PointMM
	Deg    float64
}

// Apply turns a point.
func (t Turn) Apply(p PointMM) PointMM {
	s, c := math.Sincos(t.Deg * math.Pi / 180)
	dx, dy := p.X-t.Center.X, p.Y-t.Center.Y
	return PointMM{X: t.Center.X + dx*c - dy*s, Y: t.Center.Y + dx*s + dy*c}
}

// Back turns a point back.
func (t Turn) Back(p PointMM) PointMM {
	return Turn{Center: t.Center, Deg: -t.Deg}.Apply(p)
}

// Corners are a rectangle's corners, turned, clockwise from its top left.
func (t Turn) Corners(r RectMM) [4]PointMM {
	return [4]PointMM{
		t.Apply(PointMM{X: r.X, Y: r.Y}), t.Apply(PointMM{X: r.X + r.Width, Y: r.Y}),
		t.Apply(PointMM{X: r.X + r.Width, Y: r.Y + r.Height}), t.Apply(PointMM{X: r.X, Y: r.Y + r.Height}),
	}
}

// Box is the square around a rectangle, turned.
func (t Turn) Box(r RectMM) RectMM {
	cs := t.Corners(r)
	x0, y0, x1, y1 := cs[0].X, cs[0].Y, cs[0].X, cs[0].Y
	for _, p := range cs[1:] {
		x0, y0, x1, y1 = math.Min(x0, p.X), math.Min(y0, p.Y), math.Max(x1, p.X), math.Max(y1, p.Y)
	}
	return RectMM{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
}

// turnRaster is a 1-bit engraving covering placed, turned within box at the
// same resolution, white (not burned) around it, and box widened to the
// whole dots it is drawn in.
func turnRaster(img *image.Gray, placed RectMM, box RectMM, t Turn) (*image.Gray, RectMM) {
	b := img.Bounds()
	sx, sy := float64(b.Dx())/placed.Width, float64(b.Dy())/placed.Height
	w, h := max(1, int(math.Ceil(box.Width*sx-1e-6))), max(1, int(math.Ceil(box.Height*sy-1e-6)))
	out := image.NewGray(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			q := t.Back(PointMM{X: box.X + (float64(x)+0.5)/sx, Y: box.Y + (float64(y)+0.5)/sy})
			u, v := int(math.Floor((q.X-placed.X)*sx)), int(math.Floor((q.Y-placed.Y)*sy))
			if u < 0 || v < 0 || u >= b.Dx() || v >= b.Dy() {
				out.Pix[y*out.Stride+x] = 0xff
				continue
			}
			out.Pix[y*out.Stride+x] = img.Pix[(b.Min.Y+v-img.Rect.Min.Y)*img.Stride+(b.Min.X+u-img.Rect.Min.X)]
		}
	}
	box.Width, box.Height = float64(w)/sx, float64(h)/sy
	return out, box
}
