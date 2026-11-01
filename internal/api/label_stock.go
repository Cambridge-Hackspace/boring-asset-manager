package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// LabelStockRequest creates or updates custom label stock (docs/printing.md).
type LabelStockRequest struct {
	Name     string  `json:"name"`
	WidthMM  float64 `json:"width_mm"`
	LengthMM float64 `json:"length_mm"`
	GapMM    float64 `json:"gap_mm"`
}

func (req LabelStockRequest) stock(id string) printers.Stock {
	return printers.Stock{ID: id, Name: req.Name, WidthMM: req.WidthMM, LengthMM: req.LengthMM, GapMM: req.GapMM}
}

func (s *Server) handleListLabelStock(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListLabelStock()
	if err != nil {
		writeServerError(w, err, "list label stock")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"stock": list})
}

func (s *Server) handleCreateLabelStock(w http.ResponseWriter, r *http.Request) {
	var req LabelStockRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	st, err := s.store.CreateLabelStock(req.stock(""))
	if writeLabelStockError(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"stock": st})
}

func (s *Server) handleUpdateLabelStock(w http.ResponseWriter, r *http.Request) {
	var req LabelStockRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	st, err := s.store.UpdateLabelStock(req.stock(r.PathValue("id")))
	if writeLabelStockError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"stock": st})
}

func (s *Server) handleDeleteLabelStock(w http.ResponseWriter, r *http.Request) {
	if writeLabelStockError(w, s.store.DeleteLabelStock(r.PathValue("id"))) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Label stock deleted"})
}

func writeLabelStockError(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, store.ErrLabelStockNotFound):
		writeJSONError(w, http.StatusNotFound, "Label stock not found")
	case errors.Is(err, store.ErrLabelStockNameExists), errors.Is(err, store.ErrLabelStockInUse):
		writeJSONError(w, http.StatusConflict, err.Error())
	case errors.Is(err, printers.ErrInvalidPrinter):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	default:
		writeStoreError(w, err, "save the label stock")
	}
	return true
}
