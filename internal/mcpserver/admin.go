package mcpserver

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type userIn struct {
	ID string `json:"id" jsonschema:"the user, from list_users"`
}

type userRolesIn struct {
	ID      string   `json:"id" jsonschema:"the user, from list_users"`
	RoleIDs []string `json:"role_ids,omitempty" jsonschema:"roles held in every scope their permissions name"`
	Roles   []struct {
		RoleID   string   `json:"role_id"`
		ScopeIDs []string `json:"scope_ids,omitempty" jsonschema:"scopes from list_scopes to narrow the role to; none for every scope"`
	} `json:"roles,omitempty" jsonschema:"roles each narrowed to some scopes; the same role twice is held over both's scopes"`
}

type roleIn struct {
	ID          string   `json:"id,omitempty" jsonschema:"the role to change; empty to make one"`
	Name        *string  `json:"name,omitempty"`
	Description *string  `json:"description,omitempty"`
	Permissions []string `json:"permissions,omitempty" jsonschema:"the whole list, replacing what it holds: keys such as assets.read, or scope:key for one scope"`
}

type channelIn struct {
	ID      string            `json:"id,omitempty" jsonschema:"the channel to change; empty to make one"`
	Name    *string           `json:"name,omitempty"`
	Driver  string            `json:"driver,omitempty" jsonschema:"for a new channel: discord, mqtt, …, from list_channel_drivers"`
	Options map[string]string `json:"options,omitempty" jsonschema:"the driver's options to set; secrets, such as a webhook URL, are kept when left out"`
	Enabled *bool             `json:"enabled,omitempty"`
	Events  []string          `json:"events,omitempty" jsonschema:"the event kinds it announces, the whole list"`
}

func adminTools() []tool {
	users := []string{rbac.PermUsersManage}
	settings := []string{rbac.PermSettingsManage}
	return []tool{
		// People.
		simple(&mcp.Tool{Name: "list_users", Description: "Every user who is not archived: name, email, whether active, and their roles, with the scopes any is narrowed to.",
			Annotations: readOnly("List users")}, "GET /api/v1/users", users,
			func(none) (request, error) { return request{path: "/api/v1/users"}, nil },
			func(_ none, out map[string]any) string { return fmt.Sprintf("%d users.", count(out, "users")) }),
		simple(&mcp.Tool{Name: "create_user", Description: "Make an account: email, name and a first password (8 to 72 bytes), active unless said.",
			Annotations: changes("Create a user", false)}, "POST /api/v1/users", users,
			func(in struct {
				Email    string `json:"email"`
				Name     string `json:"name"`
				Password string `json:"password"`
				Active   *bool  `json:"is_active,omitempty"`
			}) (request, error) {
				return request{path: "/api/v1/users", body: map[string]any{"email": in.Email, "name": in.Name, "password": in.Password, "is_active": in.Active}}, nil
			},
			func(_ struct {
				Email    string `json:"email"`
				Name     string `json:"name"`
				Password string `json:"password"`
				Active   *bool  `json:"is_active,omitempty"`
			}, out map[string]any) string {
				return fmt.Sprintf("Made the account for %q.", nameOf(out, "user"))
			}),
		simple(&mcp.Tool{Name: "set_user_active", Description: "Turn an account on or off. Off signs them out and revokes their assistants. Refused if no one active could manage users after.",
			Annotations: destroys("Turn an account on or off", true)}, "PATCH /api/v1/users/{id}/status", users,
			func(in struct {
				ID     string `json:"id"`
				Active bool   `json:"is_active"`
			}) (request, error) {
				return request{path: pathOf("/api/v1/users/{id}/status", in.ID), body: map[string]bool{"is_active": in.Active}}, need(in.ID, "id")
			},
			func(in struct {
				ID     string `json:"id"`
				Active bool   `json:"is_active"`
			}, _ map[string]any) string {
				if in.Active {
					return "Turned the account on."
				}
				return "Turned the account off."
			}),
		simple(&mcp.Tool{Name: "archive_user", Description: "Archive someone who has left: they cannot sign in, their loans stay theirs. Refused if no one could manage users after.",
			Annotations: destroys("Archive a user", true)}, "DELETE /api/v1/users/{id}", users,
			func(in userIn) (request, error) {
				return request{path: pathOf("/api/v1/users/{id}", in.ID)}, need(in.ID, "id")
			},
			func(userIn, map[string]any) string { return "Archived the user." }),
		simple(&mcp.Tool{Name: "set_user_name", Description: "Change someone's name, as others see them.",
			Annotations: changes("Rename a user", true)}, "PUT /api/v1/users/{id}/name", users,
			func(in struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}) (request, error) {
				return request{path: pathOf("/api/v1/users/{id}/name", in.ID), body: map[string]string{"name": in.Name}}, need(in.ID, "id")
			},
			func(_ struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}, out map[string]any) string {
				return fmt.Sprintf("Now named %q.", nameOf(out, "user"))
			}),
		simple(&mcp.Tool{Name: "set_user_password", Description: "Set someone else's password, for one who has forgotten theirs; it signs them out everywhere. Never your own person's: that is theirs to change, signed in.",
			Annotations: destroys("Set a password", true)}, "PUT /api/v1/users/{id}/password", users,
			func(in struct {
				ID       string `json:"id"`
				Password string `json:"password" jsonschema:"8 to 72 bytes; tell it to them some safe way"`
			}) (request, error) {
				return request{path: pathOf("/api/v1/users/{id}/password", in.ID), body: map[string]string{"password": in.Password}}, need(in.ID, "id")
			},
			func(struct {
				ID       string `json:"id"`
				Password string `json:"password" jsonschema:"8 to 72 bytes; tell it to them some safe way"`
			}, map[string]any) string {
				return "Set; they are signed out everywhere."
			}),
		simple(&mcp.Tool{Name: "set_user_roles", Description: "The roles someone holds, the whole list, each in every scope or narrowed to some: narrowed, a role grants its asset permissions " +
			"in those scopes only, and none of its others (users.manage, labels.print, …). Refused if no one active could manage users after.",
			Annotations: destroys("Set roles", true)}, "PUT /api/v1/users/{id}/roles", users,
			func(in userRolesIn) (request, error) {
				roles := make([]map[string]any, 0, len(in.RoleIDs)+len(in.Roles))
				for _, id := range in.RoleIDs {
					roles = append(roles, map[string]any{"role_id": id, "scope_ids": []string{}})
				}
				for _, a := range in.Roles {
					scopes := a.ScopeIDs
					if scopes == nil {
						scopes = []string{}
					}
					roles = append(roles, map[string]any{"role_id": a.RoleID, "scope_ids": scopes})
				}
				return request{path: pathOf("/api/v1/users/{id}/roles", in.ID), body: map[string]any{"roles": roles}}, need(in.ID, "id")
			},
			func(_ userRolesIn, out map[string]any) string {
				return fmt.Sprintf("They hold %d roles.", count(out, "roles"))
			}),

		// Roles and scopes.
		simple(&mcp.Tool{Name: "list_roles", Description: "The roles, with their permissions, how many people hold each, and which are built in.",
			Annotations: readOnly("List roles")}, "GET /api/v1/roles", users,
			func(none) (request, error) { return request{path: "/api/v1/roles"}, nil },
			func(_ none, out map[string]any) string { return fmt.Sprintf("%d roles.", count(out, "roles")) }),
		define(&mcp.Tool{Name: "save_role", Description: "Make a role, or change one: its name, description, or permissions (the whole list). The admin role cannot change.",
			Annotations: destroys("Save a role", true)}, []string{"POST /api/v1/roles", "PUT /api/v1/roles/{id}"}, users,
			func(ctx context.Context, e *env, in roleIn) (reply[map[string]any], error) {
				out := map[string]any{}
				c := given(map[string]any{"name": in.Name, "description": in.Description})
				if in.Permissions != nil {
					c["permissions"] = in.Permissions
				}
				if in.ID == "" {
					if err := e.call(ctx, http.MethodPost, "/api/v1/roles", c, &out); err != nil {
						return reply[map[string]any]{}, err
					}
					return say(out, "Made the role %q.", nameOf(out, "role"))
				}
				var list struct {
					Roles []map[string]any `json:"roles"`
				}
				if err := e.call(ctx, http.MethodGet, "/api/v1/roles", nil, &list); err != nil {
					return reply[map[string]any]{}, err
				}
				var current map[string]any
				for _, r := range list.Roles {
					if r["id"] == in.ID {
						current = r
					}
				}
				if current == nil {
					return reply[map[string]any]{}, fmt.Errorf("no role has the ID %q", in.ID)
				}
				body, _ := merged(map[string]any{"name": current["name"], "description": current["description"], "permissions": current["permissions"]}, c)
				if err := e.call(ctx, http.MethodPut, pathOf("/api/v1/roles/{id}", in.ID), body, &out); err != nil {
					return reply[map[string]any]{}, err
				}
				return say(out, "Saved the role %q.", nameOf(out, "role"))
			}),
		simple(&mcp.Tool{Name: "delete_role", Description: "Delete a role, taking it from everyone who holds it.", Annotations: destroys("Delete a role", true)}, "DELETE /api/v1/roles/{id}", users,
			func(in idIn) (request, error) {
				return request{path: pathOf("/api/v1/roles/{id}", in.ID)}, need(in.ID, "id")
			},
			func(idIn, map[string]any) string { return "Deleted the role." }),
		simple(&mcp.Tool{Name: "list_scopes", Description: "The scopes: labels on categories that roles hold permissions in, with how many categories and roles use each, and how many roles of users and tokens are narrowed to each.",
			Annotations: readOnly("List scopes")}, "GET /api/v1/scopes", []string{rbac.PermUsersManage, rbac.PermCategoriesManage},
			func(none) (request, error) { return request{path: "/api/v1/scopes"}, nil },
			func(_ none, out map[string]any) string { return fmt.Sprintf("%d scopes.", count(out, "scopes")) }),
		define(&mcp.Tool{Name: "save_scope", Description: "Make a scope, or rename one (its permissions in roles, and roles narrowed to it, follow): 1 to 64 lowercase letters, digits, - and _.",
			Annotations: changes("Save a scope", true)}, []string{"POST /api/v1/scopes", "PUT /api/v1/scopes/{id}"}, users,
			func(ctx context.Context, e *env, in struct {
				ID   string `json:"id,omitempty"`
				Name string `json:"name"`
			}) (reply[map[string]any], error) {
				method, path := http.MethodPost, "/api/v1/scopes"
				if in.ID != "" {
					method, path = http.MethodPut, pathOf("/api/v1/scopes/{id}", in.ID)
				}
				out := map[string]any{}
				if err := e.call(ctx, method, path, map[string]string{"name": in.Name}, &out); err != nil {
					return reply[map[string]any]{}, err
				}
				return say(out, "Saved the scope %q.", nameOf(out, "scope"))
			}),
		simple(&mcp.Tool{Name: "delete_scope", Description: "Delete a scope no category, role, user or token uses.", Annotations: destroys("Delete a scope", true)}, "DELETE /api/v1/scopes/{id}", users,
			func(in idIn) (request, error) {
				return request{path: pathOf("/api/v1/scopes/{id}", in.ID)}, need(in.ID, "id")
			},
			func(idIn, map[string]any) string { return "Deleted the scope." }),
		simple(&mcp.Tool{Name: "set_category_scopes", Description: "A category's own scopes, the whole list, passed down to its subcategories: who may reach its assets. public lets anyone see them.",
			Annotations: destroys("Set a category's scopes", true)}, "PUT /api/v1/categories/{id}/scopes", users,
			func(in struct {
				CategoryID string   `json:"category_id"`
				ScopeIDs   []string `json:"scope_ids"`
			}) (request, error) {
				ids := in.ScopeIDs
				if ids == nil {
					ids = []string{}
				}
				return request{path: pathOf("/api/v1/categories/{id}/scopes", in.CategoryID), body: map[string]any{"scope_ids": ids}}, need(in.CategoryID, "category_id")
			},
			func(in struct {
				CategoryID string   `json:"category_id"`
				ScopeIDs   []string `json:"scope_ids"`
			}, _ map[string]any) string {
				return fmt.Sprintf("It is in %d scopes.", len(in.ScopeIDs))
			}),
		simple(&mcp.Tool{Name: "list_live_connections", Description: "The devices and people connected for live events now, on every server, by token or user.",
			Annotations: readOnly("Live connections")}, "GET /api/v1/tokens/connections", users,
			func(none) (request, error) { return request{path: "/api/v1/tokens/connections"}, nil },
			func(_ none, out map[string]any) string {
				return fmt.Sprintf("%d connections.", count(out, "connections"))
			}),

		// Notification channels.
		simple(&mcp.Tool{Name: "list_channel_drivers", Description: "The kinds of notification channel, with their options (which are secret), and the event kinds channels can announce.",
			Annotations: readOnly("Channel drivers")}, "GET /api/v1/notifiers/drivers", settings,
			func(none) (request, error) { return request{path: "/api/v1/notifiers/drivers"}, nil },
			func(_ none, out map[string]any) string { return fmt.Sprintf("%d drivers.", count(out, "drivers")) }),
		simple(&mcp.Tool{Name: "list_channels", Description: "The notification channels. Secrets, such as webhook URLs, show only as set or not.",
			Annotations: readOnly("List channels")}, "GET /api/v1/notifiers", settings,
			func(none) (request, error) { return request{path: "/api/v1/notifiers"}, nil },
			func(_ none, out map[string]any) string { return fmt.Sprintf("%d channels.", count(out, "channels")) }),
		simple(&mcp.Tool{Name: "get_channel", Description: "One notification channel; secrets show only as set or not.", Annotations: readOnly("Get a channel")}, "GET /api/v1/notifiers/{id}", settings,
			func(in idIn) (request, error) {
				return request{path: pathOf("/api/v1/notifiers/{id}", in.ID)}, need(in.ID, "id")
			},
			func(_ idIn, out map[string]any) string { return fmt.Sprintf("%q.", nameOf(out, "channel")) }),
		define(&mcp.Tool{Name: "save_channel", Description: "Make a notification channel, or change one: only what is given changes, and secrets left out are kept.",
			Annotations: changes("Save a channel", true)}, []string{"POST /api/v1/notifiers", "PUT /api/v1/notifiers/{id}"}, settings,
			func(ctx context.Context, e *env, in channelIn) (reply[map[string]any], error) {
				out := map[string]any{}
				if in.ID == "" {
					body := map[string]any{"name": in.Name, "driver": in.Driver, "options": in.Options, "enabled": in.Enabled, "events": in.Events}
					if err := e.call(ctx, http.MethodPost, "/api/v1/notifiers", body, &out); err != nil {
						return reply[map[string]any]{}, err
					}
					return say(out, "Made the channel %q.", nameOf(out, "channel"))
				}
				var got struct {
					Channel struct {
						Name    string            `json:"name"`
						Driver  string            `json:"driver"`
						Options map[string]string `json:"options"`
						Enabled bool              `json:"enabled"`
						Events  []string          `json:"events"`
					} `json:"channel"`
				}
				if err := e.call(ctx, http.MethodGet, pathOf("/api/v1/notifiers/{id}", in.ID), nil, &got); err != nil {
					return reply[map[string]any]{}, err
				}
				c := got.Channel
				if in.Name != nil {
					c.Name = *in.Name
				}
				if in.Enabled != nil {
					c.Enabled = *in.Enabled
				}
				if in.Events != nil {
					c.Events = in.Events
				}
				if c.Options == nil {
					c.Options = map[string]string{}
				}
				for k, v := range in.Options {
					c.Options[k] = v
				}
				body := map[string]any{"name": c.Name, "driver": c.Driver, "options": c.Options, "enabled": c.Enabled, "events": c.Events}
				if err := e.call(ctx, http.MethodPut, pathOf("/api/v1/notifiers/{id}", in.ID), body, &out); err != nil {
					return reply[map[string]any]{}, err
				}
				return say(out, "Saved the channel %q.", nameOf(out, "channel"))
			}),
		simple(&mcp.Tool{Name: "delete_channel", Description: "Delete a notification channel.", Annotations: destroys("Delete a channel", true)}, "DELETE /api/v1/notifiers/{id}", settings,
			func(in idIn) (request, error) {
				return request{path: pathOf("/api/v1/notifiers/{id}", in.ID)}, need(in.ID, "id")
			},
			func(idIn, map[string]any) string { return "Deleted the channel." }),
		simple(&mcp.Tool{Name: "test_channel", Description: "Send a test message on a channel, to check it is set up.", Annotations: changes("Test a channel", false)}, "POST /api/v1/notifiers/{id}/test", settings,
			func(in idIn) (request, error) {
				return request{path: pathOf("/api/v1/notifiers/{id}/test", in.ID)}, need(in.ID, "id")
			},
			func(idIn, map[string]any) string { return "Sent a test message." }),
		simple(&mcp.Tool{Name: "list_deliveries", Description: "Recent notification deliveries: what was announced where, and whether it went or why not.",
			Annotations: readOnly("Deliveries")}, "GET /api/v1/notifiers/deliveries", settings,
			func(in struct {
				Limit int `json:"limit,omitempty"`
			}) (request, error) {
				q := url.Values{}
				if in.Limit > 0 {
					q.Set("limit", strconv.Itoa(in.Limit))
				}
				return request{path: "/api/v1/notifiers/deliveries", query: q}, nil
			},
			func(_ struct {
				Limit int `json:"limit,omitempty"`
			}, out map[string]any) string {
				return fmt.Sprintf("%d deliveries.", count(out, "deliveries"))
			}),
	}
}
