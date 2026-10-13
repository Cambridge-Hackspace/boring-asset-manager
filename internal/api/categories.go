package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// CreateCategoryRequest defines the payload for creating a category.
type CreateCategoryRequest struct {
	Name         string  `json:"name"`
	ParentID     *string `json:"parent_id,omitempty"`
	ColorHex     string  `json:"color_hex,omitempty"`
	IsConsumable bool    `json:"is_consumable"`
	IsRunningLow bool    `json:"is_running_low"`
}

// UpdateCategoryRequest defines the payload for updating a category.
type UpdateCategoryRequest struct {
	Name         string  `json:"name"`
	ParentID     *string `json:"parent_id,omitempty"`
	ColorHex     string  `json:"color_hex,omitempty"`
	IsConsumable bool    `json:"is_consumable"`
	IsRunningLow bool    `json:"is_running_low"`
}

// AssignCategoryFieldRequest defines the payload for mapping a field to a category.
type AssignCategoryFieldRequest struct {
	FieldID    string `json:"field_id"`
	IsRequired bool   `json:"is_required"`
}

func (s *Server) handleListCategories(w http.ResponseWriter, r *http.Request) {
	categories, err := s.store.ListCategories()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Failed to list categories")
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
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to query category: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"category": cat,
	})
}

func (s *Server) handleCreateCategory(w http.ResponseWriter, r *http.Request) {
	var req CreateCategoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	cat, err := s.store.CreateCategory(req.Name, req.ParentID, req.ColorHex, req.IsConsumable, req.IsRunningLow)
	if errors.Is(err, store.ErrCycleDetected) || errors.Is(err, store.ErrSelfParent) {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"category": cat,
	})
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

	cat, err := s.store.UpdateCategory(id, req.Name, req.ParentID, req.ColorHex, req.IsConsumable, req.IsRunningLow)
	if errors.Is(err, store.ErrCategoryNotFound) {
		writeJSONError(w, http.StatusNotFound, "Category not found")
		return
	}
	if errors.Is(err, store.ErrCycleDetected) || errors.Is(err, store.ErrSelfParent) {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
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

	err := s.store.DeleteCategory(id)
	if errors.Is(err, store.ErrCategoryNotFound) {
		writeJSONError(w, http.StatusNotFound, "Category not found")
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Category deleted successfully",
	})
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
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	effectiveFields, err := s.store.GetEffectiveCategoryFields(id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to compute inherited fields: %v", err))
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
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to assign field: %v", err))
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
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to remove field from category: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Field removed from category successfully",
	})
}
