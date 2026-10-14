package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// CreateAssetRequest defines the payload for registering a new asset.
type CreateAssetRequest struct {
	Name          string                     `json:"name"`
	CategoryIDs   []string                   `json:"category_ids,omitempty"`
	GeneratorKeys []models.AssetGeneratorKey `json:"generator_keys,omitempty"`
	FieldValues   map[string]string          `json:"field_values,omitempty"`
}

// UpdateAssetRequest defines the payload for modifying an asset. Omitted parts
// are left as they are: an absent name, category_ids or generator_keys keeps
// the current value, while an empty list clears it. field_values only changes
// the keys it names; an empty value removes one.
type UpdateAssetRequest struct {
	Name          string                     `json:"name,omitempty"`
	CategoryIDs   []string                   `json:"category_ids,omitempty"`
	GeneratorKeys []models.AssetGeneratorKey `json:"generator_keys,omitempty"`
	FieldValues   map[string]string          `json:"field_values,omitempty"`
}

func (s *Server) handleListAssets(w http.ResponseWriter, r *http.Request) {
	categoryID := r.URL.Query().Get("category_id")
	search := r.URL.Query().Get("search")

	assets, err := s.store.ListAssets(categoryID, search)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to list assets: %v", err))
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
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to query asset: %v", err))
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

	asset, err := s.store.CreateAsset(req.Name, req.CategoryIDs, req.GeneratorKeys, req.FieldValues)
	if errors.Is(err, store.ErrGeneratorCodeExists) {
		writeJSONError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
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

	asset, err := s.store.UpdateAsset(id, req.Name, req.CategoryIDs, req.GeneratorKeys, req.FieldValues)
	if errors.Is(err, store.ErrAssetNotFound) {
		writeJSONError(w, http.StatusNotFound, "Asset not found")
		return
	}
	if errors.Is(err, store.ErrGeneratorCodeExists) {
		writeJSONError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
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

	err := s.store.DeleteAsset(id)
	if errors.Is(err, store.ErrAssetNotFound) {
		writeJSONError(w, http.StatusNotFound, "Asset not found")
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to delete asset: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Asset deleted successfully",
	})
}
