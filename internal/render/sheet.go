package render

import (
	"image"
	"math"
)

// Blocks for sheets (docs/reports.md#asset-sheets): an asset's page of
// pictures and facts, and checklists to fill in by hand.

// NewPage starts what follows on a new page, unless this one has nothing
// yet, as for one asset to a page.
func (d *Document) NewPage() {
	if !d.empty() {
		d.newPage()
	}
}

// Figure is a picture, drawn within a box of its size in points, its shape
// kept, with a caption under it.
type Figure struct {
	Image             image.Image
	Caption           string
	WidthPt, HeightPt float64
}

// Figures draws pictures side by side, in rows that wrap at the page's
// edge. A figure without an image is left out.
func (d *Document) Figures(figs []Figure) {
	gap := pt(12)
	capH := d.lineHeight(docSmall) + pt(2)
	x, rowH := d.left(), 0.0
	for _, f := range figs {
		if f.Image == nil {
			continue
		}
		w, h := math.Min(pt(f.WidthPt), d.right()-d.left()), pt(f.HeightPt)
		if x > d.left() && x+w > d.right() {
			d.y += rowH + gap
			x, rowH = d.left(), 0
		}
		if x == d.left() {
			d.room(h + capH)
		}
		d.log = append(d.log, "figure")
		d.picture(f.Image, x, d.y, w, h)
		d.text(d.fit(f.Caption, w, docSmall, false), x, d.y+h+pt(2)+d.ascent(docSmall), docSmall, false, Gray25)
		x += w + gap
		rowH = math.Max(rowH, h+capH)
	}
	if rowH > 0 {
		d.y += rowH + gap
	}
}

// Fact is a label and its value, as on an asset's sheet.
type Fact struct {
	Label, Value string
}

// docFactLabelW is the facts' label column, in points.
const docFactLabelW = 110

// Facts draws labels beside their values, a value broken into lines to fit
// and kept to the page, its label on its first line.
func (d *Document) Facts(facts []Fact) {
	labelW := pt(docFactLabelW)
	valueW := d.right() - d.left() - labelW
	for _, f := range facts {
		lines := breakLines(d.face(false, docBody), f.Value, valueW, true, func(int) bool { return false })
		if len(lines) == 0 {
			lines = []string{""}
		}
		for i, line := range lines {
			d.room(d.lineHeight(docBody))
			base := d.y + d.ascent(docBody)
			if i == 0 {
				d.text(d.fit(f.Label, labelW-pt(6), docBody, true), d.left(), base, docBody, true, Gray25)
			}
			d.text(d.fit(line, valueW, docBody, false), d.left()+labelW, base, docBody, false, Black)
			d.y += d.lineHeight(docBody)
		}
		d.log = append(d.log, "fact "+f.Label)
		d.y += pt(2)
	}
	d.y += pt(8)
}

// docCheckboxW is a checklist's checkbox column, and docCheckbox the box,
// in points.
const (
	docCheckboxW = 20
	docCheckbox  = 9
)

// checkbox draws an empty box to tick, its top left at (x, y).
func (d *Document) checkbox(x, y float64) {
	s, line := pt(docCheckbox), pt(docRule*1.5)
	d.box(x, y, s, line, Black)
	d.box(x, y+s-line, s, line, Black)
	d.box(x, y, line, s, Black)
	d.box(x+s-line, y, line, s, Black)
}
