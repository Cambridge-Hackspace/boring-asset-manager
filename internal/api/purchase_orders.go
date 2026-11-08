package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Purchase orders (docs/purchase-orders.md): orders to a vendor, which
// follow it as contracts do. Whoever sees the vendor sees its orders, and
// vendors.manage in its scopes makes, changes, places, cancels and
// deletes them. Their prices need purchases.read in the vendor's scopes,
// or in every scope for a vendor in none, as contracts' costs do.
// Receiving is recording stock where the stock is: stock.record for an
// asset restocked, and assets.write where the assets made go.

// OrderRequest is an order as created or replaced: every property, so an
// absent one is emptied, but for lines, which stay as they are when
// absent.
type OrderRequest struct {
	VendorID        string              `json:"vendor_id"`
	Currency        string              `json:"currency"`
	VendorReference string              `json:"vendor_reference"`
	OrderedOn       string              `json:"ordered_on"`
	ExpectedOn      string              `json:"expected_on"`
	DeliverTo       string              `json:"deliver_to"`
	Notes           string              `json:"notes"`
	Lines           *[]OrderLineRequest `json:"lines"`
}

// OrderLineRequest is a line as given: with the id of one of the order's
// lines to keep it. A line drawn from a catalog entry names it. unit_price
// left out keeps a line's price, and gives a new line drawn from an entry
// the entry's; empty, it has none.
type OrderLineRequest struct {
	ID             string  `json:"id"`
	CatalogEntryID string  `json:"catalog_entry_id"`
	CategoryID     string  `json:"category_id"`
	AssetID        string  `json:"asset_id"`
	PartNumber     string  `json:"part_number"`
	Description    string  `json:"description"`
	PackSize       *int    `json:"pack_size"`
	PackUnit       string  `json:"pack_unit"`
	Quantity       int     `json:"quantity"`
	UnitPrice      *string `json:"unit_price"`
}

// orderFor reads the order a request names, answering 404 when the
// requester may not see its vendor, and 403 when manage is set and they
// may not manage it. It returns its vendor's scopes' names.
func (s *Server) orderFor(w http.ResponseWriter, r *http.Request, id string, manage bool) (*models.PurchaseOrder, []string, bool) {
	o, err := s.store.GetPurchaseOrder(id)
	if errors.Is(err, store.ErrOrderNotFound) {
		writeJSONError(w, http.StatusNotFound, "Purchase order not found")
		return nil, nil, false
	}
	if err != nil {
		writeServerError(w, err, "read the purchase order")
		return nil, nil, false
	}
	seen, scopes, err := s.visibleVendors(r.Context())
	if err != nil {
		writeServerError(w, err, "read the purchase order's vendor")
		return nil, nil, false
	}
	if _, ok := seen[o.VendorID]; !ok {
		writeJSONError(w, http.StatusNotFound, "Purchase order not found")
		return nil, nil, false
	}
	if manage && !Allows(r.Context(), rbac.PermVendorsManage, scopes[o.VendorID]) {
		writeJSONError(w, http.StatusForbidden, "Forbidden: insufficient permissions for this purchase order's vendor's scopes")
		return nil, nil, false
	}
	return o, scopes[o.VendorID], true
}

// shownOrders are orders as the request may see them: without their
// prices where it may not see them, and without the assets it may not
// read, whose lines keep their own text. scopes are each vendor's scopes'
// names, by ID. A failure to read the assets' scopes hides every asset.
func (s *Server) shownOrders(ctx context.Context, os []models.PurchaseOrder, scopes map[string][]string) []models.PurchaseOrder {
	var ids []string
	for _, o := range os {
		for _, l := range o.Lines {
			if l.AssetID != "" {
				ids = append(ids, l.AssetID)
			}
		}
	}
	assetScopes, err := s.store.AssetScopes(ids)
	if err != nil {
		assetScopes = map[string][]string{}
	}
	out := make([]models.PurchaseOrder, len(os))
	for i, o := range os {
		o.Lines = append([]models.PurchaseOrderLine{}, o.Lines...)
		for j := range o.Lines {
			l := &o.Lines[j]
			if l.AssetID == "" {
				continue
			}
			if sc, ok := assetScopes[l.AssetID]; !ok || !Allows(ctx, rbac.PermAssetsRead, sc) {
				l.AssetID, l.LinkedName, l.CatalogEntryID = "", "", ""
			}
		}
		if !seesCosts(ctx, scopes[o.VendorID]) {
			o.HidePrices()
		}
		o.CanManage = Allows(ctx, rbac.PermVendorsManage, scopes[o.VendorID])
		out[i] = o
	}
	return out
}

// shownOrder is one order as shownOrders shows it.
func (s *Server) shownOrder(ctx context.Context, o *models.PurchaseOrder, scopes []string) models.PurchaseOrder {
	return s.shownOrders(ctx, []models.PurchaseOrder{*o}, map[string][]string{o.VendorID: scopes})[0]
}

// orderInput is what a request replaces an order with, keeping what the
// request may not see as had has it, nil for one being created: its
// currency and prices unless prices says the request sees them
// (purchases.read), what lines are for of assets it may not read, and its
// lines when the request leaves them out. Each asset a line names, or its
// catalog entry is for, must be one the request reads. When it fails, it
// answers the request.
func (s *Server) orderInput(w http.ResponseWriter, r *http.Request, req OrderRequest, had *models.PurchaseOrder, prices bool) (store.OrderInput, bool) {
	in := store.OrderInput{VendorID: req.VendorID, Currency: req.Currency, VendorReference: req.VendorReference, OrderedOn: req.OrderedOn,
		ExpectedOn: req.ExpectedOn, DeliverTo: req.DeliverTo, Notes: req.Notes}
	var kept models.PurchaseOrder
	if had != nil {
		kept = *had
	}
	if !prices {
		in.Currency = kept.Currency
	}
	if req.Lines == nil {
		in.Lines = store.OrderInputOf(kept).Lines
		return in, true
	}
	hadLines := map[string]models.PurchaseOrderLine{}
	for _, l := range kept.Lines {
		hadLines[l.ID] = l
	}
	var ids []string
	for _, l := range kept.Lines {
		if l.AssetID != "" {
			ids = append(ids, l.AssetID)
		}
	}
	for _, l := range *req.Lines {
		if l.AssetID != "" {
			ids = append(ids, l.AssetID)
		}
		if l.CatalogEntryID != "" {
			if e, err := s.store.GetCatalogEntry(l.CatalogEntryID); err == nil && e.AssetID != "" {
				ids = append(ids, e.AssetID)
			}
		}
	}
	assetScopes, err := s.store.AssetScopes(ids)
	if err != nil {
		writeServerError(w, err, "read the assets' scopes")
		return in, false
	}
	reads := func(id string) bool {
		sc, ok := assetScopes[id]
		return ok && Allows(r.Context(), rbac.PermAssetsRead, sc)
	}
	in.Lines = []store.OrderLineInput{}
	for _, l := range *req.Lines {
		line := store.OrderLineInput{ID: l.ID, CatalogEntryID: l.CatalogEntryID, CategoryID: l.CategoryID, AssetID: l.AssetID,
			PartNumber: l.PartNumber, Description: l.Description, PackSize: l.PackSize, PackUnit: l.PackUnit, Quantity: l.Quantity, Price: l.UnitPrice}
		if old, ok := hadLines[l.ID]; ok && old.AssetID != "" && !reads(old.AssetID) && l.AssetID == "" {
			// What it is for, which the request cannot see, stays.
			line.AssetID, line.CatalogEntryID = old.AssetID, old.CatalogEntryID
		} else {
			if l.AssetID != "" && !reads(l.AssetID) {
				writeJSONError(w, http.StatusBadRequest, "asset not found: "+l.AssetID)
				return in, false
			}
			if l.CatalogEntryID != "" {
				if e, err := s.store.GetCatalogEntry(l.CatalogEntryID); err == nil && e.AssetID != "" && !reads(e.AssetID) {
					writeJSONError(w, http.StatusBadRequest, "catalog entry not found: "+l.CatalogEntryID)
					return in, false
				}
			}
		}
		if !prices {
			line.Price = nil
		}
		in.Lines = append(in.Lines, line)
	}
	return in, true
}

// writeOrderError answers a failure to change an order.
func writeOrderError(w http.ResponseWriter, err error, action string) {
	var state *store.OrderStateError
	switch {
	case errors.As(err, &state):
		writeJSONError(w, http.StatusConflict, state.Msg)
	case errors.Is(err, store.ErrOrderNotFound):
		writeJSONError(w, http.StatusNotFound, "Purchase order not found")
	case errors.Is(err, store.ErrVendorNotFound), errors.Is(err, store.ErrAssetNotFound), errors.Is(err, store.ErrCategoryNotFound),
		errors.Is(err, store.ErrInvalidOrder), errors.Is(err, store.ErrInvalidPurchase):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, store.ErrNotEnoughStock), errors.Is(err, store.ErrQuantityOutOfRange):
		writeJSONError(w, http.StatusConflict, err.Error())
	default:
		writeStoreError(w, err, action)
	}
}

// matchesOrder reports whether an order matches a search: every word of
// it in its number, the vendor's reference, the vendor's name, or its
// lines' text, regardless of case.
func matchesOrder(o models.PurchaseOrder, q string) bool {
	parts := []string{o.Reference, strconv.Itoa(o.Number), o.VendorReference, o.VendorName}
	for _, l := range o.Lines {
		parts = append(parts, l.PartNumber, l.Description)
	}
	text := strings.ToLower(strings.Join(parts, "\n"))
	for _, word := range strings.Fields(strings.ToLower(q)) {
		if !strings.Contains(text, word) {
			return false
		}
	}
	return true
}

// handleListOrders lists the orders the requester may see, newest first:
// GET /api/v1/purchase-orders, with ?vendor_id=, ?state= (a state, or open
// for those still to come), ?category_id=, ?asset_id= (those with a line
// for it) and ?q=.
func (s *Server) handleListOrders(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	state := q.Get("state")
	if state != "" && state != "open" && !contains(models.OrderStates, state) {
		writeJSONError(w, http.StatusBadRequest, "state must be one of "+strings.Join(models.OrderStates, ", ")+", or open")
		return
	}
	all, err := s.store.ListPurchaseOrders()
	if err != nil {
		writeServerError(w, err, "list purchase orders")
		return
	}
	seen, scopes, err := s.visibleVendors(r.Context())
	if err != nil {
		writeServerError(w, err, "list purchase orders")
		return
	}
	vendor, category, asset, search := q.Get("vendor_id"), q.Get("category_id"), q.Get("asset_id"), strings.TrimSpace(q.Get("q"))
	if asset != "" {
		if _, ok := s.assetFor(w, r, asset); !ok {
			return
		}
	}
	has := func(o models.PurchaseOrder, category, asset string) bool {
		for _, l := range o.Lines {
			if (category != "" && l.CategoryID == category) || (asset != "" && l.AssetID == asset) {
				return true
			}
		}
		return false
	}
	kept := []models.PurchaseOrder{}
	for _, o := range all {
		if _, ok := seen[o.VendorID]; !ok {
			continue
		}
		switch {
		case vendor != "" && o.VendorID != vendor,
			state == "open" && !models.OrderOpen(o.State),
			state != "" && state != "open" && o.State != state,
			category != "" && !has(o, category, ""),
			asset != "" && !has(o, "", asset),
			search != "" && !matchesOrder(o, search):
			continue
		}
		kept = append(kept, o)
	}
	writeJSON(w, http.StatusOK, map[string]any{"orders": s.shownOrders(r.Context(), kept, scopes)})
}

// handleGetOrder reads an order: GET /api/v1/purchase-orders/{id}
func (s *Server) handleGetOrder(w http.ResponseWriter, r *http.Request) {
	o, scopes, ok := s.orderFor(w, r, r.PathValue("id"), false)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"order": s.shownOrder(r.Context(), o, scopes)})
}

// handleCreateOrder makes a draft order: POST /api/v1/purchase-orders
func (s *Server) handleCreateOrder(w http.ResponseWriter, r *http.Request) {
	var req OrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if strings.TrimSpace(req.VendorID) == "" {
		writeJSONError(w, http.StatusBadRequest, "an order needs a vendor")
		return
	}
	_, scopes, ok := s.vendorFor(w, r, req.VendorID, true)
	if !ok {
		return
	}
	in, ok := s.orderInput(w, r, req, nil, seesCosts(r.Context(), scopes))
	if !ok {
		return
	}
	o, err := s.actingStore(r).CreatePurchaseOrder(in)
	if err != nil {
		writeOrderError(w, err, "create the purchase order")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"order": s.shownOrder(r.Context(), o, scopes)})
}

// handleUpdateOrder replaces what an order's state lets change: PUT
// /api/v1/purchase-orders/{id}. Moving a draft to another vendor needs
// vendors.manage for both.
func (s *Server) handleUpdateOrder(w http.ResponseWriter, r *http.Request) {
	var req OrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	had, scopes, ok := s.orderFor(w, r, r.PathValue("id"), true)
	if !ok {
		return
	}
	prices := seesCosts(r.Context(), scopes)
	if req.VendorID != had.VendorID {
		if strings.TrimSpace(req.VendorID) == "" {
			writeJSONError(w, http.StatusBadRequest, "an order needs a vendor")
			return
		}
		if _, scopes, ok = s.vendorFor(w, r, req.VendorID, true); !ok {
			return
		}
		prices = prices && seesCosts(r.Context(), scopes)
	}
	in, ok := s.orderInput(w, r, req, had, prices)
	if !ok {
		return
	}
	o, err := s.actingStore(r).UpdatePurchaseOrder(had.ID, in)
	if err != nil {
		writeOrderError(w, err, "update the purchase order")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"order": s.shownOrder(r.Context(), o, scopes)})
}

// handleDeleteOrder deletes an order: DELETE /api/v1/purchase-orders/{id}
func (s *Server) handleDeleteOrder(w http.ResponseWriter, r *http.Request) {
	o, _, ok := s.orderFor(w, r, r.PathValue("id"), true)
	if !ok {
		return
	}
	if err := s.actingStore(r).DeletePurchaseOrder(o.ID); err != nil {
		writeOrderError(w, err, "delete the purchase order")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Purchase order deleted"})
}

// PlaceOrderRequest places an order, on ordered_on when given.
type PlaceOrderRequest struct {
	OrderedOn string `json:"ordered_on"`
}

// handlePlaceOrder marks a draft placed with its vendor: POST
// /api/v1/purchase-orders/{id}/place
func (s *Server) handlePlaceOrder(w http.ResponseWriter, r *http.Request) {
	var req PlaceOrderRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "Invalid request body")
			return
		}
	}
	had, scopes, ok := s.orderFor(w, r, r.PathValue("id"), true)
	if !ok {
		return
	}
	o, err := s.actingStore(r).PlacePurchaseOrder(had.ID, req.OrderedOn)
	if err != nil {
		writeOrderError(w, err, "place the purchase order")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"order": s.shownOrder(r.Context(), o, scopes)})
}

// handleCancelOrder cancels an order placed: POST
// /api/v1/purchase-orders/{id}/cancel
func (s *Server) handleCancelOrder(w http.ResponseWriter, r *http.Request) {
	had, scopes, ok := s.orderFor(w, r, r.PathValue("id"), true)
	if !ok {
		return
	}
	o, err := s.actingStore(r).CancelPurchaseOrder(had.ID)
	if err != nil {
		writeOrderError(w, err, "cancel the purchase order")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"order": s.shownOrder(r.Context(), o, scopes)})
}

// ReceiptRequest is a delivery received against an order.
type ReceiptRequest struct {
	ReceivedOn string               `json:"received_on"`
	Lines      []ReceiptLineRequest `json:"lines"`
}

// ReceiptLineRequest is what came of one line: quantity packs, restocking
// its asset by restock, or making assets of its category, like an asset,
// or named name with fields; each holding each, in a category that
// weighs; with warranty_end.
type ReceiptLineRequest struct {
	LineID      string            `json:"line_id"`
	Quantity    int               `json:"quantity"`
	Restock     *int              `json:"restock"`
	Assets      *int              `json:"assets"`
	Like        string            `json:"like"`
	Name        string            `json:"name"`
	Fields      map[string]string `json:"fields"`
	Each        *int              `json:"each"`
	WarrantyEnd string            `json:"warranty_end"`
}

// handleReceiveOrder receives a delivery: POST
// /api/v1/purchase-orders/{id}/receive. Each line needs what its change
// needs: stock.record on an asset it restocks; assets.write where the
// assets it makes go, and, like an asset, on it and the assets they link
// to; and otherwise stock.record anywhere.
func (s *Server) handleReceiveOrder(w http.ResponseWriter, r *http.Request) {
	var req ReceiptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	o, scopes, ok := s.orderFor(w, r, r.PathValue("id"), false)
	if !ok {
		return
	}
	lines := map[string]models.PurchaseOrderLine{}
	for _, l := range o.Lines {
		lines[l.ID] = l
	}
	ctx := r.Context()
	in := store.ReceiptInput{ReceivedOn: req.ReceivedOn}
	for _, rl := range req.Lines {
		line, known := lines[rl.LineID]
		out := store.ReceiptLine{LineID: rl.LineID, Quantity: rl.Quantity, Restock: rl.Restock, Assets: rl.Assets, Name: rl.Name,
			FieldValues: rl.Fields, Each: rl.Each, WarrantyEnd: rl.WarrantyEnd}
		making := known && line.CategoryID != "" && (rl.Assets == nil && rl.Quantity > 0 || rl.Assets != nil && *rl.Assets > 0)
		switch {
		case !known:
			// The store refuses it.
		case line.AssetID != "":
			sc, err := s.store.AssetScopes([]string{line.AssetID})
			if err != nil {
				writeServerError(w, err, "read the asset's scopes")
				return
			}
			if !Allows(ctx, rbac.PermStockRecord, sc[line.AssetID]) {
				writeJSONError(w, http.StatusForbidden, "Forbidden: restocking "+lineName(line)+" needs stock.record for it")
				return
			}
		case making:
			catScopes, err := s.store.ScopesForCategories([]string{line.CategoryID})
			if err != nil {
				writeServerError(w, err, "read the category's scopes")
				return
			}
			if !Allows(ctx, rbac.PermAssetsWrite, catScopes) {
				writeJSONError(w, http.StatusForbidden, "Forbidden: making the assets of "+lineName(line)+" needs assets.write in its category's scopes")
				return
			}
			if rl.Like != "" {
				t, ok := s.assetTemplate(w, r, rl.Like)
				if !ok {
					return
				}
				out.Like = t
			}
		default:
			if !HasPermission(ctx, rbac.PermStockRecord) {
				writeJSONError(w, http.StatusForbidden, "Forbidden: receiving needs stock.record")
				return
			}
		}
		in.Lines = append(in.Lines, out)
	}
	res, err := s.actingStore(r).ReceivePurchaseOrder(o.ID, in)
	if err != nil {
		writeOrderError(w, err, "receive the purchase order")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"order": s.shownOrder(ctx, res.Order, scopes), "created_asset_ids": res.Created, "restocked": res.Restocked})
}

// lineName names a line in an answer.
func lineName(l models.PurchaseOrderLine) string {
	if l.PartNumber != "" {
		return l.PartNumber
	}
	return l.Description
}

// assetTemplate is an asset to make others like, as copying it gives
// (copy.go): assets.write on it, and only the links to assets the request
// may change, and not those taking the other asset from it. When it
// fails, it answers the request.
func (s *Server) assetTemplate(w http.ResponseWriter, r *http.Request, id string) (*store.AssetTemplate, bool) {
	a, ok := s.assetFor(w, r, id, rbac.PermAssetsWrite)
	if !ok {
		return nil, false
	}
	a = s.shown(r, a)
	t := &store.AssetTemplate{Name: a.Asset.Name, FieldValues: map[string]string{}}
	for _, c := range a.Categories {
		if c.ID != store.UncategorizedCategoryID {
			t.CategoryIDs = append(t.CategoryIDs, c.ID)
		}
	}
	for _, f := range a.EffectiveFields {
		if v := a.FieldValues[f.Field.Key]; v != "" {
			t.FieldValues[f.Field.Key] = v
		}
	}
	if ic := a.ResolvedIcon; ic != nil && ic.From == models.IconFromAsset && !ic.Missing && a.Asset.IconName != nil {
		t.Icon = *a.Asset.IconName
	}
	if st := a.Asset.Stock; st != nil {
		q := st.Quantity
		t.Stock = &store.StockInput{Quantity: &q, Unit: st.Unit, LowStockThreshold: st.LowStockThreshold}
	}
	t.Codes = a.GeneratorKeys
	types, err := s.store.ListRelationTypes()
	if err != nil {
		writeServerError(w, err, "list relation types")
		return nil, false
	}
	byID := map[string]models.RelationType{}
	for _, rt := range types {
		byID[rt.ID] = rt
	}
	for _, l := range a.Links {
		rt, known := byID[l.TypeID]
		if !known || l.Asset.Archived || (rt.IsSingle && l.Outgoing) {
			continue
		}
		other, err := s.store.GetAssetByID(l.Asset.ID)
		if err != nil || !Allows(r.Context(), rbac.PermAssetsWrite, other.Scopes) {
			continue
		}
		t.Links = append(t.Links, store.TemplateLink{TypeID: l.TypeID, OtherID: l.Asset.ID, Outgoing: l.Outgoing})
	}
	return t, true
}

// handleOrderHistory lists an order's history, newest first, a page at a
// time: GET /api/v1/purchase-orders/{id}/history?before=
func (s *Server) handleOrderHistory(w http.ResponseWriter, r *http.Request) {
	o, scopes, ok := s.orderFor(w, r, r.PathValue("id"), false)
	if !ok {
		return
	}
	events, more, err := s.store.ListOrderEvents(o.ID, r.URL.Query().Get("before"), historyPage)
	if errors.Is(err, store.ErrEventNotFound) {
		writeJSONError(w, http.StatusBadRequest, "before must name one of this purchase order's events")
		return
	}
	if err != nil {
		writeServerError(w, err, "list the purchase order's history")
		return
	}
	track, prices := tracksVendor(r.Context(), scopes), seesCosts(r.Context(), scopes)
	for i := range events {
		events[i] = redactEvent(events[i], track)
		if !prices {
			events[i] = withoutOrderPrices(events[i])
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events, "more": more})
}

// withoutOrderPrices leaves out of an order's event its changes to what
// only purchases.read sees.
func withoutOrderPrices(e models.Event) models.Event {
	if len(e.Payload.Changes) == 0 {
		return e
	}
	kept := make([]models.EventChange, 0, len(e.Payload.Changes))
	for _, c := range e.Payload.Changes {
		if !store.OrderChanges[c.What] {
			kept = append(kept, c)
		}
	}
	e.Payload.Changes = kept
	return e
}

// OrderSuggestion is a draft order to a vendor, of what is running low.
type OrderSuggestion struct {
	VendorID   string          `json:"vendor_id"`
	VendorName string          `json:"vendor_name"`
	Lines      []SuggestedLine `json:"lines"`
}

// SuggestedLine is something running low, with its preferred catalog
// entry and the packs that bring it back to its threshold.
type SuggestedLine struct {
	CategoryID     string   `json:"category_id,omitempty"`
	AssetID        string   `json:"asset_id,omitempty"`
	Name           string   `json:"name"`
	Level          int      `json:"level"`
	Threshold      *int     `json:"threshold"`
	Unit           string   `json:"unit,omitempty"`
	CatalogEntryID string   `json:"catalog_entry_id,omitempty"`
	PartNumber     string   `json:"part_number,omitempty"`
	Description    string   `json:"description,omitempty"`
	PackSize       *int     `json:"pack_size,omitempty"`
	PackUnit       string   `json:"pack_unit,omitempty"`
	UnitPrice      string   `json:"unit_price,omitempty"`
	Currency       string   `json:"currency,omitempty"`
	Quantity       int      `json:"quantity"`
	Order          *OnOrder `json:"order,omitempty"`
}

// OnOrder is an open order something is already on.
type OnOrder struct {
	ID        string `json:"id"`
	Reference string `json:"reference"`
	State     string `json:"state"`
}
