package linkgraph

import (
	"fmt"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/render"
)

// ConditionNote is how an asset's condition is noted in a drawing: empty
// when it is working.
func ConditionNote(condition string) string {
	switch condition {
	case models.ConditionOutOfService:
		return "Out of service"
	case models.ConditionNeedsMaintenance:
		return "Needs maintenance"
	}
	return ""
}

// Drawing is a laid-out graph as render draws it, under title, with a note
// of what it leaves out.
func Drawing(g *models.AssetGraph, title string) render.GraphDrawing {
	d := render.GraphDrawing{Title: title}
	index := make(map[string]int, len(g.Assets))
	for i, a := range g.Assets {
		index[a.ID] = i
		note := ConditionNote(a.Condition)
		if a.Archived {
			if note != "" {
				note += ", archived"
			} else {
				note = "Archived"
			}
		}
		d.Nodes = append(d.Nodes, render.GraphNode{Name: a.Name, Note: note, Layer: a.Layer, Row: a.Row,
			Root: a.ID == g.Root && g.Root != "", Marked: a.Condition != models.ConditionOK && a.Condition != "", Faded: a.Archived})
	}
	symmetric := map[string]bool{}
	for _, t := range g.Types {
		symmetric[t.ID] = t.IsSymmetric
	}
	for _, l := range g.Links {
		f, okF := index[l.From]
		t, okT := index[l.To]
		if okF && okT {
			d.Edges = append(d.Edges, render.GraphEdge{From: f, To: t, Dashed: symmetric[l.TypeID]})
		}
	}
	d.Notes = append(d.Notes, Summary(g)...)
	return d
}

// Summary says, in a line each, what a graph shows and what it leaves out:
// which way it reads, assets hidden, loops, and where it was cut short.
func Summary(g *models.AssetGraph) []string {
	var out []string
	if g.Root != "" {
		out = append(out, "What needs it is to the left, what it needs to the right; arrows point to what is needed.")
	} else {
		out = append(out, "Each asset is to the left of what it needs; arrows point to what is needed.")
	}
	if len(g.Assets) == 0 || (g.Root != "" && len(g.Assets) == 1) {
		out = append(out, "No followed links.")
	}
	if g.Hidden > 0 {
		out = append(out, plural(g.Hidden, "asset you may not see is", "assets you may not see are")+" left out, and not followed through.")
	}
	if len(g.Loops) > 0 {
		out = append(out, plural(len(g.Loops), "loop", "loops")+": following links comes back round, and stops there.")
	}
	if g.Truncated {
		if g.TruncatedAt > 0 {
			out = append(out, fmt.Sprintf("Cut short at %d hops, at %d assets.", g.TruncatedAt, len(g.Assets)))
		} else {
			out = append(out, fmt.Sprintf("Cut short at %d assets.", len(g.Assets)))
		}
	}
	return out
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
