package simcamera

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"math/rand/v2"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/vision"
)

// Scene is what a synthetic photo shows: a honeycomb bed, darker than
// what lies on it, lit unevenly, through a camera looking down at an
// angle, with a little noise.
type Scene struct {
	Width, Height int
	// Camera maps millimeters on the bed to the photo's pixels.
	Camera vision.Homography
	// Scrap is light scrap, X, Y, width and height in mm, with Marks
	// engraved on it; nil for none.
	Scrap *[4]float64
	Marks *vision.MarkLayout
	// Piece is a light workpiece; nil for none.
	Piece *Truth
	Seed  uint64
}

var (
	wood   = [3]float64{222, 196, 152}
	burned = [3]float64{58, 40, 28}
	cell   = [3]float64{52, 54, 60}
	rim    = [3]float64{98, 98, 104}
)

// Render draws a scene, sampling each pixel four times.
func Render(s Scene) *image.RGBA {
	back, err := s.Camera.Inverse()
	if err != nil {
		panic(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, s.Width, s.Height))
	noise := rand.New(rand.NewPCG(s.Seed, s.Seed^0x9e3779b97f4a7c15))
	for y := 0; y < s.Height; y++ {
		for x := 0; x < s.Width; x++ {
			var c [3]float64
			for _, d := range [4][2]float64{{0.25, 0.25}, {0.75, 0.25}, {0.25, 0.75}, {0.75, 0.75}} {
				p := back.Apply(vision.Point{X: float64(x) + d[0], Y: float64(y) + d[1]})
				k := s.at(p)
				for i := range c {
					c[i] += k[i] / 4
				}
			}
			// Light falls off from the top right, and toward the edges.
			u, v := float64(x)/float64(s.Width), float64(y)/float64(s.Height)
			r2 := (u-0.5)*(u-0.5) + (v-0.5)*(v-0.5)
			light := 0.8 + 0.22*u - 0.12*v - 0.35*r2
			var out [3]uint8
			for i := range c {
				out[i] = uint8(math.Round(math.Min(255, math.Max(0, c[i]*light+5*noise.NormFloat64()))))
			}
			img.SetRGBA(x, y, color.RGBA{out[0], out[1], out[2], 255})
		}
	}
	return img
}

// at is the color of the scene at a point on the bed.
func (s Scene) at(p vision.Point) [3]float64 {
	if s.Piece != nil {
		t := s.Piece
		a := t.AngleDeg * math.Pi / 180
		dx, dy := p.X-t.Center.X, p.Y-t.Center.Y
		u := dx*math.Cos(a) + dy*math.Sin(a)
		v := -dx*math.Sin(a) + dy*math.Cos(a)
		if math.Abs(u) <= t.Width/2 && math.Abs(v) <= t.Height/2 {
			return grain(u, v)
		}
	}
	if r := s.Scrap; r != nil && p.X >= r[0] && p.Y >= r[1] && p.X <= r[0]+r[2] && p.Y <= r[1]+r[3] {
		if l := s.Marks; l != nil {
			for i, m := range l.Marks {
				d := l.Diameter
				if i == 0 {
					d = l.OriginDiameter
				}
				if math.Hypot(p.X-m.X, p.Y-m.Y) <= d/2 {
					return burned
				}
			}
		}
		return grain(p.X, p.Y)
	}
	return honeycomb(p)
}

// grain is wood, with faint grain along it.
func grain(u, v float64) [3]float64 {
	k := 1 + 0.03*math.Sin(v*1.7+2*math.Sin(u*0.3))
	return [3]float64{wood[0] * k, wood[1] * k, wood[2] * k}
}

// honeycomb is a bed of hexagonal cells 6 mm across, their rims lighter
// than the holes but darker than wood.
func honeycomb(p vision.Point) [3]float64 {
	const a = 6.0
	row := a * math.Sqrt(3) / 2
	j0 := int(math.Floor(p.Y / row))
	d1, d2 := math.Inf(1), math.Inf(1)
	for j := j0 - 1; j <= j0+2; j++ {
		off := 0.0
		if j%2 != 0 {
			off = a / 2
		}
		i0 := int(math.Floor((p.X - off) / a))
		for i := i0 - 1; i <= i0+2; i++ {
			d := math.Hypot(p.X-(float64(i)*a+off), p.Y-float64(j)*row)
			switch {
			case d < d1:
				d1, d2 = d, d1
			case d < d2:
				d2 = d
			}
		}
	}
	if d2-d1 < 0.6 {
		return rim
	}
	return cell
}

// JPEG encodes a photo as a camera sends it.
func JPEG(img image.Image) []byte {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		panic(err)
	}
	return buf.Bytes()
}
