package printers

import (
	"fmt"
	"slices"
	"strings"
)

// Lasers whose drivers serve many machines of many sizes, GRBL's and
// Ruida's, take their work area and the corner their origin is in from
// their options, the model giving only defaults.

// Origins are the corners a laser's origin may be in, as the person faces
// it: top is the back of the bed.
var Origins = []string{"top_left", "top_right", "bottom_left", "bottom_right"}

// MaxWorkMM bounds a work area's side set in the options.
const MaxWorkMM = 5000

// originFlips is how points from the top left are turned for an origin:
// X runs right to left from a right corner, and Y from the front up from a
// bottom one.
func originFlips(origin string) (flipX, flipY bool) {
	return strings.HasSuffix(origin, "_right"), strings.HasPrefix(origin, "bottom_")
}

// bedOptions are the options every such driver takes.
type bedOptions struct {
	WorkWidthMM  *int    `json:"work_width_mm"`
	WorkHeightMM *int    `json:"work_height_mm"`
	Origin       *string `json:"origin"`
}

func (o bedOptions) check() error {
	for name, v := range map[string]*int{"work_width_mm": o.WorkWidthMM, "work_height_mm": o.WorkHeightMM} {
		if v != nil && (*v < 1 || *v > MaxWorkMM) {
			return fmt.Errorf("options: %s must be 1-%d", name, MaxWorkMM)
		}
	}
	if o.Origin != nil && !slices.Contains(Origins, *o.Origin) {
		return fmt.Errorf("options: origin must be one of %s", strings.Join(Origins, ", "))
	}
	return nil
}

// sized is a model with the work area the options set.
func (o bedOptions) sized(m OneShotModel) OneShotModel {
	if o.WorkWidthMM != nil {
		m.WorkWidthMM = float64(*o.WorkWidthMM)
	}
	if o.WorkHeightMM != nil {
		m.WorkHeightMM = float64(*o.WorkHeightMM)
	}
	return m
}

// origin is the corner the origin is in, or def.
func (o bedOptions) origin(def string) string {
	if o.Origin != nil {
		return *o.Origin
	}
	return def
}

// bedOptionInfo describes the options, with a model's defaults said in
// words.
func bedOptionInfo(defaultOrigin string) []OptionInfo {
	return []OptionInfo{{
		Name: "work_width_mm", Type: "int", Default: nil,
		Description: fmt.Sprintf("How far the head reaches across the bed, in millimeters (1-%d); blank takes the model's.", MaxWorkMM),
	}, {
		Name: "work_height_mm", Type: "int", Default: nil,
		Description: fmt.Sprintf("How far the head reaches from the back of the bed to the front, in millimeters (1-%d); blank takes the model's.", MaxWorkMM),
	}, {
		Name: "origin", Type: "string", Default: defaultOrigin, Values: Origins,
		Description: "The corner of the work area the machine counts from, as you face it (top is the back): where it homes, or its origin is set.",
	}}
}
