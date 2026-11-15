package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/query"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Saved searches (#390, docs/data-model.md#saved-searches): a search,
// plain words or a query, kept under a name to run again. Each is its
// person's own; one shared, by searches.share, is listed for everyone who
// can read assets. A saved query is checked again each time it is listed
// or run, so one that no longer runs says where it is wrong.

// SavedSearchResponse is one saved search.
type SavedSearchResponse struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Search string `json:"search"`
	Shared bool   `json:"shared"`
	// Mine is the requester's own, which they may change and delete.
	Mine  bool             `json:"mine"`
	Owner SavedSearchOwner `json:"owner"`
	// Problem is where a query that no longer runs is wrong, such as a
	// field it names that was deleted.
	Problem   *QueryError `json:"problem,omitempty"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
}

// SavedSearchOwner is whose a saved search is.
type SavedSearchOwner struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// SavedSearchRequest saves a search, or changes one.
type SavedSearchRequest struct {
	Name   string `json:"name"`
	Search string `json:"search"`
	Shared bool   `json:"shared"`
}

func requesterID(r *http.Request) string {
	if u := CurrentUser(r.Context()); u != nil {
		return u.ID
	}
	return ""
}

// savedSearchResponse is a saved search as the request sees it, checked
// against the fields there are now.
func (s *Server) savedSearchResponse(r *http.Request, v *store.SavedSearch) (SavedSearchResponse, error) {
	out := SavedSearchResponse{ID: v.ID, Name: v.Name, Search: v.Search, Shared: v.Shared, Mine: v.UserID == requesterID(r),
		Owner: SavedSearchOwner{ID: v.UserID, Name: v.OwnerName}, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
	if _, err := s.compileSearch(r, v.Search); err != nil {
		var qe *query.Error
		if !errors.As(err, &qe) {
			return out, err
		}
		out.Problem = &QueryError{Message: qe.Message, Start: qe.Start, End: qe.End}
	}
	return out, nil
}

// handleListSavedSearches lists the requester's saved searches and those
// shared, theirs first: GET /api/v1/searches. Signed out, it lists those
// shared.
func (s *Server) handleListSavedSearches(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.SavedSearches(requesterID(r))
	if err != nil {
		writeServerError(w, err, "list the saved searches")
		return
	}
	out := make([]SavedSearchResponse, 0, len(list))
	for i := range list {
		v, err := s.savedSearchResponse(r, &list[i])
		if err != nil {
			writeServerError(w, err, "check the saved searches")
			return
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, map[string]any{"saved_searches": out})
}

// savedSearchInput reads a saved search, refusing one shared without
// searches.share, or a query that cannot run; wasShared lets one already
// shared stay so as it is changed.
func (s *Server) savedSearchInput(w http.ResponseWriter, r *http.Request, wasShared bool) (SavedSearchRequest, bool) {
	var in SavedSearchRequest
	if !decodeJSON(w, r, &in) {
		return in, false
	}
	if in.Shared && !wasShared && !HasPermission(r.Context(), rbac.PermSearchesShare) {
		writeJSONError(w, http.StatusForbidden, "Sharing a saved search needs searches.share; save it for yourself instead")
		return in, false
	}
	if _, err := s.compileSearch(r, in.Search); err != nil {
		searchFailed(w, err)
		return in, false
	}
	return in, true
}

func writeSavedSearchError(w http.ResponseWriter, err error, action string) {
	switch {
	case errors.Is(err, store.ErrSavedSearchNotFound):
		writeJSONError(w, http.StatusNotFound, "Saved search not found")
	case errors.Is(err, store.ErrSavedSearchNameTaken):
		writeJSONError(w, http.StatusConflict, "You already have a saved search of that name")
	case errors.Is(err, store.ErrInvalidSavedSearch):
		writeJSONError(w, http.StatusBadRequest, "A saved search needs a name, of up to 100 characters, and a search, of up to 2000")
	default:
		writeServerError(w, err, action)
	}
}

func (s *Server) writeSavedSearch(w http.ResponseWriter, r *http.Request, status int, v *store.SavedSearch) {
	out, err := s.savedSearchResponse(r, v)
	if err != nil {
		writeServerError(w, err, "check the saved search")
		return
	}
	writeJSON(w, status, map[string]any{"saved_search": out})
}

// handleCreateSavedSearch saves a search under a name for the requester:
// POST /api/v1/searches {name, search, shared}.
func (s *Server) handleCreateSavedSearch(w http.ResponseWriter, r *http.Request) {
	in, ok := s.savedSearchInput(w, r, false)
	if !ok {
		return
	}
	v, err := s.store.CreateSavedSearch(requesterID(r), in.Name, in.Search, in.Shared)
	if err != nil {
		writeSavedSearchError(w, err, "save the search")
		return
	}
	s.writeSavedSearch(w, r, http.StatusCreated, v)
}

// handleUpdateSavedSearch renames one of the requester's saved searches,
// changes its search, or shares or stops sharing it: PUT
// /api/v1/searches/{id}. Another's is not found.
func (s *Server) handleUpdateSavedSearch(w http.ResponseWriter, r *http.Request) {
	id, me := r.PathValue("id"), requesterID(r)
	was, err := s.store.GetSavedSearch(id, me)
	if err == nil && was.UserID != me {
		err = store.ErrSavedSearchNotFound
	}
	if err != nil {
		writeSavedSearchError(w, err, "read the saved search")
		return
	}
	in, ok := s.savedSearchInput(w, r, was.Shared)
	if !ok {
		return
	}
	v, err := s.store.UpdateSavedSearch(id, me, in.Name, in.Search, in.Shared)
	if err != nil {
		writeSavedSearchError(w, err, "change the saved search")
		return
	}
	s.writeSavedSearch(w, r, http.StatusOK, v)
}

// handleDeleteSavedSearch deletes one of the requester's saved searches:
// DELETE /api/v1/searches/{id}. Another's is not found.
func (s *Server) handleDeleteSavedSearch(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteSavedSearch(r.PathValue("id"), requesterID(r)); err != nil {
		writeSavedSearchError(w, err, "delete the saved search")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleRunSavedSearch runs a saved search, as GET /api/v1/assets does
// with its search and any other filters given: GET
// /api/v1/searches/{id}/assets. One whose query no longer runs answers as
// a search with a mistake does, saying where.
func (s *Server) handleRunSavedSearch(w http.ResponseWriter, r *http.Request) {
	v, err := s.store.GetSavedSearch(r.PathValue("id"), requesterID(r))
	if err != nil {
		writeSavedSearchError(w, err, "read the saved search")
		return
	}
	q := r.URL.Query()
	q.Set("search", v.Search)
	assets, ok := s.filterAssets(w, r, q)
	if !ok {
		return
	}
	if assets == nil {
		assets = []models.AssetWithDetails{}
	}
	out, err := s.savedSearchResponse(r, v)
	if err != nil {
		writeServerError(w, err, "check the saved search")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"saved_search": out, "assets": assets})
}
