package assetcsv

import (
	"sort"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
	"github.com/google/uuid"
)

// maxLinkChanges bounds the link changes a plan lists; the counts have them
// all.
const maxLinkChanges = 500

// LinkChange is a link an import would add, move or remove, for the
// preview: Asset Label Other, as "Screws contained in Bin A3".
type LinkChange struct {
	Row    int    `json:"row,omitempty"`
	Change string `json:"change"` // add, move or remove
	Asset  string `json:"asset"`
	Label  string `json:"label"`
	Other  string `json:"other"`
}

// linkCell is a row's links cell, with the asset the row is about.
type linkCell struct {
	line        int
	column      string
	self, value string
}

// verb is a relation read one way, as a links cell names it.
type verb struct {
	t        models.RelationType
	outgoing bool
}

// plannedLink is a link between two assets, from its from end.
type plannedLink struct {
	typeID, from, to string
	line             int
}

func linkKey(typeID, from, to string, symmetric bool) string {
	if symmetric && to < from {
		from, to = to, from
	}
	return typeID + "\x00" + from + "\x00" + to
}

// planLinks plans the links the file's links cells state: adding those that
// do not exist, moving what a single relation puts somewhere new, and, with
// remove, removing links of the file's assets that no row states.
func (p *planner) planLinks(remove bool) error {
	plan := p.plan
	types, err := p.src.ListRelationTypes()
	if err != nil {
		return err
	}
	verbs := map[string][]verb{}
	byID := map[string]models.RelationType{}
	for _, t := range types {
		byID[t.ID] = t
		verbs[store.RelationSlug(t.Name)] = append(verbs[store.RelationSlug(t.Name)], verb{t, true})
		if !t.IsSymmetric {
			verbs[store.RelationSlug(t.InverseName)] = append(verbs[store.RelationSlug(t.InverseName)], verb{t, false})
		}
	}

	// The names and codes the assets will have once the rows are applied.
	refs := &AssetRefs{names: map[string]string{}, byName: map[string][]string{}, byCode: map[string][]string{}}
	changed := map[string]store.ImportRow{}
	for _, c := range plan.Changes {
		if c.ID != "" {
			changed[c.ID] = c
		} else {
			changed[c.NewID] = c
		}
	}
	for id, a := range p.assets {
		name, codes := a.Asset.Name, map[string]string{}
		for _, gk := range a.GeneratorKeys {
			codes[gk.GeneratorType] = gk.AssignedCode
		}
		if c, ok := changed[id]; ok {
			name = c.Name
			for t, code := range c.Codes {
				codes[t] = code
			}
		}
		refs.add(id, name, mapValues(codes))
	}
	for id, c := range changed {
		if _, ok := p.assets[id]; !ok {
			refs.add(id, c.Name, mapValues(c.Codes))
		}
	}
	nameOf := func(id string) string {
		if n, ok := refs.names[id]; ok {
			return n
		}
		if a, err := p.src.GetAssetByID(id); err == nil {
			return a.Asset.Name
		}
		return id
	}

	// The links that exist, from their from ends.
	existing := map[string]plannedLink{}
	existingID := map[string]string{}
	for id, a := range p.assets {
		for _, l := range a.Links {
			from, to := id, l.Asset.ID
			if !l.Outgoing {
				from, to = to, from
			}
			k := linkKey(l.TypeID, from, to, byID[l.TypeID].IsSymmetric)
			existing[k] = plannedLink{typeID: l.TypeID, from: from, to: to}
			existingID[k] = l.ID
		}
	}

	// What the file states.
	stated := map[string]plannedLink{}
	var order []string
	inFile := map[string]bool{}
	for _, c := range p.linkCells {
		inFile[c.self] = true
		for _, entry := range strings.Split(c.value, LinkSeparator) {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}
			v, ref, ok := strings.Cut(entry, ":")
			v, ref = strings.TrimSpace(v), strings.TrimSpace(ref)
			if !ok || v == "" || ref == "" {
				plan.problem(c.line, c.column, "%q is not verb:asset, such as contained_in:Bin A3", entry)
				continue
			}
			choices := verbs[store.RelationSlug(v)]
			if len(choices) != 1 {
				what := "no relation is written %q"
				if len(choices) > 1 {
					what = "several relations are written %q; rename one"
				}
				plan.problem(c.line, c.column, what, store.RelationSlug(v))
				continue
			}
			other, ok := p.resolveRef(ref, refs, c)
			if !ok {
				continue
			}
			if other == c.self {
				plan.problem(c.line, c.column, "an asset cannot be linked to itself (%s)", entry)
				continue
			}
			vb := choices[0]
			from, to := c.self, other
			if !vb.outgoing {
				from, to = other, c.self
			}
			k := linkKey(vb.t.ID, from, to, vb.t.IsSymmetric)
			if _, dup := stated[k]; !dup {
				stated[k] = plannedLink{typeID: vb.t.ID, from: from, to: to, line: c.line}
				order = append(order, k)
			}
		}
	}

	// A single relation's far end has one link, here or already.
	where := map[string][]plannedLink{}
	for _, k := range order {
		l := stated[k]
		if byID[l.typeID].IsSingle {
			where[l.typeID+"\x00"+l.to] = append(where[l.typeID+"\x00"+l.to], l)
		}
	}
	for _, group := range where {
		for _, l := range group[1:] {
			plan.problem(l.line, p.linksColumn(), "%s would be %s both %s and %s", nameOf(l.to), byID[l.typeID].InverseName, nameOf(group[0].from), nameOf(l.from))
		}
	}

	// Adding what is new, moving what a single relation moves.
	removed := map[string]bool{}
	var removals, additions []store.ImportLink
	changes := []LinkChange{}
	// A link changes both its assets, so the importer must be able to
	// change each; a removal is reported on the row of its asset in the file.
	lineOf := map[string]int{}
	for _, c := range p.linkCells {
		if lineOf[c.self] == 0 {
			lineOf[c.self] = c.line
		}
	}
	guarded := map[string]bool{}
	guard := func(line int, ids ...string) {
		for _, id := range ids {
			if line == 0 {
				line = lineOf[id]
			}
		}
		for _, id := range ids {
			if bad := p.forbidden(id); bad != "" && !guarded[id] {
				guarded[id] = true
				plan.problem(line, p.linksColumn(), "you cannot change the links of %s, in %s", nameOf(id), bad)
			}
		}
	}
	note := func(line int, change string, l plannedLink) {
		guard(line, l.from, l.to)
		changes = append(changes, LinkChange{Row: line, Change: change, Asset: nameOf(l.from), Label: byID[l.typeID].Name, Other: nameOf(l.to)})
	}
	for _, k := range order {
		l := stated[k]
		if _, ok := existing[k]; ok {
			continue
		}
		for _, end := range []string{l.from, l.to} {
			if _, active := refs.names[end]; !active {
				plan.problem(l.line, p.linksColumn(), "%s is archived; restore it to link it", nameOf(end))
			}
		}
		change := "add"
		if byID[l.typeID].IsSingle {
			for ek, e := range existing {
				if e.typeID == l.typeID && e.to == l.to && !removed[ek] {
					removed[ek] = true
					guard(l.line, e.from)
					removals = append(removals, store.ImportLink{Remove: true, LinkID: existingID[ek]})
					change = "move"
				}
			}
		}
		additions = append(additions, store.ImportLink{TypeID: l.typeID, From: l.from, To: l.to})
		note(l.line, change, l)
	}

	// Removing what no row states, when asked.
	if remove {
		keys := make([]string, 0, len(existing))
		for k := range existing {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			e := existing[k]
			if removed[k] || (!inFile[e.from] && !inFile[e.to]) {
				continue
			}
			if _, ok := stated[k]; ok {
				continue
			}
			removed[k] = true
			removals = append(removals, store.ImportLink{Remove: true, LinkID: existingID[k]})
			note(0, "remove", e)
		}
	}

	// No loops of single relations once it is done.
	final := map[string]map[string]plannedLink{}
	add := func(l plannedLink) {
		if !byID[l.typeID].IsSingle {
			return
		}
		if final[l.typeID] == nil {
			final[l.typeID] = map[string]plannedLink{}
		}
		final[l.typeID][l.to] = l
	}
	for k, e := range existing {
		if !removed[k] {
			add(e)
		}
	}
	for _, k := range order {
		if _, ok := existing[k]; !ok {
			add(stated[k])
		}
	}
	for typeID, up := range final {
		for start := range up {
			at, path := start, []plannedLink{}
			for depth := 0; depth <= len(up); depth++ {
				l, ok := up[at]
				if !ok {
					break
				}
				path = append(path, l)
				if l.from == start {
					line := 0
					for _, step := range path {
						if step.line > line {
							line = step.line
						}
					}
					if line > 0 && path[0].to == minKey(path) {
						plan.problem(line, p.linksColumn(), "%s links would make a loop through %s", byID[typeID].Name, nameOf(start))
					}
					break
				}
				at = l.from
			}
		}
	}

	for _, c := range changes {
		switch c.Change {
		case "add":
			plan.LinksAdded++
		case "move":
			plan.LinksMoved++
		case "remove":
			plan.LinksRemoved++
		}
	}
	if len(changes) > maxLinkChanges {
		changes = changes[:maxLinkChanges]
	}
	plan.LinkChanges = changes
	plan.Links = append(removals, additions...)
	return nil
}

// resolveRef finds the one asset a reference names, reporting it against
// the cell when it names none or several.
func (p *planner) resolveRef(ref string, refs *AssetRefs, c linkCell) (string, bool) {
	ids := refs.Resolve(ref)
	switch len(ids) {
	case 1:
		return ids[0], true
	case 0:
		// An archived asset, by its ID, is named; linking to it is refused
		// later unless the link exists.
		if _, err := uuid.Parse(ref); err == nil {
			if a, err := p.src.GetAssetByID(ref); err == nil && a.Asset.ArchivedAt != nil {
				return a.Asset.ID, true
			}
		}
		p.plan.problem(c.line, c.column, "no asset is %q", ref)
	default:
		p.plan.problem(c.line, c.column, "%q names %d assets; use its ID or a code", ref, len(ids))
	}
	return "", false
}

// linksColumn is the header of the column mapped to links.
func (p *planner) linksColumn() string {
	for _, c := range p.plan.Columns {
		if c.Target == ColumnLinks {
			return c.Header
		}
	}
	return ""
}

// minKey is the smallest far end in a loop, so a loop is reported once.
func minKey(path []plannedLink) string {
	least := path[0].to
	for _, l := range path {
		if l.to < least {
			least = l.to
		}
	}
	return least
}

func mapValues(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}
