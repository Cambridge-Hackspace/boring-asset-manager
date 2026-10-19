package labels

import (
	"fmt"
	"sort"
)

// How elements are lined up: their left edges, centers or right edges
// across, or their tops, middles or bottoms down; or spread evenly across
// or down.
const (
	AlignToLeft      = "left"
	AlignToCenter    = "center"
	AlignToRight     = "right"
	AlignToTop       = "top"
	AlignToMiddle    = "middle"
	AlignToBottom    = "bottom"
	DistributeAcross = "across"
	DistributeDown   = "down"
)

// Arrange lines elements up or spreads them out, as they lie turned: each
// aligned with the others, or with the label when toLabel is set; or, with
// distribute, the gaps between them made even, the outermost staying where
// they are. Only their positions change.
func (t *Template) Arrange(indices []int, align, distribute string, toLabel bool) error {
	if (align == "") == (distribute == "") {
		return fmt.Errorf("give align or distribute")
	}
	seen := map[int]bool{}
	for _, i := range indices {
		if i < 0 || i >= len(t.Elements) {
			return fmt.Errorf("no element %d: there are %d", i, len(t.Elements))
		}
		if seen[i] {
			return fmt.Errorf("element %d is named twice", i)
		}
		seen[i] = true
	}
	type box struct{ x, y, w, h float64 }
	boxes := make([]box, len(indices))
	for k, i := range indices {
		x, y, w, h := t.Elements[i].Bounds()
		boxes[k] = box{x, y, w, h}
	}
	move := func(k int, dx, dy float64) {
		e := &t.Elements[indices[k]]
		e.X, e.Y = tidy(e.X+dx), tidy(e.Y+dy)
	}

	if distribute != "" {
		if len(indices) < 3 {
			return fmt.Errorf("spreading out takes three elements or more")
		}
		across := distribute == DistributeAcross
		if !across && distribute != DistributeDown {
			return fmt.Errorf(`distribute must be "across" or "down"`)
		}
		start := func(b box) float64 {
			if across {
				return b.x
			}
			return b.y
		}
		size := func(b box) float64 {
			if across {
				return b.w
			}
			return b.h
		}
		order := make([]int, len(indices))
		for k := range order {
			order[k] = k
		}
		sort.SliceStable(order, func(a, b int) bool {
			return start(boxes[order[a]])+size(boxes[order[a]])/2 < start(boxes[order[b]])+size(boxes[order[b]])/2
		})
		first, last := boxes[order[0]], boxes[order[len(order)-1]]
		span := start(last) + size(last) - start(first)
		total := 0.0
		for _, b := range boxes {
			total += size(b)
		}
		gap := (span - total) / float64(len(order)-1)
		at := start(first)
		for _, k := range order {
			d := at - start(boxes[k])
			if across {
				move(k, d, 0)
			} else {
				move(k, 0, d)
			}
			at += size(boxes[k]) + gap
		}
		return nil
	}

	// What they line up with: the label, or the box round them all.
	tx0, ty0, tx1, ty1 := 0.0, 0.0, t.Width, t.Height
	if !toLabel {
		if len(indices) < 2 {
			return fmt.Errorf("lining up takes two elements or more, or one with the label")
		}
		tx0, ty0, tx1, ty1 = boxes[0].x, boxes[0].y, boxes[0].x+boxes[0].w, boxes[0].y+boxes[0].h
		for _, b := range boxes[1:] {
			tx0, ty0 = min(tx0, b.x), min(ty0, b.y)
			tx1, ty1 = max(tx1, b.x+b.w), max(ty1, b.y+b.h)
		}
	}
	for k, b := range boxes {
		switch align {
		case AlignToLeft:
			move(k, tx0-b.x, 0)
		case AlignToCenter:
			move(k, (tx0+tx1)/2-(b.x+b.w/2), 0)
		case AlignToRight:
			move(k, tx1-(b.x+b.w), 0)
		case AlignToTop:
			move(k, 0, ty0-b.y)
		case AlignToMiddle:
			move(k, 0, (ty0+ty1)/2-(b.y+b.h/2))
		case AlignToBottom:
			move(k, 0, ty1-(b.y+b.h))
		default:
			return fmt.Errorf(`align must be "left", "center", "right", "top", "middle" or "bottom"`)
		}
	}
	return nil
}
