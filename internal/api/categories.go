package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// CreateCategoryRequest defines the payload for creating a category.
type CreateCategoryRequest struct {
	Name              string  `json:"name"`
	ParentID          *string `json:"parent_id,omitempty"`
	ColorHex          string  `json:"color_hex,omitempty"`
	IsConsumable      bool    `json:"is_consumable"`
	Unit              string  `json:"unit"`
	IsLendable        bool    `json:"is_lendable"`
	IsRunningLow      bool    `json:"is_running_low"`
	LowStockThreshold *int    `json:"low_stock_threshold"`
}

// UpdateCategoryRequest defines the payload for updating a category. It
// replaces every property, so an absent low_stock_threshold removes it.
type UpdateCategoryRequest CreateCategoryRequest

func (r CreateCategoryRequest) input() store.CategoryInput {
	return store.CategoryInput{
		Name:              r.Name,
		ParentID:          r.ParentID,
		ColorHex:          r.ColorHex,
		IsConsumable:      r.IsConsumable,
		Unit:              r.Unit,
		IsLendable:        r.IsLendable,
		IsRunningLow:      r.IsRunningLow,
		LowStockThreshold: r.LowStockThreshold,
	}
}

// RunningLowRequest flags or clears a category by hand.
type RunningLowRequest struct {
	RunningLow *bool `json:"running_low"`
}

// AssignCategoryFieldRequest defines the payload for mapping a field to a category.
type AssignCategoryFieldRequest struct {
	FieldID    string `json:"field_id"`
	IsRequired bool   `json:"is_required"`
}

func (s *Server) handleListCategories(w http.ResponseWriter, r *http.Request) {
	categories, err := s.store.ListCategories()
	if err != nil {
		writeServerError(w, err, "list categories")
		return
	}
	if categories == nil {
		categories = []models.Category{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"categories": categories,
	})
}

func (s *Server) handleGetCategory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "Category ID is required")
		return
	}

	cat, err := s.store.GetCategoryByID(id)
	if errors.Is(err, store.ErrCategoryNotFound) {
		writeJSONError(w, http.StatusNotFound, "Category not found")
		return
	}
	if err != nil {
		writeServerError(w, err, "query category")
		return
	}

	count, err := s.store.ActiveCount(id)
	if err != nil {
		writeServerError(w, err, "count assets")
		return
	}

	body := map[string]any{
		"category":     cat,
		"active_count": count,
	}
	// A category with a unit is measured by the total of its stock in it.
	if cat.Unit != "" {
		total, err := s.store.CategoryTotal(id, cat.Unit)
		if err != nil {
			writeServerError(w, err, "total the stock")
			return
		}
		body["total"] = total
	}
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) handleCreateCategory(w http.ResponseWriter, r *http.Request) {
	var req CreateCategoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	cat, err := s.actingStore(r).CreateCategoryFrom(req.input())
	if err != nil {
		writeStoreError(w, err, "create the category")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"category": cat,
	})
}

// CloneCategoryRequest names a category's copy; an empty or absent name
// names it "<name> copy".
type CloneCategoryRequest struct {
	Name string `json:"name"`
}

// handleCloneCategory: POST /api/v1/categories/{id}/clone copies a category
// beside it, without its subcategories or assets (docs/data-model.md). Its scopes
// are copied too, though setting them otherwise needs users.manage: a copy
// without them would put its assets where its original's scopes keep them
// from being seen.
func (s *Server) handleCloneCategory(w http.ResponseWriter, r *http.Request) {
	var req CloneCategoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	cat, err := s.actingStore(r).CloneCategory(r.PathValue("id"), req.Name)
	if errors.Is(err, store.ErrCategoryNotFound) {
		writeJSONError(w, http.StatusNotFound, "Category not found")
		return
	}
	if err != nil {
		writeStoreError(w, err, "copy the category")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"category": cat})
}

func (s *Server) handleUpdateCategory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "Category ID is required")
		return
	}

	var req UpdateCategoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	cat, err := s.actingStore(r).UpdateCategoryFrom(id, CreateCategoryRequest(req).input())
	if errors.Is(err, store.ErrCategoryNotFound) {
		writeJSONError(w, http.StatusNotFound, "Category not found")
		return
	}
	if err != nil {
		writeStoreError(w, err, "update the category")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"category": cat,
	})
}

func (s *Server) handleDeleteCategory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "Category ID is required")
		return
	}

	// Its icon goes first, which is refused if other icons use it.
	if err := s.removeCategoryIcon(r, id); err != nil {
		writeIconError(w, err, "delete the category")
		return
	}
	err := s.actingStore(r).DeleteCategory(id)
	if errors.Is(err, store.ErrCategoryNotFound) {
		writeJSONError(w, http.StatusNotFound, "Category not found")
		return
	}
	if err != nil {
		writeStoreError(w, err, "delete the category")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Category deleted successfully",
	})
}

// handleSetRunningLow flags or clears a consumable category by hand:
// PUT /api/v1/categories/{id}/running-low
func (s *Server) handleSetRunningLow(w http.ResponseWriter, r *http.Request) {
	var req RunningLowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RunningLow == nil {
		writeJSONError(w, http.StatusBadRequest, "running_low (true or false) is required")
		return
	}
	// Flagging a category flags its assets: it needs stock.record in the
	// scopes they are in for being in it.
	scopes, err := s.store.ScopesForCategories([]string{r.PathValue("id")})
	if err != nil {
		writeServerError(w, err, "read the category's scopes")
		return
	}
	if !Allows(r.Context(), rbac.PermStockRecord, scopes) {
		writeJSONError(w, http.StatusForbidden, "Forbidden: insufficient permissions for this category's scopes")
		return
	}
	cat, changed, err := s.actingStore(r).SetRunningLow(r.PathValue("id"), *req.RunningLow)
	if errors.Is(err, store.ErrCategoryNotFound) {
		writeJSONError(w, http.StatusNotFound, "Category not found")
		return
	}
	if errors.Is(err, store.ErrRunningLowRequiresConsumable) {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeServerError(w, err, "update the category")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"category": cat, "changed": changed})
}

func (s *Server) handleGetCategoryEffectiveFields(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "Category ID is required")
		return
	}

	if _, err := s.store.GetCategoryByID(id); err != nil {
		if errors.Is(err, store.ErrCategoryNotFound) {
			writeJSONError(w, http.StatusNotFound, "Category not found")
			return
		}
		writeServerError(w, err, "find the category")
		return
	}

	effectiveFields, err := s.store.GetEffectiveCategoryFields(id)
	if err != nil {
		writeServerError(w, err, "compute inherited fields")
		return
	}
	if effectiveFields == nil {
		effectiveFields = []models.EffectiveField{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"category_id": id,
		"fields":      effectiveFields,
	})
}

func (s *Server) handleAssignCategoryField(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "Category ID is required")
		return
	}

	var req AssignCategoryFieldRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.FieldID == "" {
		writeJSONError(w, http.StatusBadRequest, "field_id is required")
		return
	}

	err := s.store.AssignCategoryField(id, req.FieldID, req.IsRequired)
	if errors.Is(err, store.ErrCategoryNotFound) || errors.Is(err, store.ErrFieldNotFound) {
		writeJSONError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		writeStoreError(w, err, "assign the field")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Field assigned to category successfully",
	})
}

func (s *Server) handleRemoveCategoryField(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	fieldID := r.PathValue("field_id")
	if id == "" || fieldID == "" {
		writeJSONError(w, http.StatusBadRequest, "Category ID and Field ID are required")
		return
	}

	err := s.store.RemoveCategoryField(id, fieldID)
	if err != nil {
		writeStoreError(w, err, "remove the field from the category")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Field removed from category successfully",
	})
}

// EffectiveFieldsRequest names the categories an asset would belong to.
type EffectiveFieldsRequest struct {
	CategoryIDs []string `json:"category_ids"`
}

// handleEffectiveFieldsForAsset resolves the effective fields for a set of
// categories, for the asset form before the asset exists:
// POST /api/v1/categories/effective-fields
func (s *Server) handleEffectiveFieldsForAsset(w http.ResponseWriter, r *http.Request) {
	var req EffectiveFieldsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	fields, err := s.store.EffectiveFieldsForAsset(req.CategoryIDs)
	if errors.Is(err, store.ErrCategoryNotFound) {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeServerError(w, err, "compute inherited fields")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"fields": fields})
}
