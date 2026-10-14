package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"net/http"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// MaxCopies caps how many copies of each label one print request makes.
const MaxCopies = 20

// PrinterRequest creates or updates a printer.
type PrinterRequest struct {
	Name   string `json:"name"`
	Driver string `json:"driver"`
	Model  string `json:"model"`
	// Connection says how to reach the printer (README §4.4).
	Connection printers.Connection `json:"connection"`
	Media      string              `json:"media"`
	// Options are driver options (see GET /api/v1/printers/drivers).
	Options json.RawMessage `json:"options,omitempty"`
}

func (req PrinterRequest) printer(id string) models.Printer {
	return models.Printer{ID: id, Name: req.Name, Driver: req.Driver, Model: req.Model, Connection: req.Connection, Media: req.Media, Options: req.Options}
}

func (s *Server) handlePrinterDrivers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"drivers": printers.Drivers()})
}

func (s *Server) handleListPrinters(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListPrinters()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Failed to list printers")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"printers": list})
}

func (s *Server) handleGetPrinter(w http.ResponseWriter, r *http.Request) {
	p, ok := s.loadPrinter(w, r.PathValue("id"))
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"printer": p})
}

func (s *Server) handleCreatePrinter(w http.ResponseWriter, r *http.Request) {
	var req PrinterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if writePrinterError(w, s.checkConnection(req.Connection)) {
		return
	}
	p, err := s.store.CreatePrinter(req.printer(""))
	if writePrinterError(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"printer": p})
}

func (s *Server) handleUpdatePrinter(w http.ResponseWriter, r *http.Request) {
	var req PrinterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if writePrinterError(w, s.checkConnection(req.Connection)) {
		return
	}
	p, err := s.store.UpdatePrinter(req.printer(r.PathValue("id")))
	if writePrinterError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"printer": p})
}

func (s *Server) handleDeletePrinter(w http.ResponseWriter, r *http.Request) {
	err := s.store.DeletePrinter(r.PathValue("id"))
	if writePrinterError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Printer deleted successfully"})
}

// checkConnection refuses connections this server may not use, such as a
// device outside its allowed paths.
func (s *Server) checkConnection(c printers.Connection) error {
	c, err := printers.NormalizeConnection(c)
	if err != nil {
		return err
	}
	return s.transport.Check(c)
}

func writePrinterError(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, store.ErrPrinterNotFound):
		writeJSONError(w, http.StatusNotFound, "Printer not found")
	case errors.Is(err, store.ErrPrinterNameExists):
		writeJSONError(w, http.StatusConflict, err.Error())
	case errors.Is(err, printers.ErrInvalidPrinter):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	default:
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to store printer: %v", err))
	}
	return true
}

func (s *Server) loadPrinter(w http.ResponseWriter, id string) (*models.Printer, bool) {
	p, err := s.store.GetPrinter(id)
	if writePrinterError(w, err) {
		return nil, false
	}
	return p, true
}

// PrintRequest prints labels for a batch of assets.
type PrintRequest struct {
	TemplateID string   `json:"template_id"`
	AssetIDs   []string `json:"asset_ids"`
	// Copies of each label, printed one asset after another; defaults to 1.
	Copies int `json:"copies,omitempty"`
}

// handlePrint renders labels and sends them to a printer:
// POST /api/v1/printers/{id}/print
func (s *Server) handlePrint(w http.ResponseWriter, r *http.Request) {
	var req PrintRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if req.Copies == 0 {
		req.Copies = 1
	}
	if req.Copies < 1 || req.Copies > MaxCopies {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("copies must be 1-%d", MaxCopies))
		return
	}
	if !checkAssetIDs(w, req.AssetIDs) {
		return
	}
	if total := len(req.AssetIDs) * req.Copies; total > MaxExportAssets {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("a print job is limited to %d labels (asked for %d)", MaxExportAssets, total))
		return
	}

	p, ok := s.loadPrinter(w, r.PathValue("id"))
	if !ok {
		return
	}
	t, ok := s.loadTemplateFor(w, req.TemplateID)
	if !ok {
		return
	}
	driver, err := printers.Lookup(p.Driver)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Printer uses an unavailable driver: %v", err))
		return
	}
	// Check the template against the printer before laying out assets.
	label := printers.LabelSpec{WidthMM: t.Width, HeightMM: t.Height, DPI: t.DPI}
	if t.Units == labels.UnitsIn {
		label.WidthMM, label.HeightMM = t.Width*25.4, t.Height*25.4
	}
	plan, err := driver.Plan(store.PrinterSettings(p), label)
	if errors.Is(err, printers.ErrDoesNotFit) {
		writeJSONError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to plan the print job: %v", err))
		return
	}

	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(exportWriteTimeout))
	scenes, ok := s.layoutBatch(w, t, req.AssetIDs)
	if !ok {
		return
	}
	rendered := make([]*image.Gray, 0, len(scenes)*req.Copies)
	for _, scene := range scenes {
		img := scene.Raster(true)
		for c := 0; c < req.Copies; c++ {
			rendered = append(rendered, img)
		}
	}
	job, err := plan.Encode(rendered)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to encode print job: %v", err))
		return
	}

	if err := s.transport.Send(r.Context(), p.Connection, job); err != nil {
		writeJSONError(w, http.StatusBadGateway, fmt.Sprintf("Could not deliver the job to %s: %v", p.Name, err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"printer_id": p.ID,
		"labels":     len(rendered),
		"bytes":      len(job),
	})
}

// FitRequest asks whether labels of a physical size suit a printer.
type FitRequest struct {
	WidthMM  float64 `json:"width_mm"`
	HeightMM float64 `json:"height_mm"`
	DPI      int     `json:"dpi"`
}

// handlePrinterFit runs a printer's fit check without printing, for the
// template editor: POST /api/v1/printers/{id}/fit
func (s *Server) handlePrinterFit(w http.ResponseWriter, r *http.Request) {
	p, ok := s.loadPrinter(w, r.PathValue("id"))
	if !ok {
		return
	}
	var req FitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.WidthMM <= 0 || req.HeightMM <= 0 || req.DPI <= 0 {
		writeJSONError(w, http.StatusBadRequest, "width_mm, height_mm and dpi must be positive")
		return
	}
	driver, err := printers.Lookup(p.Driver)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_, err = driver.Plan(store.PrinterSettings(p), printers.LabelSpec{WidthMM: req.WidthMM, HeightMM: req.HeightMM, DPI: req.DPI})
	switch {
	case errors.Is(err, printers.ErrDoesNotFit):
		writeJSON(w, http.StatusOK, map[string]any{"fits": false, "message": err.Error()})
	case err != nil:
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Fit check failed: %v", err))
	default:
		writeJSON(w, http.StatusOK, map[string]any{"fits": true})
	}
}
