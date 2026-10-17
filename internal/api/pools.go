package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Printer pools (README §6.2): managed with the printers (settings.manage),
// and listed to those who may see printers.

// PoolRequest creates or replaces a printer pool. BatchSize defaults to 10
// labels, and each member's Share to 1.
type PoolRequest struct {
	Name        string              `json:"name"`
	BatchSize   int                 `json:"batch_size"`
	FixedShares bool                `json:"fixed_shares"`
	Members     []models.PoolMember `json:"members"`
}

func (req PoolRequest) input() store.PoolInput {
	return store.PoolInput{Name: req.Name, BatchSize: req.BatchSize, FixedShares: req.FixedShares, Members: req.Members}
}

// handleListPools: GET /api/v1/printer-pools
func (s *Server) handleListPools(w http.ResponseWriter, r *http.Request) {
	pools, err := s.store.ListPools()
	if err != nil {
		writeServerError(w, err, "list printer pools")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"pools": pools})
}

// handleGetPool: GET /api/v1/printer-pools/{id}
func (s *Server) handleGetPool(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.GetPool(r.PathValue("id"))
	if err != nil {
		writePoolError(w, err, "read the printer pool")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"pool": p})
}

// handleSavePool: POST /api/v1/printer-pools and PUT /api/v1/printer-pools/{id}
func (s *Server) handleSavePool(w http.ResponseWriter, r *http.Request) {
	var req PoolRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	var (
		p   *models.PrinterPool
		err error
	)
	status := http.StatusOK
	if id := r.PathValue("id"); id != "" {
		p, err = s.store.UpdatePool(id, req.input())
	} else {
		p, err = s.store.CreatePool(req.input())
		status = http.StatusCreated
	}
	if err != nil {
		writePoolError(w, err, "save the printer pool")
		return
	}
	writeJSON(w, status, map[string]any{"pool": p})
}

// handleDeletePool: DELETE /api/v1/printer-pools/{id}
func (s *Server) handleDeletePool(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeletePool(r.PathValue("id")); err != nil {
		writePoolError(w, err, "delete the printer pool")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Printer pool deleted"})
}

// writePoolError answers a failure to read or change a pool.
func writePoolError(w http.ResponseWriter, err error, action string) {
	switch {
	case errors.Is(err, store.ErrPoolNotFound):
		writeJSONError(w, http.StatusNotFound, "Printer pool not found")
	case errors.Is(err, store.ErrPoolNameExists):
		writeJSONError(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrInvalidPool):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, store.ErrPrinterNotFound):
		writeJSONError(w, http.StatusBadRequest, "No printer has that ID")
	default:
		writeServerError(w, err, action)
	}
}
