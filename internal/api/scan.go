package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/reports"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Scan templates on categories (README §6.4).

// CategoryScanTemplateRequest names a category's scan template, or with
// null, none.
type CategoryScanTemplateRequest struct {
	ScanTemplateID *string `json:"scan_template_id"`
}

// handleSetCategoryScanTemplate: PUT /api/v1/categories/{id}/scan-template
func (s *Server) handleSetCategoryScanTemplate(w http.ResponseWriter, r *http.Request) {
	var req CategoryScanTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if req.ScanTemplateID != nil && *req.ScanTemplateID == "" {
		req.ScanTemplateID = nil
	}
	cat, err := s.store.SetCategoryScanTemplate(r.PathValue("id"), req.ScanTemplateID)
	switch {
	case errors.Is(err, store.ErrCategoryNotFound):
		writeJSONError(w, http.StatusNotFound, "Category not found")
	case errors.Is(err, store.ErrReportNotFound):
		writeJSONError(w, http.StatusBadRequest, "No scan template has that ID")
	case err != nil:
		writeReportError(w, err, "set the scan template")
	default:
		writeJSON(w, http.StatusOK, map[string]any{"category": cat})
	}
}

// handleScanConflicts lists the categories in scan template conflicts, with
// how many active assets each is in one for:
// GET /api/v1/categories/scan-conflicts. With ?category_id= (and
// ?scan_template_id=, empty for none) it answers as if that category named
// that template, so the category form can warn before saving.
func (s *Server) handleScanConflicts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var template *string
	if t := q.Get("scan_template_id"); t != "" {
		template = &t
	}
	involved, err := s.store.ScanConflicts(q.Get("category_id"), template)
	if err != nil {
		writeServerError(w, err, "find scan template conflicts")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"categories": involved})
}

// ScanResponse is what scanning a code shows: the asset, as far as its
// quick actions need it; the template; and the template laid out for it,
// its actions narrowed to those the viewer may take now.
type ScanResponse struct {
	Asset    ScanAsset       `json:"asset"`
	Template ScanTemplateRef `json:"template"`
	Result   *reports.Result `json:"result"`
}

// ScanAsset names the asset scanned. Unit is its stock's unit, when its
// stock is counted, for using some.
type ScanAsset struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Counted bool   `json:"counted"`
	Unit    string `json:"unit,omitempty"`
	// Actions are those its categories offer, when the viewer may press
	// them.
	Actions []models.OfferedAction `json:"actions,omitempty"`
}

// ScanTemplateRef names the template a scan used.
type ScanTemplateRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// fallbackScan is shown for an asset whose template is gone, which only a
// database edited by hand allows: its name, and its page.
var fallbackScan = &reports.Spec{Sections: []reports.Section{
	{Type: "details", Columns: []reports.Column{{Heading: "Name", Binding: "{{asset.name}}"}}},
	{Type: "actions", Actions: []string{"open"}},
}}

// handleScan looks an asset up by a code, or its ID, and lays out its scan
// template: GET /api/v1/scan/{code}. It needs assets.scan for the asset;
// one the viewer cannot scan is not found. Signed-out visitors use it too,
// with the public role.
func (s *Server) handleScan(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(r.PathValue("code"))
	ids, err := s.store.AssetsByCode(code)
	if err != nil {
		writeServerError(w, err, "find the code")
		return
	}
	if len(ids) == 0 {
		ids = []string{code}
	}
	var found []*models.AssetWithDetails
	for _, id := range ids {
		a, err := s.store.GetAssetByID(id)
		if errors.Is(err, store.ErrAssetNotFound) {
			continue
		}
		if err != nil {
			writeServerError(w, err, "read the asset")
			return
		}
		if a.Asset.ArchivedAt == nil && Allows(r.Context(), rbac.PermAssetsScan, a.Scopes) {
			found = append(found, a)
		}
	}
	switch {
	case len(found) == 0:
		writeJSONError(w, http.StatusNotFound, "No asset has that code")
		return
	case len(found) > 1:
		writeJSONError(w, http.StatusConflict, fmt.Sprintf("%d assets have that code, as codes of different kinds; look them up by name", len(found)))
		return
	}
	asset := found[0]

	ref, spec := ScanTemplateRef{ID: asset.ScanTemplate.TemplateID}, fallbackScan
	if report, err := s.store.GetReport(asset.ScanTemplate.TemplateID); err == nil {
		ref.Name = report.Name
		parsed, _, err := s.parseSpecOf(reports.KindScan, report.Spec)
		if err != nil {
			// A field it names was deleted since it was saved.
			writeJSONError(w, http.StatusConflict, "The scan template "+report.Name+" no longer fits: "+err.Error()+". Edit it to fix this.")
			return
		}
		spec = parsed
	} else if !errors.Is(err, store.ErrReportNotFound) {
		writeServerError(w, err, "read the scan template")
		return
	}
	result, err := s.runScan(r, spec, asset)
	if err != nil {
		writeServerError(w, err, "lay out the scan")
		return
	}
	may := s.scanActionsFor(r, asset)
	for i := range result.Sections {
		if result.Sections[i].Type != "actions" {
			continue
		}
		kept := []string{}
		for _, a := range result.Sections[i].Actions {
			if may[a] {
				kept = append(kept, a)
			}
		}
		result.Sections[i].Actions = kept
	}
	out := ScanResponse{Asset: ScanAsset{ID: asset.Asset.ID, Name: asset.Asset.Name}, Template: ref, Result: result}
	if st := asset.Asset.Stock; st != nil {
		out.Asset.Counted, out.Asset.Unit = true, st.Unit
	}
	if may["actions"] {
		out.Asset.Actions = asset.Actions
	}
	writeJSON(w, http.StatusOK, out)
}

// scanActionsFor says which quick actions the request may take on an asset
// now: those its permissions allow in the asset's scopes, and that apply.
func (s *Server) scanActionsFor(r *http.Request, a *models.AssetWithDetails) map[string]bool {
	can := func(p string) bool { return Allows(r.Context(), p, a.Scopes) }
	user := CurrentUser(r.Context())
	mine := user != nil && a.Loan != nil && a.Loan.Borrower.ID == user.ID
	return map[string]bool{
		"open": can(rbac.PermAssetsRead),
		"edit": can(rbac.PermAssetsWrite),
		"use":  a.Asset.Stock != nil && can(rbac.PermStockRecord),
		// Borrowing is for oneself, so it needs someone signed in.
		"borrow": user != nil && a.Lendable && a.Loan == nil && a.Asset.Condition != models.ConditionOutOfService && can(rbac.PermAssetsBorrow),
		"return": a.Loan != nil && (can(rbac.PermAssetsLend) || (mine && can(rbac.PermAssetsBorrow))),
		"report": can(rbac.PermAssetsReport) || can(rbac.PermAssetsWrite),
		// The actions its categories offer, such as Identify (README §5.4).
		"actions": len(a.Actions) > 0 && can(rbac.PermAssetsAct),
		// A new asset like it, or in it, as the asset's page starts one (README §3.2).
		"add_similar": can(rbac.PermAssetsWrite),
		"add_inside":  can(rbac.PermAssetsWrite) && s.hasLocationType(),
	}
}

// hasLocationType reports whether a link type makes locations, for things
// to be put in others.
func (s *Server) hasLocationType() bool {
	types, err := s.store.ListRelationTypes()
	if err != nil {
		return false
	}
	for _, t := range types {
		if t.IsLocation {
			return true
		}
	}
	return false
}
