package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/flows"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// FlowSearchView is a Search flow's page (docs/flows.md#search-search), as
// whoever opens it sees it: what it is called and says, what its results
// show, and how long it waits, unused, before it clears.
type FlowSearchView struct {
	Name       string   `json:"name"`
	Title      string   `json:"title,omitempty"`
	Text       string   `json:"text,omitempty"`
	Columns    []string `json:"columns"`
	ResetAfter int      `json:"reset_after"`
	Public     bool     `json:"public"`
}

// defaultResultColumns are what a Search step's results show when it
// names none.
var defaultResultColumns = []string{"where", "categories"}

// handleFlowSearch searches as a Search flow does (#397): GET
// /api/v1/flows/{slug}/search?search=. It answers with the flow's page,
// and, for a search, the active assets in the flow's categories or below
// that its filter and the search both match, as the asset list shows them,
// with why each matched and related terms to try.
//
// A public flow is open to everyone, signed in or not; signed out, it
// searches as the public role, and so finds only what a signed-out
// visitor may already read. Any other flow is for those who may run it.
// Signed in, it searches as the person, with their permissions.
func (s *Server) handleFlowSearch(w http.ResponseWriter, r *http.Request) {
	f, err := s.store.GetFlow(r.PathValue("slug"))
	if errors.Is(err, store.ErrFlowNotFound) {
		writeJSONError(w, http.StatusNotFound, "Flow not found")
		return
	}
	if err != nil {
		writeServerError(w, err, "read the flow")
		return
	}
	step := f.Definition.SearchOnly()
	if step == nil || !f.Enabled {
		writeJSONError(w, http.StatusNotFound, "Flow not found")
		return
	}
	public := f.Definition.Public
	if !public {
		if !authenticated(r.Context()) {
			writeJSONError(w, http.StatusUnauthorized, "Authentication required")
			return
		}
		may, err := s.mayRun(r, f)
		if err != nil {
			writeServerError(w, err, "read the flow")
			return
		}
		if !may && !HasPermission(r.Context(), rbac.PermFlowsManage) {
			writeJSONError(w, http.StatusNotFound, "Flow not found")
			return
		}
	}
	view := FlowSearchView{Name: f.Name, Title: step.Title, Text: step.Text, Columns: step.Columns, ResetAfter: step.ResetAfter, Public: public}
	if len(view.Columns) == 0 {
		view.Columns = defaultResultColumns
	}
	out := map[string]any{"flow": view, "assets": []models.AssetWithDetails{}}
	search := strings.TrimSpace(r.URL.Query().Get("search"))
	if search == "" {
		// Nothing is listed until something is searched for, so the page
		// never shows the whole of its categories at once.
		writeJSON(w, http.StatusOK, out)
		return
	}
	found, notes, err := s.flowSearch(r, f, step, search)
	if err != nil {
		var filterErr flowFilterError
		if errors.As(err, &filterErr) {
			writeJSONError(w, http.StatusConflict, "This page's filter no longer runs: "+filterErr.err.Error()+". Whoever manages flows can fix it.")
			return
		}
		searchFailed(w, err)
		return
	}
	out["assets"] = found
	if len(notes.Matched) > 0 {
		out["matched"] = notes.Matched
	}
	if len(notes.Suggestions) > 0 {
		out["suggestions"] = notes.Suggestions
	}
	writeJSON(w, http.StatusOK, out)
}

// flowFilterError is a Search step's own filter failing, as a field it
// names was deleted since the flow was saved, rather than what was typed.
type flowFilterError struct{ err error }

func (e flowFilterError) Error() string { return e.err.Error() }

// flowSearch finds what a Search flow's search finds: the active assets in
// the flow's categories or below, that the request may read (and, for a
// flow that isn't public, run flows on), as the request may see them,
// matching the step's filter and then the search.
func (s *Server) flowSearch(r *http.Request, f *models.Flow, step *flows.Step, search string) ([]models.AssetWithDetails, searchNotes, error) {
	var notes searchNotes
	fixed, err := s.compileSearch(r, step.Filter)
	if err != nil {
		return nil, notes, flowFilterError{err}
	}
	typed, err := s.compileSearch(r, search)
	if err != nil {
		return nil, notes, err
	}
	all, err := s.store.FilterAssets(store.AssetFilter{})
	if err != nil {
		return nil, notes, err
	}
	fc, err := s.flowCategories()
	if err != nil {
		return nil, notes, err
	}
	in := []models.AssetWithDetails{}
	for _, a := range all {
		if !f.Definition.Public && !Allows(r.Context(), rbac.PermFlowsRun, a.Scopes) {
			continue
		}
		for _, c := range a.Categories {
			if fc.within(c.ID, f.Definition.CategoryIDs) {
				in = append(in, a)
				break
			}
		}
	}
	// As the request may see them, before any search is matched, so that
	// nothing hidden from it, a place or a person, can find an asset.
	if in, err = s.readable(r, in); err != nil {
		return nil, notes, err
	}
	if in, err = s.searchNoted(r, in, step.Filter, fixed, false, nil); err != nil {
		return nil, notes, flowFilterError{err}
	}
	// Signed in, a search also finds assets by their comments, as the
	// asset list does; signed out, by the assets alone.
	comments := authenticated(r.Context())
	if in, err = s.searchNoted(r, in, search, typed, comments, &notes); err != nil {
		return nil, notes, err
	}
	if in == nil {
		in = []models.AssetWithDetails{}
	}
	return in, notes, nil
}

// publicReadsNone reports whether the public role may read assets in none
// of a flow's categories, so that the flow, opened signed out, would never
// find anything.
func (s *Server) publicReadsNone(d *flows.Definition) (bool, error) {
	public, err := s.store.PublicPermissions()
	if err != nil {
		return false, err
	}
	scopesOf, err := s.store.ScopeResolver()
	if err != nil {
		return false, err
	}
	for _, id := range d.CategoryIDs {
		if rbac.Allows(public, rbac.PermAssetsRead, scopesOf([]string{id})) {
			return false, nil
		}
	}
	return true, nil
}
