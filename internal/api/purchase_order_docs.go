package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/render"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Starting orders from what is running low, a purchase order as a PDF to
// send, and how orders' numbers are shown (docs/purchase-orders.md).

// handleOrderSuggestions suggests draft orders of what is running low:
// GET /api/v1/purchase-orders/suggestions. Each thing the requester sees
// running low with a preferred catalog entry is a line, of enough packs to
// bring it back to its threshold, grouped by the entry's vendor, among
// those the requester may make orders with. Things already on an open
// order, and those with no preferred entry, are listed apart.
func (s *Server) handleOrderSuggestions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cands, err := s.store.OrderCandidates()
	if err != nil {
		writeServerError(w, err, "read what is running low")
		return
	}
	seen, scopes, err := s.visibleVendors(ctx)
	if err != nil {
		writeServerError(w, err, "read the vendors")
		return
	}
	orders, err := s.store.ListPurchaseOrders()
	if err != nil {
		writeServerError(w, err, "list purchase orders")
		return
	}
	onOrder := map[string]*OnOrder{}
	// Newest first, so the oldest open order is kept.
	for _, o := range orders {
		if _, ok := seen[o.VendorID]; !ok || !models.OrderOpen(o.State) {
			continue
		}
		for _, l := range o.Lines {
			on := &OnOrder{ID: o.ID, Reference: o.Reference, State: o.State}
			if l.CategoryID != "" {
				onOrder[l.CategoryID] = on
			}
			if l.AssetID != "" {
				onOrder[l.AssetID] = on
			}
		}
	}
	var ids []string
	for _, c := range cands {
		if c.AssetID != "" {
			ids = append(ids, c.AssetID)
		}
	}
	assetScopes, err := s.store.AssetScopes(ids)
	if err != nil {
		writeServerError(w, err, "read the assets' scopes")
		return
	}
	byVendor := map[string]*OrderSuggestion{}
	var vendorOrder []string
	already, without := []SuggestedLine{}, []SuggestedLine{}
	for _, c := range cands {
		if c.AssetID != "" {
			if sc, ok := assetScopes[c.AssetID]; !ok || !Allows(ctx, rbac.PermAssetsRead, sc) {
				continue
			}
		}
		line := SuggestedLine{CategoryID: c.CategoryID, AssetID: c.AssetID, Name: c.Name, Level: c.Level, Threshold: c.Threshold, Unit: c.Unit}
		if on := onOrder[c.CategoryID+c.AssetID]; on != nil {
			line.Order = on
			already = append(already, line)
			continue
		}
		e := c.Preferred
		if e == nil {
			without = append(without, line)
			continue
		}
		if _, ok := seen[e.VendorID]; !ok || !Allows(ctx, rbac.PermVendorsManage, scopes[e.VendorID]) {
			without = append(without, line)
			continue
		}
		line.CatalogEntryID, line.PartNumber, line.Description, line.PackSize, line.PackUnit = e.ID, e.PartNumber, e.Description, e.PackSize, e.PackUnit
		if seesCosts(ctx, scopes[e.VendorID]) {
			line.UnitPrice, line.Currency = e.Price, e.Currency
		}
		line.Quantity = store.SuggestedPacks(c)
		g := byVendor[e.VendorID]
		if g == nil {
			g = &OrderSuggestion{VendorID: e.VendorID, VendorName: e.VendorName, Lines: []SuggestedLine{}}
			byVendor[e.VendorID] = g
			vendorOrder = append(vendorOrder, e.VendorID)
		}
		g.Lines = append(g.Lines, line)
	}
	vendors := []OrderSuggestion{}
	for _, id := range vendorOrder {
		vendors = append(vendors, *byVendor[id])
	}
	writeJSON(w, http.StatusOK, map[string]any{"vendors": vendors, "on_order": already, "without_entry": without})
}

// handleOrderPDF makes an order a PDF to send to its vendor: GET
// /api/v1/purchase-orders/{id}/export.pdf, on the instance's page size
// unless page_size and orientation say otherwise. Its prices are left out
// for a requester who may not see them.
func (s *Server) handleOrderPDF(w http.ResponseWriter, r *http.Request) {
	o, scopes, ok := s.orderFor(w, r, r.PathValue("id"), false)
	if !ok {
		return
	}
	shown := s.shownOrder(r.Context(), o, scopes)
	title := "Purchase order " + o.Reference
	if o.State == models.OrderDraft {
		title = "Draft purchase order " + o.Reference
	}
	req, ok := s.documentOptions(w, r, title)
	if !ok {
		return
	}
	v, err := s.store.GetVendor(o.VendorID)
	if err != nil {
		writeServerError(w, err, "read the vendor")
		return
	}
	d, err := render.NewDocument(req.opts)
	if err != nil {
		writeServerError(w, err, "start the PDF")
		return
	}
	drawOrder(d, &shown, v, req.opts.Instance)
	writeDocument(w, d, nameStem(o.Reference, "purchase-order"))
}

// oneLine joins text's lines with commas, as a fact shows an address.
func oneLine(text string) string {
	var parts []string
	for _, l := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			parts = append(parts, l)
		}
	}
	return strings.Join(parts, ", ")
}

// drawOrder draws an order to send: to whom, where to deliver it, the
// order's details, its lines with their total, and its notes; never what
// it is for here, what came, or its state.
func drawOrder(d *render.Document, o *models.PurchaseOrder, v *models.Vendor, instance string) {
	d.Heading(o.Reference)
	facts := []render.Fact{}
	add := func(label, value string) {
		if value != "" {
			facts = append(facts, render.Fact{Label: label, Value: value})
		}
	}
	add("To", v.Name)
	add("Address", oneLine(v.Address))
	add("Account number", v.AccountNumber)
	if len(v.Contacts) > 0 {
		c := v.Contacts[0]
		var how []string
		for _, s := range []string{c.Name, c.Email, c.Phone} {
			if s != "" {
				how = append(how, s)
			}
		}
		add("Contact", strings.Join(how, ", "))
	}
	add("Deliver to", strings.TrimSpace(instance+", "+oneLine(o.DeliverTo)))
	if o.DeliverTo == "" {
		facts[len(facts)-1].Value = instance
	}
	add("Order number", o.Reference)
	add("Ordered", o.OrderedOn)
	add("Expected", o.ExpectedOn)
	add("Your reference", o.VendorReference)
	add("Currency", o.Currency)
	d.Facts(facts)

	rows := make([]render.TableRow, 0, len(o.Lines)+1)
	for i, l := range o.Lines {
		pack := l.PackUnit
		if l.PackSize != nil {
			pack = strings.TrimSpace(strconv.Itoa(*l.PackSize) + " " + l.PackUnit)
		}
		rows = append(rows, render.TableRow{Cells: []string{strconv.Itoa(i + 1), l.PartNumber, l.Description, pack, strconv.Itoa(l.Quantity), l.UnitPrice, l.Total}})
	}
	if o.Total != "" {
		rows = append(rows, render.TableRow{Cells: []string{"", "", "Total " + o.Currency, "", "", "", o.Total}})
	}
	d.Table(render.Table{Columns: []string{"#", "Part number", "Description", "Pack", "Quantity", "Unit price", "Total"}, Groups: []render.TableGroup{{Rows: rows}}})
	if notes := strings.TrimSpace(o.Notes); notes != "" {
		d.Heading("Notes")
		for _, p := range strings.Split(strings.ReplaceAll(notes, "\r\n", "\n"), "\n\n") {
			d.Paragraph(strings.TrimSpace(p), render.Black)
		}
	}
}

// handleOrderNumbering reads how orders' numbers are shown: GET
// /api/v1/settings/purchase-orders.
func (s *Server) handleOrderNumbering(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.OrderNumbering()
	if err != nil {
		writeServerError(w, err, "read the order numbering")
		return
	}
	writeJSON(w, http.StatusOK, n)
}

// handleSetOrderNumbering sets it: PUT /api/v1/settings/purchase-orders,
// with {"prefix", "padding"}. Orders are shown with it from then on; what
// was written with the old, such as assets' order numbers, stays.
func (s *Server) handleSetOrderNumbering(w http.ResponseWriter, r *http.Request) {
	var req store.OrderNumbering
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Prefix = strings.TrimSpace(req.Prefix)
	if err := req.Check(); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.SetOrderNumbering(req); err != nil {
		writeServerError(w, err, "save the order numbering")
		return
	}
	writeJSON(w, http.StatusOK, req)
}
