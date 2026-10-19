package render

import (
	"image"
	"image/color"
	"image/png"
	"io"
	"math"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
	"golang.org/x/image/draw"
	"golang.org/x/image/vector"
)

// Raster draws the scene at its DPI on a white background, item by item. In
// mono mode every pixel is pure black or white, ready for a thermal print
// head: text is thresholded at 50% coverage and images are dithered. Barcode
// modules are whole dots in both modes, so they are never anti-aliased.
func (s *Scene) Raster(mono bool) *image.Gray {
	bounds := image.Rect(0, 0, s.Width, s.Height)
	img := image.NewGray(bounds)
	for i := range img.Pix {
		img.Pix[i] = 0xFF
	}

	for i := 0; i < len(s.Items); {
		it := &s.Items[i]
		switch {
		case it.Image != nil:
			drawImage(img, *it.Image, mono)
			i++
		case it.Rects != nil:
			fillRects(img, it.Rects, it.Paint)
			i++
		default:
			// Paths next to each other in one paint are filled together, so
			// where their edges meet, their coverage adds up as one shape's
			// would.
			j := i + 1
			for j < len(s.Items) && s.Items[j].isPath() && s.Items[j].Paint == it.Paint {
				j++
			}
			fillPaths(img, s.Items[i:j], mono, it.Paint)
			i = j
		}
	}
	return img
}

// fillPaths fills the items' paths in paint.
func fillPaths(img *image.Gray, items []Item, mono bool, paint Paint) {
	bounds := img.Bounds()
	z := vector.NewRasterizer(bounds.Dx(), bounds.Dy())
	for _, it := range items {
		tracePath(z, it.Path)
	}
	coverage := image.NewAlpha(bounds)
	z.Draw(coverage, bounds, image.Opaque, image.Point{})
	ink := uint8(0)
	if paint == White {
		ink = 0xFF
	}
	for i, a := range coverage.Pix {
		switch {
		case a == 0:
		case mono:
			if a >= 0x80 {
				img.Pix[i] = ink
			}
		case paint == White:
			img.Pix[i] += uint8((0xFF - uint32(img.Pix[i])) * uint32(a) / 0xFF)
		default:
			img.Pix[i] = uint8(uint32(img.Pix[i]) * uint32(0xFF-a) / 0xFF)
		}
	}
}

// fillRects fills whole-dot rectangles in paint.
func fillRects(img *image.Gray, rects []Rect, paint Paint) {
	v := uint8(0)
	if paint == White {
		v = 0xFF
	}
	for _, r := range rects {
		rect := image.Rect(r.X, r.Y, r.X+r.W, r.Y+r.H).Intersect(img.Bounds())
		for y := rect.Min.Y; y < rect.Max.Y; y++ {
			row := img.Pix[y*img.Stride:]
			for x := rect.Min.X; x < rect.Max.X; x++ {
				row[x] = v
			}
		}
	}
}

func tracePath(z *vector.Rasterizer, p Path) {
	open := false
	for _, seg := range p {
		a, b, c := seg.Pts[0], seg.Pts[1], seg.Pts[2]
		switch seg.Op {
		case MoveTo:
			if open {
				z.ClosePath()
			}
			z.MoveTo(float32(a.X), float32(a.Y))
			open = true
		case LineTo:
			z.LineTo(float32(a.X), float32(a.Y))
		case QuadTo:
			z.QuadTo(float32(a.X), float32(a.Y), float32(b.X), float32(b.Y))
		case CubeTo:
			z.CubeTo(float32(a.X), float32(a.Y), float32(b.X), float32(b.Y), float32(c.X), float32(c.Y))
		}
	}
	if open {
		z.ClosePath()
	}
}

// drawImage scales pi onto dst over what is already drawn, which shows
// through where the image is transparent. In mono mode the image's box is
// then Floyd–Steinberg dithered; what is already drawn there is pure black
// and white, which dithering leaves as it is.
func drawImage(dst *image.Gray, pi PlacedImage, mono bool) {
	if pi.Rotation != 0 {
		drawTurnedImage(dst, pi, mono)
		return
	}
	rect := image.Rect(pi.X, pi.Y, pi.X+pi.W, pi.Y+pi.H)
	scaled := image.NewGray(image.Rect(0, 0, pi.W, pi.H))
	for y := 0; y < pi.H; y++ {
		for x := 0; x < pi.W; x++ {
			v := uint8(0xFF)
			if p := (image.Point{X: x + pi.X, Y: y + pi.Y}); p.In(dst.Bounds()) {
				v = dst.GrayAt(p.X, p.Y).Y
			}
			scaled.Pix[y*scaled.Stride+x] = v
		}
	}
	draw.CatmullRom.Scale(scaled, scaled.Bounds(), pi.Image, pi.Image.Bounds(), draw.Over, nil)

	if mono {
		dither(scaled)
	}
	clip := rect.Intersect(dst.Bounds())
	for y := clip.Min.Y; y < clip.Max.Y; y++ {
		for x := clip.Min.X; x < clip.Max.X; x++ {
			dst.SetGray(x, y, scaled.GrayAt(x-pi.X, y-pi.Y))
		}
	}
}

// dither reduces img to pure black and white with Floyd–Steinberg error
// diffusion.
func dither(img *image.Gray) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	errs := make([]int, (w+2)*(h+1))
	at := func(x, y int) *int { return &errs[y*(w+2)+x+1] }
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			old := int(img.Pix[y*img.Stride+x]) + *at(x, y)
			out := 0
			if old >= 128 {
				out = 255
			}
			img.Pix[y*img.Stride+x] = uint8(out)
			diff := old - out
			*at(x+1, y) += diff * 7 / 16
			*at(x-1, y+1) += diff * 3 / 16
			*at(x, y+1) += diff * 5 / 16
			*at(x+1, y+1) += diff * 1 / 16
		}
	}
}

// WritePNG encodes the scene as an 8-bit grayscale PNG, or a 1-bit PNG in
// mono mode.
func WritePNG(w io.Writer, s *Scene, mono bool) error {
	img := s.Raster(mono)
	if !mono {
		return png.Encode(w, img)
	}
	bw := image.NewPaletted(img.Bounds(), color.Palette{color.Black, color.White})
	for i, v := range img.Pix {
		if v != 0 {
			bw.Pix[i] = 1
		}
	}
	return png.Encode(w, bw)
}

// drawTurnedImage draws an image turned by pi.Rotation about (pi.Cx, pi.Cy):
// scaled to its rectangle, then each dot it covers takes the color found
// there turned back, between the four nearest pixels, over what is already
// drawn. In mono mode the dots it covers are then dithered, as drawImage
// does.
func drawTurnedImage(dst *image.Gray, pi PlacedImage, mono bool) {
	if pi.W <= 0 || pi.H <= 0 {
		return
	}
	scaled := image.NewRGBA(image.Rect(0, 0, pi.W, pi.H))
	draw.CatmullRom.Scale(scaled, scaled.Bounds(), pi.Image, pi.Image.Bounds(), draw.Src, nil)

	sin, cos := labels.SinCos(pi.Rotation)
	turned := func(x, y float64) (float64, float64) {
		dx, dy := x-pi.Cx, y-pi.Cy
		return pi.Cx + dx*cos - dy*sin, pi.Cy + dx*sin + dy*cos
	}
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, c := range [][2]float64{{0, 0}, {1, 0}, {1, 1}, {0, 1}} {
		x, y := turned(float64(pi.X)+c[0]*float64(pi.W), float64(pi.Y)+c[1]*float64(pi.H))
		minX, minY, maxX, maxY = math.Min(minX, x), math.Min(minY, y), math.Max(maxX, x), math.Max(maxY, y)
	}
	area := image.Rect(int(math.Floor(minX)), int(math.Floor(minY)), int(math.Ceil(maxX)), int(math.Ceil(maxY))).Intersect(dst.Bounds())
	if area.Empty() {
		return
	}

	// Premultiplied gray and alpha at a point of the scaled image, from 0 to
	// 0xFFFF, between its four nearest pixels; none outside it.
	sample := func(u, v float64) (float64, float64) {
		u, v = u-0.5, v-0.5
		x0, y0 := int(math.Floor(u)), int(math.Floor(v))
		fx, fy := u-float64(x0), v-float64(y0)
		var g, a float64
		for _, n := range [4]struct {
			x, y int
			w    float64
		}{{x0, y0, (1 - fx) * (1 - fy)}, {x0 + 1, y0, fx * (1 - fy)}, {x0, y0 + 1, (1 - fx) * fy}, {x0 + 1, y0 + 1, fx * fy}} {
			if n.w == 0 || n.x < 0 || n.y < 0 || n.x >= pi.W || n.y >= pi.H {
				continue
			}
			r, gr, b, al := scaled.At(n.x, n.y).RGBA()
			g += n.w * (0.299*float64(r) + 0.587*float64(gr) + 0.114*float64(b))
			a += n.w * float64(al)
		}
		return g, a
	}

	region := image.NewGray(area)
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			back := float64(dst.GrayAt(x, y).Y)
			// The dot's center, turned back onto the image.
			u, v := unturn(pi, sin, cos, float64(x)+0.5, float64(y)+0.5)
			g, a := sample(u-float64(pi.X), v-float64(pi.Y))
			out := (g/0x101 + back*(0xFFFF-a)/0xFFFF)
			region.SetGray(x, y, color.Gray{Y: uint8(math.Round(math.Min(255, math.Max(0, out))))})
		}
	}
	if mono {
		dither(region)
	}
	draw.Draw(dst, area, region, area.Min, draw.Src)
}

// unturn turns a point back by the image's rotation about its center.
func unturn(pi PlacedImage, sin, cos, x, y float64) (float64, float64) {
	dx, dy := x-pi.Cx, y-pi.Cy
	return pi.Cx + dx*cos + dy*sin, pi.Cy - dx*sin + dy*cos
}
