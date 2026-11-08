package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/reports"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Categories that imply other categories, and the pools they make
// (docs/stock-and-lending.md#pools). People see only what they may: the
// places listed, and any total shown, are of the assets they can read,
// while alerts, which go to channels, use the real totals.

// visibleAssets returns, of these assets, a test for those the request
// may read.
func (s *Server) visibleAssets(r *http.Request, ids []string) (func(string) bool, error) {
	if UserPermissions(r.Context())[rbac.PermAssetsRead] {
		return func(string) bool { return true }, nil // reads everything
	}
	scopes, err := s.store.AssetScopes(ids)
	if err != nil {
		return nil, err
	}
	return func(id string) bool {
		sc, ok := scopes[id]
		return ok && Allows(r.Context(), rbac.PermAssetsRead, sc)
	}, nil
}

// poolsFor lists the pools an asset counts in, with the places the request
// may see, the asset itself always among them, each pool totaled over
// the assets in it the request sees.
func (s *Server) poolsFor(r *http.Request, id string) ([]models.AssetPool, error) {
	pools, err := s.store.AssetPools(id)
	if err != nil || len(pools) == 0 {
		return nil, err
	}
	var ids []string
	for _, p := range pools {
		for _, pl := range p.Places {
			ids = append(ids, pl.AssetID)
		}
	}
	can, err := s.visibleAssets(r, ids)
	if err != nil {
		return nil, err
	}
	for i := range pools {
		p := &pools[i]
		places := []models.PoolPlace{}
		for _, pl := range p.Places {
			if pl.AssetID != id && !can(pl.AssetID) {
				continue
			}
			pl.Location = s.visibleLocation(can, pl.Location)
			places = append(places, pl)
		}
		p.Places = places
	}
	// The total of a pool is of every asset in it the request sees, those
	// with none left among them.
	members, err := s.store.AssetPoolMemberships()
	if err != nil {
		return nil, err
	}
	var all []string
	for aid := range members {
		all = append(all, aid)
	}
	can, err = s.visibleAssets(r, all)
	if err != nil {
		return nil, err
	}
	for i := range pools {
		p := &pools[i]
		p.Total = 0
		for aid, m := range members {
			if aid != id && !can(aid) {
				continue
			}
			for _, pid := range m.Pools {
				if pid == p.CategoryID {
					p.Total += m.Quantity
				}
			}
		}
	}
	return pools, nil
}

// visibleLocation keeps the places of a location the request may see.
func (s *Server) visibleLocation(can func(string) bool, location []models.LinkedAsset) []models.LinkedAsset {
	out := []models.LinkedAsset{}
	for _, l := range location {
		if can(l.ID) {
			out = append(out, l)
		}
	}
	return out
}

// poolData works out what a report grouped by pool needs: the pools each
// asset adds to, and each pool's total over the assets the request may
// see.
func (s *Server) poolData(r *http.Request) (*reports.PoolData, error) {
	members, err := s.store.AssetPoolMemberships()
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(members))
	for id := range members {
		ids = append(ids, id)
	}
	can, err := s.visibleAssets(r, ids)
	if err != nil {
		return nil, err
	}
	data := &reports.PoolData{Of: map[string][]string{}, Totals: map[string]int{}}
	for id, m := range members {
		if !can(id) {
			continue
		}
		data.Of[id] = m.Pools
		for _, p := range m.Pools {
			data.Totals[p] += m.Quantity
		}
	}
	return data, nil
}

// categoryLevel measures a category over the assets the request may see:
// how many, and with a unit, their total in it.
func (s *Server) categoryLevel(r *http.Request, cat *models.Category) (count, total int, err error) {
	members, err := s.store.CategoryAssets(cat.ID, cat.Unit)
	if err != nil {
		return 0, 0, err
	}
	ids := make([]string, 0, len(members))
	for id := range members {
		ids = append(ids, id)
	}
	can, err := s.visibleAssets(r, ids)
	if err != nil {
		return 0, 0, err
	}
	for id, q := range members {
		if can(id) {
			count++
			total += q
		}
	}
	return count, total, nil
}

// CategoryLinksRequest sets a category's links: the categories its members
// also count in, and those whose members count in it. A list left out
// leaves that side as it is.
type CategoryLinksRequest struct {
	Implies    *[]string `json:"implies"`
	IncludedBy *[]string `json:"included_by"`
}

// handleSetCategoryLinks: PUT /api/v1/categories/{id}/links. A link that
// would lead back to where it starts is refused (409); one adding nothing
// to a total is made, with a warning.
func (s *Server) handleSetCategoryLinks(w http.ResponseWriter, r *http.Request) {
	var req CategoryLinksRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	cat, err := s.actingStore(r).SetCategoryLinks(r.PathValue("id"), store.CategoryLinks{Implies: req.Implies, IncludedBy: req.IncludedBy})
	switch {
	// The category itself; one named in the lists comes wrapped, and is the
	// request's mistake.
	case err == store.ErrCategoryNotFound:
		writeJSONError(w, http.StatusNotFound, "Category not found")
		return
	case errors.Is(err, store.ErrImpliesCycle):
		writeJSONError(w, http.StatusConflict, err.Error())
		return
	case err != nil:
		writeStoreError(w, err, "link the categories")
		return
	}
	if err := s.showCategory(r, cat); err != nil {
		writeServerError(w, err, "link the categories")
		return
	}
	warnings, err := s.store.CategoryLinkWarnings(cat.ID)
	if err != nil {
		writeServerError(w, err, "check the links")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"category": cat, "link_warnings": warnings})
}

// MoveStockRequest moves stock to another asset.
type MoveStockRequest struct {
	To     string `json:"to"`
	Amount int    `json:"amount"`
}

// handleMoveStock: POST /api/v1/assets/{id}/move moves stock from the asset
// to another in one step (docs/stock-and-lending.md#moving-stock). It needs
// stock.record in both assets' scopes, and refuses more than there is (409).
func (s *Server) handleMoveStock(w http.ResponseWriter, r *http.Request) {
	var req MoveStockRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.To == "" {
		writeJSONError(w, http.StatusBadRequest, "Give to, the asset to move it to, and amount, how much")
		return
	}
	if _, ok := s.assetFor(w, r, r.PathValue("id"), rbac.PermStockRecord); !ok {
		return
	}
	if _, ok := s.assetFor(w, r, req.To, rbac.PermStockRecord); !ok {
		return
	}
	from, to, err := s.actingStore(r).MoveStock(r.PathValue("id"), req.To, req.Amount)
	switch {
	case errors.Is(err, store.ErrAssetNotFound):
		writeJSONError(w, http.StatusNotFound, "Asset not found")
	case errors.Is(err, store.ErrNotEnoughStock):
		writeJSONError(w, http.StatusConflict, err.Error())
	case err != nil:
		writeStoreError(w, err, "move the stock")
	default:
		writeJSON(w, http.StatusOK, map[string]any{"from": s.shown(r, from), "to": s.shown(r, to)})
	}
}
