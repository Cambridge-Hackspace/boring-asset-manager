package api

import (
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/query"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/terms"
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

// searchNotes are what a search says beside the assets it found: why each
// one matched other than as typed ("TPU, narrower than flexible
// filament"), by its ID, and related terms to search for instead
// (docs/data-model.md#related-terms).
type searchNotes struct {
	Matched     map[string]string
	Suggestions []string
}

// search keeps the assets a search matches, in the order a query's ORDER
// BY asks for. Plain words match as MatchesSearch does; with comments
// set, they also find assets by their comments' text, as the asset list
// does (docs/comments.md). plan is search's compiled plan, nil for plain
// words.
func (s *Server) search(r *http.Request, assets []models.AssetWithDetails, search string, plan *query.Plan, comments bool) ([]models.AssetWithDetails, error) {
	return s.searchNoted(r, assets, search, plan, comments, nil)
}

// searchNoted searches as search does, and also through the related
// terms and despite typing mistakes (docs/data-model.md#related-terms):
// plain words find, after the assets that match them as typed, those
// that match a term they expand to, and only when neither finds anything,
// those that match despite a mistake. A query does so where it says ~~.
// notes, when not nil, is told why each matched other than as typed.
func (s *Server) searchNoted(r *http.Request, assets []models.AssetWithDetails, search string, plan *query.Plan, comments bool, notes *searchNotes) ([]models.AssetWithDetails, error) {
	if search == "" {
		return assets, nil
	}
	graph, err := s.store.TermGraph()
	if err != nil {
		return nil, err
	}
	why := map[string]string{}
	defer func() {
		if notes != nil {
			notes.Matched = why
		}
	}()
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
		if notes != nil {
			notes.Suggestions = graph.Related(search)
		}
		return plainSearch(assets, search, c, graph, why), nil
	}
	env, err := s.searchEnv(r)
	if err != nil {
		return nil, err
	}
	env.Terms, env.Why = graph, why
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
	// Without an order of its own, what matched as typed comes first.
	if !plan.Ordered() {
		sort.SliceStable(kept, func(i, j int) bool { return why[kept[i].Asset.ID] == "" && why[kept[j].Asset.ID] != "" })
	}
	found := make(map[string]bool, len(kept))
	for _, a := range kept {
		found[a.Asset.ID] = true
	}
	for id := range why {
		if !found[id] {
			delete(why, id)
		}
	}
	return kept, nil
}

// plainSearch keeps the assets plain words find: those that match them as
// typed, or by their comments (commented), then those that match a term
// they expand to, then, only when neither finds any, those that match
// despite a typing mistake. why is told why each of the last two matched.
func plainSearch(assets []models.AssetWithDetails, search string, commented map[string]bool, graph *terms.Graph, why map[string]string) []models.AssetWithDetails {
	expansions := graph.Expand(search)
	var exact, expanded []models.AssetWithDetails
	for i := range assets {
		a := &assets[i]
		if store.MatchesSearch(a, search) || commented[a.Asset.ID] {
			exact = append(exact, *a)
			continue
		}
		for _, x := range expansions {
			if store.MatchesSearch(a, x.Label) || inCategoryCalled(a, x.Label) {
				expanded = append(expanded, *a)
				why[a.Asset.ID] = x.Why
				break
			}
		}
	}
	if len(exact)+len(expanded) > 0 {
		return append(exact, expanded...)
	}
	// Empty, not nil: finding nothing is "assets": [], never null.
	near := []models.AssetWithDetails{}
	for i := range assets {
		a := &assets[i]
		if w, ok := terms.Near(search, searchTexts(a)); ok {
			near = append(near, *a)
			why[a.Asset.ID] = terms.NearWhy(w, search)
		}
	}
	return near
}

// inCategoryCalled reports whether one of an asset's categories has label
// in its name.
func inCategoryCalled(a *models.AssetWithDetails, label string) bool {
	want := terms.Fold(label)
	for _, c := range a.Categories {
		if strings.Contains(terms.Fold(c.Name), want) {
			return true
		}
	}
	return false
}

// searchTexts are the words a typing mistake is forgiven against: an
// asset's name, where it is, its field values and its categories' names.
// Codes are left out: a code is matched as typed.
func searchTexts(a *models.AssetWithDetails) []string {
	out := []string{a.Asset.Name}
	for _, p := range a.Location {
		out = append(out, p.Name)
	}
	out = append(out, store.FieldTexts(a)...)
	for _, c := range a.Categories {
		out = append(out, c.Name)
	}
	return out
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
