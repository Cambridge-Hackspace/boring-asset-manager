package api

import (
	"net/http"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// AssetCopy is a new asset like another, to create with POST
// /api/v1/assets and then link as its links say (README §3.2).
type AssetCopy struct {
	Name        string            `json:"name"`
	CategoryIDs []string          `json:"category_ids"`
	FieldValues map[string]string `json:"field_values"`
	// Stock is the original's, quantity and all, or nil when it is not counted.
	Stock *StockRequest `json:"stock"`
	// GeneratorKeys are the next codes in the series of the original's.
	GeneratorKeys []models.AssetGeneratorKey `json:"generator_keys"`
	Links         []CopiedLink               `json:"links"`
	// IconName is the original's own icon, if it has one the build still
	// draws; empty, the copy is drawn with its categories'.
	IconName string `json:"icon_name,omitempty"`
}

// CopiedLink is a link the copy makes as its original has it: to add with
// POST /api/v1/assets/{id}/links once the copy is made.
type CopiedLink struct {
	TypeID string `json:"type_id"`
	// Label is the type's name read from the copy, as on the original.
	Label      string             `json:"label"`
	Asset      models.LinkedAsset `json:"asset"`
	Outgoing   bool               `json:"outgoing"`
	IsLocation bool               `json:"is_location"`
}

// handleCopyAsset: GET /api/v1/assets/{id}/copy answers with a new asset
// like this one, made of nothing yet, for a form to fill and a person to
// change before saving: its name, categories, fields, stock, its own icon,
// the next codes in the series of its own, and its links, but for those
// that would take the other asset from it, as a box's contents would be.
func (s *Server) handleCopyAsset(w http.ResponseWriter, r *http.Request) {
	a, ok := s.assetFor(w, r, r.PathValue("id"), rbac.PermAssetsWrite)
	if !ok {
		return
	}
	a = s.shown(r, a)
	out := AssetCopy{Name: a.Asset.Name, CategoryIDs: []string{}, FieldValues: map[string]string{}, GeneratorKeys: []models.AssetGeneratorKey{}, Links: []CopiedLink{}}
	for _, c := range a.Categories {
		if c.ID != store.UncategorizedCategoryID {
			out.CategoryIDs = append(out.CategoryIDs, c.ID)
		}
	}
	// What its categories give it; values kept from former ones stay behind.
	for _, f := range a.EffectiveFields {
		if v := a.FieldValues[f.Field.Key]; v != "" {
			out.FieldValues[f.Field.Key] = v
		}
	}
	if ic := a.ResolvedIcon; ic != nil && ic.From == models.IconFromAsset && !ic.Missing {
		out.IconName = ic.Name
	}
	if st := a.Asset.Stock; st != nil {
		q := st.Quantity
		out.Stock = &StockRequest{Quantity: &q, Unit: st.Unit, LowStockThreshold: st.LowStockThreshold}
	}
	for _, k := range a.GeneratorKeys {
		next, err := s.store.NextCodeLike(k.GeneratorType, k.AssignedCode)
		if err != nil {
			writeServerError(w, err, "suggest a code")
			return
		}
		if next != "" {
			out.GeneratorKeys = append(out.GeneratorKeys, models.AssetGeneratorKey{GeneratorType: k.GeneratorType, AssignedCode: next})
		}
	}

	types, err := s.store.ListRelationTypes()
	if err != nil {
		writeServerError(w, err, "list relation types")
		return
	}
	byID := map[string]models.RelationType{}
	for _, t := range types {
		byID[t.ID] = t
	}
	for _, l := range a.Links {
		t, known := byID[l.TypeID]
		// The far end of a single type has one such link: the copy taking
		// it would take it from the original (a box's contents).
		if !known || l.Asset.Archived || (t.IsSingle && l.Outgoing) {
			continue
		}
		// A link changes both assets, so only those the caller may change.
		other, err := s.store.GetAssetByID(l.Asset.ID)
		if err != nil || !Allows(r.Context(), rbac.PermAssetsWrite, other.Scopes) {
			continue
		}
		out.Links = append(out.Links, CopiedLink{TypeID: l.TypeID, Label: l.Label, Asset: l.Asset, Outgoing: l.Outgoing, IsLocation: t.IsLocation})
	}
	writeJSON(w, http.StatusOK, map[string]any{"copy": out})
}
