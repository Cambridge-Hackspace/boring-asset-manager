package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/generators"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/render"
)

// maxCheckedAssets bounds how many assets' labels are laid out to find the
// templates that cannot label them: enough for a page of assets, and
// printing a larger batch still says which fail.
const maxCheckedAssets = 50

// LabelOptionsRequest names the assets labels are wanted for.
type LabelOptionsRequest struct {
	AssetIDs []string `json:"asset_ids"`
}

// LabelTemplateOption is a template the assets' labels can use.
type LabelTemplateOption struct {
	ID     string        `json:"id"`
	Name   string        `json:"name"`
	Units  string        `json:"units"`
	Width  float64       `json:"width"`
	Height float64       `json:"height"`
	DPI    int           `json:"dpi"`
	Sizing string        `json:"sizing,omitempty"`
	Adapt  *labels.Adapt `json:"adapt,omitempty"`
	// PrintMirror and PrintRotation are how its labels print mirrored and
	// turned, for a preview as they are once stuck on.
	PrintMirror   string  `json:"print_mirror,omitempty"`
	PrintRotation float64 `json:"print_rotation,omitempty"`
	// Preferred is set for one of the assets' categories' templates, which
	// are listed first, in their order of preference.
	Preferred bool `json:"preferred"`
	// Problems are the assets it cannot label, and why.
	Problems []LabelProblem `json:"problems"`
}

// LabelFit is whether a template's labels print on a printer: why not, or
// what to know before they do.
type LabelFit struct {
	Fits     bool     `json:"fits"`
	Reason   string   `json:"reason,omitempty"`
	Warnings []string `json:"warnings"`
}

// LabelOptionsResponse is everything the Labels panel offers: templates in
// order of preference, the one to start with, and for those who may print,
// the printers and how each template suits each.
type LabelOptionsResponse struct {
	Templates  []LabelTemplateOption `json:"templates"`
	TemplateID string                `json:"template_id"`
	Printers   []SpoolChoice         `json:"printers"`
	// Fits maps a template and a printer to how its labels suit it.
	Fits map[string]map[string]LabelFit `json:"fits"`
}

// handleLabelOptions says which templates and printers can label assets:
// POST /api/v1/labels/options
func (s *Server) handleLabelOptions(w http.ResponseWriter, r *http.Request) {
	var req LabelOptionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if !checkAssetIDs(w, req.AssetIDs) {
		return
	}
	var assets []*models.AssetWithDetails
	for _, id := range req.AssetIDs {
		a, err := s.store.GetAssetByID(id)
		if err != nil || !Allows(r.Context(), rbac.PermAssetsRead, a.Scopes) {
			continue
		}
		assets = append(assets, s.shown(r, a))
	}
	templates, err := s.store.ListTemplates()
	if err != nil {
		writeServerError(w, err, "list templates")
		return
	}

	// The assets' categories' templates first, in their order.
	byID := map[string]*labels.Template{}
	for i := range templates {
		byID[templates[i].ID] = &templates[i]
	}
	var order []*labels.Template
	preferred := map[string]bool{}
	for _, a := range assets {
		for _, id := range a.LabelTemplate.TemplateIDs {
			if t := byID[id]; t != nil && !preferred[id] {
				preferred[id] = true
				order = append(order, t)
			}
		}
	}
	for i := range templates {
		if !preferred[templates[i].ID] {
			order = append(order, &templates[i])
		}
	}

	resp := LabelOptionsResponse{Templates: []LabelTemplateOption{}, Printers: []SpoolChoice{}, Fits: map[string]map[string]LabelFit{}}
	images := s.newLabelImages(r.Context())
	for _, t := range order {
		opt := LabelTemplateOption{ID: t.ID, Name: t.Name, Units: t.Units, Width: t.Width, Height: t.Height, DPI: t.DPI,
			Sizing: t.Sizing, Adapt: t.Adapt, PrintMirror: t.PrintMirror, PrintRotation: t.PrintRotation, Preferred: preferred[t.ID], Problems: []LabelProblem{}}
		if len(assets) <= maxCheckedAssets {
			for _, a := range assets {
				if _, err := render.Layout(t, s.labelData(a), images); err != nil {
					opt.Problems = append(opt.Problems, labelProblemFor(a, err))
				}
			}
		}
		resp.Templates = append(resp.Templates, opt)
	}

	canPrint := HasPermission(r.Context(), rbac.PermLabelsPrint)
	printsAnywhere := map[string]bool{}
	if canPrint {
		list, err := s.store.ListPrinters()
		if err != nil {
			writeServerError(w, err, "list printers")
			return
		}
		for i := range list {
			resp.Printers = append(resp.Printers, SpoolChoice{ID: list[i].ID, Name: list[i].Name})
		}
		for _, t := range order {
			resp.Fits[t.ID] = map[string]LabelFit{}
			for i := range list {
				p := &list[i]
				_, _, warnings, err := s.planFor(p, t)
				switch {
				case err == nil:
					resp.Fits[t.ID][p.ID] = LabelFit{Fits: true, Warnings: nonNil(warnings)}
					printsAnywhere[t.ID] = true
				default:
					resp.Fits[t.ID][p.ID] = LabelFit{Reason: unfitMessage(t, p, err), Warnings: []string{}}
				}
			}
		}
	}

	// Start with the first template that labels every asset and, for those
	// who print, prints somewhere; preferring the categories' own.
	usable := func(o LabelTemplateOption) bool { return len(o.Problems) == 0 && (!canPrint || printsAnywhere[o.ID]) }
	for _, pass := range []func(LabelTemplateOption) bool{
		func(o LabelTemplateOption) bool { return o.Preferred && usable(o) },
		usable,
		func(o LabelTemplateOption) bool { return o.Preferred },
		func(LabelTemplateOption) bool { return true },
	} {
		for _, o := range resp.Templates {
			if resp.TemplateID == "" && pass(o) {
				resp.TemplateID = o.ID
			}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// labelProblemFor says, in a sentence, why an asset's label cannot be laid
// out.
func labelProblemFor(a *models.AssetWithDetails, err error) LabelProblem {
	p := LabelProblem{AssetID: a.Asset.ID, Message: err.Error()}
	var le *render.LayoutError
	if !errors.As(err, &le) {
		return p
	}
	p.Path = le.Path
	if kind, ok := strings.CutPrefix(le.Message, "asset has no "); ok {
		kind = strings.TrimSuffix(kind, " code")
		for _, g := range generators.Describe() {
			if g.Type == kind {
				kind = g.Name
			}
		}
		p.Message = fmt.Sprintf("This template prints a %s, and %s has none.", kind, a.Asset.Name)
		return p
	}
	p.Message = fmt.Sprintf("%s's label cannot be laid out: %s.", a.Asset.Name, strings.TrimSuffix(le.Message, "."))
	return p
}
