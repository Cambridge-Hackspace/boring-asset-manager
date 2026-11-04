package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Favorites (#225, docs/favorites.md): what each person stars to find
// again. A favorite they can no longer see, or that is gone, is kept but not
// listed, so it comes back if they can see it again.

// FavoriteResponse is one favorite, as GET /api/v1/auth/me/favorites lists
// it: enough to link to it and show its name.
type FavoriteResponse struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Name string `json:"name"`
	// Slug is a flow's address, /flows/<slug>.
	Slug string `json:"slug,omitempty"`
	// Archived marks an archived asset.
	Archived  bool      `json:"archived,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// favorite is what a favorite stars, as the request may see it, or nil
// when it may not, or it is gone.
func (s *Server) favorite(r *http.Request, kind, id string) (*FavoriteResponse, error) {
	ctx := r.Context()
	switch kind {
	case store.FavoriteAsset:
		a, err := s.store.GetAssetByID(id)
		if errors.Is(err, store.ErrAssetNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if !Allows(ctx, rbac.PermAssetsRead, a.Scopes) {
			return nil, nil
		}
		return &FavoriteResponse{Kind: kind, ID: id, Name: a.Asset.Name, Archived: a.Asset.ArchivedAt != nil}, nil
	case store.FavoriteCategory:
		if !HasPermission(ctx, rbac.PermAssetsRead) {
			return nil, nil
		}
		c, err := s.store.GetCategoryByID(id)
		if errors.Is(err, store.ErrCategoryNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return &FavoriteResponse{Kind: kind, ID: id, Name: c.Name}, nil
	case store.FavoriteReport:
		if !HasPermission(ctx, rbac.PermAssetsRead) {
			return nil, nil
		}
		rep, err := s.store.GetReport(id)
		if errors.Is(err, store.ErrReportNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return &FavoriteResponse{Kind: kind, ID: id, Name: rep.Name}, nil
	case store.FavoriteFlow:
		f, err := s.store.GetFlowByID(id)
		if errors.Is(err, store.ErrFlowNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		// Seen as GET /api/v1/flows/{slug} sees it.
		if !HasPermission(ctx, rbac.PermFlowsManage) {
			may, err := s.mayRun(r, f)
			if err != nil {
				return nil, err
			}
			if !may || !f.Enabled {
				return nil, nil
			}
		}
		return &FavoriteResponse{Kind: kind, ID: id, Name: f.Name, Slug: f.Slug}, nil
	}
	return nil, nil
}

// handleListFavorites: GET /api/v1/auth/me/favorites, oldest first.
func (s *Server) handleListFavorites(w http.ResponseWriter, r *http.Request) {
	u := CurrentUser(r.Context())
	list, err := s.store.Favorites(u.ID)
	if err != nil {
		writeServerError(w, err, "list favorites")
		return
	}
	out := []FavoriteResponse{}
	for _, f := range list {
		shown, err := s.favorite(r, f.Kind, f.ObjectID)
		if err != nil {
			writeServerError(w, err, "list favorites")
			return
		}
		if shown != nil {
			shown.CreatedAt = f.CreatedAt
			out = append(out, *shown)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"favorites": out})
}

// handleAddFavorite: PUT /api/v1/auth/me/favorites/{kind}/{id}. Only what
// the person can see can be starred; anything else is not found.
func (s *Server) handleAddFavorite(w http.ResponseWriter, r *http.Request) {
	u := CurrentUser(r.Context())
	kind, id := r.PathValue("kind"), r.PathValue("id")
	shown, err := s.favorite(r, kind, id)
	if err != nil {
		writeServerError(w, err, "add a favorite")
		return
	}
	if shown == nil {
		if !validFavoriteKind(kind) {
			writeJSONError(w, http.StatusBadRequest, store.ErrInvalidFavorite.Error())
			return
		}
		writeJSONError(w, http.StatusNotFound, "Nothing to star: it is not there, or you cannot see it")
		return
	}
	f, err := s.store.AddFavorite(u.ID, kind, id)
	if err != nil {
		writeServerError(w, err, "add a favorite")
		return
	}
	shown.CreatedAt = f.CreatedAt
	writeJSON(w, http.StatusOK, map[string]any{"favorite": shown})
}

// handleRemoveFavorite: DELETE /api/v1/auth/me/favorites/{kind}/{id}. It
// unstars even what the person can no longer see.
func (s *Server) handleRemoveFavorite(w http.ResponseWriter, r *http.Request) {
	u := CurrentUser(r.Context())
	err := s.store.RemoveFavorite(u.ID, r.PathValue("kind"), r.PathValue("id"))
	if errors.Is(err, store.ErrInvalidFavorite) {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeServerError(w, err, "remove a favorite")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func validFavoriteKind(kind string) bool {
	for _, k := range store.FavoriteKinds {
		if k == kind {
			return true
		}
	}
	return false
}
