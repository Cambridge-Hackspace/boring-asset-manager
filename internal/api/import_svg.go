package api

import (
	"encoding/json"
	"net/http"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/svgimport"
)

// ImportSVGRequest is SVG artwork to turn into shapes, and optionally the
// box on a template, in its units, to fit it into.
type ImportSVGRequest struct {
	SVG string      `json:"svg"`
	Box *PreviewBox `json:"box,omitempty"`
}

// ImportSVGResponse is the artwork as shapes: placed in the box asked for,
// or in the artwork's own units and size; with what was left out.
type ImportSVGResponse struct {
	Width    float64          `json:"width"`
	Height   float64          `json:"height"`
	Elements []labels.Element `json:"elements"`
	Skipped  []string         `json:"skipped"`
}

// importSVGBody leaves room for the artwork's JSON escaping.
const importSVGBody = 3 * svgimport.MaxBytes

// handleImportSVG turns SVG artwork into shapes for a template, storing
// nothing: POST /api/v1/templates/import-svg
func (s *Server) handleImportSVG(w http.ResponseWriter, r *http.Request) {
	var req ImportSVGRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, importSVGBody)).Decode(&req); err != nil || req.SVG == "" {
		writeJSONError(w, http.StatusBadRequest, `Expected {"svg": "<svg ...>", "box": {"x", "y", "width", "height"} (optional)}`)
		return
	}
	if b := req.Box; b != nil && !(b.Width > 0 && b.Height > 0) {
		writeJSONError(w, http.StatusBadRequest, "box must have a positive width and height")
		return
	}
	res, err := svgimport.Import([]byte(req.SVG))
	if err != nil {
		writeJSONError(w, http.StatusUnprocessableEntity, "The artwork cannot be imported: "+err.Error()+".")
		return
	}
	out := ImportSVGResponse{Width: res.Width, Height: res.Height, Elements: res.Elements, Skipped: nonNil(res.Skipped)}
	if b := req.Box; b != nil {
		out.Width, out.Height, out.Elements = b.Width, b.Height, res.Place(b.X, b.Y, b.Width, b.Height)
	}
	writeJSON(w, http.StatusOK, out)
}
