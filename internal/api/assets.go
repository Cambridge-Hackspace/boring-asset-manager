package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/assetcsv"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/generators"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// CreateAssetRequest defines the payload for registering a new asset.
type CreateAssetRequest struct {
	Name          string                     `json:"name"`
	CategoryIDs   []string                   `json:"category_ids,omitempty"`
	GeneratorKeys []models.AssetGeneratorKey `json:"generator_keys,omitempty"`
	FieldValues   map[string]string          `json:"field_values,omitempty"`
	// Stock counts the asset's stock by quantity; absent or null does not.
	Stock *StockRequest `json:"stock,omitempty"`
}

// UpdateAssetRequest defines the payload for modifying an asset. Omitted parts
// are left as they are: an absent name, category_ids or generator_keys keeps
// the current value, while an empty list clears it. field_values only changes
// the keys it names; an empty value removes one. An absent stock keeps the
// asset's, null stops counting it by quantity, and an object replaces it.
type UpdateAssetRequest struct {
	Name          string                     `json:"name,omitempty"`
	CategoryIDs   []string                   `json:"category_ids,omitempty"`
	GeneratorKeys []models.AssetGeneratorKey `json:"generator_keys,omitempty"`
	FieldValues   map[string]string          `json:"field_values,omitempty"`
	Stock         json.RawMessage            `json:"stock,omitempty"`
}

// StockRequest is an asset's stock (README §5.1). An absent quantity keeps
// the asset's current one, so an edit cannot undo a use made meanwhile; an
// asset whose stock was not counted starts at 0.
type StockRequest struct {
	Quantity          *int   `json:"quantity"`
	Unit              string `json:"unit"`
	LowStockThreshold *int   `json:"low_stock_threshold"`
}

func (r *StockRequest) input() *store.StockInput {
	if r == nil {
		return nil
	}
	return &store.StockInput{Quantity: r.Quantity, Unit: r.Unit, LowStockThreshold: r.LowStockThreshold}
}

// stockUpdate reads an update's stock: nil when absent.
func (r UpdateAssetRequest) stockUpdate() (*store.StockUpdate, error) {
	if len(r.Stock) == 0 {
		return nil, nil
	}
	var req *StockRequest
	if err := json.Unmarshal(r.Stock, &req); err != nil {
		return nil, err
	}
	return &store.StockUpdate{Stock: req.input()}, nil
}

// QuantityRequest changes an asset's quantity: by adjust (negative to use
// some, positive to restock) or to set. Exactly one is given.
type QuantityRequest struct {
	Adjust *int `json:"adjust"`
	Set    *int `json:"set"`
}

// historyPage is how many events one page of
// GET /api/v1/assets/{id}/history holds.
const historyPage = 50

func (s *Server) handleListAssets(w http.ResponseWriter, r *http.Request) {
	categoryID := r.URL.Query().Get("category_id")
	search := r.URL.Query().Get("search")

	list := s.store.ListAssets
	switch r.URL.Query().Get("archived") {
	case "", "false":
	case "true":
		list = s.store.ListArchivedAssets
	default:
		writeJSONError(w, http.StatusBadRequest, "archived must be true or false")
		return
	}
	assets, err := list(categoryID, search)
	if err != nil {
		writeServerError(w, err, "list assets")
		return
	}
	if assets == nil {
		assets = []models.AssetWithDetails{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"assets": assets,
	})
}

func (s *Server) handleGetAsset(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "Asset ID is required")
		return
	}

	asset, err := s.store.GetAssetByID(id)
	if errors.Is(err, store.ErrAssetNotFound) {
		writeJSONError(w, http.StatusNotFound, "Asset not found")
		return
	}
	if err != nil {
		writeServerError(w, err, "query asset")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"asset": asset,
	})
}

func (s *Server) handleCreateAsset(w http.ResponseWriter, r *http.Request) {
	var req CreateAssetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	asset, err := s.actingStore(r).CreateAssetWithStock(req.Name, req.CategoryIDs, req.GeneratorKeys, req.FieldValues, req.Stock.input())
	if errors.Is(err, store.ErrGeneratorCodeExists) {
		writeJSONError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeStoreError(w, err, "create the asset")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"asset": asset,
	})
}

func (s *Server) handleUpdateAsset(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "Asset ID is required")
		return
	}

	var req UpdateAssetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	stock, err := req.stockUpdate()
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid stock")
		return
	}

	asset, err := s.actingStore(r).UpdateAssetWithStock(id, req.Name, req.CategoryIDs, req.GeneratorKeys, req.FieldValues, stock)
	if errors.Is(err, store.ErrAssetNotFound) {
		writeJSONError(w, http.StatusNotFound, "Asset not found")
		return
	}
	if errors.Is(err, store.ErrGeneratorCodeExists) {
		writeJSONError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeStoreError(w, err, "update the asset")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"asset": asset,
	})
}

func (s *Server) handleDeleteAsset(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "Asset ID is required")
		return
	}

	err := s.actingStore(r).DeleteAsset(id)
	if errors.Is(err, store.ErrAssetNotFound) {
		writeJSONError(w, http.StatusNotFound, "Asset not found")
		return
	}
	if err != nil {
		writeStoreError(w, err, "archive the asset")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Asset deleted successfully",
	})
}

// handleExportAssets writes the assets the list would show as CSV
// (README §6.1): GET /api/v1/assets/export.csv?category_id=&search=
func (s *Server) handleExportAssets(w http.ResponseWriter, r *http.Request) {
	assets, err := s.store.ListAssets(r.URL.Query().Get("category_id"), r.URL.Query().Get("search"))
	if err != nil {
		writeServerError(w, err, "list assets")
		return
	}
	cats, err := s.store.ListAllCategories()
	if err != nil {
		writeServerError(w, err, "list categories")
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="assets-%s.csv"`, time.Now().UTC().Format("20060102")))
	if err := assetcsv.Export(w, assets, assetcsv.NewCategories(cats), generators.AllowedGeneratorTypes()); err != nil {
		// Headers are gone; all that is left is to stop.
		return
	}
}

// handleAdjustQuantity uses up or restocks an asset counted by quantity:
// POST /api/v1/assets/{id}/quantity
func (s *Server) handleAdjustQuantity(w http.ResponseWriter, r *http.Request) {
	var req QuantityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || (req.Adjust == nil) == (req.Set == nil) {
		writeJSONError(w, http.StatusBadRequest, "Give either adjust or set, as a whole number")
		return
	}
	delta := 0
	if req.Adjust != nil {
		delta = *req.Adjust
	}
	asset, err := s.actingStore(r).AdjustQuantity(r.PathValue("id"), delta, req.Set)
	switch {
	case errors.Is(err, store.ErrAssetNotFound):
		writeJSONError(w, http.StatusNotFound, "Asset not found")
	case errors.Is(err, store.ErrNotEnoughStock):
		writeJSONError(w, http.StatusConflict, err.Error())
	case err != nil:
		writeStoreError(w, err, "change the quantity")
	default:
		writeJSON(w, http.StatusOK, map[string]any{"asset": asset})
	}
}

// handleSetAssetRunningLow flags or clears an asset counted by quantity by
// hand: PUT /api/v1/assets/{id}/running-low
func (s *Server) handleSetAssetRunningLow(w http.ResponseWriter, r *http.Request) {
	var req RunningLowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RunningLow == nil {
		writeJSONError(w, http.StatusBadRequest, "running_low (true or false) is required")
		return
	}
	asset, changed, err := s.actingStore(r).SetAssetRunningLow(r.PathValue("id"), *req.RunningLow)
	switch {
	case errors.Is(err, store.ErrAssetNotFound):
		writeJSONError(w, http.StatusNotFound, "Asset not found")
	case err != nil:
		writeStoreError(w, err, "update the asset")
	default:
		writeJSON(w, http.StatusOK, map[string]any{"asset": asset, "changed": changed})
	}
}

// handleAssetHistory lists an asset's events a page at a time, newest first:
// GET /api/v1/assets/{id}/history[?before=<event id>]
func (s *Server) handleAssetHistory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.store.GetAssetByID(id); errors.Is(err, store.ErrAssetNotFound) {
		writeJSONError(w, http.StatusNotFound, "Asset not found")
		return
	} else if err != nil {
		writeServerError(w, err, "query asset")
		return
	}
	events, more, err := s.store.ListAssetEvents(id, r.URL.Query().Get("before"), historyPage)
	if errors.Is(err, store.ErrEventNotFound) {
		writeJSONError(w, http.StatusBadRequest, "before must name one of this asset's events")
		return
	}
	if err != nil {
		writeServerError(w, err, "list the asset's history")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events, "more": more})
}

// handleRestoreAsset brings an archived asset back, with the codes archiving
// released that no other asset has taken since:
// POST /api/v1/assets/{id}/restore
func (s *Server) handleRestoreAsset(w http.ResponseWriter, r *http.Request) {
	asset, skipped, err := s.actingStore(r).RestoreAsset(r.PathValue("id"))
	switch {
	case errors.Is(err, store.ErrAssetNotFound):
		writeJSONError(w, http.StatusNotFound, "Asset not found")
	case errors.Is(err, store.ErrAssetNotArchived):
		writeJSONError(w, http.StatusConflict, "The asset is not archived")
	case errors.Is(err, store.ErrGeneratorCodeExists):
		writeJSONError(w, http.StatusConflict, "Another asset took one of its codes at the same moment; try again")
	case err != nil:
		writeStoreError(w, err, "restore the asset")
	default:
		writeJSON(w, http.StatusOK, map[string]any{"asset": asset, "skipped_codes": skipped})
	}
}
