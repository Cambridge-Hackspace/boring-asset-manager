package vision

import (
	"image"
	"image/color"
	"math"
)

// Rectify is a photo as seen from straight above the work area: workW ×
// workH mm at pxPerMM pixels a millimeter, each pixel taken from where h
// (the photo's pixels to the bed) puts it in the photo. What the photo does
// not show is dark gray.
func Rectify(img image.Image, h Homography, workW, workH, pxPerMM float64) (*image.RGBA, error) {
	back, err := h.Inverse()
	if err != nil {
		return nil, err
	}
	w, ht := max(1, int(math.Round(workW*pxPerMM))), max(1, int(math.Round(workH*pxPerMM)))
	out := image.NewRGBA(image.Rect(0, 0, w, ht))
	b := img.Bounds()
	missing := color.RGBA{48, 48, 48, 255}
	for y := 0; y < ht; y++ {
		for x := 0; x < w; x++ {
			p := back.Apply(Point{X: (float64(x) + 0.5) / pxPerMM, Y: (float64(y) + 0.5) / pxPerMM})
			// Pixel i covers [i, i+1): its center is at i + 0.5.
			u, v := p.X-0.5, p.Y-0.5
			if u < -0.5 || v < -0.5 || u > float64(b.Dx())-0.5 || v > float64(b.Dy())-0.5 || math.IsNaN(u) {
				out.SetRGBA(x, y, missing)
				continue
			}
			out.SetRGBA(x, y, bilinear(img, b, u, v))
		}
	}
	return out, nil
}

// bilinear samples a photo between its pixels' centers.
func bilinear(img image.Image, b image.Rectangle, u, v float64) color.RGBA {
	x0, y0 := int(math.Floor(u)), int(math.Floor(v))
	fx, fy := u-float64(x0), v-float64(y0)
	at := func(x, y int) [3]float64 {
		x = min(max(x, 0), b.Dx()-1)
		y = min(max(y, 0), b.Dy()-1)
		r, g, bl, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
		return [3]float64{float64(r >> 8), float64(g >> 8), float64(bl >> 8)}
	}
	a, c, d, e := at(x0, y0), at(x0+1, y0), at(x0, y0+1), at(x0+1, y0+1)
	var out [3]uint8
	for i := range out {
		top := a[i]*(1-fx) + c[i]*fx
		bottom := d[i]*(1-fx) + e[i]*fx
		out[i] = uint8(math.Round(top*(1-fy) + bottom*fy))
	}
	return color.RGBA{out[0], out[1], out[2], 255}
}

// RectifyScale is the pixels a millimeter a work area is rectified at so
// that its longer side is at most maxSide pixels, and at most 8.
func RectifyScale(workW, workH float64, maxSide int) float64 {
	return math.Min(8, float64(maxSide)/math.Max(workW, workH))
}
