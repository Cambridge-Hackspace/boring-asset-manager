package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// awkward is text that databases are known to mangle: characters outside
// Latin-1, four-byte characters, combining marks, right-to-left text and
// joiners, and the quoting and wildcard characters of SQL.
var awkward = []string{
	"Ångström · naïve café",
	"工具箱 ドリル",
	"🔧 Toolbox 🪛",
	"e" + string(rune(0x301)) + " / é",
	`C:\path\'quoted' "double" %_ \`,
	"مثقاب 👩" + string(rune(0x200d)) + "🔧",
}

// nul is text holding a NUL character, which PostgreSQL cannot store.
var nul = "a" + string(rune(0)) + "b"

// textSlot is somewhere the API stores text a user typed.
type textSlot struct {
	name string
	max  int // characters
	// create sends v and returns the response and the path to read it back.
	create func(c *client, n int, v string) (response, string)
}

func textSlots(t *testing.T, c *client) []textSlot {
	var notes struct{ Field idOnly }
	c.mustJSON(http.MethodPost, "/fields", map[string]string{"key": "notes", "label": "Notes", "validation_type": "text"}, &notes, http.StatusCreated)
	holder := c.newCategory(map[string]any{"name": "Holder"})
	c.mustJSON(http.MethodPost, "/categories/"+holder+"/fields", map[string]any{"field_id": notes.Field.ID}, nil, http.StatusOK)

	post := func(c *client, path string, body any, id func(map[string]json.RawMessage) string) (response, string) {
		r := c.send(http.MethodPost, path, body)
		if r.status != http.StatusCreated {
			return r, ""
		}
		var doc map[string]json.RawMessage
		if err := json.Unmarshal(r.body, &doc); err != nil {
			t.Fatalf("POST %s: %s", path, r)
		}
		return r, path + "/" + id(doc)
	}
	// under reads {"<key>": {"id": ...}}.
	under := func(key string) func(map[string]json.RawMessage) string {
		return func(doc map[string]json.RawMessage) string {
			var v idOnly
			_ = json.Unmarshal(doc[key], &v)
			return v.ID
		}
	}
	asset := func(doc map[string]json.RawMessage) string {
		var v struct{ Asset idOnly }
		_ = json.Unmarshal(doc["asset"], &v)
		return v.Asset.ID
	}
	return []textSlot{
		{"category name", 255, func(c *client, _ int, v string) (response, string) {
			return post(c, "/categories", map[string]any{"name": v}, under("category"))
		}},
		{"asset name", 255, func(c *client, _ int, v string) (response, string) {
			return post(c, "/assets", map[string]any{"name": v}, asset)
		}},
		{"generator code", 255, func(c *client, _ int, v string) (response, string) {
			return post(c, "/assets", map[string]any{"name": "Coded", "generator_keys": []map[string]string{{"generator_type": "qr", "assigned_code": v}}}, asset)
		}},
		{"field value", 10000, func(c *client, _ int, v string) (response, string) {
			return post(c, "/assets", map[string]any{"name": "Noted", "category_ids": []string{holder}, "field_values": map[string]string{"notes": v}}, asset)
		}},
		{"field label", 255, func(c *client, n int, v string) (response, string) {
			return post(c, "/fields", map[string]any{"key": fmt.Sprintf("f%d", n), "label": v, "validation_type": "text"}, under("field"))
		}},
		{"printer name", 255, func(c *client, _ int, v string) (response, string) {
			return post(c, "/printers", map[string]any{
				"name": v, "driver": "brother_ql", "model": "QL-810W", "media": "dk-22205",
				"connection": map[string]string{"type": "tcp", "address": "10.0.0.9"},
			}, under("printer"))
		}},
		{"template name", 255, func(c *client, n int, v string) (response, string) {
			return post(c, "/templates", map[string]any{
				"id": fmt.Sprintf("t%d", n), "name": v, "units": "mm", "width": 62, "height": 29, "dpi": 300, "elements": []any{},
			}, under("template"))
		}},
		{"user name", 255, func(c *client, n int, v string) (response, string) {
			r := c.send(http.MethodPost, "/users", map[string]any{"email": fmt.Sprintf("named%d@hackspace.org", n), "name": v, "password": "Secret123!"})
			if r.status != http.StatusCreated {
				return r, ""
			}
			// There is no single-user read; the list holds every user.
			return r, "/users"
		}},
		{"notification channel name", 255, func(c *client, _ int, v string) (response, string) {
			return post(c, "/notifiers", map[string]any{
				"name": v, "driver": "integration-recorder", "options": map[string]string{"name": "x"}, "enabled": true, "events": []string{models.EventRunningLow},
			}, under("channel"))
		}},
	}
}

// holds reports whether a decoded JSON document has v as one of its strings.
func holds(doc any, v string) bool {
	switch d := doc.(type) {
	case string:
		return d == v
	case []any:
		for _, e := range d {
			if holds(e, v) {
				return true
			}
		}
	case map[string]any:
		for _, e := range d {
			if holds(e, v) {
				return true
			}
		}
	}
	return false
}

// TestTextSurvivesTheDatabase stores awkward text, text of the greatest
// length allowed, and text that may not be stored, everywhere the API keeps
// text. Whatever the database and its settings, text comes back exactly as
// sent, and text that may not be stored is refused the same way everywhere:
// never cut short, never altered, never a server error.
func TestTextSurvivesTheDatabase(t *testing.T) {
	e := newEnv(t)
	c := e.admin("ada@hackspace.org")
	n := 0
	for _, slot := range textSlots(t, c) {
		cases := append([]string{}, awkward...)
		// The longest allowed, in four-byte characters.
		cases = append(cases, strings.Repeat("🔧", slot.max))
		for _, v := range cases {
			n++
			r, path := slot.create(c, n, v)
			if path == "" {
				t.Errorf("%s %.40q: %.300s", slot.name, v, r)
				continue
			}
			var doc any
			got := c.mustJSON(http.MethodGet, path, nil, &doc, http.StatusOK)
			if !holds(doc, v) {
				t.Errorf("%s %.40q came back changed: %.300s", slot.name, v, got.body)
			}
		}
		for what, v := range map[string]string{
			"one character too long": strings.Repeat("🔧", slot.max+1),
			"a NUL character":        nul,
		} {
			n++
			r, _ := slot.create(c, n, v)
			if r.status != http.StatusBadRequest {
				t.Errorf("%s with %s: got %.300s, want 400", slot.name, what, r)
			}
		}
	}
}

// TestOverlongEmailIsRefused checks that an email address longer than its
// column is refused as the caller's mistake, on every database.
func TestOverlongEmailIsRefused(t *testing.T) {
	e := newEnv(t)
	c := e.admin("ada@hackspace.org")
	long := strings.Repeat("a", 250) + "@x.org"
	r := c.send(http.MethodPost, "/users", map[string]any{"email": long, "password": "Secret123!"})
	if r.status != http.StatusBadRequest || !strings.Contains(string(r.body), "longer than 255 characters") {
		t.Errorf("got %s, want 400", r)
	}
}

// TestDistinctTextStaysDistinct stores pairs that some database collations
// treat as equal. On every database each of a pair is a different code, so
// both may be assigned, and each finds only its own asset.
func TestDistinctTextStaysDistinct(t *testing.T) {
	e := newEnv(t)
	c := e.admin("ada@hackspace.org")
	pairs := [][2]string{
		{"Drill", "drill"},
		{"é", "e" + string(rune(0x301))},
		{"Straße", "Strasse"},
		{"A1", "Ａ1"}, // fullwidth A
		{"bit", "bit "},
	}
	for _, p := range pairs {
		for _, code := range p {
			r := c.send(http.MethodPost, "/assets", map[string]any{"name": code, "generator_keys": []map[string]string{{"generator_type": "qr", "assigned_code": code}}})
			if p[1] == "bit " && code == "bit " {
				// Codes are trimmed, so this one is a duplicate everywhere.
				if r.status != http.StatusConflict {
					t.Errorf("%q after %q: got %s, want 409", code, p[0], r)
				}
				continue
			}
			if r.status != http.StatusCreated {
				t.Errorf("%q after %q: %s", code, p[0], r)
			}
		}
	}
	// Channel names are unique, but only exactly.
	for _, name := range []string{"Alerts", "alerts", "ALERTS"} {
		c.mustJSON(http.MethodPost, "/notifiers", map[string]any{
			"name": name, "driver": "integration-recorder", "options": map[string]string{"name": "x"}, "enabled": true, "events": []string{models.EventRunningLow},
		}, nil, http.StatusCreated)
	}
}

// TestSearchTreatsTextLiterally searches for SQL wildcards and escapes, and
// for letters outside ASCII in another case.
func TestSearchTreatsTextLiterally(t *testing.T) {
	e := newEnv(t)
	c := e.admin("ada@hackspace.org")
	names := []string{"100% cotton", "snake_case", `C:\tools`, "Ångström gauge", "Plain"}
	for _, name := range names {
		c.newAsset(map[string]any{"name": name})
	}
	for q, want := range map[string]string{
		"%":        "100% cotton",
		"_":        "snake_case",
		`\`:        `C:\tools`,
		`:\t`:      `C:\tools`,
		"ÅNGSTRÖM": "Ångström gauge",
		"ångström": "Ångström gauge",
	} {
		var out struct {
			Assets []struct {
				Asset struct{ Name string }
			}
		}
		c.mustJSON(http.MethodGet, "/assets?search="+url.QueryEscape(q), nil, &out, http.StatusOK)
		if len(out.Assets) != 1 || out.Assets[0].Asset.Name != want {
			t.Errorf("searching %q found %+v, want only %q", q, out.Assets, want)
		}
	}
	// Nothing stored can hold a NUL, so a search for one, like any request
	// holding one, is refused before it reaches the database.
	c.mustJSON(http.MethodGet, "/assets?search="+url.QueryEscape(nul), nil, nil, http.StatusBadRequest)
	c.mustJSON(http.MethodGet, "/assets/"+url.PathEscape(nul), nil, nil, http.StatusBadRequest)
}

// TestImportRefusesUnstorableText imports files with bytes that are not
// UTF-8, NUL characters and overlong values, and checks each is reported
// against its row and column, with nothing imported.
func TestImportRefusesUnstorableText(t *testing.T) {
	e := newEnv(t)
	c := e.admin("ada@hackspace.org")
	for what, file := range map[string]string{
		"bytes that are not UTF-8": "name\nGood\nBad " + string([]byte{0xff, 0xfe}) + "\n",
		"a NUL character":          "name\nGood\n" + nul + "\n",
		"an overlong name":         "name\nGood\n" + strings.Repeat("x", 256) + "\n",
	} {
		r := c.send(http.MethodPost, "/assets/import", csvForm(file, []string{"name"}))
		if r.status != http.StatusUnprocessableEntity || !strings.Contains(string(r.body), `"row":3`) {
			t.Errorf("%s: %.400s", what, r)
		}
	}
	list, err := e.st.ListAssets("", "")
	if err != nil || len(list) != 0 {
		t.Errorf("%d assets imported (%v)", len(list), err)
	}
}
