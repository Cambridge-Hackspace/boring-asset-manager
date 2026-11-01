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
	"strconv"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

// Version is the version of the definition this package writes and reads:
// 2, in which a Form asks only its own questions and a Set fields step
// puts values into an asset's fields (#174). UpgradeV1 converts version 1.
const Version = 2

// The kinds of step.
const (
	KindChooseCategory = "choose_category"
	KindForm           = "form"
	KindSetFields      = "set_fields"
	KindFind           = "find"
	KindCreate         = "create"
	KindLabel          = "label"
	KindChange         = "change"
	KindConfirm        = "confirm"
	KindDone           = "done"
)

// Kinds lists the kinds of step, in the order an editor offers them.
var Kinds = []string{KindChooseCategory, KindForm, KindSetFields, KindFind, KindCreate, KindLabel, KindChange, KindConfirm, KindDone}

// Definition is what a flow does: the categories it files into or takes
// from, and its steps, in order.
type Definition struct {
	Version int `json:"version"`
	// Description is one line of instructions, for its poster.
	Description string   `json:"description,omitempty"`
	CategoryIDs []string `json:"category_ids"`
	Steps       []Step   `json:"steps"`
	// LeaveUntil is the key of a date field: what the flow files is
	// overdue once that date has passed, and announced as such once.
	LeaveUntil string `json:"leave_until,omitempty"`
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

	// Form: its questions, in the order asked. Each answer is the run's
	// value of the question's key.
	Questions []Question `json:"questions,omitempty"`

	// Set fields: the asset's fields to set, each to a template, and its
	// photo, a photo question's answer ("{photo}"). Before a Create it
	// fills the new asset's fields as it is filed; after a Find or Create
	// it updates that asset.
	Fields []FieldValue `json:"fields,omitempty"`
	Photo  string       `json:"photo,omitempty"`

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

// Question is one thing a Form asks. Its answer is the run's value of its
// key, for templates, and for a Set fields step to put in a field.
type Question struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Help  string `json:"help,omitempty"`
	// Type is what is asked for (QuestionText when empty); Options, the
	// choices of a choice question.
	Type    string   `json:"type,omitempty"`
	Options []Option `json:"options,omitempty"`
	// Default is a template: a fixed value, {today}, or an earlier answer.
	Default  string `json:"default,omitempty"`
	Required bool   `json:"required,omitempty"`
}

// The types of question. A yes/no question's answer is yes or no; a
// photo's is the file's name, the photo itself being added by the page
// that took it once the asset it is of exists (README §6.7).
const (
	QuestionText     = "text"
	QuestionLongText = "long_text"
	QuestionNumber   = "number"
	QuestionDate     = "date"
	QuestionYesNo    = "yes_no"
	QuestionChoice   = "choice"
	QuestionPhoto    = "photo"
)

// QuestionTypes lists the types of question, in the order an editor
// offers them.
var QuestionTypes = []string{QuestionText, QuestionLongText, QuestionNumber, QuestionDate, QuestionYesNo, QuestionChoice, QuestionPhoto}

// Option is one choice of a choice question: its value, the answer, and
// how it is shown (as the value, when empty).
type Option struct {
	Value string `json:"value"`
	Label string `json:"label,omitempty"`
}

// Problem says why an answer does not suit a question, or is empty when it
// does. An empty answer suits any.
func (q *Question) Problem(answer string) string {
	if answer == "" {
		return ""
	}
	switch q.Type {
	case QuestionNumber:
		if _, err := strconv.ParseFloat(answer, 64); err != nil {
			return "is a number"
		}
	case QuestionDate:
		if _, err := time.Parse("2006-01-02", answer); err != nil {
			return "is a date, as 2026-10-30"
		}
	case QuestionYesNo:
		if answer != "yes" && answer != "no" {
			return "is yes or no"
		}
	case QuestionChoice:
		if !slices.ContainsFunc(q.Options, func(o Option) bool { return o.Value == answer }) {
			return "is one of its choices"
		}
	}
	return ""
}

// FieldValue is one row of a Set fields step: the key of an asset field,
// and the value to give it, a template.
type FieldValue struct {
	Field string `json:"field"`
	Value string `json:"value"`
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
	// Hook is the key of a hook to trigger, for ChangeHook.
	Hook string `json:"hook,omitempty"`
}

// The changes a Change step can make. Archiving is how bam removes an
// asset: it is kept, out of the lists, and can be restored. Setting its
// fields is a Set fields step's work (#174).
const (
	ChangeArchive   = "archive"
	ChangeCondition = "condition"
	ChangeBorrow    = "borrow"
	ChangeReturn    = "return"
	ChangePlace     = "place"
	ChangeCategory  = "category"
	ChangeHook      = "hook"
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

// PhotoQuestion is the key of the photo question a Set fields step's photo
// names ("{photo}"), or empty when it names none.
func (s *Step) PhotoQuestion() string {
	if refs := References(s.Photo); len(refs) == 1 && s.Photo == "{"+refs[0]+"}" {
		return refs[0]
	}
	return ""
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

// Asks reports whether a step is a screen that asks the person something:
// not Done, and not one that does its work as soon as it is reached.
func (s *Step) Asks() bool { return s.Kind != KindDone && !s.Auto() }

// Ahead counts the steps after i that ask something and whose condition
// holds on v (#166). A step whose condition depends on an answer not yet
// given counts once it holds, so a run's total can grow as it goes.
func (d *Definition) Ahead(i int, v Values) int {
	n := 0
	for j := i + 1; j < len(d.Steps); j++ {
		if d.Steps[j].Asks() && d.Steps[j].When.Holds(v) {
			n++
		}
	}
	return n
}

// Auto reports whether a step does its work as soon as it is reached,
// asking nothing.
func (s *Step) Auto() bool {
	switch s.Kind {
	case KindLabel, KindChange, KindSetFields:
		return true
	case KindChooseCategory:
		return s.Fixed != ""
	case KindCreate:
		return s.Location == nil || s.Location.Mode != PlaceScan
	}
	return false
}

// The places a Set fields step can be, which decide what it sets.
const (
	// OnNew is before a Create, with no asset yet: it fills the new
	// asset's fields as it is filed.
	OnNew = "new"
	// OnFound and OnFiled are after a Find or a Create: it updates the
	// asset found, or filed.
	OnFound = "found"
	OnFiled = "filed"
)

// SetsOn says what the Set fields step at i sets: the new asset the next
// Create files, when a Create comes after it before any Find (OnNew), or
// else the asset the nearest Find or Create before it found or filed
// (OnFound, OnFiled); empty when there is neither.
func (d *Definition) SetsOn(i int) string {
	for _, s := range d.Steps[i+1:] {
		if s.Kind == KindCreate {
			return OnNew
		}
		if s.Kind == KindFind {
			break
		}
	}
	for j := i - 1; j >= 0; j-- {
		switch d.Steps[j].Kind {
		case KindFind:
			return OnFound
		case KindCreate:
			return OnFiled
		}
	}
	return ""
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
		case KindCreate, KindSetFields:
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
	case ChangePlace, ChangeCategory:
		return rbac.PermAssetsWrite
	case ChangeHook:
		return rbac.PermAssetsHook
	}
	return ""
}

// Catalog is what a flow's settings may name, as the instance has it: the
// categories, the keys of the fields their assets record (own and
// inherited, across all the flow's categories), label templates, printers
// and pools as destinations, hooks by key, flows by ID, and assets (for
// places).
type Catalog struct {
	Flows        map[string]bool
	Categories   map[string]bool
	Fields       map[string]bool
	DateFields   map[string]bool
	Templates    map[string]bool
	Destinations map[string]bool
	Hooks        map[string]bool
	Asset        func(id string) bool
	// Names are the categories' names, by ID, and Labels the fields', by
	// key, to say what is missing.
	Names  map[string]string
	Labels map[string]string
	// Required lists, by the ID of each of the flow's categories, the keys
	// of the fields its assets must have, own and inherited.
	Required map[string][]string
	// Suits says why a fixed value does not suit a field, if it does not.
	Suits func(field, value string) error
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

// V1Refused is why a version 1 definition is refused, saying what changed.
const V1Refused = "the definition is version 1, which this server no longer takes: in version 2 a Form asks only its own questions " +
	"(its fields, ask_name and ask_photo became questions), and a Set fields step (set_fields) sets the asset's fields, " +
	"in place of Change's fields; give version 2, or import it in a package, which converts it"

// Check lists what stops a flow being saved: every step well formed, every
// category, field, template, printer, place and hook it names existing,
// every template and condition naming only values set before it, and
// every field a category Create may file into requires set before it.
func (d *Definition) Check(c Catalog) []Problem {
	var out []Problem
	flowWide := func(format string, args ...any) { out = append(out, Problem{Message: fmt.Sprintf(format, args...)}) }
	switch d.Version {
	case Version:
	case 1:
		flowWide("%s", V1Refused)
	default:
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
	if d.LeaveUntil != "" && !c.DateFields[d.LeaveUntil] {
		flowWide("its date to collect by, %q, is not a date field its categories record", d.LeaveUntil)
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
	// The categories a Create may file into, as the last Choose category
	// offers them; the photo questions asked so far; and the fields Set
	// fields steps fill for the next Create, each under its condition.
	filing := d.CategoryIDs
	photos := map[string]bool{}
	type filling struct {
		when *Condition
		keys []string
	}
	var pending []filling
	fieldsKnown := false
	ids := map[string]bool{}
	for i, s := range d.Steps {
		bad := func(format string, args ...any) {
			out = append(out, Problem{Step: s.ID, Message: fmt.Sprintf(format, args...)})
		}
		known := func(name string) bool { return set[name] || (fieldsKnown && c.Fields[name]) }
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
			switch {
			case s.Fixed != "":
				filing = []string{s.Fixed}
			case len(s.Categories) > 0:
				filing = s.Categories
			default:
				filing = d.CategoryIDs
			}
			set[ValueCategory], set[ValueCategoryID] = true, true
		case KindForm:
			seen := map[string]bool{}
			for _, q := range s.Questions {
				switch {
				case !questionKey.MatchString(q.Key):
					bad("a question's key is lowercase letters, digits and _, starting with a letter")
				case slices.Contains(builtIn, q.Key) || isStepValue(q.Key):
					bad("the question %q has the name of a value every run has, or a step sets", q.Key)
				case seen[q.Key]:
					// An earlier step's may be asked again, as by forms
					// shown for different categories: the later answer
					// is the value.
					bad("the question %q is asked twice", q.Key)
				}
				if strings.TrimSpace(q.Label) == "" {
					bad("the question %q needs a label", q.Key)
				}
				if !slices.Contains(QuestionTypes, q.Type) && q.Type != "" {
					bad("the question %q is of a type there is not: %s", q.Key, strings.Join(QuestionTypes, ", "))
				}
				if q.Type == QuestionChoice {
					values := map[string]bool{}
					for _, o := range q.Options {
						if strings.TrimSpace(o.Value) == "" {
							bad("each choice of %q needs a value", q.Key)
						} else if values[o.Value] {
							bad("the question %q offers %q twice", q.Key, o.Value)
						}
						values[o.Value] = true
					}
					if len(q.Options) == 0 {
						bad("the question %q needs choices to choose from", q.Key)
					}
				} else if len(q.Options) > 0 {
					bad("only a choice question has choices: %q", q.Key)
				}
				uses("the default of "+q.Key, q.Default)
				if len(References(q.Default)) == 0 {
					if p := q.Problem(q.Default); p != "" {
						bad("the default of %q %s", q.Key, p)
					}
				}
				seen[q.Key] = true
			}
			for _, q := range s.Questions {
				set[q.Key] = true
				if q.Type == QuestionPhoto {
					photos[q.Key] = true
				}
			}
		case KindSetFields:
			on := d.SetsOn(i)
			onNew := on == OnNew
			if on == "" {
				bad("it sets fields on nothing: put it before a Create, or after a Find or Create")
			}
			if len(s.Fields) == 0 && s.Photo == "" {
				bad("it needs at least one field to set")
			}
			seen := map[string]bool{}
			var keys []string
			for _, row := range s.Fields {
				switch {
				case row.Field == "":
					bad("choose the field each row sets")
					continue
				case !c.Fields[row.Field]:
					bad("no field the flow's categories record has the key %q", row.Field)
				case seen[row.Field]:
					bad("it sets %s twice", c.label(row.Field))
				}
				seen[row.Field] = true
				keys = append(keys, row.Field)
				uses("the value of "+row.Field, row.Value)
				if len(References(row.Value)) == 0 && row.Value != "" && c.Suits != nil && c.Fields[row.Field] {
					if err := c.Suits(row.Field, row.Value); err != nil {
						bad("%s cannot be %q: %v", c.label(row.Field), row.Value, err)
					}
				}
			}
			if s.Photo != "" {
				if q := s.PhotoQuestion(); q == "" || !photos[q] {
					bad("its photo is the answer to a photo question asked before it, such as {photo}")
				}
			}
			for _, k := range keys {
				set[k] = true
			}
			if onNew {
				pending = append(pending, filling{when: s.When, keys: keys})
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
			fieldsKnown = true
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
			// Every field a category it may file into requires is set by
			// a Set fields step before it, one with no condition or one
			// shown only for that category.
			into := filing
			if w := s.When; w != nil && w.Value == ValueCategoryID && w.Op == OpIs {
				into = []string{w.To}
			}
			for _, cat := range into {
				filled := map[string]bool{}
				for _, p := range pending {
					if p.when == nil || (p.when.Value == ValueCategoryID && p.when.Op == OpIs && p.when.To == cat) {
						for _, k := range p.keys {
							filled[k] = true
						}
					}
				}
				var missing []string
				for _, k := range c.Required[cat] {
					if !filled[k] {
						missing = append(missing, c.label(k))
					}
				}
				if len(missing) > 0 {
					name := c.Names[cat]
					if name == "" {
						name = cat
					}
					bad("%s requires %s, which no Set fields step before it sets", name, strings.Join(missing, ", "))
				}
			}
			pending = nil
			set[ValueAsset], set[ValueAssetID], set[ValueCode] = true, true, true
			// The asset filed brings its field values.
			fieldsKnown = true
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
				case ChangeHook:
					if !c.Hooks[ch.Hook] {
						bad("no hook has the key %q", ch.Hook)
					}
				case "fields":
					bad("a Change no longer sets fields: a Set fields step does")
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

// label is a field's label, or its key when the catalog has none.
func (c *Catalog) label(key string) string {
	if l := c.Labels[key]; l != "" {
		return l
	}
	return key
}

func isStepValue(name string) bool {
	switch name {
	case ValueCategory, ValueCategoryID, ValueAsset, ValueAssetID, ValueCode:
		return true
	}
	return false
}

// stepBases are what a new step's ID is made from, by kind, as the editor
// makes them (web/src/lib/flowEditor.ts newStepID).
var stepBases = map[string]string{
	KindChooseCategory: "kind", KindForm: "form", KindSetFields: "set", KindFind: "find", KindCreate: "file",
	KindLabel: "label", KindChange: "change", KindConfirm: "confirm", KindDone: "done",
}

// NewStepID is an ID for a new step of a kind, not among those taken, as
// the editor makes them: its kind's base, then base_2, base_3 and so on.
func NewStepID(kind string, taken map[string]bool) string {
	base := stepBases[kind]
	if base == "" {
		base = "step"
	}
	if !taken[base] {
		return base
	}
	for n := 2; ; n++ {
		if id := fmt.Sprintf("%s_%d", base, n); !taken[id] {
			return id
		}
	}
}

// Starter is a ready-made flow to adjust.
type Starter struct {
	Key         string     `json:"key"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Definition  Definition `json:"definition"`
}

// Starters are the two ready-made flows, filing into or taking from these
// categories: Leave something, and Collect it. Leave something asks for
// what it is and for each field the categories record, as fieldsOf gives
// them, and sets those fields: it is the version 1 starter, which asked
// for the categories' fields, converted (UpgradeV1). fieldsOf may be nil,
// for categories that record none.
func Starters(categoryIDs []string, fieldsOf func(categoryID string) ([]FieldDef, error)) ([]Starter, error) {
	cats := append([]string{}, categoryIDs...)
	v1 := v1Definition{Version: 1, Description: "Scan to leave something here, with your name on it.", CategoryIDs: cats, Steps: []v1Step{
		{Step: Step{ID: "kind", Kind: KindChooseCategory, Title: "What are you leaving?"}},
		{Step: Step{ID: "about", Kind: KindForm, Title: "About it"}, AskName: true, NameLabel: "What is it?"},
		{Step: Step{ID: "file", Kind: KindCreate, Name: "{name}"}},
		{Step: Step{ID: "label", Kind: KindLabel, Label: &Label{Mode: LabelShow}}},
		{Step: Step{ID: "done", Kind: KindDone, Title: "Filed", Text: "{asset} is filed. Put its label on it."}},
	}}
	if len(cats) == 1 {
		v1.Steps = v1.Steps[1:]
	}
	leave, err := upgradeV1(v1, fieldsOf)
	if err != nil {
		return nil, err
	}
	collect := Definition{Version: Version, Description: "Scan to collect something you left here.", CategoryIDs: cats, Steps: []Step{
		{ID: "find", Kind: KindFind, Title: "What are you collecting?", FindBy: []string{FindScan, FindSearch, FindList}},
		{ID: "confirm", Kind: KindConfirm, Title: "Collect it?", Text: "{asset} will be taken off the list."},
		{ID: "finish", Kind: KindChange, Changes: []Change{{Kind: ChangeArchive}}},
		{ID: "done", Kind: KindDone, Title: "Collected", Text: "{asset} is collected."},
	}}
	return []Starter{
		{Key: "leave", Name: "Leave something", Description: "Choose what kind of thing it is, fill in its form, set its fields from the answers, file it and label it.", Definition: leave},
		{Key: "collect", Name: "Collect it", Description: "Find it by its code, by search or from a list, then archive it.", Definition: collect},
	}, nil
}
