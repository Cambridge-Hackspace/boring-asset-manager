package api

import (
	"bytes"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/linkgraph"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/render"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Graphs of followed links (docs/data-model.md#following-links): what an
// asset needs and what needs it, through several hops, and the map of a
// category or a search. An asset the request may not read is counted as
// hidden, never named, and never followed through.

// canRead says whether the request may read an asset in these scopes.
func canRead(r *http.Request) store.CanRead {
	return func(scopes []string) bool { return Allows(r.Context(), rbac.PermAssetsRead, scopes) }
}

// graphTypes reads ?types=, followed link types' IDs, comma-separated.
func graphTypes(q url.Values) []string {
	var ids []string
	for _, v := range q["types"] {
		for _, id := range strings.Split(v, ",") {
			if id = strings.TrimSpace(id); id != "" {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// graphOptions reads a graph's query: types, direction and depth.
func graphOptions(q url.Values) (store.GraphOptions, error) {
	opts := store.GraphOptions{TypeIDs: graphTypes(q), Direction: q.Get("direction")}
	if v := q.Get("depth"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > store.MaxGraphDepth {
			return opts, errors.New("depth must be a whole number from 1 to " + strconv.Itoa(store.MaxGraphDepth))
		}
		opts.Depth = n
	}
	return opts, nil
}

// writeGraphError answers a graph that could not be followed.
func writeGraphError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrInvalidGraph):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, store.ErrAssetNotFound):
		writeJSONError(w, http.StatusNotFound, "Asset not found")
	default:
		writeServerError(w, err, "follow the links")
	}
}

// assetGraph follows an asset's links as the request asks, laid out, with
// the asset as the request reads it. On a failure it answers the request.
func (s *Server) assetGraph(w http.ResponseWriter, r *http.Request) (*models.AssetGraph, *models.AssetWithDetails, bool) {
	a, ok := s.assetFor(w, r, r.PathValue("id"))
	if !ok {
		return nil, nil, false
	}
	opts, err := graphOptions(r.URL.Query())
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return nil, nil, false
	}
	g, err := s.store.FollowLinks(a.Asset.ID, opts, canRead(r))
	if err != nil {
		writeGraphError(w, err)
		return nil, nil, false
	}
	linkgraph.Layout(g)
	return g, a, true
}

// handleAssetGraph: GET /api/v1/assets/{id}/graph[?types=&direction=&depth=]
func (s *Server) handleAssetGraph(w http.ResponseWriter, r *http.Request) {
	g, _, ok := s.assetGraph(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"graph": g})
}

// mapGraph is the map of the assets the asset list's filters select, as
// the request may read them, with the followed links among them, laid out,
// and what it is of. On a failure it answers the request.
func (s *Server) mapGraph(w http.ResponseWriter, r *http.Request) (*models.AssetGraph, string, bool) {
	q := r.URL.Query()
	assets, ok := s.filterAssets(w, r, q)
	if !ok {
		return nil, "", false
	}
	g, err := s.store.GraphAmong(assets, graphTypes(q))
	if err != nil {
		writeGraphError(w, err)
		return nil, "", false
	}
	linkgraph.Layout(g)
	of := "every asset"
	if id := q.Get("category_id"); id != "" {
		if c, err := s.store.GetCategoryByID(id); err == nil {
			of = c.Name
		}
	}
	if search := q.Get("search"); search != "" {
		of += ", matching “" + search + "”"
	}
	return g, of, true
}

// handleMapGraph: GET /api/v1/assets/graph, with the asset list's filters
// and ?types=.
func (s *Server) handleMapGraph(w http.ResponseWriter, r *http.Request) {
	g, _, ok := s.mapGraph(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"graph": g})
}

// handleAssetGraphDrawing draws an asset's graph as SVG or PDF:
// GET /api/v1/assets/{id}/graph.svg and graph.pdf.
func (s *Server) handleAssetGraphDrawing(format string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		g, a, ok := s.assetGraph(w, r)
		if !ok {
			return
		}
		s.writeGraphDrawing(w, r, g, a.Asset.Name+": what it needs, and what needs it", nameStem(a.Asset.Name, "asset")+"-graph", format)
	}
}

// handleMapGraphDrawing draws the map as SVG or PDF:
// GET /api/v1/assets/graph.svg and graph.pdf.
func (s *Server) handleMapGraphDrawing(format string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		g, of, ok := s.mapGraph(w, r)
		if !ok {
			return
		}
		s.writeGraphDrawing(w, r, g, "Graph of "+of, "graph", format)
	}
}

// writeGraphDrawing draws a graph on a page, landscape unless the query
// asks otherwise, as a PDF, or the page as SVG.
func (s *Server) writeGraphDrawing(w http.ResponseWriter, r *http.Request, g *models.AssetGraph, title, stem, format string) {
	req, ok := s.documentOptions(w, r, title)
	if !ok {
		return
	}
	if r.URL.Query().Get("orientation") == "" {
		req.opts.Landscape = true
	}
	d, err := render.NewDocument(req.opts)
	if err != nil {
		writeServerError(w, err, "start the drawing")
		return
	}
	d.Graph(linkgraph.Drawing(g, title))
	if format == "pdf" {
		writeDocument(w, d, stem)
		return
	}
	var buf bytes.Buffer
	if err := render.WriteSVG(&buf, d.Pages()[0]); err != nil {
		writeServerError(w, err, "write the SVG")
		return
	}
	setDownloadHeaders(w, "image/svg+xml", stem+".svg", "attachment")
	_, _ = w.Write(buf.Bytes())
}
