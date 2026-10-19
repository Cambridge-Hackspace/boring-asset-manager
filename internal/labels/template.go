// Package labels defines label templates: their JSON document, validation,
// and the content bindings that fill them from an asset (README §4.1).
package labels

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/generators"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/textcheck"
	"github.com/google/uuid"
)

// Units of template geometry.
const (
	UnitsMM = "mm"
	UnitsIn = "in"
)

// Element types.
const (
	ElementGenerator   = "generator"
	ElementText        = "text"
	ElementStaticImage = "static_image"
	ElementShape       = "shape"
)

// Shapes, and the paints they are drawn in. Labels print black and white;
// white paints over whatever is beneath it.
const (
	ShapeRectangle = "rectangle"
	ShapeEllipse   = "ellipse"
	ShapeLine      = "line"
	ShapePolygon   = "polygon"
	ShapePath      = "path"
	PaintBlack     = "black"
	PaintWhite     = "white"
	PaintNone      = "none"
	// How a path's fill decides what is inside where it crosses itself.
	FillNonzero = "nonzero"
	FillEvenOdd = "evenodd"
	// MaxPolygonPoints bounds a polygon.
	MaxPolygonPoints = 256
)

// Mirrors flip an image.
const (
	MirrorAcross = "across"
	MirrorDown   = "down"
	MirrorBoth   = "both"
)

// MaxGuides bounds a template's guides each way; maxGroupLength a group's
// name.
const (
	MaxGuides      = 50
	maxGroupLength = 64
)

// Guides are lines in the editor at places across (X) and down (Y).
type Guides struct {
	X []float64 `json:"x,omitempty"`
	Y []float64 `json:"y,omitempty"`
}

// DefaultStrokeMM is a shape's stroke width when it gives none: four dots
// at 406 DPI, three at 300 and two at 203.
const DefaultStrokeMM = 0.25

// Text styling values.
const (
	WeightNormal = "normal"
	WeightBold   = "bold"
	FamilySans   = "sans"
	FamilyMono   = "mono"
	AlignLeft    = "left"
	AlignCenter  = "center"
	AlignRight   = "right"
	// A code's vertical alignment in its box.
	AlignTop    = "top"
	AlignMiddle = "middle"
	AlignBottom = "bottom"
)

// What a code's margin is counted in: its modules, growing and shrinking
// with the code, or the template's units.
const (
	MarginModules = "modules"
	MarginLength  = "length"
)

// Margin is the space kept clear around a code, side by side. A side left
// out takes the code's standard quiet zone.
type Margin struct {
	Top    *float64 `json:"top,omitempty"`
	Right  *float64 `json:"right,omitempty"`
	Bottom *float64 `json:"bottom,omitempty"`
	Left   *float64 `json:"left,omitempty"`
}

// Sides are the margin's sides, top, right, bottom and left, nil where left
// out.
func (m *Margin) Sides() [4]*float64 {
	if m == nil {
		return [4]*float64{}
	}
	return [4]*float64{m.Top, m.Right, m.Bottom, m.Left}
}

// SideNames name a margin's sides in the order Sides gives them.
var SideNames = [4]string{"top", "right", "bottom", "left"}

// LayeringList is the only layering: elements are drawn in the order they
// are listed, each over those before it. Documents without it were written
// when images were always drawn first, then text, then codes, and are drawn
// so until they are saved, when they are put in that order.
const LayeringList = "list"

// Limits that keep rendering bounded.
const (
	MinDPI            = 150
	MaxDPI            = 600
	MaxCanvasDots     = 10000 // per side, at the template's DPI
	MaxElements       = 100
	MaxFontSize       = 200.0 // points
	DefaultMinFont    = 6.0   // points
	MaxQuietZone      = 20    // modules
	MaxModuleMM       = 5.0
	maxBindingLength  = 1024
	geometryTolerance = 1e-9
)

var idRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// Template is a label layout. The ID is a UUID assigned when it is first
// saved. Templates from earlier versions keep the slugs their authors chose.
type Template struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Units  string  `json:"units"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	DPI    int     `json:"dpi"`
	// Sizing is "" (or "fixed") for a fixed template, or "adaptive" for one
	// laid out again for each label it prints on, within Adapt.
	Sizing string `json:"sizing,omitempty"`
	Adapt  *Adapt `json:"adapt,omitempty"`
	// Layering is LayeringList, or empty in a document written before it;
	// see DrawingOrder.
	Layering string `json:"layering,omitempty"`
	// Feed is which way its labels go through the printer when they fit
	// either way: FeedAuto (or empty), FeedAcross or FeedAlong.
	Feed string `json:"feed,omitempty"`
	// Turnable lets an adaptive template take labels turned a quarter turn
	// from the sizes it takes, its content turned to match.
	Turnable bool `json:"turnable,omitempty"`
	// Guides are lines in the editor to line elements up against, across
	// (at each X) and down (at each Y), in the template's units. They never
	// print.
	Guides *Guides `json:"guides,omitempty"`
	// Elements are drawn in this order: the first at the back.
	Elements  []Element `json:"elements"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Element is one positioned node. Which properties apply depends on Type;
// the rest must be left unset.
type Element struct {
	Type   string  `json:"type"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	// Rotation turns the element clockwise about the center of its box, in
	// degrees from 0 up to 360. X, Y, Width and Height are its box before it
	// is turned; Bounds is where it is after.
	Rotation float64 `json:"rotation,omitempty"`
	// AnchorX and AnchorY say how it follows an adaptive template's label
	// across and down; empty is AnchorScale.
	AnchorX string `json:"anchor_x,omitempty"`
	AnchorY string `json:"anchor_y,omitempty"`
	// MinSize and MaxSize bound each side of its box, in the template's
	// units, as an adaptive template is laid out; 0 is no bound. Laid out
	// smaller, it does as WhenTooSmall says; larger, it is centered in its
	// box at MaxSize.
	MinSize float64 `json:"min_size,omitempty"`
	MaxSize float64 `json:"max_size,omitempty"`
	// WhenTooSmall is what an element of an adaptive template does on a
	// label where it does not fit: TooSmallRefuse (codes' and text's
	// default), TooSmallHide (images' default), or TooSmallAlternative,
	// giving way to Alternative laid out in its box.
	WhenTooSmall string   `json:"when_too_small,omitempty"`
	Alternative  *Element `json:"alternative,omitempty"`

	// generator and text
	ContentBinding string `json:"content_binding,omitempty"`

	// generator
	GeneratorType   string `json:"generator_type,omitempty"`
	ErrorCorrection string `json:"error_correction,omitempty"`
	QuietZone       *int   `json:"quiet_zone,omitempty"`
	// MinModuleMM is the smallest module the code is drawn with, for the
	// scanners in use; two dots at the printer's DPI is always the least.
	MinModuleMM float64 `json:"min_module_mm,omitempty"`
	// Margin, counted as MarginUnit says (modules when empty), replaces
	// QuietZone side by side. The code sits in its box as Align (left,
	// center or right) and VerticalAlign (top, middle or bottom) say,
	// centered when they are empty.
	Margin        *Margin `json:"margin,omitempty"`
	MarginUnit    string  `json:"margin_unit,omitempty"`
	VerticalAlign string  `json:"vertical_align,omitempty"`

	// text
	FontSize   float64 `json:"font_size,omitempty"`
	FontWeight string  `json:"font_weight,omitempty"`
	FontFamily string  `json:"font_family,omitempty"`
	// Align is how text lines up in its box, and where a code sits in it.
	Align       string  `json:"align,omitempty"`
	Wrap        bool    `json:"wrap,omitempty"`
	MinFontSize float64 `json:"min_font_size,omitempty"`

	// Group names the group an element is in, which the editor moves,
	// resizes and turns as one; Locked keeps the editor from selecting it on
	// the canvas by accident. Neither changes what prints.
	Group  string `json:"group,omitempty"`
	Locked bool   `json:"locked,omitempty"`

	// static_image
	ImageID string `json:"image_id,omitempty"`
	// Mirror flips an image across (left for right), down (top for bottom)
	// or both.
	Mirror string `json:"mirror,omitempty"`

	// shape, and Fill for text too: what it is, how it is drawn, and its
	// paint. A rectangle may
	// round its corners; a line runs from its box's top left to its bottom
	// right, or from its bottom left to its top right when flipped; a
	// polygon's points are fractions of its box across and down, closed
	// unless Closed says otherwise. A rectangle's or ellipse's stroke lies
	// inside its box; a line's or polygon's is centered on it.
	Shape        string       `json:"shape,omitempty"`
	CornerRadius float64      `json:"corner_radius,omitempty"`
	Flip         bool         `json:"flip,omitempty"`
	Points       [][2]float64 `json:"points,omitempty"`
	Closed       *bool        `json:"closed,omitempty"`
	Stroke       string       `json:"stroke,omitempty"`
	StrokeWidth  float64      `json:"stroke_width,omitempty"`
	Fill         string       `json:"fill,omitempty"`
	// A path's SVG path data, with points as fractions of its box, as
	// imported artwork has; filled by FillRule, nonzero unless evenodd.
	D        string `json:"d,omitempty"`
	FillRule string `json:"fill_rule,omitempty"`
}

// Parse decodes a template document, rejecting unknown properties, applies
// defaults and validates it.
func Parse(data []byte) (*Template, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var t Template
	if err := dec.Decode(&t); err != nil {
		return nil, &ValidationError{Problems: []Problem{{Message: fmt.Sprintf("malformed template: %v", err)}}}
	}
	if dec.More() {
		return nil, &ValidationError{Problems: []Problem{{Message: "malformed template: trailing data after the document"}}}
	}
	t.Normalize()
	if err := t.Validate(); err != nil {
		return nil, err
	}
	return &t, nil
}

// layer is where an element of each type was drawn before templates were
// drawn in list order.
var layer = map[string]int{ElementStaticImage: 0, ElementText: 1, ElementGenerator: 2}

// DrawingOrder is the order the elements are drawn in, by index: as listed,
// or, in a document written before layering, images, then text, then codes,
// each type in its own order.
func (t *Template) DrawingOrder() []int {
	order := make([]int, len(t.Elements))
	for i := range order {
		order[i] = i
	}
	if t.Layering != LayeringList {
		sort.SliceStable(order, func(a, b int) bool {
			return layer[t.Elements[order[a]].Type] < layer[t.Elements[order[b]].Type]
		})
	}
	return order
}

// ListOrder lists the elements in the order they are drawn, so that a
// document written before layering is drawn the same in list order.
func (t *Template) ListOrder() {
	order := t.DrawingOrder()
	listed := make([]Element, len(order))
	for k, i := range order {
		listed[k] = t.Elements[i]
	}
	t.Elements, t.Layering = listed, LayeringList
}

// Normalize fills in defaulted text properties so stored templates are
// explicit.
func (t *Template) Normalize() {
	if t.Elements == nil {
		t.Elements = []Element{}
	}
	if t.Sizing == SizingFixed {
		t.Sizing = ""
	}
	if t.Adaptive() && t.Adapt != nil && t.Adapt.ContinuousLength == "" {
		t.Adapt.ContinuousLength = LengthProportional
	}
	for i := range t.Elements {
		normalizeElement(&t.Elements[i], t.Units)
		if alt := t.Elements[i].Alternative; alt != nil {
			normalizeElement(alt, t.Units)
		}
	}
}

// normalizeElement fills in a text element's or shape's defaulted
// properties.
func normalizeElement(e *Element, units string) {
	if e.Type == ElementShape {
		if e.Stroke == "" {
			e.Stroke = PaintBlack
		}
		if e.Fill == "" {
			e.Fill = PaintNone
		}
		if e.Stroke != PaintNone && e.StrokeWidth == 0 {
			e.StrokeWidth = DefaultStrokeMM
			if units == UnitsIn {
				e.StrokeWidth = math.Round(DefaultStrokeMM/25.4*1000) / 1000
			}
		}
		if e.Shape == ShapePolygon && e.Closed == nil {
			closed := true
			e.Closed = &closed
		}
		return
	}
	if e.Type != ElementText {
		return
	}
	if e.FontWeight == "" {
		e.FontWeight = WeightNormal
	}
	if e.FontFamily == "" {
		e.FontFamily = FamilySans
	}
	if e.Align == "" {
		e.Align = AlignLeft
	}
	if e.MinFontSize == 0 && e.FontSize > 0 {
		e.MinFontSize = math.Min(DefaultMinFont, e.FontSize)
	}
}

// InchesPerUnit converts the template's units to inches.
func (t *Template) InchesPerUnit() float64 {
	if t.Units == UnitsIn {
		return 1
	}
	return 1 / 25.4
}

// Validate checks the template against README §4.1 and returns a
// *ValidationError listing every problem found.
func (t *Template) Validate() error {
	var v validator

	// A new template has no ID until it is saved.
	if t.ID != "" && !idRegex.MatchString(t.ID) {
		v.add("id", "must be 1-64 characters of lowercase letters, digits, '_' or '-', starting with a letter or digit")
	}
	if name := strings.TrimSpace(t.Name); name == "" {
		v.add("name", fmt.Sprintf("must be 1-%d characters", textcheck.MaxName))
	} else if p := textcheck.Problem(name, textcheck.MaxName); p != "" {
		v.add("name", p)
	}

	unitsOK := t.Units == UnitsMM || t.Units == UnitsIn
	if !unitsOK {
		v.add("units", `must be "mm" or "in"`)
	}
	if t.DPI < MinDPI || t.DPI > MaxDPI {
		v.add("dpi", fmt.Sprintf("must be between %d and %d", MinDPI, MaxDPI))
	}
	canvasOK := true
	for _, side := range []struct {
		path  string
		value float64
	}{{"width", t.Width}, {"height", t.Height}} {
		if !finitePositive(side.value) {
			v.add(side.path, "must be a positive number")
			canvasOK = false
			continue
		}
		if unitsOK && t.DPI >= MinDPI && side.value*t.InchesPerUnit()*float64(t.DPI) > MaxCanvasDots {
			v.add(side.path, fmt.Sprintf("exceeds %d dots at %d DPI", MaxCanvasDots, t.DPI))
		}
	}

	t.validateSizing(&v, canvasOK && unitsOK)
	if t.Layering != "" && t.Layering != LayeringList {
		v.add("layering", `must be "list"`)
	}
	if !ValidFeed(t.Feed) {
		v.add("feed", `must be "auto", "across" or "along"`)
	}
	if t.Turnable && !t.Adaptive() {
		v.add("turnable", "applies only to adaptive templates")
	}
	if g := t.Guides; g != nil {
		for _, axis := range []struct {
			path  string
			at    []float64
			limit float64
		}{{"guides.x", g.X, t.Width}, {"guides.y", g.Y, t.Height}} {
			if len(axis.at) > MaxGuides {
				v.add(axis.path, fmt.Sprintf("at most %d guides are allowed", MaxGuides))
			}
			for _, at := range axis.at {
				if !(at >= 0 && at <= axis.limit+geometryTolerance) {
					v.add(axis.path, "must be on the label")
					break
				}
			}
		}
	}

	if len(t.Elements) > MaxElements {
		v.add("elements", fmt.Sprintf("at most %d elements are allowed", MaxElements))
	}
	if n := t.pathSegments(); n > MaxTemplatePathSegments {
		v.add("elements", fmt.Sprintf("its paths have %d segments; at most %d are allowed", n, MaxTemplatePathSegments))
	}
	for i := range t.Elements {
		t.validateElement(&v, fmt.Sprintf("elements[%d]", i), &t.Elements[i], canvasOK)
	}
	return v.err()
}

func (t *Template) validateElement(v *validator, path string, e *Element, canvasOK bool) {
	at := func(prop string) string { return path + "." + prop }

	geometryOK := true
	turned := e.Rotation != 0
	if math.IsNaN(e.Rotation) || e.Rotation < 0 || e.Rotation >= 360 {
		v.add(at("rotation"), "must be at least 0 degrees and less than 360")
		geometryOK, turned = false, false
	}
	// A line may be level or upright: its box no height, or no width.
	line := e.Type == ElementShape && e.Shape == ShapeLine
	for _, g := range []struct {
		prop     string
		value    float64
		positive bool
	}{{"x", e.X, false}, {"y", e.Y, false}, {"width", e.Width, !line}, {"height", e.Height, !line}} {
		// A turned element's box may start before the canvas, as long as
		// the element, turned, lies on it.
		ok := !math.IsNaN(g.value) && !math.IsInf(g.value, 0) && (turned && !g.positive && g.prop[0] != 'w' && g.prop[0] != 'h' || g.value >= 0) && (!g.positive || g.value > 0)
		if !ok {
			geometryOK = false
			switch {
			case g.positive:
				v.add(at(g.prop), "must be a positive number")
			case turned:
				v.add(at(g.prop), "must be a number")
			default:
				v.add(at(g.prop), "must be zero or a positive number")
			}
		}
	}
	if line && geometryOK && e.Width == 0 && e.Height == 0 {
		v.add(at("width"), "a line needs a length: a width, a height or both")
		geometryOK = false
	}
	if geometryOK && canvasOK && !turned {
		if e.X+e.Width > t.Width+geometryTolerance {
			v.add(at("width"), "extends past the right edge of the canvas")
		}
		if e.Y+e.Height > t.Height+geometryTolerance {
			v.add(at("height"), "extends past the bottom edge of the canvas")
		}
	}
	if geometryOK && canvasOK && turned {
		x, y, w, h := e.Bounds()
		for _, edge := range []struct {
			past bool
			name string
		}{{x < -geometryTolerance, "left"}, {y < -geometryTolerance, "top"}, {x+w > t.Width+geometryTolerance, "right"}, {y+h > t.Height+geometryTolerance, "bottom"}} {
			if edge.past {
				v.add(at("rotation"), fmt.Sprintf("turned, it extends past the %s edge of the canvas", edge.name))
			}
		}
	}

	if !anchors[e.AnchorX] {
		v.add(at("anchor_x"), `must be "scale", "start", "end", "both" or "center"`)
	}
	if !anchors[e.AnchorY] {
		v.add(at("anchor_y"), `must be "scale", "start", "end", "both" or "center"`)
	}
	t.validateLimits(v, path, e, canvasOK)

	// unset reports properties that do not apply to this element type.
	unset := func(props map[string]bool) {
		for prop, isSet := range props {
			if isSet {
				v.add(at(prop), fmt.Sprintf("does not apply to %s elements", e.Type))
			}
		}
	}
	generatorProps := map[string]bool{
		"generator_type":   e.GeneratorType != "",
		"error_correction": e.ErrorCorrection != "",
		"quiet_zone":       e.QuietZone != nil,
		"min_module_mm":    e.MinModuleMM != 0,
		"margin":           e.Margin != nil,
		"margin_unit":      e.MarginUnit != "",
		"vertical_align":   e.VerticalAlign != "",
	}
	if e.Type != ElementStaticImage && e.Mirror != "" {
		v.add(at("mirror"), "applies only to images: shapes are flipped by their points, and text and codes are turned")
	}
	if len(e.Group) > maxGroupLength || strings.TrimSpace(e.Group) != e.Group {
		v.add(at("group"), fmt.Sprintf("must be at most %d characters, without spaces at either end", maxGroupLength))
	}
	shapeProps := map[string]bool{
		"shape":         e.Shape != "",
		"corner_radius": e.CornerRadius != 0,
		"flip":          e.Flip,
		"points":        e.Points != nil,
		"closed":        e.Closed != nil,
		"stroke":        e.Stroke != "",
		"stroke_width":  e.StrokeWidth != 0,
		"fill":          e.Fill != "",
		"d":             e.D != "",
		"fill_rule":     e.FillRule != "",
	}
	textProps := map[string]bool{
		"font_size":     e.FontSize != 0,
		"font_weight":   e.FontWeight != "",
		"font_family":   e.FontFamily != "",
		"wrap":          e.Wrap,
		"min_font_size": e.MinFontSize != 0,
	}

	switch e.Type {
	case ElementGenerator:
		unset(textProps)
		unset(shapeProps)
		unset(map[string]bool{"image_id": e.ImageID != ""})
		g, err := generators.Lookup(e.GeneratorType)
		if err != nil {
			v.add(at("generator_type"), fmt.Sprintf("must be one of %s", strings.Join(generators.AllowedGeneratorTypes(), ", ")))
		} else if e.ErrorCorrection != "" && !contains(g.ErrorCorrectionLevels(), e.ErrorCorrection) {
			if levels := g.ErrorCorrectionLevels(); len(levels) == 0 {
				v.add(at("error_correction"), fmt.Sprintf("%s has no error correction levels", e.GeneratorType))
			} else {
				v.add(at("error_correction"), fmt.Sprintf("must be one of %s for %s", strings.Join(levels, ", "), e.GeneratorType))
			}
		}
		if math.IsNaN(e.MinModuleMM) || e.MinModuleMM < 0 || e.MinModuleMM > MaxModuleMM {
			v.add(at("min_module_mm"), fmt.Sprintf("must be between 0 and %g mm", MaxModuleMM))
		}
		if e.QuietZone != nil && (*e.QuietZone < 0 || *e.QuietZone > MaxQuietZone) {
			v.add(at("quiet_zone"), fmt.Sprintf("must be between 0 and %d modules", MaxQuietZone))
		}
		validateMargin(v, path, e)
		if e.Align != "" && e.Align != AlignLeft && e.Align != AlignCenter && e.Align != AlignRight {
			v.add(at("align"), `must be "left", "center" or "right"`)
		}
		if e.VerticalAlign != "" && e.VerticalAlign != AlignTop && e.VerticalAlign != AlignMiddle && e.VerticalAlign != AlignBottom {
			v.add(at("vertical_align"), `must be "top", "middle" or "bottom"`)
		}
		validateBinding(v, at("content_binding"), e.ContentBinding, false)

	case ElementText:
		unset(generatorProps)
		// Text takes a fill: black, or white for text reversed out of black.
		textFill := e.Fill
		delete(shapeProps, "fill")
		unset(shapeProps)
		unset(map[string]bool{"image_id": e.ImageID != ""})
		if textFill != "" && textFill != PaintBlack && textFill != PaintWhite {
			v.add(at("fill"), `must be "black" or "white"`)
		}
		if !(e.FontSize > 0 && e.FontSize <= MaxFontSize) {
			v.add(at("font_size"), fmt.Sprintf("must be greater than 0 and at most %g points", MaxFontSize))
		} else if !(e.MinFontSize > 0 && e.MinFontSize <= e.FontSize) {
			v.add(at("min_font_size"), "must be greater than 0 and at most font_size")
		}
		if e.FontWeight != WeightNormal && e.FontWeight != WeightBold {
			v.add(at("font_weight"), `must be "normal" or "bold"`)
		}
		if e.FontFamily != FamilySans && e.FontFamily != FamilyMono {
			v.add(at("font_family"), `must be "sans" or "mono"`)
		}
		if e.Align != AlignLeft && e.Align != AlignCenter && e.Align != AlignRight {
			v.add(at("align"), `must be "left", "center" or "right"`)
		}
		validateBinding(v, at("content_binding"), e.ContentBinding, true)

	case ElementStaticImage:
		unset(generatorProps)
		unset(textProps)
		unset(shapeProps)
		if e.Mirror != "" && e.Mirror != MirrorAcross && e.Mirror != MirrorDown && e.Mirror != MirrorBoth {
			v.add(at("mirror"), `must be "across", "down" or "both"`)
		}
		unset(map[string]bool{"content_binding": e.ContentBinding != "", "align": e.Align != ""})
		if _, err := uuid.Parse(e.ImageID); err != nil {
			v.add(at("image_id"), "must be the ID of an uploaded image")
		}

	case ElementShape:
		unset(generatorProps)
		unset(textProps)
		unset(map[string]bool{"image_id": e.ImageID != "", "content_binding": e.ContentBinding != "", "align": e.Align != ""})
		validateShape(v, path, e)

	default:
		v.add(at("type"), `must be "generator", "text", "static_image" or "shape"`)
	}
}

// validateShape checks a shape's own properties: its kind and what applies
// to it, and its paint, which must draw something.
func validateShape(v *validator, path string, e *Element) {
	at := func(prop string) string { return path + "." + prop }
	paints := map[string]bool{PaintBlack: true, PaintWhite: true, PaintNone: true}
	if !paints[e.Stroke] {
		v.add(at("stroke"), `must be "black", "white" or "none"`)
	}
	if !paints[e.Fill] {
		v.add(at("fill"), `must be "black", "white" or "none"`)
	}
	if e.Stroke == PaintNone && e.Fill == PaintNone {
		v.add(at("stroke"), "draws nothing with fill none: give it a stroke or a fill")
	}
	if math.IsNaN(e.StrokeWidth) || math.IsInf(e.StrokeWidth, 0) || e.StrokeWidth < 0 {
		v.add(at("stroke_width"), "must be zero or a positive number")
	} else if e.Stroke != PaintNone && e.StrokeWidth == 0 {
		v.add(at("stroke_width"), "must be positive for a stroke")
	}

	only := func(prop string, set bool, shape string) {
		if set && e.Shape != shape {
			v.add(at(prop), fmt.Sprintf("applies only to %ss", shape))
		}
	}
	only("corner_radius", e.CornerRadius != 0, ShapeRectangle)
	only("flip", e.Flip, ShapeLine)
	only("points", e.Points != nil, ShapePolygon)
	only("closed", e.Closed != nil, ShapePolygon)
	only("d", e.D != "", ShapePath)
	only("fill_rule", e.FillRule != "", ShapePath)

	switch e.Shape {
	case ShapeRectangle:
		if math.IsNaN(e.CornerRadius) || e.CornerRadius < 0 || e.CornerRadius > math.Min(e.Width, e.Height)/2+geometryTolerance {
			v.add(at("corner_radius"), "must be from 0 to half the box's shorter side")
		}
	case ShapeEllipse:
	case ShapeLine:
		if e.Fill != PaintNone {
			v.add(at("fill"), "does not apply: a line has no inside to fill")
		}
	case ShapePolygon:
		least := 3
		if e.Closed != nil && !*e.Closed {
			least = 2
		}
		if len(e.Points) < least || len(e.Points) > MaxPolygonPoints {
			v.add(at("points"), fmt.Sprintf("must be %d to %d points", least, MaxPolygonPoints))
		}
		for i, p := range e.Points {
			if !(p[0] >= 0 && p[0] <= 1 && p[1] >= 0 && p[1] <= 1) {
				v.add(fmt.Sprintf("%s[%d]", at("points"), i), "must be fractions of the box, each from 0 to 1")
				break
			}
		}
		if e.Closed != nil && !*e.Closed && e.Fill != PaintNone {
			v.add(at("fill"), "does not apply: an open polygon has no inside to fill")
		}
	case ShapePath:
		segs, err := ParsePathData(e.D)
		if err != nil {
			v.add(at("d"), err.Error())
			break
		}
		for _, s := range segs {
			for _, p := range s.Points() {
				if p[0] < -geometryTolerance || p[0] > 1+geometryTolerance || p[1] < -geometryTolerance || p[1] > 1+geometryTolerance {
					v.add(at("d"), "must keep its points within its box: fractions of it, each from 0 to 1")
					return
				}
			}
		}
		if e.FillRule != "" && e.FillRule != FillNonzero && e.FillRule != FillEvenOdd {
			v.add(at("fill_rule"), `must be "nonzero" or "evenodd"`)
		}
	default:
		v.add(at("shape"), `must be "rectangle", "ellipse", "line", "polygon" or "path"`)
	}
}

// pathSegments counts the path segments of a template's paths, for
// MaxTemplatePathSegments; paths that do not parse count none.
func (t *Template) pathSegments() int {
	n := 0
	for _, e := range t.Elements {
		if e.Type == ElementShape && e.Shape == ShapePath {
			if segs, err := ParsePathData(e.D); err == nil {
				n += len(segs)
			}
		}
	}
	return n
}

// validateMargin checks a code's margin: in modules, each side up to
// MaxQuietZone; in length, up to half the box across or down; never with
// quiet_zone, which it replaces.
func validateMargin(v *validator, path string, e *Element) {
	at := func(prop string) string { return path + "." + prop }
	if e.MarginUnit != "" && e.MarginUnit != MarginModules && e.MarginUnit != MarginLength {
		v.add(at("margin_unit"), `must be "modules" or "length"`)
		return
	}
	if e.Margin == nil {
		return
	}
	if e.QuietZone != nil {
		v.add(at("margin"), "cannot be set with quiet_zone, which it replaces")
	}
	for i, side := range e.Margin.Sides() {
		if side == nil {
			continue
		}
		m := *side
		bound, unit := float64(MaxQuietZone), "modules"
		if e.MarginUnit == MarginLength {
			bound, unit = e.Width/2, "half the box's width"
			if i%2 == 0 {
				bound, unit = e.Height/2, "half the box's height"
			}
			if math.IsNaN(m) || m < 0 || m > bound+geometryTolerance {
				v.add(at("margin."+SideNames[i]), "must be from 0 to "+unit)
			}
			continue
		}
		if math.IsNaN(m) || m < 0 || m > bound {
			v.add(at("margin."+SideNames[i]), fmt.Sprintf("must be from 0 to %g %s", bound, unit))
		}
	}
}

func validateBinding(v *validator, path, binding string, required bool) {
	if len(binding) > maxBindingLength {
		v.add(path, fmt.Sprintf("must be at most %d characters", maxBindingLength))
		return
	}
	b, err := ParseBinding(binding)
	if err != nil {
		v.add(path, err.Error())
		return
	}
	if required && b.IsEmpty() {
		v.add(path, "is required")
	}
}

// Binding returns the element's parsed content binding. A generator element
// without one encodes the asset's code for its generator type.
func (e *Element) Binding() (Binding, error) {
	if e.Type == ElementGenerator && e.ContentBinding == "" {
		return ParseBinding("{{asset.code." + e.GeneratorType + "}}")
	}
	return ParseBinding(e.ContentBinding)
}

// FieldKeys returns the distinct field keys the template's bindings use.
func (t *Template) FieldKeys() []string {
	seen := map[string]bool{}
	var keys []string
	add := func(e *Element) {
		b, err := e.Binding()
		if err != nil {
			return
		}
		for _, ref := range b.Refs() {
			if ref.Kind == RefField && !seen[ref.Arg] {
				seen[ref.Arg] = true
				keys = append(keys, ref.Arg)
			}
		}
	}
	for i := range t.Elements {
		add(&t.Elements[i])
		if alt := t.Elements[i].Alternative; alt != nil {
			add(alt)
		}
	}
	return keys
}

// ImageIDs returns the distinct images the template places.
func (t *Template) ImageIDs() []string {
	seen := map[string]bool{}
	var ids []string
	for _, e := range t.Elements {
		if e.Type == ElementStaticImage && !seen[e.ImageID] {
			seen[e.ImageID] = true
			ids = append(ids, e.ImageID)
		}
	}
	return ids
}

func finitePositive(f float64) bool {
	return f > 0 && !math.IsInf(f, 1)
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}
