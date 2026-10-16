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
	// Kind, when creating, is report (the default) or scan; a saved
	// report keeps its kind.
	Kind string `json:"kind"`
}

// parseSpec checks a spec, its bindings' field keys included, and returns
// it with its JSON in a canonical form.
func (s *Server) parseSpec(raw json.RawMessage) (*reports.Spec, []byte, error) {
	return s.parseSpecOf(reports.KindReport, raw)
}

// parseSpecOf checks a spec as a report or a scan template.
func (s *Server) parseSpecOf(kind string, raw json.RawMessage) (*reports.Spec, []byte, error) {
	parse := reports.Parse
	if kind == reports.KindScan {
		parse = reports.ParseScan
	}
	spec, err := parse(raw, func(key string) bool {
		_, err := s.store.GetFieldByKey(key)
		return err == nil
	})
	if err != nil {
		return nil, nil, err
	}
	canonical, err := json.Marshal(spec)
	return spec, canonical, err
}

// runSpec runs a report against the assets its filter finds among those
// the request can read.
func (s *Server) runSpec(r *http.Request, spec *reports.Spec) (*reports.Result, error) {
	f := spec.Filter
	assets, err := s.store.FilterAssets(store.AssetFilter{Search: f.Search, Archived: f.Archived, Condition: f.Condition, Loan: f.Loan})
	if err == nil {
		assets, err = s.readable(r, assets)
	}
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

// handleListReports: GET /api/v1/reports[?kind=report|scan]
func (s *Server) handleListReports(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListReports()
	if err != nil {
		writeServerError(w, err, "list reports")
		return
	}
	// ?kind=report or scan lists those alone.
	if kind := r.URL.Query().Get("kind"); kind != "" {
		kept := list[:0]
		for _, rep := range list {
			if rep.Kind == kind {
				kept = append(kept, rep)
			}
		}
		list = kept
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
	kind := req.Kind
	id := r.PathValue("id")
	if id != "" {
		existing, err := s.store.GetReport(id)
		if err != nil {
			writeReportError(w, err, "read the report")
			return
		}
		if kind != "" && kind != existing.Kind {
			writeJSONError(w, http.StatusBadRequest, "A report keeps its kind")
			return
		}
		kind = existing.Kind
	}
	switch kind {
	case "":
		kind = reports.KindReport
	case reports.KindReport, reports.KindScan:
	default:
		writeJSONError(w, http.StatusBadRequest, "kind must be report or scan")
		return
	}
	_, canonical, err := s.parseSpecOf(kind, req.Spec)
	if err != nil {
		writeReportError(w, err, "check the report")
		return
	}
	var report *models.Report
	status := http.StatusOK
	switch {
	case id != "":
		report, err = s.store.UpdateReport(id, req.Name, canonical)
	case kind == reports.KindScan:
		report, err = s.store.CreateScanTemplate(req.Name, canonical)
		status = http.StatusCreated
	default:
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
func (s *Server) savedReport(w http.ResponseWriter, r *http.Request, id string) (*models.Report, *reports.Result, bool) {
	report, err := s.store.GetReport(id)
	if err != nil {
		writeReportError(w, err, "read the report")
		return nil, nil, false
	}
	if report.Kind == reports.KindScan {
		writeJSONError(w, http.StatusConflict, "A scan template shows one scanned asset; preview it with one")
		return nil, nil, false
	}
	spec, _, err := s.parseSpec(report.Spec)
	if err != nil {
		// A field it names was deleted since it was saved.
		writeJSONError(w, http.StatusConflict, "The report no longer fits: "+err.Error()+". Edit it to fix this.")
		return nil, nil, false
	}
	result, err := s.runSpec(r, spec)
	if err != nil {
		writeServerError(w, err, "run the report")
		return nil, nil, false
	}
	return report, result, true
}

// handleRunReport: GET /api/v1/reports/{id}/run
func (s *Server) handleRunReport(w http.ResponseWriter, r *http.Request) {
	report, result, ok := s.savedReport(w, r, r.PathValue("id"))
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
		// Kind scan previews a scan template for AssetID.
		Kind    string `json:"kind"`
		AssetID string `json:"asset_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Spec) == 0 {
		writeJSONError(w, http.StatusBadRequest, "spec is required")
		return
	}
	if req.Kind == reports.KindScan {
		spec, _, err := s.parseSpecOf(reports.KindScan, req.Spec)
		if err != nil {
			writeReportError(w, err, "check the scan template")
			return
		}
		asset, ok := s.assetFor(w, r, req.AssetID)
		if !ok {
			return
		}
		result, err := s.runScan(r, spec, asset)
		if err != nil {
			writeServerError(w, err, "run the scan template")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"result": result})
		return
	}
	spec, _, err := s.parseSpec(req.Spec)
	if err != nil {
		writeReportError(w, err, "check the report")
		return
	}
	result, err := s.runSpec(r, spec)
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
	report, result, ok := s.savedReport(w, r, r.PathValue("id"))
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
	case errors.Is(err, reports.ErrInvalidSpec), errors.Is(err, store.ErrInvalidReport), errors.Is(err, store.ErrNotScanTemplate):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, store.ErrReportInUse):
		writeJSONError(w, http.StatusConflict, err.Error())
	default:
		writeStoreError(w, err, action)
	}
}

// runScan lays out a scan template for an asset the request may see, with
// the assets it holds that the request may read.
func (s *Server) runScan(r *http.Request, spec *reports.Spec, asset *models.AssetWithDetails) (*reports.Result, error) {
	types, err := s.store.ListRelationTypes()
	if err != nil {
		return nil, err
	}
	location := map[string]bool{}
	for _, t := range types {
		location[t.ID] = t.IsLocation
	}
	var contents []models.AssetWithDetails
	for _, l := range asset.Links {
		if !l.Outgoing || !location[l.TypeID] || l.Asset.Archived || len(contents) >= maxScanContents {
			continue
		}
		a, err := s.store.GetAssetByID(l.Asset.ID)
		if errors.Is(err, store.ErrAssetNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		contents = append(contents, *a)
	}
	if contents, err = s.readable(r, contents); err != nil {
		return nil, err
	}
	shown := s.shown(r, asset)
	return spec.RunScan(shown, contents, func(a *models.AssetWithDetails) labels.Data {
		return bindingData(a, s.cfg.Server.BaseURL)
	}), nil
}

// maxScanContents bounds how many of what a scanned asset holds a scan
// shows.
const maxScanContents = 500

// handleSetDefaultScanTemplate makes a scan template the one assets show
// when no category names one: POST /api/v1/reports/{id}/default
func (s *Server) handleSetDefaultScanTemplate(w http.ResponseWriter, r *http.Request) {
	if err := s.store.SetDefaultScanTemplate(r.PathValue("id")); err != nil {
		writeReportError(w, err, "set the default scan template")
		return
	}
	report, err := s.store.GetReport(r.PathValue("id"))
	if err != nil {
		writeReportError(w, err, "read the report")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"report": report})
}
