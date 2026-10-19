package render

import (
	"image"
	"image/color"
	"image/png"
	"io"

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
			fillRects(img, it.Rects)
			i++
		default:
			// Paths next to each other are filled together, so where their
			// edges meet, their coverage adds up as one shape's would.
			j := i + 1
			for j < len(s.Items) && s.Items[j].isPath() {
				j++
			}
			fillPaths(img, s.Items[i:j], mono)
			i = j
		}
	}
	return img
}

// fillPaths fills the items' paths in black.
func fillPaths(img *image.Gray, items []Item, mono bool) {
	bounds := img.Bounds()
	z := vector.NewRasterizer(bounds.Dx(), bounds.Dy())
	for _, it := range items {
		tracePath(z, it.Path)
	}
	coverage := image.NewAlpha(bounds)
	z.Draw(coverage, bounds, image.Opaque, image.Point{})
	for i, a := range coverage.Pix {
		switch {
		case a == 0:
		case mono:
			if a >= 0x80 {
				img.Pix[i] = 0
			}
		default:
			img.Pix[i] = uint8(uint32(img.Pix[i]) * uint32(0xFF-a) / 0xFF)
		}
	}
}

// fillRects fills whole-dot rectangles in black.
func fillRects(img *image.Gray, rects []Rect) {
	for _, r := range rects {
		rect := image.Rect(r.X, r.Y, r.X+r.W, r.Y+r.H).Intersect(img.Bounds())
		for y := rect.Min.Y; y < rect.Max.Y; y++ {
			row := img.Pix[y*img.Stride:]
			for x := rect.Min.X; x < rect.Max.X; x++ {
				row[x] = 0
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
