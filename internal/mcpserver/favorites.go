package mcpserver

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Favorites (#225, docs/favorites.md): what the person acted for stars, to
// find again.

type favorite struct {
	Kind      string    `json:"kind" jsonschema:"asset, category, report or flow"`
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug,omitempty" jsonschema:"a flow's address: run it at /f/<slug>"`
	Archived  bool      `json:"archived,omitempty" jsonschema:"set on an archived asset"`
	CreatedAt time.Time `json:"created_at" jsonschema:"when it was starred"`
}

type favoritesOut struct {
	Favorites []favorite `json:"favorites" jsonschema:"oldest first; only what they can see now"`
}

type favoriteIn struct {
	Kind string `json:"kind" jsonschema:"asset, category, report or flow"`
	ID   string `json:"id" jsonschema:"the asset's, category's, report's or flow's ID"`
}

func favoritePath(in favoriteIn) string {
	return "/api/v1/auth/me/favorites/" + url.PathEscape(in.Kind) + "/" + url.PathEscape(in.ID)
}

func favoriteTools() []tool {
	list := define(&mcp.Tool{
		Name: "list_favorites",
		Description: "What the person you act for has starred, to find again: assets, categories, reports and flows. " +
			"Those they can no longer see are left out, and come back if they can see them again.",
		Annotations: readOnly("List my favorites"),
	}, []string{"GET /api/v1/auth/me/favorites"}, nil, func(ctx context.Context, e *env, _ none) (reply[favoritesOut], error) {
		var res favoritesOut
		if err := e.call(ctx, http.MethodGet, "/api/v1/auth/me/favorites", nil, &res); err != nil {
			return reply[favoritesOut]{}, err
		}
		if res.Favorites == nil {
			res.Favorites = []favorite{}
		}
		return say(res, "%d favorites.", len(res.Favorites))
	})
	list.person = true

	add := define(&mcp.Tool{
		Name:        "add_favorite",
		Description: "Star an asset, category, report or flow for the person you act for, so it shows among their favorites on the home page.",
		Annotations: changes("Add a favorite", true),
	}, []string{"PUT /api/v1/auth/me/favorites/{kind}/{id}"}, nil, func(ctx context.Context, e *env, in favoriteIn) (reply[favorite], error) {
		var res struct {
			Favorite favorite `json:"favorite"`
		}
		if err := e.call(ctx, http.MethodPut, favoritePath(in), nil, &res); err != nil {
			return reply[favorite]{}, err
		}
		return say(res.Favorite, "Starred the %s %q.", res.Favorite.Kind, res.Favorite.Name)
	})
	add.person = true

	remove := define(&mcp.Tool{
		Name:        "remove_favorite",
		Description: "Unstar an asset, category, report or flow for the person you act for. It is only taken from their favorites.",
		Annotations: changes("Remove a favorite", true),
	}, []string{"DELETE /api/v1/auth/me/favorites/{kind}/{id}"}, nil, func(ctx context.Context, e *env, in favoriteIn) (reply[favoriteIn], error) {
		if err := e.call(ctx, http.MethodDelete, favoritePath(in), nil, nil); err != nil {
			return reply[favoriteIn]{}, err
		}
		return say(in, "No longer a favorite.")
	})
	remove.person = true

	return []tool{list, add, remove}
}
