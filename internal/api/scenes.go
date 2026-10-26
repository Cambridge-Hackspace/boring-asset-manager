package api

import (
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
func firstCode(a *models.AssetWithDetails) string {
	for _, t := range []string{"qr", "micro_qr", "code128"} {
		for _, k := range a.GeneratorKeys {
			if k.GeneratorType == t {
				return k.AssignedCode
			}
		}
	}
	if len(a.GeneratorKeys) > 0 {
		return a.GeneratorKeys[0].AssignedCode
	}
	return ""
}

// GET /api/v1/assets/{id}/scene: what is in an asset, laid out on its
// floor; contents the caller cannot read are left out.
func (s *Server) handleAssetScene(w http.ResponseWriter, r *http.Request) {
	a, ok := s.assetFor(w, r, r.PathValue("id"), rbac.PermAssetsRead)
	if !ok {
		return
	}
	contents, err := s.store.FilterAssets(store.AssetFilter{Within: a.Asset.ID})
	if err != nil {
		writeServerError(w, err, "read what is in it")
		return
	}
	icons, err := s.builtIcons()
	if err != nil {
		writeServerError(w, err, "read the icons")
		return
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
		item := SceneItem{Placed: p, Name: c.Asset.Name, Code: firstCode(c), Condition: c.Asset.Condition, Icon: c.ResolvedIcon, Holds: c.Holds}
		if footprint(icons, c.ResolvedIcon) == nil {
			item.Shapes = scene.Box(p.Size)
		}
		out.Items = append(out.Items, item)
	}
	writeJSON(w, http.StatusOK, out)
}
