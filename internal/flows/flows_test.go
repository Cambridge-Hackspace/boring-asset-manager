package flows

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

var catalog = Catalog{
	Categories:   map[string]bool{"short": true, "long": true, "other": true},
	Fields:       map[string]bool{"owner": true, "until": true, "length": true, "wood": true},
	DateFields:   map[string]bool{"until": true},
	Templates:    map[string]bool{"tpl": true},
	Destinations: map[string]bool{"printer:p1": true, "pool:q1": true},
	Hooks:        map[string]bool{"identify": true},
	Asset:        func(id string) bool { return id == "shelf" },
	Names:        map[string]string{"short": "Short boards", "long": "Long boards"},
	Labels:       map[string]string{"owner": "Owner", "until": "Until", "length": "Length", "wood": "Wood"},
	// Both kinds of board require an owner; long boards, a date too.
	Required: map[string][]string{"short": {"owner"}, "long": {"owner", "until"}},
	Suits: func(field, value string) error {
		switch field {
		case "until":
			if _, err := time.Parse("2006-01-02", value); err != nil {
				return errors.New("not a date")
			}
		case "wood":
			if value != "oak" && value != "pine" {
				return errors.New("not one of its choices")
			}
		}
		return nil
	},
}

// boardFields are the fields each kind of board records, as the store
// gives them.
func boardFields(id string) ([]FieldDef, error) {
	owner := FieldDef{Key: "owner", Label: "Owner", Type: "text", Required: true}
	until := FieldDef{Key: "until", Label: "Until", Type: "date", Required: true}
	switch id {
	case "short":
		return []FieldDef{owner}, nil
	case "long":
		return []FieldDef{owner, until}, nil
	}
	return nil, nil
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
		starters, err := Starters(cats, boardFields)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range starters {
			if s.Definition.Version != Version {
				t.Errorf("%s over %v is version %d", s.Key, cats, s.Definition.Version)
			}
			if got := problems(s.Definition); len(got) != 0 {
				t.Errorf("%s over %v: %v", s.Key, cats, got)
			}
		}
	}
	// Leave something: Choose category, Form, Set fields, Create, Label,
	// Done; with one category there is nothing to choose.
	starters, _ := Starters([]string{"short", "long"}, boardFields)
	var kinds []string
	for _, s := range starters[0].Definition.Steps {
		kinds = append(kinds, s.Kind)
	}
	if want := []string{KindChooseCategory, KindForm, KindSetFields, KindCreate, KindLabel, KindDone}; !reflect.DeepEqual(kinds, want) {
		t.Errorf("leave something: %v", kinds)
	}
	if s := starters[0].Definition.Steps; s[3].Name != "{name}" || s[1].Questions[0].Key != "name" || s[1].Questions[0].Label != "What is it?" {
		t.Errorf("leave something asks %+v and files %q", s[1].Questions, s[3].Name)
	}
	one, _ := Starters([]string{"short"}, boardFields)
	if k := one[0].Definition.Steps[0].Kind; k != KindForm {
		t.Errorf("leave, one category, starts with %s", k)
	}
	// Collect it is as it was.
	if s := one[1].Definition.Steps; len(s) != 4 || s[2].Changes[0].Kind != ChangeArchive {
		t.Errorf("collect it: %+v", s)
	}
}

func TestCheckFindsWhatStopsAFlow(t *testing.T) {
	base := func(steps ...Step) Definition {
		return Definition{Version: Version, CategoryIDs: []string{"short", "long"}, Steps: append(steps, Step{ID: "done", Kind: KindDone})}
	}
	choose := Step{ID: "kind", Kind: KindChooseCategory}
	form := Step{ID: "about", Kind: KindForm, Questions: []Question{{Key: "what", Label: "What is it?"}}}
	find := Step{ID: "f", Kind: KindFind, FindBy: []string{FindList}}
	create := Step{ID: "file", Kind: KindCreate, Name: "{what}"}
	owner := FieldValue{Field: "owner", Value: "{user}"}
	until := FieldValue{Field: "until", Value: "{today}"}
	long := &Condition{Value: ValueCategoryID, Op: OpIs, To: "long"}
	cases := []struct {
		name string
		def  Definition
		want string
	}{
		{"no categories", Definition{Version: Version, Steps: []Step{{ID: "done", Kind: KindDone}}}, "at least one category"},
		{"unknown category", Definition{Version: Version, CategoryIDs: []string{"nope"}, Steps: []Step{{ID: "done", Kind: KindDone}}}, `no category has the ID "nope"`},
		{"version 1", Definition{Version: 1, CategoryIDs: []string{"short"}, Steps: []Step{{ID: "done", Kind: KindDone}}}, "Set fields step (set_fields)"},
		{"a newer version", Definition{Version: 3, CategoryIDs: []string{"short"}, Steps: []Step{{ID: "done", Kind: KindDone}}}, "version 3"},
		{"no done", Definition{Version: Version, CategoryIDs: []string{"short"}, Steps: []Step{{ID: "c", Kind: KindConfirm}}}, "ends with a Done step"},
		{"done not last", base(Step{ID: "early", Kind: KindDone}), "Done is the last step"},
		{"bad id", base(Step{ID: "Not OK", Kind: KindConfirm}), "a step's ID"},
		{"twice the id", base(Step{ID: "c", Kind: KindConfirm}, Step{ID: "c", Kind: KindConfirm}), `another step has the ID "c"`},
		{"unknown kind", base(Step{ID: "x", Kind: "dance"}), `"dance" is not a kind of step`},
		{"a create before the category", base(form, create), "it needs a category"},
		{"a question named as a value every run has", base(Step{ID: "f", Kind: KindForm, Questions: []Question{{Key: "user", Label: "Who?"}}}), "has the name of a value"},
		{"a question asked twice", base(Step{ID: "f", Kind: KindForm, Questions: []Question{{Key: "what", Label: "What?"}, {Key: "what", Label: "And?"}}}), `"what" is asked twice`},
		{"a question of no type", base(Step{ID: "f", Kind: KindForm, Questions: []Question{{Key: "q", Label: "Q", Type: "colour"}}}), "of a type there is not"},
		{"a choice with nothing to choose", base(Step{ID: "f", Kind: KindForm, Questions: []Question{{Key: "q", Label: "Q", Type: QuestionChoice}}}), "needs choices"},
		{"a choice offered twice", base(Step{ID: "f", Kind: KindForm, Questions: []Question{{Key: "q", Label: "Q", Type: QuestionChoice, Options: []Option{{Value: "a"}, {Value: "a"}}}}}), `offers "a" twice`},
		{"a default that does not suit", base(Step{ID: "f", Kind: KindForm, Questions: []Question{{Key: "q", Label: "Q", Type: QuestionYesNo, Default: "maybe"}}}), "is yes or no"},
		{"a template naming a later value", base(choose, Step{ID: "c", Kind: KindConfirm, Text: "Leave {what}?"}, form), "names {what}, which no earlier step sets"},
		{"a condition on a later value", base(choose, Step{ID: "c", Kind: KindConfirm, When: &Condition{Value: "what", Op: OpSet}}, form), `names "what"`},
		{"a condition on a category not used", base(choose, Step{ID: "c", Kind: KindConfirm, When: &Condition{Value: ValueCategoryID, Op: OpIs, To: "other"}}), "a category the flow does not use"},
		{"offering a category not used", base(Step{ID: "kind", Kind: KindChooseCategory, Categories: []string{"other"}}), "offers a category the flow does not use"},
		{"a label before an asset", base(Step{ID: "l", Kind: KindLabel, Label: &Label{Mode: LabelShow}}), "it needs an asset"},
		{"printing nowhere", base(find, Step{ID: "l", Kind: KindLabel, Label: &Label{Mode: LabelPrint, Destination: "printer:gone"}}), "does not exist"},
		{"a create with no name", base(choose, Step{ID: "c", Kind: KindCreate}), "needs a name"},
		{"a place that isn't", base(choose, Step{ID: "c", Kind: KindCreate, Name: "x", Location: &Location{Mode: PlaceFixed, AssetID: "nowhere"}}), "its place is not an asset"},
		{"find by nothing", base(Step{ID: "f", Kind: KindFind}), "a way to find"},
		{"an unknown change", base(find, Step{ID: "x", Kind: KindChange, Changes: []Change{{Kind: "burn"}}}), `"burn" is not a change`},
		{"a change setting fields", base(find, Step{ID: "x", Kind: KindChange, Changes: []Change{{Kind: "fields"}}}), "a Set fields step does"},
		{"an unknown hook", base(find, Step{ID: "x", Kind: KindChange, Changes: []Change{{Kind: ChangeHook, Hook: "dance"}}}), `no hook has the key "dance"`},

		// Set fields.
		{"an unknown field", base(find, Step{ID: "s", Kind: KindSetFields, Fields: []FieldValue{{Field: "colour", Value: "red"}}}), `"colour"`},
		{"a row with no field", base(find, Step{ID: "s", Kind: KindSetFields, Fields: []FieldValue{{Value: "red"}}}), "choose the field each row sets"},
		{"a field set twice", base(find, Step{ID: "s", Kind: KindSetFields, Fields: []FieldValue{owner, owner}}), "it sets Owner twice"},
		{"setting nothing", base(find, Step{ID: "s", Kind: KindSetFields}), "at least one field"},
		{"setting fields on nothing", base(Step{ID: "s", Kind: KindSetFields, Fields: []FieldValue{owner}}), "sets fields on nothing"},
		{"a value naming a later answer", base(find, Step{ID: "s", Kind: KindSetFields, Fields: []FieldValue{{Field: "owner", Value: "{what}"}}}, form), "names {what}, which no earlier step sets"},
		{"a date that is not one", base(find, Step{ID: "s", Kind: KindSetFields, Fields: []FieldValue{{Field: "until", Value: "Friday"}}}), `Until cannot be "Friday": not a date`},
		{"a choice that is not one", base(find, Step{ID: "s", Kind: KindSetFields, Fields: []FieldValue{{Field: "wood", Value: "teak"}}}), `Wood cannot be "teak"`},
		{"a photo that is no photo", base(form, find, Step{ID: "s", Kind: KindSetFields, Photo: "{what}"}), "a photo question asked before it"},
		{"a required field left unset", base(choose, form, Step{ID: "s", Kind: KindSetFields, Fields: []FieldValue{owner}}, create),
			"step file: Long boards requires Until, which no Set fields step before it sets"},
		{"a required field set for another category", base(choose, form,
			Step{ID: "s", Kind: KindSetFields, Fields: []FieldValue{owner}},
			Step{ID: "s2", Kind: KindSetFields, Fields: []FieldValue{until}, When: &Condition{Value: ValueCategoryID, Op: OpIs, To: "short"}}, create),
			"Long boards requires Until"},
		{"a required field set on a condition that is not the category", base(choose, form,
			Step{ID: "s", Kind: KindSetFields, Fields: []FieldValue{owner, until}, When: &Condition{Value: "what", Op: OpSet}}, create),
			"Short boards requires Owner"},
		{"a required field set after the create", base(choose, form, create, Step{ID: "s", Kind: KindSetFields, Fields: []FieldValue{owner, until}}), "requires Owner, Until"},
	}
	for _, c := range cases {
		got := strings.Join(problems(c.def), "; ")
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: %q, want it to say %q", c.name, got, c.want)
		}
	}

	// A required field set by a step shown only for its category counts
	// for that category; a Create that only files short boards needs only
	// theirs.
	ok := []Definition{
		base(choose, form, Step{ID: "s", Kind: KindSetFields, Fields: []FieldValue{owner}},
			Step{ID: "s2", Kind: KindSetFields, Fields: []FieldValue{until}, When: long}, create),
		base(Step{ID: "kind", Kind: KindChooseCategory, Categories: []string{"short"}}, form, Step{ID: "s", Kind: KindSetFields, Fields: []FieldValue{owner}}, create),
		base(choose, form, Step{ID: "s", Kind: KindSetFields, Fields: []FieldValue{owner}},
			Step{ID: "file", Kind: KindCreate, Name: "{what}", When: &Condition{Value: ValueCategoryID, Op: OpIs, To: "short"}}),
	}
	for i, d := range ok {
		if got := problems(d); len(got) != 0 {
			t.Errorf("set by category %d: %v", i, got)
		}
	}

	// A full flow using every kind passes.
	full := base(
		Step{ID: "kind", Kind: KindChooseCategory},
		Step{ID: "about", Kind: KindForm, Questions: []Question{
			{Key: "name", Label: "What is it?", Required: true},
			{Key: "owner", Label: "Whose is it?", Default: "{user}"},
			{Key: "notes", Label: "Anything else?", Type: QuestionLongText},
			{Key: "boards", Label: "How many?", Type: QuestionNumber, Default: "1"},
			{Key: "leave_until", Label: "Leave until", Type: QuestionDate, Default: "{today}"},
			{Key: "fragile", Label: "Fragile?", Type: QuestionYesNo, Default: "no"},
			{Key: "kind_of_wood", Label: "Which wood?", Type: QuestionChoice, Options: []Option{{Value: "oak", Label: "Oak"}, {Value: "pine"}}, Default: "oak"},
			{Key: "photo", Label: "A photo", Type: QuestionPhoto},
		}},
		Step{ID: "long_only", Kind: KindConfirm, Text: "{owner}: {name}", When: long},
		Step{ID: "set", Kind: KindSetFields, Fields: []FieldValue{{Field: "owner", Value: "{owner}"}, {Field: "until", Value: "{leave_until}"}, {Field: "wood", Value: "{kind_of_wood}"}}, Photo: "{photo}"},
		Step{ID: "file", Kind: KindCreate, Name: "{owner}: {name}", Location: &Location{Mode: PlaceScan}},
		Step{ID: "label", Kind: KindLabel, Label: &Label{Mode: LabelPrint, Destination: "pool:q1", TemplateID: "tpl"}},
		Step{ID: "set_2", Kind: KindSetFields, Fields: []FieldValue{{Field: "length", Value: "{boards}"}, {Field: "wood", Value: "pine"}}},
		Step{ID: "change", Kind: KindChange, Changes: []Change{
			{Kind: ChangeCondition, Condition: "needs_maintenance", Note: "left by {owner}"},
			{Kind: ChangePlace, AssetID: "shelf"}, {Kind: ChangeCategory, CategoryID: "other"}, {Kind: ChangeHook, Hook: "identify"},
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
	// What each Set fields step sets, by where it is.
	if on := []string{full.SetsOn(3), full.SetsOn(6)}; !reflect.DeepEqual(on, []string{OnNew, OnFiled}) {
		t.Errorf("sets on %v", on)
	}
	if afterFind := base(find, Step{ID: "s", Kind: KindSetFields}); afterFind.SetsOn(1) != OnFound {
		t.Errorf("after a find, sets on %s", afterFind.SetsOn(1))
	}
	// Setting fields asks nothing.
	if s := (Step{Kind: KindSetFields}); s.Asks() || !s.Auto() {
		t.Error("Set fields asks something")
	}
}

// Each type of question takes the answers that suit it.
func TestAnswersSuitTheirQuestions(t *testing.T) {
	for _, c := range []struct {
		q      Question
		ok, no []string
	}{
		{Question{Type: QuestionText}, []string{"", "anything"}, nil},
		{Question{Type: QuestionLongText}, []string{"one\ntwo"}, nil},
		{Question{Type: QuestionNumber}, []string{"3", "2.5", "-1"}, []string{"three"}},
		{Question{Type: QuestionDate}, []string{"2026-10-30"}, []string{"Friday", "30/10/2026"}},
		{Question{Type: QuestionYesNo}, []string{"yes", "no"}, []string{"maybe", "Yes"}},
		{Question{Type: QuestionChoice, Options: []Option{{Value: "oak"}, {Value: "pine", Label: "Pine"}}}, []string{"oak", "pine"}, []string{"Pine", "teak"}},
		{Question{Type: QuestionPhoto}, []string{"board.jpg"}, nil},
	} {
		for _, a := range c.ok {
			if p := c.q.Problem(a); p != "" {
				t.Errorf("%s takes %q: %s", c.q.Type, a, p)
			}
		}
		for _, a := range c.no {
			if p := c.q.Problem(a); p == "" {
				t.Errorf("%s refuses %q", c.q.Type, a)
			}
		}
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
		{ID: "set", Kind: KindSetFields},
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
	if n := d.Ahead(6, Values{}); n != 0 {
		t.Errorf("ahead of the last: %d", n)
	}
}
