// Package linkgraph lays out a graph of followed links (docs/data-model.md#following-links)
// in layers, for the web interface to draw and for its SVG and PDF: each
// asset in a column, those that need others to the left of what they need,
// and in each column, rows ordered to cross as few links as a few sweeps
// find. It is drawn in-house, rather than by a library, as layered layouts
// of a few hundred boxes need little more than this.
package linkgraph

import (
	"sort"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// sweeps is how many times rows are ordered, each time both ways.
const sweeps = 4

// minWrap is the fewest rows a column of unlinked assets wraps at.
const minWrap = 10

// Layout sets each asset's Layer and Row. One asset's graph puts it in the
// middle, what needs it to the left by hops and what it needs to the
// right. A map puts each asset one column left of the furthest left of
// what it needs, breaking loops, and its assets with no links after the
// rest, in columns as tall as the tallest.
func Layout(g *models.AssetGraph) {
	n := len(g.Assets)
	if n == 0 {
		return
	}
	index := make(map[string]int, n)
	for i, a := range g.Assets {
		index[a.ID] = i
	}
	symmetric := map[string]bool{}
	for _, t := range g.Types {
		symmetric[t.ID] = t.IsSymmetric
	}
	// out holds directed links, from what needs to what is needed; near
	// every link, either way, for ordering rows.
	out := make([][]int, n)
	near := make([][]int, n)
	for _, l := range g.Links {
		f, okF := index[l.From]
		t, okT := index[l.To]
		if !okF || !okT || f == t {
			continue
		}
		near[f], near[t] = append(near[f], t), append(near[t], f)
		if !symmetric[l.TypeID] {
			out[f] = append(out[f], t)
		}
	}

	layer := make([]int, n)
	if g.Root != "" {
		for i, a := range g.Assets {
			switch a.Side {
			case models.GraphIn:
				layer[i] = -a.Hops
			case models.GraphOut:
				layer[i] = a.Hops
			}
		}
	} else {
		layer = mapLayers(g, out, near)
	}
	low := layer[0]
	for _, l := range layer {
		low = min(low, l)
	}
	columns := map[int][]int{}
	for i := range layer {
		layer[i] -= low
		columns[layer[i]] = append(columns[layer[i]], i)
	}
	keys := make([]int, 0, len(columns))
	for k := range columns {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	byName := func(col []int) {
		sort.SliceStable(col, func(i, j int) bool {
			a, b := g.Assets[col[i]], g.Assets[col[j]]
			if a.Name != b.Name {
				return a.Name < b.Name
			}
			return a.ID < b.ID
		})
	}
	row := make([]float64, n)
	for _, k := range keys {
		byName(columns[k])
		for r, i := range columns[k] {
			row[i] = float64(r)
		}
	}
	// Barycenter sweeps: each column ordered by where its neighbors in
	// the column before are, left to right, then right to left.
	order := func(k, from int) {
		col := columns[k]
		mean := make(map[int]float64, len(col))
		for _, i := range col {
			sum, count := 0.0, 0
			for _, j := range near[i] {
				if layer[j] == from {
					sum += row[j]
					count++
				}
			}
			if count > 0 {
				mean[i] = sum / float64(count)
			} else {
				mean[i] = row[i]
			}
		}
		sort.SliceStable(col, func(a, b int) bool { return mean[col[a]] < mean[col[b]] })
		for r, i := range col {
			row[i] = float64(r)
		}
	}
	for s := 0; s < sweeps; s++ {
		for x := 1; x < len(keys); x++ {
			order(keys[x], keys[x-1])
		}
		for x := len(keys) - 2; x >= 0; x-- {
			order(keys[x], keys[x+1])
		}
	}
	for i := range g.Assets {
		g.Assets[i].Layer, g.Assets[i].Row = layer[i], int(row[i])
	}
}

// mapLayers puts each linked asset one column left of the furthest left of
// what it needs, after a depth-first walk by name sets aside the links that
// close loops, and the unlinked after them.
func mapLayers(g *models.AssetGraph, out, near [][]int) []int {
	n := len(g.Assets)
	byName := make([]int, n)
	for i := range byName {
		byName[i] = i
	}
	sort.SliceStable(byName, func(i, j int) bool {
		a, b := g.Assets[byName[i]], g.Assets[byName[j]]
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.ID < b.ID
	})
	// Keep the links that do not close a loop: an iterative depth-first
	// walk, dropping links back to an asset still being walked.
	const (
		unseen = iota
		walking
		done
	)
	state := make([]int, n)
	kept := make([][]int, n)
	type frame struct{ v, edge int }
	for _, start := range byName {
		if state[start] != unseen {
			continue
		}
		state[start] = walking
		calls := []frame{{start, 0}}
		for len(calls) > 0 {
			f := &calls[len(calls)-1]
			if f.edge < len(out[f.v]) {
				w := out[f.v][f.edge]
				f.edge++
				switch state[w] {
				case unseen:
					kept[f.v] = append(kept[f.v], w)
					state[w] = walking
					calls = append(calls, frame{w, 0})
				case done:
					kept[f.v] = append(kept[f.v], w)
				}
				continue
			}
			state[f.v] = done
			calls = calls[:len(calls)-1]
		}
	}
	// Longest path to what needs nothing more: from the right, each asset
	// a column left of the furthest left of what it needs.
	depth := make([]int, n)
	ready := make([]bool, n)
	var visit func(v int) int
	visit = func(v int) int {
		if ready[v] {
			return depth[v]
		}
		ready[v] = true
		d := 0
		for _, w := range kept[v] {
			d = max(d, visit(w)+1)
		}
		depth[v] = d
		return d
	}
	tallest := 0
	for v := range depth {
		visit(v)
	}
	// Columns from the left: what needs most is furthest left.
	top := 0
	for v := range depth {
		if len(near[v]) > 0 {
			top = max(top, depth[v])
		}
	}
	layer := make([]int, n)
	count := map[int]int{}
	for v := range layer {
		if len(near[v]) > 0 {
			layer[v] = top - depth[v]
			count[layer[v]]++
			tallest = max(tallest, count[layer[v]])
		}
	}
	// The unlinked, by name, in columns after a gap.
	wrap := max(tallest, minWrap)
	first := top + 2
	if tallest == 0 {
		first = 0
	}
	placed := 0
	for _, v := range byName {
		if len(near[v]) == 0 {
			layer[v] = first + placed/wrap
			placed++
		}
	}
	return layer
}
