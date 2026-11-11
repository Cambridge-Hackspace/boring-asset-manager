package labels

import (
	"errors"
	"testing"
)

// TestWorkpieceAnchor: a corner or the center, an inset only in a corner.
func TestWorkpieceAnchor(t *testing.T) {
	for _, c := range []struct {
		anchor WorkpieceAnchor
		path   string
	}{
		{WorkpieceAnchor{Corner: CornerTopRight, InsetMM: 3}, ""},
		{WorkpieceAnchor{Corner: CornerCenter}, ""},
		{WorkpieceAnchor{Corner: "middle"}, "workpiece_anchor.corner"},
		{WorkpieceAnchor{Corner: CornerCenter, InsetMM: 2}, "workpiece_anchor.inset_mm"},
		{WorkpieceAnchor{Corner: CornerBottomLeft, InsetMM: -1}, "workpiece_anchor.inset_mm"},
	} {
		tpl := validTemplate()
		a := c.anchor
		tpl.WorkpieceAnchor = &a
		err := tpl.Validate()
		if c.path == "" {
			if err != nil {
				t.Errorf("%+v: %v", a, err)
			}
			continue
		}
		var ve *ValidationError
		if !errors.As(err, &ve) || len(ve.Problems) != 1 || ve.Problems[0].Path != c.path {
			t.Errorf("%+v: %v, want a problem at %s", a, err, c.path)
		}
	}
}
