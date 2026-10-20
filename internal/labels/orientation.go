package labels

import "math"

// Which way a label goes through the printer, for labels that fit either
// way: as fits, not turned when that fits (FeedAuto, the default); its
// width across the roll; or its width along the roll.
const (
	FeedAuto   = "auto"
	FeedAcross = "across"
	FeedAlong  = "along"
)

// ValidFeed reports whether a feed is one of the feeds, or empty for the
// default.
func ValidFeed(feed string) bool {
	return feed == "" || feed == FeedAuto || feed == FeedAcross || feed == FeedAlong
}

// Turned is the template turned a quarter turn, clockwise or not, with
// everything on it: its width and height swap, as do an adaptive
// template's ranges, and each element turns about the label's center, its
// box keeping its size, so the design reads the same, turned. Anchors
// follow the sides they were on. Turning one way four times gives back the
// template.
func (t *Template) Turned(clockwise bool) *Template {
	out := *t
	out.Width, out.Height = t.Height, t.Width
	if t.Adapt != nil {
		a := *t.Adapt
		a.MinWidth, a.MaxWidth, a.MinHeight, a.MaxHeight = t.Adapt.MinHeight, t.Adapt.MaxHeight, t.Adapt.MinWidth, t.Adapt.MaxWidth
		out.Adapt = &a
	}
	if t.Guides != nil {
		// Guides across become guides down, and the other way.
		g := Guides{}
		for _, y := range t.Guides.Y {
			if clockwise {
				g.X = append(g.X, tidy(t.Height-y))
			} else {
				g.X = append(g.X, y)
			}
		}
		for _, x := range t.Guides.X {
			if clockwise {
				g.Y = append(g.Y, x)
			} else {
				g.Y = append(g.Y, tidy(t.Width-x))
			}
		}
		out.Guides = &g
	}
	if t.Grid != nil {
		// The grid turns too: its lines across become lines down, from the
		// side they come round to.
		g := *t.Grid
		g.X, g.Y = t.Grid.Y, t.Grid.X
		if clockwise {
			g.OffsetX, g.OffsetY = tidy(math.Mod(t.Height-t.Grid.OffsetY, t.Grid.Y)), t.Grid.OffsetX
		} else {
			g.OffsetX, g.OffsetY = t.Grid.OffsetY, tidy(math.Mod(t.Width-t.Grid.OffsetX, t.Grid.X))
		}
		if g.OffsetX >= g.X {
			g.OffsetX = 0
		}
		if g.OffsetY >= g.Y {
			g.OffsetY = 0
		}
		out.Grid = &g
	}
	out.Elements = make([]Element, len(t.Elements))
	for i, e := range t.Elements {
		cx, cy := e.X+e.Width/2, e.Y+e.Height/2
		if clockwise {
			// Across becomes down; down, from the far side, across.
			cx, cy = t.Height-cy, cx
			e.Rotation += 90
			e.AnchorX, e.AnchorY = reversed(e.AnchorY), e.AnchorX
		} else {
			cx, cy = cy, t.Width-cx
			e.Rotation += 270
			e.AnchorX, e.AnchorY = e.AnchorY, reversed(e.AnchorX)
		}
		e.Rotation = math.Mod(e.Rotation, 360)
		e.X, e.Y = tidy(cx-e.Width/2), tidy(cy-e.Height/2)
		out.Elements[i] = e
	}
	return &out
}

// reversed is an anchor as seen from the other side: start and end swap.
func reversed(anchor string) string {
	switch anchor {
	case AnchorStart:
		return AnchorEnd
	case AnchorEnd:
		return AnchorStart
	}
	return anchor
}

// tidy rounds away the noise of turning, to a millionth of a unit.
func tidy(v float64) float64 {
	return math.Round(v*1e6) / 1e6
}
