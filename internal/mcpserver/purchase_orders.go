package mcpserver

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Purchase orders (docs/purchase-orders.md): orders to vendors, placed,
// received line by line, and sent as PDFs. Their files are attachments,
// which the attachment tools reach with purchase_order_id.

type listOrdersIn struct {
	VendorID   string `json:"vendor_id,omitempty" jsonschema:"only the orders to this vendor"`
	State      string `json:"state,omitempty" jsonschema:"only the orders in this state: draft, ordered, partly_received, received or canceled; or open, for those still to come"`
	CategoryID string `json:"category_id,omitempty" jsonschema:"only the orders with a line for this category"`
	Asset      string `json:"asset,omitempty" jsonschema:"only the orders with a line for this asset, by ID or code"`
	Query      string `json:"query,omitempty" jsonschema:"words every order listed has in its number, the vendor's reference, the vendor's name or its lines"`
}

// orderLineIn is a line of an order.
type orderLineIn struct {
	ID             string  `json:"id,omitempty" jsonschema:"one of the order's lines, to keep it and what has come of it"`
	CatalogEntryID string  `json:"catalog_entry_id,omitempty" jsonschema:"the vendor's catalog entry it is drawn from; it takes the entry's category or asset, text, pack and price"`
	CategoryID     string  `json:"category_id,omitempty" jsonschema:"for a free line, the category receiving it makes assets in"`
	AssetID        string  `json:"asset_id,omitempty" jsonschema:"for a free line, the asset counted by quantity receiving it restocks"`
	PartNumber     string  `json:"part_number,omitempty"`
	Description    string  `json:"description,omitempty"`
	PackSize       *int    `json:"pack_size,omitempty" jsonschema:"how many a pack holds; for an asset's line, how much one pack restocks it by"`
	PackUnit       string  `json:"pack_unit,omitempty"`
	Quantity       int     `json:"quantity" jsonschema:"how many packs, 1 to 1000000"`
	UnitPrice      *string `json:"unit_price,omitempty" jsonschema:"a pack's price, a decimal in the order's currency; left out, a line keeps its price and a new catalog line takes the entry's"`
}

type orderIn struct {
	ID              string         `json:"id,omitempty" jsonschema:"the order to change; empty to make a draft"`
	VendorID        *string        `json:"vendor_id,omitempty" jsonschema:"the vendor it is to; fixed once it is placed"`
	Currency        *string        `json:"currency,omitempty" jsonschema:"the ISO 4217 currency of its prices, such as GBP; fixed once it is placed"`
	VendorReference *string        `json:"vendor_reference,omitempty" jsonschema:"the vendor's order or confirmation number"`
	OrderedOn       *string        `json:"ordered_on,omitempty" jsonschema:"when it was placed, YYYY-MM-DD"`
	ExpectedOn      *string        `json:"expected_on,omitempty" jsonschema:"when it is expected, YYYY-MM-DD"`
	DeliverTo       *string        `json:"deliver_to,omitempty" jsonschema:"where it is delivered; a new order takes the last order's"`
	Notes           *string        `json:"notes,omitempty" jsonschema:"notes, in Markdown, printed on the order"`
	Lines           *[]orderLineIn `json:"lines,omitempty" jsonschema:"the whole list of its lines, in order"`
}

func (in orderIn) changes() map[string]any {
	out := given(map[string]any{
		"vendor_id": in.VendorID, "currency": in.Currency, "vendor_reference": in.VendorReference, "ordered_on": in.OrderedOn,
		"expected_on": in.ExpectedOn, "deliver_to": in.DeliverTo, "notes": in.Notes,
	})
	if in.Lines != nil {
		out["lines"] = *in.Lines
	}
	return out
}

type placeOrderIn struct {
	ID        string `json:"id"`
	OrderedOn string `json:"ordered_on,omitempty" jsonschema:"when it was placed, YYYY-MM-DD; today when not given"`
}

type receiptLineIn struct {
	LineID      string            `json:"line_id"`
	Quantity    int               `json:"quantity" jsonschema:"packs come, at most what is outstanding"`
	Restock     *int              `json:"restock,omitempty" jsonschema:"for an asset's line: how much it restocks; packs times the pack's size when not given"`
	Assets      *int              `json:"assets,omitempty" jsonschema:"for a category's line: how many assets to make; one per pack when not given, 0 for none"`
	Like        string            `json:"like,omitempty" jsonschema:"for a category's line: an asset, by ID, to make them like, as duplicating it does, codes continuing its series"`
	Name        string            `json:"name,omitempty" jsonschema:"the assets' name; the line's description when not given"`
	Fields      map[string]string `json:"fields,omitempty" jsonschema:"the assets' field values, by field key"`
	Each        *int              `json:"each,omitempty" jsonschema:"in a category that weighs, how much stock each asset holds, in its unit"`
	WarrantyEnd string            `json:"warranty_end,omitempty" jsonschema:"when the assets' warranty ends, YYYY-MM-DD"`
}

type receiveOrderIn struct {
	ID          string          `json:"id"`
	ReceivedOn  string          `json:"received_on,omitempty" jsonschema:"when it came, YYYY-MM-DD; today when not given"`
	Lines       []receiptLineIn `json:"lines"`
	QueueLabels bool            `json:"queue_labels,omitempty" jsonschema:"add the assets made to the print queue, with their categories' label templates"`
}

type orderHistoryIn struct {
	ID     string `json:"id"`
	Before string `json:"before,omitempty" jsonschema:"the last event of the previous page, for older ones"`
}

type orderNumberingIn struct {
	Prefix  string `json:"prefix" jsonschema:"what comes before the number, such as PO-; up to 16 letters, digits, hyphens and underscores, or empty"`
	Padding int    `json:"padding" jsonschema:"at least how many digits, padded with zeros, 0 to 9; 0 pads none"`
}

func referenceOf(out map[string]any) string {
	if o, ok := out["order"].(map[string]any); ok {
		if r, ok := o["reference"].(string); ok {
			return r
		}
	}
	return "the order"
}

func orderTools() []tool {
	manage := []string{rbac.PermVendorsManage}
	read := []string{rbac.PermAssetsRead}
	return []tool{
		define(&mcp.Tool{Name: "list_purchase_orders", Description: "The purchase orders the caller may see, newest first, with their lines and totals where the caller may see prices; " +
			"optionally only those to a vendor, in a state, with a line for a category or an asset, or matching a search.",
			Annotations: readOnly("List purchase orders")}, []string{"GET /api/v1/purchase-orders"}, read,
			func(ctx context.Context, e *env, in listOrdersIn) (reply[map[string]any], error) {
				q := url.Values{}
				for k, v := range map[string]string{"vendor_id": in.VendorID, "state": in.State, "category_id": in.CategoryID, "q": in.Query} {
					if v != "" {
						q.Set(k, v)
					}
				}
				if in.Asset != "" {
					id, err := e.assetID(ctx, in.Asset)
					if err != nil {
						return reply[map[string]any]{}, err
					}
					q.Set("asset_id", id)
				}
				path := "/api/v1/purchase-orders"
				if len(q) > 0 {
					path += "?" + q.Encode()
				}
				out := map[string]any{}
				if err := e.call(ctx, http.MethodGet, path, nil, &out); err != nil {
					return reply[map[string]any]{}, err
				}
				return say(out, "%d purchase orders.", count(out, "orders"))
			}),
		simple(&mcp.Tool{Name: "get_purchase_order", Description: "One purchase order: its vendor, state, dates, where it is delivered, notes, its lines with what each is for, " +
			"how many packs were ordered and have come, prices and totals where the caller may see them, and whether the caller may change it.",
			Annotations: readOnly("Get a purchase order")}, "GET /api/v1/purchase-orders/{id}", read,
			func(in idIn) (request, error) {
				return request{path: pathOf("/api/v1/purchase-orders/{id}", in.ID)}, need(in.ID, "id")
			},
			func(_ idIn, out map[string]any) string { return referenceOf(out) + "." }),
		define(&mcp.Tool{Name: "save_purchase_order", Description: "Make a draft purchase order to a vendor, or change one: only what is given changes, and lines, when given, replace its lines. " +
			"A placed order keeps its vendor and currency, and what has come of its lines; a received or canceled one changes only its reference and notes. " +
			"Needs vendors.manage in the vendor's scopes.",
			Annotations: changes("Save a purchase order", true)}, []string{"POST /api/v1/purchase-orders", "PUT /api/v1/purchase-orders/{id}"}, manage,
			func(ctx context.Context, e *env, in orderIn) (reply[map[string]any], error) {
				out := map[string]any{}
				if in.ID == "" {
					if in.VendorID == nil {
						return reply[map[string]any]{}, fmt.Errorf("a new order needs a vendor_id")
					}
					if err := e.call(ctx, http.MethodPost, "/api/v1/purchase-orders", in.changes(), &out); err != nil {
						return reply[map[string]any]{}, err
					}
					return say(out, "Made %s.", referenceOf(out))
				}
				var got struct {
					Order models.PurchaseOrder `json:"order"`
				}
				if err := e.call(ctx, http.MethodGet, pathOf("/api/v1/purchase-orders/{id}", in.ID), nil, &got); err != nil {
					return reply[map[string]any]{}, err
				}
				current := got.Order
				body, err := merged(current, in.changes())
				if err != nil {
					return reply[map[string]any]{}, err
				}
				if in.Lines == nil {
					// Lines left out are kept, prices and all.
					delete(body, "lines")
				}
				if err := e.call(ctx, http.MethodPut, pathOf("/api/v1/purchase-orders/{id}", in.ID), body, &out); err != nil {
					return reply[map[string]any]{}, err
				}
				return say(out, "Saved %s.", referenceOf(out))
			}),
		simple(&mcp.Tool{Name: "delete_purchase_order", Description: "Delete a purchase order, in any state, with its files; what it received stays received.",
			Annotations: destroys("Delete a purchase order", true)}, "DELETE /api/v1/purchase-orders/{id}", manage,
			func(in idIn) (request, error) {
				return request{path: pathOf("/api/v1/purchase-orders/{id}", in.ID)}, need(in.ID, "id")
			},
			func(idIn, map[string]any) string { return "Deleted the purchase order." }),
		simple(&mcp.Tool{Name: "place_purchase_order", Description: "Mark a draft purchase order as placed with its vendor. It needs a line.",
			Annotations: changes("Place a purchase order", false)}, "POST /api/v1/purchase-orders/{id}/place", manage,
			func(in placeOrderIn) (request, error) {
				return request{path: pathOf("/api/v1/purchase-orders/{id}/place", in.ID), body: map[string]string{"ordered_on": in.OrderedOn}}, need(in.ID, "id")
			},
			func(_ placeOrderIn, out map[string]any) string { return "Placed " + referenceOf(out) + "." }),
		simple(&mcp.Tool{Name: "cancel_purchase_order", Description: "Cancel a purchase order placed or partly received: what came stays, and the rest is not expected. A draft is deleted instead.",
			Annotations: changes("Cancel a purchase order", false)}, "POST /api/v1/purchase-orders/{id}/cancel", manage,
			func(in idIn) (request, error) {
				return request{path: pathOf("/api/v1/purchase-orders/{id}/cancel", in.ID)}, need(in.ID, "id")
			},
			func(_ idIn, out map[string]any) string { return "Canceled " + referenceOf(out) + "." }),
		define(&mcp.Tool{Name: "receive_purchase_order", Description: "Receive a delivery against a purchase order, every line given or none: an asset's line restocks it, " +
			"and a category's line makes its assets with the order's vendor, dates, number and price. Needs stock.record for an asset restocked, " +
			"and assets.write where the assets made go.",
			Annotations: changes("Receive a purchase order", false)}, []string{"POST /api/v1/purchase-orders/{id}/receive", "POST /api/v1/spool"}, read,
			func(ctx context.Context, e *env, in receiveOrderIn) (reply[map[string]any], error) {
				if err := need(in.ID, "id"); err != nil {
					return reply[map[string]any]{}, err
				}
				out := map[string]any{}
				body := map[string]any{"received_on": in.ReceivedOn, "lines": in.Lines}
				if err := e.call(ctx, http.MethodPost, pathOf("/api/v1/purchase-orders/{id}/receive", in.ID), body, &out); err != nil {
					return reply[map[string]any]{}, err
				}
				made, _ := out["created_asset_ids"].([]any)
				text := fmt.Sprintf("Received against %s: %d assets made, %d restocked.", referenceOf(out), len(made), count(out, "restocked"))
				if in.QueueLabels && len(made) > 0 {
					queued := map[string]any{}
					if err := e.call(ctx, http.MethodPost, "/api/v1/spool", map[string]any{"asset_ids": made}, &queued); err != nil {
						text += " Their labels could not be queued: " + err.Error()
					} else {
						out["queued"] = queued
						text += fmt.Sprintf(" %d labels queued.", count(queued, "added"))
					}
				}
				return say(out, "%s", text)
			}),
		simple(&mcp.Tool{Name: "suggest_purchase_orders", Description: "Draft orders of what is running low: by vendor, each thing's preferred catalog entry with enough packs to bring it back to its threshold, " +
			"with what is already on an open order, and what has no preferred entry, apart. Make them with save_purchase_order.",
			Annotations: readOnly("Suggest purchase orders")}, "GET /api/v1/purchase-orders/suggestions", manage,
			func(none) (request, error) { return request{path: "/api/v1/purchase-orders/suggestions"}, nil },
			func(_ none, out map[string]any) string {
				return fmt.Sprintf("%d vendors to order from; %d things already on order; %d with no preferred entry.",
					count(out, "vendors"), count(out, "on_order"), count(out, "without_entry"))
			}),
		simple(&mcp.Tool{Name: "purchase_order_history", Description: "A purchase order's history, newest first, a page at a time: when it was made, each change, " +
			"when it was placed, each delivery received, and when it was canceled.",
			Annotations: readOnly("Purchase order history")}, "GET /api/v1/purchase-orders/{id}/history", read,
			func(in orderHistoryIn) (request, error) {
				q := url.Values{}
				if in.Before != "" {
					q.Set("before", in.Before)
				}
				return request{path: pathOf("/api/v1/purchase-orders/{id}/history", in.ID), query: q}, need(in.ID, "id")
			},
			func(_ orderHistoryIn, out map[string]any) string {
				more := ""
				if m, _ := out["more"].(bool); m {
					more = " Older ones remain."
				}
				return fmt.Sprintf("%d events.%s", count(out, "events"), more)
			}),
		define(&mcp.Tool{Name: "purchase_order_pdf", Description: "A purchase order as a PDF to send to its vendor, as people download it: to whom, where to deliver, " +
			"its lines and total.", Annotations: readOnly("Purchase order as PDF")}, []string{"GET /api/v1/purchase-orders/{id}/export.pdf"}, read,
			func(ctx context.Context, e *env, in struct {
				ID string `json:"id"`
				pageIn
			}) (reply[map[string]any], error) {
				if err := need(in.ID, "id"); err != nil {
					return reply[map[string]any]{}, err
				}
				res := e.api(ctx, http.MethodGet, pathOf("/api/v1/purchase-orders/{id}/export.pdf", in.ID)+in.query(url.Values{}), nil, "")
				return pdfReply(res, "bam://purchase-orders/"+in.ID+".pdf")
			}),
		simple(&mcp.Tool{Name: "get_order_numbering", Description: "How purchase orders' numbers are shown: a prefix, and at least how many digits.",
			Annotations: readOnly("Order numbering")}, "GET /api/v1/settings/purchase-orders", read,
			func(none) (request, error) { return request{path: "/api/v1/settings/purchase-orders"}, nil },
			func(_ none, out map[string]any) string {
				return fmt.Sprintf("Prefix %q, padded to %v digits.", out["prefix"], out["padding"])
			}),
		simple(&mcp.Tool{Name: "set_order_numbering", Description: "Set how purchase orders' numbers are shown from now on. Assets received already keep the order number they were given.",
			Annotations: changes("Set order numbering", true)}, "PUT /api/v1/settings/purchase-orders", []string{rbac.PermSettingsManage},
			func(in orderNumberingIn) (request, error) {
				return request{path: "/api/v1/settings/purchase-orders", body: in}, nil
			},
			func(_ orderNumberingIn, out map[string]any) string {
				return fmt.Sprintf("Saved: prefix %q, padded to %v digits.", out["prefix"], out["padding"])
			}),
	}
}
