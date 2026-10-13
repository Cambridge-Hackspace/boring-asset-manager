package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// CreateFieldRequest defines the payload for creating a field.
type CreateFieldRequest struct {
	Key            string          `json:"key"`
	Label          string          `json:"label"`
	ValidationType string          `json:"validation_type"`
	Options        json.RawMessage `json:"options,omitempty"`
}

// UpdateFieldRequest defines the payload for updating a field.
type UpdateFieldRequest struct {
	Label          string          `json:"label"`
	ValidationType string          `json:"validation_type"`
	Options        json.RawMessage `json:"options,omitempty"`
}

func (s *Server) handleListFields(w http.ResponseWriter, r *http.Request) {
	fields, err := s.store.ListFields()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Failed to list fields")
		return
	}
	if fields == nil {
		fields = []models.Field{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"fields": fields,
	})
}

func (s *Server) handleGetField(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "Field ID is required")
		return
	}

	f, err := s.store.GetFieldByID(id)
	if errors.Is(err, store.ErrFieldNotFound) {
		writeJSONError(w, http.StatusNotFound, "Field not found")
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to query field: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"field": f,
	})
}

func (s *Server) handleCreateField(w http.ResponseWriter, r *http.Request) {
	var req CreateFieldRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	f, err := s.store.CreateField(req.Key, req.Label, req.ValidationType, req.Options)
	if errors.Is(err, store.ErrFieldKeyExists) {
		writeJSONError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"field": f,
	})
}

func (s *Server) handleUpdateField(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "Field ID is required")
		return
	}

	var req UpdateFieldRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	f, err := s.store.UpdateField(id, req.Label, req.ValidationType, req.Options)
	if errors.Is(err, store.ErrFieldNotFound) {
		writeJSONError(w, http.StatusNotFound, "Field not found")
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"field": f,
	})
}

func (s *Server) handleDeleteField(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "Field ID is required")
		return
	}

	err := s.store.DeleteField(id)
	if errors.Is(err, store.ErrFieldNotFound) {
		writeJSONError(w, http.StatusNotFound, "Field not found")
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to delete field: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Field deleted successfully",
	})
}
