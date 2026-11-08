package store

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// Categories that imply other categories (docs/stock-and-lending.md#pools):
// the members of one count in another, wherever it is in the tree, for
// totals, alerts, filters and reports, and for nothing else. Membership
// through a link is worked out when it is needed, from category_implies.

var (
	// ErrImpliesCycle is a link that would lead back to the category it
	// starts from, directly or through the tree.
	ErrImpliesCycle = errors.New("a category cannot count in itself: that link would lead back to where it starts")
	// ErrImpliesSystem is a link to or from Uncategorized.
	ErrImpliesSystem = errors.New("the system Uncategorized category cannot imply or be implied by others")
)

// queryer runs a query, in a transaction or outside one.
type queryer interface {
	Query(string, ...any) (*sql.Rows, error)
}

// catGraph is the category tree with the links between categories, for
// working out where an asset counts.
type catGraph struct {
	parents map[string]string
	implies map[string][]string
	// counted caches each category's reach.
	counted map[string]models.Reached
}

// readImplies maps each category that implies others to them, sorted.
func readImplies(q queryer) (map[string][]string, error) {
	rows, err := q.Query("SELECT category_id, implied_id FROM category_implies ORDER BY category_id, implied_id")
	if err != nil {
		return nil, fmt.Errorf("reading implied categories: %w", err)
	}
	defer rows.Close()
	implies := map[string][]string{}
	for rows.Next() {
		var from, to string
		if err := rows.Scan(&from, &to); err != nil {
			return nil, fmt.Errorf("scanning an implied category: %w", err)
		}
		implies[from] = append(implies[from], to)
	}
	return implies, rows.Err()
}

// readCatGraph reads the tree and the links.
func readCatGraph(q queryer) (*catGraph, error) {
	rows, err := q.Query("SELECT id, parent_id FROM categories")
	if err != nil {
		return nil, fmt.Errorf("reading categories: %w", err)
	}
	parents := map[string]string{}
	for rows.Next() {
		var id string
		var parent sql.NullString
		if err := rows.Scan(&id, &parent); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scanning category: %w", err)
		}
		parents[id] = parent.String
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	implies, err := readImplies(q)
	if err != nil {
		return nil, err
	}
	return newCatGraph(parents, implies), nil
}

func newCatGraph(parents map[string]string, implies map[string][]string) *catGraph {
	return &catGraph{parents: parents, implies: implies, counted: map[string]models.Reached{}}
}

// reach is where an asset in these categories counts.
func (g *catGraph) reach(ids []string) models.Reached {
	if len(ids) == 1 {
		if r, ok := g.counted[ids[0]]; ok {
			return r
		}
	}
	r := models.Reach(ids, func(id string) string { return g.parents[id] }, func(id string) []string { return g.implies[id] })
	if len(ids) == 1 {
		g.counted[ids[0]] = r
	}
	return r
}

// membersOf lists the categories whose assets are members of id: id
// itself, and every category whose links reach it. A filter by id keeps
// the assets in any of them.
func (g *catGraph) membersOf(id string) []string {
	out := []string{id}
	for c := range g.parents {
		if c == id {
			continue
		}
		if _, ok := g.reach([]string{c}).Linked[id]; ok {
			out = append(out, c)
		}
	}
	sort.Strings(out[1:])
	return out
}

// cycle reports whether some link leads back to the category it starts
// from.
func (g *catGraph) cycle() bool {
	for from, to := range g.implies {
		if g.reach(to).Counted[from] {
			return true
		}
	}
	return false
}

// path names a category with those above it: "Workshop › Fasteners › M5".
func path(id string, parents, names map[string]string) string {
	var parts []string
	for depth := 0; id != "" && depth < 100; depth++ {
		parts = append([]string{names[id]}, parts...)
		id = parents[id]
	}
	return strings.Join(parts, " › ")
}

// possessive is a name with 's, or a lone apostrophe after an s.
func possessive(name string) string {
	if strings.HasSuffix(name, "s") {
		return name + "'"
	}
	return name + "'s"
}

// withImplies sets categories' links, both ways.
func withImplies(q queryer, categories []models.Category) error {
	implies, err := readImplies(q)
	if err != nil {
		return err
	}
	included := map[string][]string{}
	for from, to := range implies {
		for _, t := range to {
			included[t] = append(included[t], from)
		}
	}
	for i := range categories {
		c := &categories[i]
		c.Implies, c.IncludedBy = []string{}, []string{}
		c.Implies = append(c.Implies, implies[c.ID]...)
		c.IncludedBy = append(c.IncludedBy, included[c.ID]...)
		sort.Strings(c.IncludedBy)
	}
	return nil
}

// CategoryLinks are the links to set on a category: those it implies, and
// those implying it. A nil list leaves that side as it is.
type CategoryLinks struct {
	Implies    *[]string
	IncludedBy *[]string
}

// SetCategoryLinks replaces a category's links on the sides given. A link
// that would lead back to where it starts is refused with ErrImpliesCycle,
// and the categories whose totals the links move are checked against their
// thresholds, as moving a category is.
func (s *Store) SetCategoryLinks(id string, in CategoryLinks) (*models.Category, error) {
	if id == UncategorizedCategoryID {
		return nil, ErrImpliesSystem
	}
	err := s.withInventory(func(ic *inventoryChange) error {
		var n int
		if err := s.TxQueryRow(ic.tx, "SELECT COUNT(*) FROM categories WHERE id = ?", id).Scan(&n); err != nil {
			return fmt.Errorf("reading category: %w", err)
		}
		if n == 0 {
			return ErrCategoryNotFound
		}
		replace := func(list []string, column, other string) error {
			if _, err := s.TxExec(ic.tx, "DELETE FROM category_implies WHERE "+column+" = ?", id); err != nil {
				return fmt.Errorf("clearing implied categories: %w", err)
			}
			seen := map[string]bool{}
			for _, c := range list {
				if seen[c] {
					continue
				}
				seen[c] = true
				if c == id {
					return ErrImpliesCycle
				}
				if c == UncategorizedCategoryID {
					return ErrImpliesSystem
				}
				var found int
				if err := s.TxQueryRow(ic.tx, "SELECT COUNT(*) FROM categories WHERE id = ?", c).Scan(&found); err != nil {
					return fmt.Errorf("reading category: %w", err)
				}
				if found == 0 {
					return fmt.Errorf("%w: %s", ErrCategoryNotFound, c)
				}
				if _, err := s.TxExec(ic.tx, "INSERT INTO category_implies ("+column+", "+other+") VALUES (?, ?)", id, c); err != nil {
					return fmt.Errorf("linking categories: %w", err)
				}
			}
			return nil
		}
		if in.Implies != nil {
			if err := replace(*in.Implies, "category_id", "implied_id"); err != nil {
				return err
			}
		}
		if in.IncludedBy != nil {
			if err := replace(*in.IncludedBy, "implied_id", "category_id"); err != nil {
				return err
			}
		}
		g, err := readCatGraph(txQuerier{s, ic.tx})
		if err != nil {
			return err
		}
		if g.cycle() {
			return ErrImpliesCycle
		}
		_, err = s.TxExec(ic.tx, "UPDATE categories SET updated_at = ? WHERE id = ?", time.Now().UTC(), id)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.GetCategoryByID(id)
}

// checkMoveCycle refuses moving a category under parent when a link would
// then lead back to it.
func (s *Store) checkMoveCycle(id string, parent *string) error {
	if parent == nil || *parent == "" {
		return nil
	}
	g, err := readCatGraph(storeQuerier{s})
	if err != nil {
		return err
	}
	if g.reach([]string{*parent}).Counted[id] {
		return ErrImpliesCycle
	}
	return nil
}

// LinkWarning is a link adding nothing to a total: none of the members of
// the category implying another count their stock in the other's unit
// (docs/stock-and-lending.md#units). They still count as its members.
type LinkWarning struct {
	CategoryID string `json:"category_id"`
	ImpliedID  string `json:"implied_id"`
	Unit       string `json:"unit"`
	Message    string `json:"message"`
}

// CategoryLinkWarnings warns of each link to or from a category that adds
// nothing to the total of the category it implies.
func (s *Store) CategoryLinkWarnings(id string) ([]LinkWarning, error) {
	q := storeQuerier{s}
	g, err := readCatGraph(q)
	if err != nil {
		return nil, err
	}
	units, names := map[string]string{}, map[string]string{}
	rows, err := s.Query("SELECT id, name, is_consumable, stock_unit FROM categories")
	if err != nil {
		return nil, fmt.Errorf("reading categories: %w", err)
	}
	for rows.Next() {
		var cid, name, unit string
		var consumable bool
		if err := rows.Scan(&cid, &name, &consumable, &unit); err != nil {
			rows.Close()
			return nil, err
		}
		names[cid] = name
		if consumable && unit != "" {
			units[cid] = unit
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	type link struct{ from, to string }
	var links []link
	for from, to := range g.implies {
		for _, t := range to {
			if (from == id || t == id) && units[t] != "" {
				links = append(links, link{from, t})
			}
		}
	}
	if len(links) == 0 {
		return []LinkWarning{}, nil
	}
	// The units each category's members count their stock in.
	held := map[string]map[string]bool{}
	byAsset := map[string][]string{}
	unitOf := map[string]string{}
	rows, err = s.Query(`SELECT ac.asset_id, ac.category_id, a.stock_unit FROM asset_categories ac
		JOIN assets a ON a.id = ac.asset_id WHERE a.archived_at IS NULL AND a.stock_quantity IS NOT NULL`)
	if err != nil {
		return nil, fmt.Errorf("reading category assignments: %w", err)
	}
	for rows.Next() {
		var asset, cat, unit string
		if err := rows.Scan(&asset, &cat, &unit); err != nil {
			rows.Close()
			return nil, err
		}
		byAsset[asset] = append(byAsset[asset], cat)
		unitOf[asset] = unit
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for asset, cats := range byAsset {
		for c := range g.reach(cats).Counted {
			if held[c] == nil {
				held[c] = map[string]bool{}
			}
			held[c][unitOf[asset]] = true
		}
	}
	sort.Slice(links, func(i, j int) bool {
		if links[i].from != links[j].from {
			return links[i].from < links[j].from
		}
		return links[i].to < links[j].to
	})
	out := []LinkWarning{}
	for _, l := range links {
		unit := units[l.to]
		if held[l.from][unit] {
			continue
		}
		out = append(out, LinkWarning{CategoryID: l.from, ImpliedID: l.to, Unit: unit,
			Message: fmt.Sprintf("None of %s assets count in %s, so they add nothing to %s total.",
				possessive(path(l.from, g.parents, names)), unit, possessive(names[l.to]))})
	}
	return out, nil
}

// countsIn lists the categories an asset in these categories counts in
// through links, by name, each with the category it comes through.
func (t categoryTraits) countsIn(ids []string) []models.CountsIn {
	out := []models.CountsIn{}
	if t.graph == nil || len(t.graph.implies) == 0 {
		return out
	}
	for cat, through := range t.graph.reach(ids).Linked {
		out = append(out, models.CountsIn{CategoryID: cat, CategoryName: t.names[cat], ThroughID: through, ThroughName: t.names[through]})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CategoryName != out[j].CategoryName {
			return out[i].CategoryName < out[j].CategoryName
		}
		return out[i].CategoryID < out[j].CategoryID
	})
	return out
}

// AssetPools lists the pools an asset counts in (docs/stock-and-lending.md#pools):
// the consumable categories with a unit, its own, above it or reached
// through links, that total its stock, each with every active asset whose
// stock it totals, the asset itself among them, and where each is: here
// first, then the most stock first. Totals are of every such asset; the API sums those
// the requester may see.
func (s *Store) AssetPools(id string) ([]models.AssetPool, error) {
	q := storeQuerier{s}
	g, err := readCatGraph(q)
	if err != nil {
		return nil, err
	}
	type pool struct {
		name, unit string
		threshold  *int
	}
	pools := map[string]pool{}
	rows, err := s.Query("SELECT id, name, stock_unit, low_stock_threshold FROM categories WHERE is_consumable = ? AND stock_unit <> ''", true)
	if err != nil {
		return nil, fmt.Errorf("reading categories: %w", err)
	}
	for rows.Next() {
		var cid string
		var p pool
		var threshold sql.NullInt64
		if err := rows.Scan(&cid, &p.name, &p.unit, &threshold); err != nil {
			rows.Close()
			return nil, err
		}
		if threshold.Valid {
			t := int(threshold.Int64)
			p.threshold = &t
		}
		pools[cid] = p
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []models.AssetPool{}
	if len(pools) == 0 {
		return out, nil
	}
	type held struct {
		name     string
		quantity int
		unit     string
		cats     []string
	}
	assets := map[string]*held{}
	rows, err = s.Query(`SELECT a.id, a.name, a.stock_quantity, a.stock_unit, ac.category_id FROM assets a
		JOIN asset_categories ac ON ac.asset_id = a.id
		WHERE a.archived_at IS NULL AND a.stock_quantity IS NOT NULL`)
	if err != nil {
		return nil, fmt.Errorf("reading stock: %w", err)
	}
	for rows.Next() {
		var aid, name, unit, cat string
		var quantity int
		if err := rows.Scan(&aid, &name, &quantity, &unit, &cat); err != nil {
			rows.Close()
			return nil, err
		}
		h := assets[aid]
		if h == nil {
			h = &held{name: name, quantity: quantity, unit: unit}
			assets[aid] = h
		}
		h.cats = append(h.cats, cat)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	self := assets[id]
	if self == nil || self.unit == "" {
		return out, nil
	}
	mine := g.reach(self.cats).Counted
	ctx := &assetContext{list: true}
	for cid, p := range pools {
		if !mine[cid] || p.unit != self.unit {
			continue
		}
		ap := models.AssetPool{CategoryID: cid, Name: p.name, Unit: p.unit, LowStockThreshold: p.threshold, Places: []models.PoolPlace{}}
		for aid, h := range assets {
			if h.unit != p.unit || !g.reach(h.cats).Counted[cid] {
				continue
			}
			ap.Total += h.quantity
			where := &models.AssetWithDetails{Asset: models.Asset{ID: aid}}
			if err := s.linkDetails(where, ctx); err != nil {
				return nil, err
			}
			ap.Places = append(ap.Places, models.PoolPlace{AssetID: aid, Name: h.name, Quantity: h.quantity, Location: where.Location})
		}
		// Here first, then the most stock first.
		sort.Slice(ap.Places, func(i, j int) bool {
			a, b := ap.Places[i], ap.Places[j]
			if (a.AssetID == id) != (b.AssetID == id) {
				return a.AssetID == id
			}
			if a.Quantity != b.Quantity {
				return a.Quantity > b.Quantity
			}
			return a.Name < b.Name
		})
		out = append(out, ap)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// PoolMembership is the pools an asset adds to, and how much.
type PoolMembership struct {
	Pools    []string
	Quantity int
}

// AssetPoolMemberships maps each active asset counted by quantity that adds
// to a pool (a consumable category with a unit) to the pools it adds to and
// its quantity.
func (s *Store) AssetPoolMemberships() (map[string]PoolMembership, error) {
	g, err := readCatGraph(storeQuerier{s})
	if err != nil {
		return nil, err
	}
	units := map[string]string{}
	rows, err := s.Query("SELECT id, stock_unit FROM categories WHERE is_consumable = ? AND stock_unit <> ''", true)
	if err != nil {
		return nil, fmt.Errorf("reading categories: %w", err)
	}
	for rows.Next() {
		var cid, unit string
		if err := rows.Scan(&cid, &unit); err != nil {
			rows.Close()
			return nil, err
		}
		units[cid] = unit
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := map[string]PoolMembership{}
	if len(units) == 0 {
		return out, nil
	}
	cats, unitOf, quantities := map[string][]string{}, map[string]string{}, map[string]int{}
	rows, err = s.Query(`SELECT a.id, a.stock_unit, a.stock_quantity, ac.category_id FROM assets a JOIN asset_categories ac ON ac.asset_id = a.id
		WHERE a.archived_at IS NULL AND a.stock_quantity IS NOT NULL AND a.stock_unit <> ''`)
	if err != nil {
		return nil, fmt.Errorf("reading stock: %w", err)
	}
	for rows.Next() {
		var aid, unit, cat string
		var quantity int
		if err := rows.Scan(&aid, &unit, &quantity, &cat); err != nil {
			rows.Close()
			return nil, err
		}
		cats[aid] = append(cats[aid], cat)
		unitOf[aid], quantities[aid] = unit, quantity
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for aid, c := range cats {
		var pools []string
		for cid := range g.reach(c).Counted {
			if units[cid] == unitOf[aid] {
				pools = append(pools, cid)
			}
		}
		if len(pools) > 0 {
			sort.Strings(pools)
			out[aid] = PoolMembership{Pools: pools, Quantity: quantities[aid]}
		}
	}
	return out, nil
}
