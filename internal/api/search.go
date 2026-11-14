package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/query"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Searches (docs/data-model.md#search) are plain words, or queries
// (docs/data-model.md#queries) such as `field.diameter >= 1.75 AND in ~
// "shelf a"`. Both are matched in Go against the assets as the request may
// see them, after readable has hidden what it cannot, so nothing hidden
// can be searched for or used to find out what an asset holds.

// QueryErrorResponse is a search that is a query with a mistake: the
// message, and where in the search it is, in UTF-16 code units as a
// browser counts characters.
type QueryErrorResponse struct {
	Error      string     `json:"error"`
	QueryError QueryError `json:"query_error"`
}

// QueryError is where a query's mistake is.
type QueryError struct {
	Message string `json:"message"`
	Start   int    `json:"start"`
	End     int    `json:"end"`
}

// writeQueryError answers a search whose query cannot be run.
func writeQueryError(w http.ResponseWriter, err *query.Error) {
	writeJSON(w, http.StatusBadRequest, QueryErrorResponse{
		Error:      "The search can't be run: " + err.Message,
		QueryError: QueryError{Message: err.Message, Start: err.Start, End: err.End},
	})
}

// searchSchema is what a query from this request may name.
func (s *Server) searchSchema(r *http.Request) (query.Schema, error) {
	list, err := s.store.ListFields()
	if err != nil {
		return query.Schema{}, err
	}
	fields := make(map[string]models.Field, len(list))
	for _, f := range list {
		fields[f.Key] = f
	}
	return query.Schema{Fields: fields, SignedIn: CurrentUser(r.Context()) != nil}, nil
}

// compileSearch is the plan for a search that is a query, and nil for
// plain words or none. A query's mistake is a *query.Error.
func (s *Server) compileSearch(r *http.Request, search string) (*query.Plan, error) {
	if !query.IsQuery(search) {
		return nil, nil
	}
	schema, err := s.searchSchema(r)
	if err != nil {
		return nil, err
	}
	return query.Compile(search, schema)
}

// search keeps the assets a search matches, in the order a query's ORDER
// BY asks for. Plain words match as MatchesSearch does; with comments
// set, they also find assets by their comments' text, as the asset list
// does (docs/comments.md). plan is search's compiled plan, nil for plain
// words.
func (s *Server) search(r *http.Request, assets []models.AssetWithDetails, search string, plan *query.Plan, comments bool) ([]models.AssetWithDetails, error) {
	if search == "" {
		return assets, nil
	}
	commented := map[string]map[string]bool{}
	commentedBy := func(term string) (map[string]bool, error) {
		if !comments {
			return nil, nil
		}
		if c, ok := commented[term]; ok {
			return c, nil
		}
		c, err := s.store.CommentedAssets(term)
		commented[term] = c
		return c, err
	}
	if plan == nil {
		c, err := commentedBy(search)
		if err != nil {
			return nil, err
		}
		return searchedOrCommented(assets, search, c), nil
	}
	env, err := s.searchEnv(r)
	if err != nil {
		return nil, err
	}
	for _, t := range plan.Terms {
		if _, err := commentedBy(t); err != nil {
			return nil, err
		}
	}
	env.Plain = func(a *models.AssetWithDetails, term string) bool {
		return store.MatchesSearch(a, term) || commented[term][a.Asset.ID]
	}
	kept := assets[:0]
	for i := range assets {
		if plan.Match(&assets[i], env) {
			kept = append(kept, assets[i])
		}
	}
	plan.Sort(kept, env)
	return kept, nil
}

// searchEnv is what matching a query needs besides the assets.
func (s *Server) searchEnv(r *http.Request) (*query.Env, error) {
	env := &query.Env{Now: time.Now().UTC(), Categories: map[string]models.Category{}}
	if u := CurrentUser(r.Context()); u != nil {
		env.Me = u.ID
		starred, err := s.store.FavoriteIDs(u.ID, store.FavoriteAsset)
		if err != nil {
			return nil, err
		}
		env.Favorites = starred
	}
	cats, err := s.store.ListAllCategories()
	if err != nil {
		return nil, err
	}
	for _, c := range cats {
		env.Categories[c.ID] = c
	}
	return env, nil
}

// searchFailed answers a request whose search went wrong: a query's
// mistake as a bad request, anything else as the server's fault.
func searchFailed(w http.ResponseWriter, err error) {
	var qe *query.Error
	if errors.As(err, &qe) {
		writeQueryError(w, qe)
		return
	}
	writeServerError(w, err, "search the assets")
}
