// Package labels defines label templates: their JSON document, validation,
// and the content bindings that fill them from an asset (README §4.1).
package labels

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
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
)

// Text styling values.
const (
	WeightNormal = "normal"
	WeightBold   = "bold"
	FamilySans   = "sans"
	FamilyMono   = "mono"
	AlignLeft    = "left"
	AlignCenter  = "center"
	AlignRight   = "right"
)

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
	Sizing    string    `json:"sizing,omitempty"`
	Adapt     *Adapt    `json:"adapt,omitempty"`
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

	// text
	FontSize    float64 `json:"font_size,omitempty"`
	FontWeight  string  `json:"font_weight,omitempty"`
	FontFamily  string  `json:"font_family,omitempty"`
	Align       string  `json:"align,omitempty"`
	Wrap        bool    `json:"wrap,omitempty"`
	MinFontSize float64 `json:"min_font_size,omitempty"`

	// static_image
	ImageID string `json:"image_id,omitempty"`
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
		normalizeElement(&t.Elements[i])
		if alt := t.Elements[i].Alternative; alt != nil {
			normalizeElement(alt)
		}
	}
}

// normalizeElement fills in a text element's defaulted properties.
func normalizeElement(e *Element) {
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

	if len(t.Elements) > MaxElements {
		v.add("elements", fmt.Sprintf("at most %d elements are allowed", MaxElements))
	}
	for i := range t.Elements {
		t.validateElement(&v, fmt.Sprintf("elements[%d]", i), &t.Elements[i], canvasOK)
	}
	return v.err()
}

func (t *Template) validateElement(v *validator, path string, e *Element, canvasOK bool) {
	at := func(prop string) string { return path + "." + prop }

	geometryOK := true
	for _, g := range []struct {
		prop     string
		value    float64
		positive bool
	}{{"x", e.X, false}, {"y", e.Y, false}, {"width", e.Width, true}, {"height", e.Height, true}} {
		ok := !math.IsNaN(g.value) && !math.IsInf(g.value, 0) && g.value >= 0 && (!g.positive || g.value > 0)
		if !ok {
			geometryOK = false
			if g.positive {
				v.add(at(g.prop), "must be a positive number")
			} else {
				v.add(at(g.prop), "must be zero or a positive number")
			}
		}
	}
	if geometryOK && canvasOK {
		if e.X+e.Width > t.Width+geometryTolerance {
			v.add(at("width"), "extends past the right edge of the canvas")
		}
		if e.Y+e.Height > t.Height+geometryTolerance {
			v.add(at("height"), "extends past the bottom edge of the canvas")
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
	}
	textProps := map[string]bool{
		"font_size":     e.FontSize != 0,
		"font_weight":   e.FontWeight != "",
		"font_family":   e.FontFamily != "",
		"align":         e.Align != "",
		"wrap":          e.Wrap,
		"min_font_size": e.MinFontSize != 0,
	}

	switch e.Type {
	case ElementGenerator:
		unset(textProps)
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
		validateBinding(v, at("content_binding"), e.ContentBinding, false)

	case ElementText:
		unset(generatorProps)
		unset(map[string]bool{"image_id": e.ImageID != ""})
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
		unset(map[string]bool{"content_binding": e.ContentBinding != ""})
		if _, err := uuid.Parse(e.ImageID); err != nil {
			v.add(at("image_id"), "must be the ID of an uploaded image")
		}

	default:
		v.add(at("type"), `must be "generator", "text" or "static_image"`)
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
