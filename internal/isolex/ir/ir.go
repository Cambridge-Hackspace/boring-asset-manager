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

// Version is the IR format this package reads; a document of any other
// version is refused rather than half understood.
const Version = 1

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

// Shape is one drawn shape, with the fields of its Kind set. A group
// places a badge: its Shapes drawn translated by (TX, TY) and scaled.
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

	TX     float64 `json:"tx,omitempty"` // group
	TY     float64 `json:"ty,omitempty"`
	Scale  float64 `json:"scale,omitempty"`
	Shapes []Shape `json:"shapes,omitempty"`
}

// Parse reads an IR document, refusing another format version and any
// shape of a kind it does not know.
func Parse(data []byte) (*Document, error) {
	var doc Document
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("isolex IR: %w", err)
	}
	if doc.IRVersion != Version {
		return nil, fmt.Errorf("isolex IR: version %d, but this build reads version %d", doc.IRVersion, Version)
	}
	for _, icon := range doc.Icons {
		if err := checkKinds(icon.Name, icon.Shapes); err != nil {
			return nil, err
		}
	}
	return &doc, nil
}

func checkKinds(icon string, shapes []Shape) error {
	for _, s := range shapes {
		switch s.Kind {
		case KindPolygon, KindPath, KindEllipse, KindCircle, KindRect:
		case KindGroup:
			if err := checkKinds(icon, s.Shapes); err != nil {
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
