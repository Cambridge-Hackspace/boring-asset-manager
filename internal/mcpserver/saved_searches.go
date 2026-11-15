package mcpserver

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Saved searches (#390, docs/data-model.md#saved-searches): searches the
// person you act for kept under a name, and those shared with everyone, so
// an agent runs the same named searches people do.

type savedSearchProblem struct {
	Message string `json:"message"`
	Start   int    `json:"start" jsonschema:"where in the search the mistake starts, in UTF-16 code units"`
	End     int    `json:"end"`
}

type savedSearch struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Search string `json:"search" jsonschema:"plain words or a query, as search_assets takes it"`
	Shared bool   `json:"shared" jsonschema:"listed for everyone who can read assets"`
	Mine   bool   `json:"mine" jsonschema:"the person's own, which they may change and delete; others are shared by someone else"`
	Owner  struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"owner"`
	Problem   *savedSearchProblem `json:"problem,omitempty" jsonschema:"set when its query no longer runs, such as a field it names that was deleted: what is wrong, and where"`
	CreatedAt time.Time           `json:"created_at"`
	UpdatedAt time.Time           `json:"updated_at"`
}

type savedSearchesOut struct {
	SavedSearches []savedSearch `json:"saved_searches" jsonschema:"the person's own first, then those shared, each by name"`
}

type savedSearchIn struct {
	ID string `json:"id" jsonschema:"the saved search's ID, from list_saved_searches"`
}

type saveSearchIn struct {
	Name   string `json:"name" jsonschema:"what to call it, up to 100 characters; unique among the person's own"`
	Search string `json:"search" jsonschema:"plain words or a query, as search_assets takes it"`
	Shared bool   `json:"shared,omitempty" jsonschema:"list it for everyone who can read assets; needs searches.share"`
}

type changeSavedSearchIn struct {
	ID string `json:"id" jsonschema:"the saved search's ID, one of the person's own"`
	saveSearchIn
}

type runSavedSearchIn struct {
	ID     string `json:"id" jsonschema:"the saved search's ID, from list_saved_searches"`
	Limit  int    `json:"limit,omitempty" jsonschema:"how many to return, 1 to 200 (25 if not given)"`
	Offset int    `json:"offset,omitempty" jsonschema:"how many to skip, to page through"`
}

type runSavedSearchOut struct {
	SavedSearch savedSearch    `json:"saved_search"`
	Total       int            `json:"total" jsonschema:"how many assets match"`
	Assets      []assetSummary `json:"assets"`
	More        int            `json:"more" jsonschema:"how many more match after these; call again with a larger offset"`
}

func savedSearchPath(id string) string { return "/api/v1/searches/" + url.PathEscape(id) }

func savedSearchTools() []tool {
	list := define(&mcp.Tool{
		Name: "list_saved_searches",
		Description: "The searches the person you act for saved under a name, and those shared with everyone who can read assets. " +
			"Run one with run_saved_search. One whose query no longer runs comes with a problem saying what is wrong and where.",
		Annotations: readOnly("List saved searches"),
	}, []string{"GET /api/v1/searches"}, []string{rbac.PermAssetsRead}, func(ctx context.Context, e *env, _ none) (reply[savedSearchesOut], error) {
		var res savedSearchesOut
		if err := e.call(ctx, http.MethodGet, "/api/v1/searches", nil, &res); err != nil {
			return reply[savedSearchesOut]{}, err
		}
		if res.SavedSearches == nil {
			res.SavedSearches = []savedSearch{}
		}
		names := []string{}
		for _, v := range res.SavedSearches {
			names = append(names, fmt.Sprintf("%q", v.Name))
		}
		text := fmt.Sprintf("%d saved searches", len(names))
		if len(names) > 0 {
			text += ": " + strings.Join(names, ", ")
		}
		return say(res, "%s.", text)
	})

	run := define(&mcp.Tool{
		Name: "run_saved_search",
		Description: "Run a saved search, the person's own or a shared one, and get the assets it finds, as search_assets gives them. " +
			"One whose query no longer runs answers with what is wrong and where.",
		Annotations: readOnly("Run a saved search"),
	}, []string{"GET /api/v1/searches/{id}/assets"}, []string{rbac.PermAssetsRead}, func(ctx context.Context, e *env, in runSavedSearchIn) (reply[runSavedSearchOut], error) {
		var res struct {
			SavedSearch savedSearch               `json:"saved_search"`
			Assets      []models.AssetWithDetails `json:"assets"`
		}
		if err := e.call(ctx, http.MethodGet, savedSearchPath(in.ID)+"/assets", nil, &res); err != nil {
			return reply[runSavedSearchOut]{}, err
		}
		shown, more, err := page(res.Assets, in.Offset, in.Limit)
		if err != nil {
			return reply[runSavedSearchOut]{}, err
		}
		out := runSavedSearchOut{SavedSearch: res.SavedSearch, Total: len(res.Assets), More: more, Assets: []assetSummary{}}
		base := e.baseURL(ctx)
		for i := range shown {
			out.Assets = append(out.Assets, summarize(&shown[i], base))
		}
		return say(out, "%q finds %d assets.", res.SavedSearch.Name, len(res.Assets))
	})

	save := define(&mcp.Tool{
		Name:        "save_search",
		Description: "Save a search, plain words or a query as search_assets takes it, under a name for the person you act for, so they find it by the search box and on the home page. A query is checked first; one with a mistake is not saved.",
		Annotations: changes("Save a search", false),
	}, []string{"POST /api/v1/searches"}, []string{rbac.PermAssetsRead}, func(ctx context.Context, e *env, in saveSearchIn) (reply[savedSearch], error) {
		var res struct {
			SavedSearch savedSearch `json:"saved_search"`
		}
		if err := e.call(ctx, http.MethodPost, "/api/v1/searches", in, &res); err != nil {
			return reply[savedSearch]{}, err
		}
		return say(res.SavedSearch, "Saved %q.", res.SavedSearch.Name)
	})
	save.person = true

	change := define(&mcp.Tool{
		Name:        "change_saved_search",
		Description: "Rename one of the person's own saved searches, change its search, or share it or stop sharing it. Give its name, search and shared as they should be.",
		Annotations: changes("Change a saved search", true),
	}, []string{"PUT /api/v1/searches/{id}"}, []string{rbac.PermAssetsRead}, func(ctx context.Context, e *env, in changeSavedSearchIn) (reply[savedSearch], error) {
		var res struct {
			SavedSearch savedSearch `json:"saved_search"`
		}
		if err := e.call(ctx, http.MethodPut, savedSearchPath(in.ID), in.saveSearchIn, &res); err != nil {
			return reply[savedSearch]{}, err
		}
		return say(res.SavedSearch, "Changed %q.", res.SavedSearch.Name)
	})
	change.person = true

	remove := define(&mcp.Tool{
		Name:        "delete_saved_search",
		Description: "Delete one of the person's own saved searches. Shared ones are deleted only by whoever saved them.",
		Annotations: destroys("Delete a saved search", true),
	}, []string{"DELETE /api/v1/searches/{id}"}, []string{rbac.PermAssetsRead}, func(ctx context.Context, e *env, in savedSearchIn) (reply[savedSearchIn], error) {
		if err := e.call(ctx, http.MethodDelete, savedSearchPath(in.ID), nil, nil); err != nil {
			return reply[savedSearchIn]{}, err
		}
		return say(in, "Deleted the saved search.")
	})
	remove.person = true

	return []tool{list, run, save, change, remove}
}
