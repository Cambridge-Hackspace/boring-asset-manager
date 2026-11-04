package api

import (
	"bytes"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/assetcsv"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/generators"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/reports"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/xlsx"
)

// xlsxType is a workbook's content type.
const xlsxType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// writeWorkbook sends sheets as a workbook to download as filename.
func writeWorkbook(w http.ResponseWriter, sheets []xlsx.Sheet, filename string) {
	var buf bytes.Buffer
	if err := xlsx.Write(&buf, sheets); err != nil {
		writeServerError(w, err, "write the workbook")
		return
	}
	setDownloadHeaders(w, xlsxType, filename, "attachment")
	_, _ = w.Write(buf.Bytes())
}

// handleExportAssetsXLSX exports the asset list as a workbook:
// GET /api/v1/assets/export.xlsx, with the CSV export's filters
// (docs/csv.md#exporting-to-excel).
func (s *Server) handleExportAssetsXLSX(w http.ResponseWriter, r *http.Request) {
	assets, cats, refs, ok := s.exportedAssets(w, r)
	if !ok {
		return
	}
	fields, err := s.store.ListFields()
	if err != nil {
		writeServerError(w, err, "list fields")
		return
	}
	sheets, err := assetcsv.Workbook(assets, cats, generators.AllowedGeneratorTypes(), refs, fields, s.store.GetEffectiveCategoryFields)
	if err != nil {
		writeServerError(w, err, "read the categories' fields")
		return
	}
	writeWorkbook(w, sheets, fmt.Sprintf("assets-%s.xlsx", time.Now().UTC().Format("20060102")))
}

// sectionSheetName is a section's sheet's name: its title, or its place.
func sectionSheetName(sec reports.SectionResult, n int) string {
	if sec.Title != "" {
		return sec.Title
	}
	return "Section " + strconv.Itoa(n)
}

// handleReportSectionXLSX downloads one section of a report as a
// workbook: GET /api/v1/reports/{id}/sections/{n}/xlsx.
func (s *Server) handleReportSectionXLSX(w http.ResponseWriter, r *http.Request) {
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
	sec := result.Sections[n-1]
	name := nameStem(report.Name, "report")
	if len(result.Sections) > 1 {
		name += "-" + strconv.Itoa(n)
	}
	writeWorkbook(w, []xlsx.Sheet{reports.Sheet(sec, sectionSheetName(sec, n), map[string]bool{})}, name+".xlsx")
}

// handleReportXLSX downloads a whole report as a workbook, a sheet per
// table or chart: GET /api/v1/reports/{id}/xlsx.
func (s *Server) handleReportXLSX(w http.ResponseWriter, r *http.Request) {
	report, result, ok := s.savedReport(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	taken := map[string]bool{}
	var sheets []xlsx.Sheet
	for i, sec := range result.Sections {
		if sec.Type == "table" || sec.Type == "chart" {
			sheets = append(sheets, reports.Sheet(sec, sectionSheetName(sec, i+1), taken))
		}
	}
	if len(sheets) == 0 {
		writeJSONError(w, http.StatusNotFound, "The report has no tables or charts")
		return
	}
	writeWorkbook(w, sheets, nameStem(report.Name, "report")+".xlsx")
}
