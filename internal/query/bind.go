package query

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/fields"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/generators"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// Schema is what a query may name: the instance's fields, by key, and
// whether someone is signed in, which `me` and favorite need.
type Schema struct {
	Fields   map[string]models.Field
	SignedIn bool
}

// Env is what matching needs beyond the asset: the time relative dates
// count from, who `me` is, their favorites, every category by ID (to
// match an asset by the categories above its own), and Plain, which
// matches a term as the search box does.
type Env struct {
	Now        time.Time
	Me         string
	Favorites  map[string]bool
	Categories map[string]models.Category
	Plain      func(a *models.AssetWithDetails, term string) bool
}

type matcher func(a *models.AssetWithDetails, env *Env) bool

// Plan is a query checked against a schema, ready to match assets.
type Plan struct {
	match matcher
	order []sorter
	// Terms are the plain words in it, which Env.Plain is asked about.
	Terms []string
	// Archived is set when it asks whether assets are archived, so they
	// must be among those it is matched against.
	Archived bool
}

// Match reports whether an asset matches.
func (p *Plan) Match(a *models.AssetWithDetails, env *Env) bool {
	return p.match == nil || p.match(a, env)
}

// Sort puts assets in the plan's order, if it has one, keeping the order
// they came in among equals.
func (p *Plan) Sort(assets []models.AssetWithDetails, env *Env) {
	if len(p.order) == 0 {
		return
	}
	sort.SliceStable(assets, func(i, j int) bool {
		for _, s := range p.order {
			if c := s(&assets[i], &assets[j], env); c != 0 {
				return c < 0
			}
		}
		return false
	})
}

// Compile parses a query and checks it against a schema. Its errors are
// *Error.
func Compile(input string, s Schema) (*Plan, error) {
	q, err := Parse(input)
	if err != nil {
		return nil, err
	}
	return Bind(q, s)
}

// Bind checks a parsed query against a schema.
func Bind(q *Query, s Schema) (*Plan, error) {
	b := &binder{schema: s, plan: &Plan{}}
	if q.Where != nil {
		m, err := b.node(q.Where)
		if err != nil {
			return nil, err
		}
		b.plan.match = m
	}
	for _, o := range q.Order {
		srt, err := b.sorter(o)
		if err != nil {
			return nil, err
		}
		b.plan.order = append(b.plan.order, srt)
	}
	return b.plan, nil
}

type binder struct {
	schema Schema
	plan   *Plan
}

func (b *binder) node(n Node) (matcher, error) {
	switch n := n.(type) {
	case And:
		l, err := b.node(n.L)
		if err != nil {
			return nil, err
		}
		r, err := b.node(n.R)
		if err != nil {
			return nil, err
		}
		return func(a *models.AssetWithDetails, e *Env) bool { return l(a, e) && r(a, e) }, nil
	case Or:
		l, err := b.node(n.L)
		if err != nil {
			return nil, err
		}
		r, err := b.node(n.R)
		if err != nil {
			return nil, err
		}
		return func(a *models.AssetWithDetails, e *Env) bool { return l(a, e) || r(a, e) }, nil
	case Not:
		x, err := b.node(n.X)
		if err != nil {
			return nil, err
		}
		return func(a *models.AssetWithDetails, e *Env) bool { return !x(a, e) }, nil
	case Term:
		term := n.Text
		b.plan.Terms = append(b.plan.Terms, term)
		return func(a *models.AssetWithDetails, e *Env) bool { return e.Plain != nil && e.Plain(a, term) }, nil
	case Compare:
		k, err := b.key(n.Key)
		if err != nil {
			return nil, err
		}
		return k.compare(n.Op, n.Value)
	case In:
		k, err := b.key(n.Key)
		if err != nil {
			return nil, err
		}
		var any []matcher
		for _, v := range n.Values {
			m, err := k.compare("=", v)
			if err != nil {
				return nil, err
			}
			any = append(any, m)
		}
		m := func(a *models.AssetWithDetails, e *Env) bool {
			for _, m := range any {
				if m(a, e) {
					return true
				}
			}
			return false
		}
		if n.Negate {
			return func(a *models.AssetWithDetails, e *Env) bool { return !m(a, e) }, nil
		}
		return m, nil
	case Empty:
		k, err := b.key(n.Key)
		if err != nil {
			return nil, err
		}
		if k.empty == nil {
			return nil, errAt(n.s, "%s is never empty", k.name)
		}
		empty := k.empty
		if n.Negate {
			return func(a *models.AssetWithDetails, e *Env) bool { return !empty(a, e) }, nil
		}
		return empty, nil
	}
	panic(fmt.Sprintf("query: unknown node %T", n))
}

// key is what a key looks at, and how its values compare.
type key struct {
	name string
	s    span
	// compare builds a matcher for op and value; ops lists those it takes.
	compare func(op string, v Value) (matcher, error)
	empty   matcher
	// has reports whether an asset has a value to sort by, and cmp orders
	// two that do; cmp is nil for a key nothing is sorted by.
	has func(a *models.AssetWithDetails, env *Env) bool
	cmp func(a, b *models.AssetWithDetails, env *Env) int
}

// keys are the keys that aren't a field or a code type, for messages.
const keyList = "name, code, category, in, condition, loan, assigned, favorite, archived, created, updated, quantity, or field. and a field's key"

func (b *binder) key(k Key) (*key, error) {
	name := k.Name
	switch {
	case name == "name":
		return textKey(k, func(a *models.AssetWithDetails, _ *Env) []string { return []string{a.Asset.Name} }), nil
	case name == "code":
		return textKey(k, func(a *models.AssetWithDetails, _ *Env) []string {
			var out []string
			for _, gk := range a.GeneratorKeys {
				out = append(out, gk.AssignedCode)
			}
			return out
		}), nil
	case strings.HasPrefix(name, "code."):
		t := strings.TrimPrefix(name, "code.")
		if !generators.IsCodeType(t) {
			return nil, errAt(k.s, "there is no code type %q: use one of %s", t, strings.Join(generators.CodeTypes(), ", "))
		}
		kk := textKey(k, func(a *models.AssetWithDetails, _ *Env) []string {
			for _, gk := range a.GeneratorKeys {
				if gk.GeneratorType == t {
					return []string{gk.AssignedCode}
				}
			}
			return nil
		})
		if t == generators.TypeNFC {
			// A tag's serial number is found however it is written.
			inner := kk.compare
			kk.compare = func(op string, v Value) (matcher, error) {
				if op == "=" || op == "!=" {
					v.Text = generators.NormalizeCode(t, v.Text)
				}
				return inner(op, v)
			}
		}
		return kk, nil
	case name == "category":
		return b.categoryKey(k), nil
	case name == "in":
		kk := textKey(k, func(a *models.AssetWithDetails, _ *Env) []string {
			var out []string
			for _, p := range a.Location {
				out = append(out, p.Name)
			}
			return out
		})
		return kk, nil
	case name == "condition":
		return enumKey(k, map[string]func(a *models.AssetWithDetails, e *Env) bool{
			models.ConditionOK: func(a *models.AssetWithDetails, _ *Env) bool { return a.Asset.Condition == models.ConditionOK },
			models.ConditionNeedsMaintenance: func(a *models.AssetWithDetails, _ *Env) bool {
				return a.Asset.Condition == models.ConditionNeedsMaintenance
			},
			models.ConditionOutOfService: func(a *models.AssetWithDetails, _ *Env) bool {
				return a.Asset.Condition == models.ConditionOutOfService
			},
			"problem": func(a *models.AssetWithDetails, _ *Env) bool { return a.Asset.Condition != models.ConditionOK },
		}, nil, func(a, c *models.AssetWithDetails, _ *Env) int {
			return compareStrings(a.Asset.Condition, c.Asset.Condition)
		}), nil
	case name == "loan":
		lent := func(a *models.AssetWithDetails, _ *Env) bool { return a.Loan != nil && a.Loan.CheckedInAt == nil }
		return enumKey(k, map[string]func(a *models.AssetWithDetails, e *Env) bool{
			"open": lent,
			"overdue": func(a *models.AssetWithDetails, e *Env) bool {
				return lent(a, e) && a.Loan.DueAt != nil && a.Loan.DueAt.Before(e.Now)
			},
		}, func(a *models.AssetWithDetails, e *Env) bool { return !lent(a, e) }, nil), nil
	case name == "assigned":
		return peopleKey(k, b.schema.SignedIn, func(a *models.AssetWithDetails) []string {
			var ids []string
			for key := range a.PersonFields {
				ids = append(ids, fields.PersonIDs(a.FieldValues[key])...)
			}
			return ids
		}, func(a *models.AssetWithDetails) bool { return len(a.HiddenPeople) > 0 }), nil
	case name == "favorite":
		if !b.schema.SignedIn {
			return nil, errAt(k.s, "favorite needs someone signed in, whose favorites they are")
		}
		return boolKey(k, func(a *models.AssetWithDetails, e *Env) bool { return e.Favorites[a.Asset.ID] }), nil
	case name == "archived":
		b.plan.Archived = true
		return boolKey(k, func(a *models.AssetWithDetails, _ *Env) bool { return a.Asset.ArchivedAt != nil }), nil
	case name == "created":
		return dateKey(k, func(a *models.AssetWithDetails) (time.Time, bool) { return a.Asset.CreatedAt, true }), nil
	case name == "updated":
		return dateKey(k, func(a *models.AssetWithDetails) (time.Time, bool) { return a.Asset.UpdatedAt, true }), nil
	case name == "quantity":
		return numberKey(k, func(a *models.AssetWithDetails) (float64, bool) {
			if a.Asset.Stock == nil {
				return 0, false
			}
			return float64(a.Asset.Stock.Quantity), true
		}), nil
	case strings.HasPrefix(name, "field."):
		return b.fieldKey(k, strings.TrimPrefix(name, "field."))
	}
	return nil, errAt(k.s, "there is nothing called %q to search by: use %s", k.Name, keyList)
}

// applies reports whether a field is one of an asset's: a value kept from
// a category it has left is not searched.
func applies(a *models.AssetWithDetails, key string) bool {
	for _, f := range a.EffectiveFields {
		if f.Field.Key == key {
			return true
		}
	}
	return false
}

func (b *binder) fieldKey(k Key, fieldKey string) (*key, error) {
	f, ok := b.schema.Fields[fieldKey]
	if !ok {
		return nil, errAt(k.s, "no field has the key %q", fieldKey)
	}
	value := func(a *models.AssetWithDetails) (string, bool) {
		if !applies(a, fieldKey) {
			return "", false
		}
		v := strings.TrimSpace(a.FieldValues[fieldKey])
		return v, v != ""
	}
	switch f.ValidationType {
	case fields.TypeNumber:
		return numberKey(k, func(a *models.AssetWithDetails) (float64, bool) {
			v, ok := value(a)
			if !ok {
				return 0, false
			}
			n, err := strconv.ParseFloat(v, 64)
			return n, err == nil
		}), nil
	case fields.TypeDate:
		return dateKey(k, func(a *models.AssetWithDetails) (time.Time, bool) {
			v, ok := value(a)
			if !ok {
				return time.Time{}, false
			}
			t, _, ok := parseDate(v)
			return t, ok
		}), nil
	case fields.TypeSelect:
		opts, err := fields.ValidateSelectOptions(f.Options)
		if err != nil {
			return nil, errAt(k.s, "the field %s's choices can't be read", fieldKey)
		}
		labels := map[string]string{}
		var names []string
		for _, c := range opts.Choices {
			labels[c.Value] = c.Label
			names = append(names, c.Label)
		}
		kk := textKey(k, func(a *models.AssetWithDetails, _ *Env) []string {
			v, ok := value(a)
			if !ok {
				return nil
			}
			return []string{v, labels[v]}
		})
		inner := kk.compare
		kk.compare = func(op string, v Value) (matcher, error) {
			if op == "=" || op == "!=" {
				found := false
				for _, c := range opts.Choices {
					if fold(c.Value) == fold(v.Text) || fold(c.Label) == fold(v.Text) {
						found = true
					}
				}
				if !found {
					return nil, errAt(v.s, "%s isn't one of %s's choices: %s", v.Text, fieldKey, strings.Join(names, ", "))
				}
			}
			return inner(op, v)
		}
		return kk, nil
	case fields.TypePerson:
		return peopleKey(k, b.schema.SignedIn, func(a *models.AssetWithDetails) []string {
			if !applies(a, fieldKey) {
				return nil
			}
			return fields.PersonIDs(a.FieldValues[fieldKey])
		}, func(a *models.AssetWithDetails) bool { return a.HiddenPeople[fieldKey] > 0 }), nil
	}
	return textKey(k, func(a *models.AssetWithDetails, _ *Env) []string {
		v, ok := value(a)
		if !ok {
			return nil
		}
		return []string{v}
	}), nil
}

func (b *binder) categoryKey(k Key) *key {
	// The categories an asset is in: its own, those above them, and those
	// it counts in through categories implying others.
	names := func(a *models.AssetWithDetails, e *Env) []string {
		seen := map[string]bool{}
		var out []string
		add := func(id string) {
			for d := 0; id != "" && d < 100 && !seen[id]; d++ {
				seen[id] = true
				c, ok := e.Categories[id]
				if !ok {
					return
				}
				out = append(out, c.Name, categoryPath(c, e), c.ID)
				if c.ParentID == nil {
					return
				}
				id = *c.ParentID
			}
		}
		for _, c := range a.Categories {
			add(c.ID)
		}
		for _, c := range a.CountsIn {
			add(c.CategoryID)
		}
		return out
	}
	kk := textKey(k, names)
	kk.empty = nil
	kk.has = func(a *models.AssetWithDetails, _ *Env) bool { return len(a.Categories) > 0 }
	kk.cmp = func(a, c *models.AssetWithDetails, _ *Env) int {
		return compareStrings(a.Categories[0].Name, c.Categories[0].Name)
	}
	return kk
}

// categoryPath is a category's name after its ancestors', as "Shop >
// Saws".
func categoryPath(c models.Category, e *Env) string {
	parts := []string{c.Name}
	for d := 0; c.ParentID != nil && d < 100; d++ {
		p, ok := e.Categories[*c.ParentID]
		if !ok {
			break
		}
		parts = append([]string{p.Name}, parts...)
		c = p
	}
	return strings.Join(parts, " > ")
}

// fold is text as compared: every letter in lower case.
func fold(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func compareStrings(a, b string) int { return strings.Compare(fold(a), fold(b)) }

// textKey compares text, ignoring case: = and != whole values, ~ and !~
// part of one. values lists what an asset has.
func textKey(k Key, values func(a *models.AssetWithDetails, e *Env) []string) *key {
	kk := &key{name: k.Name, s: k.s}
	kk.compare = func(op string, v Value) (matcher, error) {
		want := fold(v.Text)
		var m matcher
		switch op {
		case "=", "!=":
			m = func(a *models.AssetWithDetails, e *Env) bool {
				for _, have := range values(a, e) {
					if fold(have) == want {
						return true
					}
				}
				return false
			}
		case "~", "!~":
			m = func(a *models.AssetWithDetails, e *Env) bool {
				for _, have := range values(a, e) {
					if strings.Contains(fold(have), want) {
						return true
					}
				}
				return false
			}
		default:
			return nil, errAt(span{k.s.start, v.s.end}, "%s is text: compare it with =, !=, ~ or !~, not %s", k.Name, op)
		}
		if strings.HasPrefix(op, "!") {
			return func(a *models.AssetWithDetails, e *Env) bool { return !m(a, e) }, nil
		}
		return m, nil
	}
	kk.empty = func(a *models.AssetWithDetails, e *Env) bool {
		for _, v := range values(a, e) {
			if strings.TrimSpace(v) != "" {
				return false
			}
		}
		return true
	}
	kk.has = func(a *models.AssetWithDetails, e *Env) bool { return len(values(a, e)) > 0 }
	kk.cmp = func(a, c *models.AssetWithDetails, e *Env) int {
		return compareStrings(values(a, e)[0], values(c, e)[0])
	}
	return kk
}

// enumKey compares against a fixed set of values, each with what it
// matches; empty is what IS EMPTY matches, nil when nothing is empty.
func enumKey(k Key, is map[string]func(a *models.AssetWithDetails, e *Env) bool, empty matcher, sortBy func(a, b *models.AssetWithDetails, e *Env) int) *key {
	var names []string
	for n := range is {
		names = append(names, n)
	}
	sort.Strings(names)
	kk := &key{name: k.Name, s: k.s, empty: empty, cmp: sortBy}
	kk.has = func(*models.AssetWithDetails, *Env) bool { return true }
	kk.compare = func(op string, v Value) (matcher, error) {
		want := strings.ReplaceAll(fold(v.Text), " ", "_")
		m, ok := is[want]
		if !ok {
			return nil, errAt(v.s, "%s is one of %s, not %s", k.Name, strings.Join(names, ", "), v.Text)
		}
		switch op {
		case "=":
			return m, nil
		case "!=":
			return func(a *models.AssetWithDetails, e *Env) bool { return !m(a, e) }, nil
		}
		return nil, errAt(span{k.s.start, v.s.end}, "compare %s with = or !=, not %s", k.Name, op)
	}
	return kk
}

func boolKey(k Key, is matcher) *key {
	kk := &key{name: k.Name, s: k.s}
	kk.compare = func(op string, v Value) (matcher, error) {
		var want bool
		switch fold(v.Text) {
		case "true", "yes":
			want = true
		case "false", "no":
		default:
			return nil, errAt(v.s, "%s is true or false, not %s", k.Name, v.Text)
		}
		if op == "!=" {
			want = !want
		} else if op != "=" {
			return nil, errAt(span{k.s.start, v.s.end}, "compare %s with = or !=, not %s", k.Name, op)
		}
		return func(a *models.AssetWithDetails, e *Env) bool { return is(a, e) == want }, nil
	}
	return kk
}

// peopleKey matches people by name, or `me`, ignoring case. ids lists the
// people an asset's fields name that the requester may see; hidden
// reports whether they name others.
func peopleKey(k Key, signedIn bool, ids func(a *models.AssetWithDetails) []string, hidden func(a *models.AssetWithDetails) bool) *key {
	names := func(a *models.AssetWithDetails, _ *Env) []string {
		var out []string
		for _, id := range ids(a) {
			if p, ok := a.People[id]; ok {
				out = append(out, p.Name)
			}
		}
		return out
	}
	kk := textKey(k, names)
	text := kk.compare
	kk.compare = func(op string, v Value) (matcher, error) {
		if !v.Quoted && fold(v.Text) == "me" && (op == "=" || op == "!=") {
			if !signedIn {
				return nil, errAt(v.s, "me needs someone signed in")
			}
			m := func(a *models.AssetWithDetails, e *Env) bool {
				for _, id := range ids(a) {
					if id == e.Me {
						return true
					}
				}
				return false
			}
			if op == "!=" {
				return func(a *models.AssetWithDetails, e *Env) bool { return !m(a, e) }, nil
			}
			return m, nil
		}
		return text(op, v)
	}
	kk.empty = func(a *models.AssetWithDetails, _ *Env) bool { return len(ids(a)) == 0 && !hidden(a) }
	return kk
}

func numberKey(k Key, value func(a *models.AssetWithDetails) (float64, bool)) *key {
	kk := &key{name: k.Name, s: k.s}
	kk.compare = func(op string, v Value) (matcher, error) {
		want, err := strconv.ParseFloat(strings.TrimSpace(v.Text), 64)
		if err != nil {
			return nil, errAt(v.s, "%s is a number, and %s isn't one", k.Name, v.Text)
		}
		var test func(n float64) bool
		switch op {
		case "=", "!=":
			test = func(n float64) bool { return n == want }
		case "<":
			test = func(n float64) bool { return n < want }
		case "<=":
			test = func(n float64) bool { return n <= want }
		case ">":
			test = func(n float64) bool { return n > want }
		case ">=":
			test = func(n float64) bool { return n >= want }
		default:
			return nil, errAt(span{k.s.start, v.s.end}, "%s is a number: compare it with =, !=, <, <=, > or >=, not %s", k.Name, op)
		}
		m := func(a *models.AssetWithDetails, _ *Env) bool {
			n, ok := value(a)
			return ok && test(n)
		}
		if op == "!=" {
			return func(a *models.AssetWithDetails, e *Env) bool { return !m(a, e) }, nil
		}
		return m, nil
	}
	kk.empty = func(a *models.AssetWithDetails, _ *Env) bool { _, ok := value(a); return !ok }
	kk.has = func(a *models.AssetWithDetails, _ *Env) bool { _, ok := value(a); return ok }
	kk.cmp = func(a, c *models.AssetWithDetails, _ *Env) int {
		an, _ := value(a)
		cn, _ := value(c)
		switch {
		case an < cn:
			return -1
		case an > cn:
			return 1
		}
		return 0
	}
	return kk
}

// parseDate reads a date as a field holds one: a day, or an instant. day
// is set for a day.
func parseDate(s string) (t time.Time, day bool, ok bool) {
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, true, true
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), false, true
		}
	}
	return time.Time{}, false, false
}

// dateRange reads a date to compare with: a day (2026-03-01), an instant,
// today, now, or a time before or after now (-30d, +2w; h hours, d days,
// w weeks, m months, y years). It is the stretch [from, to): a day
// spans the day in UTC; anything else is one instant, but compared with =
// spans the day it falls in.
func dateRange(v Value, now time.Time, op string) (from, to time.Time, err error) {
	text := fold(v.Text)
	day := func(t time.Time) (time.Time, time.Time) {
		d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		return d, d.AddDate(0, 0, 1)
	}
	var at time.Time
	switch {
	case text == "today":
		f, t := day(now.UTC())
		return f, t, nil
	case text == "now":
		at = now.UTC()
	case strings.HasPrefix(text, "-") || strings.HasPrefix(text, "+"):
		n, unit := text[1:len(text)-1], text[len(text)-1:]
		count, perr := strconv.Atoi(n)
		if len(text) < 3 || perr != nil {
			return from, to, errAt(v.s, "%s isn't a date: write 2026-03-01, today, or a time from now such as -30d", v.Text)
		}
		if text[0] == '-' {
			count = -count
		}
		switch unit {
		case "h":
			at = now.Add(time.Duration(count) * time.Hour)
		case "d":
			at = now.AddDate(0, 0, count)
		case "w":
			at = now.AddDate(0, 0, 7*count)
		case "m":
			at = now.AddDate(0, count, 0)
		case "y":
			at = now.AddDate(count, 0, 0)
		default:
			return from, to, errAt(v.s, "%s isn't a time from now: end it with h, d, w, m or y", v.Text)
		}
		at = at.UTC()
	default:
		t, isDay, ok := parseDate(v.Text)
		if !ok {
			return from, to, errAt(v.s, "%s isn't a date: write 2026-03-01, today, or a time from now such as -30d", v.Text)
		}
		if isDay {
			f, t := day(t)
			return f, t, nil
		}
		at = t
	}
	if op == "=" || op == "!=" {
		f, t := day(at)
		return f, t, nil
	}
	return at, at, nil
}

func dateKey(k Key, value func(a *models.AssetWithDetails) (time.Time, bool)) *key {
	kk := &key{name: k.Name, s: k.s}
	kk.compare = func(op string, v Value) (matcher, error) {
		switch op {
		case "=", "!=", "<", "<=", ">", ">=":
		default:
			return nil, errAt(span{k.s.start, v.s.end}, "%s is a date: compare it with =, !=, <, <=, > or >=, not %s", k.Name, op)
		}
		// Check the date now, so a mistake is shown before anything is
		// matched; relative dates are read again at match time.
		if _, _, err := dateRange(v, time.Now(), op); err != nil {
			return nil, err
		}
		m := func(a *models.AssetWithDetails, e *Env) bool {
			t, ok := value(a)
			if !ok {
				return false
			}
			from, to, _ := dateRange(v, e.Now, op)
			point := from.Equal(to)
			switch op {
			case "=", "!=":
				return !t.Before(from) && t.Before(to)
			case "<":
				return t.Before(from)
			case "<=":
				if point {
					return !t.After(from)
				}
				return t.Before(to)
			case ">":
				if point {
					return t.After(from)
				}
				return !t.Before(to)
			}
			return !t.Before(from) // >=
		}
		if op == "!=" {
			return func(a *models.AssetWithDetails, e *Env) bool { return !m(a, e) }, nil
		}
		return m, nil
	}
	kk.empty = func(a *models.AssetWithDetails, _ *Env) bool { _, ok := value(a); return !ok }
	kk.has = func(a *models.AssetWithDetails, _ *Env) bool { _, ok := value(a); return ok }
	kk.cmp = func(a, c *models.AssetWithDetails, _ *Env) int {
		at, _ := value(a)
		ct, _ := value(c)
		return at.Compare(ct)
	}
	return kk
}

func (b *binder) sorter(o Order) (sorter, error) {
	k, err := b.key(o.Key)
	if err != nil {
		return nil, err
	}
	if k.cmp == nil {
		return nil, errAt(o.Key.s, "assets can't be sorted by %s", o.Key.Name)
	}
	has, cmp, desc := k.has, k.cmp, o.Desc
	// Assets without a value come last, whichever way the rest go.
	return func(a, c *models.AssetWithDetails, e *Env) int {
		ah, ch := has(a, e), has(c, e)
		switch {
		case ah && ch:
			if desc {
				return -cmp(a, c, e)
			}
			return cmp(a, c, e)
		case ah:
			return -1
		case ch:
			return 1
		}
		return 0
	}, nil
}

type sorter func(a, b *models.AssetWithDetails, env *Env) int
