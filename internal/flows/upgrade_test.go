package flows

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// upgraded converts a version 1 definition, failing the test if it was not
// converted or does not read as version 2.
func upgraded(t *testing.T, v1 string, fieldsOf func(string) ([]FieldDef, error)) Definition {
	t.Helper()
	out, changed, err := UpgradeV1(v1, fieldsOf)
	if err != nil || !changed {
		t.Fatalf("converting: %v %v", changed, err)
	}
	var d Definition
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatal(err)
	}
	if d.Version != Version {
		t.Errorf("version %d", d.Version)
	}
	return d
}

func kindsOf(d Definition) string {
	var out []string
	for _, s := range d.Steps {
		out = append(out, s.ID+":"+s.Kind)
	}
	return strings.Join(out, " ")
}

// shedFields are a shed's: long boards record an owner, required, a date
// and a kind of wood to choose, and a note, all inherited from wood but
// the date; short boards, the owner and the note.
func shedFields(id string) ([]FieldDef, error) {
	owner := FieldDef{Key: "owner", Label: "Owner", Type: "text", Required: true}
	note := FieldDef{Key: "note", Label: "Note", Type: "text"}
	switch id {
	case "long":
		return []FieldDef{
			{Key: "length", Label: "Length", Type: "number"},
			note, owner,
			{Key: "until", Label: "Until", Type: "date", Required: true},
			{Key: "wood", Label: "Wood", Type: "select", Choices: []Option{{Value: "oak", Label: "Oak"}, {Value: "pine", Label: "Pine"}}, Default: "pine"},
		}, nil
	case "short":
		return []FieldDef{note, owner}, nil
	}
	return nil, nil
}

// A Form's asked fields become its own questions, as they were asked, and
// a Set fields step before Create sets each from its answer.
func TestUpgradingAskedFields(t *testing.T) {
	d := upgraded(t, `{"version":1,"description":"Leave it","category_ids":["long"],"leave_until":"until","steps":[
		{"id":"about","kind":"form","title":"About it","fields":[
			{"key":"until","label":"Leave until","help":"When you'll take it","default":"{today}"},
			{"key":"note","required":true}]},
		{"id":"file","kind":"create","name":"{owner}'s board"},
		{"id":"done","kind":"done"}]}`, shedFields)
	if got := kindsOf(d); got != "about:form set:set_fields file:create done:done" {
		t.Fatalf("steps: %s", got)
	}
	if d.Description != "Leave it" || d.LeaveUntil != "until" || d.Steps[0].Title != "About it" {
		t.Errorf("kept: %+v", d)
	}
	want := []Question{
		{Key: "until", Label: "Leave until", Help: "When you'll take it", Type: QuestionDate, Default: "{today}", Required: true},
		{Key: "note", Label: "Note", Type: QuestionText, Required: true},
		{Key: "length", Label: "Length", Type: QuestionNumber},
		{Key: "owner", Label: "Owner", Type: QuestionText, Required: true},
		{Key: "wood", Label: "Wood", Type: QuestionChoice, Options: []Option{{Value: "oak", Label: "Oak"}, {Value: "pine", Label: "Pine"}}, Default: "pine"},
	}
	if !reflect.DeepEqual(d.Steps[0].Questions, want) {
		t.Errorf("questions:\n%+v\nwant\n%+v", d.Steps[0].Questions, want)
	}
	rows := []FieldValue{{"until", "{until}"}, {"note", "{note}"}, {"length", "{length}"}, {"owner", "{owner}"}, {"wood", "{wood}"}}
	if !reflect.DeepEqual(d.Steps[1].Fields, rows) || d.Steps[1].Photo != "" {
		t.Errorf("set fields: %+v", d.Steps[1])
	}
	if d.Steps[2].Name != "{owner}'s board" {
		t.Errorf("create: %+v", d.Steps[2])
	}
}

// A field hidden with a default is set to it directly; one hidden with
// none is left alone; one hidden that the category requires is asked
// still, as version 1 asked it.
func TestUpgradingHiddenFields(t *testing.T) {
	d := upgraded(t, `{"version":1,"category_ids":["long"],"steps":[
		{"id":"about","kind":"form","fields":[
			{"key":"note","hidden":true,"default":"Left by {user}"},
			{"key":"length","hidden":true},
			{"key":"owner","hidden":true,"default":"{user}"},
			{"key":"wood","hidden":true,"default":"oak"}]},
		{"id":"file","kind":"create","name":"Board"},
		{"id":"done","kind":"done"}]}`, shedFields)
	var keys []string
	for _, q := range d.Steps[0].Questions {
		keys = append(keys, q.Key)
	}
	if !reflect.DeepEqual(keys, []string{"owner", "until"}) || d.Steps[0].Questions[0].Default != "{user}" {
		t.Errorf("asked: %+v", d.Steps[0].Questions)
	}
	rows := []FieldValue{{"owner", "{owner}"}, {"until", "{until}"}, {"note", "Left by {user}"}, {"wood", "oak"}}
	if !reflect.DeepEqual(d.Steps[1].Fields, rows) {
		t.Errorf("set fields: %+v", d.Steps[1].Fields)
	}
}

// Asking for the name and a photo become questions of their own, {name}
// first and {photo} last, and the photo is set on the asset filed.
func TestUpgradingNameAndPhoto(t *testing.T) {
	d := upgraded(t, `{"version":1,"category_ids":["short"],"steps":[
		{"id":"about","kind":"form","ask_name":true,"name_label":"What is it?","ask_photo":true,
			"fields":[{"key":"owner","default":"{user}"}],
			"questions":[{"key":"what","label":"Which?","type":"number"}]},
		{"id":"file","kind":"create","name":"{name}"},
		{"id":"done","kind":"done"}]}`, shedFields)
	var got []string
	for _, q := range d.Steps[0].Questions {
		got = append(got, q.Key+":"+q.Type+":"+q.Label)
	}
	if want := []string{"name:text:What is it?", "owner:text:Owner", "note:text:Note", "what:number:Which?", "photo:photo:A photo"}; !reflect.DeepEqual(got, want) {
		t.Errorf("questions: %v", got)
	}
	if !d.Steps[0].Questions[0].Required {
		t.Error("its name is not required")
	}
	if s := d.Steps[1]; s.Kind != KindSetFields || s.Photo != "{photo}" || len(s.Fields) != 2 {
		t.Errorf("set fields: %+v", s)
	}
	// A photo alone still gets its Set fields step; a name alone needs
	// none, the name being Create's.
	d = upgraded(t, `{"version":1,"category_ids":["none"],"steps":[{"id":"about","kind":"form","ask_photo":true},{"id":"file","kind":"create","name":"x"},{"id":"done","kind":"done"}]}`, shedFields)
	if got := kindsOf(d); got != "about:form set:set_fields file:create done:done" || d.Steps[1].Photo != "{photo}" {
		t.Errorf("a photo alone: %s %+v", got, d.Steps[1])
	}
	d = upgraded(t, `{"version":1,"category_ids":["none"],"steps":[{"id":"about","kind":"form","ask_name":true},{"id":"file","kind":"create","name":"{name}"},{"id":"done","kind":"done"}]}`, nil)
	if got := kindsOf(d); got != "about:form file:create done:done" {
		t.Errorf("a name alone: %s", got)
	}
}

// Change's fields become a Set fields step where they were, the Change
// split around it, with IDs as the editor gives them.
func TestUpgradingChangesFields(t *testing.T) {
	d := upgraded(t, `{"version":1,"category_ids":["short"],"steps":[
		{"id":"find","kind":"find","find_by":["list"]},
		{"id":"change","kind":"change","title":"Finishing","when":{"value":"owner","op":"set"},"changes":[
			{"kind":"condition","condition":"needs_maintenance","note":"by {user}"},
			{"kind":"fields","fields":{"owner":"","note":"Collected {today}"}},
			{"kind":"hook","hook":"identify"},
			{"kind":"archive"}]},
		{"id":"tidy","kind":"change","changes":[{"kind":"fields","fields":{"note":"gone"}}]},
		{"id":"done","kind":"done"}]}`, shedFields)
	if got := kindsOf(d); got != "find:find change:change set:set_fields change_2:change tidy:set_fields done:done" {
		t.Fatalf("steps: %s", got)
	}
	when := &Condition{Value: "owner", Op: OpSet}
	if s := d.Steps[1]; !reflect.DeepEqual(s.Changes, []Change{{Kind: ChangeCondition, Condition: "needs_maintenance", Note: "by {user}"}}) || !reflect.DeepEqual(s.When, when) || s.Title != "Finishing" {
		t.Errorf("before: %+v", s)
	}
	if s := d.Steps[2]; !reflect.DeepEqual(s.Fields, []FieldValue{{"note", "Collected {today}"}, {"owner", ""}}) || !reflect.DeepEqual(s.When, when) {
		t.Errorf("the fields: %+v", s)
	}
	if s := d.Steps[3]; !reflect.DeepEqual(s.Changes, []Change{{Kind: ChangeHook, Hook: "identify"}, {Kind: ChangeArchive}}) || !reflect.DeepEqual(s.When, when) {
		t.Errorf("after: %+v", s)
	}
	if s := d.Steps[4]; !reflect.DeepEqual(s.Fields, []FieldValue{{"note", "gone"}}) {
		t.Errorf("fields alone: %+v", s)
	}
}

// A form asked of several categories asks what any of them records,
// required only where all require it; a field hidden with a default by a
// form shown on a condition is set on that condition.
func TestUpgradingAcrossCategories(t *testing.T) {
	d := upgraded(t, `{"version":1,"category_ids":["short","long"],"steps":[
		{"id":"kind","kind":"choose_category"},
		{"id":"about","kind":"form"},
		{"id":"long_only","kind":"form","when":{"value":"category_id","op":"is","to":"long"},"fields":[{"key":"wood","hidden":true,"default":"oak"}]},
		{"id":"file","kind":"create","name":"{owner}"},
		{"id":"file_again","kind":"create","name":"{owner} 2"},
		{"id":"done","kind":"done"}]}`, shedFields)
	if got := kindsOf(d); got != "kind:choose_category about:form long_only:form set:set_fields set_2:set_fields file:create set_3:set_fields set_4:set_fields file_again:create done:done" {
		t.Fatalf("steps: %s", got)
	}
	req := map[string]bool{}
	for _, q := range d.Steps[1].Questions {
		req[q.Key] = q.Required
	}
	if want := map[string]bool{"length": false, "note": false, "owner": true, "until": false, "wood": false}; !reflect.DeepEqual(req, want) {
		t.Errorf("required: %v", req)
	}
	if len(d.Steps[2].Questions) != 4 {
		t.Errorf("the long boards' form: %+v", d.Steps[2].Questions)
	}
	long := &Condition{Value: ValueCategoryID, Op: OpIs, To: "long"}
	if s := d.Steps[4]; !reflect.DeepEqual(s.When, long) || !reflect.DeepEqual(s.Fields, []FieldValue{{"wood", "oak"}}) {
		t.Errorf("the hidden default: %+v", s)
	}
	// Each Create files the answers, as version 1 did.
	if !reflect.DeepEqual(d.Steps[3].Fields, d.Steps[6].Fields) || len(d.Steps[3].Fields) != 5 {
		t.Errorf("the second create's: %+v", d.Steps[6])
	}
	// It passes saving: each category's required fields are set.
	c := catalog
	c.Fields = map[string]bool{"owner": true, "until": true, "length": true, "wood": true, "note": true}
	for _, p := range d.Check(c) {
		t.Errorf("converted: %s", p)
	}
}

// What is version 2 already is left as it is; what is not JSON is an
// error.
func TestUpgradingLeavesVersion2(t *testing.T) {
	v2 := `{"version":2,"category_ids":["short"],"steps":[{"id":"done","kind":"done"}]}`
	if out, changed, err := UpgradeV1(v2, shedFields); err != nil || changed || out != v2 {
		t.Errorf("version 2: %v %v %s", changed, err, out)
	}
	if _, _, err := UpgradeV1("{", shedFields); err == nil {
		t.Error("not JSON")
	}
}

// The corpus works out a category's fields as the store does: own and
// inherited, required where any level requires it, in key order.
func TestFieldCorpus(t *testing.T) {
	c := NewFieldCorpus()
	c.AddField("f1", "owner", "Owner", "text", "")
	c.AddField("f2", "wood", "Wood", "select", `{"choices":[{"label":"Oak","value":"oak"}],"default":"oak"}`)
	c.AddField("f3", "until", "Until", "date", "null")
	c.Parents["long"] = "wood"
	c.Assigned["wood"] = []FieldAssignment{{FieldID: "f1", Required: true}, {FieldID: "f2"}}
	c.Assigned["long"] = []FieldAssignment{{FieldID: "f3"}, {FieldID: "f2", Required: true}, {FieldID: "gone"}}
	got, err := c.Effective("long")
	if err != nil {
		t.Fatal(err)
	}
	want := []FieldDef{
		{Key: "owner", Label: "Owner", Type: "text", Required: true},
		{Key: "until", Label: "Until", Type: "date"},
		{Key: "wood", Label: "Wood", Type: "select", Choices: []Option{{Value: "oak", Label: "Oak"}}, Default: "oak", Required: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("long:\n%+v\nwant\n%+v", got, want)
	}
	// A loop in the tree ends.
	c.Parents["wood"] = "long"
	if got, _ := c.Effective("wood"); len(got) != 3 {
		t.Errorf("a loop: %+v", got)
	}
}
