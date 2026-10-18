package mcpserver

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type idIn struct {
	ID string `json:"id"`
}

type categoryIn struct {
	ID                string  `json:"id,omitempty" jsonschema:"the category to change; empty to make one"`
	Name              *string `json:"name,omitempty"`
	ParentID          *string `json:"parent_id,omitempty" jsonschema:"the category it is under; empty for the top"`
	ColorHex          *string `json:"color_hex,omitempty" jsonschema:"such as #3366ff"`
	IsConsumable      *bool   `json:"is_consumable,omitempty" jsonschema:"its assets are used up, and counted"`
	Unit              *string `json:"unit,omitempty" jsonschema:"for a consumable category, totals its assets' stock in this unit (such as g) instead of counting them"`
	IsLendable        *bool   `json:"is_lendable,omitempty" jsonschema:"its assets, and those below it, can be lent"`
	LowStockThreshold *int    `json:"low_stock_threshold,omitempty" jsonschema:"for a consumable category, flag it running low below this"`
	ClearThreshold    bool    `json:"clear_threshold,omitempty" jsonschema:"remove its low stock threshold"`
}

func (in categoryIn) changes() map[string]any {
	c := given(map[string]any{"name": in.Name, "color_hex": in.ColorHex, "is_consumable": in.IsConsumable, "unit": in.Unit,
		"is_lendable": in.IsLendable, "low_stock_threshold": in.LowStockThreshold})
	if in.ParentID != nil {
		if *in.ParentID == "" {
			c["parent_id"] = nil
		} else {
			c["parent_id"] = *in.ParentID
		}
	}
	if in.ClearThreshold {
		c["low_stock_threshold"] = nil
	}
	return c
}

type categoryDetail struct {
	Category           models.Category         `json:"category"`
	Fields             []models.EffectiveField `json:"fields" jsonschema:"the fields its assets record, its own and those from above it"`
	SuggestedLinkTypes []string                `json:"suggested_link_type_ids"`
	Actions            []any                   `json:"actions" jsonschema:"the actions it offers on its assets"`
	Scopes             []string                `json:"scope_ids,omitempty" jsonschema:"its own scopes, if you may see them"`
}

type fieldIn struct {
	ID             string  `json:"id,omitempty" jsonschema:"the field to change; empty to make one"`
	Key            string  `json:"key,omitempty" jsonschema:"a new field's key, used in imports and label bindings; cannot change"`
	Label          *string `json:"label,omitempty"`
	ValidationType *string `json:"validation_type,omitempty" jsonschema:"text, number, date, url or select"`
	Options        any     `json:"options,omitempty" jsonschema:"for select, the choices, as {\"choices\": [...]}"`
}

type linkTypeIn struct {
	ID          string  `json:"id,omitempty" jsonschema:"the link type to change; empty to make one"`
	Name        *string `json:"name,omitempty" jsonschema:"read from the first asset, such as contains"`
	InverseName *string `json:"inverse_name,omitempty" jsonschema:"read from the second, such as contained in"`
	IsSymmetric *bool   `json:"is_symmetric,omitempty" jsonschema:"reads the same both ways, as used with"`
	IsSingle    *bool   `json:"is_single,omitempty" jsonschema:"the second asset can have one such link only"`
	IsLocation  *bool   `json:"is_location,omitempty" jsonschema:"its links say where things are (must be single)"`
}

type actionIn struct {
	ID      string  `json:"id,omitempty" jsonschema:"the action to change; empty to make one"`
	Key     *string `json:"key,omitempty" jsonschema:"what devices listen for: lowercase letters, digits, - and _, starting with a letter"`
	Label   *string `json:"label,omitempty" jsonschema:"what people see on the button"`
	Confirm *string `json:"confirm,omitempty" jsonschema:"a question to ask before pressing it; empty for none"`
}

type categoryFieldIn struct {
	CategoryID string `json:"category_id"`
	FieldID    string `json:"field_id"`
	Required   bool   `json:"required,omitempty"`
}

type categoryListIn struct {
	CategoryID string   `json:"category_id"`
	IDs        []string `json:"ids" jsonschema:"the whole list, in order: those not named are taken off"`
}

type categoryActionsIn struct {
	CategoryID string `json:"category_id"`
	Actions    []struct {
		ActionID string `json:"action_id"`
		Cascades bool   `json:"cascades,omitempty" jsonschema:"offer it on the assets of its subcategories too"`
	} `json:"actions" jsonschema:"the whole list: those not named are taken off"`
}

type templateChoiceIn struct {
	CategoryID string  `json:"category_id"`
	TemplateID *string `json:"template_id,omitempty" jsonschema:"the template; empty or absent for none, so its assets use one from above"`
}

type conflictsIn struct {
	CategoryID string  `json:"category_id,omitempty" jsonschema:"with template_id: answer as if this category named that template"`
	TemplateID *string `json:"template_id,omitempty"`
}

func (in conflictsIn) query(param string) url.Values {
	q := url.Values{}
	if in.CategoryID != "" {
		q.Set("category_id", in.CategoryID)
		if in.TemplateID != nil {
			q.Set(param, *in.TemplateID)
		} else {
			q.Set(param, "")
		}
	}
	return q
}

func catalogTools() []tool {
	manage := []string{rbac.PermCategoriesManage}
	read := []string{rbac.PermAssetsRead}
	return []tool{
		simple(&mcp.Tool{Name: "list_categories", Description: "Every category, with its parent (the tree), whether it is lendable or consumable, and whether it is running low.",
			Annotations: readOnly("List categories")}, "GET /api/v1/categories", read,
			func(none) (request, error) { return request{path: "/api/v1/categories"}, nil },
			func(_ none, out map[string]any) string {
				return fmt.Sprintf("%d categories.", count(out, "categories"))
			}),
		define(&mcp.Tool{Name: "get_category", Description: "One category: its settings, the fields its assets record (its own and inherited, with their types and choices), " +
			"the link types it suggests, the actions it offers, and its scopes.", Annotations: readOnly("Get a category")},
			[]string{"GET /api/v1/categories/{id}", "GET /api/v1/categories/{id}/fields", "GET /api/v1/categories/{id}/relation-types",
				"GET /api/v1/categories/{id}/actions", "GET /api/v1/categories/{id}/scopes"}, read,
			func(ctx context.Context, e *env, in idIn) (reply[categoryDetail], error) {
				var out categoryDetail
				var got struct{ Category models.Category }
				if err := e.call(ctx, http.MethodGet, pathOf("/api/v1/categories/{id}", in.ID), nil, &got); err != nil {
					return reply[categoryDetail]{}, err
				}
				out.Category = got.Category
				var fields struct{ Fields []models.EffectiveField }
				if err := e.call(ctx, http.MethodGet, pathOf("/api/v1/categories/{id}/fields", in.ID), nil, &fields); err != nil {
					return reply[categoryDetail]{}, err
				}
				out.Fields = fields.Fields
				var types struct {
					IDs []string `json:"relation_type_ids"`
				}
				if err := e.call(ctx, http.MethodGet, pathOf("/api/v1/categories/{id}/relation-types", in.ID), nil, &types); err != nil {
					return reply[categoryDetail]{}, err
				}
				out.SuggestedLinkTypes = types.IDs
				var actions struct{ Actions []any }
				if err := e.call(ctx, http.MethodGet, pathOf("/api/v1/categories/{id}/actions", in.ID), nil, &actions); err != nil {
					return reply[categoryDetail]{}, err
				}
				out.Actions = actions.Actions
				// Scopes only for those who may see them.
				if e.caller.may([]string{rbac.PermUsersManage, rbac.PermCategoriesManage}) {
					var scopes struct {
						IDs []string `json:"scope_ids"`
					}
					if err := e.call(ctx, http.MethodGet, pathOf("/api/v1/categories/{id}/scopes", in.ID), nil, &scopes); err != nil {
						return reply[categoryDetail]{}, err
					}
					out.Scopes = scopes.IDs
				}
				var names []string
				for _, f := range out.Fields {
					names = append(names, f.Field.Key)
				}
				return say(out, "%q records %s.", out.Category.Name, orNone(names))
			}),
		define(&mcp.Tool{Name: "fields_for", Description: "The fields an asset in these categories would record, required anywhere meaning required: what create_asset takes.",
			Annotations: readOnly("Fields for categories")}, []string{"POST /api/v1/categories/effective-fields"}, read,
			func(ctx context.Context, e *env, in struct {
				CategoryIDs []string `json:"category_ids"`
			}) (reply[map[string]any], error) {
				ids := in.CategoryIDs
				if ids == nil {
					ids = []string{}
				}
				out := map[string]any{}
				if err := e.call(ctx, http.MethodPost, "/api/v1/categories/effective-fields", map[string]any{"category_ids": ids}, &out); err != nil {
					return reply[map[string]any]{}, err
				}
				return say(out, "%d fields.", count(out, "fields"))
			}),
		define(&mcp.Tool{Name: "save_category", Description: "Make a category, or change one: only what is given changes. Moving it under another changes which scopes its assets are in.",
			Annotations: changes("Save a category", true)}, []string{"POST /api/v1/categories", "PUT /api/v1/categories/{id}"}, manage,
			func(ctx context.Context, e *env, in categoryIn) (reply[map[string]any], error) {
				out := map[string]any{}
				if in.ID == "" {
					if in.Name == nil {
						return reply[map[string]any]{}, fmt.Errorf("a new category needs a name")
					}
					if err := e.call(ctx, http.MethodPost, "/api/v1/categories", in.changes(), &out); err != nil {
						return reply[map[string]any]{}, err
					}
					return say(out, "Made %q.", nameOf(out, "category"))
				}
				var got struct{ Category models.Category }
				if err := e.call(ctx, http.MethodGet, pathOf("/api/v1/categories/{id}", in.ID), nil, &got); err != nil {
					return reply[map[string]any]{}, err
				}
				body, err := merged(got.Category, in.changes())
				if err != nil {
					return reply[map[string]any]{}, err
				}
				if err := e.call(ctx, http.MethodPut, pathOf("/api/v1/categories/{id}", in.ID), body, &out); err != nil {
					return reply[map[string]any]{}, err
				}
				return say(out, "Saved %q.", nameOf(out, "category"))
			}),
		simple(&mcp.Tool{Name: "delete_category", Description: "Delete a category. Its assets stay, without it; its subcategories must go first.",
			Annotations: destroys("Delete a category", true)}, "DELETE /api/v1/categories/{id}", manage,
			func(in idIn) (request, error) {
				return request{path: pathOf("/api/v1/categories/{id}", in.ID)}, need(in.ID, "id")
			},
			func(idIn, map[string]any) string { return "Deleted the category." }),
		simple(&mcp.Tool{Name: "set_category_running_low", Description: "Flag a category as running low, or clear it, by hand.",
			Annotations: changes("Flag a category running low", true)}, "PUT /api/v1/categories/{id}/running-low", []string{rbac.PermStockRecord},
			func(in struct {
				ID         string `json:"id"`
				RunningLow bool   `json:"running_low"`
			}) (request, error) {
				return request{path: pathOf("/api/v1/categories/{id}/running-low", in.ID), body: map[string]bool{"running_low": in.RunningLow}}, need(in.ID, "id")
			},
			func(_ struct {
				ID         string `json:"id"`
				RunningLow bool   `json:"running_low"`
			}, out map[string]any) string {
				return fmt.Sprintf("%q flagged as you said.", nameOf(out, "category"))
			}),
		simple(&mcp.Tool{Name: "assign_field", Description: "Give a category's assets, and those below it, a field; required or not.",
			Annotations: changes("Assign a field", true)}, "POST /api/v1/categories/{id}/fields", manage,
			func(in categoryFieldIn) (request, error) {
				return request{path: pathOf("/api/v1/categories/{id}/fields", in.CategoryID), body: map[string]any{"field_id": in.FieldID, "is_required": in.Required}}, need(in.CategoryID, "category_id")
			},
			func(categoryFieldIn, map[string]any) string { return "Assigned the field." }),
		simple(&mcp.Tool{Name: "unassign_field", Description: "Take a field off a category. Its assets keep the values they had.",
			Annotations: destroys("Unassign a field", true)}, "DELETE /api/v1/categories/{id}/fields/{field_id}", manage,
			func(in categoryFieldIn) (request, error) {
				return request{path: pathOf("/api/v1/categories/{id}/fields/{field_id}", in.CategoryID, in.FieldID)}, need(in.CategoryID, "category_id")
			},
			func(categoryFieldIn, map[string]any) string { return "Took the field off." }),
		simple(&mcp.Tool{Name: "set_suggested_link_types", Description: "The link types a category suggests for its assets (by ID), offered first in the web app.",
			Annotations: changes("Suggest link types", true)}, "PUT /api/v1/categories/{id}/relation-types", manage,
			func(in categoryListIn) (request, error) {
				ids := in.IDs
				if ids == nil {
					ids = []string{}
				}
				return request{path: pathOf("/api/v1/categories/{id}/relation-types", in.CategoryID), body: map[string]any{"relation_type_ids": ids}}, need(in.CategoryID, "category_id")
			},
			func(in categoryListIn, _ map[string]any) string {
				return fmt.Sprintf("It suggests %d link types.", len(in.IDs))
			}),
		simple(&mcp.Tool{Name: "set_scan_template", Description: "The scan template (a report of kind scan) a category's assets show when scanned, or none, to use one from above.",
			Annotations: changes("Set the scan template", true)}, "PUT /api/v1/categories/{id}/scan-template", manage,
			func(in templateChoiceIn) (request, error) {
				var id *string
				if in.TemplateID != nil && *in.TemplateID != "" {
					id = in.TemplateID
				}
				return request{path: pathOf("/api/v1/categories/{id}/scan-template", in.CategoryID), body: map[string]any{"scan_template_id": id}}, need(in.CategoryID, "category_id")
			},
			func(templateChoiceIn, map[string]any) string { return "Set." }),
		simple(&mcp.Tool{Name: "set_label_templates", Description: "The label templates a category's assets print with, in order of preference: each label takes the first that suits its printer. An empty list for none.",
			Annotations: changes("Set label templates", true)}, "PUT /api/v1/categories/{id}/label-template", manage,
			func(in categoryListIn) (request, error) {
				ids := in.IDs
				if ids == nil {
					ids = []string{}
				}
				return request{path: pathOf("/api/v1/categories/{id}/label-template", in.CategoryID), body: map[string]any{"label_template_ids": ids}}, need(in.CategoryID, "category_id")
			},
			func(in categoryListIn, _ map[string]any) string {
				return fmt.Sprintf("It names %d label templates.", len(in.IDs))
			}),
		simple(&mcp.Tool{Name: "scan_template_conflicts", Description: "Categories in ties over which scan template their assets show, for someone to settle; or, with category_id and template_id, the ties choosing that would make.",
			Annotations: readOnly("Scan template ties")}, "GET /api/v1/categories/scan-conflicts", read,
			func(in conflictsIn) (request, error) {
				return request{path: "/api/v1/categories/scan-conflicts", query: in.query("scan_template_id")}, nil
			},
			func(_ conflictsIn, out map[string]any) string {
				return fmt.Sprintf("%d categories in ties.", count(out, "categories"))
			}),
		simple(&mcp.Tool{Name: "label_template_conflicts", Description: "Categories in ties over which label templates their assets print with; or, with category_id and template_id, the ties choosing that would make.",
			Annotations: readOnly("Label template ties")}, "GET /api/v1/categories/label-conflicts", read,
			func(in conflictsIn) (request, error) {
				return request{path: "/api/v1/categories/label-conflicts", query: in.query("label_template_id")}, nil
			},
			func(_ conflictsIn, out map[string]any) string {
				return fmt.Sprintf("%d categories in ties.", count(out, "categories"))
			}),
		simple(&mcp.Tool{Name: "set_category_actions", Description: "The actions a category offers on its assets, each with whether it also does on those of its subcategories.",
			Annotations: changes("Set category actions", true)}, "PUT /api/v1/categories/{id}/actions", manage,
			func(in categoryActionsIn) (request, error) {
				actions := []map[string]any{}
				for _, a := range in.Actions {
					actions = append(actions, map[string]any{"action_id": a.ActionID, "cascades": a.Cascades})
				}
				return request{path: pathOf("/api/v1/categories/{id}/actions", in.CategoryID), body: map[string]any{"actions": actions}}, need(in.CategoryID, "category_id")
			},
			func(in categoryActionsIn, _ map[string]any) string {
				return fmt.Sprintf("It offers %d actions.", len(in.Actions))
			}),

		// Fields.
		simple(&mcp.Tool{Name: "list_fields", Description: "Every field assets can record, shared by all categories: key, label, type and, for select, the choices.",
			Annotations: readOnly("List fields")}, "GET /api/v1/fields", read,
			func(none) (request, error) { return request{path: "/api/v1/fields"}, nil },
			func(_ none, out map[string]any) string { return fmt.Sprintf("%d fields.", count(out, "fields")) }),
		simple(&mcp.Tool{Name: "get_field", Description: "One field.", Annotations: readOnly("Get a field")}, "GET /api/v1/fields/{id}", read,
			func(in idIn) (request, error) {
				return request{path: pathOf("/api/v1/fields/{id}", in.ID)}, need(in.ID, "id")
			},
			func(_ idIn, out map[string]any) string { return fmt.Sprintf("%q.", nameOf(out, "field")) }),
		define(&mcp.Tool{Name: "save_field", Description: "Make a field, or change one's label, type or choices: only what is given changes. Its key cannot change.",
			Annotations: changes("Save a field", true)}, []string{"POST /api/v1/fields", "PUT /api/v1/fields/{id}"}, manage,
			func(ctx context.Context, e *env, in fieldIn) (reply[map[string]any], error) {
				out := map[string]any{}
				c := given(map[string]any{"label": in.Label, "validation_type": in.ValidationType})
				if in.Options != nil {
					c["options"] = in.Options
				}
				if in.ID == "" {
					c["key"] = in.Key
					if err := e.call(ctx, http.MethodPost, "/api/v1/fields", c, &out); err != nil {
						return reply[map[string]any]{}, err
					}
					return say(out, "Made the field %q.", nameOf(out, "field"))
				}
				var got struct{ Field models.Field }
				if err := e.call(ctx, http.MethodGet, pathOf("/api/v1/fields/{id}", in.ID), nil, &got); err != nil {
					return reply[map[string]any]{}, err
				}
				body, err := merged(got.Field, c)
				if err != nil {
					return reply[map[string]any]{}, err
				}
				if err := e.call(ctx, http.MethodPut, pathOf("/api/v1/fields/{id}", in.ID), body, &out); err != nil {
					return reply[map[string]any]{}, err
				}
				return say(out, "Saved the field %q.", nameOf(out, "field"))
			}),
		simple(&mcp.Tool{Name: "delete_field", Description: "Delete a field, and every asset's value of it.",
			Annotations: destroys("Delete a field", true)}, "DELETE /api/v1/fields/{id}", manage,
			func(in idIn) (request, error) {
				return request{path: pathOf("/api/v1/fields/{id}", in.ID)}, need(in.ID, "id")
			},
			func(idIn, map[string]any) string { return "Deleted the field." }),

		// Link types.
		simple(&mcp.Tool{Name: "list_link_types", Description: "The kinds of link between assets, each read from both ends (contains / contained in), and which says where things are.",
			Annotations: readOnly("List link types")}, "GET /api/v1/relation-types", read,
			func(none) (request, error) { return request{path: "/api/v1/relation-types"}, nil },
			func(_ none, out map[string]any) string {
				return fmt.Sprintf("%d link types.", count(out, "relation_types"))
			}),
		define(&mcp.Tool{Name: "save_link_type", Description: "Make a link type, or change one: only what is given changes. While links use it only its names can change.",
			Annotations: changes("Save a link type", true)}, []string{"POST /api/v1/relation-types", "PUT /api/v1/relation-types/{id}"}, manage,
			func(ctx context.Context, e *env, in linkTypeIn) (reply[map[string]any], error) {
				c := given(map[string]any{"name": in.Name, "inverse_name": in.InverseName, "is_symmetric": in.IsSymmetric, "is_single": in.IsSingle, "is_location": in.IsLocation})
				out := map[string]any{}
				if in.ID == "" {
					if err := e.call(ctx, http.MethodPost, "/api/v1/relation-types", c, &out); err != nil {
						return reply[map[string]any]{}, err
					}
					return say(out, "Made the link type %q.", nameOf(out, "relation_type"))
				}
				types, err := e.relationTypes(ctx)
				if err != nil {
					return reply[map[string]any]{}, err
				}
				current, ok := types[in.ID]
				if !ok {
					return reply[map[string]any]{}, fmt.Errorf("no link type has the ID %q", in.ID)
				}
				body, err := merged(current, c)
				if err != nil {
					return reply[map[string]any]{}, err
				}
				if err := e.call(ctx, http.MethodPut, pathOf("/api/v1/relation-types/{id}", in.ID), body, &out); err != nil {
					return reply[map[string]any]{}, err
				}
				return say(out, "Saved the link type %q.", nameOf(out, "relation_type"))
			}),
		simple(&mcp.Tool{Name: "delete_link_type", Description: "Delete a link type no link uses.",
			Annotations: destroys("Delete a link type", true)}, "DELETE /api/v1/relation-types/{id}", manage,
			func(in idIn) (request, error) {
				return request{path: pathOf("/api/v1/relation-types/{id}", in.ID)}, need(in.ID, "id")
			},
			func(idIn, map[string]any) string { return "Deleted the link type." }),

		// Actions.
		simple(&mcp.Tool{Name: "list_actions", Description: "Every action categories can offer on assets: its key (what devices listen for), label and question.",
			Annotations: readOnly("List actions")}, "GET /api/v1/actions", read,
			func(none) (request, error) { return request{path: "/api/v1/actions"}, nil },
			func(_ none, out map[string]any) string { return fmt.Sprintf("%d actions.", count(out, "actions")) }),
		define(&mcp.Tool{Name: "save_action", Description: "Make an action, or change one: only what is given changes.",
			Annotations: changes("Save an action", true)}, []string{"POST /api/v1/actions", "PUT /api/v1/actions/{id}"}, manage,
			func(ctx context.Context, e *env, in actionIn) (reply[map[string]any], error) {
				c := given(map[string]any{"key": in.Key, "label": in.Label, "confirm": in.Confirm})
				out := map[string]any{}
				if in.ID == "" {
					if err := e.call(ctx, http.MethodPost, "/api/v1/actions", c, &out); err != nil {
						return reply[map[string]any]{}, err
					}
					return say(out, "Made the action %q.", nameOf(out, "action"))
				}
				var list struct{ Actions []models.AssetAction }
				if err := e.call(ctx, http.MethodGet, "/api/v1/actions", nil, &list); err != nil {
					return reply[map[string]any]{}, err
				}
				var current *models.AssetAction
				for i := range list.Actions {
					if list.Actions[i].ID == in.ID {
						current = &list.Actions[i]
					}
				}
				if current == nil {
					return reply[map[string]any]{}, fmt.Errorf("no action has the ID %q", in.ID)
				}
				body, err := merged(current, c)
				if err != nil {
					return reply[map[string]any]{}, err
				}
				if err := e.call(ctx, http.MethodPut, pathOf("/api/v1/actions/{id}", in.ID), body, &out); err != nil {
					return reply[map[string]any]{}, err
				}
				return say(out, "Saved the action %q.", nameOf(out, "action"))
			}),
		simple(&mcp.Tool{Name: "delete_action", Description: "Delete an action, taking it off every category.",
			Annotations: destroys("Delete an action", true)}, "DELETE /api/v1/actions/{id}", manage,
			func(in idIn) (request, error) {
				return request{path: pathOf("/api/v1/actions/{id}", in.ID)}, need(in.ID, "id")
			},
			func(idIn, map[string]any) string { return "Deleted the action." }),
	}
}
