package render

import (
	"math"
)

// A graph of followed links (docs/data-model.md#following-links), drawn on
// a document's page as the web interface draws it: a box for each asset in
// its column and row, and an arrow for each link, from what needs to what
// is needed. The layout is the caller's (package linkgraph); a graph too
// big for the page is drawn smaller to fit it.

// GraphNode is an asset in a graph: its name, and a note, such as its
// condition, under it.
type GraphNode struct {
	Name, Note string
	Layer, Row int
	// Root is the asset the graph is of, drawn bold; Marked one whose
	// condition is not working, with a black mark; Faded one archived.
	Root, Marked, Faded bool
}

// GraphEdge is a link, from one node to another by index. Dashed draws a
// link of a symmetric type, which has no arrow.
type GraphEdge struct {
	From, To int
	Dashed   bool
}

// GraphDrawing is a graph as drawn: its title, nodes and edges, and lines
// under it, such as a key.
type GraphDrawing struct {
	Title string
	Nodes []GraphNode
	Edges []GraphEdge
	Notes []string
}

// Sizes of a graph's boxes and the gaps between them, in points, before it
// is drawn smaller to fit.
const (
	graphBoxW   = 140
	graphBoxH   = 30
	graphColGap = 56
	graphRowGap = 12
	graphLine   = 0.75
	graphArrow  = 5
)

// Graph draws a graph on a page of its own, under its title, as large as
// it fits, up to full size.
func (d *Document) Graph(g GraphDrawing) {
	if !d.empty() {
		d.newPage()
	}
	if g.Title != "" {
		d.Heading(g.Title)
	}
	for _, n := range g.Notes {
		d.Paragraph(n, Gray25)
	}
	cols, rows := 0, map[int]int{}
	for _, n := range g.Nodes {
		cols = max(cols, n.Layer+1)
		rows[n.Layer] = max(rows[n.Layer], n.Row+1)
	}
	tallest := 0
	for _, r := range rows {
		tallest = max(tallest, r)
	}
	if cols == 0 {
		return
	}
	fullW := float64(cols)*pt(graphBoxW) + float64(cols-1)*pt(graphColGap)
	fullH := float64(tallest)*pt(graphBoxH) + float64(tallest-1)*pt(graphRowGap)
	scale := math.Min(1, math.Min((d.right()-d.left())/fullW, (d.bottom()-d.y)/fullH))
	boxW, boxH := pt(graphBoxW)*scale, pt(graphBoxH)*scale
	colStep, rowStep := boxW+pt(graphColGap)*scale, boxH+pt(graphRowGap)*scale
	// Each column is centered on the tallest.
	at := func(n GraphNode) (x, y float64) {
		offset := float64(tallest-rows[n.Layer]) / 2
		return d.left() + float64(n.Layer)*colStep, d.y + (float64(n.Row)+offset)*rowStep
	}
	line := math.Max(pt(graphLine)*scale, pt(0.25))
	arrow := pt(graphArrow) * math.Max(scale, 0.4)
	for _, e := range g.Edges {
		if e.From < 0 || e.From >= len(g.Nodes) || e.To < 0 || e.To >= len(g.Nodes) {
			continue
		}
		fx, fy := at(g.Nodes[e.From])
		tx, ty := at(g.Nodes[e.To])
		var a, b Point
		switch {
		case g.Nodes[e.To].Layer > g.Nodes[e.From].Layer:
			a, b = Point{fx + boxW, fy + boxH/2}, Point{tx, ty + boxH/2}
		case g.Nodes[e.To].Layer < g.Nodes[e.From].Layer:
			a, b = Point{fx, fy + boxH/2}, Point{tx + boxW, ty + boxH/2}
		case ty > fy:
			a, b = Point{fx + boxW/2, fy + boxH}, Point{tx + boxW/2, ty}
		default:
			a, b = Point{fx + boxW/2, fy}, Point{tx + boxW/2, ty + boxH}
		}
		paint := Gray50
		if e.Dashed {
			d.page.Items = append(d.page.Items, Item{Path: dashed(a, b, line, pt(3)*math.Max(scale, 0.4)), Paint: paint})
			continue
		}
		d.page.Items = append(d.page.Items, Item{Path: stroke([]Point{a, b}, line), Paint: paint})
		d.page.Items = append(d.page.Items, Item{Path: arrowHead(a, b, arrow), Paint: paint})
	}
	size := math.Max(docBody*scale, 2.5)
	small := math.Max(docSmall*scale, 2)
	pad := pt(docCellPad) * scale
	for _, n := range g.Nodes {
		x, y := at(n)
		edge := line
		if n.Root {
			edge = line * 2.5
		}
		d.box(x, y, boxW, boxH, White)
		d.box(x, y, boxW, edge, Black)
		d.box(x, y+boxH-edge, boxW, edge, Black)
		d.box(x, y, edge, boxH, Black)
		d.box(x+boxW-edge, y, edge, boxH, Black)
		textX := x + pad + edge
		if n.Marked {
			m := pt(5) * scale
			d.box(textX, y+(boxH-m)/2, m, m, Black)
			textX += m + pad
		}
		paint := Black
		if n.Faded {
			paint = Gray50
		}
		width := x + boxW - pad - edge - textX
		if n.Note == "" {
			d.text(d.fit(n.Name, width, size, n.Root), textX, y+(boxH+d.ascent(size))/2-pt(0.5)*scale, size, n.Root, paint)
			continue
		}
		base := y + (boxH-d.lineHeight(size)-d.lineHeight(small))/2 + d.ascent(size)
		d.text(d.fit(n.Name, width, size, n.Root), textX, base, size, n.Root, paint)
		d.text(d.fit(n.Note, width, small, false), textX, base+d.lineHeight(small), small, false, Gray25)
	}
	d.y += fullH * scale
}

// arrowHead is a triangle at b, pointing along a to b.
func arrowHead(a, b Point, size float64) Path {
	dx, dy := b.X-a.X, b.Y-a.Y
	l := math.Hypot(dx, dy)
	if l == 0 {
		return nil
	}
	ux, uy := dx/l, dy/l
	back := Point{b.X - ux*size, b.Y - uy*size}
	left := Point{back.X - uy*size/2, back.Y + ux*size/2}
	right := Point{back.X + uy*size/2, back.Y - ux*size/2}
	return polygonContour([]Point{b, left, right}, true).appendTo(nil)
}

// dashed is a line from a to b in dashes dash long, with gaps as long.
func dashed(a, b Point, width, dash float64) Path {
	l := math.Hypot(b.X-a.X, b.Y-a.Y)
	if l == 0 || dash <= 0 {
		return nil
	}
	var p Path
	for s := 0.0; s < l; s += 2 * dash {
		e := math.Min(s+dash, l)
		p = append(p, stroke([]Point{
			{a.X + (b.X-a.X)*s/l, a.Y + (b.Y-a.Y)*s/l},
			{a.X + (b.X-a.X)*e/l, a.Y + (b.Y-a.Y)*e/l},
		}, width)...)
	}
	return p
}
