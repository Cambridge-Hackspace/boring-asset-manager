package api

import (
	"errors"
	"fmt"
	"math"
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
// suit the media loaded. A fixed template prints as it is; an adaptive one
// is laid out for the printer's label and DPI.
func (s *Server) planFor(p *models.Printer, t *labels.Template) (printers.Plan, *labels.Template, []string, error) {
	settings := store.PrinterSettings(p)
	if !t.Adaptive() {
		plan, fit, err := printers.PlanLabel(p.Driver, settings, labelSpec(t))
		return plan, t, fit.Warnings, err
	}
	g, err := printers.GeometryOf(p.Driver, settings)
	if err != nil {
		return nil, nil, nil, err
	}
	var reasons []string
	for _, size := range adaptiveSizes(t, g) {
		laid, err := render.Adapt(t, size.width, size.height, g.DPI, s.labelSample())
		if err != nil {
			if !errors.Is(err, render.ErrDoesNotAdapt) {
				return nil, nil, nil, err
			}
			reasons = append(reasons, err.Error())
			continue
		}
		plan, fit, err := printers.PlanLabel(p.Driver, settings, labelSpec(laid))
		if errors.Is(err, printers.ErrDoesNotFit) {
			reasons = append(reasons, strings.TrimPrefix(err.Error(), printers.ErrDoesNotFit.Error()+": "))
			continue
		}
		return plan, laid, fit.Warnings, err
	}
	return nil, nil, nil, fmt.Errorf("%w: %s does not adapt to %s: %s", printers.ErrDoesNotFit, t.Name, g.Roll, strings.Join(dedupe(reasons), "; "))
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
