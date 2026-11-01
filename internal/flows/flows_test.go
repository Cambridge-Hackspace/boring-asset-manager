package flows

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

var catalog = Catalog{
	Categories:   map[string]bool{"short": true, "long": true, "other": true},
	Fields:       map[string]bool{"owner": true, "until": true, "length": true},
	Templates:    map[string]bool{"tpl": true},
	Destinations: map[string]bool{"printer:p1": true, "pool:q1": true},
	Hooks:        map[string]bool{"identify": true},
	Asset:        func(id string) bool { return id == "shelf" },
}

// problems is what Check finds, as one string per problem.
func problems(d Definition) []string {
	var out []string
	for _, p := range d.Check(catalog) {
		out = append(out, p.String())
	}
	return out
}

func TestStartersPassTheirOwnChecks(t *testing.T) {
	for _, cats := range [][]string{{"short", "long"}, {"short"}} {
		for _, s := range Starters(cats) {
			if got := problems(s.Definition); len(got) != 0 {
				t.Errorf("%s over %v: %v", s.Key, cats, got)
			}
		}
	}
	// With one category there is nothing to choose.
	if k := Starters([]string{"short"})[0].Definition.Steps[0].Kind; k != KindForm {
		t.Errorf("leave, one category, starts with %s", k)
	}
}

func TestCheckFindsWhatStopsAFlow(t *testing.T) {
	base := func(steps ...Step) Definition {
		return Definition{Version: Version, CategoryIDs: []string{"short", "long"}, Steps: append(steps, Step{ID: "done", Kind: KindDone})}
	}
	choose := Step{ID: "kind", Kind: KindChooseCategory}
	form := Step{ID: "about", Kind: KindForm, Questions: []Question{{Key: "what", Label: "What is it?"}}}
	cases := []struct {
		name string
		def  Definition
		want string
	}{
		{"no categories", Definition{Version: Version, Steps: []Step{{ID: "done", Kind: KindDone}}}, "at least one category"},
		{"unknown category", Definition{Version: Version, CategoryIDs: []string{"nope"}, Steps: []Step{{ID: "done", Kind: KindDone}}}, `no category has the ID "nope"`},
		{"a newer version", Definition{Version: 2, CategoryIDs: []string{"short"}, Steps: []Step{{ID: "done", Kind: KindDone}}}, "version 2"},
		{"no done", Definition{Version: Version, CategoryIDs: []string{"short"}, Steps: []Step{{ID: "c", Kind: KindConfirm}}}, "ends with a Done step"},
		{"done not last", base(Step{ID: "early", Kind: KindDone}), "Done is the last step"},
		{"bad id", base(Step{ID: "Not OK", Kind: KindConfirm}), "a step's ID"},
		{"twice the id", base(Step{ID: "c", Kind: KindConfirm}, Step{ID: "c", Kind: KindConfirm}), `another step has the ID "c"`},
		{"unknown kind", base(Step{ID: "x", Kind: "dance"}), `"dance" is not a kind of step`},
		{"a form before the category", base(form), "it needs a category"},
		{"an unknown field", base(choose, Step{ID: "f", Kind: KindForm, Fields: []FieldSetting{{Key: "colour"}}}), `"colour"`},
		{"a question named as a field", base(choose, Step{ID: "f", Kind: KindForm, Questions: []Question{{Key: "owner", Label: "Owner"}}}), "has the name of a field"},
		{"a template naming a later value", base(choose, Step{ID: "c", Kind: KindConfirm, Text: "Leave {what}?"}, form), "names {what}, which no earlier step sets"},
		{"a condition on a later value", base(choose, Step{ID: "c", Kind: KindConfirm, When: &Condition{Value: "what", Op: OpSet}}, form), `names "what"`},
		{"a condition on a category not used", base(choose, Step{ID: "c", Kind: KindConfirm, When: &Condition{Value: ValueCategoryID, Op: OpIs, To: "other"}}), "a category the flow does not use"},
		{"offering a category not used", base(Step{ID: "kind", Kind: KindChooseCategory, Categories: []string{"other"}}), "offers a category the flow does not use"},
		{"a label before an asset", base(Step{ID: "l", Kind: KindLabel, Label: &Label{Mode: LabelShow}}), "it needs an asset"},
		{"printing nowhere", base(Step{ID: "f", Kind: KindFind, FindBy: []string{FindList}}, Step{ID: "l", Kind: KindLabel, Label: &Label{Mode: LabelPrint, Destination: "printer:gone"}}), "does not exist"},
		{"a create with no name", base(choose, Step{ID: "c", Kind: KindCreate}), "needs a name"},
		{"a place that isn't", base(choose, Step{ID: "c", Kind: KindCreate, Name: "x", Location: &Location{Mode: PlaceFixed, AssetID: "nowhere"}}), "its place is not an asset"},
		{"find by nothing", base(Step{ID: "f", Kind: KindFind}), "a way to find"},
		{"an unknown change", base(Step{ID: "f", Kind: KindFind, FindBy: []string{FindList}}, Step{ID: "x", Kind: KindChange, Changes: []Change{{Kind: "burn"}}}), `"burn" is not a change`},
		{"an unknown hook", base(Step{ID: "f", Kind: KindFind, FindBy: []string{FindList}}, Step{ID: "x", Kind: KindChange, Changes: []Change{{Kind: ChangeHook, Hook: "dance"}}}), `no hook has the key "dance"`},
	}
	for _, c := range cases {
		got := strings.Join(problems(c.def), "; ")
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: %q, want it to say %q", c.name, got, c.want)
		}
	}

	// A full flow using every kind passes.
	full := base(
		Step{ID: "kind", Kind: KindChooseCategory},
		Step{ID: "about", Kind: KindForm, AskName: true, Fields: []FieldSetting{{Key: "owner", Required: true}, {Key: "until", Default: "{today}"}},
			Questions: []Question{{Key: "what", Label: "What is it?"}}},
		Step{ID: "long_only", Kind: KindConfirm, Text: "{owner}: {what}", When: &Condition{Value: ValueCategoryID, Op: OpIs, To: "long"}},
		Step{ID: "file", Kind: KindCreate, Name: "{owner}: {what}", Location: &Location{Mode: PlaceScan}},
		Step{ID: "label", Kind: KindLabel, Label: &Label{Mode: LabelPrint, Destination: "pool:q1", TemplateID: "tpl"}},
		Step{ID: "change", Kind: KindChange, Changes: []Change{
			{Kind: ChangeCondition, Condition: "needs_maintenance", Note: "left by {owner}"},
			{Kind: ChangePlace, AssetID: "shelf"}, {Kind: ChangeCategory, CategoryID: "other"},
			{Kind: ChangeFields, Fields: map[string]string{"length": "{what}"}}, {Kind: ChangeHook, Hook: "identify"},
			{Kind: ChangeBorrow}, {Kind: ChangeReturn}, {Kind: ChangeArchive},
		}},
	)
	if got := problems(full); len(got) != 0 {
		t.Errorf("a full flow: %v", got)
	}
	want := []string{rbac.PermAssetsBorrow, rbac.PermAssetsHook, rbac.PermAssetsDelete, rbac.PermAssetsReport, rbac.PermAssetsWrite, rbac.PermLabelsPrint}
	if got := full.Permissions(); !reflect.DeepEqual(got, sorted(want)) {
		t.Errorf("permissions: %v", got)
	}
}

func sorted(s []string) []string {
	out := append([]string{}, s...)
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func TestTemplatesAndConditions(t *testing.T) {
	v := Values{"owner": "Sam", "what": "oak"}
	if got := Render("{owner}: {what}", v); got != "Sam: oak" {
		t.Errorf("render: %q", got)
	}
	// A value not set renders empty; other braces are left as they are.
	if got := Render("{owner} until {until} {Not a ref}", v); got != "Sam until  {Not a ref}" {
		t.Errorf("render with a missing value: %q", got)
	}
	if got := References("{a} and {b_2}"); !reflect.DeepEqual(got, []string{"a", "b_2"}) {
		t.Errorf("references: %v", got)
	}
	for _, c := range []struct {
		cond *Condition
		want bool
	}{
		{nil, true},
		{&Condition{Value: "owner", Op: OpIs, To: "Sam"}, true},
		{&Condition{Value: "owner", Op: OpIsNot, To: "Sam"}, false},
		{&Condition{Value: "until", Op: OpSet}, false},
		{&Condition{Value: "until", Op: OpUnset}, true},
		{&Condition{Value: "owner", Op: "??"}, false},
	} {
		if got := c.cond.Holds(v); got != c.want {
			t.Errorf("%+v: %v", c.cond, got)
		}
	}
}

func TestNextSkipsStepsWhoseConditionFails(t *testing.T) {
	d := Definition{Steps: []Step{
		{ID: "a", Kind: KindConfirm},
		{ID: "b", Kind: KindConfirm, When: &Condition{Value: ValueCategoryID, Op: OpIs, To: "long"}},
		{ID: "c", Kind: KindDone},
	}}
	if got := d.Next(1, Values{ValueCategoryID: "short"}); got != 2 {
		t.Errorf("short skips b: %d", got)
	}
	if got := d.Next(1, Values{ValueCategoryID: "long"}); got != 1 {
		t.Errorf("long stops at b: %d", got)
	}
	if got := d.Next(3, nil); got != 3 {
		t.Errorf("past the end: %d", got)
	}
}

func TestStart(t *testing.T) {
	at := time.Date(2026, 10, 30, 9, 5, 0, 0, time.UTC)
	name := func(id string) string { return map[string]string{"short": "Short boards"}[id] }
	v := Start("Leave something", "Sam", at, &Definition{CategoryIDs: []string{"short"}}, name)
	want := Values{ValueToday: "2026-10-30", ValueNow: "2026-10-30 09:05", ValueUser: "Sam", ValueFlow: "Leave something", ValueCategory: "Short boards", ValueCategoryID: "short"}
	if !reflect.DeepEqual(v, want) {
		t.Errorf("start: %v", v)
	}
	if v := Start("x", "y", at, &Definition{CategoryIDs: []string{"short", "long"}}, name); v[ValueCategoryID] != "" {
		t.Errorf("two categories leave it to be chosen: %v", v)
	}
}

// Asks and Ahead count the screens a run shows (#166).
func TestAsksAndAhead(t *testing.T) {
	long := &Condition{Value: ValueCategoryID, Op: OpIs, To: "long"}
	d := Definition{Steps: []Step{
		{ID: "kind", Kind: KindChooseCategory},
		{ID: "fixed", Kind: KindChooseCategory, Fixed: "short"},
		{ID: "about", Kind: KindForm},
		{ID: "long_only", Kind: KindConfirm, When: long},
		{ID: "file", Kind: KindCreate},
		{ID: "put", Kind: KindCreate, Location: &Location{Mode: PlaceScan}},
		{ID: "label", Kind: KindLabel},
		{ID: "done", Kind: KindDone},
	}}
	var asks []string
	for i := range d.Steps {
		if d.Steps[i].Asks() {
			asks = append(asks, d.Steps[i].ID)
		}
	}
	if want := []string{"kind", "about", "long_only", "put"}; !reflect.DeepEqual(asks, want) {
		t.Errorf("asks %v, want %v", asks, want)
	}
	if n := d.Ahead(0, Values{}); n != 2 {
		t.Errorf("ahead of kind, no category: %d", n)
	}
	if n := d.Ahead(0, Values{ValueCategoryID: "long"}); n != 3 {
		t.Errorf("ahead of kind, long: %d", n)
	}
	if n := d.Ahead(5, Values{}); n != 0 {
		t.Errorf("ahead of the last: %d", n)
	}
}
