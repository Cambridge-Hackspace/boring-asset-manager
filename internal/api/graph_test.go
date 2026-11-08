package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// graphScene is a workshop whose laser cutter depends on its extractor,
// which depends on the main breaker, which depends on the substation: the
// breaker is in the kitchen, whose scope a workshop reader cannot read.
type graphScene struct {
	srv                                   *Server
	admin, workshop                       *http.Cookie
	adminID                               string
	tools                                 *models.Category
	laser, extractor, breaker, substation string
}

func newGraphScene(t *testing.T) graphScene {
	t.Helper()
	srv, admin, _ := setupTestServer(t)
	st := srv.store
	sc := graphScene{srv: srv, admin: sessionCookie(t, srv, admin.ID), adminID: admin.ID}
	workshop, err := st.CreateScope("workshop")
	if err != nil {
		t.Fatal(err)
	}
	sc.tools, _ = st.CreateCategory("Tools", nil, "", false, false)
	if _, err := st.SetCategoryScopes(sc.tools.ID, []string{workshop.ID}); err != nil {
		t.Fatal(err)
	}
	kitchen, _ := st.CreateCategory("Kitchen", nil, "", false, false)
	kitchenScope, _ := st.CreateScope("kitchen")
	if _, err := st.SetCategoryScopes(kitchen.ID, []string{kitchenScope.ID}); err != nil {
		t.Fatal(err)
	}
	add := func(name, cat string) string {
		a, err := st.CreateAsset(name, []string{cat}, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		return a.Asset.ID
	}
	sc.laser, sc.extractor = add("Laser cutter", sc.tools.ID), add("Extractor", sc.tools.ID)
	sc.breaker, sc.substation = add("Main breaker", kitchen.ID), add("Substation", sc.tools.ID)
	var dep models.RelationType
	types, _ := st.ListRelationTypes()
	for _, rt := range types {
		if rt.Name == "depends on" {
			dep = rt
		}
	}
	for _, pair := range [][2]string{{sc.laser, sc.extractor}, {sc.extractor, sc.breaker}, {sc.breaker, sc.substation}} {
		if _, err := st.AddLink(pair[0], dep.ID, pair[1], true); err != nil {
			t.Fatal(err)
		}
	}
	sc.workshop = scopedReader(t, srv, "workshop")
	return sc
}

func readGraph(t *testing.T, rec interface{ Bytes() []byte }) models.AssetGraph {
	t.Helper()
	var v struct {
		Graph models.AssetGraph `json:"graph"`
	}
	if err := json.Unmarshal(rec.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return v.Graph
}

func graphAssetNames(g models.AssetGraph) []string {
	var out []string
	for _, a := range g.Assets {
		out = append(out, a.Name)
	}
	return out
}

func TestFollowingLinksOverTheAPI(t *testing.T) {
	sc := newGraphScene(t)
	path := "/api/v1/assets/" + sc.laser + "/graph"

	rec := doJSON(t, sc.srv, sc.admin, http.MethodGet, path+"?direction=out&depth=10", "")
	g := readGraph(t, rec.Body)
	if rec.Code != http.StatusOK || len(g.Assets) != 4 || len(g.Links) != 3 || g.Hidden != 0 {
		t.Fatalf("the admin's graph: %d %s", rec.Code, rec.Body)
	}
	// Laid out from the laser cutter, to the right by hops.
	for _, a := range g.Assets {
		if a.Layer != a.Hops || a.Row != 0 {
			t.Errorf("%s at %d, %d", a.Name, a.Layer, a.Row)
		}
	}

	// A workshop reader is told the breaker is there, not what it is, and
	// is not shown the substation past it.
	rec = doJSON(t, sc.srv, sc.workshop, http.MethodGet, path+"?direction=out&depth=10", "")
	g = readGraph(t, rec.Body)
	if rec.Code != http.StatusOK || strings.Join(graphAssetNames(g), ",") != "Laser cutter,Extractor" || g.Hidden != 1 || len(g.Links) != 1 {
		t.Fatalf("the workshop reader's graph: %d %s", rec.Code, rec.Body)
	}
	if bytes.Contains(rec.Body.Bytes(), []byte(sc.breaker)) || bytes.Contains(rec.Body.Bytes(), []byte("Main breaker")) || bytes.Contains(rec.Body.Bytes(), []byte("Substation")) {
		t.Errorf("the hidden asset shows: %s", rec.Body)
	}
	// From the breaker, which they cannot read, there is nothing.
	if rec := doJSON(t, sc.srv, sc.workshop, http.MethodGet, "/api/v1/assets/"+sc.breaker+"/graph", ""); rec.Code != http.StatusNotFound {
		t.Errorf("the breaker's graph: %d", rec.Code)
	}
	// What needs the substation, as they see it: nothing past the breaker.
	rec = doJSON(t, sc.srv, sc.workshop, http.MethodGet, "/api/v1/assets/"+sc.substation+"/graph?direction=in", "")
	if g = readGraph(t, rec.Body); strings.Join(graphAssetNames(g), ",") != "Substation" || g.Hidden != 1 {
		t.Errorf("what needs the substation: %s", rec.Body)
	}

	for _, bad := range []string{"?depth=11", "?depth=x", "?direction=up", "?types=missing"} {
		if rec := doJSON(t, sc.srv, sc.admin, http.MethodGet, path+bad, ""); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", bad, rec.Code, rec.Body)
		}
	}

	// The map of the tools, as the workshop reader reads them.
	rec = doJSON(t, sc.srv, sc.workshop, http.MethodGet, "/api/v1/assets/graph?category_id="+sc.tools.ID, "")
	g = readGraph(t, rec.Body)
	if rec.Code != http.StatusOK || len(g.Assets) != 3 || len(g.Links) != 1 || g.Root != "" {
		t.Errorf("the map: %d %s", rec.Code, rec.Body)
	}

	// Drawn as SVG and PDF.
	for _, p := range []string{path + ".svg", path + ".pdf", "/api/v1/assets/graph.pdf?category_id=" + sc.tools.ID, "/api/v1/assets/graph.svg"} {
		rec := doJSON(t, sc.srv, sc.workshop, http.MethodGet, p, "")
		if rec.Code != http.StatusOK || !(bytes.HasPrefix(rec.Body.Bytes(), []byte("%PDF")) || bytes.Contains(rec.Body.Bytes()[:min(200, rec.Body.Len())], []byte("<svg"))) {
			t.Errorf("%s: %d %.80s", p, rec.Code, rec.Body)
		}
	}

	// Whether a type is followed changes while links use it.
	rec = doJSON(t, sc.srv, sc.admin, http.MethodGet, "/api/v1/relation-types", "")
	var types struct {
		RelationTypes []models.RelationType `json:"relation_types"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &types)
	for _, rt := range types.RelationTypes {
		if rt.Name != "depends on" {
			continue
		}
		if !rt.IsFollowed {
			t.Error("depends on is not followed")
		}
		rec := doJSON(t, sc.srv, sc.admin, http.MethodPut, "/api/v1/relation-types/"+rt.ID, `{"name":"depends on","inverse_name":"needed by","is_followed":false}`)
		if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `"is_followed":true`) {
			t.Errorf("unfollowing: %d %s", rec.Code, rec.Body)
		}
	}
	rec = doJSON(t, sc.srv, sc.admin, http.MethodGet, path, "")
	if g = readGraph(t, rec.Body); len(g.Assets) != 1 || len(g.Types) != 0 {
		t.Errorf("with nothing followed: %s", rec.Body)
	}
}

func TestReportsCountWhatNeedsAnAsset(t *testing.T) {
	sc := newGraphScene(t)
	spec := `{"filter":{"category_ids":["` + sc.tools.ID + `"]},"sections":[{"type":"table","sort_by":1,"descending":true,"columns":[` +
		`{"heading":"Name","binding":"{{asset.name}}"},{"heading":"Needed by, in all","binding":"{{asset.needed_by_all}}"},` +
		`{"heading":"Needed by","binding":"{{asset.needed_by}}"},{"heading":"Needs","binding":"{{asset.needs}}"},{"heading":"Needs, in all","binding":"{{asset.needs_all}}"}]}]}`
	rec := doJSON(t, sc.srv, sc.admin, http.MethodPost, "/api/v1/reports/preview", `{"name":"Reach","spec":`+spec+`}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview: %d %s", rec.Code, rec.Body)
	}
	var res struct {
		Result struct {
			Sections []struct {
				Groups []struct {
					Rows []struct{ Cells []string }
				}
			}
		}
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil || len(res.Result.Sections) != 1 {
		t.Fatalf("result: %s", rec.Body)
	}
	var rows []string
	for _, row := range res.Result.Sections[0].Groups[0].Rows {
		rows = append(rows, strings.Join(row.Cells, " "))
	}
	// Ranked by what would stop if each went: the substation's loss
	// reaches furthest.
	want := []string{"Substation 3 1 0 0", "Extractor 1 1 1 2", "Laser cutter 0 0 1 3"}
	if strings.Join(rows, "; ") != strings.Join(want, "; ") {
		t.Errorf("rows %q", rows)
	}

	// On a label, too, as the editor previews it.
	rec = doJSON(t, sc.srv, sc.admin, http.MethodPost, "/api/v1/templates/preview-data", `{"asset_id":"`+sc.extractor+`"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"links":{"needs":"1","needs_all":"2","needed_by":"1","needed_by_all":"1"}`) {
		t.Errorf("preview data: %d %s", rec.Code, rec.Body)
	}
}

func TestFollowingLinksOverMCP(t *testing.T) {
	sc := newGraphScene(t)
	cs := mcpSession(t, sc.srv, tokenWith(t, sc.srv, sc.adminID, "assistant", rbac.PermAssetsRead))
	var g struct {
		Assets []map[string]any `json:"assets"`
		Links  []map[string]any `json:"links"`
		Hidden int              `json:"hidden"`
	}
	res := callTool(t, cs, "asset_graph", map[string]any{"asset": sc.breaker, "direction": "in", "depth": 3})
	if res.IsError {
		t.Fatalf("asset_graph: %s", toolText(res))
	}
	structured(t, res, &g)
	if len(g.Assets) != 3 || len(g.Links) != 2 || !strings.Contains(toolText(res), "3 assets and 2 links") {
		t.Errorf("what needs the breaker: %+v, %s", g, toolText(res))
	}
	if _, ok := g.Assets[0]["layer"]; ok {
		t.Error("an assistant is told where the drawing puts it")
	}
	res = callTool(t, cs, "asset_graph", map[string]any{"category_id": sc.tools.ID})
	structured(t, res, &g)
	if res.IsError || len(g.Assets) != 3 {
		t.Errorf("the map: %s", toolText(res))
	}
	if res := callTool(t, cs, "asset_graph", map[string]any{"depth": 2}); !res.IsError {
		t.Error("a depth without an asset")
	}
	if res := callTool(t, cs, "asset_graph", map[string]any{"asset": sc.laser, "depth": store.MaxGraphDepth + 1}); !res.IsError {
		t.Error("too deep")
	}
}
