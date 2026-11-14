package query

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/terms"
)

func TestTellsQueriesFromPlainWords(t *testing.T) {
	for search, want := range map[string]bool{
		"shelf a":                        false,
		"black AND decker":               false,
		`"a = b"`:                        false,
		`6" ruler`:                       false,
		"name = drill":                   true,
		"field.diameter>=1.75":           true,
		"drill ORDER BY name":            true,
		"order by":                       true,
		`name = "unclosed`:               true,
		"condition != ok OR loan = open": true,
	} {
		if got := IsQuery(search); got != want {
			t.Errorf("IsQuery(%q) = %v, want %v", search, got, want)
		}
	}
}

// show writes a parsed condition back out, with every join in
// parentheses, so precedence can be seen.
func show(n Node) string {
	switch n := n.(type) {
	case And:
		return "(" + show(n.L) + " AND " + show(n.R) + ")"
	case Or:
		return "(" + show(n.L) + " OR " + show(n.R) + ")"
	case Not:
		return "NOT " + show(n.X)
	case Term:
		return "[" + n.Text + "]"
	case Compare:
		return n.Key.Name + n.Op + n.Value.Text
	case In:
		var vs []string
		for _, v := range n.Values {
			vs = append(vs, v.Text)
		}
		not := ""
		if n.Negate {
			not = "NOT "
		}
		return n.Key.Name + " " + not + "IN(" + strings.Join(vs, ",") + ")"
	case Empty:
		if n.Negate {
			return n.Key.Name + " IS NOT EMPTY"
		}
		return n.Key.Name + " IS EMPTY"
	}
	return "?"
}

func TestParsesWithAndBeforeOr(t *testing.T) {
	for in, want := range map[string]string{
		"a = 1 OR b = 2 AND c = 3":                "(a=1 OR (b=2 AND c=3))",
		"(a = 1 OR b = 2) AND c = 3":              "((a=1 OR b=2) AND c=3)",
		"NOT a = 1 AND b = 2":                     "(NOT a=1 AND b=2)",
		"not (a = 1 or b = 2)":                    "NOT (a=1 OR b=2)",
		`"shelf a" AND Name ~ 'tpu'`:              "([shelf a] AND name~tpu)",
		"condition NOT IN (ok, 'out of service')": "condition NOT IN(ok,out of service)",
		"loan IS EMPTY OR field.x IS NOT EMPTY":   "(loan IS EMPTY OR field.x IS NOT EMPTY)",
		`name = "say \"hi\""`:                     `name=say "hi"`,
		"created >= -30d":                         "created>=-30d",
	} {
		q, err := Parse(in)
		if err != nil {
			t.Errorf("Parse(%q): %v", in, err)
			continue
		}
		if got := show(q.Where); got != want {
			t.Errorf("Parse(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestParsesTheOrder(t *testing.T) {
	q, err := Parse("category = tools ORDER BY field.bought DESC, name")
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Order) != 2 || q.Order[0].Key.Name != "field.bought" || !q.Order[0].Desc || q.Order[1].Key.Name != "name" || q.Order[1].Desc {
		t.Fatalf("order = %+v", q.Order)
	}
	q, err = Parse("ORDER BY name")
	if err != nil || q.Where != nil || len(q.Order) != 1 {
		t.Fatalf("an order alone: %+v, %v", q, err)
	}
}

// at checks that err is an *Error over want in input, with a message
// holding msg.
func at(t *testing.T, input string, err error, want, msg string) {
	t.Helper()
	var qe *Error
	if !errors.As(err, &qe) {
		t.Fatalf("%q: got %v, want a query error", input, err)
	}
	if got := input[qe.Start:qe.End]; got != want {
		t.Errorf("%q: the error is over %q, want %q (%s)", input, got, want, qe.Message)
	}
	if !strings.Contains(qe.Message, msg) {
		t.Errorf("%q: message %q, want it to say %q", input, qe.Message, msg)
	}
}

func TestSaysWhereAMistakeIs(t *testing.T) {
	for _, c := range []struct{ in, over, msg string }{
		{`name = "drill`, `"drill`, "never closed"},
		{"(name = a OR b = c", "(name = a OR b = c", "never closed"},
		{"name = a)", ")", "closes nothing"},
		{"name = a b = c", "b", "expected AND, OR"},
		{"name =", "", "expected a value"},
		{"name = a AND", "", "expected something to search for"},
		{"AND name = a", "AND", "before AND"},
		{"condition IN ok", "ok", "list in parentheses"},
		{"loan IS FULL", "FULL", "EMPTY"},
		{"name ! a", "!", "write NOT"},
		{"name = a ORDER name", "ORDER", "BY"},
	} {
		_, err := Parse(c.in)
		at(t, c.in, err, c.over, c.msg)
	}
}

func TestCountsPositionsAsABrowserDoes(t *testing.T) {
	// 𝔸 is one character to a person and two UTF-16 code units, as a
	// browser's string counts it.
	_, err := Parse(`"𝔸" AND name = "x`)
	var qe *Error
	if !errors.As(err, &qe) || qe.Start != 16 || qe.End != 18 {
		t.Fatalf("got %+v, want the unclosed quote at 16 to 18", err)
	}
}

func choiceOptions(t *testing.T, choices ...string) json.RawMessage {
	t.Helper()
	var cs []models.SelectChoice
	for i := 0; i < len(choices); i += 2 {
		cs = append(cs, models.SelectChoice{Value: choices[i], Label: choices[i+1]})
	}
	raw, err := json.Marshal(models.SelectOptions{Choices: cs})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func schema(t *testing.T) Schema {
	return Schema{SignedIn: true, Fields: map[string]models.Field{
		"diameter": {Key: "diameter", ValidationType: "number"},
		"bought":   {Key: "bought", ValidationType: "date"},
		"serial":   {Key: "serial", ValidationType: "text"},
		"material": {Key: "material", ValidationType: "select", Options: choiceOptions(t, "tpu", "TPU (flexible)", "pla", "PLA")},
		"keeper":   {Key: "keeper", ValidationType: "person"},
	}}
}

var now = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

func asset(name string, fields map[string]string, keys ...string) models.AssetWithDetails {
	a := models.AssetWithDetails{Asset: models.Asset{ID: name, Name: name, Condition: models.ConditionOK, CreatedAt: now.AddDate(0, 0, -10)}, FieldValues: fields}
	for k := range fields {
		a.EffectiveFields = append(a.EffectiveFields, models.EffectiveField{Field: models.Field{Key: k}})
	}
	for i := 0; i < len(keys); i += 2 {
		a.GeneratorKeys = append(a.GeneratorKeys, models.AssetGeneratorKey{GeneratorType: keys[i], AssignedCode: keys[i+1]})
	}
	return a
}

func env() *Env {
	parent := "c-filament"
	return &Env{Now: now, Me: "u-me", Favorites: map[string]bool{"Spool": true}, Categories: map[string]models.Category{
		"c-filament": {ID: "c-filament", Name: "Filament"},
		"c-flexible": {ID: "c-flexible", Name: "Flexible", ParentID: &parent},
		"c-tools":    {ID: "c-tools", Name: "Tools"},
	}, Plain: func(a *models.AssetWithDetails, term string) bool {
		return strings.Contains(strings.ToLower(a.Asset.Name), strings.ToLower(term))
	}}
}

// found lists, in order, the assets a query finds among these.
func found(t *testing.T, q string, assets []models.AssetWithDetails) string {
	t.Helper()
	p, err := Compile(q, schema(t))
	if err != nil {
		t.Fatalf("Compile(%q): %v", q, err)
	}
	e := env()
	var kept []models.AssetWithDetails
	for i := range assets {
		if p.Match(&assets[i], e) {
			kept = append(kept, assets[i])
		}
	}
	p.Sort(kept, e)
	var names []string
	for _, a := range kept {
		names = append(names, a.Asset.Name)
	}
	return strings.Join(names, ",")
}

func shelf() []models.AssetWithDetails {
	spool := asset("Spool", map[string]string{"diameter": "1.75", "material": "tpu", "bought": "2026-11-01", "keeper": "u-me"}, "qr", "SP-001", "nfc", "04:A2:3B:1C")
	spool.Categories = []models.Category{{ID: "c-flexible", Name: "Flexible"}}
	spool.Location = []models.LinkedAsset{{Name: "Shop"}, {Name: "Shelf A"}}
	spool.People = map[string]models.FieldPerson{"u-me": {ID: "u-me", Name: "Ada Lovelace"}}
	spool.PersonFields = map[string]bool{"keeper": false}
	thick := asset("Thick spool", map[string]string{"diameter": "2.85", "material": "pla", "bought": "2025-01-10"}, "qr", "SP-002")
	thick.Categories = []models.Category{{ID: "c-filament", Name: "Filament"}}
	thick.Asset.Stock = &models.AssetStock{Quantity: 3}
	drill := asset("Drill", map[string]string{"serial": "DR-9"}, "code128", "HS-105")
	drill.Categories = []models.Category{{ID: "c-tools", Name: "Tools"}}
	drill.Asset.Condition = models.ConditionOutOfService
	due := now.Add(-time.Hour)
	drill.Loan = &models.Loan{DueAt: &due}
	return []models.AssetWithDetails{spool, thick, drill}
}

func TestMatchesByEachKindOfKey(t *testing.T) {
	for q, want := range map[string]string{
		"name = drill":                         "Drill",
		"name ~ SPOOL":                         "Spool,Thick spool",
		"name !~ spool":                        "Drill",
		"code = hs-105":                        "Drill",
		"code.qr ~ SP":                         "Spool,Thick spool",
		"code.nfc = 04a23b1c":                  "Spool",
		"category = filament":                  "Spool,Thick spool",
		`category = "Filament > Flexible"`:     "Spool",
		"category != Tools":                    "Spool,Thick spool",
		"in = 'shelf a'":                       "Spool",
		"in IS EMPTY":                          "Thick spool,Drill",
		"condition = 'out of service'":         "Drill",
		"condition = problem":                  "Drill",
		"condition IN (ok, needs_maintenance)": "Spool,Thick spool",
		"loan = overdue":                       "Drill",
		"loan IS EMPTY":                        "Spool,Thick spool",
		"assigned = me":                        "Spool",
		"assigned ~ lovelace":                  "Spool",
		"assigned IS EMPTY":                    "Thick spool,Drill",
		"favorite = true":                      "Spool",
		"quantity >= 3":                        "Thick spool",
		"quantity IS EMPTY":                    "Spool,Drill",
		"field.diameter >= 1.75":               "Spool,Thick spool",
		"field.diameter < 2":                   "Spool",
		"field.diameter != 1.75":               "Thick spool,Drill",
		"field.material = 'tpu (flexible)'":    "Spool",
		"field.material = PLA":                 "Thick spool",
		"field.material ~ flex":                "Spool",
		"field.serial IS NOT EMPTY":            "Drill",
		"field.keeper = me":                    "Spool",
		"field.bought = 2026-11-01":            "Spool",
		"field.bought < 2026-01-01":            "Thick spool",
		"field.bought > -30d":                  "Spool",
		"field.bought <= 2026-11-01":           "Spool,Thick spool",
		"field.bought >= 2026-11-02":           "",
		"created = -10d":                       "Spool,Thick spool,Drill",
		"created > -1w":                        "",
		"spool AND NOT thick":                  "Spool",
		"drill OR field.diameter > 2":          "Thick spool,Drill",
	} {
		if got := found(t, q, shelf()); got != want {
			t.Errorf("%s found %q, want %q", q, got, want)
		}
	}
}

func TestSortsWithMissingValuesLast(t *testing.T) {
	for q, want := range map[string]string{
		"ORDER BY name":                  "Drill,Spool,Thick spool",
		"ORDER BY name DESC":             "Thick spool,Spool,Drill",
		"ORDER BY field.diameter DESC":   "Thick spool,Spool,Drill",
		"ORDER BY field.diameter":        "Spool,Thick spool,Drill",
		"ORDER BY field.bought, name":    "Thick spool,Spool,Drill",
		"ORDER BY quantity DESC":         "Thick spool,Spool,Drill",
		"name ~ spool ORDER BY category": "Thick spool,Spool",
	} {
		if got := found(t, q, shelf()); got != want {
			t.Errorf("%s gave %q, want %q", q, got, want)
		}
	}
}

func TestRefusesWhatCannotBeCompared(t *testing.T) {
	for _, c := range []struct{ in, over, msg string }{
		{"colour = red", "colour", "nothing called"},
		{"field.nope = 1", "field.nope", `no field has the key "nope"`},
		{"code.ean = 1", "code.ean", "no code type"},
		{"field.diameter = thick", "thick", "isn't one"},
		{"field.diameter ~ 1", "field.diameter ~ 1", "is a number"},
		{"name < b", "name < b", "is text"},
		{"field.bought > someday", "someday", "isn't a date"},
		{"field.bought > -3x", "-3x", "end it with h, d, w, m or y"},
		{"field.material = abs", "abs", "TPU (flexible), PLA"},
		{"condition = broken", "broken", "is one of needs_maintenance, ok, out_of_service, problem"},
		{"favorite = maybe", "maybe", "true or false"},
		{"category IS EMPTY", "category IS EMPTY", "never empty"},
		{"ORDER BY favorite", "favorite", "can't be sorted"},
	} {
		_, err := Compile(c.in, schema(t))
		at(t, c.in, err, c.over, c.msg)
	}
}

func TestNeedsSomeoneForMeAndFavorites(t *testing.T) {
	anon := schema(t)
	anon.SignedIn = false
	for _, c := range []struct{ in, over string }{
		{"assigned = me", "me"},
		{"favorite = true", "favorite"},
	} {
		_, err := Compile(c.in, anon)
		at(t, c.in, err, c.over, "signed in")
	}
	// Quoted, me is only a name.
	if _, err := Compile(`assigned = "me"`, anon); err != nil {
		t.Errorf("a quoted me: %v", err)
	}
}

func TestDoesNotSearchFieldsAnAssetHasLeft(t *testing.T) {
	// A value kept from a category the asset has left is not its field.
	a := asset("Left", nil)
	a.FieldValues = map[string]string{"serial": "OLD-1"}
	if got := found(t, "field.serial = old-1", []models.AssetWithDetails{a}); got != "" {
		t.Fatalf("found %q by a field it no longer has", got)
	}
}

func TestNotesArchivedAndTerms(t *testing.T) {
	p, err := Compile(`"shelf a" OR archived = true OR drill`, schema(t))
	if err != nil {
		t.Fatal(err)
	}
	if !p.Archived || strings.Join(p.Terms, "|") != "shelf a|drill" {
		t.Fatalf("archived %v, terms %q", p.Archived, p.Terms)
	}
}

func TestDoubleTildeMatchesThroughRelatedTermsAndDespiteTypos(t *testing.T) {
	g := terms.New([]models.Term{
		{ID: "flex", Label: "Flexible filament"},
		{ID: "tpu", Label: "TPU", AltLabels: []string{"thermoplastic polyurethane"}},
		{ID: "tool", Label: "Tool"},
		{ID: "drill", Label: "Drill"},
	}, []models.TermLink{
		{TermID: "tpu", OtherID: "flex", Relation: models.TermBroader},
		{TermID: "drill", OtherID: "tool", Relation: models.TermBroader},
	})
	assets := shelf()
	assets[0].Asset.Name = "TPU spool"
	for q, want := range map[string]struct{ names, why string }{
		// As typed, it finds what ~ does, with no reason to give.
		`name ~~ spool`: {"TPU spool,Thick spool", ""},
		// Through a narrower term.
		`name ~~ "flexible filament"`: {"TPU spool", "TPU, narrower than Flexible filament"},
		// Through another name.
		`name ~~ "thermoplastic polyurethane"`: {"TPU spool", "TPU, another name for thermoplastic polyurethane"},
		// Broader terms aren't searched: TPU doesn't find flexible filament's other things.
		`name ~~ tpu`: {"TPU spool", ""},
		// Despite a typing mistake.
		`name ~~ dirll`:                   {"Drill", "drill, close to dirll"},
		`category ~~ tool`:                {"Drill", ""},
		`category ~~ fillament`:           {"TPU spool,Thick spool", "filament, close to fillament;filament, close to fillament"},
		`NOT name ~~ "flexible filament"`: {"Thick spool,Drill", ""},
		// ~ stays exact.
		`name ~ "flexible filament"`: {"", ""},
	} {
		p, err := Compile(q, schema(t))
		if err != nil {
			t.Fatalf("Compile(%q): %v", q, err)
		}
		e := env()
		e.Terms, e.Why = g, map[string]string{}
		var names, why []string
		for i := range assets {
			if p.Match(&assets[i], e) {
				names = append(names, assets[i].Asset.Name)
				if w := e.Why[assets[i].Asset.ID]; w != "" {
					why = append(why, w)
				}
			}
		}
		if got := strings.Join(names, ","); got != want.names {
			t.Errorf("%s found %q, want %q", q, got, want.names)
		}
		if got := strings.Join(why, ";"); got != want.why {
			t.Errorf("%s said why %q, want %q", q, got, want.why)
		}
	}
	// Without terms, ~~ still forgives typing mistakes.
	if got := found(t, "name ~~ dirll", shelf()); got != "Drill" {
		t.Errorf("without terms: %q", got)
	}
	_, err := Compile("field.diameter ~~ 2", schema(t))
	at(t, "field.diameter ~~ 2", err, "field.diameter ~~ 2", "compare it with")
}
