package assetcsv

import (
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/money"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/textcheck"
)

// purchaseColumns are the columns of where an asset came from, in the order
// Export writes them.
var purchaseColumns = []string{ColumnVendor, ColumnOrderDate, ColumnReceivedDate, ColumnPrice, ColumnCurrency, ColumnOrderNumber, ColumnWarrantyEnd}

func isPurchaseColumn(t string) bool {
	for _, c := range purchaseColumns {
		if t == c {
			return true
		}
	}
	return false
}

// purchase plans a row's purchase cells: nil when they are all blank, and
// otherwise the details they give, each checked, with a blank one kept as
// the asset has it (store.ImportRow). A vendor is named by name or ID; a
// price is in the row's currency, or the asset's.
func (p *planner) purchase(line int, existing *models.AssetWithDetails, cell func(string) (string, string, bool)) *store.PurchaseInput {
	var (
		in    store.PurchaseInput
		given bool
	)
	cols := map[string]string{}
	for _, c := range []struct {
		column string
		into   *string
	}{
		{ColumnVendor, &in.VendorID}, {ColumnOrderDate, &in.OrderDate}, {ColumnReceivedDate, &in.ReceivedDate}, {ColumnPrice, &in.Price},
		{ColumnCurrency, &in.Currency}, {ColumnOrderNumber, &in.OrderNumber}, {ColumnWarrantyEnd, &in.WarrantyEnd},
	} {
		v, col, _ := cell(c.column)
		cols[c.column] = col
		if v != "" {
			*c.into, given = v, true
		}
	}
	if !given {
		return nil
	}
	var had models.Purchase
	if existing != nil {
		had = existing.Purchase
	}
	if in.VendorID != "" {
		in.VendorID = p.vendor(line, cols[ColumnVendor], in.VendorID)
	}
	for _, d := range []struct {
		column, what string
		value        *string
	}{
		{ColumnOrderDate, "the order date", &in.OrderDate}, {ColumnReceivedDate, "the received date", &in.ReceivedDate},
		{ColumnWarrantyEnd, "the warranty end", &in.WarrantyEnd},
	} {
		v, err := store.CheckDate(d.what, *d.value)
		if err != nil {
			p.plan.problem(line, cols[d.column], "%s must be a date written YYYY-MM-DD", d.what)
		}
		*d.value = v
	}
	currency, err := money.NormalizeCurrency(in.Currency)
	if err != nil {
		p.plan.problem(line, cols[ColumnCurrency], "%v", err)
	}
	in.Currency = currency
	if in.Price != "" {
		if currency == "" {
			currency = had.Currency
		}
		if currency == "" {
			p.plan.problem(line, cols[ColumnPrice], "a price needs its currency")
		} else if minor, err := money.Parse(in.Price, currency); err != nil {
			p.plan.problem(line, cols[ColumnPrice], "%v", err)
		} else {
			in.Price = money.Format(minor, currency)
		}
	}
	if prob := textcheck.Problem(in.OrderNumber, textcheck.MaxName); prob != "" {
		p.plan.problem(line, cols[ColumnOrderNumber], "the order number %s", prob)
	}
	return &in
}

// vendor finds the vendor a cell names, by ID or by name ignoring case,
// among those the importer may see, giving its ID; a problem when there is
// none, or more than one by that name.
func (p *planner) vendor(line int, col, ref string) string {
	var found []models.Vendor
	for _, v := range p.opts.Vendors {
		if v.ID == ref {
			return v.ID
		}
		if strings.EqualFold(strings.TrimSpace(v.Name), ref) {
			found = append(found, v)
		}
	}
	switch len(found) {
	case 0:
		p.plan.problem(line, col, "no vendor is %q", ref)
		return ""
	case 1:
		return found[0].ID
	}
	p.plan.problem(line, col, "%d vendors are named %q; give its ID instead", len(found), ref)
	return ""
}

// unpriced returns the scopes of an asset, joined, when a row gives its
// price, currency or order number and the importer may not see them there;
// "" otherwise.
func (p *planner) unpriced(id string, in *store.PurchaseInput) string {
	if p.opts.SeesPrices == nil || in == nil || in.Price == "" && in.Currency == "" && in.OrderNumber == "" {
		return ""
	}
	scopes, ok := p.after[id]
	if a, had := p.assets[id]; had && !ok {
		scopes = a.Scopes
	}
	if !p.opts.SeesPrices(scopes) {
		return strings.Join(scopes, ", ")
	}
	return ""
}

// samePurchase reports whether applying the details a row gives would
// leave an asset's as they are.
func samePurchase(a models.Purchase, in store.PurchaseInput) bool {
	for _, c := range [][2]string{
		{in.VendorID, a.VendorID}, {in.OrderDate, a.OrderDate}, {in.ReceivedDate, a.ReceivedDate}, {in.Price, a.Price},
		{in.Currency, a.Currency}, {in.OrderNumber, a.OrderNumber}, {in.WarrantyEnd, a.WarrantyEnd},
	} {
		if c[0] != "" && c[0] != c[1] {
			return false
		}
	}
	return true
}
