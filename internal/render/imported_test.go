package render

import (
	"encoding/json"
	"math"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/svgimport"
)

// warningIcon is a warning sign: a triangle with an exclamation mark cut
// out of it by the even-odd rule, in a stroked circle.
const warningIcon = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
	<path fill-rule="evenodd" d="M12 3 L22 20 H2 Z M11 9 h2 v6 h-2 z M11 16.5 h2 v2 h-2 z"/>
	<circle cx="12" cy="12" r="11" fill="none" stroke="#000" stroke-width="1"/>
</svg>`

// The "imported" golden: the icon at two sizes, and turned 30° about its
// middle.
func init() {
	r, err := svgimport.Import([]byte(warningIcon))
	if err != nil {
		panic(err)
	}
	var elements []labels.Element
	elements = append(elements, r.Place(2, 5, 20, 20)...)
	elements = append(elements, r.Place(25, 9, 12, 12)...)
	sin, cos := math.Sincos(30 * math.Pi / 180)
	for _, e := range r.Place(42, 5, 20, 20) {
		// Each shape turned, and moved round the icon's middle, (52, 15).
		dx, dy := e.X+e.Width/2-52, e.Y+e.Height/2-15
		e.X += dx*cos - dy*sin - dx
		e.Y += dx*sin + dy*cos - dy
		e.Rotation = 30
		elements = append(elements, e)
	}
	doc, err := json.Marshal(map[string]any{"id": "imported", "name": "Imported", "units": "mm", "width": 66, "height": 30, "dpi": 300,
		"layering": "list", "elements": elements})
	if err != nil {
		panic(err)
	}
	goldenTemplates["imported"] = string(doc)
	goldenPayloads["imported"] = []string{}
}
