package integration

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// TestInheritanceConflictResolution builds a tree where the same fields are
// assigned at several levels and in two branches, then checks what the API
// resolves for categories, for a would-be asset and for a real one.
func TestInheritanceConflictResolution(t *testing.T) {
	e := newEnv(t)
	c := e.admin("ada@hackspace.org")

	fieldIDs := map[string]string{}
	for _, f := range []struct{ key, kind string }{{"serial", "text"}, {"voltage", "number"}, {"notes", "text"}} {
		var out struct{ Field idOnly }
		c.mustJSON(http.MethodPost, "/fields", map[string]string{"key": f.key, "label": strings.ToUpper(f.key[:1]) + f.key[1:], "validation_type": f.kind}, &out, http.StatusCreated)
		fieldIDs[f.key] = out.Field.ID
	}
	tools := c.newCategory(map[string]any{"name": "Tools"})
	power := c.newCategory(map[string]any{"name": "Power", "parent_id": tools})
	drills := c.newCategory(map[string]any{"name": "Drills", "parent_id": power})
	rentals := c.newCategory(map[string]any{"name": "Rentals"})
	assign := func(cat, field string, required bool) {
		c.mustJSON(http.MethodPost, "/categories/"+cat+"/fields", map[string]any{"field_id": fieldIDs[field], "is_required": required}, nil, http.StatusOK)
	}
	assign(tools, "serial", false)   // optional up the tree...
	assign(rentals, "serial", true)  // ...required in the other branch
	assign(power, "voltage", true)   // required on an ancestor...
	assign(drills, "voltage", false) // ...optional nearer: still required
	assign(drills, "notes", false)
	assign(rentals, "notes", false)

	byKey := func(list []effectiveField) map[string]effectiveField {
		m := map[string]effectiveField{}
		for _, ef := range list {
			m[ef.Field.Key] = ef
		}
		return m
	}
	var own struct{ Fields []effectiveField }
	c.mustJSON(http.MethodGet, "/categories/"+drills+"/fields", nil, &own, http.StatusOK)
	got := byKey(own.Fields)
	for key, want := range map[string]effectiveField{
		"serial":  {IsRequired: false, Source: tools},
		"voltage": {IsRequired: true, Source: drills},
		"notes":   {IsRequired: false, Source: drills},
	} {
		if g := got[key]; g.IsRequired != want.IsRequired || g.Source != want.Source {
			t.Errorf("Drills %s: required %v from %s, want %v from %s", key, g.IsRequired, g.Source, want.IsRequired, want.Source)
		}
	}

	// In both branches: required anywhere means required.
	var union struct{ Fields []effectiveField }
	c.mustJSON(http.MethodPost, "/categories/effective-fields", map[string]any{"category_ids": []string{drills, rentals}}, &union, http.StatusOK)
	got = byKey(union.Fields)
	if len(got) != 3 || !got["serial"].IsRequired || !got["voltage"].IsRequired || got["notes"].IsRequired {
		t.Errorf("union %+v", union.Fields)
	}

	// The asset form's rules are the server's rules.
	r := c.send(http.MethodPost, "/assets", map[string]any{"name": "Drill", "category_ids": []string{drills, rentals}, "field_values": map[string]string{"voltage": "18"}})
	if r.status != http.StatusBadRequest || !strings.Contains(string(r.body), "serial") {
		t.Errorf("missing serial: %s", r)
	}
	id := c.newAsset(map[string]any{"name": "Drill", "category_ids": []string{drills, rentals}, "field_values": map[string]string{"voltage": "18", "serial": "SN1"}})

	// Leaving Rentals makes serial optional, so it can be cleared.
	c.mustJSON(http.MethodPut, "/assets/"+id, map[string]any{"category_ids": []string{drills}, "field_values": map[string]string{"serial": ""}}, nil, http.StatusOK)
	// Leaving Drills for Rentals drops voltage from the form but keeps its value.
	c.mustJSON(http.MethodPut, "/assets/"+id, map[string]any{"category_ids": []string{rentals}, "field_values": map[string]string{"serial": "SN2"}}, nil, http.StatusOK)
	var a assetResponse
	c.mustJSON(http.MethodGet, "/assets/"+id, nil, &a, http.StatusOK)
	if a.Asset.FieldValues["voltage"] != "18" || a.Asset.FieldValues["serial"] != "SN2" {
		t.Errorf("values %v", a.Asset.FieldValues)
	}
	if _, ok := byKey(a.Asset.Effective)["voltage"]; ok {
		t.Error("voltage still on the form after leaving Drills")
	}
}

// TestAntiLockoutUnderConcurrency has the only two administrators remove
// each other at the same moment, round after round, by deleting the other or
// by taking away their roles. However the requests interleave, exactly one
// may succeed.
func TestAntiLockoutUnderConcurrency(t *testing.T) {
	e := newEnv(t)
	e.admin("admin0@hackspace.org")
	survivor := "admin0@hackspace.org"

	for round := 1; round <= 4; round++ {
		newcomer := fmt.Sprintf("admin%d@hackspace.org", round)
		if _, err := e.st.CreateAdminUser(newcomer, password); err != nil {
			t.Fatal(err)
		}
		pair := []string{survivor, newcomer}
		clients := []*client{e.signIn(pair[0]), e.signIn(pair[1])}
		ids := make([]string, 2)
		for i, email := range pair {
			u, err := e.st.GetUserByEmail(email)
			if err != nil {
				t.Fatal(err)
			}
			ids[i] = u.ID
		}

		results := make([]response, 2)
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := range clients {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				other := ids[1-i]
				if round%2 == 0 {
					results[i] = clients[i].send(http.MethodDelete, "/users/"+other, nil)
				} else {
					results[i] = clients[i].send(http.MethodPut, "/users/"+other+"/roles", map[string]any{"role_ids": []string{}})
				}
			}(i)
		}
		close(start)
		wg.Wait()

		won := -1
		for i, r := range results {
			switch r.status {
			case http.StatusOK:
				if won >= 0 {
					t.Fatalf("round %d: both removals succeeded: %v", round, results)
				}
				won = i
			case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden:
				// Refused by the guard, or the caller was already removed.
			default:
				t.Fatalf("round %d: unexpected %s", round, r)
			}
		}
		if won < 0 {
			t.Fatalf("round %d: neither removal succeeded: %v", round, results)
		}
		if n, err := e.st.CountActiveAdmins(); err != nil || n != 1 {
			t.Fatalf("round %d: %d active admins (%v)", round, n, err)
		}
		// The loser, deleted or demoted, is no longer an administrator; the
		// next round pairs the survivor with a newcomer.
		survivor = pair[won]
	}
}
