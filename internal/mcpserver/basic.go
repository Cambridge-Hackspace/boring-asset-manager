package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/generators"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type none struct{}

type whoamiOut struct {
	Kind        string   `json:"kind" jsonschema:"what the caller is: token, an API token holding roles"`
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Permissions []string `json:"permissions" jsonschema:"the permissions held: each key alone holds it in every scope, scope:key in that scope only"`
}

type serverInfoOut struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Commit   string `json:"commit,omitempty"`
	BaseURL  string `json:"base_url" jsonschema:"the address people reach BAM! at; pages and scan links start with it"`
	Database string `json:"database" jsonschema:"the database in use: sqlite, postgres or mariadb"`
	Healthy  bool   `json:"healthy" jsonschema:"whether the server can reach its database"`
	Ready    bool   `json:"ready"`
}

type generatorsOut struct {
	Generators []generators.Info `json:"generators" jsonschema:"the kinds of code an asset can have, with what each may hold"`
}

func basicTools() []tool {
	return []tool{
		define(&mcp.Tool{
			Name:        "whoami",
			Description: "Who you are acting as in BAM!, and the permissions you hold. A permission held as scope:key applies only to assets in that scope.",
			Annotations: readOnly("Who am I"),
		}, []string{"GET /api/v1/auth/me"}, nil, func(ctx context.Context, e *env, _ none) (reply[whoamiOut], error) {
			var perms []string
			for p := range e.caller.Permissions {
				perms = append(perms, p)
			}
			slices.Sort(perms)
			out := whoamiOut{Kind: e.caller.Kind, ID: e.caller.ID, Name: e.caller.Name, Permissions: perms}
			return say(out, "Acting as the %s %q, holding %s.", e.caller.Kind, e.caller.Name, describePermissions(perms))
		}),
		define(&mcp.Tool{
			Name:        "server_info",
			Description: "The BAM! server's version, the address people reach it at, its database, and whether it is healthy.",
			Annotations: readOnly("Server information"),
		}, []string{"GET /api/v1/info", "GET /healthz", "GET /readyz"}, nil, func(ctx context.Context, e *env, _ none) (reply[serverInfoOut], error) {
			var info struct {
				Name    string `json:"name"`
				Version string `json:"version"`
				Commit  string `json:"commit"`
				BaseURL string `json:"base_url"`
				Dialect string `json:"dialect"`
			}
			if err := e.call(ctx, http.MethodGet, "/api/v1/info", nil, &info); err != nil {
				return reply[serverInfoOut]{}, err
			}
			// Unhealthy is an answer here, not a failure.
			health := e.api(ctx, http.MethodGet, "/healthz", nil, "")
			var ready struct {
				Ready bool `json:"ready"`
			}
			_ = json.Unmarshal(e.api(ctx, http.MethodGet, "/readyz", nil, "").Body, &ready)
			out := serverInfoOut{Name: info.Name, Version: info.Version, Commit: info.Commit, BaseURL: info.BaseURL,
				Database: info.Dialect, Healthy: health.Status == http.StatusOK, Ready: ready.Ready}
			state := "healthy"
			if !out.Healthy {
				state = "unable to reach its database"
			}
			return say(out, "%s %s at %s, on %s, %s.", out.Name, out.Version, out.BaseURL, out.Database, state)
		}),
		define(&mcp.Tool{
			Name:        "list_generators",
			Description: "The kinds of code an asset can have (QR Code, Micro QR Code, Code 128), with what each may hold. Codes are given by these types.",
			Annotations: readOnly("Code types"),
		}, []string{"GET /api/v1/generators"}, []string{rbac.PermAssetsRead}, func(ctx context.Context, e *env, _ none) (reply[generatorsOut], error) {
			var out generatorsOut
			if err := e.call(ctx, http.MethodGet, "/api/v1/generators", nil, &out); err != nil {
				return reply[generatorsOut]{}, err
			}
			var names []string
			for _, g := range out.Generators {
				names = append(names, g.Name+" ("+g.Type+")")
			}
			return say(out, "Code types: %s.", strings.Join(names, ", "))
		}),
	}
}

func describePermissions(perms []string) string {
	if len(perms) == 0 {
		return "no permissions"
	}
	return strings.Join(perms, ", ")
}
