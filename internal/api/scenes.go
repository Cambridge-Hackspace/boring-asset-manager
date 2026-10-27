package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"sync"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex/ir"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/scene"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Scenes (README §3.7): a location's contents laid out on its floor, each
// drawn with its icon, as internal/scene lays them out.

// builtIcons is the current build's IR, read once for each build.
type builtIcons struct {
	mu   sync.Mutex
	hash string
	doc  *ir.Document
}

// icons returns the current build's icons by name, none when there is no
// build.
func (s *Server) builtIcons() (map[string]*ir.Icon, error) {
	b, err := s.store.CurrentIconBuild()
	if err != nil || b == nil {
		return map[string]*ir.Icon{}, err
	}
	s.built.mu.Lock()
	defer s.built.mu.Unlock()
	if s.built.hash != b.Hash {
		doc, err := ir.Parse(b.IR)
		if err != nil {
			return nil, err
		}
		s.built.hash, s.built.doc = b.Hash, doc
	}
	out := make(map[string]*ir.Icon, len(s.built.doc.Icons))
	for i := range s.built.doc.Icons {
		out[s.built.doc.Icons[i].Name] = &s.built.doc.Icons[i]
	}
	return out, nil
}

// SceneItem is an asset in a scene, where it stands and how it is drawn:
// with its icon, or, with none in three dimensions, with Shapes, a plain
// box its size.
type SceneItem struct {
	scene.Placed
	Name      string               `json:"name"`
	Code      string               `json:"code,omitempty"`
	Condition string               `json:"condition"`
	Icon      *models.ResolvedIcon `json:"icon,omitempty"`
	Shapes    []ir.Shape           `json:"shapes,omitempty"`
	// Holds is how many things are in it, to look inside.
	Holds int `json:"holds"`
	// Movable is set when the caller may arrange it.
	Movable bool `json:"movable,omitempty"`
}

// SceneResponse is a location's scene: the location, the way down to it,
// outermost first, its floor and viewBox, and its contents, back to
// front.
type SceneResponse struct {
	Location struct {
		ID   string               `json:"id"`
		Name string               `json:"name"`
		Icon *models.ResolvedIcon `json:"icon,omitempty"`
	} `json:"location"`
	Path    []models.LinkedAsset `json:"path"`
	Floor   [2]float64           `json:"floor"`
	Viewbox [4]float64           `json:"viewbox"`
	Items   []SceneItem          `json:"items"`
}

// footprint is the room an icon takes, nil for none or a flat one.
func footprint(icons map[string]*ir.Icon, ic *models.ResolvedIcon) *[3]float64 {
	if ic == nil {
		return nil
	}
	if icon := icons[ic.Name]; icon != nil && icon.Footprint != nil {
		return icon.Footprint
	}
	return nil
}

// firstCode is an asset's code to show: its first, by the order codes are
// listed.
func firstCode(a *models.AssetWithDetails) string { return a.FirstCode() }

// GET /api/v1/assets/{id}/scene: what is in an asset, laid out on its
// floor; contents the caller cannot read are left out.
func (s *Server) handleAssetScene(w http.ResponseWriter, r *http.Request) {
	a, ok := s.assetFor(w, r, r.PathValue("id"), rbac.PermAssetsRead)
	if !ok {
		return
	}
	s.writeScene(w, r, a)
}

// writeScene answers with a location's scene, as the caller may see it.
func (s *Server) writeScene(w http.ResponseWriter, r *http.Request, a *models.AssetWithDetails) {
	out, _, err := s.sceneFor(r, a)
	if err != nil {
		writeServerError(w, err, "lay out the scene")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// sceneFor lays out a location's scene as the caller may see it, with the
// build's icons it is drawn with.
func (s *Server) sceneFor(r *http.Request, a *models.AssetWithDetails) (*SceneResponse, map[string]*ir.Icon, error) {
	contents, err := s.store.FilterAssets(store.AssetFilter{Within: a.Asset.ID})
	if err != nil {
		return nil, nil, err
	}
	placements, err := s.store.Placements(a.Asset.ID)
	if err != nil {
		return nil, nil, err
	}
	icons, err := s.builtIcons()
	if err != nil {
		return nil, nil, err
	}
	byID := map[string]*models.AssetWithDetails{}
	var items []scene.Item
	for i := range contents {
		c := &contents[i]
		if c.Asset.ID == a.Asset.ID || !Allows(r.Context(), rbac.PermAssetsRead, c.Scopes) {
			continue
		}
		byID[c.Asset.ID] = c
		item := scene.Item{ID: c.Asset.ID, Name: c.Asset.Name}
		if f := footprint(icons, c.ResolvedIcon); f != nil {
			item.Size = *f
		}
		if at, ok := placements[c.Asset.ID]; ok {
			item.At = &at
		}
		items = append(items, item)
	}
	laid := scene.Layout(footprint(icons, a.ResolvedIcon), items)

	var out SceneResponse
	out.Location.ID, out.Location.Name, out.Location.Icon = a.Asset.ID, a.Asset.Name, a.ResolvedIcon
	out.Path = append(append([]models.LinkedAsset{}, a.Location...), models.LinkedAsset{ID: a.Asset.ID, Name: a.Asset.Name})
	out.Floor, out.Viewbox = laid.Floor, laid.Viewbox
	out.Items = make([]SceneItem, 0, len(laid.Items))
	for _, p := range laid.Items {
		c := byID[p.ID]
		item := SceneItem{Placed: p, Name: c.Asset.Name, Code: firstCode(c), Condition: c.Asset.Condition, Icon: c.ResolvedIcon, Holds: c.Holds,
			Movable: Allows(r.Context(), rbac.PermAssetsWrite, c.Scopes)}
		if footprint(icons, c.ResolvedIcon) == nil {
			item.Shapes = scene.Box(p.Size)
		}
		out.Items = append(out.Items, item)
	}
	return &out, icons, nil
}

// defaultSceneHeight is how high a scene's image is drawn unless asked,
// and maxSceneHeight the most it may be asked to be.
const (
	defaultSceneHeight = 512
	maxSceneHeight     = 2048
)

// GET /api/v1/assets/{id}/scene/image: a location's scene drawn as an
// image, as its page draws it, in format svg or png, theme light or dark,
// height pixels high, with show, an asset's ID, outlined.
func (s *Server) handleSceneImage(w http.ResponseWriter, r *http.Request) {
	opts, ok := imageOptionsOf(w, r, defaultSceneHeight, maxSceneHeight)
	if !ok {
		return
	}
	a, ok := s.assetFor(w, r, r.PathValue("id"), rbac.PermAssetsRead)
	if !ok {
		return
	}
	laid, icons, err := s.sceneFor(r, a)
	if err != nil {
		writeServerError(w, err, "lay out the scene")
		return
	}
	colors, err := s.themeColors(opts.theme)
	if err != nil {
		writeServerError(w, err, "read the icon palette")
		return
	}
	drawn := make([]scene.Drawn, len(laid.Items))
	for i, it := range laid.Items {
		d := scene.Drawn{Placed: it.Placed, Shapes: it.Shapes, Color: colors["ink-muted"]}
		if it.Icon != nil {
			if icon := icons[it.Icon.Name]; icon != nil && it.Shapes == nil {
				d.Shapes = icon.Shapes
			}
			// drawn with its own texts
			if raw := it.Icon.Drawn[it.Icon.Name]; raw != nil && it.Shapes == nil {
				if icon, err := ir.ParseIcon(raw); err == nil {
					d.Shapes = icon.Shapes
				}
			}
			if it.Icon.Color != "" {
				d.Color = it.Icon.Color
			}
		}
		drawn[i] = d
	}
	picture, own := scene.Picture(a.Asset.Name, laid.Floor, laid.Viewbox, drawn, r.URL.Query().Get("show"))
	for token, c := range own {
		colors[token] = c
	}
	hash := "none"
	if b, err := s.store.CurrentIconBuild(); err == nil && b != nil {
		hash = b.Hash
	}
	writeIconImage(w, r, hash, picture, colors, opts)
}

// PlacementRequest places an asset in a location's scene, at X, Y and Z in
// tiles from the floor's corner: in the location In, or with none, the
// one it was put in first.
type PlacementRequest struct {
	In string  `json:"in,omitempty"`
	X  float64 `json:"x"`
	Y  float64 `json:"y"`
	Z  float64 `json:"z"`
}

// PUT /api/v1/assets/{id}/placement: place an asset where it stands in
// its location's scene; DELETE leaves it to be packed again (?in= names
// the location). Either answers with the location's scene.
func (s *Server) handlePlaceAsset(w http.ResponseWriter, r *http.Request) {
	var at *[3]float64
	in := r.URL.Query().Get("in")
	if r.Method == http.MethodPut {
		var req PlacementRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "x, y and z are required")
			return
		}
		at, in = &[3]float64{req.X, req.Y, req.Z}, req.In
	}
	if _, ok := s.assetFor(w, r, r.PathValue("id"), rbac.PermAssetsWrite); !ok {
		return
	}
	placeID, err := s.actingStore(r).PlaceAsset(r.PathValue("id"), in, at)
	switch {
	case errors.Is(err, store.ErrAssetNotFound):
		writeJSONError(w, http.StatusNotFound, "Asset not found")
		return
	case errors.Is(err, store.ErrInvalidPlacement):
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	case errors.Is(err, store.ErrNotInLocation), errors.Is(err, store.ErrAssetArchived):
		writeJSONError(w, http.StatusConflict, err.Error())
		return
	case err != nil:
		writeStoreError(w, err, "place the asset")
		return
	}
	place, ok := s.assetFor(w, r, placeID, rbac.PermAssetsRead)
	if !ok {
		return
	}
	s.writeScene(w, r, place)
}
