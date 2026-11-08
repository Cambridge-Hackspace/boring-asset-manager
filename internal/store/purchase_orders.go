package store

import (
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/money"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/textcheck"
	"github.com/google/uuid"
)

// Purchase orders (docs/purchase-orders.md): orders to a vendor, whose
// scopes decide who sees them; the API checks them. Each has lines, drawn
// from the vendor's catalog or written freely, with quantities of packs
// and prices, and is received line by line: restocking what is counted by
// quantity, and creating the assets of what is counted by items.

const (
	// MaxOrderLines is how many lines an order may have.
	MaxOrderLines = 500
	// MaxOrderQuantity is the most packs a line may order.
	MaxOrderQuantity = 1_000_000
	// MaxOrderNotes is the longest notes, in Markdown.
	MaxOrderNotes = 20000
	// MaxDeliverTo is the longest delivery address.
	MaxDeliverTo = 1000
	// MaxReceiptAssets is how many assets a receipt may make, all its
	// lines together.
	MaxReceiptAssets = 500
	// ordersLock serializes allocating orders' numbers.
	ordersLock = "purchase_orders"
)

var (
	ErrOrderNotFound = errors.New("purchase order not found")
	// ErrInvalidOrder is wrapped by every problem with an order's details
	// or a receipt.
	ErrInvalidOrder = errors.New("invalid purchase order")
	// ErrVendorHasOrders refuses deleting a vendor that purchase orders
	// are to, which would delete them too.
	ErrVendorHasOrders = errors.New("the vendor has purchase orders; delete them first")
)

// OrderStateError is a change an order's state does not allow, such as
// changing the vendor of one placed, or receiving more than is
// outstanding. Its message says why.
type OrderStateError struct{ Msg string }

func (e *OrderStateError) Error() string { return e.Msg }

func stateError(format string, args ...any) error {
	return &OrderStateError{Msg: fmt.Sprintf(format, args...)}
}

func invalidOrder(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidOrder, fmt.Sprintf(format, args...))
}

// Settings for how orders' numbers are shown
// (docs/purchase-orders.md#numbers): a prefix, and at least how many
// digits, padded with zeros. Only showing changes: an order's number is
// kept as a whole number.
const (
	OrderPrefixSetting  = "purchase_orders.number_prefix"
	OrderPaddingSetting = "purchase_orders.number_padding"
	// MaxOrderPrefix is the longest prefix, and MaxOrderPadding the most
	// digits a number is padded to.
	MaxOrderPrefix  = 16
	MaxOrderPadding = 9
)

// OrderNumbering is how orders' numbers are shown: Prefix, then the number
// padded with zeros to at least Padding digits; 0 pads none.
type OrderNumbering struct {
	Prefix  string `json:"prefix"`
	Padding int    `json:"padding"`
}

// DefaultOrderNumbering shows numbers as PO-0012.
var DefaultOrderNumbering = OrderNumbering{Prefix: "PO-", Padding: 4}

// ErrInvalidNumbering is a prefix or padding that cannot be kept.
var ErrInvalidNumbering = errors.New("invalid order numbering")

// Check reports whether the numbering can be kept: a prefix of at most
// MaxOrderPrefix letters, digits, hyphens and underscores, so that it is
// safe in a file's name and on a label, and may be empty; and padding from
// 0 to MaxOrderPadding.
func (n OrderNumbering) Check() error {
	if len(n.Prefix) > MaxOrderPrefix {
		return fmt.Errorf("%w: the prefix is at most %d characters", ErrInvalidNumbering, MaxOrderPrefix)
	}
	for _, r := range n.Prefix {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return fmt.Errorf("%w: the prefix may have only letters A to Z, digits, hyphens and underscores", ErrInvalidNumbering)
		}
	}
	if n.Padding < 0 || n.Padding > MaxOrderPadding {
		return fmt.Errorf("%w: the padding is from 0 to %d digits", ErrInvalidNumbering, MaxOrderPadding)
	}
	return nil
}

// Format shows an order's number: PO-0012, wider once it grows past the
// padding.
func (n OrderNumbering) Format(number int) string {
	return fmt.Sprintf("%s%0*d", n.Prefix, n.Padding, number)
}

// OrderReference shows an order's number as the default numbering does.
func OrderReference(number int) string {
	return DefaultOrderNumbering.Format(number)
}

// orderNumberingIn reads how orders' numbers are shown through q, the
// default where it is not set.
func orderNumberingIn(q vendorQuerier) (OrderNumbering, error) {
	n := DefaultOrderNumbering
	rows, err := q.Query("SELECT setting_key, value FROM settings WHERE setting_key IN (?, ?)", OrderPrefixSetting, OrderPaddingSetting)
	if err != nil {
		return n, fmt.Errorf("reading order numbering: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return n, err
		}
		switch key {
		case OrderPrefixSetting:
			n.Prefix = value
		case OrderPaddingSetting:
			if p, err := strconv.Atoi(value); err == nil {
				n.Padding = p
			}
		}
	}
	return n, rows.Err()
}

// OrderNumbering reads how orders' numbers are shown.
func (s *Store) OrderNumbering() (OrderNumbering, error) {
	return orderNumberingIn(storeQuerier{s})
}

// SetOrderNumbering sets how orders' numbers are shown, from now on. What
// was written with the old, such as the order numbers of assets received
// already, stays as it was written.
func (s *Store) SetOrderNumbering(n OrderNumbering) error {
	if err := n.Check(); err != nil {
		return err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if err := s.setSettingTx(tx, OrderPrefixSetting, n.Prefix); err != nil {
		return err
	}
	if err := s.setSettingTx(tx, OrderPaddingSetting, strconv.Itoa(n.Padding)); err != nil {
		return err
	}
	return tx.Commit()
}

// OrderInput is everything a caller sets on an order. Dates are
// YYYY-MM-DD. Lines replace the order's: a line given with the ID of one
// of its lines keeps it, and what it has received.
type OrderInput struct {
	VendorID        string
	Currency        string
	VendorReference string
	OrderedOn       string
	ExpectedOn      string
	DeliverTo       string
	Notes           string
	Lines           []OrderLineInput
}

// OrderLineInput is a line as given. A line drawn from a catalog entry
// names it, and takes its category or asset, and whatever text and pack
// the line leaves empty. Price is a decimal in the order's currency; nil
// keeps a line's price, and gives a new line drawn from an entry the
// entry's price when it is in the order's currency, and an empty one none.
type OrderLineInput struct {
	ID             string
	CatalogEntryID string
	CategoryID     string
	AssetID        string
	PartNumber     string
	Description    string
	PackSize       *int
	PackUnit       string
	Quantity       int
	Price          *string
}

// OrderInputOf is the input that keeps an order as it is.
func OrderInputOf(o models.PurchaseOrder) OrderInput {
	in := OrderInput{VendorID: o.VendorID, Currency: o.Currency, VendorReference: o.VendorReference, OrderedOn: o.OrderedOn,
		ExpectedOn: o.ExpectedOn, DeliverTo: o.DeliverTo, Notes: o.Notes, Lines: []OrderLineInput{}}
	for _, l := range o.Lines {
		in.Lines = append(in.Lines, OrderLineInput{ID: l.ID, CatalogEntryID: l.CatalogEntryID, CategoryID: l.CategoryID, AssetID: l.AssetID,
			PartNumber: l.PartNumber, Description: l.Description, PackSize: l.PackSize, PackUnit: l.PackUnit, Quantity: l.Quantity})
	}
	return in
}

// orderDate reads a date written YYYY-MM-DD, trimmed; empty stays empty.
func orderDate(what, s string) (string, error) {
	d, err := CheckDate(what, s)
	if err != nil {
		return "", invalidOrder("%s must be a date written YYYY-MM-DD", what)
	}
	return d, nil
}

// clean trims an order's details and checks what can be checked of them
// alone.
func (in *OrderInput) clean() error {
	in.VendorID = strings.TrimSpace(in.VendorID)
	in.VendorReference = strings.TrimSpace(in.VendorReference)
	in.DeliverTo = strings.TrimSpace(in.DeliverTo)
	if in.VendorID == "" {
		return invalidOrder("an order needs a vendor")
	}
	for _, c := range []struct {
		what, value string
		max         int
	}{
		{"the vendor's reference", in.VendorReference, textcheck.MaxName},
		{"where to deliver it", in.DeliverTo, MaxDeliverTo},
		{"the notes", in.Notes, MaxOrderNotes},
	} {
		if err := textcheck.Check(c.what, c.value, c.max); err != nil {
			return invalidOrder("%v", err)
		}
	}
	var err error
	for _, d := range []struct {
		what string
		into *string
	}{{"the date ordered", &in.OrderedOn}, {"the date expected", &in.ExpectedOn}} {
		if *d.into, err = orderDate(d.what, *d.into); err != nil {
			return err
		}
	}
	if in.Currency, err = money.NormalizeCurrency(in.Currency); err != nil {
		return invalidOrder("%v", err)
	}
	if len(in.Lines) > MaxOrderLines {
		return invalidOrder("an order can have at most %d lines", MaxOrderLines)
	}
	for i := range in.Lines {
		l := &in.Lines[i]
		l.ID, l.CatalogEntryID = strings.TrimSpace(l.ID), strings.TrimSpace(l.CatalogEntryID)
		l.CategoryID, l.AssetID = strings.TrimSpace(l.CategoryID), strings.TrimSpace(l.AssetID)
		l.PartNumber, l.Description, l.PackUnit = strings.TrimSpace(l.PartNumber), strings.TrimSpace(l.Description), strings.TrimSpace(l.PackUnit)
		n := i + 1
		if l.CategoryID != "" && l.AssetID != "" {
			return invalidOrder("line %d is for a category or an asset, not both", n)
		}
		if l.Quantity < 1 || l.Quantity > MaxOrderQuantity {
			return invalidOrder("line %d: the quantity must be from 1 to %d packs", n, MaxOrderQuantity)
		}
		if l.PackSize != nil && (*l.PackSize < 1 || *l.PackSize > MaxPackSize) {
			return invalidOrder("line %d: a pack holds from 1 to %d", n, MaxPackSize)
		}
		for _, c := range []struct {
			what, value string
			max         int
		}{
			{"the part number", l.PartNumber, textcheck.MaxName},
			{"the description", l.Description, MaxCatalogDescription},
			{"the pack unit", l.PackUnit, MaxPackUnit},
		} {
			if err := textcheck.Check(c.what, c.value, c.max); err != nil {
				return invalidOrder("line %d: %v", n, err)
			}
		}
	}
	return nil
}

// orderLineRow is a line checked and ready to keep.
type orderLineRow struct {
	id                                  string
	categoryEntry, assetEntry           *string
	category, asset                     *string
	partNumber, description, packUnit   string
	packSize                            *int
	quantity, received                  int
	priceMinor                          *int64
	catalogEntryID, categoryID, assetID string
}

const orderSelect = `SELECT o.id, o.number, o.vendor_id, v.name, o.state, o.currency, o.vendor_reference, o.ordered_on, o.expected_on,
	o.received_on, o.deliver_to, o.notes, o.created_at, o.updated_at
	FROM purchase_orders o JOIN vendors v ON v.id = o.vendor_id`

// orderLineSelect reads lines, with the name of what each is for.
const orderLineSelect = `SELECT l.purchase_order_id, l.id, l.category_entry_id, l.asset_entry_id, l.category_id, l.asset_id,
	COALESCE(k.name, a.name, ''), l.part_number, l.description, l.pack_size, l.pack_unit, l.quantity, l.quantity_received, l.unit_price_minor
	FROM purchase_order_lines l LEFT JOIN categories k ON k.id = l.category_id LEFT JOIN assets a ON a.id = l.asset_id`

// readOrders reads orders, with their lines, newest first, their numbers
// shown as the instance's numbering says. where narrows them, with args,
// naming the orders table o and vendors v.
func readOrders(q vendorQuerier, where string, args ...any) ([]models.PurchaseOrder, error) {
	numbering, err := orderNumberingIn(q)
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(orderSelect+where+" ORDER BY o.number DESC", args...)
	if err != nil {
		return nil, fmt.Errorf("reading purchase orders: %w", err)
	}
	out := []models.PurchaseOrder{}
	byID := map[string]int{}
	totals := map[string]int64{}
	for rows.Next() {
		var (
			o                       models.PurchaseOrder
			ordered, expected, recv sql.NullString
		)
		if err := rows.Scan(&o.ID, &o.Number, &o.VendorID, &o.VendorName, &o.State, &o.Currency, &o.VendorReference, &ordered, &expected,
			&recv, &o.DeliverTo, &o.Notes, &o.CreatedAt, &o.UpdatedAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scanning purchase order: %w", err)
		}
		o.Reference = numbering.Format(o.Number)
		o.OrderedOn, o.ExpectedOn, o.ReceivedOn = ordered.String, expected.String, recv.String
		o.Lines = []models.PurchaseOrderLine{}
		byID[o.ID] = len(out)
		out = append(out, o)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return out, nil
	}
	within := " WHERE l.purchase_order_id IN (SELECT o.id FROM purchase_orders o JOIN vendors v ON v.id = o.vendor_id" + where + ")"
	rows, err = q.Query(orderLineSelect+within+" ORDER BY l.purchase_order_id, l.position", args...)
	if err != nil {
		return nil, fmt.Errorf("reading purchase order lines: %w", err)
	}
	priced := map[string]bool{}
	for rows.Next() {
		var (
			orderID                                          string
			l                                                models.PurchaseOrderLine
			categoryEntry, assetEntry, category, asset, name sql.NullString
			packSize                                         sql.NullInt64
			price                                            sql.NullInt64
		)
		if err := rows.Scan(&orderID, &l.ID, &categoryEntry, &assetEntry, &category, &asset, &name, &l.PartNumber, &l.Description, &packSize,
			&l.PackUnit, &l.Quantity, &l.QuantityReceived, &price); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scanning purchase order line: %w", err)
		}
		i, ok := byID[orderID]
		if !ok {
			continue
		}
		o := &out[i]
		l.CatalogEntryID = categoryEntry.String
		if assetEntry.Valid {
			l.CatalogEntryID = assetEntry.String
		}
		l.CategoryID, l.AssetID, l.LinkedName = category.String, asset.String, name.String
		if packSize.Valid {
			n := int(packSize.Int64)
			l.PackSize = &n
		}
		if price.Valid {
			l.UnitPrice = money.Format(price.Int64, o.Currency)
			line := price.Int64 * int64(l.Quantity)
			l.Total = money.Format(line, o.Currency)
			totals[orderID] += line
			priced[orderID] = true
		}
		o.Lines = append(o.Lines, l)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for id, i := range byID {
		if priced[id] {
			out[i].Total = money.Format(totals[id], out[i].Currency)
		}
	}
	return out, nil
}

// ListPurchaseOrders returns every order, newest first, with its lines.
func (s *Store) ListPurchaseOrders() ([]models.PurchaseOrder, error) {
	return readOrders(storeQuerier{s}, "")
}

// GetPurchaseOrder returns an order, or ErrOrderNotFound.
func (s *Store) GetPurchaseOrder(id string) (*models.PurchaseOrder, error) {
	return getOrder(storeQuerier{s}, id)
}

func getOrder(q vendorQuerier, id string) (*models.PurchaseOrder, error) {
	os, err := readOrders(q, " WHERE o.id = ?", id)
	if err != nil {
		return nil, err
	}
	if len(os) == 0 {
		return nil, ErrOrderNotFound
	}
	return &os[0], nil
}

// lockOrderTx holds an order's row until tx ends, so that two changes at
// once each see what the other did, and reads it.
func (s *Store) lockOrderTx(tx *sql.Tx, id string) (*models.PurchaseOrder, error) {
	if _, err := s.TxExec(tx, "UPDATE purchase_orders SET updated_at = updated_at WHERE id = ?", id); err != nil {
		return nil, fmt.Errorf("locking purchase order: %w", err)
	}
	return getOrder(txQuerier{s, tx}, id)
}

// overflows reports whether a times b passes the largest price kept.
func overflows(a, b int64) bool {
	return a != 0 && b > money.MaxMinor/a
}

// orderLines checks an order's lines against what they name, within tx,
// and gives what is kept, settling the order's currency: a new order's
// first line taking an entry's price gives it the entry's. before is the
// order as it was, nil for one being made.
func (s *Store) orderLines(tx *sql.Tx, in *OrderInput, before *models.PurchaseOrder) ([]orderLineRow, error) {
	had := map[string]models.PurchaseOrderLine{}
	var prices map[string]*int64
	if before != nil {
		for _, l := range before.Lines {
			had[l.ID] = l
		}
		// The lines' prices as kept, rather than as shown.
		prices = map[string]*int64{}
		rows, err := s.TxQuery(tx, "SELECT id, unit_price_minor FROM purchase_order_lines WHERE purchase_order_id = ?", before.ID)
		if err != nil {
			return nil, fmt.Errorf("reading lines' prices: %w", err)
		}
		for rows.Next() {
			var id string
			var p sql.NullInt64
			if err := rows.Scan(&id, &p); err != nil {
				rows.Close()
				return nil, err
			}
			if p.Valid {
				v := p.Int64
				prices[id] = &v
			}
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	type pending struct {
		row   orderLineRow
		price *string
		entry *models.CatalogEntry
	}
	var lines []pending
	seen := map[string]bool{}
	for i, l := range in.Lines {
		n := i + 1
		p := pending{price: l.Price}
		r := &p.row
		old, kept := had[l.ID]
		if kept && !seen[l.ID] {
			r.id, r.received = l.ID, old.QuantityReceived
			seen[l.ID] = true
		} else {
			r.id, kept = uuid.NewString(), false
		}
		r.partNumber, r.description, r.packSize, r.packUnit, r.quantity = l.PartNumber, l.Description, l.PackSize, l.PackUnit, l.Quantity
		r.categoryID, r.assetID = l.CategoryID, l.AssetID
		if l.CatalogEntryID != "" {
			es, err := readCatalog(txQuerier{s, tx}, CatalogFilter{}, l.CatalogEntryID)
			if err != nil {
				return nil, err
			}
			if len(es) == 0 {
				return nil, invalidOrder("line %d: catalog entry not found: %s", n, l.CatalogEntryID)
			}
			e := es[0]
			if e.VendorID != in.VendorID {
				return nil, invalidOrder("line %d: the catalog entry is another vendor's", n)
			}
			if (r.categoryID != "" && r.categoryID != e.CategoryID) || (r.assetID != "" && r.assetID != e.AssetID) {
				return nil, invalidOrder("line %d is for what its catalog entry is for", n)
			}
			r.categoryID, r.assetID, r.catalogEntryID = e.CategoryID, e.AssetID, e.ID
			if e.CategoryID != "" {
				r.categoryEntry = &e.ID
			} else {
				r.assetEntry = &e.ID
			}
			if r.partNumber == "" && r.description == "" {
				r.partNumber, r.description = e.PartNumber, e.Description
			}
			if r.packSize == nil {
				r.packSize = e.PackSize
			}
			if r.packUnit == "" {
				r.packUnit = e.PackUnit
			}
			p.entry = &e
		}
		if r.partNumber == "" && r.description == "" {
			return nil, invalidOrder("line %d needs a part number or a description", n)
		}
		for _, link := range []struct {
			id, table string
			missing   error
		}{{r.categoryID, "categories", ErrCategoryNotFound}, {r.assetID, "assets", ErrAssetNotFound}} {
			if link.id == "" {
				continue
			}
			var count int
			if err := s.TxQueryRow(tx, "SELECT COUNT(*) FROM "+link.table+" WHERE id = ?", link.id).Scan(&count); err != nil {
				return nil, fmt.Errorf("checking what line %d is for: %w", n, err)
			}
			if count == 0 {
				return nil, fmt.Errorf("%w: %s", link.missing, link.id)
			}
		}
		r.category, r.asset = nonEmpty(r.categoryID), nonEmpty(r.assetID)
		if kept && old.QuantityReceived > 0 {
			if r.quantity < old.QuantityReceived {
				return nil, stateError("line %d: %d packs have come, so it cannot order fewer", n, old.QuantityReceived)
			}
			if r.categoryID != old.CategoryID || r.assetID != old.AssetID {
				return nil, stateError("line %d: something has come, so what it is for cannot change", n)
			}
		}
		if kept && p.price == nil {
			r.priceMinor = prices[r.id]
		}
		lines = append(lines, p)
	}
	for id, l := range had {
		if !seen[id] && l.QuantityReceived > 0 {
			return nil, stateError("a line something has come of cannot be removed: %s", lineText(l.PartNumber, l.Description))
		}
	}
	// A new order with no currency takes the first entry's whose price it
	// takes.
	if in.Currency == "" && before == nil {
		for _, p := range lines {
			if p.price == nil && p.entry != nil && p.entry.Price != "" && p.row.id != "" && !hadLine(had, p.row.id) {
				in.Currency = p.entry.Currency
				break
			}
		}
	}
	out := make([]orderLineRow, len(lines))
	var total int64
	for i, p := range lines {
		r := p.row
		n := i + 1
		switch {
		case p.price != nil && strings.TrimSpace(*p.price) != "":
			if in.Currency == "" {
				return nil, invalidOrder("a price needs the order's currency")
			}
			minor, err := money.Parse(strings.TrimSpace(*p.price), in.Currency)
			if err != nil {
				return nil, invalidOrder("line %d: the price: %v", n, err)
			}
			r.priceMinor = &minor
		case p.price != nil:
			r.priceMinor = nil
		case !hadLine(had, r.id) && p.entry != nil && p.entry.Price != "" && p.entry.Currency == in.Currency:
			minor, err := money.Parse(p.entry.Price, in.Currency)
			if err != nil {
				return nil, invalidOrder("line %d: the catalog's price: %v", n, err)
			}
			r.priceMinor = &minor
		}
		if r.priceMinor != nil {
			if in.Currency == "" {
				return nil, invalidOrder("a price needs the order's currency")
			}
			if overflows(int64(r.quantity), *r.priceMinor) {
				return nil, invalidOrder("line %d: its total is more than can be kept", n)
			}
			line := int64(r.quantity) * *r.priceMinor
			if total > money.MaxMinor-line {
				return nil, invalidOrder("the order's total is more than can be kept")
			}
			total += line
		}
		out[i] = r
	}
	return out, nil
}

func hadLine(had map[string]models.PurchaseOrderLine, id string) bool {
	_, ok := had[id]
	return ok
}

// lineText names a line by its part number and description, as history
// and errors do.
func lineText(partNumber, description string) string {
	return strings.TrimSpace(strings.TrimSpace(partNumber + " " + description))
}

// writeOrderLinesTx replaces an order's lines with these, in order.
func (s *Store) writeOrderLinesTx(tx *sql.Tx, id string, lines []orderLineRow) error {
	if _, err := s.TxExec(tx, "DELETE FROM purchase_order_lines WHERE purchase_order_id = ?", id); err != nil {
		return fmt.Errorf("replacing purchase order lines: %w", err)
	}
	for i, r := range lines {
		if _, err := s.TxExec(tx, `INSERT INTO purchase_order_lines (id, purchase_order_id, position, category_entry_id, asset_entry_id, category_id, asset_id,
			part_number, description, pack_size, pack_unit, quantity, quantity_received, unit_price_minor) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			r.id, id, i, r.categoryEntry, r.assetEntry, r.category, r.asset, r.partNumber, r.description, r.packSize, r.packUnit, r.quantity, r.received,
			r.priceMinor); err != nil {
			return fmt.Errorf("keeping purchase order line: %w", err)
		}
	}
	return nil
}

// receivedState is the state of a placed order with these lines: received
// once every line has come, partly received once any has.
func receivedState(lines []orderLineRow) string {
	some, all := false, len(lines) > 0
	for _, l := range lines {
		if l.received > 0 {
			some = true
		}
		if l.received < l.quantity {
			all = false
		}
	}
	switch {
	case all:
		return models.OrderReceived
	case some:
		return models.OrderPartlyReceived
	}
	return models.OrderOrdered
}

// vendorExistsTx checks a vendor exists.
func (s *Store) vendorExistsTx(tx *sql.Tx, id string) error {
	var n int
	if err := s.TxQueryRow(tx, "SELECT COUNT(*) FROM vendors WHERE id = ?", id).Scan(&n); err != nil {
		return fmt.Errorf("reading the vendor: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%w: %s", ErrVendorNotFound, id)
	}
	return nil
}

// CreatePurchaseOrder makes a draft order, numbered one more than the
// highest, and records it in its history. With no delivery address, it
// takes the newest order's.
func (s *Store) CreatePurchaseOrder(in OrderInput) (*models.PurchaseOrder, error) {
	if err := in.clean(); err != nil {
		return nil, err
	}
	id := uuid.NewString()
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if err := s.takeLock(tx, ordersLock); err != nil {
		return nil, err
	}
	if err := s.vendorExistsTx(tx, in.VendorID); err != nil {
		return nil, err
	}
	lines, err := s.orderLines(tx, &in, nil)
	if err != nil {
		return nil, err
	}
	var number int
	if err := s.TxQueryRow(tx, "SELECT COALESCE(MAX(number), 0) + 1 FROM purchase_orders").Scan(&number); err != nil {
		return nil, fmt.Errorf("numbering the order: %w", err)
	}
	if in.DeliverTo == "" {
		err := s.TxQueryRow(tx, "SELECT deliver_to FROM purchase_orders ORDER BY number DESC LIMIT 1").Scan(&in.DeliverTo)
		if err != nil && err != sql.ErrNoRows {
			return nil, fmt.Errorf("reading where orders are delivered: %w", err)
		}
	}
	now := dbTime(time.Now())
	if _, err := s.TxExec(tx, `INSERT INTO purchase_orders (id, number, vendor_id, state, currency, vendor_reference, ordered_on, expected_on, received_on,
		deliver_to, notes, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, number, in.VendorID, models.OrderDraft, in.Currency, in.VendorReference, nonEmpty(in.OrderedOn), nonEmpty(in.ExpectedOn), nil,
		in.DeliverTo, in.Notes, now, now); err != nil {
		return nil, fmt.Errorf("keeping the purchase order: %w", err)
	}
	if err := s.writeOrderLinesTx(tx, id, lines); err != nil {
		return nil, err
	}
	after, err := getOrder(txQuerier{s, tx}, id)
	if err != nil {
		return nil, err
	}
	if err := s.recordOrderEvent(tx, models.EventOrderCreated, after, models.EventPayload{Trigger: models.TriggerManual, Changes: diffOrders(nil, after)}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing: %w", err)
	}
	return s.GetPurchaseOrder(id)
}

// UpdatePurchaseOrder replaces what an order's state lets change, and
// records what changed in its history:
//
//   - a draft, everything;
//   - one placed or partly received, all but its vendor and currency, with
//     lines that something has come of kept, ordering at least what came,
//     for what they were;
//   - one received or canceled, only its reference and notes.
func (s *Store) UpdatePurchaseOrder(id string, in OrderInput) (*models.PurchaseOrder, error) {
	if err := in.clean(); err != nil {
		return nil, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	before, err := s.lockOrderTx(tx, id)
	if err != nil {
		return nil, err
	}
	if before.State != models.OrderDraft {
		switch {
		case in.VendorID != before.VendorID:
			return nil, stateError("the order has been placed: its vendor cannot change")
		case in.Currency != before.Currency && !(before.Currency == "" && in.Currency != ""):
			return nil, stateError("the order has been placed: its currency cannot change")
		}
	}
	if err := s.vendorExistsTx(tx, in.VendorID); err != nil {
		return nil, err
	}
	lines, err := s.orderLines(tx, &in, before)
	if err != nil {
		return nil, err
	}
	state, receivedOn := before.State, before.ReceivedOn
	if before.State == models.OrderReceived || before.State == models.OrderCanceled {
		if in.OrderedOn != before.OrderedOn || in.ExpectedOn != before.ExpectedOn || in.DeliverTo != before.DeliverTo || in.Currency != before.Currency ||
			!sameLines(lines, before) {
			return nil, stateError("the order is %s: only its reference and notes can change", strings.ReplaceAll(before.State, "_", " "))
		}
	}
	if before.State == models.OrderOrdered || before.State == models.OrderPartlyReceived {
		if len(lines) == 0 {
			return nil, stateError("a placed order needs at least one line; cancel it instead")
		}
		state = receivedState(lines)
		if state == models.OrderReceived && receivedOn == "" {
			receivedOn = time.Now().UTC().Format(DateLayout)
		}
	}
	if _, err := s.TxExec(tx, `UPDATE purchase_orders SET vendor_id = ?, state = ?, currency = ?, vendor_reference = ?, ordered_on = ?, expected_on = ?,
		received_on = ?, deliver_to = ?, notes = ?, updated_at = ? WHERE id = ?`,
		in.VendorID, state, in.Currency, in.VendorReference, nonEmpty(in.OrderedOn), nonEmpty(in.ExpectedOn), nonEmpty(receivedOn), in.DeliverTo,
		in.Notes, dbTime(time.Now()), id); err != nil {
		return nil, fmt.Errorf("keeping the purchase order: %w", err)
	}
	if err := s.writeOrderLinesTx(tx, id, lines); err != nil {
		return nil, err
	}
	after, err := getOrder(txQuerier{s, tx}, id)
	if err != nil {
		return nil, err
	}
	if changes := diffOrders(before, after); len(changes) > 0 {
		if err := s.recordOrderEvent(tx, models.EventOrderEdited, after, models.EventPayload{Trigger: models.TriggerManual, Changes: changes}); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing: %w", err)
	}
	return s.GetPurchaseOrder(id)
}

// sameLines reports whether lines are an order's lines as they are.
func sameLines(lines []orderLineRow, o *models.PurchaseOrder) bool {
	if len(lines) != len(o.Lines) {
		return false
	}
	for i, r := range lines {
		l := o.Lines[i]
		price := ""
		if r.priceMinor != nil {
			price = money.Format(*r.priceMinor, o.Currency)
		}
		if r.id != l.ID || r.quantity != l.Quantity || r.partNumber != l.PartNumber || r.description != l.Description || r.packUnit != l.PackUnit ||
			!sameSize(r.packSize, l.PackSize) || r.categoryID != l.CategoryID || r.assetID != l.AssetID || price != l.UnitPrice {
			return false
		}
	}
	return true
}

func sameSize(a, b *int) bool {
	return (a == nil) == (b == nil) && (a == nil || *a == *b)
}

// PlacePurchaseOrder marks a draft as placed with its vendor, on orderedOn
// (YYYY-MM-DD), else the date it was given, else today, and records
// purchase_order.ordered, which channels can subscribe to.
func (s *Store) PlacePurchaseOrder(id, orderedOn string) (*models.PurchaseOrder, error) {
	on, err := orderDate("the date ordered", orderedOn)
	if err != nil {
		return nil, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	before, err := s.lockOrderTx(tx, id)
	if err != nil {
		return nil, err
	}
	if before.State != models.OrderDraft {
		return nil, stateError("the order has already been placed")
	}
	if len(before.Lines) == 0 {
		return nil, stateError("an order needs at least one line to be placed")
	}
	if on == "" {
		on = before.OrderedOn
	}
	if on == "" {
		on = time.Now().UTC().Format(DateLayout)
	}
	if _, err := s.TxExec(tx, "UPDATE purchase_orders SET state = ?, ordered_on = ?, updated_at = ? WHERE id = ?",
		models.OrderOrdered, on, dbTime(time.Now()), id); err != nil {
		return nil, fmt.Errorf("placing the purchase order: %w", err)
	}
	after, err := getOrder(txQuerier{s, tx}, id)
	if err != nil {
		return nil, err
	}
	var ordered []models.EventOrderLine
	for _, l := range after.Lines {
		ordered = append(ordered, models.EventOrderLine{LineID: l.ID, PartNumber: l.PartNumber, Description: l.Description, Quantity: l.Quantity})
	}
	if err := s.recordOrderEvent(tx, models.EventOrderOrdered, after, models.EventPayload{Trigger: models.TriggerManual, OrderLines: ordered}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing: %w", err)
	}
	return s.GetPurchaseOrder(id)
}

// CancelPurchaseOrder cancels an order placed or partly received: what
// came stays, and the rest is not expected. A draft is deleted instead.
func (s *Store) CancelPurchaseOrder(id string) (*models.PurchaseOrder, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	before, err := s.lockOrderTx(tx, id)
	if err != nil {
		return nil, err
	}
	switch before.State {
	case models.OrderDraft:
		return nil, stateError("a draft is not canceled: delete it, or leave it unplaced")
	case models.OrderReceived:
		return nil, stateError("the order has been received")
	case models.OrderCanceled:
		return nil, stateError("the order has already been canceled")
	}
	if _, err := s.TxExec(tx, "UPDATE purchase_orders SET state = ?, updated_at = ? WHERE id = ?", models.OrderCanceled, dbTime(time.Now()), id); err != nil {
		return nil, fmt.Errorf("canceling the purchase order: %w", err)
	}
	after, err := getOrder(txQuerier{s, tx}, id)
	if err != nil {
		return nil, err
	}
	if err := s.recordOrderEvent(tx, models.EventOrderCanceled, after, models.EventPayload{Trigger: models.TriggerManual}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing: %w", err)
	}
	return s.GetPurchaseOrder(id)
}

// DeletePurchaseOrder deletes an order, in any state, with its lines and
// its files' rows. What it received stays received, and its files stay
// until bam storage sweep, as other attachments' do.
func (s *Store) DeletePurchaseOrder(id string) error {
	res, err := s.Exec("DELETE FROM purchase_orders WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("deleting purchase order: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrOrderNotFound
	}
	return nil
}

// vendorHasOrders reports whether any purchase order is to a vendor.
func (s *Store) vendorHasOrders(id string) (bool, error) {
	var n int
	if err := s.QueryRow("SELECT COUNT(*) FROM purchase_orders WHERE vendor_id = ?", id).Scan(&n); err != nil {
		return false, fmt.Errorf("reading the vendor's purchase orders: %w", err)
	}
	return n > 0, nil
}

// ReceiptInput is a delivery received against an order, on ReceivedOn
// (YYYY-MM-DD, today when empty).
type ReceiptInput struct {
	ReceivedOn string
	Lines      []ReceiptLine
}

// ReceiptLine is what came of one line: Quantity packs.
//
// For a line for an asset, Restock is how much its quantity goes up:
// Quantity times the pack's size unless given, which it must be when the
// pack's size is not known.
//
// For a line for a category, Assets is how many assets to make, one per
// pack unless given. They are made like Like, an asset as the API copies
// it, or else named Name (the line's description, part number, or the
// category's name, unless given) in the category with FieldValues. In a
// category that weighs, each starts counting stock in its unit, with Each
// (the pack's size, when the pack is in that unit and one asset is made
// per pack, unless given). Each has WarrantyEnd as its warranty's end.
type ReceiptLine struct {
	LineID      string
	Quantity    int
	Restock     *int
	Assets      *int
	Like        *AssetTemplate
	Name        string
	FieldValues map[string]string
	Each        *int
	WarrantyEnd string
}

// AssetTemplate is an asset to make others like, as copying it gives
// (docs/data-model.md): its name, categories, fields, stock, own icon,
// codes, each continued in its series for each copy, and links.
type AssetTemplate struct {
	Name        string
	CategoryIDs []string
	FieldValues map[string]string
	Stock       *StockInput
	Icon        string
	Codes       []models.AssetGeneratorKey
	Links       []TemplateLink
}

// TemplateLink is a link each copy makes, read from the copy.
type TemplateLink struct {
	TypeID   string
	OtherID  string
	Outgoing bool
}

// Restocked is an asset a receipt restocked, and by how much.
type Restocked struct {
	AssetID string `json:"asset_id"`
	By      int    `json:"by"`
}

// ReceiptResult is what a receipt did: the order as it is now, the
// assets made, and those restocked.
type ReceiptResult struct {
	Order     *models.PurchaseOrder
	Created   []string
	Restocked []Restocked
}

// receiptPlan is a line's receipt, checked and ready to apply.
type receiptPlan struct {
	line    models.PurchaseOrderLine
	qty     int
	restock int
	assets  []*newAsset
	codes   []models.AssetGeneratorKey
	links   []TemplateLink
}

// forOrder is a view of the store whose events name the order.
func (s *Store) forOrder(o *models.PurchaseOrder) *Store {
	c := *s
	c.order = eventOrder(o)
	return &c
}

// ReceivePurchaseOrder receives a delivery against an order placed or
// partly received: every line given, or none. Assets' lines restock them,
// and categories' lines make assets with the order's purchase details:
// its vendor, dates, number, and the line's price for the packs come,
// shared between them and rounded half up. Thresholds are rechecked once,
// at the end, as any change to stock. Each change names the order in its
// history, and the order records purchase_order.received, which channels
// can subscribe to.
func (s *Store) ReceivePurchaseOrder(id string, in ReceiptInput) (*ReceiptResult, error) {
	on, err := orderDate("the date received", in.ReceivedOn)
	if err != nil {
		return nil, err
	}
	if on == "" {
		on = time.Now().UTC().Format(DateLayout)
	}
	if len(in.Lines) == 0 {
		return nil, invalidOrder("a receipt needs at least one line")
	}
	order, err := s.GetPurchaseOrder(id)
	if err != nil {
		return nil, err
	}
	plans, err := s.planReceipt(order, in, on)
	if err != nil {
		return nil, err
	}
	result := &ReceiptResult{Created: []string{}, Restocked: []Restocked{}}
	view := s.forOrder(order)
	err = view.withInventory(func(ic *inventoryChange) error {
		now, err := view.lockOrderTx(ic.tx, id)
		if err != nil {
			return err
		}
		if now.State != models.OrderOrdered && now.State != models.OrderPartlyReceived {
			return stateError("only an order placed and not yet all received can be received")
		}
		current := map[string]models.PurchaseOrderLine{}
		for _, l := range now.Lines {
			current[l.ID] = l
		}
		var receipt []models.EventOrderLine
		for _, p := range plans {
			l, ok := current[p.line.ID]
			if !ok || l.Outstanding() < p.qty || l.CategoryID != p.line.CategoryID || l.AssetID != p.line.AssetID {
				return stateError("the order changed while it was being received; look again")
			}
			ev := models.EventOrderLine{LineID: l.ID, PartNumber: l.PartNumber, Description: l.Description, Quantity: p.qty}
			if l.AssetID != "" && p.restock > 0 {
				if err := view.restockTx(ic.tx, l, p.restock); err != nil {
					return err
				}
				ev.AssetID, ev.Restocked = l.AssetID, p.restock
				result.Restocked = append(result.Restocked, Restocked{AssetID: l.AssetID, By: p.restock})
			}
			for _, na := range p.assets {
				assetID := uuid.NewString()
				made := *na
				made.keys = nil
				for _, k := range p.codes {
					m := trailingNumber.FindStringSubmatch(k.AssignedCode)
					if m == nil {
						continue
					}
					next, err := nextCodeIn(txQuerier{view, ic.tx}, k.GeneratorType, &m[1])
					if err != nil {
						return err
					}
					if next != "" {
						made.keys = append(made.keys, models.AssetGeneratorKey{GeneratorType: k.GeneratorType, AssignedCode: next})
					}
				}
				if err := view.insertNewAssetTx(ic.tx, assetID, &made, models.TriggerOrder); err != nil {
					return err
				}
				for _, link := range p.links {
					if err := view.addLinkTx(ic.tx, assetID, link.TypeID, link.OtherID, link.Outgoing, models.TriggerOrder); err != nil {
						return fmt.Errorf("linking %s: %w", made.name, err)
					}
				}
				result.Created = append(result.Created, assetID)
				ev.Assets = append(ev.Assets, models.NamedThing{ID: assetID, Name: made.name})
			}
			if _, err := view.TxExec(ic.tx, "UPDATE purchase_order_lines SET quantity_received = quantity_received + ? WHERE id = ?", p.qty, l.ID); err != nil {
				return fmt.Errorf("receiving the line: %w", err)
			}
			receipt = append(receipt, ev)
		}
		var outstanding int
		if err := view.TxQueryRow(ic.tx, "SELECT COUNT(*) FROM purchase_order_lines WHERE purchase_order_id = ? AND quantity_received < quantity", id).Scan(&outstanding); err != nil {
			return fmt.Errorf("reading what is outstanding: %w", err)
		}
		state, receivedOn := models.OrderPartlyReceived, nonEmpty(now.ReceivedOn)
		if outstanding == 0 {
			state, receivedOn = models.OrderReceived, &on
		}
		if _, err := view.TxExec(ic.tx, "UPDATE purchase_orders SET state = ?, received_on = ?, updated_at = ? WHERE id = ?", state, receivedOn, dbTime(time.Now()), id); err != nil {
			return fmt.Errorf("receiving the order: %w", err)
		}
		after, err := getOrder(txQuerier{view, ic.tx}, id)
		if err != nil {
			return err
		}
		return view.recordOrderEvent(ic.tx, models.EventOrderReceived, after, models.EventPayload{Trigger: models.TriggerManual, OrderLines: receipt})
	})
	if err != nil {
		return nil, err
	}
	if result.Order, err = s.GetPurchaseOrder(id); err != nil {
		return nil, err
	}
	return result, nil
}

// planReceipt checks a receipt against the order as read, and prepares
// what it makes.
func (s *Store) planReceipt(order *models.PurchaseOrder, in ReceiptInput, on string) ([]receiptPlan, error) {
	if order.State != models.OrderOrdered && order.State != models.OrderPartlyReceived {
		return nil, stateError("only an order placed and not yet all received can be received")
	}
	lines := map[string]models.PurchaseOrderLine{}
	numbers := map[string]int{}
	for i, l := range order.Lines {
		lines[l.ID], numbers[l.ID] = l, i+1
	}
	// The price each asset made is given comes from the lines as kept.
	prices := map[string]int64{}
	rows, err := s.Query("SELECT id, unit_price_minor FROM purchase_order_lines WHERE purchase_order_id = ? AND unit_price_minor IS NOT NULL", order.ID)
	if err != nil {
		return nil, fmt.Errorf("reading lines' prices: %w", err)
	}
	for rows.Next() {
		var id string
		var p int64
		if err := rows.Scan(&id, &p); err != nil {
			rows.Close()
			return nil, err
		}
		prices[id] = p
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	total := 0
	var plans []receiptPlan
	for _, r := range in.Lines {
		l, ok := lines[strings.TrimSpace(r.LineID)]
		if !ok {
			return nil, invalidOrder("line not found: %s", r.LineID)
		}
		n := numbers[l.ID]
		if seen[l.ID] {
			return nil, invalidOrder("line %d is given twice", n)
		}
		seen[l.ID] = true
		if r.Quantity < 1 {
			return nil, invalidOrder("line %d: at least one pack must come", n)
		}
		if r.Quantity > l.Outstanding() {
			return nil, stateError("line %d: only %d outstanding", n, l.Outstanding())
		}
		p := receiptPlan{line: l, qty: r.Quantity}
		switch {
		case l.AssetID != "":
			switch {
			case r.Restock != nil:
				p.restock = *r.Restock
			case l.PackSize != nil:
				p.restock = r.Quantity * *l.PackSize
			default:
				return nil, invalidOrder("line %d: its pack's size is not known, so say how much it restocks", n)
			}
			if p.restock < 0 || p.restock > MaxQuantity {
				return nil, invalidOrder("line %d: it can restock from 0 to %d", n, MaxQuantity)
			}
		case l.CategoryID != "":
			count := r.Quantity
			if r.Assets != nil {
				count = *r.Assets
			}
			if count < 0 || count > MaxReceiptAssets {
				return nil, invalidOrder("line %d: it can make from 0 to %d assets", n, MaxReceiptAssets)
			}
			total += count
			if total > MaxReceiptAssets {
				return nil, invalidOrder("a receipt can make at most %d assets", MaxReceiptAssets)
			}
			if count == 0 {
				break
			}
			if err := s.planAssets(&p, order, r, n, count, prices, on); err != nil {
				return nil, err
			}
		}
		plans = append(plans, p)
	}
	return plans, nil
}

// planAssets prepares the assets a category's line makes.
func (s *Store) planAssets(p *receiptPlan, order *models.PurchaseOrder, r ReceiptLine, n, count int, prices map[string]int64, on string) error {
	l := p.line
	cat, err := s.GetCategoryByID(l.CategoryID)
	if err != nil {
		return err
	}
	var (
		name   string
		cats   []string
		values map[string]string
		stock  *StockInput
		icon   string
	)
	name = strings.TrimSpace(r.Name)
	cats, values = []string{l.CategoryID}, map[string]string{}
	if t := r.Like; t != nil {
		if name == "" {
			name = t.Name
		}
		cats, icon = append([]string{}, t.CategoryIDs...), t.Icon
		for k, v := range t.FieldValues {
			values[k] = v
		}
		if t.Stock != nil {
			st := *t.Stock
			stock = &st
		}
		if !slices.Contains(cats, l.CategoryID) {
			cats = append(cats, l.CategoryID)
		}
		p.codes, p.links = t.Codes, t.Links
	}
	for k, v := range r.FieldValues {
		values[k] = v
	}
	switch {
	case name != "":
	case l.Description != "":
		name = l.Description
	case l.PartNumber != "":
		name = l.PartNumber
	default:
		name = cat.Name
	}
	if len([]rune(name)) > textcheck.MaxName {
		name = string([]rune(name)[:textcheck.MaxName])
	}
	// A category that weighs counts each asset's stock in its unit.
	if cat.IsConsumable && cat.Unit != "" {
		each := r.Each
		if each == nil && count == r.Quantity && l.PackSize != nil && strings.EqualFold(l.PackUnit, cat.Unit) {
			each = l.PackSize
		}
		if each == nil {
			return invalidOrder("line %d: %s weighs in %s, so say how much each asset holds", n, cat.Name, cat.Unit)
		}
		if stock == nil || !strings.EqualFold(stock.Unit, cat.Unit) {
			stock = &StockInput{Unit: cat.Unit}
		}
		q := *each
		stock.Quantity = &q
	} else if r.Each != nil && stock != nil {
		q := *r.Each
		stock.Quantity = &q
	}
	purchase := PurchaseInput{VendorID: order.VendorID, OrderDate: order.OrderedOn, ReceivedDate: on, OrderNumber: order.Reference,
		WarrantyEnd: r.WarrantyEnd}
	if price, ok := prices[l.ID]; ok && order.Currency != "" {
		// The packs come, shared between the assets made, half up.
		all := price * int64(r.Quantity)
		each := (2*all + int64(count)) / (2 * int64(count))
		purchase.Price, purchase.Currency = money.Format(each, order.Currency), order.Currency
	}
	na, err := s.prepareAsset(name, cats, nil, values, stock, icon, &purchase)
	if err != nil {
		return fmt.Errorf("line %d: %w", n, err)
	}
	for i := 0; i < count; i++ {
		p.assets = append(p.assets, na)
	}
	return nil
}

// restockTx adds by to a line's asset, as using and restocking does,
// recording it in the asset's history.
func (s *Store) restockTx(tx *sql.Tx, l models.PurchaseOrderLine, by int) error {
	was, err := s.readStockTx(tx, l.AssetID)
	switch {
	case errors.Is(err, ErrAssetArchived):
		return stateError("%s is archived: restore it, or change the line", l.LinkedName)
	case err != nil:
		return err
	case was == nil:
		return stateError("%s does not count its stock by quantity: change the line", l.LinkedName)
	}
	quantity := was.Quantity + by
	if quantity > MaxQuantity {
		return ErrQuantityOutOfRange
	}
	if _, err := s.TxExec(tx, "UPDATE assets SET stock_quantity = ?, updated_at = ? WHERE id = ?", quantity, time.Now().UTC(), l.AssetID); err != nil {
		return fmt.Errorf("restocking: %w", err)
	}
	return s.recordAssetEvent(tx, models.EventQuantityChanged, l.AssetID, models.EventPayload{Trigger: models.TriggerOrder, Quantity: &quantity, Change: &by})
}

// orderPriceText is a line's price with its currency, as history shows it.
func orderPriceText(price, currency string) string {
	if price == "" {
		return ""
	}
	return strings.TrimSpace(price + " " + currency)
}

// orderLineText is a line as history shows it, without its price.
func orderLineText(l models.PurchaseOrderLine) string {
	text := strconv.Itoa(l.Quantity) + " × " + lineText(l.PartNumber, l.Description)
	if l.PackSize != nil || l.PackUnit != "" {
		pack := l.PackUnit
		if l.PackSize != nil {
			pack = strings.TrimSpace(strconv.Itoa(*l.PackSize) + " " + l.PackUnit)
		}
		text += " (" + pack + ")"
	}
	return text
}

// diffOrders lists what changed from before to after, nil before being
// an order just made: its details, then its lines, each by its text, and
// apart from it, its price.
func diffOrders(before, after *models.PurchaseOrder) []models.EventChange {
	if before == nil {
		before = &models.PurchaseOrder{}
	}
	var changes []models.EventChange
	if before.VendorID != after.VendorID {
		changes = append(changes, models.EventChange{What: "vendor", Key: after.VendorID, From: before.VendorName, To: after.VendorName})
	}
	for _, f := range []struct{ what, from, to string }{
		{"currency", before.Currency, after.Currency},
		{"vendor_reference", before.VendorReference, after.VendorReference},
		{"ordered_on", before.OrderedOn, after.OrderedOn},
		{"expected_on", before.ExpectedOn, after.ExpectedOn},
		{"deliver_to", before.DeliverTo, after.DeliverTo},
	} {
		if f.from != f.to {
			changes = append(changes, models.EventChange{What: f.what, From: f.from, To: f.to})
		}
	}
	if before.Notes != after.Notes {
		changes = append(changes, models.EventChange{What: "notes"})
	}
	had := map[string]models.PurchaseOrderLine{}
	for _, l := range before.Lines {
		had[l.ID] = l
	}
	has := map[string]bool{}
	for _, l := range after.Lines {
		has[l.ID] = true
		old, ok := had[l.ID]
		switch {
		case !ok:
			changes = append(changes, models.EventChange{What: "line", Key: l.ID, To: orderLineText(l)})
		case orderLineText(old) != orderLineText(l):
			changes = append(changes, models.EventChange{What: "line", Key: l.ID, From: orderLineText(old), To: orderLineText(l)})
		}
		if from, to := orderPriceText(old.UnitPrice, before.Currency), orderPriceText(l.UnitPrice, after.Currency); from != to {
			changes = append(changes, models.EventChange{What: "price", Key: l.ID, Label: lineText(l.PartNumber, l.Description), From: from, To: to})
		}
	}
	for _, l := range before.Lines {
		if !has[l.ID] {
			changes = append(changes, models.EventChange{What: "line", Key: l.ID, From: orderLineText(l)})
		}
	}
	return changes
}

// OrderChanges are the kinds of change in an order's history that only
// purchases.read sees.
var OrderChanges = map[string]bool{"price": true, "currency": true}

// eventOrder is an order as its events name it.
func eventOrder(o *models.PurchaseOrder) *models.EventOrder {
	return &models.EventOrder{ID: o.ID, Number: o.Reference, VendorID: o.VendorID, VendorName: o.VendorName}
}

// recordOrderEvent stores an event about an order, as it is now, in its
// history.
func (s *Store) recordOrderEvent(tx *sql.Tx, kind string, o *models.PurchaseOrder, payload models.EventPayload) error {
	payload.PurchaseOrder = eventOrder(o)
	_, err := s.insertEventID(tx, kind, nil, nil, payload)
	return err
}

// recordOrderEditedTx records changes to an order's attachments in its
// history.
func (s *Store) recordOrderEditedTx(tx *sql.Tx, id string, payload models.EventPayload) error {
	o, err := getOrder(txQuerier{s, tx}, id)
	if err != nil {
		return err
	}
	return s.recordOrderEvent(tx, models.EventOrderEdited, o, payload)
}

// ListOrderEvents returns an order's own events, newest first: up to
// limit of them, starting after the event before when it is not empty.
// more reports whether older events remain. The changes a receipt made to
// assets and categories name the order too, but are their history.
func (s *Store) ListOrderEvents(id, before string, limit int) (events []models.Event, more bool, err error) {
	const own = " FROM events WHERE purchase_order_id = ? AND asset_id IS NULL AND category_id IS NULL"
	query, args := `SELECT `+eventColumns+own, []any{id}
	if before != "" {
		var at sql.NullTime
		err := s.QueryRow("SELECT created_at"+own+" AND id = ?", id, before).Scan(&at)
		if err == sql.ErrNoRows {
			return nil, false, ErrEventNotFound
		}
		if err != nil {
			return nil, false, fmt.Errorf("finding the page's first event: %w", err)
		}
		query += " AND (created_at < ? OR (created_at = ? AND id < ?))"
		args = append(args, at.Time, at.Time, before)
	}
	rows, err := s.Query(fmt.Sprintf(query+" ORDER BY created_at DESC, id DESC LIMIT %d", limit+1), args...)
	if err != nil {
		return nil, false, fmt.Errorf("listing purchase order events: %w", err)
	}
	defer rows.Close()
	events = []models.Event{}
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, false, err
		}
		events = append(events, e)
	}
	if len(events) > limit {
		return events[:limit], true, rows.Err()
	}
	return events, false, rows.Err()
}

// OrderCandidate is something running low to suggest ordering: a
// consumable category, or an asset counted by quantity, with its level,
// threshold and unit, and its preferred catalog entry, if it has one.
type OrderCandidate struct {
	CategoryID string
	AssetID    string
	Name       string
	Level      int
	Threshold  *int
	Unit       string
	// Weighs is set for a category that totals its assets' stock in Unit.
	Weighs    bool
	Preferred *models.CatalogEntry
}

// OrderCandidates lists what is running low, each with its preferred
// catalog entry: consumable categories flagged, and active assets counted
// by quantity flagged. The API decides who sees which.
func (s *Store) OrderCandidates() ([]OrderCandidate, error) {
	preferred, err := s.ListCatalogEntries(CatalogFilter{Preferred: true})
	if err != nil {
		return nil, err
	}
	byThing := map[string]*models.CatalogEntry{}
	for i := range preferred {
		e := &preferred[i]
		byThing[e.CategoryID+"/"+e.AssetID] = e
	}
	cats, err := s.ListCategories()
	if err != nil {
		return nil, err
	}
	out := []OrderCandidate{}
	for _, c := range cats {
		if !c.IsConsumable || !c.IsRunningLow {
			continue
		}
		cand := OrderCandidate{CategoryID: c.ID, Name: c.Name, Threshold: c.LowStockThreshold, Unit: c.Unit, Weighs: c.Unit != "",
			Preferred: byThing[c.ID+"/"]}
		if cand.Weighs {
			cand.Level, err = s.CategoryTotal(c.ID, c.Unit)
		} else {
			cand.Level, err = s.ActiveCount(c.ID)
		}
		if err != nil {
			return nil, err
		}
		out = append(out, cand)
	}
	rows, err := s.Query("SELECT id, name, stock_quantity, stock_unit, low_stock_threshold FROM assets WHERE archived_at IS NULL AND is_running_low = ? AND stock_quantity IS NOT NULL ORDER BY name, id", true)
	if err != nil {
		return nil, fmt.Errorf("reading assets running low: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cand      OrderCandidate
			threshold sql.NullInt64
		)
		if err := rows.Scan(&cand.AssetID, &cand.Name, &cand.Level, &cand.Unit, &threshold); err != nil {
			return nil, err
		}
		if threshold.Valid {
			t := int(threshold.Int64)
			cand.Threshold = &t
		}
		cand.Preferred = byThing["/"+cand.AssetID]
		out = append(out, cand)
	}
	return out, rows.Err()
}

// SuggestedPacks is how many packs of a candidate's preferred entry bring
// it back to its threshold, at least one (docs/purchase-orders.md#starting-from-the-running-low-list).
func SuggestedPacks(c OrderCandidate) int {
	if c.Threshold == nil || c.Preferred == nil {
		return 1
	}
	short := *c.Threshold - c.Level
	if short < 1 {
		return 1
	}
	e := c.Preferred
	packs := 1
	switch {
	case c.AssetID != "":
		if e.PackSize != nil {
			packs = (short + *e.PackSize - 1) / *e.PackSize
		}
	case c.Weighs:
		if e.PackSize != nil && strings.EqualFold(e.PackUnit, c.Unit) {
			packs = (short + *e.PackSize - 1) / *e.PackSize
		}
	default:
		// A category counted by items: each pack is an asset.
		packs = short
	}
	return max(1, min(packs, MaxOrderQuantity))
}
