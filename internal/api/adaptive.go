package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/render"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// sampleCache holds sample label data for a few seconds, since checking
// every template against every printer asks for it many times at once.
type sampleCache struct {
	mu   sync.Mutex
	data *labels.Data
	at   time.Time
}

// sampleTTL is how long sample label data is kept.
const sampleTTL = 5 * time.Second

// labelSample is what adaptive templates' codes are checked against: the
// longest code assigned with each generator, and the longest value of each
// field, among active assets. It is nil when it cannot be read.
func (s *Server) labelSample() *labels.Data {
	c := &s.sample
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.data != nil && time.Since(c.at) < sampleTTL {
		return c.data
	}
	codes, fields, err := s.store.LongestLabelValues()
	if err != nil {
		return nil
	}
	c.data = &labels.Data{AssetID: "00000000-0000-0000-0000-000000000000", BaseURL: s.cfg.Server.BaseURL, Codes: codes, Fields: fields}
	c.at = time.Now()
	return c.data
}

// planFor plans printing labels of template t on printer p: its driver's
// plan, the template as printed there, and warnings for the person
// printing; or an error wrapping printers.ErrDoesNotFit when they do not
// suit the media loaded, with t as it is. A fixed template prints as it
// is; an adaptive one is laid out for the printer's label and DPI.
func (s *Server) planFor(p *models.Printer, t *labels.Template) (printers.Plan, *labels.Template, []string, error) {
	settings := store.PrinterSettings(p)
	if !t.Adaptive() {
		plan, fit, err := printers.PlanLabel(p.Driver, settings, labelSpec(t))
		return plan, t, fit.Warnings, err
	}
	g, err := printers.GeometryOf(p.Driver, settings)
	if err != nil {
		return nil, t, nil, err
	}
	var reasons []string
	for _, size := range adaptiveSizes(t, g) {
		laid, notices, err := render.Adapt(t, size.width, size.height, g.DPI, s.labelSample())
		if err != nil {
			if !errors.Is(err, render.ErrDoesNotAdapt) {
				return nil, t, nil, err
			}
			reasons = append(reasons, err.Error())
			continue
		}
		plan, fit, err := printers.PlanLabel(p.Driver, settings, labelSpec(laid))
		if errors.Is(err, printers.ErrDoesNotFit) {
			reasons = append(reasons, printers.Why(err))
			continue
		}
		return plan, laid, append(notices, fit.Warnings...), err
	}
	return nil, t, nil, fmt.Errorf("%w: it does not adapt to %s: %s", printers.ErrDoesNotFit, g.Roll, strings.Join(dedupe(reasons), "; "))
}

// unfitMessage says why template t's labels cannot print on printer p, from
// the error planFor gave: "Shipping can't print on Front desk QL: it is
// laid out at 203 DPI, but Brother QL printers print at 300 DPI."
func unfitMessage(t *labels.Template, p *models.Printer, err error) string {
	return fmt.Sprintf("%s can't print on %s: %s.", t.Name, p.Name, strings.TrimSuffix(printers.Why(err), "."))
}

type labelSize struct{ width, height float64 }

// adaptiveSizes are the sizes, in the template's units, an adaptive
// template could take on a printer's roll: its width across the roll, or
// its height, with the other side the die-cut label's length or, on a
// continuous roll, as the template says; those nearest its own shape first.
func adaptiveSizes(t *labels.Template, g printers.Geometry) []labelSize {
	unit := 1 / t.InchesPerUnit() / 25.4 // units per millimeter
	across := g.WidthMM * unit
	along := func(designAcross, designAlong float64) float64 {
		switch {
		case g.DieCut():
			return g.LengthMM * unit
		case t.Adapt.ContinuousLength == labels.LengthFixed:
			return designAlong
		default:
			return designAlong * across / designAcross
		}
	}
	sizes := []labelSize{
		{across, along(t.Width, t.Height)},
		{along(t.Height, t.Width), across},
	}
	distortion := func(z labelSize) float64 {
		return math.Abs(math.Log(z.width/t.Width)) + math.Abs(math.Log(z.height/t.Height))
	}
	sort.SliceStable(sizes, func(i, j int) bool { return distortion(sizes[i]) < distortion(sizes[j]) })
	return sizes
}

func dedupe(list []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range list {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// AdaptRequest asks how an adaptive template lays out: at a size and DPI
// (POST /api/v1/templates/adapt), or across the sizes it takes, at a DPI
// (POST /api/v1/templates/range; DPI defaults to the template's).
type AdaptRequest struct {
	Template json.RawMessage `json:"template"`
	Width    float64         `json:"width"`
	Height   float64         `json:"height"`
	DPI      int             `json:"dpi"`
	// PrinterID, in place of a size and DPI, lays it out for that printer's
	// roll, as printing there would.
	PrinterID string `json:"printer_id,omitempty"`
}

// readAdaptRequest reads an adapt request's template, which must be valid
// and adaptive.
func readAdaptRequest(w http.ResponseWriter, r *http.Request) (*labels.Template, AdaptRequest, bool) {
	var req AdaptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Template) == 0 {
		writeJSONError(w, http.StatusBadRequest, `Expected {"template": {...}}`)
		return nil, req, false
	}
	doc, err := withPreviewDefaults(req.Template)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "template must be a JSON object")
		return nil, req, false
	}
	t, err := labels.Parse(doc)
	if writeTemplateError(w, err) {
		return nil, req, false
	}
	if !t.Adaptive() {
		writeJSONError(w, http.StatusBadRequest, "The template is not adaptive")
		return nil, req, false
	}
	if req.DPI == 0 {
		req.DPI = t.DPI
	}
	if req.DPI < labels.MinDPI || req.DPI > labels.MaxDPI {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("dpi must be between %d and %d", labels.MinDPI, labels.MaxDPI))
		return nil, req, false
	}
	return t, req, true
}

// handleAdaptTemplate lays an adaptive template out at a size and DPI, with
// the longest codes assigned, answering with the template laid out and its
// notices, or why it does not fit: POST /api/v1/templates/adapt
func (s *Server) handleAdaptTemplate(w http.ResponseWriter, r *http.Request) {
	t, req, ok := readAdaptRequest(w, r)
	if !ok {
		return
	}
	if req.PrinterID != "" {
		p, ok := s.loadPrinter(w, req.PrinterID)
		if !ok {
			return
		}
		_, laid, warnings, err := s.planFor(p, t)
		switch {
		case errors.Is(err, printers.ErrDoesNotFit):
			writeJSON(w, http.StatusOK, map[string]any{"fits": false, "problem": LabelProblem{Message: err.Error()}})
		case err != nil:
			writeServerError(w, err, "lay the template out")
		default:
			writeJSON(w, http.StatusOK, map[string]any{"fits": true, "template": laid, "notices": nonNil(warnings)})
		}
		return
	}
	laid, notices, err := render.Adapt(t, req.Width, req.Height, req.DPI, s.labelSample())
	var ae *render.AdaptError
	switch {
	case errors.As(err, &ae):
		writeJSON(w, http.StatusOK, map[string]any{"fits": false, "problem": LabelProblem{Path: ae.Path, Message: ae.Message}})
	case err != nil:
		writeServerError(w, err, "lay the template out")
	default:
		writeJSON(w, http.StatusOK, map[string]any{"fits": true, "template": laid, "notices": nonNil(notices)})
	}
}

// handleTemplateRange works out the sizes an adaptive template takes, in
// its own shape, with the longest codes assigned: POST /api/v1/templates/range
func (s *Server) handleTemplateRange(w http.ResponseWriter, r *http.Request) {
	t, req, ok := readAdaptRequest(w, r)
	if !ok {
		return
	}
	span, err := render.Range(t, req.DPI, s.labelSample())
	var ae *render.AdaptError
	switch {
	case errors.As(err, &ae):
		writeJSON(w, http.StatusOK, map[string]any{"fits": false, "problem": LabelProblem{Path: ae.Path, Message: ae.Message}})
	case err != nil:
		writeServerError(w, err, "work out the template's sizes")
	default:
		size := func(scale float64) map[string]float64 {
			return map[string]float64{"width": tidy(t.Width * scale), "height": tidy(t.Height * scale)}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"fits": true, "units": t.Units, "dpi": req.DPI,
			"smallest": size(span.MinScale), "largest": size(span.MaxScale),
			"smaller": span.Smaller, "larger": span.Larger,
		})
	}
}

// tidy rounds a length to a hundredth.
func tidy(v float64) float64 { return math.Round(v*100) / 100 }
