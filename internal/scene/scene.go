// Package scene lays out a location's contents as an isometric scene
// (README §3.7): where on its floor each stands, which is drawn over which,
// and where each is drawn on the page, by isolex's projection. Everything
// that draws a scene draws what Layout returns, so the page and an export
// agree.
package scene

import (
	"math"
	"sort"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex/ir"
)

// isolex's projection (its SPEC, §7): P(x, y, z) on the page, in output
// units, for a point in tile units.
const (
	S   = 40.0
	cos = 0.8660254037844387
	sin = 0.5
	// Pad is the margin kept around a scene, as isolex keeps around an
	// icon.
	Pad = 8.0
)

// P projects a point in tile units onto the page.
func P(x, y, z float64) (float64, float64) {
	return (x - y) * cos * S, (x+y)*sin*S - z*S
}

// Item is something in the location: its footprint in tiles, width,
// depth and height, zero for none (drawn as a box of one tile), and At,
// where someone placed it, if they did.
type Item struct {
	ID, Name string
	Size     [3]float64
	At       *[3]float64
}

// Placed is an item where it stands: its corner nearest the floor's
// origin at X, Y, Z in tiles, the space it takes, Size, and where its
// icon is drawn, translated to TX, TY. Arranged is set when it stands
// where someone placed it, rather than where it was packed, and Overlaps
// lists the items it shares space with, by name and then ID.
type Placed struct {
	ID       string     `json:"id"`
	X        float64    `json:"x"`
	Y        float64    `json:"y"`
	Z        float64    `json:"z"`
	Size     [3]float64 `json:"size"`
	TX       float64    `json:"tx"`
	TY       float64    `json:"ty"`
	Arranged bool       `json:"arranged,omitempty"`
	Overlaps []string   `json:"overlaps,omitempty"`
	center   float64
}

// Scene is a location laid out: its floor, Width by Depth tiles, the
// items in the order they are drawn, back to front, and the page's
// viewBox, [minx, miny, width, height].
type Scene struct {
	Floor   [2]float64 `json:"floor"`
	Items   []Placed   `json:"items"`
	Viewbox [4]float64 `json:"viewbox"`
}

// Layout lays items out on a floor. Those placed stand where they were
// placed. The rest are packed in rows, by name and then ID, around them:
// as wide as floor's width when it is given, and otherwise near square,
// but never narrower than the widest item; and as deep as it is given or
// the rows take. The floor grows to take anything placed beyond it. Each
// takes whole tiles, its footprint rounded up.
func Layout(floor *[3]float64, items []Item) Scene {
	sorted := append([]Item(nil), items...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := strings.ToLower(sorted[i].Name), strings.ToLower(sorted[j].Name)
		if a != b {
			return a < b
		}
		return sorted[i].ID < sorted[j].ID
	})
	tiles := func(v float64) float64 {
		if !(v > 0) || math.IsInf(v, 0) {
			return 1
		}
		return math.Ceil(v - 1e-9)
	}
	at := func(v float64) float64 {
		if !(v > 0) || math.IsInf(v, 0) {
			return 0
		}
		return v
	}
	var width, depth, area, widest float64
	sizes := make([][3]float64, len(sorted))
	for i, it := range sorted {
		w, d, h := tiles(it.Size[0]), tiles(it.Size[1]), it.Size[2]
		if !(h > 0) || math.IsInf(h, 0) {
			h = 1
		}
		sizes[i] = [3]float64{w, d, h}
		area += w * d
		widest = math.Max(widest, w)
	}
	if floor != nil && floor[0] > 0 {
		width, depth = tiles(floor[0]), tiles(floor[1])
	} else {
		width = math.Ceil(math.Sqrt(area))
	}
	width = math.Max(width, widest)

	placed := make([]Placed, len(sorted))
	// taken is the floor space placed items stand over, at any height
	var taken []Placed
	for i, it := range sorted {
		if it.At == nil {
			continue
		}
		p := Placed{ID: it.ID, X: at(it.At[0]), Y: at(it.At[1]), Z: at(it.At[2]), Size: sizes[i], Arranged: true}
		placed[i] = p
		taken = append(taken, p)
		width = math.Max(width, math.Ceil(p.X+p.Size[0]-1e-9))
		depth = math.Max(depth, math.Ceil(p.Y+p.Size[1]-1e-9))
	}
	free := func(x, y, w, d float64) bool {
		for _, o := range taken {
			if x < o.X+o.Size[0]-1e-9 && o.X < x+w-1e-9 && y < o.Y+o.Size[1]-1e-9 && o.Y < y+d-1e-9 {
				return false
			}
		}
		return true
	}
	var x, y, row float64
	for i, it := range sorted {
		if it.At != nil {
			continue
		}
		w, d := sizes[i][0], sizes[i][1]
		for {
			if x > 0 && x+w > width {
				x, y, row = 0, y+math.Max(row, 1), 0
				continue
			}
			if free(x, y, w, d) {
				break
			}
			x++
		}
		placed[i] = Placed{ID: it.ID, X: x, Y: y, Size: sizes[i]}
		taken = append(taken, placed[i])
		x += w
		row = math.Max(row, d)
	}
	depth = math.Max(depth, y+row)
	if width == 0 && depth == 0 {
		width, depth = 1, 1
	}
	for i := range placed {
		for j := range placed {
			if i != j && overlap(placed[i], placed[j]) {
				placed[i].Overlaps = append(placed[i].Overlaps, placed[j].ID)
			}
		}
	}
	return finish([2]float64{width, depth}, placed)
}

// overlap says whether two items share space: more than a face.
func overlap(a, b Placed) bool {
	const e = 1e-9
	for k, c := range [3][2]float64{{a.X, b.X}, {a.Y, b.Y}, {a.Z, b.Z}} {
		if c[0]+a.Size[k] <= c[1]+e || c[1]+b.Size[k] <= c[0]+e {
			return false
		}
	}
	return true
}

// finish orders placed items back to front and works out where each is
// drawn and the scene's viewBox.
func finish(floor [2]float64, placed []Placed) Scene {
	for i := range placed {
		p := &placed[i]
		p.TX, p.TY = P(p.X, p.Y, p.Z)
		p.center = p.X + p.Y + p.Z + (p.Size[0]+p.Size[1]+p.Size[2])/2
	}
	out := Scene{Floor: floor, Items: order(placed)}
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	grow := func(x, y, z float64) {
		px, py := P(x, y, z)
		minX, maxX = math.Min(minX, px), math.Max(maxX, px)
		minY, maxY = math.Min(minY, py), math.Max(maxY, py)
	}
	for _, c := range corners(0, 0, 0, floor[0], floor[1], 0) {
		grow(c[0], c[1], c[2])
	}
	for _, p := range out.Items {
		for _, c := range corners(p.X, p.Y, p.Z, p.Size[0], p.Size[1], p.Size[2]) {
			grow(c[0], c[1], c[2])
		}
	}
	out.Viewbox = [4]float64{minX - Pad, minY - Pad, maxX - minX + 2*Pad, maxY - minY + 2*Pad}
	return out
}

func corners(x, y, z, w, d, h float64) [8][3]float64 {
	return [8][3]float64{{x, y, z}, {x + w, y, z}, {x, y + d, z}, {x + w, y + d, z},
		{x, y, z + h}, {x + w, y, z + h}, {x, y + d, z + h}, {x + w, y + d, z + h}}
}

// behind says whether a is wholly behind or below b, so drawn first: on
// the far side of a plane between them.
func behind(a, b Placed) bool {
	const e = 1e-9
	return a.X+a.Size[0] <= b.X+e || a.Y+a.Size[1] <= b.Y+e || a.Z+a.Size[2] <= b.Z+e
}

// screen is the box a placed item covers on the page.
func screen(p Placed) (minX, minY, maxX, maxY float64) {
	minX, minY, maxX, maxY = math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, c := range corners(p.X, p.Y, p.Z, p.Size[0], p.Size[1], p.Size[2]) {
		px, py := P(c[0], c[1], c[2])
		minX, maxX = math.Min(minX, px), math.Max(maxX, px)
		minY, maxY = math.Min(minY, py), math.Max(maxY, py)
	}
	return
}

// order sorts placed items back to front: each drawn after every item it
// covers on the page and stands in front of, the rest farthest first, by
// the middle of their boxes and then by ID. Boxes that overlap each other,
// which give no answer, fall back to that too.
func order(placed []Placed) []Placed {
	n := len(placed)
	after := make([][]int, n) // after[i]: those drawn after i
	needs := make([]int, n)   // how many must be drawn before each
	for i := 0; i < n; i++ {
		ax0, ay0, ax1, ay1 := screen(placed[i])
		for j := i + 1; j < n; j++ {
			bx0, by0, bx1, by1 := screen(placed[j])
			if ax1 <= bx0 || bx1 <= ax0 || ay1 <= by0 || by1 <= ay0 {
				continue
			}
			ij, ji := behind(placed[i], placed[j]), behind(placed[j], placed[i])
			switch {
			case ij && !ji:
				after[i] = append(after[i], j)
				needs[j]++
			case ji && !ij:
				after[j] = append(after[j], i)
				needs[i]++
			}
		}
	}
	less := func(i, j int) bool {
		if placed[i].center != placed[j].center {
			return placed[i].center < placed[j].center
		}
		return placed[i].ID < placed[j].ID
	}
	done := make([]bool, n)
	out := make([]Placed, 0, n)
	for len(out) < n {
		next := -1
		for i := 0; i < n; i++ {
			if !done[i] && needs[i] == 0 && (next < 0 || less(i, next)) {
				next = i
			}
		}
		if next < 0 { // a cycle: the farthest left
			for i := 0; i < n; i++ {
				if !done[i] && (next < 0 || less(i, next)) {
					next = i
				}
			}
		}
		done[next] = true
		out = append(out, placed[next])
		for _, j := range after[next] {
			needs[j]--
		}
	}
	return out
}

// Box is what an item with no icon of its own, or a flat one, is drawn
// as: a plain box its size, its faces in the palette's body colors, as
// isolex draws a box, placed at the origin.
func Box(size [3]float64) []ir.Shape {
	w, d, h := size[0], size[1], size[2]
	pt := func(x, y, z float64) [2]float64 {
		px, py := P(x, y, z)
		return [2]float64{px, py}
	}
	face := func(fill string, pts ...[2]float64) ir.Shape {
		return ir.Shape{Kind: ir.KindPolygon, Points: pts, Fill: fill, Stroke: "outline", StrokeWidth: 1.2, FillOpacity: 1}
	}
	return []ir.Shape{
		face("body-left", pt(0, d, 0), pt(w, d, 0), pt(w, d, h), pt(0, d, h)),
		face("body-right", pt(w, 0, 0), pt(w, d, 0), pt(w, d, h), pt(w, 0, h)),
		face("body-top", pt(0, 0, h), pt(w, 0, h), pt(w, d, h), pt(0, d, h)),
	}
}
