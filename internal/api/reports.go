package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/reports"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// ReportRequest creates or edits a report.
type ReportRequest struct {
	Name string          `json:"name"`
	Spec json.RawMessage `json:"spec"`
}

// parseSpec checks a spec, its bindings' field keys included, and returns
// it with its JSON in a canonical form.
func (s *Server) parseSpec(raw json.RawMessage) (*reports.Spec, []byte, error) {
	spec, err := reports.Parse(raw, func(key string) bool {
		_, err := s.store.GetFieldByKey(key)
		return err == nil
	})
	if err != nil {
		return nil, nil, err
	}
	canonical, err := json.Marshal(spec)
	return spec, canonical, err
}

// runSpec runs a report against the assets its filter finds.
func (s *Server) runSpec(spec *reports.Spec) (*reports.Result, error) {
	f := spec.Filter
	assets, err := s.store.FilterAssets(store.AssetFilter{Search: f.Search, Archived: f.Archived, Condition: f.Condition, Loan: f.Loan})
	if err != nil {
		return nil, err
	}
	categories, err := s.store.ListCategories()
	if err != nil {
		return nil, err
	}
	kept := make([]models.AssetWithDetails, 0, len(assets))
	for i := range assets {
		if f.Matches(&assets[i], categories) {
			kept = append(kept, assets[i])
		}
	}
	now := time.Now()
	var history []models.Event
	if kinds, since := spec.HistoryKinds(now); len(kinds) > 0 {
		if history, err = s.store.AssetEventsSince(kinds, since); err != nil {
			return nil, err
		}
	}
	return spec.Run(kept, categories, func(a *models.AssetWithDetails) labels.Data {
		return bindingData(a, s.cfg.Server.BaseURL)
	}, history, now), nil
}

// handleListReports: GET /api/v1/reports
func (s *Server) handleListReports(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListReports()
	if err != nil {
		writeServerError(w, err, "list reports")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reports": list})
}

// handleGetReport: GET /api/v1/reports/{id}
func (s *Server) handleGetReport(w http.ResponseWriter, r *http.Request) {
	report, err := s.store.GetReport(r.PathValue("id"))
	if err != nil {
		writeReportError(w, err, "read the report")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"report": report})
}

// handleSaveReport creates a report (POST /api/v1/reports) or replaces one
// (PUT /api/v1/reports/{id}).
func (s *Server) handleSaveReport(w http.ResponseWriter, r *http.Request) {
	var req ReportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Spec) == 0 {
		writeJSONError(w, http.StatusBadRequest, "name and spec are required")
		return
	}
	_, canonical, err := s.parseSpec(req.Spec)
	if err != nil {
		writeReportError(w, err, "check the report")
		return
	}
	var report *models.Report
	status := http.StatusOK
	if id := r.PathValue("id"); id != "" {
		report, err = s.store.UpdateReport(id, req.Name, canonical)
	} else {
		report, err = s.store.CreateReport(req.Name, canonical)
		status = http.StatusCreated
	}
	if err != nil {
		writeReportError(w, err, "save the report")
		return
	}
	writeJSON(w, status, map[string]any{"report": report})
}

// handleDeleteReport: DELETE /api/v1/reports/{id}
func (s *Server) handleDeleteReport(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteReport(r.PathValue("id")); err != nil {
		writeReportError(w, err, "delete the report")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Report deleted"})
}

// savedReport loads a report and runs it.
func (s *Server) savedReport(w http.ResponseWriter, id string) (*models.Report, *reports.Result, bool) {
	report, err := s.store.GetReport(id)
	if err != nil {
		writeReportError(w, err, "read the report")
		return nil, nil, false
	}
	spec, _, err := s.parseSpec(report.Spec)
	if err != nil {
		// A field it names was deleted since it was saved.
		writeJSONError(w, http.StatusConflict, "The report no longer fits: "+err.Error()+". Edit it to fix this.")
		return nil, nil, false
	}
	result, err := s.runSpec(spec)
	if err != nil {
		writeServerError(w, err, "run the report")
		return nil, nil, false
	}
	return report, result, true
}

// handleRunReport: GET /api/v1/reports/{id}/run
func (s *Server) handleRunReport(w http.ResponseWriter, r *http.Request) {
	report, result, ok := s.savedReport(w, r.PathValue("id"))
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"report": report, "result": result})
}

// handlePreviewReport runs an unsaved spec, for the editor:
// POST /api/v1/reports/preview
func (s *Server) handlePreviewReport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Spec json.RawMessage `json:"spec"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Spec) == 0 {
		writeJSONError(w, http.StatusBadRequest, "spec is required")
		return
	}
	spec, _, err := s.parseSpec(req.Spec)
	if err != nil {
		writeReportError(w, err, "check the report")
		return
	}
	result, err := s.runSpec(spec)
	if err != nil {
		writeServerError(w, err, "run the report")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": result})
}

// handleReportCSV downloads one table of a report as CSV:
// GET /api/v1/reports/{id}/sections/{n}/csv, counting sections from 1.
func (s *Server) handleReportCSV(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || n < 1 {
		writeJSONError(w, http.StatusBadRequest, "The section is a number from 1")
		return
	}
	report, result, ok := s.savedReport(w, r.PathValue("id"))
	if !ok {
		return
	}
	if n > len(result.Sections) {
		writeJSONError(w, http.StatusNotFound, "The report has no such section")
		return
	}
	var buf bytes.Buffer
	if err := reports.WriteCSV(&buf, result.Sections[n-1]); err != nil {
		writeServerError(w, err, "write the CSV")
		return
	}
	name := nameStem(report.Name, "report")
	if len(result.Sections) > 1 {
		name += "-" + strconv.Itoa(n)
	}
	setDownloadHeaders(w, "text/csv; charset=utf-8", name+".csv", "attachment")
	_, _ = w.Write(buf.Bytes())
}

func writeReportError(w http.ResponseWriter, err error, action string) {
	switch {
	case errors.Is(err, store.ErrReportNotFound):
		writeJSONError(w, http.StatusNotFound, "Report not found")
	case errors.Is(err, reports.ErrInvalidSpec), errors.Is(err, store.ErrInvalidReport):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	default:
		writeStoreError(w, err, action)
	}
}
