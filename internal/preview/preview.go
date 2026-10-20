// Package preview describes a laid-out label and supplies the stand-in data
// a template is previewed with. The server's previews and the editor's live
// renderer (internal/live, run in the browser) share it, so both say the
// same of the same label.
package preview

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/generators"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/render"
)

// Element is one element of a previewed label, as format json describes it:
// where it is, in the template's units, and what it prints.
type Element struct {
	Index int    `json:"index"`
	Type  string `json:"type"`
	// Name is what the editor's layers call it, when it has been named.
	Name          string  `json:"name,omitempty"`
	X             float64 `json:"x"`
	Y             float64 `json:"y"`
	Width         float64 `json:"width"`
	Height        float64 `json:"height"`
	Value         string  `json:"value,omitempty"`
	GeneratorType string  `json:"generator_type,omitempty"`
	ImageID       string  `json:"image_id,omitempty"`
	FontSize      float64 `json:"font_size,omitempty"`
	// DrawnFontSize is the size text was drawn at, in points: its
	// font_size, or smaller where it shrank to fit, or as it filled its box.
	DrawnFontSize float64 `json:"drawn_font_size,omitempty"`
	// Rotation is the degrees it is turned clockwise about the center of
	// its box, and Bounds where it lies once turned; both are left out when
	// it is not turned.
	Rotation float64 `json:"rotation,omitempty"`
	Bounds   *Box    `json:"bounds,omitempty"`
	// Code is how a code is laid out in its box, before it is turned.
	Code *Code `json:"code,omitempty"`
	// A shape: what it is, how it is drawn, and its paint.
	Shape        string       `json:"shape,omitempty"`
	Stroke       string       `json:"stroke,omitempty"`
	StrokeWidth  float64      `json:"stroke_width,omitempty"`
	Fill         string       `json:"fill,omitempty"`
	CornerRadius float64      `json:"corner_radius,omitempty"`
	Flip         bool         `json:"flip,omitempty"`
	Points       [][2]float64 `json:"points,omitempty"`
	Closed       *bool        `json:"closed,omitempty"`
}

// Code is how a code is laid out in its box: the size of its modules, and
// the space between it and each side of its box as drawn, in the template's
// units and in dots, with the sides narrower than its standard quiet zone.
type Code struct {
	Module     float64  `json:"module"`
	ModuleDots int      `json:"module_dots"`
	Margin     Sides    `json:"margin"`
	MarginDots Sides    `json:"margin_dots"`
	Narrow     []string `json:"narrow,omitempty"`
}

// Sides are lengths on each side of a box.
type Sides struct {
	Top    float64 `json:"top"`
	Right  float64 `json:"right"`
	Bottom float64 `json:"bottom"`
	Left   float64 `json:"left"`
}

// Box is a box on a previewed label, in the template's units.
type Box struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// Description is a previewed label as format json describes it.
type Description struct {
	Width    float64   `json:"width"`
	Height   float64   `json:"height"`
	Units    string    `json:"units"`
	DPI      int       `json:"dpi"`
	Elements []Element `json:"elements"`
	// Warnings never stop it printing, such as a code's margin narrower
	// than its standard quiet zone.
	Warnings []string `json:"warnings"`
}

// Describe says what each element of a laid-out label prints, and how its
// codes were laid out in the scene.
func Describe(t *labels.Template, data labels.Data, scene *render.Scene) Description {
	d := Description{Width: t.Width, Height: t.Height, Units: t.Units, DPI: t.DPI, Elements: []Element{}, Warnings: []string{}}
	dotsPerUnit := float64(t.DPI) * t.InchesPerUnit()
	units := func(dots int) float64 { return math.Round(float64(dots)/dotsPerUnit*1000) / 1000 }
	for i := range t.Elements {
		e := &t.Elements[i]
		pe := Element{Index: i, Type: e.Type, Name: e.Name, X: e.X, Y: e.Y, Width: e.Width, Height: e.Height,
			GeneratorType: e.GeneratorType, ImageID: e.ImageID, FontSize: e.FontSize, Rotation: e.Rotation,
			Shape: e.Shape, Stroke: e.Stroke, StrokeWidth: e.StrokeWidth, Fill: e.Fill, CornerRadius: e.CornerRadius, Flip: e.Flip, Points: e.Points, Closed: e.Closed}
		if e.Type == labels.ElementShape && e.Stroke != labels.PaintNone && e.StrokeWidth*dotsPerUnit < 1 {
			d.Warnings = append(d.Warnings, fmt.Sprintf("elements[%d] (%s) has a stroke thinner than one dot at %d DPI: it is drawn one dot wide.", i, e.Shape, t.DPI))
		}
		if size, ok := scene.TextSizes[i]; ok {
			pe.DrawnFontSize = size
		}
		if e.Rotation != 0 {
			x, y, w, h := e.Bounds()
			pe.Bounds = &Box{X: x, Y: y, Width: w, Height: h}
		}
		if c, ok := scene.Codes[i]; ok {
			m := c.Margin
			pe.Code = &Code{Module: units(c.Module), ModuleDots: c.Module, Narrow: c.Narrow(),
				Margin:     Sides{Top: units(m[0]), Right: units(m[1]), Bottom: units(m[2]), Left: units(m[3])},
				MarginDots: Sides{Top: float64(m[0]), Right: float64(m[1]), Bottom: float64(m[2]), Left: float64(m[3])}}
			if len(pe.Code.Narrow) > 0 {
				side := "side"
				if len(pe.Code.Narrow) > 1 {
					side = "sides"
				}
				d.Warnings = append(d.Warnings, fmt.Sprintf("elements[%d] (%s code) has less space than its standard quiet zone on its %s %s, which some scanners need.",
					i, e.GeneratorType, joinSides(pe.Code.Narrow), side))
			}
		}
		if b, err := e.Binding(); err == nil && e.Type != labels.ElementStaticImage {
			pe.Value = b.Resolve(data)
		}
		d.Elements = append(d.Elements, pe)
	}
	return d
}

// joinSides names sides in a sentence: "left", "left and top".
func joinSides(sides []string) string {
	if len(sides) < 2 {
		return strings.Join(sides, "")
	}
	return strings.Join(sides[:len(sides)-1], ", ") + " and " + sides[len(sides)-1]
}

// Sample stands in for an asset when previewing without one. Every
// generator gets a code that each symbology accepts; fields get their
// placeholders from WithFields, as they depend on the template.
func Sample(baseURL string) labels.Data {
	d := labels.Data{
		AssetID:    "00000000-0000-4000-8000-000000000000",
		AssetName:  "Sample asset",
		BaseURL:    baseURL,
		Codes:      map[string]string{},
		Fields:     map[string]string{},
		Categories: "Tools, Power tools",
		Location:   "Shelf 2 › Bin A3",
		Condition:  "Working",
		Quantity:   "40",
		Unit:       "pcs",
		Borrower:   "Ada Lovelace",
		Due:        "2026-01-31",
	}
	for _, genType := range generators.AllowedGeneratorTypes() {
		d.Codes[genType] = "BAM-0001"
	}
	return d
}

// WithFields gives every field the template binds a placeholder, its key
// in brackets, and returns d.
func WithFields(d labels.Data, t *labels.Template) labels.Data {
	fields := make(map[string]string, len(d.Fields))
	for k, v := range d.Fields {
		fields[k] = v
	}
	for _, key := range t.FieldKeys() {
		fields[key] = "[" + key + "]"
	}
	d.Fields = fields
	return d
}

// Data stands in for an asset when previewing t without one.
func Data(t *labels.Template, baseURL string) labels.Data {
	return WithFields(Sample(baseURL), t)
}

// WithDefaults fills a blank ID or name with a placeholder: previews need
// neither, and the editor previews a template before it is named.
func WithDefaults(doc json.RawMessage) ([]byte, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(doc, &obj); err != nil || obj == nil {
		return nil, errors.New("not an object")
	}
	for key, placeholder := range map[string]string{"id": `"preview"`, "name": `"Preview"`} {
		raw, ok := obj[key]
		var s *string
		if !ok || json.Unmarshal(raw, &s) == nil && (s == nil || strings.TrimSpace(*s) == "") {
			obj[key] = json.RawMessage(placeholder)
		}
	}
	return json.Marshal(obj)
}
