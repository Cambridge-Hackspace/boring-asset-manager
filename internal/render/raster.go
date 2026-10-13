package render

import (
	"image"
	"image/color"
	"image/png"
	"io"

	"golang.org/x/image/draw"
	"golang.org/x/image/vector"
)

// Raster draws the scene at its DPI on a white background. In mono mode every
// pixel is pure black or white, ready for a thermal print head: text is
// thresholded at 50% coverage and images are dithered. Barcode modules are
// whole dots in both modes, so they are never anti-aliased.
func (s *Scene) Raster(mono bool) *image.Gray {
	bounds := image.Rect(0, 0, s.Width, s.Height)
	img := image.NewGray(bounds)
	for i := range img.Pix {
		img.Pix[i] = 0xFF
	}

	for _, pi := range s.Images {
		drawImage(img, pi, mono)
	}

	if len(s.Paths) > 0 {
		z := vector.NewRasterizer(s.Width, s.Height)
		for _, p := range s.Paths {
			tracePath(z, p)
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

	for _, r := range s.Rects {
		rect := image.Rect(r.X, r.Y, r.X+r.W, r.Y+r.H).Intersect(bounds)
		for y := rect.Min.Y; y < rect.Max.Y; y++ {
			row := img.Pix[y*img.Stride:]
			for x := rect.Min.X; x < rect.Max.X; x++ {
				row[x] = 0
			}
		}
	}
	return img
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

// drawImage scales pi onto dst, compositing transparency over white. In mono
// mode the result is Floyd–Steinberg dithered.
func drawImage(dst *image.Gray, pi PlacedImage, mono bool) {
	rect := image.Rect(pi.X, pi.Y, pi.X+pi.W, pi.Y+pi.H)
	scaled := image.NewGray(image.Rect(0, 0, pi.W, pi.H))
	for i := range scaled.Pix {
		scaled.Pix[i] = 0xFF
	}
	draw.CatmullRom.Scale(scaled, scaled.Bounds(), pi.Image, pi.Image.Bounds(), draw.Over, nil)

	if mono {
		dither(scaled)
	}
	clip := rect.Intersect(dst.Bounds())
	for y := clip.Min.Y; y < clip.Max.Y; y++ {
		for x := clip.Min.X; x < clip.Max.X; x++ {
			v := scaled.GrayAt(x-pi.X, y-pi.Y).Y
			// Darken only, so overlapping images never lighten what is below.
			if v < dst.GrayAt(x, y).Y {
				dst.SetGray(x, y, color.Gray{Y: v})
			}
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
