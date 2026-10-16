package api

import (
	"encoding/json"
	"errors"
	"net/http"

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
