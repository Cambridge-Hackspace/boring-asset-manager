package api

import (
	"context"
	"net/http"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Purchases (docs/data-model.md#purchases): where an asset came from. Those
// who read an asset see its vendor, when they may see the vendor, and its
// dates; its price, currency, order number and invoice need purchases.read
// for the asset too, and so does the invoice's attachment (attachments.go).
// Changing them follows assets.write, but those without
// purchases.read leave what they cannot see as it is, and a vendor they
// cannot see stays unless they name another.

// PurchaseRequest is an asset's purchase details as created or replaced:
// every one of them, so an absent one is emptied.
type PurchaseRequest struct {
	VendorID     string `json:"vendor_id"`
	OrderDate    string `json:"order_date"`
	ReceivedDate string `json:"received_date"`
	WarrantyEnd  string `json:"warranty_end"`
	Price        string `json:"price"`
	Currency     string `json:"currency"`
	OrderNumber  string `json:"order_number"`
	InvoiceID    string `json:"invoice_attachment_id"`
}

// purchaseInput is what a request's purchase details replace an asset's
// with, keeping what the request may not see as had has it: had is the
// asset as stored, with scopes, or nil for one being created in scopes. A
// nil req changes nothing. When it fails, it answers the request.
func (s *Server) purchaseInput(w http.ResponseWriter, r *http.Request, req *PurchaseRequest, had *models.Purchase, scopes []string) (*store.PurchaseInput, bool) {
	if req == nil {
		return nil, true
	}
	in := store.PurchaseInput{VendorID: req.VendorID, OrderDate: req.OrderDate, ReceivedDate: req.ReceivedDate, WarrantyEnd: req.WarrantyEnd,
		Price: req.Price, Currency: req.Currency, OrderNumber: req.OrderNumber, InvoiceID: req.InvoiceID}
	var kept models.Purchase
	if had != nil {
		kept = *had
	}
	if !Allows(r.Context(), rbac.PermPurchasesRead, scopes) {
		in.Price, in.Currency, in.OrderNumber, in.InvoiceID = kept.Price, kept.Currency, kept.OrderNumber, kept.InvoiceID
	}
	if in.VendorID == "" && kept.VendorID == "" {
		return &in, true
	}
	seen, _, err := s.visibleVendors(r.Context())
	if err != nil {
		writeServerError(w, err, "read the vendors")
		return nil, false
	}
	if _, ok := seen[in.VendorID]; in.VendorID != "" && !ok && in.VendorID != kept.VendorID {
		writeJSONError(w, http.StatusBadRequest, "vendor not found: "+in.VendorID)
		return nil, false
	}
	if _, ok := seen[kept.VendorID]; in.VendorID == "" && kept.VendorID != "" && !ok {
		in.VendorID = kept.VendorID
	}
	return &in, true
}

// redactPurchases leaves out of each asset's purchase details what the
// request may not see: the price, currency, order number and invoice
// without purchases.read for it, the invoice from its photos too, and a
// vendor it may not see. A failure to read the vendors hides every vendor.
func (s *Server) redactPurchases(ctx context.Context, assets []models.AssetWithDetails) {
	var seen map[string]models.Vendor
	for i := range assets {
		p := &assets[i].Purchase
		if !Allows(ctx, rbac.PermPurchasesRead, assets[i].Scopes) {
			if p.InvoiceID != "" {
				assets[i].Photos = withoutPhoto(assets[i].Photos, p.InvoiceID)
			}
			p.HidePrices()
		}
		if p.VendorID == "" {
			continue
		}
		if seen == nil {
			var err error
			if seen, _, err = s.visibleVendors(ctx); err != nil {
				seen = map[string]models.Vendor{}
			}
		}
		if _, ok := seen[p.VendorID]; !ok {
			p.HideVendor()
		}
	}
}

// withoutPhoto leaves out of photos the one named id.
func withoutPhoto(photos []models.AssetPhoto, id string) []models.AssetPhoto {
	out := make([]models.AssetPhoto, 0, len(photos))
	for _, p := range photos {
		if p.ID != id {
			out = append(out, p)
		}
	}
	return out
}

// redactPriceChanges leaves out of an asset's event the changes to what
// only purchases.read sees, unless prices is set.
func redactPriceChanges(e models.Event, prices bool) models.Event {
	if prices || len(e.Payload.Changes) == 0 {
		return e
	}
	kept := make([]models.EventChange, 0, len(e.Payload.Changes))
	for _, c := range e.Payload.Changes {
		if !store.PriceChanges[c.What] {
			kept = append(kept, c)
		}
	}
	e.Payload.Changes = kept
	return e
}
