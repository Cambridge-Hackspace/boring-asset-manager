package render

import (
	"errors"
	"fmt"
	"image"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
)

// Documents are pages of headings, tables and charts drawn as scenes, for
// exporting reports and asset lists as PDF (docs/reports.md#pdf), with
// the fonts labels embed, so that a document looks the same wherever it
// was made.

// docDPI is a document page's dots per inch: a dot is a tenth of a point,
// fine enough for rules and boxes of whole dots.
const docDPI = 720

// pt is a length in points, in dots.
func pt(v float64) float64 { return v * docDPI / 72 }

// Sizes of a document's text, rules and spacing, in points.
const (
	docMargin   = 36
	docBody     = 9
	docSmall    = 7.5
	docHeading  = 13
	docCellPad  = 3
	docRule     = 0.5
	docImageRow = 36  // a row's height when it has pictures
	docPhotoW   = 40  // a photo's column
	docLabelW   = 72  // a label preview's column
	docChartH   = 190 // a chart's height
)

// PageSize is a page's size, upright, in points.
type PageSize struct {
	Name string
	W, H float64
}

var (
	Letter = PageSize{Name: "letter", W: 612, H: 792}
	A4     = PageSize{Name: "a4", W: 595.28, H: 841.89}
)

// PageSizeNamed is the page size called name, letter or a4.
func PageSizeNamed(name string) (PageSize, bool) {
	switch strings.ToLower(name) {
	case Letter.Name:
		return Letter, true
	case A4.Name:
		return A4, true
	}
	return PageSize{}, false
}

// DocumentOptions are a document's pages and what heads each.
type DocumentOptions struct {
	Size      PageSize
	Landscape bool
	// Instance is at the top left of every page, Title at the top right,
	// and Made under them: when the document was made, and by whom.
	Instance, Title, Made string
}

// Document lays out blocks top to bottom, page after page.
type Document struct {
	opts  DocumentOptions
	pages []*Scene
	page  *Scene
	// w and h are the page's size, and y where the next block goes, in
	// dots.
	w, h, y float64
	faces   map[faceKey]*face
	// log is what was laid out, in order: "page", "header", "title …" and
	// "row", for tests of where pages break.
	log []string
}

type faceKey struct {
	bold bool
	size float64
}

// NewDocument starts a document on its first page.
func NewDocument(opts DocumentOptions) (*Document, error) {
	if opts.Size.W == 0 {
		opts.Size = Letter
	}
	w, h := opts.Size.W, opts.Size.H
	if opts.Landscape {
		w, h = h, w
	}
	d := &Document{opts: opts, w: math.Round(pt(w)), h: math.Round(pt(h)), faces: map[faceKey]*face{}}
	if _, err := loadFonts(); err != nil {
		return nil, err
	}
	d.newPage()
	return d, nil
}

func (d *Document) face(bold bool, size float64) *face {
	key := faceKey{bold, size}
	if fc := d.faces[key]; fc != nil {
		return fc
	}
	weight := labels.WeightNormal
	if bold {
		weight = labels.WeightBold
	}
	fonts, _ := loadFonts()
	fc, err := newFace(fonts[fontKey{labels.FamilySans, weight}], size, docDPI)
	if err != nil {
		// The embedded fonts parsed as they loaded, so a face of them is
		// made at any size.
		panic(err)
	}
	d.faces[key] = fc
	return fc
}

// left, right and bottom are the edges blocks stay within, in dots.
func (d *Document) left() float64   { return pt(docMargin) }
func (d *Document) right() float64  { return d.w - pt(docMargin) }
func (d *Document) bottom() float64 { return d.h - pt(docMargin) - pt(docSmall) - pt(6) }

// text draws s with its baseline at (x, y), in dots.
func (d *Document) text(s string, x, y float64, size float64, bold bool, paint Paint) {
	if s == "" {
		return
	}
	d.page.Items = append(d.page.Items, Item{Text: &TextRun{Bold: bold, Size: size, X: x, Y: y, Text: s}, Paint: paint})
}

// textRight draws s ending at x.
func (d *Document) textRight(s string, x, y float64, size float64, bold bool, paint Paint) {
	d.text(s, x-d.face(bold, size).width(s), y, size, bold, paint)
}

// box fills a rectangle, in dots, rounded to whole dots.
func (d *Document) box(x, y, w, h float64, paint Paint) {
	r := Rect{X: round(x), Y: round(y), W: round(x+w) - round(x), H: round(y+h) - round(y)}
	if r.W <= 0 || r.H <= 0 {
		return
	}
	d.page.Items = append(d.page.Items, Item{Rects: []Rect{r}, Paint: paint})
}

// fit cuts s short with an ellipsis to fit width.
func (d *Document) fit(s string, width, size float64, bold bool) string {
	fc := d.face(bold, size)
	if fc.width(s) <= width {
		return s
	}
	return ellipsize(fc, s, width)
}

// lineHeight is the distance between baselines at size.
func (d *Document) lineHeight(size float64) float64 {
	return fixedToFloat(d.face(false, size).metrics.Height)
}

func (d *Document) ascent(size float64) float64 {
	return fixedToFloat(d.face(false, size).metrics.Ascent)
}

// newPage starts a page with its header.
func (d *Document) newPage() {
	d.page = &Scene{Width: int(d.w), Height: int(d.h), DPI: docDPI, Units: "in"}
	d.pages = append(d.pages, d.page)
	d.log = append(d.log, "page")
	top := pt(docMargin)
	base := top + d.ascent(docSmall)
	half := (d.right() - d.left()) / 2
	d.text(d.fit(d.opts.Instance, half-pt(6), docSmall, true), d.left(), base, docSmall, true, Black)
	d.textRight(d.fit(d.opts.Title, half-pt(6), docSmall, true), d.right(), base, docSmall, true, Black)
	base += d.lineHeight(docSmall)
	d.text(d.fit(d.opts.Made, half*2, docSmall, false), d.left(), base, docSmall, false, Gray25)
	rule := base + pt(4)
	d.box(d.left(), rule, d.right()-d.left(), pt(docRule), Gray50)
	d.y = rule + pt(12)
}

// room makes room for h dots, starting a page if there is not that much
// left on this one, unless this one has nothing yet. It reports whether a
// page was started.
func (d *Document) room(h float64) bool {
	if d.y+h <= d.bottom() || d.empty() {
		return false
	}
	d.newPage()
	return true
}

// empty reports whether nothing but the header is on the page.
func (d *Document) empty() bool {
	return d.y == d.firstY()
}

func (d *Document) firstY() float64 {
	return pt(docMargin) + d.ascent(docSmall) + d.lineHeight(docSmall) + pt(4) + pt(12)
}

// Heading is a section's title.
func (d *Document) Heading(s string) {
	h := d.lineHeight(docHeading)
	// A heading stays with what follows it.
	d.room(h + 4*d.lineHeight(docBody))
	d.text(d.fit(s, d.right()-d.left(), docHeading, true), d.left(), d.y+d.ascent(docHeading), docHeading, true, Black)
	d.y += h + pt(4)
}

// Paragraph is text broken into lines to fit the page's width.
func (d *Document) Paragraph(s string, paint Paint) {
	lines := breakLines(d.face(false, docBody), s, d.right()-d.left(), true, func(int) bool { return false })
	for _, line := range lines {
		d.room(d.lineHeight(docBody))
		d.text(line, d.left(), d.y+d.ascent(docBody), docBody, false, paint)
		d.y += d.lineHeight(docBody)
	}
	d.y += pt(6)
}

// Table is rows under a heading row, in groups, each with a title when
// the table is grouped. Pictures are columns of images before the text,
// headed by PictureColumns.
type Table struct {
	Columns        []string
	PictureColumns []string
	Groups         []TableGroup
}

// TableGroup is rows under a title, or with none.
type TableGroup struct {
	Title string
	Rows  []TableRow
}

// TableRow is a row's cells and pictures, a picture nil where it has none.
type TableRow struct {
	Cells    []string
	Pictures []image.Image
}

// Table draws a table, breaking it across pages: the heading row is
// repeated on each, and a group's title kept with its first rows.
func (d *Document) Table(t Table) {
	pad := pt(docCellPad)
	widths, numeric := d.columns(t)
	pictureW := make([]float64, len(t.PictureColumns))
	for i, name := range t.PictureColumns {
		pictureW[i] = pt(docPhotoW)
		if strings.EqualFold(name, "label") {
			pictureW[i] = pt(docLabelW)
		}
	}
	textRow := d.lineHeight(docBody) + 2*pad
	rowH := textRow
	if len(t.PictureColumns) > 0 {
		rowH = math.Max(rowH, pt(docImageRow))
	}

	header := func() {
		d.log = append(d.log, "header")
		d.box(d.left(), d.y, d.right()-d.left(), textRow, Gray90)
		x := d.left()
		base := d.y + pad + d.ascent(docBody)
		for i, name := range t.PictureColumns {
			d.text(d.fit(name, pictureW[i]-2*pad, docBody, true), x+pad, base, docBody, true, Black)
			x += pictureW[i]
		}
		for i, name := range t.Columns {
			name = d.fit(name, widths[i]-2*pad, docBody, true)
			if numeric[i] {
				d.textRight(name, x+widths[i]-pad, base, docBody, true, Black)
			} else {
				d.text(name, x+pad, base, docBody, true, Black)
			}
			x += widths[i]
		}
		d.y += textRow
	}
	title := func(s string) {
		d.log = append(d.log, "title "+s)
		d.text(d.fit(s, d.right()-d.left(), docBody, true), d.left(), d.y+pad+d.ascent(docBody), docBody, true, Black)
		d.y += textRow
	}

	d.room(2*textRow + rowH)
	header()
	for _, g := range t.Groups {
		if g.Title != "" {
			// The title with the first two rows, or all there are.
			first := min(len(g.Rows), 2)
			if d.room(textRow + float64(first)*rowH) {
				header()
			}
			title(g.Title)
		}
		for _, r := range g.Rows {
			if d.room(rowH) {
				header()
				if g.Title != "" {
					title(g.Title + " (continued)")
				}
			}
			x := d.left()
			for i := range t.PictureColumns {
				if i < len(r.Pictures) && r.Pictures[i] != nil {
					d.picture(r.Pictures[i], x+pad, d.y+pad, pictureW[i]-2*pad, rowH-2*pad)
				}
				x += pictureW[i]
			}
			base := d.y + (rowH-textRow)/2 + pad + d.ascent(docBody)
			for i := range t.Columns {
				cell := ""
				if i < len(r.Cells) {
					cell = d.fit(r.Cells[i], widths[i]-2*pad, docBody, false)
				}
				if numeric[i] {
					d.textRight(cell, x+widths[i]-pad, base, docBody, false, Black)
				} else {
					d.text(cell, x+pad, base, docBody, false, Black)
				}
				x += widths[i]
			}
			d.log = append(d.log, "row")
			d.y += rowH
			d.box(d.left(), d.y-pt(docRule), d.right()-d.left(), pt(docRule), Gray75)
		}
	}
	d.y += pt(12)
}

// columns fits the text columns to the page: each as wide as its widest
// cell, unless together they are too wide, when the widest share what
// the narrower leave. A column of numbers is right-aligned.
func (d *Document) columns(t Table) ([]float64, []bool) {
	pad := pt(docCellPad)
	avail := d.right() - d.left()
	for _, name := range t.PictureColumns {
		if strings.EqualFold(name, "label") {
			avail -= pt(docLabelW)
		} else {
			avail -= pt(docPhotoW)
		}
	}
	n := len(t.Columns)
	natural := make([]float64, n)
	numeric := make([]bool, n)
	seen := make([]bool, n)
	for i := range numeric {
		numeric[i] = true
		natural[i] = d.face(true, docBody).width(t.Columns[i]) + 2*pad
	}
	for _, g := range t.Groups {
		for _, r := range g.Rows {
			for i := 0; i < n && i < len(r.Cells); i++ {
				c := r.Cells[i]
				natural[i] = math.Max(natural[i], d.face(false, docBody).width(c)+2*pad)
				if c != "" {
					seen[i] = true
					if _, err := strconv.ParseFloat(c, 64); err != nil {
						numeric[i] = false
					}
				}
			}
		}
	}
	for i := range numeric {
		numeric[i] = numeric[i] && seen[i]
	}
	widths := make([]float64, n)
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return natural[order[a]] < natural[order[b]] })
	left := avail
	for k, i := range order {
		share := left / float64(n-k)
		widths[i] = math.Min(natural[i], share)
		left -= widths[i]
	}
	// What is left over widens the last column, so the rules span the page.
	if n > 0 && left > 0 {
		widths[n-1] += left
	}
	return widths, numeric
}

// picture draws img within a box, its shape kept, centered.
func (d *Document) picture(img image.Image, x, y, w, h float64) {
	b := img.Bounds()
	if b.Dx() == 0 || b.Dy() == 0 {
		return
	}
	scale := math.Min(w/float64(b.Dx()), h/float64(b.Dy()))
	iw, ih := float64(b.Dx())*scale, float64(b.Dy())*scale
	d.page.Items = append(d.page.Items, Item{Image: &PlacedImage{
		X: round(x + (w-iw)/2), Y: round(y + (h-ih)/2), W: max(round(iw), 1), H: max(round(ih), 1), Image: img,
	}})
}

// Chart kinds.
const (
	ChartBar   = "bar"
	ChartDonut = "donut"
	ChartLine  = "line"
)

// Chart is a chart's series of points, each point a group or period,
// under its title.
type Chart struct {
	Title  string
	Kind   string
	Series []ChartSeries
}

// ChartSeries is one series' points, under its label.
type ChartSeries struct {
	Label  string
	Points []ChartPoint
}

// ChartPoint is a group's or period's value.
type ChartPoint struct {
	Label string
	Value int
}

// shades tell a chart's series, or a donut's slices, apart.
var shades = []Paint{Gray25, Gray50, Gray75, Black, Gray90}

// ErrUnknownChart is returned for a chart of no kind drawn here.
var ErrUnknownChart = errors.New("unknown chart")

// Chart draws a chart as vectors, on one page with its title.
func (d *Document) Chart(c Chart) error {
	titled := 0.0
	if c.Title != "" {
		titled = d.lineHeight(docHeading) + pt(4)
	}
	d.room(titled + pt(docChartH))
	if c.Title != "" {
		d.text(d.fit(c.Title, d.right()-d.left(), docHeading, true), d.left(), d.y+d.ascent(docHeading), docHeading, true, Black)
		d.y += titled
	}
	switch c.Kind {
	case ChartBar:
		d.barChart(c)
	case ChartDonut:
		d.donut(c)
	case ChartLine:
		d.lineChart(c)
	default:
		return fmt.Errorf("%w: %q", ErrUnknownChart, c.Kind)
	}
	d.y += pt(12)
	return nil
}

// legend draws each series' shade and label in a row, if there are several.
func (d *Document) legend(series []ChartSeries) {
	if len(series) < 2 {
		return
	}
	x := d.left()
	sw := pt(8)
	for i, s := range series {
		d.box(x, d.y+pt(1), sw, sw, shades[i%len(shades)])
		d.text(s.Label, x+sw+pt(3), d.y+d.ascent(docSmall), docSmall, false, Black)
		x += sw + pt(3) + d.face(false, docSmall).width(s.Label) + pt(12)
	}
	d.y += d.lineHeight(docSmall) + pt(4)
}

func points(c Chart) []ChartPoint {
	if len(c.Series) == 0 {
		return nil
	}
	return c.Series[0].Points
}

func peak(c Chart) int {
	top := 0
	for _, s := range c.Series {
		for _, p := range s.Points {
			top = max(top, p.Value)
		}
	}
	return top
}

// barChart draws a bar per series for each group, across, with its value.
func (d *Document) barChart(c Chart) {
	d.legend(c.Series)
	groups := points(c)
	if len(groups) == 0 {
		return
	}
	labelW := 0.0
	for _, g := range groups {
		labelW = math.Max(labelW, d.face(false, docSmall).width(g.Label))
	}
	labelW = math.Min(labelW+pt(6), (d.right()-d.left())/3)
	valueW := d.face(false, docSmall).width(strconv.Itoa(peak(c))) + pt(6)
	plotW := d.right() - d.left() - labelW - valueW
	n := len(c.Series)
	barH := math.Min(pt(10), (pt(docChartH)-pt(16))/float64(len(groups)*n))
	top := max(peak(c), 1)
	for gi, g := range groups {
		y := d.y + float64(gi*n)*barH + float64(gi)*pt(3)
		d.textRight(d.fit(g.Label, labelW-pt(6), docSmall, false), d.left()+labelW-pt(6), y+(float64(n)*barH+d.ascent(docSmall))/2, docSmall, false, Black)
		for si, s := range c.Series {
			v := 0
			if gi < len(s.Points) {
				v = s.Points[gi].Value
			}
			w := plotW * float64(v) / float64(top)
			by := y + float64(si)*barH
			d.box(d.left()+labelW, by+pt(0.5), w, barH-pt(1), shades[si%len(shades)])
			d.text(strconv.Itoa(v), d.left()+labelW+w+pt(3), by+(barH+d.ascent(docSmall))/2, docSmall, false, Black)
		}
	}
	d.y += float64(len(groups)*n)*barH + float64(len(groups))*pt(3)
}

// donut draws the first series' slices around a ring, with a legend of
// each slice's label and value beside it.
func (d *Document) donut(c Chart) {
	slices := points(c)
	total := 0
	for _, p := range slices {
		total += p.Value
	}
	r := pt(docChartH)/2 - pt(10)
	cx, cy := d.left()+r, d.y+r
	if total > 0 {
		start := -math.Pi / 2
		for i, p := range slices {
			if p.Value == 0 {
				continue
			}
			end := start + 2*math.Pi*float64(p.Value)/float64(total)
			d.page.Items = append(d.page.Items, Item{Path: ring(cx, cy, r, r*0.55, start, end), Paint: shades[i%len(shades)]})
			start = end
		}
	}
	x := cx + r + pt(18)
	y := d.y + pt(4)
	sw := pt(8)
	for i, p := range slices {
		d.box(x, y, sw, sw, shades[i%len(shades)])
		d.text(d.fit(fmt.Sprintf("%s: %d", p.Label, p.Value), d.right()-x-sw-pt(4), docSmall, false), x+sw+pt(4), y+d.ascent(docSmall)-pt(1), docSmall, false, Black)
		y += d.lineHeight(docSmall) + pt(2)
	}
	d.y = math.Max(d.y+2*r, y) + pt(4)
}

// ring is the outline of a ring's slice from angle a to b, in radians
// clockwise from three o'clock, as dots run down the page. Products are
// converted before they are added, so that no platform fuses them into
// one rounding, and the same document is drawn the same everywhere.
func ring(cx, cy, outer, inner, a, b float64) Path {
	steps := max(int(math.Ceil((b-a)/(math.Pi/36))), 1)
	at := func(r, t float64) Point { return Point{cx + float64(r*math.Cos(t)), cy + float64(r*math.Sin(t))} }
	p := Path{{Op: MoveTo, Pts: [3]Point{at(outer, a)}}}
	for i := 1; i <= steps; i++ {
		p = append(p, Segment{Op: LineTo, Pts: [3]Point{at(outer, a+(b-a)*float64(i)/float64(steps))}})
	}
	for i := steps; i >= 0; i-- {
		p = append(p, Segment{Op: LineTo, Pts: [3]Point{at(inner, a+(b-a)*float64(i)/float64(steps))}})
	}
	return p
}

// lineChart draws a line per series across the periods, over a baseline,
// with the periods' labels under it.
func (d *Document) lineChart(c Chart) {
	d.legend(c.Series)
	periods := points(c)
	if len(periods) == 0 {
		return
	}
	valueW := d.face(false, docSmall).width(strconv.Itoa(peak(c))) + pt(6)
	x0, x1 := d.left()+valueW, d.right()
	top, base := d.y+pt(4), d.y+pt(docChartH)-pt(30)
	peakV := max(peak(c), 1)
	d.textRight(strconv.Itoa(peak(c)), x0-pt(4), top+d.ascent(docSmall)/2, docSmall, false, Black)
	d.textRight("0", x0-pt(4), base+d.ascent(docSmall)/2, docSmall, false, Black)
	d.box(x0, base, x1-x0, pt(docRule), Gray50)
	step := (x1 - x0) / float64(max(len(periods)-1, 1))
	at := func(i, v int) Point {
		return Point{x0 + float64(float64(i)*step), base - float64((base-top)*float64(v))/float64(peakV)}
	}
	// Labels as often as they fit.
	every := 1
	for float64(every)*step < d.face(false, docSmall).width(periods[0].Label)+pt(6) {
		every++
	}
	for i, p := range periods {
		if i%every == 0 {
			lx := at(i, 0).X - d.face(false, docSmall).width(p.Label)/2
			d.text(p.Label, math.Max(lx, x0), base+pt(4)+d.ascent(docSmall), docSmall, false, Black)
		}
	}
	for si, s := range c.Series {
		var pts []Point
		for i, p := range s.Points {
			pts = append(pts, at(i, p.Value))
		}
		d.page.Items = append(d.page.Items, Item{Path: stroke(pts, pt(1.25)), Paint: shades[si%len(shades)]})
	}
	d.y += pt(docChartH)
}

// stroke is a line through pts, width wide, as a filled outline: a
// quadrilateral per segment, which overlap where they meet.
func stroke(pts []Point, width float64) Path {
	var p Path
	for i := 1; i < len(pts); i++ {
		a, b := pts[i-1], pts[i]
		dx, dy := b.X-a.X, b.Y-a.Y
		l := math.Hypot(dx, dy)
		if l == 0 {
			continue
		}
		nx, ny := float64(-dy/l*width)/2, float64(dx/l*width)/2
		p = append(p,
			Segment{Op: MoveTo, Pts: [3]Point{{a.X + nx, a.Y + ny}}},
			Segment{Op: LineTo, Pts: [3]Point{{b.X + nx, b.Y + ny}}},
			Segment{Op: LineTo, Pts: [3]Point{{b.X - nx, b.Y - ny}}},
			Segment{Op: LineTo, Pts: [3]Point{{a.X - nx, a.Y - ny}}},
		)
	}
	return p
}

// Pages finishes the document, numbering its pages, and answers them.
func (d *Document) Pages() []*Scene {
	for i, page := range d.pages {
		d.page = page
		label := fmt.Sprintf("Page %d of %d", i+1, len(d.pages))
		base := d.h - pt(docMargin)
		x := (d.w - d.face(false, docSmall).width(label)) / 2
		d.text(label, x, base, docSmall, false, Gray25)
	}
	return d.pages
}
