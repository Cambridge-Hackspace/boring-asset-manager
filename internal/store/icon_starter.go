package store

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// StateAxis is the axis the state starter defines, which an icon opts into
// with `axes "state"` to be drawn in each state (README §3.7).
const StateAxis = "state"

// stateBadge is the badge a state's variant puts in an icon's corner: an
// isolex body on a 24 by 24 canvas, with %[1]s for the stroke width, and
// the tokens it paints with.
type stateBadge struct {
	describe string
	body     string
	tokens   []string
}

var stateBadges = map[string]stateBadge{
	models.IconStateOutOfService: {"A red octagon with a cross: out of service.", `poly fill="accent" stroke="outline" width=%[1]s {
        pt 7 2; pt 17 2; pt 22 7; pt 22 17; pt 17 22; pt 7 22; pt 2 17; pt 2 7
      }
      path d="M8,8 L16,16 M16,8 L8,16" stroke="ink" width=%[1]s`, []string{"accent", "outline", "ink"}},
	models.IconStateNeedsMaintenance: {"A pale red disc with an exclamation mark: needs maintenance.", `circle 12 12 10 fill="accent-dim" stroke="outline" width=%[1]s
      path d="M12,6 L12,14" stroke="ink" width=%[1]s
      circle 12 18 1.5 fill="ink"`, []string{"accent-dim", "outline", "ink"}},
	models.IconStateOverdue: {"A clock ringed in red: overdue.", `circle 12 12 10 fill="body-top" stroke="accent" width=%[1]s
      path d="M12,6 L12,12 L16,15" stroke="ink" width=%[1]s`, []string{"body-top", "accent", "ink"}},
	models.IconStateOnLoan: {"An arrow leaving a disc: on loan.", `circle 12 12 10 fill="body-top" stroke="outline" width=%[1]s
      path d="M7,12 L17,12 M13,8 L17,12 L13,16" stroke="ink" width=%[1]s`, []string{"body-top", "outline", "ink"}},
	models.IconStateOutOfStock: {"An empty box struck through in red: out of stock.", `rect 3 3 18 18 rx=3 fill="body-top" stroke="accent" width=%[1]s
      path d="M7,17 L17,7" stroke="accent" width=%[1]s`, []string{"body-top", "accent"}},
	models.IconStateLowStock: {"A box with a little left in it: running low.", `rect 3 3 18 18 rx=3 fill="body-top" stroke="outline" width=%[1]s
      rect 6 15 12 3 fill="accent-dim"`, []string{"body-top", "outline", "accent-dim"}},
}

// IconStateStarter is a library file to start drawing states from: the
// state axis, with a variant for each state's variant that puts a badge in
// the icon's top right corner, and the badges, drawn within the current
// settings: at a stroke width they allow, granting the reserved tokens
// they paint with.
func (s *Store) IconStateStarter() (string, error) {
	variants, err := s.IconStateVariants()
	if err != nil {
		return "", err
	}
	settings, err := readIconSettings(s.Query)
	if err != nil {
		return "", err
	}
	return stateStarter(variants, settings), nil
}

func stateStarter(variants map[string]string, settings models.IconSettings) string {
	width := strokeWidth(2, settings.StrokeWidths)
	var axis, badges strings.Builder
	axis.WriteString("// An icon is drawn in each state with `axes \"" + StateAxis + "\"`: as <icon>-down, say, with\n" +
		"// the state's badge in its top right corner. Which variant each state is\n" +
		"// drawn with is chosen under Asset states.\n" +
		"axis \"" + StateAxis + "\" {\n")
	done := map[string]bool{}
	for _, st := range models.IconStates {
		v := variants[st]
		if v == "" || done[v] {
			continue
		}
		done[v] = true
		b := stateBadges[st]
		badge := "state-badge-" + v
		fmt.Fprintf(&axis, "  variant %q {\n    part \"state-badge\" {\n      badge %q corner=\"ne\"\n    }\n  }\n", v, badge)
		fmt.Fprintf(&badges, "\nicon %q {\n  category %q\n  canvas 24 24\n  describe %q\n", badge, StateAxis, b.describe)
		for _, t := range b.tokens {
			if slices.Contains(settings.Reserved, t) {
				fmt.Fprintf(&badges, "  grant %q reason=\"marks an asset's state\"\n", t)
			}
		}
		fmt.Fprintf(&badges, "  part \"badge\" {\n    at 0 0 0 {\n      "+b.body+"\n    }\n  }\n}\n", width)
	}
	axis.WriteString("}\n")
	return axis.String() + badges.String()
}

// strokeWidth is the width allowed nearest want: want when any is.
func strokeWidth(want float64, allowed []float64) string {
	best := want
	for i, w := range allowed {
		if i == 0 || math.Abs(w-want) < math.Abs(best-want) {
			best = w
		}
	}
	return strconv.FormatFloat(best, 'f', -1, 64)
}
