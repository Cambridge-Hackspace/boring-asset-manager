package api

import (
	"slices"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

// TestPersonFieldsOverMCP: an assistant sets person fields by email or a
// name only one person has, reads them by name and user ID, lists the
// people it may choose, and adds someone without an account only with
// people.add.
func TestPersonFieldsOverMCP(t *testing.T) {
	f := newPersonFixture(t)
	srv := f.srv
	secret := tokenWith(t, srv, f.admin.ID, "assistant", rbac.PermAssetsRead, rbac.PermAssetsWrite, rbac.PermAssetsTrack)
	cs := mcpSession(t, srv, secret)
	if slices.Contains(toolNames(t, cs), "add_person") {
		t.Error("add_person listed without people.add")
	}

	var created struct {
		Created []struct{ ID string }
		Failed  *struct{ Error string }
	}
	structured(t, callTool(t, cs, "create_asset", map[string]any{"assets": []map[string]any{{
		"name": "Lathe", "category_ids": []string{f.cat},
		"fields": map[string]string{"keeper": f.bobEmail, "trained": "ada; " + f.cy.ID},
	}}}), &created)
	if len(created.Created) != 1 {
		t.Fatalf("created: %+v %+v", created, created.Failed)
	}
	id := created.Created[0].ID
	a, _ := srv.store.GetAssetByID(id)
	if a.FieldValues["keeper"] != f.bob.ID || a.FieldValues["trained"] != f.ada.ID+","+f.cy.ID {
		t.Errorf("stored: %v", a.FieldValues)
	}

	res := callTool(t, cs, "get_asset", map[string]any{"asset": id})
	var got struct {
		Fields []struct {
			Key, Value string
			People     []struct{ ID, Name string }
		}
	}
	structured(t, res, &got)
	found := false
	for _, fv := range got.Fields {
		if fv.Key == "trained" {
			found = fv.Value == "Ada, Cy" && len(fv.People) == 2 && fv.People[0].ID == f.ada.ID
		}
	}
	if !found {
		t.Errorf("read: %+v", got.Fields)
	}
	if raw := toolText(res); strings.Contains(raw, f.bobEmail) {
		t.Errorf("get_asset shows an email: %s", raw)
	}

	// A name two people have is refused, naming the field.
	_, _ = srv.store.CreatePerson("Bob", "")
	res = callTool(t, cs, "update_asset", map[string]any{"asset": id, "fields": map[string]string{"keeper": "Bob"}})
	if !res.IsError || !strings.Contains(toolText(res), "keeper") {
		t.Errorf("an ambiguous name: %s", toolText(res))
	}

	var people struct {
		People []struct{ Name string }
	}
	structured(t, callTool(t, cs, "list_field_people", map[string]any{"asset": id, "query": "cy"}), &people)
	if len(people.People) != 1 || people.People[0].Name != "Cy" {
		t.Errorf("listed: %+v", people)
	}

	adder := tokenWith(t, srv, f.admin.ID, "adder", rbac.PermAssetsRead, rbac.PermPeopleAdd)
	cs = mcpSession(t, srv, adder)
	var added struct {
		Person struct{ ID, Name string }
	}
	structured(t, callTool(t, cs, "add_person", map[string]any{"name": "Dee"}), &added)
	if added.Person.Name != "Dee" {
		t.Errorf("added: %+v", added)
	}
	if u, _ := srv.store.GetUserByID(added.Person.ID); u == nil || u.CanSignIn {
		t.Errorf("added as: %+v", u)
	}
}
