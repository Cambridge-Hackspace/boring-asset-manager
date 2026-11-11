package vision

import (
	"bytes"
	"cmp"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg" // Cameras send JPEGs.
	_ "image/png"
	"math"
	"slices"
)

// MaxPhotoSide bounds the side a photo is worked on at: a larger one is
// shrunk to it first, which is ample for marks and outlines a few
// millimeters across, and keeps the work quick.
const MaxPhotoSide = 1600

// Decode reads a photo, JPEG or PNG.
func Decode(data []byte) (image.Image, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("the camera's photo could not be read: %w", err)
	}
	return img, nil
}

// gray is a photo's brightness, 0 to 255, at a scale: a pixel of it covers
// scale pixels of the photo each way.
type gray struct {
	w, h  int
	pix   []float32
	scale float64
}

func (g *gray) at(x, y int) float32 { return g.pix[y*g.w+x] }

// toPhoto is a pixel of the gray image, by its center, as the photo's
// pixel coordinates.
func (g *gray) toPhoto(x, y float64) Point {
	return Point{X: x * g.scale, Y: y * g.scale}
}

// grayOf is a photo's brightness, shrunk by a whole factor, averaging, until
// it fits MaxPhotoSide.
func grayOf(img image.Image) *gray {
	b := img.Bounds()
	f := 1
	for max(b.Dx(), b.Dy())/f > MaxPhotoSide {
		f++
	}
	g := &gray{w: b.Dx() / f, h: b.Dy() / f, scale: float64(f)}
	g.pix = make([]float32, g.w*g.h)
	for y := 0; y < g.h; y++ {
		for x := 0; x < g.w; x++ {
			var sum float32
			for dy := 0; dy < f; dy++ {
				for dx := 0; dx < f; dx++ {
					sum += luma(img.At(b.Min.X+x*f+dx, b.Min.Y+y*f+dy))
				}
			}
			g.pix[y*g.w+x] = sum / float32(f*f)
		}
	}
	return g
}

// luma is a color's brightness, 0 to 255 (Rec. 601).
func luma(c color.Color) float32 {
	r, g, b, _ := c.RGBA()
	return (0.299*float32(r) + 0.587*float32(g) + 0.114*float32(b)) / 257
}

// MeanBrightness is a photo's average brightness, 0 to 255, sampled: a
// camera that answers with a black frame (docs/printing.md#xtool) gives
// nearly 0.
func MeanBrightness(img image.Image) float64 {
	b := img.Bounds()
	step := max(1, max(b.Dx(), b.Dy())/200)
	var sum float64
	n := 0
	for y := b.Min.Y; y < b.Max.Y; y += step {
		for x := b.Min.X; x < b.Max.X; x += step {
			sum += float64(luma(img.At(x, y)))
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

// blurred is g averaged over a square 2r+1 pixels across, which smooths
// noise and the grain of wood and the bed.
func (g *gray) blurred(r int) *gray {
	if r <= 0 {
		return g
	}
	out := &gray{w: g.w, h: g.h, scale: g.scale, pix: make([]float32, len(g.pix))}
	// Summed-area table, one larger each way.
	w1 := g.w + 1
	sat := make([]float64, w1*(g.h+1))
	for y := 0; y < g.h; y++ {
		var row float64
		for x := 0; x < g.w; x++ {
			row += float64(g.at(x, y))
			sat[(y+1)*w1+x+1] = sat[y*w1+x+1] + row
		}
	}
	for y := 0; y < g.h; y++ {
		y0, y1 := max(0, y-r), min(g.h, y+r+1)
		for x := 0; x < g.w; x++ {
			x0, x1 := max(0, x-r), min(g.w, x+r+1)
			sum := sat[y1*w1+x1] - sat[y0*w1+x1] - sat[y1*w1+x0] + sat[y0*w1+x0]
			out.pix[y*g.w+x] = float32(sum / float64((x1-x0)*(y1-y0)))
		}
	}
	return out
}

// otsu is the brightness that best splits g's pixels in two (Otsu's
// method), over those the mask allows, or all when it is nil.
func (g *gray) otsu(mask []bool) float32 {
	var hist [256]float64
	total := 0.0
	for i, v := range g.pix {
		if mask != nil && !mask[i] {
			continue
		}
		hist[min(255, max(0, int(v)))]++
		total++
	}
	var sum float64
	for i, n := range hist {
		sum += float64(i) * n
	}
	var below, sumBelow, best float64
	threshold := 128
	for t := 0; t < 256; t++ {
		below += hist[t]
		if below == 0 {
			continue
		}
		above := total - below
		if above == 0 {
			break
		}
		sumBelow += float64(t) * hist[t]
		mb, ma := sumBelow/below, (sum-sumBelow)/above
		between := below * above * (mb - ma) * (mb - ma)
		if between > best {
			best, threshold = between, t
		}
	}
	return float32(threshold) + 0.5
}

// blob is a connected set of pixels.
type blob struct {
	area                   int
	sumX, sumY             float64
	minX, minY, maxX, maxY int
	// edge is set when it touches the image's edge.
	edge bool
	// pixels are its pixels' indices, kept only when asked for.
	pixels []int
}

func (b *blob) center() (float64, float64) {
	return b.sumX / float64(b.area), b.sumY / float64(b.area)
}

// blobs labels the 4-connected sets of pixels in. keep keeps the pixels of
// blobs at least that large.
func blobs(w, h int, in []bool, keep int) []*blob {
	seen := make([]bool, len(in))
	var out []*blob
	stack := make([]int, 0, 1024)
	var pixels []int
	for start := range in {
		if !in[start] || seen[start] {
			continue
		}
		b := &blob{minX: w, minY: h, maxX: -1, maxY: -1}
		pixels = pixels[:0]
		stack = append(stack[:0], start)
		seen[start] = true
		for len(stack) > 0 {
			i := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			x, y := i%w, i/w
			b.area++
			b.sumX += float64(x)
			b.sumY += float64(y)
			b.minX, b.maxX = min(b.minX, x), max(b.maxX, x)
			b.minY, b.maxY = min(b.minY, y), max(b.maxY, y)
			if x == 0 || y == 0 || x == w-1 || y == h-1 {
				b.edge = true
			}
			pixels = append(pixels, i)
			for _, j := range [4]int{i - 1, i + 1, i - w, i + w} {
				if j < 0 || j >= len(in) || seen[j] || !in[j] {
					continue
				}
				if (j == i-1 && x == 0) || (j == i+1 && x == w-1) {
					continue
				}
				seen[j] = true
				stack = append(stack, j)
			}
		}
		if b.area >= keep && keep > 0 {
			b.pixels = append([]int(nil), pixels...)
		}
		out = append(out, b)
	}
	return out
}

// convexHull is the convex hull of points, counterclockwise as the image
// is drawn (y down), by Andrew's monotone chain.
func convexHull(ps []Point) []Point {
	if len(ps) < 3 {
		return append([]Point(nil), ps...)
	}
	sorted := append([]Point(nil), ps...)
	sortPoints(sorted)
	cross := func(o, a, b Point) float64 { return (a.X-o.X)*(b.Y-o.Y) - (a.Y-o.Y)*(b.X-o.X) }
	hull := make([]Point, 0, 2*len(sorted))
	for _, p := range sorted {
		for len(hull) >= 2 && cross(hull[len(hull)-2], hull[len(hull)-1], p) <= 0 {
			hull = hull[:len(hull)-1]
		}
		hull = append(hull, p)
	}
	lower := len(hull) + 1
	for i := len(sorted) - 2; i >= 0; i-- {
		p := sorted[i]
		for len(hull) >= lower && cross(hull[len(hull)-2], hull[len(hull)-1], p) <= 0 {
			hull = hull[:len(hull)-1]
		}
		hull = append(hull, p)
	}
	return hull[:len(hull)-1]
}

func sortPoints(ps []Point) {
	slices.SortFunc(ps, func(a, b Point) int {
		if c := cmp.Compare(a.X, b.X); c != 0 {
			return c
		}
		return cmp.Compare(a.Y, b.Y)
	})
}

// round is v to a hundredth.
func round(v float64) float64 {
	return math.Round(v*100) / 100
}
