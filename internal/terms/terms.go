// Package terms expands a search through the instance's related terms
// (docs/data-model.md#related-terms), and matches words despite typing
// mistakes. It reads the graph it is given and nothing else: nothing
// leaves the server.
package terms

import (
	"sort"
	"strings"
	"unicode"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// maxDepth bounds how far narrower terms are followed, should links ever
// loop.
const maxDepth = 32

// Graph is the related terms, indexed for expanding searches.
type Graph struct {
	byID     map[string]*models.Term
	byLabel  map[string][]string
	narrower map[string][]string
	related  map[string][]string
}

// New indexes terms and the links between them. Links to terms not given
// are ignored.
func New(terms []models.Term, links []models.TermLink) *Graph {
	g := &Graph{byID: map[string]*models.Term{}, byLabel: map[string][]string{}, narrower: map[string][]string{}, related: map[string][]string{}}
	for i := range terms {
		t := &terms[i]
		g.byID[t.ID] = t
		for _, l := range Labels(t) {
			f := Fold(l)
			if !contains(g.byLabel[f], t.ID) {
				g.byLabel[f] = append(g.byLabel[f], t.ID)
			}
		}
	}
	for _, l := range links {
		if g.byID[l.TermID] == nil || g.byID[l.OtherID] == nil {
			continue
		}
		switch l.Relation {
		case models.TermBroader:
			g.narrower[l.OtherID] = append(g.narrower[l.OtherID], l.TermID)
		case models.TermRelated:
			g.related[l.TermID] = append(g.related[l.TermID], l.OtherID)
			g.related[l.OtherID] = append(g.related[l.OtherID], l.TermID)
		}
	}
	// The same graph expands the same way, whatever order it was read in.
	byLabel := func(ids []string) {
		sort.Slice(ids, func(i, j int) bool {
			a, b := g.byID[ids[i]], g.byID[ids[j]]
			if fa, fb := Fold(a.Label), Fold(b.Label); fa != fb {
				return fa < fb
			}
			return a.ID < b.ID
		})
	}
	for _, m := range []map[string][]string{g.narrower, g.related, g.byLabel} {
		for _, ids := range m {
			byLabel(ids)
		}
	}
	return g
}

// Empty reports whether there are no terms, so nothing expands.
func (g *Graph) Empty() bool { return g == nil || len(g.byID) == 0 }

// Labels are a term's names: its preferred label, then its others.
func Labels(t *models.Term) []string {
	return append([]string{t.Label}, t.AltLabels...)
}

// Expansion is another name a search also looks for, and why: "TPU,
// narrower than flexible filament".
type Expansion struct {
	Label string `json:"label"`
	Why   string `json:"why"`
}

// Expand is what a search for text also looks for: the other names of
// each term called text, and the names of every term narrower than it,
// nearest first. Broader terms are not looked for, and related ones are
// only suggested (Related). The text itself is not among them.
func (g *Graph) Expand(text string) []Expansion {
	if g.Empty() {
		return nil
	}
	want := Fold(text)
	seen := map[string]bool{want: true}
	var out []Expansion
	for _, id := range g.byLabel[want] {
		type step struct {
			id   string
			path []*models.Term
		}
		queue := []step{{id, []*models.Term{g.byID[id]}}}
		visited := map[string]bool{id: true}
		for len(queue) > 0 {
			s := queue[0]
			queue = queue[1:]
			t := s.path[len(s.path)-1]
			for _, l := range Labels(t) {
				f := Fold(l)
				if f == "" || seen[f] {
					continue
				}
				seen[f] = true
				out = append(out, Expansion{Label: l, Why: why(l, text, s.path)})
			}
			if len(s.path) > maxDepth {
				continue
			}
			for _, n := range g.narrower[s.id] {
				if visited[n] {
					continue
				}
				visited[n] = true
				path := append(append([]*models.Term{}, s.path...), g.byID[n])
				queue = append(queue, step{n, path})
			}
		}
	}
	return out
}

// why says how a label was reached from the text searched for, along
// path from the term called text to the term label names.
func why(label, text string, path []*models.Term) string {
	t := path[len(path)-1]
	if len(path) == 1 {
		return label + ", another name for " + strings.TrimSpace(text)
	}
	var b strings.Builder
	b.WriteString(label)
	if Fold(label) != Fold(t.Label) {
		b.WriteString(" (another name for " + t.Label + ")")
	}
	for i := len(path) - 2; i >= 0; i-- {
		b.WriteString(", narrower than " + path[i].Label)
	}
	return b.String()
}

// Related are the preferred labels of the terms related to each term
// called text, to suggest searching for instead: they are not searched.
func (g *Graph) Related(text string) []string {
	if g.Empty() {
		return nil
	}
	want := Fold(text)
	seen := map[string]bool{want: true}
	for _, e := range g.Expand(text) {
		seen[Fold(e.Label)] = true
	}
	var out []string
	for _, id := range g.byLabel[want] {
		for _, r := range g.related[id] {
			l := g.byID[r].Label
			if !seen[Fold(l)] {
				seen[Fold(l)] = true
				out = append(out, l)
			}
		}
	}
	return out
}

// Fold is text as terms compare it: in lower case, with runs of spaces as
// one, trimmed.
func Fold(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// Words are the words of text, folded: runs of letters and digits.
func Words(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// Allowed is how many mistakes a word of n letters may have and still
// match: none below five, one up to eight, two from nine, so that short
// words don't match everything.
func Allowed(n int) int {
	switch {
	case n < 5:
		return 0
	case n < 9:
		return 1
	}
	return 2
}

// Distance is how many letters must be added, removed, changed or swapped
// with the next to make a into b (optimal string alignment).
func Distance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev2 := make([]int, len(rb)+1)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
			if i > 1 && j > 1 && ra[i-1] == rb[j-2] && ra[i-2] == rb[j-1] {
				cur[j] = min(cur[j], prev2[j-2]+1)
			}
		}
		prev2, prev, cur = prev, cur, prev2
	}
	return prev[len(rb)]
}

// NearWhy says why a search matched a near word: "filament, close to
// filamnet".
func NearWhy(near, search string) string {
	return near + ", close to " + strings.TrimSpace(search)
}

// Near finds, for every word of search, a word of texts close enough to
// it (Allowed), and says which: "filament" for "filamnet". It is false
// when a word has none, or when every word is found as typed, which an
// exact search already finds.
func Near(search string, texts []string) (string, bool) {
	words := Words(search)
	if len(words) == 0 {
		return "", false
	}
	var have []string
	for _, t := range texts {
		have = append(have, Words(t)...)
	}
	var found []string
	inexact := false
	for _, w := range words {
		best, bestD := "", -1
		for _, h := range have {
			if strings.Contains(h, w) {
				best, bestD = h, 0
				break
			}
			allowed := Allowed(len([]rune(w)))
			if allowed == 0 {
				continue
			}
			// Compare with as much of the word as was typed, and the whole.
			d := Distance(w, h)
			if r := []rune(h); len(r) > len([]rune(w)) {
				d = min(d, Distance(w, string(r[:len([]rune(w))])))
			}
			if d <= allowed && (bestD < 0 || d < bestD) {
				best, bestD = h, d
			}
		}
		if bestD < 0 {
			return "", false
		}
		if bestD > 0 {
			inexact = true
		}
		found = append(found, best)
	}
	if !inexact {
		return "", false
	}
	return strings.Join(found, " "), true
}
