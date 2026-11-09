package mcpserver

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Person fields (docs/data-model.md#person-fields) name people by user ID.
// Tools take them by user ID, email or a display name only one person has,
// several separated by ";", and the API checks each is someone the caller
// may choose there.

type fieldPeopleIn struct {
	Asset       string   `json:"asset,omitempty" jsonschema:"the asset whose person fields are being set, by ID or code"`
	CategoryIDs []string `json:"category_ids,omitempty" jsonschema:"or the categories of an asset being added"`
	Query       string   `json:"query,omitempty" jsonschema:"part of a name, to narrow the list"`
}

type addPersonIn struct {
	Name  string `json:"name" jsonschema:"their name, as fields show it"`
	Email string `json:"email,omitempty" jsonschema:"their email, which joins them to their first sign-in through a provider that links by verified email; none is fine"`
}

func peopleTools() []tool {
	list := define(&mcp.Tool{Name: "list_field_people", Description: "The people a person field of an asset, or of an asset being added in some categories, may be set to, by name, " +
		"with the user ID to set each by. It takes assets.track there; without it, only yourself. can_add says whether add_person may add someone without an account.",
		Annotations: readOnly("List people for fields")}, []string{"GET /api/v1/people/choices"}, []string{rbac.PermAssetsRead},
		func(ctx context.Context, e *env, in fieldPeopleIn) (reply[map[string]any], error) {
			q, err := e.peopleScope(ctx, in.Asset, in.CategoryIDs)
			if err != nil {
				return reply[map[string]any]{}, err
			}
			if in.Query != "" {
				q.Set("q", in.Query)
			}
			out := map[string]any{}
			if err := e.call(ctx, http.MethodGet, "/api/v1/people/choices?"+q.Encode(), nil, &out); err != nil {
				return reply[map[string]any]{}, err
			}
			return say(out, "%d people.", count(out, "people"))
		})
	add := simple(&mcp.Tool{Name: "add_person", Description: "Add a person without an account, to name in person fields: a name, and an email or none. " +
		"They cannot sign in until they first sign in through an identity provider that links by verified email, or are given a password.",
		Annotations: changes("Add a person", false)}, "POST /api/v1/people", []string{rbac.PermPeopleAdd, rbac.PermUsersManage},
		func(in addPersonIn) (request, error) {
			if strings.TrimSpace(in.Name) == "" {
				return request{}, fmt.Errorf("name is required")
			}
			return request{path: "/api/v1/people", body: map[string]string{"name": in.Name, "email": in.Email}}, nil
		},
		func(_ addPersonIn, out map[string]any) string {
			return fmt.Sprintf("Added %s, who has no account yet.", nameOf(out, "person"))
		})
	return []tool{list, add}
}

// peopleScope is the query naming where person fields are being set: an
// asset, or the categories of one being added.
func (e *env) peopleScope(ctx context.Context, asset string, categoryIDs []string) (url.Values, error) {
	q := url.Values{}
	switch {
	case len(categoryIDs) > 0:
		q["category_id"] = categoryIDs
	case asset != "":
		id, err := e.assetID(ctx, asset)
		if err != nil {
			return nil, err
		}
		q.Set("asset_id", id)
	}
	return q, nil
}

// resolvePeople gives the fields' values with each person field's people,
// named by user ID, email or a display name only one has and separated by
// ";", as the user IDs the API stores. Others are left as they are.
func (e *env) resolvePeople(ctx context.Context, values map[string]string, asset string, categoryIDs []string) (map[string]string, error) {
	if len(values) == 0 {
		return values, nil
	}
	var defs struct {
		Fields []models.Field `json:"fields"`
	}
	if err := e.call(ctx, http.MethodGet, "/api/v1/fields", nil, &defs); err != nil {
		return nil, err
	}
	person := map[string]bool{}
	for _, f := range defs.Fields {
		person[f.Key] = f.ValidationType == "person"
	}
	out := make(map[string]string, len(values))
	var scope url.Values
	for k, v := range values {
		out[k] = v
		if !person[k] || strings.TrimSpace(v) == "" {
			continue
		}
		if scope == nil {
			var err error
			if scope, err = e.peopleScope(ctx, asset, categoryIDs); err != nil {
				return nil, err
			}
		}
		var ids []string
		for _, ref := range strings.Split(v, ";") {
			ref = strings.TrimSpace(ref)
			if ref == "" {
				continue
			}
			q := url.Values{"ref": {ref}}
			for key, vals := range scope {
				q[key] = vals
			}
			var found struct {
				People []models.FieldPerson `json:"people"`
			}
			if err := e.call(ctx, http.MethodGet, "/api/v1/people/choices?"+q.Encode(), nil, &found); err != nil {
				return nil, fmt.Errorf("field %s: %w", k, err)
			}
			for _, p := range found.People {
				ids = append(ids, p.ID)
			}
		}
		out[k] = strings.Join(ids, ",")
	}
	return out, nil
}
