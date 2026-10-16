package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// RelationTypeRequest creates or edits a relation type.
type RelationTypeRequest struct {
	Name        string `json:"name"`
	InverseName string `json:"inverse_name"`
	IsSymmetric bool   `json:"is_symmetric"`
	IsSingle    bool   `json:"is_single"`
	IsLocation  bool   `json:"is_location"`
}

func (r RelationTypeRequest) input() store.RelationTypeInput {
	return store.RelationTypeInput{Name: r.Name, InverseName: r.InverseName, IsSymmetric: r.IsSymmetric, IsSingle: r.IsSingle, IsLocation: r.IsLocation}
}

// handleListRelationTypes: GET /api/v1/relation-types
func (s *Server) handleListRelationTypes(w http.ResponseWriter, r *http.Request) {
	types, err := s.store.ListRelationTypes()
	if err != nil {
		writeServerError(w, err, "list relation types")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"relation_types": types})
}

// handleCreateRelationType: POST /api/v1/relation-types
func (s *Server) handleCreateRelationType(w http.ResponseWriter, r *http.Request) {
	var req RelationTypeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	t, err := s.store.CreateRelationType(req.input())
	if err != nil {
		writeLinkError(w, err, "create the relation type")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"relation_type": t})
}

// handleUpdateRelationType: PUT /api/v1/relation-types/{id}
func (s *Server) handleUpdateRelationType(w http.ResponseWriter, r *http.Request) {
	var req RelationTypeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	t, err := s.store.UpdateRelationType(r.PathValue("id"), req.input())
	if err != nil {
		writeLinkError(w, err, "update the relation type")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"relation_type": t})
}

// handleDeleteRelationType: DELETE /api/v1/relation-types/{id}
func (s *Server) handleDeleteRelationType(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteRelationType(r.PathValue("id")); err != nil {
		writeLinkError(w, err, "delete the relation type")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Relation type deleted"})
}

// CategoryRelationTypesRequest sets the relation types a category suggests.
type CategoryRelationTypesRequest struct {
	RelationTypeIDs []string `json:"relation_type_ids"`
}

// handleCategoryRelationTypes: GET /api/v1/categories/{id}/relation-types
func (s *Server) handleCategoryRelationTypes(w http.ResponseWriter, r *http.Request) {
	ids, err := s.store.CategoryRelationTypes(r.PathValue("id"))
	if err != nil {
		writeLinkError(w, err, "read the category's relations")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"relation_type_ids": ids})
}

// handleSetCategoryRelationTypes: PUT /api/v1/categories/{id}/relation-types
func (s *Server) handleSetCategoryRelationTypes(w http.ResponseWriter, r *http.Request) {
	var req CategoryRelationTypesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RelationTypeIDs == nil {
		writeJSONError(w, http.StatusBadRequest, "relation_type_ids (a list) is required")
		return
	}
	ids, err := s.store.SetCategoryRelationTypes(r.PathValue("id"), req.RelationTypeIDs)
	if err != nil {
		writeLinkError(w, err, "set the category's relations")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"relation_type_ids": ids})
}

// AddLinkRequest links the asset to another. Outgoing reads from the asset
// by the type's name ("contains"), and otherwise by its inverse ("contained
// in").
type AddLinkRequest struct {
	TypeID   string `json:"type_id"`
	AssetID  string `json:"asset_id"`
	Outgoing bool   `json:"outgoing"`
}

// handleAddLink: POST /api/v1/assets/{id}/links
func (s *Server) handleAddLink(w http.ResponseWriter, r *http.Request) {
	var req AddLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TypeID == "" || req.AssetID == "" {
		writeJSONError(w, http.StatusBadRequest, "type_id and asset_id are required")
		return
	}
	a, err := s.actingStore(r).AddLink(r.PathValue("id"), req.TypeID, req.AssetID, req.Outgoing)
	if err != nil {
		writeLinkError(w, err, "link the assets")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"asset": a})
}

// handleRemoveLink: DELETE /api/v1/assets/{id}/links/{link_id}
func (s *Server) handleRemoveLink(w http.ResponseWriter, r *http.Request) {
	a, err := s.actingStore(r).RemoveLink(r.PathValue("id"), r.PathValue("link_id"))
	if err != nil {
		writeLinkError(w, err, "unlink the assets")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"asset": a})
}

func writeLinkError(w http.ResponseWriter, err error, action string) {
	switch {
	case errors.Is(err, store.ErrAssetNotFound):
		writeJSONError(w, http.StatusNotFound, "Asset not found")
	case errors.Is(err, store.ErrCategoryNotFound):
		writeJSONError(w, http.StatusNotFound, "Category not found")
	case errors.Is(err, store.ErrRelationTypeNotFound):
		writeJSONError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, store.ErrLinkNotFound):
		writeJSONError(w, http.StatusNotFound, "Link not found")
	case errors.Is(err, store.ErrRelationTypeInUse), errors.Is(err, store.ErrLinkExists), errors.Is(err, store.ErrLinkCycle),
		errors.Is(err, store.ErrAssetArchived):
		writeJSONError(w, http.StatusConflict, err.Error())
	case store.IsUniqueViolation(err):
		writeJSONError(w, http.StatusConflict, "Another relation type already has this name")
	case errors.Is(err, store.ErrInvalidRelationType), errors.Is(err, store.ErrLinkToSelf):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	default:
		writeStoreError(w, err, action)
	}
}
