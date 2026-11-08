package store

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// Following links through several hops (docs/data-model.md#following-links):
// the laser cutter depends on the extractor, which depends on the
// three-phase supply. Only types marked is_followed are followed. Each hop
// is read with a query or two for the whole of its frontier, never one per
// asset, and following is bounded by depth and by MaxGraphAssets, so a
// loop, or a very large graph, ends.

// Limits on following links.
const (
	// MaxGraphDepth is the most hops a graph follows, and DefaultGraphDepth
	// how many it follows when asked for none.
	MaxGraphDepth     = 10
	DefaultGraphDepth = 5
	// MaxGraphAssets is the most assets a graph holds: past it, the graph
	// is cut short and says so.
	MaxGraphAssets = 500
	// graphChunk is how many IDs one IN (...) list names.
	graphChunk = 200
)

// ErrInvalidGraph wraps the reason a graph cannot be followed as asked.
var ErrInvalidGraph = errors.New("invalid graph")

// GraphOptions are what a graph follows: which followed types (none for
// all of them), which way, and how far. Limit bounds its assets, at most
// MaxGraphAssets, which 0 means.
type GraphOptions struct {
	TypeIDs   []string
	Direction string
	Depth     int
	Limit     int
}

// CanRead says whether the requester may read an asset in these scopes.
type CanRead func(scopes []string) bool

// normalize fills in defaults and checks the options.
func (o *GraphOptions) normalize() error {
	if o.Direction == "" {
		o.Direction = models.GraphBoth
	}
	switch o.Direction {
	case models.GraphOut, models.GraphIn, models.GraphBoth:
	default:
		return fmt.Errorf("%w: direction must be out, in or both", ErrInvalidGraph)
	}
	if o.Depth == 0 {
		o.Depth = DefaultGraphDepth
	}
	if o.Depth < 1 || o.Depth > MaxGraphDepth {
		return fmt.Errorf("%w: depth must be from 1 to %d", ErrInvalidGraph, MaxGraphDepth)
	}
	if o.Limit <= 0 || o.Limit > MaxGraphAssets {
		o.Limit = MaxGraphAssets
	}
	return nil
}

// FollowedTypes lists the followed relation types among ids, or all of them
// when ids is empty. A type that is not followed, or not there, is refused.
func (s *Store) FollowedTypes(ids []string) ([]models.RelationType, error) {
	types, err := s.ListRelationTypes()
	if err != nil {
		return nil, err
	}
	byID := map[string]models.RelationType{}
	out := []models.RelationType{}
	for _, t := range types {
		if t.IsFollowed {
			byID[t.ID] = t
			if len(ids) == 0 {
				out = append(out, t)
			}
		}
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		t, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("%w: %q is not a followed link type", ErrInvalidGraph, id)
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// graphEdge is a link as following reads it.
type graphEdge struct {
	id, typeID, from, to string
}

// chunks splits ids into lists of at most graphChunk.
func chunks(ids []string) [][]string {
	var out [][]string
	for len(ids) > graphChunk {
		out = append(out, ids[:graphChunk])
		ids = ids[graphChunk:]
	}
	if len(ids) > 0 {
		out = append(out, ids)
	}
	return out
}

func anyArgs(ids []string) []any {
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return out
}

func typeIDs(types []models.RelationType) []string {
	ids := make([]string, len(types))
	for i, t := range types {
		ids[i] = t.ID
	}
	return ids
}

// readEdges reads the links of these types where, when among is false,
// either end is one of ids, and when it is true, both ends are.
func (s *Store) readEdges(types []models.RelationType, ids []string, among bool) ([]graphEdge, error) {
	if len(types) == 0 || len(ids) == 0 {
		return nil, nil
	}
	tids := typeIDs(types)
	seen := map[string]bool{}
	var out []graphEdge
	for _, chunk := range chunks(ids) {
		query := "SELECT l.id, l.relation_type_id, l.from_asset_id, l.to_asset_id FROM asset_links l WHERE l.relation_type_id IN (" + placeholders(len(tids)) + ")"
		args := anyArgs(tids)
		if among {
			// From this chunk, to any of them: every link among them is
			// read once, by its from end's chunk.
			query += " AND l.from_asset_id IN (" + placeholders(len(chunk)) + ") AND l.to_asset_id IN (" + placeholders(len(ids)) + ")"
			args = append(append(args, anyArgs(chunk)...), anyArgs(ids)...)
		} else {
			query += " AND (l.from_asset_id IN (" + placeholders(len(chunk)) + ") OR l.to_asset_id IN (" + placeholders(len(chunk)) + "))"
			args = append(append(args, anyArgs(chunk)...), anyArgs(chunk)...)
		}
		rows, err := s.Query(query, args...)
		if err != nil {
			return nil, fmt.Errorf("reading links: %w", err)
		}
		for rows.Next() {
			var e graphEdge
			if err := rows.Scan(&e.id, &e.typeID, &e.from, &e.to); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scanning link: %w", err)
			}
			if !seen[e.id] {
				seen[e.id] = true
				out = append(out, e)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	// The order of the rows is the database's: sort them, so a graph cut
	// short is cut the same way on each.
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out, nil
}

// graphNode is an asset as following reads it, before it is shown.
type graphNode struct {
	asset      models.Asset
	categories []models.GraphCategory
	scopes     []string
}

// readNodes reads these assets, with their categories and scopes, a chunk
// at a time. Unknown IDs are left out.
func (s *Store) readNodes(ctx *assetContext, ids []string) (map[string]*graphNode, error) {
	out := map[string]*graphNode{}
	if len(ids) == 0 {
		return out, nil
	}
	traits, err := s.contextTraits(ctx)
	if err != nil {
		return nil, err
	}
	for _, chunk := range chunks(ids) {
		rows, err := s.Query("SELECT id, name, archived_at, asset_condition, icon_name, label_icon_name FROM assets WHERE id IN ("+placeholders(len(chunk))+")", anyArgs(chunk)...)
		if err != nil {
			return nil, fmt.Errorf("reading assets: %w", err)
		}
		for rows.Next() {
			var (
				n               graphNode
				icon, labelIcon sql.NullString
			)
			if err := rows.Scan(&n.asset.ID, &n.asset.Name, &n.asset.ArchivedAt, &n.asset.Condition, &icon, &labelIcon); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scanning asset: %w", err)
			}
			if icon.Valid {
				n.asset.IconName = &icon.String
			}
			if labelIcon.Valid {
				n.asset.LabelIconName = &labelIcon.String
			}
			n.categories = []models.GraphCategory{}
			out[n.asset.ID] = &n
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		rows, err = s.Query(`SELECT ac.asset_id, c.id, c.name, c.color_hex FROM asset_categories ac
			JOIN categories c ON c.id = ac.category_id
			WHERE ac.asset_id IN (`+placeholders(len(chunk))+`) ORDER BY c.name, c.id`, anyArgs(chunk)...)
		if err != nil {
			return nil, fmt.Errorf("reading asset categories: %w", err)
		}
		for rows.Next() {
			var (
				id    string
				c     models.GraphCategory
				color sql.NullString
			)
			if err := rows.Scan(&id, &c.ID, &c.Name, &color); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scanning asset category: %w", err)
			}
			c.Color = color.String
			if n := out[id]; n != nil {
				n.categories = append(n.categories, c)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	for _, n := range out {
		cats := make([]string, len(n.categories))
		for i, c := range n.categories {
			cats[i] = c.ID
		}
		n.scopes = traits.scopesOf(cats)
	}
	return out, nil
}

// shownAssets makes graph assets of nodes, with the icons they are drawn
// with: their own or their categories', with params bound to their fields
// and in the state their condition shows.
func (s *Store) shownAssets(ctx *assetContext, ids []string, nodes map[string]*graphNode) ([]models.GraphAsset, error) {
	values := map[string]map[string]string{}
	for _, chunk := range chunks(ids) {
		rows, err := s.Query(`SELECT afv.asset_id, f.field_key, afv.value FROM asset_field_values afv
			JOIN fields f ON f.id = afv.field_id WHERE afv.asset_id IN (`+placeholders(len(chunk))+`)`, anyArgs(chunk)...)
		if err != nil {
			return nil, fmt.Errorf("reading field values: %w", err)
		}
		for rows.Next() {
			var id, key, value string
			if err := rows.Scan(&id, &key, &value); err != nil {
				rows.Close()
				return nil, err
			}
			if values[id] == nil {
				values[id] = map[string]string{}
			}
			values[id][key] = value
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	out := make([]models.GraphAsset, 0, len(ids))
	for _, id := range ids {
		n := nodes[id]
		cats := make([]string, len(n.categories))
		for i, c := range n.categories {
			cats[i] = c.ID
		}
		var states []string
		switch n.asset.Condition {
		case models.ConditionOutOfService:
			states = []string{models.IconStateOutOfService}
		case models.ConditionNeedsMaintenance:
			states = []string{models.IconStateNeedsMaintenance}
		}
		icon, err := s.contextIcon(ctx, &n.asset, models.RenditionScreen, cats, values[id], states)
		if err != nil {
			return nil, err
		}
		out = append(out, models.GraphAsset{
			ID: id, Name: n.asset.Name, Categories: n.categories, Icon: icon,
			Condition: n.asset.Condition, Archived: n.asset.ArchivedAt != nil,
		})
	}
	return out, nil
}

// FollowLinks follows the followed links of an asset, from it, through as
// many hops as opts says: out to what it needs, in to what needs it, or
// both. An asset canRead refuses is counted as hidden, never named, and
// never followed through. A loop ends where it comes back round, and is
// reported. Past opts.Limit assets, the graph is cut short at the hop
// where it went past, keeping the first by name, and says so. The graph is
// not laid out (Layer and Row are 0): package linkgraph does that.
func (s *Store) FollowLinks(rootID string, opts GraphOptions, canRead CanRead) (*models.AssetGraph, error) {
	if err := opts.normalize(); err != nil {
		return nil, err
	}
	types, err := s.FollowedTypes(opts.TypeIDs)
	if err != nil {
		return nil, err
	}
	symmetric := map[string]bool{}
	for _, t := range types {
		symmetric[t.ID] = t.IsSymmetric
	}
	ctx := &assetContext{}
	nodes, err := s.readNodes(ctx, []string{rootID})
	if err != nil {
		return nil, err
	}
	if nodes[rootID] == nil || !canRead(nodes[rootID].scopes) {
		return nil, ErrAssetNotFound
	}
	g := &models.AssetGraph{Root: rootID, Direction: opts.Direction, Depth: opts.Depth, Types: types, Links: []models.GraphLink{}, Loops: [][]string{}}
	type place struct {
		hops int
		side string
	}
	placed := map[string]place{rootID: {0, models.GraphRoot}}
	order := []string{rootID}
	hidden := map[string]bool{}
	cut := map[string]bool{}

	sides := []string{opts.Direction}
	if opts.Direction == models.GraphBoth {
		sides = []string{models.GraphOut, models.GraphIn}
	}
follow:
	for _, side := range sides {
		frontier := []string{rootID}
		for hop := 1; hop <= opts.Depth && len(frontier) > 0; hop++ {
			edges, err := s.readEdges(types, frontier, false)
			if err != nil {
				return nil, err
			}
			inFrontier := map[string]bool{}
			for _, id := range frontier {
				inFrontier[id] = true
			}
			// Each asset found, and the frontier's assets that found it.
			var found []string
			foundBy := map[string][]string{}
			for _, e := range edges {
				var at, next string
				switch {
				case (side == models.GraphOut || symmetric[e.typeID]) && inFrontier[e.from]:
					at, next = e.from, e.to
				case (side == models.GraphIn || symmetric[e.typeID]) && inFrontier[e.to]:
					at, next = e.to, e.from
				default:
					continue
				}
				if _, ok := placed[next]; ok || hidden[next] {
					continue
				}
				if foundBy[next] == nil {
					found = append(found, next)
				}
				foundBy[next] = append(foundBy[next], at)
			}
			read, err := s.readNodes(ctx, found)
			if err != nil {
				return nil, err
			}
			var readable []string
			for _, id := range found {
				n := read[id]
				if n == nil {
					continue
				}
				if !canRead(n.scopes) {
					hidden[id] = true
					continue
				}
				nodes[id] = n
				readable = append(readable, id)
			}
			sort.Slice(readable, func(i, j int) bool {
				a, b := nodes[readable[i]].asset.Name, nodes[readable[j]].asset.Name
				if a != b {
					return a < b
				}
				return readable[i] < readable[j]
			})
			if room := opts.Limit - len(placed); len(readable) > room {
				for _, id := range readable[room:] {
					for _, by := range foundBy[id] {
						cut[by] = true
					}
				}
				readable = readable[:room]
				g.Truncated, g.TruncatedAt = true, hop
			}
			for _, id := range readable {
				placed[id] = place{hop, side}
				order = append(order, id)
			}
			if g.Truncated {
				break follow
			}
			frontier = readable
		}
	}
	g.Hidden = len(hidden)

	// Every followed link among the assets placed, so the graph shows a
	// link between two assets at the same hop, or one that comes back
	// round.
	edges, err := s.readEdges(types, order, true)
	if err != nil {
		return nil, err
	}
	for _, e := range edges {
		g.Links = append(g.Links, models.GraphLink{ID: e.id, TypeID: e.typeID, From: e.from, To: e.to})
	}
	if g.Assets, err = s.shownAssets(ctx, order, nodes); err != nil {
		return nil, err
	}
	for i := range g.Assets {
		a := &g.Assets[i]
		p := placed[a.ID]
		a.Hops, a.Side, a.Cut = p.hops, p.side, cut[a.ID]
	}
	g.Loops = graphLoops(g, symmetric)
	return g, nil
}

// GraphAmong is the map of these assets, already read and kept to those
// the requester may read: the followed links among them, of these types
// (none for all). Past MaxGraphAssets assets it is cut short, keeping the
// first.
func (s *Store) GraphAmong(assets []models.AssetWithDetails, typeIDs []string) (*models.AssetGraph, error) {
	types, err := s.FollowedTypes(typeIDs)
	if err != nil {
		return nil, err
	}
	g := &models.AssetGraph{Types: types, Assets: []models.GraphAsset{}, Links: []models.GraphLink{}, Loops: [][]string{}}
	if len(assets) > MaxGraphAssets {
		assets, g.Truncated = assets[:MaxGraphAssets], true
	}
	ids := make([]string, len(assets))
	for i, a := range assets {
		ids[i] = a.Asset.ID
		cats := make([]models.GraphCategory, len(a.Categories))
		for j, c := range a.Categories {
			cats[j] = models.GraphCategory{ID: c.ID, Name: c.Name, Color: c.ColorHex}
		}
		g.Assets = append(g.Assets, models.GraphAsset{ID: a.Asset.ID, Name: a.Asset.Name, Categories: cats, Icon: a.ResolvedIcon,
			Condition: a.Asset.Condition, Archived: a.Asset.ArchivedAt != nil})
	}
	edges, err := s.readEdges(types, ids, true)
	if err != nil {
		return nil, err
	}
	for _, e := range edges {
		g.Links = append(g.Links, models.GraphLink{ID: e.id, TypeID: e.typeID, From: e.from, To: e.to})
	}
	symmetric := map[string]bool{}
	for _, t := range types {
		symmetric[t.ID] = t.IsSymmetric
	}
	g.Loops = graphLoops(g, symmetric)
	return g, nil
}

// graphLoops finds a graph's loops: each set of assets that can follow
// links from any of them round to each of the others (a strongly
// connected component of two or more), by the links of types that are not
// symmetric, whose every link would otherwise be a loop. Each loop's
// assets are by name, and the loops by their first.
func graphLoops(g *models.AssetGraph, symmetric map[string]bool) [][]string {
	index := map[string]int{}
	for i, a := range g.Assets {
		index[a.ID] = i
	}
	next := make([][]int, len(g.Assets))
	for _, l := range g.Links {
		f, okF := index[l.From]
		t, okT := index[l.To]
		if okF && okT && !symmetric[l.TypeID] {
			next[f] = append(next[f], t)
		}
	}
	// Tarjan's algorithm, kept iterative so a long chain cannot run the
	// stack deep.
	n := len(g.Assets)
	num, low := make([]int, n), make([]int, n)
	onStack := make([]bool, n)
	for i := range num {
		num[i] = -1
	}
	var stack []int
	var loops [][]int
	counter := 0
	type frame struct{ v, edge int }
	for start := 0; start < n; start++ {
		if num[start] >= 0 {
			continue
		}
		calls := []frame{{start, 0}}
		num[start], low[start] = counter, counter
		counter++
		stack = append(stack, start)
		onStack[start] = true
		for len(calls) > 0 {
			f := &calls[len(calls)-1]
			if f.edge < len(next[f.v]) {
				w := next[f.v][f.edge]
				f.edge++
				if num[w] < 0 {
					num[w], low[w] = counter, counter
					counter++
					stack = append(stack, w)
					onStack[w] = true
					calls = append(calls, frame{w, 0})
				} else if onStack[w] {
					low[f.v] = min(low[f.v], num[w])
				}
				continue
			}
			v := f.v
			calls = calls[:len(calls)-1]
			if len(calls) > 0 {
				u := calls[len(calls)-1].v
				low[u] = min(low[u], low[v])
			}
			if low[v] == num[v] {
				var comp []int
				for {
					w := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					onStack[w] = false
					comp = append(comp, w)
					if w == v {
						break
					}
				}
				if len(comp) > 1 {
					loops = append(loops, comp)
				}
			}
		}
	}
	out := [][]string{}
	for _, comp := range loops {
		sort.Slice(comp, func(i, j int) bool {
			a, b := g.Assets[comp[i]], g.Assets[comp[j]]
			if a.Name != b.Name {
				return a.Name < b.Name
			}
			return a.ID < b.ID
		})
		ids := make([]string, len(comp))
		for i, c := range comp {
			ids[i] = g.Assets[c].ID
		}
		out = append(out, ids)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := g.Assets[index[out[i][0]]], g.Assets[index[out[j][0]]]
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.ID < b.ID
	})
	return out
}

// FollowCounts counts, for each of ids, the assets it needs and those that
// need it through followed links: directly, and in all, through as many
// as MaxGraphDepth hops. An asset canRead refuses is counted, as a graph
// counts it hidden, but not followed through. Every followed link is read
// at once, so a report of many assets costs a few queries.
func (s *Store) FollowCounts(ids []string, canRead CanRead) (map[string]models.LinkCounts, error) {
	out := make(map[string]models.LinkCounts, len(ids))
	types, err := s.FollowedTypes(nil)
	if err != nil {
		return nil, err
	}
	if len(types) == 0 || len(ids) == 0 {
		return out, nil
	}
	tids := typeIDs(types)
	symmetric := map[string]bool{}
	for _, t := range types {
		symmetric[t.ID] = t.IsSymmetric
	}
	rows, err := s.Query("SELECT relation_type_id, from_asset_id, to_asset_id FROM asset_links WHERE relation_type_id IN ("+placeholders(len(tids))+")", anyArgs(tids)...)
	if err != nil {
		return nil, fmt.Errorf("reading followed links: %w", err)
	}
	needs, neededBy := map[string][]string{}, map[string][]string{}
	touched := map[string]bool{}
	for rows.Next() {
		var typeID, from, to string
		if err := rows.Scan(&typeID, &from, &to); err != nil {
			rows.Close()
			return nil, err
		}
		needs[from] = append(needs[from], to)
		neededBy[to] = append(neededBy[to], from)
		if symmetric[typeID] {
			needs[to] = append(needs[to], from)
			neededBy[from] = append(neededBy[from], to)
		}
		touched[from], touched[to] = true, true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	var all []string
	for id := range touched {
		all = append(all, id)
	}
	sort.Strings(all)
	scopes, err := s.AssetScopes(all)
	if err != nil {
		return nil, err
	}
	readable := func(id string) bool { return canRead(scopes[id]) }
	// count is how many assets are one hop from id, and how many are
	// within MaxGraphDepth hops, not following through those unreadable.
	count := func(id string, next map[string][]string) (int, int) {
		seen := map[string]bool{id: true}
		frontier := []string{id}
		direct, total := 0, 0
		for hop := 1; hop <= MaxGraphDepth && len(frontier) > 0; hop++ {
			var after []string
			for _, at := range frontier {
				for _, n := range next[at] {
					if seen[n] {
						continue
					}
					seen[n] = true
					total++
					if hop == 1 {
						direct++
					}
					if readable(n) {
						after = append(after, n)
					}
				}
			}
			frontier = after
		}
		return direct, total
	}
	for _, id := range ids {
		var c models.LinkCounts
		c.Needs, c.NeedsAll = count(id, needs)
		c.NeededBy, c.NeededByAll = count(id, neededBy)
		out[id] = c
	}
	return out, nil
}
