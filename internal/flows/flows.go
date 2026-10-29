// Package flows describes flows (README §6.7, #121): guided pages for doing
// one everyday thing with assets, such as filing a new one or finishing with
// one, built from an ordered list of configurable steps. It checks a flow
// before it is saved, renders its templates, and decides which step comes
// next. It touches no database: what a step does is done by the API, as the
// person running the flow.
package flows

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

// Version is the version of the definition this package writes and reads.
const Version = 1

// The kinds of step.
const (
	KindChooseCategory = "choose_category"
	KindForm           = "form"
	KindFind           = "find"
	KindCreate         = "create"
	KindLabel          = "label"
	KindChange         = "change"
	KindConfirm        = "confirm"
	KindDone           = "done"
)

// Kinds lists the kinds of step, in the order an editor offers them.
var Kinds = []string{KindChooseCategory, KindForm, KindFind, KindCreate, KindLabel, KindChange, KindConfirm, KindDone}

// Definition is what a flow does: the categories it files into or takes
// from, and its steps, in order.
type Definition struct {
	Version int `json:"version"`
	// Description is one line of instructions, for its poster.
	Description string   `json:"description,omitempty"`
	CategoryIDs []string `json:"category_ids"`
	Steps       []Step   `json:"steps"`
}

// Step is one step of a flow. Which of its settings apply depends on its
// kind; the others are ignored.
type Step struct {
	// ID names the step, unique in the flow.
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Title string `json:"title,omitempty"`
	// Text is shown with the step, a template: what Confirm asks, what Done
	// says.
	Text string `json:"text,omitempty"`
	// When, if set, shows the step only when it holds.
	When *Condition `json:"when,omitempty"`

	// Choose category: the categories offered, of the flow's (all of them
	// when empty), or one fixed category, skipping the choice.
	Categories []string `json:"categories,omitempty"`
	Fixed      string   `json:"fixed,omitempty"`

	// Form: the creator's settings for the fields the chosen category's
	// assets record, in the order to ask them; whether to ask for the
	// asset's name and a photo; and questions of the run's own.
	Fields    []FieldSetting `json:"fields,omitempty"`
	AskName   bool           `json:"ask_name,omitempty"`
	NameLabel string         `json:"name_label,omitempty"`
	AskPhoto  bool           `json:"ask_photo,omitempty"`
	Questions []Question     `json:"questions,omitempty"`

	// Find: how to find the asset (scan, search, list), and what narrows
	// the assets found: those a flow filed (by its ID, this one's or
	// another's, such as Leave something's for Collect it), and those with
	// a field matching a value.
	FindBy  []string `json:"find_by,omitempty"`
	FiledBy string   `json:"filed_by,omitempty"`
	Match   *Match   `json:"match,omitempty"`

	// Create: the new asset's name, a template, and where to place it.
	Name     string    `json:"name,omitempty"`
	Location *Location `json:"location,omitempty"`

	// Label: what to do with the asset's label.
	Label *Label `json:"label,omitempty"`

	// Change: what happens to the asset, in order.
	Changes []Change `json:"changes,omitempty"`
}

// Condition holds when a value of the run is, is not, is set or is not
// set: {"value": "category_id", "op": "is", "to": "<id>"}.
type Condition struct {
	Value string `json:"value"`
	Op    string `json:"op"`
	To    string `json:"to,omitempty"`
}

// The operators of a condition.
const (
	OpIs    = "is"
	OpIsNot = "is_not"
	OpSet   = "set"
	OpUnset = "unset"
)

// FieldSetting is the creator's setting for one field of a Form, by its
// key. A field with no setting is asked as its category defines it.
type FieldSetting struct {
	Key    string `json:"key"`
	Hidden bool   `json:"hidden,omitempty"`
	Label  string `json:"label,omitempty"`
	Help   string `json:"help,omitempty"`
	// Default is a template: a fixed value, {today}, or an earlier answer.
	Default  string `json:"default,omitempty"`
	Required bool   `json:"required,omitempty"`
}

// Question is asked by a Form for the run's own use, in templates; it is
// recorded in no field.
type Question struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Help     string `json:"help,omitempty"`
	Type     string `json:"type,omitempty"` // text (the default), number or date
	Default  string `json:"default,omitempty"`
	Required bool   `json:"required,omitempty"`
}

// Match narrows what Find finds to assets whose field Field has the value
// Value, a template ("{owner}").
type Match struct {
	Field string `json:"field"`
	Value string `json:"value"`
}

// Location is where Create places the asset: a fixed place, or one the
// person scans.
type Location struct {
	Mode    string `json:"mode"` // fixed or scan
	AssetID string `json:"asset_id,omitempty"`
}

// The ways of placing an asset.
const (
	PlaceFixed = "fixed"
	PlaceScan  = "scan"
)

// Label is what a Label step does with the asset's label: prints it to a
// printer or pool, adds it to the print queue, or shows its code on screen.
// TemplateID is the template to use; empty, the category's.
type Label struct {
	Mode        string `json:"mode"`
	Destination string `json:"destination,omitempty"` // printer:<id> or pool:<id>
	TemplateID  string `json:"template_id,omitempty"`
}

// The ways of handling a label.
const (
	LabelPrint = "print"
	LabelQueue = "queue"
	LabelShow  = "show"
)

// Change is one thing a Change step does to the asset.
type Change struct {
	Kind string `json:"kind"`
	// Condition and Note, for ChangeCondition.
	Condition string `json:"condition,omitempty"`
	Note      string `json:"note,omitempty"`
	// AssetID is the place, for ChangePlace.
	AssetID string `json:"asset_id,omitempty"`
	// CategoryID, for ChangeCategory.
	CategoryID string `json:"category_id,omitempty"`
	// Fields, for ChangeFields: a template for each field key.
	Fields map[string]string `json:"fields,omitempty"`
	// Action is the key of an action to press, for ChangeAction.
	Action string `json:"action,omitempty"`
}

// The changes a Change step can make. Archiving is how bam removes an
// asset: it is kept, out of the lists, and can be restored.
const (
	ChangeArchive   = "archive"
	ChangeCondition = "condition"
	ChangeBorrow    = "borrow"
	ChangeReturn    = "return"
	ChangePlace     = "place"
	ChangeCategory  = "category"
	ChangeFields    = "fields"
	ChangeAction    = "action"
)

// The ways Find finds an asset.
const (
	FindScan   = "scan"
	FindSearch = "search"
	FindList   = "list"
)

// The values every run has, and those its steps set.
const (
	ValueToday      = "today"
	ValueNow        = "now"
	ValueUser       = "user"
	ValueFlow       = "flow"
	ValueCategory   = "category"
	ValueCategoryID = "category_id"
	ValueName       = "name"
	ValueAsset      = "asset"
	ValueAssetID    = "asset_id"
	ValueCode       = "code"
)

// builtIn are the values a run has from the start.
var builtIn = []string{ValueToday, ValueNow, ValueUser, ValueFlow}

// Values are a run's values, by name.
type Values map[string]string

// Start is a run's values as it begins: the date and time, who runs it,
// the flow, and the category when the flow has only one.
func Start(flow, user string, now time.Time, d *Definition, categoryName func(string) string) Values {
	v := Values{
		ValueToday: now.Format("2006-01-02"),
		ValueNow:   now.Format("2006-01-02 15:04"),
		ValueUser:  user,
		ValueFlow:  flow,
	}
	if len(d.CategoryIDs) == 1 {
		v[ValueCategoryID] = d.CategoryIDs[0]
		v[ValueCategory] = categoryName(d.CategoryIDs[0])
	}
	return v
}

var reference = regexp.MustCompile(`\{([a-z][a-z0-9_]*)\}`)

// Render fills a template's {name}s with the run's values. A value the run
// has not set is empty.
func Render(template string, v Values) string {
	return reference.ReplaceAllStringFunc(template, func(m string) string {
		return v[m[1:len(m)-1]]
	})
}

// References lists the values a template names, in order.
func References(template string) []string {
	var out []string
	for _, m := range reference.FindAllStringSubmatch(template, -1) {
		out = append(out, m[1])
	}
	return out
}

// Holds reports whether a condition holds for these values; no condition
// always does.
func (c *Condition) Holds(v Values) bool {
	if c == nil {
		return true
	}
	switch c.Op {
	case OpIs:
		return v[c.Value] == c.To
	case OpIsNot:
		return v[c.Value] != c.To
	case OpSet:
		return v[c.Value] != ""
	case OpUnset:
		return v[c.Value] == ""
	}
	return false
}

// Next is the index of the first step from i on whose condition holds, or
// len(Steps) when none does.
func (d *Definition) Next(i int, v Values) int {
	for ; i < len(d.Steps); i++ {
		if d.Steps[i].When.Holds(v) {
			return i
		}
	}
	return len(d.Steps)
}

// Auto reports whether a step does its work as soon as it is reached,
// asking nothing.
func (s *Step) Auto() bool {
	switch s.Kind {
	case KindLabel, KindChange:
		return true
	case KindChooseCategory:
		return s.Fixed != ""
	case KindCreate:
		return s.Location == nil || s.Location.Mode != PlaceScan
	}
	return false
}

// Permissions are those a flow uses: whoever saves it must hold each, so it
// never offers more than they could do.
func (d *Definition) Permissions() []string {
	set := map[string]bool{}
	for _, s := range d.Steps {
		switch s.Kind {
		case KindFind:
			set[rbac.PermAssetsRead] = true
			if slices.Contains(s.FindBy, FindScan) {
				set[rbac.PermAssetsScan] = true
			}
		case KindCreate:
			set[rbac.PermAssetsWrite] = true
		case KindLabel:
			if s.Label != nil && s.Label.Mode != LabelShow {
				set[rbac.PermLabelsPrint] = true
			}
		case KindChange:
			for _, c := range s.Changes {
				set[changePermission(c.Kind)] = true
			}
		}
	}
	delete(set, "")
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func changePermission(kind string) string {
	switch kind {
	case ChangeArchive:
		return rbac.PermAssetsDelete
	case ChangeCondition:
		return rbac.PermAssetsReport
	case ChangeBorrow, ChangeReturn:
		return rbac.PermAssetsBorrow
	case ChangePlace, ChangeCategory, ChangeFields:
		return rbac.PermAssetsWrite
	case ChangeAction:
		return rbac.PermAssetsAct
	}
	return ""
}

// Catalog is what a flow's settings may name, as the instance has it: the
// categories, the keys of the fields their assets record (own and
// inherited, across all the flow's categories), label templates, printers
// and pools as destinations, actions by key, flows by ID, and assets (for
// places).
type Catalog struct {
	Flows        map[string]bool
	Categories   map[string]bool
	Fields       map[string]bool
	Templates    map[string]bool
	Destinations map[string]bool
	Actions      map[string]bool
	Asset        func(id string) bool
}

// Problem is something that stops a flow being saved, by the step at fault
// (empty for the flow as a whole).
type Problem struct {
	Step    string `json:"step,omitempty"`
	Message string `json:"message"`
}

func (p Problem) String() string {
	if p.Step == "" {
		return p.Message
	}
	return fmt.Sprintf("step %s: %s", p.Step, p.Message)
}

var (
	stepID      = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
	questionKey = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
)

// MaxSteps is the most steps a flow has.
const MaxSteps = 50

// Check lists what stops a flow being saved: every step well formed, every
// category, field, template, printer, place and action it names existing,
// and every template and condition naming only values set before it.
func (d *Definition) Check(c Catalog) []Problem {
	var out []Problem
	flowWide := func(format string, args ...any) { out = append(out, Problem{Message: fmt.Sprintf(format, args...)}) }
	if d.Version != Version {
		flowWide("the definition is version %d; this server reads version %d", d.Version, Version)
	}
	if len(d.CategoryIDs) == 0 {
		flowWide("a flow needs at least one category to file into or take from")
	}
	flowCats := map[string]bool{}
	for _, id := range d.CategoryIDs {
		if !c.Categories[id] {
			flowWide("no category has the ID %q", id)
		}
		flowCats[id] = true
	}
	if len(d.Steps) == 0 {
		flowWide("a flow needs at least one step")
	}
	if len(d.Steps) > MaxSteps {
		flowWide("a flow has at most %d steps", MaxSteps)
	}

	// What is set so far, as the steps go: names the run has from the
	// start, and those earlier steps set.
	set := map[string]bool{}
	for _, n := range builtIn {
		set[n] = true
	}
	if len(d.CategoryIDs) == 1 {
		set[ValueCategory], set[ValueCategoryID] = true, true
	}
	fieldsAsked := false
	ids := map[string]bool{}
	for i, s := range d.Steps {
		bad := func(format string, args ...any) {
			out = append(out, Problem{Step: s.ID, Message: fmt.Sprintf(format, args...)})
		}
		known := func(name string) bool { return set[name] || (fieldsAsked && c.Fields[name]) }
		uses := func(what, template string) {
			for _, ref := range References(template) {
				if !known(ref) {
					bad("%s names {%s}, which no earlier step sets", what, ref)
				}
			}
		}
		if !stepID.MatchString(s.ID) {
			bad("a step's ID is 1 to 32 lowercase letters, digits and _, starting with a letter")
		} else if ids[s.ID] {
			bad("another step has the ID %q", s.ID)
		}
		ids[s.ID] = true
		if s.When != nil {
			switch s.When.Op {
			case OpIs, OpIsNot, OpSet, OpUnset:
			default:
				bad("a condition's op is is, is_not, set or unset")
			}
			if !known(s.When.Value) {
				bad("its condition names %q, which no earlier step sets", s.When.Value)
			}
			if s.When.Value == ValueCategoryID && (s.When.Op == OpIs || s.When.Op == OpIsNot) && !flowCats[s.When.To] {
				bad("its condition names a category the flow does not use")
			}
		}
		needsCategory := func() {
			if !set[ValueCategoryID] {
				bad("it needs a category: choose one in an earlier step, or give the flow only one")
			}
		}
		needsAsset := func() {
			if !set[ValueAssetID] {
				bad("it needs an asset: find or create one in an earlier step")
			}
		}
		uses("its text", s.Text)
		uses("its title", s.Title)
		switch s.Kind {
		case KindChooseCategory:
			for _, id := range s.Categories {
				if !flowCats[id] {
					bad("it offers a category the flow does not use")
				}
			}
			if s.Fixed != "" && !flowCats[s.Fixed] {
				bad("its fixed category is not one the flow uses")
			}
			set[ValueCategory], set[ValueCategoryID] = true, true
		case KindForm:
			needsCategory()
			seen := map[string]bool{}
			for _, f := range s.Fields {
				if !c.Fields[f.Key] {
					bad("no field the flow's categories record has the key %q", f.Key)
				}
				if seen[f.Key] {
					bad("the field %q is set twice", f.Key)
				}
				seen[f.Key] = true
				uses("the default of "+f.Key, f.Default)
			}
			for _, q := range s.Questions {
				switch {
				case !questionKey.MatchString(q.Key):
					bad("a question's key is lowercase letters, digits and _, starting with a letter")
				case c.Fields[q.Key] || slices.Contains(builtIn, q.Key) || set[q.Key] || seen[q.Key] || isStepValue(q.Key):
					bad("the question %q has the name of a field or a value the run already has", q.Key)
				}
				if strings.TrimSpace(q.Label) == "" {
					bad("the question %q needs a label", q.Key)
				}
				switch q.Type {
				case "", "text", "number", "date":
				default:
					bad("the question %q is text, number or date", q.Key)
				}
				uses("the default of "+q.Key, q.Default)
				seen[q.Key] = true
			}
			fieldsAsked = true
			for _, q := range s.Questions {
				set[q.Key] = true
			}
			if s.AskName {
				set[ValueName] = true
			}
		case KindFind:
			if len(s.FindBy) == 0 {
				bad("it needs a way to find: scan, search or list")
			}
			for _, by := range s.FindBy {
				if by != FindScan && by != FindSearch && by != FindList {
					bad("it finds by scan, search or list, not %q", by)
				}
			}
			if s.FiledBy != "" && !c.Flows[s.FiledBy] {
				bad("it looks among what a flow filed, but no flow has the ID %q", s.FiledBy)
			}
			if s.Match != nil {
				if !c.Fields[s.Match.Field] {
					bad("no field the flow's categories record has the key %q", s.Match.Field)
				}
				uses("its match", s.Match.Value)
			}
			set[ValueAsset], set[ValueAssetID], set[ValueCode] = true, true, true
			// The asset found brings its field values.
			fieldsAsked = true
		case KindCreate:
			needsCategory()
			if strings.TrimSpace(s.Name) == "" {
				bad("it needs a name for the new asset, such as {name} or {owner}: {what}")
			}
			uses("its name", s.Name)
			if l := s.Location; l != nil {
				switch {
				case l.Mode == PlaceFixed && (l.AssetID == "" || c.Asset == nil || !c.Asset(l.AssetID)):
					bad("its place is not an asset")
				case l.Mode != PlaceFixed && l.Mode != PlaceScan:
					bad("its place is fixed or scanned")
				}
			}
			set[ValueAsset], set[ValueAssetID], set[ValueCode] = true, true, true
		case KindLabel:
			needsAsset()
			l := s.Label
			if l == nil {
				bad("it needs to say what to do with the label: print, queue or show")
				break
			}
			switch l.Mode {
			case LabelPrint:
				if !c.Destinations[l.Destination] {
					bad("it prints to a printer or pool that does not exist")
				}
			case LabelQueue:
				if l.Destination != "" && !c.Destinations[l.Destination] {
					bad("it queues for a printer or pool that does not exist")
				}
			case LabelShow:
			default:
				bad("it prints, queues or shows the label")
			}
			if l.TemplateID != "" && !c.Templates[l.TemplateID] {
				bad("its label template does not exist")
			}
		case KindChange:
			needsAsset()
			if len(s.Changes) == 0 {
				bad("it needs at least one change")
			}
			for _, ch := range s.Changes {
				switch ch.Kind {
				case ChangeArchive, ChangeBorrow, ChangeReturn:
				case ChangeCondition:
					switch ch.Condition {
					case "ok", "needs_maintenance", "out_of_service":
					default:
						bad("a condition is ok, needs_maintenance or out_of_service")
					}
					uses("its note", ch.Note)
				case ChangePlace:
					if ch.AssetID == "" || c.Asset == nil || !c.Asset(ch.AssetID) {
						bad("the place it moves to is not an asset")
					}
				case ChangeCategory:
					if !c.Categories[ch.CategoryID] {
						bad("the category it moves to does not exist")
					}
				case ChangeFields:
					if len(ch.Fields) == 0 {
						bad("setting fields needs at least one")
					}
					for key, tmpl := range ch.Fields {
						if !c.Fields[key] {
							bad("no field the flow's categories record has the key %q", key)
						}
						uses("the value of "+key, tmpl)
					}
				case ChangeAction:
					if !c.Actions[ch.Action] {
						bad("no action has the key %q", ch.Action)
					}
				default:
					bad("%q is not a change it can make", ch.Kind)
				}
			}
		case KindConfirm:
		case KindDone:
			if i != len(d.Steps)-1 {
				bad("Done is the last step")
			}
		default:
			bad("%q is not a kind of step", s.Kind)
		}
	}
	if n := len(d.Steps); n > 0 && d.Steps[n-1].Kind != KindDone {
		flowWide("a flow ends with a Done step")
	}
	return out
}

func isStepValue(name string) bool {
	switch name {
	case ValueCategory, ValueCategoryID, ValueName, ValueAsset, ValueAssetID, ValueCode:
		return true
	}
	return false
}

// Starter is a ready-made flow to adjust.
type Starter struct {
	Key         string     `json:"key"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Definition  Definition `json:"definition"`
}

// Starters are the two ready-made flows, filing into or taking from these
// categories: Leave something, and Collect it.
func Starters(categoryIDs []string) []Starter {
	cats := append([]string{}, categoryIDs...)
	leave := Definition{Version: Version, Description: "Scan to leave something here, with your name on it.", CategoryIDs: cats, Steps: []Step{
		{ID: "kind", Kind: KindChooseCategory, Title: "What are you leaving?"},
		{ID: "about", Kind: KindForm, Title: "About it", AskName: true, NameLabel: "What is it?"},
		{ID: "file", Kind: KindCreate, Name: "{name}"},
		{ID: "label", Kind: KindLabel, Label: &Label{Mode: LabelShow}},
		{ID: "done", Kind: KindDone, Title: "Filed", Text: "{asset} is filed. Put its label on it."},
	}}
	if len(cats) == 1 {
		leave.Steps = leave.Steps[1:]
	}
	collect := Definition{Version: Version, Description: "Scan to collect something you left here.", CategoryIDs: cats, Steps: []Step{
		{ID: "find", Kind: KindFind, Title: "What are you collecting?", FindBy: []string{FindScan, FindSearch, FindList}},
		{ID: "confirm", Kind: KindConfirm, Title: "Collect it?", Text: "{asset} will be taken off the list."},
		{ID: "finish", Kind: KindChange, Changes: []Change{{Kind: ChangeArchive}}},
		{ID: "done", Kind: KindDone, Title: "Collected", Text: "{asset} is collected."},
	}}
	return []Starter{
		{Key: "leave", Name: "Leave something", Description: "Choose what kind of thing it is, fill in its form, file it and label it.", Definition: leave},
		{Key: "collect", Name: "Collect it", Description: "Find it by its code, by search or from a list, then archive it.", Definition: collect},
	}
}
