package flows

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
)

// Converting version 1 definitions (#174). In version 1 a Form asked for
// the fields the chosen category records, with the creator's settings on
// top, and for the asset's name and a photo, and Create filed the answers;
// a Change could set fields. In version 2 a Form asks only its own
// questions, and a Set fields step puts values into fields. UpgradeV1 is
// the one conversion: migration 050 runs it over every saved flow, an
// archive written before it runs it over the flows it holds, and the
// starters are made with it.

// FieldDef is a field as a category's assets record it, own or inherited:
// its key, label and validation type (text, number, date, url or select),
// a select field's choices and default, and whether the category requires
// it.
type FieldDef struct {
	Key      string
	Label    string
	Type     string
	Choices  []Option
	Default  string
	Required bool
}

// v1Definition is a version 1 definition, as far as converting reads it.
type v1Definition struct {
	Version     int      `json:"version"`
	Description string   `json:"description,omitempty"`
	CategoryIDs []string `json:"category_ids"`
	Steps       []v1Step `json:"steps"`
	LeaveUntil  string   `json:"leave_until,omitempty"`
}

// v1Step is a version 1 step: a step as version 2 has it, with Form's
// settings for fields, its name and its photo, and Change's fields, which
// version 2 does not have. Its own fields and changes take the JSON names
// over the step's.
type v1Step struct {
	Step
	Fields    []v1FieldSetting `json:"fields,omitempty"`
	AskName   bool             `json:"ask_name,omitempty"`
	NameLabel string           `json:"name_label,omitempty"`
	AskPhoto  bool             `json:"ask_photo,omitempty"`
	Changes   []v1Change       `json:"changes,omitempty"`
}

// v1FieldSetting is a version 1 Form's setting for one field, by its key.
type v1FieldSetting struct {
	Key      string `json:"key"`
	Hidden   bool   `json:"hidden,omitempty"`
	Label    string `json:"label,omitempty"`
	Help     string `json:"help,omitempty"`
	Default  string `json:"default,omitempty"`
	Required bool   `json:"required,omitempty"`
}

// v1Change is a version 1 change, which could set fields: a template for
// each field key.
type v1Change struct {
	Change
	Fields map[string]string `json:"fields,omitempty"`
}

// UpgradeV1 converts a version 1 definition to version 2, and reports
// whether it was version 1; any other is given back as it is. fieldsOf
// gives the fields a category's assets record, own and inherited, in
// order; it may be nil, as when there are none.
//
//   - Each Form's fields, as it asked them for the categories it could be
//     asked for, become questions of its own with the same key (so {owner}
//     and the like still work), label, help, default and required, and a
//     type from the field's: a select field's choices are copied. A field
//     only some of those categories record, or require, is asked of all,
//     required only when all of them require it. Its name and photo, if it
//     asked for them, become the questions {name} and {photo}.
//   - A field hidden but given a default becomes a row of Set fields
//     directly, set to that default.
//   - Just before each Create, a Set fields step sets each of those fields
//     from its answer, or default, and the photo, if one was asked.
//   - A Change's fields become a Set fields step where they were, the
//     Change split around it, so everything happens in the same order.
//
// New steps are given IDs as the editor gives them (NewStepID).
func UpgradeV1(definition string, fieldsOf func(categoryID string) ([]FieldDef, error)) (string, bool, error) {
	var head struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal([]byte(definition), &head); err != nil {
		return "", false, err
	}
	if head.Version != 1 {
		return definition, false, nil
	}
	var v1 v1Definition
	if err := json.Unmarshal([]byte(definition), &v1); err != nil {
		return "", false, err
	}
	d, err := upgradeV1(v1, fieldsOf)
	if err != nil {
		return "", false, err
	}
	out, err := json.Marshal(d)
	return string(out), true, err
}

// filling is fields a converted flow sets on the asset a Create files:
// rows, under a condition.
type filling struct {
	when *Condition
	rows []FieldValue
}

// add sets a field, in place of any row for it already.
func (f *filling) add(row FieldValue) {
	for i := range f.rows {
		if f.rows[i].Field == row.Field {
			f.rows[i] = row
			return
		}
	}
	f.rows = append(f.rows, row)
}

func upgradeV1(v1 v1Definition, fieldsOf func(string) ([]FieldDef, error)) (Definition, error) {
	d := Definition{Version: Version, Description: v1.Description, CategoryIDs: v1.CategoryIDs, LeaveUntil: v1.LeaveUntil, Steps: []Step{}}
	taken := map[string]bool{}
	for _, s := range v1.Steps {
		taken[s.ID] = true
	}
	newID := func(kind string) string {
		id := NewStepID(kind, taken)
		taken[id] = true
		return id
	}
	// What the form fills, for every Create after it, as version 1 filed
	// the answers each time; fields hidden with a default by a form shown
	// only on a condition, under it; and the photo question, for the next
	// Create.
	asked := &filling{}
	var hidden []*filling
	photo := ""
	possible := v1.CategoryIDs
	for _, s := range v1.Steps {
		switch s.Kind {
		case KindChooseCategory:
			switch {
			case s.Fixed != "":
				possible = []string{s.Fixed}
			case len(s.Categories) > 0:
				possible = s.Categories
			default:
				possible = v1.CategoryIDs
			}
			d.Steps = append(d.Steps, s.Step)
		case KindForm:
			cats := possible
			if w := s.When; w != nil && w.Value == ValueCategoryID && w.Op == OpIs {
				cats = []string{w.To}
			}
			form, rows, defaults, err := upgradeForm(s, cats, fieldsOf)
			if err != nil {
				return d, err
			}
			for _, r := range rows {
				asked.add(r)
			}
			if len(defaults) > 0 {
				into := asked
				if s.When != nil {
					into = &filling{when: s.When}
					hidden = append(hidden, into)
				}
				for _, r := range defaults {
					into.add(r)
				}
			}
			if q := photoOf(form); q != "" {
				photo = "{" + q + "}"
			}
			d.Steps = append(d.Steps, form)
		case KindCreate:
			if len(asked.rows) > 0 || photo != "" {
				d.Steps = append(d.Steps, Step{ID: newID(KindSetFields), Kind: KindSetFields, When: s.When, Fields: slices.Clone(asked.rows), Photo: photo})
			}
			for _, h := range hidden {
				d.Steps = append(d.Steps, Step{ID: newID(KindSetFields), Kind: KindSetFields, When: h.when, Fields: slices.Clone(h.rows)})
			}
			photo = ""
			d.Steps = append(d.Steps, s.Step)
		case KindChange:
			first := true
			piece := func(st Step) {
				if !first {
					st.ID = newID(st.Kind)
				}
				first = false
				d.Steps = append(d.Steps, st)
			}
			var run []Change
			flush := func() {
				if len(run) > 0 {
					st := s.Step
					st.Changes = run
					piece(st)
					run = nil
				}
			}
			for _, c := range s.Changes {
				if c.Kind != "fields" {
					run = append(run, c.Change)
					continue
				}
				flush()
				keys := make([]string, 0, len(c.Fields))
				for k := range c.Fields {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				rows := make([]FieldValue, 0, len(keys))
				for _, k := range keys {
					rows = append(rows, FieldValue{Field: k, Value: c.Fields[k]})
				}
				piece(Step{ID: s.ID, Kind: KindSetFields, When: s.When, Fields: rows})
			}
			flush()
			if first {
				// No changes at all: kept as it was, for saving to say so.
				d.Steps = append(d.Steps, s.Step)
			}
		default:
			d.Steps = append(d.Steps, s.Step)
		}
	}
	return d, nil
}

func photoOf(form Step) string {
	for _, q := range form.Questions {
		if q.Type == QuestionPhoto {
			return q.Key
		}
	}
	return ""
}

// upgradeForm converts a version 1 Form, as it was asked for any of cats:
// the Form, with its questions; the rows that set the fields it asked from
// their answers; and those that set fields it hid with a default.
func upgradeForm(s v1Step, cats []string, fieldsOf func(string) ([]FieldDef, error)) (Step, []FieldValue, []FieldValue, error) {
	form := s.Step
	form.Fields, form.Questions = nil, nil
	byCat := make([]map[string]FieldDef, len(cats))
	var order []string
	defs := map[string]FieldDef{}
	for i, id := range cats {
		byCat[i] = map[string]FieldDef{}
		if fieldsOf == nil {
			continue
		}
		list, err := fieldsOf(id)
		if err != nil {
			return form, nil, nil, fmt.Errorf("the fields of %s: %w", id, err)
		}
		for _, f := range list {
			byCat[i][f.Key] = f
			if _, ok := defs[f.Key]; !ok {
				defs[f.Key] = f
				order = append(order, f.Key)
			}
		}
	}
	// The creator's order first, then the categories'.
	settings := map[string]v1FieldSetting{}
	var keys []string
	for _, fs := range s.Fields {
		settings[fs.Key] = fs
		if _, ok := defs[fs.Key]; ok && !slices.Contains(keys, fs.Key) {
			keys = append(keys, fs.Key)
		}
	}
	for _, k := range order {
		if !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
	}
	taken := map[string]bool{}
	for _, q := range s.Questions {
		taken[q.Key] = true
	}
	var questions []Question
	var rows, defaults []FieldValue
	for _, k := range keys {
		def, set := defs[k], settings[k]
		recorded, requiredBy := 0, 0
		for _, fields := range byCat {
			if f, ok := fields[k]; ok {
				recorded++
				if f.Required {
					requiredBy++
				}
			}
		}
		if set.Hidden && requiredBy == 0 {
			if set.Default != "" {
				defaults = append(defaults, FieldValue{Field: k, Value: set.Default})
			}
			continue
		}
		q := Question{Key: k, Label: def.Label, Help: set.Help, Type: questionType(def.Type), Default: set.Default,
			Required: recorded == len(cats) && (set.Required || requiredBy == len(cats))}
		if set.Label != "" {
			q.Label = set.Label
		}
		if q.Type == QuestionChoice {
			q.Options = slices.Clone(def.Choices)
			if q.Default == "" {
				q.Default = def.Default
			}
		}
		questions = append(questions, q)
		taken[k] = true
		rows = append(rows, FieldValue{Field: k, Value: "{" + k + "}"})
	}
	// Its name, first; a field keyed name took {name} in version 1, and
	// still does.
	if s.AskName && !taken["name"] {
		label := s.NameLabel
		if label == "" {
			label = "Its name"
		}
		questions = append([]Question{{Key: "name", Label: label, Type: QuestionText, Required: true}}, questions...)
		taken["name"] = true
	}
	questions = append(questions, s.Questions...)
	if s.AskPhoto {
		key := "photo"
		for n := 2; taken[key]; n++ {
			key = fmt.Sprintf("photo_%d", n)
		}
		questions = append(questions, Question{Key: key, Label: "A photo", Type: QuestionPhoto})
	}
	form.Questions = questions
	return form, rows, defaults, nil
}

// questionType is the type of question that asks for a field of a
// validation type.
func questionType(validation string) string {
	switch validation {
	case "number":
		return QuestionNumber
	case "date":
		return QuestionDate
	case "select":
		return QuestionChoice
	case "boolean", "bool":
		return QuestionYesNo
	}
	return QuestionText
}

// FieldCorpus is an instance's fields and categories, as their tables hold
// them, to work out the fields each category's assets record when
// converting, where the store is not at hand: in a migration, or reading
// an archive.
type FieldCorpus struct {
	// Fields by ID; their Required is not used.
	Fields map[string]FieldDef
	// Parents, by category ID.
	Parents map[string]string
	// Assigned, by category ID: the fields it assigns itself.
	Assigned map[string][]FieldAssignment
}

// FieldAssignment is a field a category assigns its assets, by the
// field's ID.
type FieldAssignment struct {
	FieldID  string
	Required bool
}

// NewFieldCorpus is an empty corpus.
func NewFieldCorpus() *FieldCorpus {
	return &FieldCorpus{Fields: map[string]FieldDef{}, Parents: map[string]string{}, Assigned: map[string][]FieldAssignment{}}
}

// AddField adds a field, as the fields table holds it: its select options
// as JSON ({"choices": [{"label", "value"}], "default"}).
func (c *FieldCorpus) AddField(id, key, label, validation, options string) {
	f := FieldDef{Key: key, Label: label, Type: validation}
	if options != "" {
		var opts struct {
			Choices []struct {
				Label string `json:"label"`
				Value string `json:"value"`
			} `json:"choices"`
			Default *string `json:"default"`
		}
		if json.Unmarshal([]byte(options), &opts) == nil {
			for _, ch := range opts.Choices {
				f.Choices = append(f.Choices, Option{Value: ch.Value, Label: ch.Label})
			}
			if opts.Default != nil {
				f.Default = *opts.Default
			}
		}
	}
	c.Fields[id] = f
}

// Effective lists the fields a category's assets record, own and
// inherited, by key, as the store's EffectiveFieldsForAsset does: a field
// assigned at several levels is required if any requires it.
func (c *FieldCorpus) Effective(categoryID string) ([]FieldDef, error) {
	seen := map[string]bool{}
	byID := map[string]FieldDef{}
	for id, depth := categoryID, 0; id != "" && depth < 100 && !seen[id]; id, depth = c.Parents[id], depth+1 {
		seen[id] = true
		for _, a := range c.Assigned[id] {
			f, ok := c.Fields[a.FieldID]
			if !ok {
				continue
			}
			if had, ok := byID[a.FieldID]; ok {
				had.Required = had.Required || a.Required
				byID[a.FieldID] = had
				continue
			}
			f.Required = a.Required
			byID[a.FieldID] = f
		}
	}
	out := make([]FieldDef, 0, len(byID))
	for _, f := range byID {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}
