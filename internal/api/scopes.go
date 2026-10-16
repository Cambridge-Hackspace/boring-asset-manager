package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Scoped permissions (README §3.3). A route needing an asset permission
// lets in whoever holds it in any scope; the handler then checks the asset
// itself with assetFor. An asset the requester cannot read is not found,
// so its existence does not show; one they can read but not act on is
// forbidden.

// assetFor reads the asset a request is about and checks the requester
// holds one of perms in its scopes, or, when none are given, may read it.
// One they can neither read nor act on is not found; one they can read but
// not act on is forbidden. When it fails, it answers the request.
func (s *Server) assetFor(w http.ResponseWriter, r *http.Request, id string, perms ...string) (*models.AssetWithDetails, bool) {
	a, err := s.store.GetAssetByID(id)
	if err != nil && !errors.Is(err, store.ErrAssetNotFound) {
		writeServerError(w, err, "query asset")
		return nil, false
	}
	allowed := err == nil && allowsAny(r, a.Scopes, perms)
	if err != nil || (!allowed && !Allows(r.Context(), rbac.PermAssetsRead, a.Scopes)) {
		writeJSONError(w, http.StatusNotFound, "Asset not found")
		return nil, false
	}
	if !allowed {
		writeJSONError(w, http.StatusForbidden, "Forbidden: insufficient permissions for this asset's scopes")
		return nil, false
	}
	return a, true
}

// allowsAny reports whether the request holds one of perms, or read when
// there are none, in these scopes.
func allowsAny(r *http.Request, scopes, perms []string) bool {
	if len(perms) == 0 {
		perms = []string{rbac.PermAssetsRead}
	}
	for _, p := range perms {
		if Allows(r.Context(), p, scopes) {
			return true
		}
	}
	return false
}

// mayPlace reports whether the request may put an asset in these
// categories with perm: it must hold it in the scopes they give. When it
// may not, or the scopes cannot be read, it answers the request.
func (s *Server) mayPlace(w http.ResponseWriter, r *http.Request, perm string, categoryIDs []string) bool {
	scopes, err := s.store.ScopesForCategories(categoryIDs)
	if err != nil {
		writeServerError(w, err, "read the categories' scopes")
		return false
	}
	if !Allows(r.Context(), perm, scopes) {
		writeJSONError(w, http.StatusForbidden, "Forbidden: you cannot put assets in "+strings.Join(scopes, ", "))
		return false
	}
	return true
}

// readable keeps the assets the request may read, each showing only the
// links to assets it may read too.
func (s *Server) readable(r *http.Request, assets []models.AssetWithDetails) ([]models.AssetWithDetails, error) {
	kept := assets[:0:0]
	for _, a := range assets {
		if Allows(r.Context(), rbac.PermAssetsRead, a.Scopes) {
			kept = append(kept, a)
		}
	}
	return kept, s.hideLinks(r, kept)
}

// shown is an asset as the request may see it: without links to assets it
// cannot read. A failure to read their scopes hides every link.
func (s *Server) shown(r *http.Request, a *models.AssetWithDetails) *models.AssetWithDetails {
	if a == nil {
		return nil
	}
	one := []models.AssetWithDetails{*a}
	if err := s.hideLinks(r, one); err != nil {
		one[0].Links, one[0].Location = []models.AssetLink{}, []models.LinkedAsset{}
	}
	return &one[0]
}

// hideLinks takes from each asset its links to, and the places in its
// location of, assets the request cannot read.
func (s *Server) hideLinks(r *http.Request, assets []models.AssetWithDetails) error {
	if UserPermissions(r.Context())[rbac.PermAssetsRead] {
		return nil // reads everything
	}
	var ids []string
	for _, a := range assets {
		for _, l := range a.Links {
			ids = append(ids, l.Asset.ID)
		}
		for _, l := range a.Location {
			ids = append(ids, l.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	scopes, err := s.store.AssetScopes(ids)
	if err != nil {
		return err
	}
	can := func(id string) bool { return Allows(r.Context(), rbac.PermAssetsRead, scopes[id]) }
	for i := range assets {
		a := &assets[i]
		links := []models.AssetLink{}
		for _, l := range a.Links {
			if can(l.Asset.ID) {
				links = append(links, l)
			}
		}
		location := []models.LinkedAsset{}
		for _, l := range a.Location {
			if can(l.ID) {
				location = append(location, l)
			}
		}
		a.Links, a.Location = links, location
	}
	return nil
}

// ScopeRequest names a scope.
type ScopeRequest struct {
	Name string `json:"name"`
}

func (s *Server) handleListScopes(w http.ResponseWriter, r *http.Request) {
	scopes, err := s.store.ListScopes()
	if err != nil {
		writeServerError(w, err, "list scopes")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"scopes": scopes})
}

func (s *Server) handleSaveScope(w http.ResponseWriter, r *http.Request) {
	var req ScopeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	var (
		sc  *models.Scope
		err error
	)
	status := http.StatusOK
	if id := r.PathValue("id"); id != "" {
		sc, err = s.store.RenameScope(id, req.Name)
	} else {
		sc, err = s.store.CreateScope(req.Name)
		status = http.StatusCreated
	}
	if err != nil {
		writeScopeError(w, err, "save the scope")
		return
	}
	writeJSON(w, status, map[string]any{"scope": sc})
}

func (s *Server) handleDeleteScope(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteScope(r.PathValue("id")); err != nil {
		writeScopeError(w, err, "delete the scope")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Scope deleted"})
}

// CategoryScopesRequest replaces the scopes on a category.
type CategoryScopesRequest struct {
	ScopeIDs []string `json:"scope_ids"`
}

func (s *Server) handleCategoryScopes(w http.ResponseWriter, r *http.Request) {
	ids, err := s.store.CategoryScopes(r.PathValue("id"))
	if err != nil {
		writeScopeError(w, err, "read the category's scopes")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"scope_ids": ids})
}

func (s *Server) handleSetCategoryScopes(w http.ResponseWriter, r *http.Request) {
	var req CategoryScopesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	ids, err := s.store.SetCategoryScopes(r.PathValue("id"), req.ScopeIDs)
	if err != nil {
		writeScopeError(w, err, "set the category's scopes")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"scope_ids": ids})
}

// writeScopeError answers a failure to read or change scopes.
func writeScopeError(w http.ResponseWriter, err error, action string) {
	switch {
	case errors.Is(err, store.ErrScopeNotFound), errors.Is(err, store.ErrCategoryNotFound):
		writeJSONError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, store.ErrInvalidScope):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, store.ErrScopeReserved), errors.Is(err, store.ErrScopeInUse):
		writeJSONError(w, http.StatusConflict, err.Error())
	case store.IsUniqueViolation(err):
		writeJSONError(w, http.StatusConflict, "Another scope has that name")
	default:
		writeServerError(w, err, action)
	}
}
