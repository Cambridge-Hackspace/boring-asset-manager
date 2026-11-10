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
// Every ink but white is drawn black, as a printer of black alone prints
// it (RasterInks draws in more).
func (s *Scene) Raster(mono bool) *image.Gray {
	return s.draw(1, mono, Paint.gray, nil)[0]
}

// RasterColor draws the scene as Raster does, anti-aliased, but in its
// inks' own colors: a preview of a label as designed.
func (s *Scene) RasterColor() *image.RGBA {
	planes := s.draw(3, false, Paint.Color, nil)
	out := image.NewRGBA(planes[0].Rect)
	for i := range planes[0].Pix {
		out.Pix[4*i], out.Pix[4*i+1], out.Pix[4*i+2], out.Pix[4*i+3] = planes[0].Pix[i], planes[1].Pix[i], planes[2].Pix[i], 0xFF
	}
	return out
}

// RasterInks draws the scene dot for dot in a palette's inks, as a printer
// or display with those inks shows it (docs/labels.md#inks): each dot one
// of them, indexed in the palette's order. Paths are thresholded at 50%
// coverage, as in mono mode; an ink the palette lacks is drawn black; and
// images are dithered into the palette, Floyd–Steinberg, to the nearest
// ink. In black and white it is Raster in mono mode, dot for dot.
func (s *Scene) RasterInks(p labels.Palette) *image.Paletted {
	if !p.Valid() {
		p = labels.PaletteBW
	}
	inks := p.Colors()
	palette := make(color.Palette, len(inks))
	index := make(map[color.RGBA]uint8, len(inks))
	for i, c := range inks {
		palette[i] = c
		index[c] = uint8(i)
	}
	out := image.NewPaletted(image.Rect(0, 0, s.Width, s.Height), palette)
	if p == labels.PaletteBW {
		// Black is the first ink, white the second.
		for i, v := range s.Raster(true).Pix {
			if v != 0 {
				out.Pix[i] = 1
			}
		}
		return out
	}
	planes := s.draw(3, true, func(q Paint) color.RGBA { return q.In(p).Color() }, inks)
	for i := range out.Pix {
		c := color.RGBA{planes[0].Pix[i], planes[1].Pix[i], planes[2].Pix[i], 0xFF}
		k, ok := index[c]
		if !ok {
			// a gray, in a document: the nearest ink
			k = uint8(nearestInk(inks, [3]int{int(c.R), int(c.G), int(c.B)}))
		}
		out.Pix[i] = k
	}
	return out
}

// UsesInks reports whether the scene draws in an ink besides black and
// white.
func (s *Scene) UsesInks() bool {
	for _, it := range s.Items {
		if it.Paint != Black && it.Paint != White && it.Paint.Ink() != "" {
			return true
		}
	}
	return false
}

// gray is a paint as Raster draws it: its level, in each channel.
func (p Paint) gray() color.RGBA {
	l := p.Level()
	return color.RGBA{l, l, l, 0xFF}
}

// draw draws the scene on n planes, white to begin with: one of gray, or
// three of red, green and blue, each paint in each plane as colorOf has
// it. Given inks, images are dithered into them as they are drawn; in one
// plane, in mono mode, into black and white.
func (s *Scene) draw(n int, mono bool, colorOf func(Paint) color.RGBA, inks []color.RGBA) []*image.Gray {
	s = s.outlined()
	bounds := image.Rect(0, 0, s.Width, s.Height)
	planes := make([]*image.Gray, n)
	for k := range planes {
		planes[k] = image.NewGray(bounds)
		for i := range planes[k].Pix {
			planes[k].Pix[i] = 0xFF
		}
	}
	levels := func(p Paint) []uint8 {
		c := colorOf(p)
		return []uint8{c.R, c.G, c.B}[:n]
	}

	for i := 0; i < len(s.Items); {
		it := &s.Items[i]
		switch {
		case it.Image != nil:
			if n == 1 {
				drawImage(planes[0], *it.Image, mono)
			} else {
				drawColorImage(planes, *it.Image, inks)
			}
			i++
		case it.Rects != nil:
			for k, v := range levels(it.Paint) {
				fillRects(planes[k], it.Rects, v)
			}
			i++
		default:
			// Paths next to each other in one paint are filled together, so
			// where their edges meet, their coverage adds up as one shape's
			// would.
			j := i + 1
			for !it.EvenOdd && j < len(s.Items) && s.Items[j].isPath() && s.Items[j].Paint == it.Paint && !s.Items[j].EvenOdd {
				j++
			}
			coverage := cover(bounds, s.Items[i:j])
			for k, v := range levels(it.Paint) {
				blend(planes[k], coverage, mono, v)
			}
			i = j
		}
	}
	return planes
}

// cover is the coverage of the items' paths, filled together.
func cover(bounds image.Rectangle, items []Item) *image.Alpha {
	coverage := image.NewAlpha(bounds)
	if len(items) == 1 && items[0].EvenOdd {
		evenOdd(coverage, items[0].Path)
	} else {
		paths := make([]Path, len(items))
		for i, it := range items {
			paths[i] = it.Path
		}
		fill(coverage, paths)
	}
	return coverage
}

// blend paints a level over img where coverage covers it: in mono mode
// wherever it covers at least half a dot, otherwise blended over what is
// beneath by coverage.
func blend(img *image.Gray, coverage *image.Alpha, mono bool, ink uint8) {
	for i, a := range coverage.Pix {
		switch {
		case a == 0:
		case mono:
			if a >= 0x80 {
				img.Pix[i] = ink
			}
		default:
			img.Pix[i] = uint8((uint32(img.Pix[i])*uint32(0xFF-a) + uint32(ink)*uint32(a)) / 0xFF)
		}
	}
}

// evenOdd is the coverage of a path filled by the even-odd rule: each of
// its outlines filled alone, and where they overlap, cancelling, as an odd
// number of crossings is inside and an even number out.
func evenOdd(coverage *image.Alpha, p Path) {
	bounds := coverage.Bounds()
	one := image.NewAlpha(bounds)
	for start := 0; start < len(p); {
		end := start + 1
		for end < len(p) && p[end].Op != MoveTo {
			end++
		}
		clear(one.Pix)
		fill(one, []Path{p[start:end]})
		for i, a := range one.Pix {
			if a == 0 {
				continue
			}
			// c XOR a, as coverage: c + a - 2ca.
			c := uint32(coverage.Pix[i])
			coverage.Pix[i] = uint8(c + uint32(a) - 2*c*uint32(a)/0xFF)
		}
		start = end
	}
}

// fillRects fills whole-dot rectangles with a level.
func fillRects(img *image.Gray, rects []Rect, v uint8) {
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

// tile is the largest square the rasterizer fills at once. Up to 512 dots
// across, x/image/vector adds up coverage in integers, which its assembly
// on amd64 and its Go everywhere else (arm64, and the editor's live
// renderer in WebAssembly) do identically; above it, in floating point,
// which they round differently, flipping dots on the edges of text. Filling
// in tiles keeps every label, at any size, the same dot for dot wherever
// it is drawn.
const tile = 512

// fill adds the coverage of paths, filled together by the nonzero rule, to
// coverage, which is blank where they fall: tile by tile, each path traced
// only into the tiles its control points reach.
func fill(coverage *image.Alpha, paths []Path) {
	bounds := coverage.Bounds()
	boxes := make([][4]float64, len(paths))
	for i, p := range paths {
		boxes[i] = pathBox(p)
	}
	var z vector.Rasterizer
	for y := bounds.Min.Y; y < bounds.Max.Y; y += tile {
		for x := bounds.Min.X; x < bounds.Max.X; x += tile {
			r := image.Rect(x, y, min(x+tile, bounds.Max.X), min(y+tile, bounds.Max.Y))
			z.Reset(r.Dx(), r.Dy())
			traced := false
			for i, p := range paths {
				b := boxes[i]
				// A path wholly to the left of a tile still adds to it, as its
				// edges' winding carries across; above, below or to the right,
				// it cannot.
				if b[1] >= float64(r.Max.Y) || b[3] <= float64(r.Min.Y) || b[0] >= float64(r.Max.X) {
					continue
				}
				tracePath(&z, p, float64(x), float64(y))
				traced = true
			}
			if traced {
				z.Draw(coverage, r, image.Opaque, image.Point{})
			}
		}
	}
}

// pathBox is the box around a path's points and control points, which holds
// the whole curve: left, top, right, bottom.
func pathBox(p Path) [4]float64 {
	b := [4]float64{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for _, seg := range p {
		n := 1
		switch seg.Op {
		case QuadTo:
			n = 2
		case CubeTo:
			n = 3
		}
		for _, pt := range seg.Pts[:n] {
			b[0], b[1] = math.Min(b[0], pt.X), math.Min(b[1], pt.Y)
			b[2], b[3] = math.Max(b[2], pt.X), math.Max(b[3], pt.Y)
		}
	}
	return b
}

// tracePath traces p into z, moved by (-ox, -oy): z's top left is that
// point of the label.
func tracePath(z *vector.Rasterizer, p Path, ox, oy float64) {
	open := false
	at := func(pt Point) (float32, float32) { return float32(pt.X - ox), float32(pt.Y - oy) }
	for _, seg := range p {
		ax, ay := at(seg.Pts[0])
		bx, by := at(seg.Pts[1])
		cx, cy := at(seg.Pts[2])
		switch seg.Op {
		case MoveTo:
			if open {
				z.ClosePath()
			}
			z.MoveTo(ax, ay)
			open = true
		case LineTo:
			z.LineTo(ax, ay)
		case QuadTo:
			z.QuadTo(ax, ay, bx, by)
		case CubeTo:
			z.CubeTo(ax, ay, bx, by, cx, cy)
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
// mono mode. A scene in inks besides black and white is an 8-bit color PNG
// of them as designed, except in mono mode, which is as a printer of black
// alone prints it.
func WritePNG(w io.Writer, s *Scene, mono bool) error {
	if !mono && s.UsesInks() {
		return png.Encode(w, s.RasterColor())
	}
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

// WriteInksPNG encodes the scene as an indexed PNG in a palette's inks, as
// RasterInks draws it.
func WriteInksPNG(w io.Writer, s *Scene, p labels.Palette) error {
	return png.Encode(w, s.RasterInks(p))
}

// channel is one channel of an image, red, green or blue by c, as a gray
// image of the same alpha.
type channel struct {
	image.Image
	c int
}

func (ch channel) ColorModel() color.Model { return color.RGBA64Model }

func (ch channel) At(x, y int) color.Color {
	r, g, b, a := ch.Image.At(x, y).RGBA()
	v := uint16([3]uint32{r, g, b}[ch.c])
	return color.RGBA64{v, v, v, uint16(a)}
}

// drawColorImage draws an image on planes of red, green and blue, each as
// drawImage draws one channel of it in gray, over what is already drawn.
// Given inks, the dots it covers are then dithered into them.
func drawColorImage(planes []*image.Gray, pi PlacedImage, inks []color.RGBA) {
	for k, plane := range planes {
		one := pi
		one.Image = channel{pi.Image, k}
		drawImage(plane, one, false)
	}
	if inks == nil {
		return
	}
	area := image.Rect(pi.X, pi.Y, pi.X+pi.W, pi.Y+pi.H).Intersect(planes[0].Bounds())
	if pi.Rotation != 0 {
		area = turnedArea(pi, planes[0].Bounds())
	}
	ditherInks(planes, area, inks)
}

// ditherInks reduces the dots in r of planes of red, green and blue to
// inks with Floyd–Steinberg error diffusion: each the ink nearest it, by
// the sum of the squares of the differences in each channel, the first
// listed where two are as near.
func ditherInks(planes []*image.Gray, r image.Rectangle, inks []color.RGBA) {
	w, h := r.Dx(), r.Dy()
	if w <= 0 || h <= 0 {
		return
	}
	errs := make([][3]int, (w+2)*(h+1))
	at := func(x, y int) *[3]int { return &errs[y*(w+2)+x+1] }
	stride := planes[0].Stride
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			off := (r.Min.Y+y)*stride + r.Min.X + x
			var want [3]int
			for k := range want {
				want[k] = int(planes[k].Pix[off]) + at(x, y)[k]
			}
			ink := inks[nearestInk(inks, want)]
			got := [3]int{int(ink.R), int(ink.G), int(ink.B)}
			for k := range want {
				planes[k].Pix[off] = uint8(got[k])
				diff := want[k] - got[k]
				at(x+1, y)[k] += diff * 7 / 16
				at(x-1, y+1)[k] += diff * 3 / 16
				at(x, y+1)[k] += diff * 5 / 16
				at(x+1, y+1)[k] += diff * 1 / 16
			}
		}
	}
}

// nearestInk is the index of the ink nearest a color.
func nearestInk(inks []color.RGBA, c [3]int) int {
	best, least := 0, -1
	for i, ink := range inks {
		dr, dg, db := c[0]-int(ink.R), c[1]-int(ink.G), c[2]-int(ink.B)
		if d := dr*dr + dg*dg + db*db; least < 0 || d < least {
			best, least = i, d
		}
	}
	return best
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
	area := turnedArea(pi, dst.Bounds())
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

// turnedArea is the dots of bounds an image turned by its rotation may
// cover: the box around its turned corners.
func turnedArea(pi PlacedImage, bounds image.Rectangle) image.Rectangle {
	if pi.W <= 0 || pi.H <= 0 {
		return image.Rectangle{}
	}
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
	return image.Rect(int(math.Floor(minX)), int(math.Floor(minY)), int(math.Ceil(maxX)), int(math.Ceil(maxY))).Intersect(bounds)
}
