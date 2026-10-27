// Package ir reads the shape IR that the isolex compiler, by Axonibyte
// Innovations, LLC, emits (isolex SPEC §6, src/ir.rs; ../NOTICE): icons as lists of shapes, every position a number
// and every color a theme token left for the renderer to resolve. It has
// no dependencies beyond the standard library, so the label renderer can
// use it under TinyGo as well as on the server.
package ir

import (
	"encoding/json"
	"fmt"
)

// Version is the newest IR format this package reads. It reads version
// 1 too: a build stored by an older isolex is served until a new one
// replaces it, and stays when the lexicon does not compile under the new
// isolex. A document of any other version is refused rather than half
// understood.
const Version = 2

// Document is a compiled lexicon.
type Document struct {
	IRVersion int    `json:"ir_version"`
	Icons     []Icon `json:"icons"`
}

// Icon is one emitted icon. Exactly one of Footprint and Canvas is set:
// an isometric volume in tile units, or a flat screen-space frame.
type Icon struct {
	Name      string      `json:"name"`
	Category  string      `json:"category"`
	Footprint *[3]float64 `json:"footprint,omitempty"`
	Canvas    *[2]float64 `json:"canvas,omitempty"`
	Viewbox   [4]float64  `json:"viewbox"`
	Describe  []string    `json:"describe"`
	Grants    [][2]string `json:"grants"`
	Shapes    []Shape     `json:"shapes"`
}

// Kinds of shape.
const (
	KindPolygon = "polygon"
	KindPath    = "path"
	KindEllipse = "ellipse"
	KindCircle  = "circle"
	KindRect    = "rect"
	KindGroup   = "group"
)

// Shape is one drawn shape, with the fields of its Kind set.
//
// A group is its Shapes drawn through a map. In IR version 2 (isolex 0.4)
// that is Matrix, six numbers as SVG's matrix(), with an optional convex
// Clip polygon in the coordinates the group is drawn in, and Strokes
// "fixed" for strokes that keep their width whatever the matrix does;
// a transform's shapes, a badge's or a glyph's, and groups never nest.
// In version 1 a group only placed a badge: translated by (TX, TY) and
// scaled by Scale. Map gives either as a matrix.
type Shape struct {
	Kind string `json:"kind"`

	Points [][2]float64 `json:"points,omitempty"` // polygon
	D      string       `json:"d,omitempty"`      // path, already formatted
	CX     float64      `json:"cx,omitempty"`     // ellipse, circle
	CY     float64      `json:"cy,omitempty"`
	RX     float64      `json:"rx,omitempty"` // ellipse; rect's corner radius
	RY     float64      `json:"ry,omitempty"`
	R      float64      `json:"r,omitempty"` // circle
	X      float64      `json:"x,omitempty"` // rect
	Y      float64      `json:"y,omitempty"`
	W      float64      `json:"w,omitempty"`
	H      float64      `json:"h,omitempty"`

	Fill        string  `json:"fill,omitempty"`
	FillOpacity float64 `json:"fill_opacity,omitempty"`
	Stroke      string  `json:"stroke,omitempty"`
	StrokeWidth float64 `json:"stroke_width,omitempty"`
	Dash        *string `json:"dash,omitempty"`

	Matrix  *[6]float64  `json:"matrix,omitempty"` // group, version 2
	Clip    [][2]float64 `json:"clip,omitempty"`
	Strokes string       `json:"strokes,omitempty"`
	TX      float64      `json:"tx,omitempty"` // group, version 1
	TY      float64      `json:"ty,omitempty"`
	Scale   float64      `json:"scale,omitempty"`
	Shapes  []Shape      `json:"shapes,omitempty"`
}

// Map is a group's map as SVG's matrix(a b c d e f): a point (x, y) goes
// to (a·x + c·y + e, b·x + d·y + f). A version 1 group's is its scale
// and then its translation.
func (s *Shape) Map() [6]float64 {
	if s.Matrix != nil {
		return *s.Matrix
	}
	return [6]float64{s.Scale, 0, 0, s.Scale, s.TX, s.TY}
}

// FixedStrokes says a group's strokes keep the width they are written
// at, not scaled by its matrix.
func (s *Shape) FixedStrokes() bool { return s.Strokes == "fixed" }

// Mul is the map that applies n, then m.
func Mul(m, n [6]float64) [6]float64 {
	return [6]float64{
		m[0]*n[0] + m[2]*n[1],
		m[1]*n[0] + m[3]*n[1],
		m[0]*n[2] + m[2]*n[3],
		m[1]*n[2] + m[3]*n[3],
		m[0]*n[4] + m[2]*n[5] + m[4],
		m[1]*n[4] + m[3]*n[5] + m[5],
	}
}

// Apply maps the point p through m.
func Apply(m [6]float64, p [2]float64) [2]float64 {
	return [2]float64{m[0]*p[0] + m[2]*p[1] + m[4], m[1]*p[0] + m[3]*p[1] + m[5]}
}

// Identity is the map that moves nothing.
var Identity = [6]float64{1, 0, 0, 1, 0, 0}

// Parse reads an IR document, refusing another format version and any
// shape of a kind it does not know.
func Parse(data []byte) (*Document, error) {
	var doc Document
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("isolex IR: %w", err)
	}
	if doc.IRVersion != 1 && doc.IRVersion != Version {
		return nil, fmt.Errorf("isolex IR: version %d, but this build reads versions 1 and %d", doc.IRVersion, Version)
	}
	for _, icon := range doc.Icons {
		if err := checkKinds(icon.Name, icon.Shapes, doc.IRVersion); err != nil {
			return nil, err
		}
	}
	return &doc, nil
}

// ParseIcon reads one icon of an IR document of this build's version, as
// the compiler draws one icon by itself.
func ParseIcon(data []byte) (*Icon, error) {
	var icon Icon
	if err := json.Unmarshal(data, &icon); err != nil {
		return nil, fmt.Errorf("isolex IR: %w", err)
	}
	if err := checkKinds(icon.Name, icon.Shapes, Version); err != nil {
		return nil, err
	}
	return &icon, nil
}

func checkKinds(icon string, shapes []Shape, version int) error {
	for _, s := range shapes {
		switch s.Kind {
		case KindPolygon, KindPath, KindEllipse, KindCircle, KindRect:
		case KindGroup:
			if version >= 2 && s.Matrix == nil {
				return fmt.Errorf("isolex IR: icon %q has a group without a matrix", icon)
			}
			if s.Clip != nil && len(s.Clip) < 3 {
				return fmt.Errorf("isolex IR: icon %q has a group clipped to fewer than three points", icon)
			}
			if err := checkKinds(icon, s.Shapes, version); err != nil {
				return err
			}
		default:
			return fmt.Errorf("isolex IR: icon %q has a shape of unknown kind %q", icon, s.Kind)
		}
	}
	return nil
}

// Icon returns the icon named name, or nil.
func (d *Document) Icon(name string) *Icon {
	for i := range d.Icons {
		if d.Icons[i].Name == name {
			return &d.Icons[i]
		}
	}
	return nil
}
