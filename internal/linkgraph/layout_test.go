package linkgraph

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/render"
)

func graphOf(root string, assets []models.GraphAsset, links ...[2]string) *models.AssetGraph {
	g := &models.AssetGraph{Root: root, Assets: assets, Types: []models.RelationType{{ID: "dep"}}}
	for i, l := range links {
		g.Links = append(g.Links, models.GraphLink{ID: fmt.Sprint(i), TypeID: "dep", From: l[0], To: l[1]})
	}
	return g
}

func at(g *models.AssetGraph) map[string][2]int {
	out := map[string][2]int{}
	for _, a := range g.Assets {
		out[a.ID] = [2]int{a.Layer, a.Row}
	}
	return out
}

func TestAnAssetsGraphIsLaidOutByHops(t *testing.T) {
	g := graphOf("root", []models.GraphAsset{
		{ID: "root", Name: "Extractor", Side: models.GraphRoot},
		{ID: "supply", Name: "Supply", Side: models.GraphOut, Hops: 1},
		{ID: "breaker", Name: "Breaker", Side: models.GraphOut, Hops: 2},
		{ID: "laser", Name: "Laser", Side: models.GraphIn, Hops: 1},
		{ID: "bench", Name: "Bench", Side: models.GraphIn, Hops: 2},
		{ID: "saw", Name: "Saw", Side: models.GraphIn, Hops: 1},
	}, [2]string{"laser", "root"}, [2]string{"saw", "root"}, [2]string{"bench", "laser"}, [2]string{"root", "supply"}, [2]string{"supply", "breaker"})
	Layout(g)
	got := at(g)
	// What needs it to the left, by hops; what it needs to the right.
	for id, layer := range map[string]int{"bench": 0, "laser": 1, "saw": 1, "root": 2, "supply": 3, "breaker": 4} {
		if got[id][0] != layer {
			t.Errorf("%s in layer %d, not %d", id, got[id][0], layer)
		}
	}
	if got["laser"][1] == got["saw"][1] {
		t.Errorf("two in one place: %v", got)
	}
}

func TestAMapIsLaidOutLeftOfWhatEachNeeds(t *testing.T) {
	g := graphOf("", []models.GraphAsset{
		{ID: "a", Name: "A"}, {ID: "b", Name: "B"}, {ID: "c", Name: "C"}, {ID: "d", Name: "D"}, {ID: "lone", Name: "Lone"},
	}, [2]string{"a", "b"}, [2]string{"b", "c"}, [2]string{"a", "c"}, [2]string{"c", "a"}, [2]string{"d", "c"})
	Layout(g)
	got := at(g)
	// The loop round A, B and C is broken where the walk came back to A.
	if !(got["a"][0] < got["b"][0] && got["b"][0] < got["c"][0] && got["d"][0] < got["c"][0]) {
		t.Errorf("layers %v", got)
	}
	// The unlinked come after the rest, past a gap.
	if got["lone"][0] != got["c"][0]+2 {
		t.Errorf("the unlinked asset at %v", got["lone"])
	}
	seen := map[[2]int]string{}
	for id, p := range got {
		if other, ok := seen[p]; ok {
			t.Errorf("%s and %s both at %v", id, other, p)
		}
		seen[p] = id
	}
}

func TestOrderingRowsCrossesFewerLinks(t *testing.T) {
	// Two columns linked across: by name they cross; ordered, they do not.
	g := graphOf("", []models.GraphAsset{
		{ID: "a1", Name: "A1"}, {ID: "a2", Name: "A2"}, {ID: "b1", Name: "B1"}, {ID: "b2", Name: "B2"},
	}, [2]string{"a1", "b2"}, [2]string{"a2", "b1"})
	Layout(g)
	got := at(g)
	if (got["a1"][1] < got["a2"][1]) != (got["b2"][1] < got["b1"][1]) {
		t.Errorf("the links cross: %v", got)
	}
}

func TestADrawingShowsWhatTheGraphHolds(t *testing.T) {
	g := graphOf("root", []models.GraphAsset{
		{ID: "root", Name: "Extractor", Side: models.GraphRoot},
		{ID: "supply", Name: "Three-phase", Side: models.GraphOut, Hops: 1, Condition: models.ConditionOutOfService},
		{ID: "breaker", Name: "Main breaker", Side: models.GraphOut, Hops: 2, Archived: true},
	}, [2]string{"root", "supply"}, [2]string{"supply", "breaker"})
	g.Hidden, g.Truncated, g.TruncatedAt = 2, true, 2
	g.Loops = [][]string{{"root", "supply"}}
	Layout(g)
	d := Drawing(g, "Extractor")
	if len(d.Nodes) != 3 || len(d.Edges) != 2 || !d.Nodes[0].Root || !d.Nodes[1].Marked || d.Nodes[1].Note != "Out of service" || !d.Nodes[2].Faded {
		t.Errorf("drawing %+v", d)
	}
	notes := strings.Join(d.Notes, " ")
	for _, want := range []string{"2 assets you may not see are left out", "1 loop", "Cut short at 2 hops, at 3 assets"} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes %q lack %q", notes, want)
		}
	}
	doc, err := render.NewDocument(render.DocumentOptions{Landscape: true, Title: "Graph"})
	if err != nil {
		t.Fatal(err)
	}
	doc.Graph(d)
	var svg, pdf bytes.Buffer
	if err := render.WriteSVG(&svg, doc.Pages()[0]); err != nil {
		t.Fatal(err)
	}
	if err := render.WritePDF(&pdf, doc.Pages()...); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(svg.String(), "<svg") && !strings.Contains(svg.String(), "<svg") {
		t.Errorf("svg %.80s", svg.String())
	}
	if !bytes.HasPrefix(pdf.Bytes(), []byte("%PDF")) {
		t.Errorf("pdf %.20q", pdf.Bytes())
	}
	if len(doc.Pages()) != 1 {
		t.Errorf("%d pages", len(doc.Pages()))
	}
}

func TestALargeDrawingFitsOnePage(t *testing.T) {
	var assets []models.GraphAsset
	var links [][2]string
	for i := 0; i < 500; i++ {
		assets = append(assets, models.GraphAsset{ID: fmt.Sprint(i), Name: fmt.Sprintf("Asset %03d", i)})
		if i > 0 {
			links = append(links, [2]string{fmt.Sprint(i - 1), fmt.Sprint(i)})
		}
	}
	g := graphOf("", assets, links...)
	Layout(g)
	doc, err := render.NewDocument(render.DocumentOptions{Landscape: true})
	if err != nil {
		t.Fatal(err)
	}
	doc.Graph(Drawing(g, "Every asset"))
	if n := len(doc.Pages()); n != 1 {
		t.Errorf("%d pages", n)
	}
}
