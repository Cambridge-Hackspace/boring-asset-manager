package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/money"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/textcheck"
)

// Purchases (docs/data-model.md#purchases): where an asset came from. They
// are kept in asset_purchases, a row for each asset that has any.

// DateLayout is how a purchase's dates are written, and kept.
const DateLayout = "2006-01-02"

var (
	// ErrInvalidPurchase is wrapped by every problem with purchase details.
	ErrInvalidPurchase = errors.New("invalid purchase details")
	// ErrInvoiceNotAttached refuses an invoice that is not one of the
	// asset's own attachments.
	ErrInvoiceNotAttached = errors.New("the invoice must be one of the asset's attachments")
)

// PurchaseInput is an asset's purchase details as given, every one of them:
// an empty one is not recorded. Dates are YYYY-MM-DD; Price is a decimal in
// Currency, which it needs.
type PurchaseInput struct {
	VendorID     string
	OrderDate    string
	ReceivedDate string
	WarrantyEnd  string
	Price        string
	Currency     string
	OrderNumber  string
	InvoiceID    string
}

// PurchaseInputOf is the input that keeps a purchase as it is.
func PurchaseInputOf(p models.Purchase) PurchaseInput {
	return PurchaseInput{VendorID: p.VendorID, OrderDate: p.OrderDate, ReceivedDate: p.ReceivedDate, WarrantyEnd: p.WarrantyEnd,
		Price: p.Price, Currency: p.Currency, OrderNumber: p.OrderNumber, InvoiceID: p.InvoiceID}
}

// purchaseRow is purchase details checked and ready to keep.
type purchaseRow struct {
	vendorID, invoiceID                  *string
	orderDate, receivedDate, warrantyEnd *string
	priceMinor                           *int64
	currency, orderNumber                string
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// CheckDate reads a date written YYYY-MM-DD, trimmed; empty stays empty.
func CheckDate(what, s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	t, err := time.Parse(DateLayout, s)
	if err != nil || t.Year() < 1000 {
		return "", fmt.Errorf("%w: %s must be a date written YYYY-MM-DD", ErrInvalidPurchase, what)
	}
	return t.Format(DateLayout), nil
}

// check validates the details, and gives what is kept: nil when none is
// given.
func (in PurchaseInput) check() (*purchaseRow, error) {
	var (
		row purchaseRow
		err error
	)
	dates := []struct {
		what, value string
		into        **string
	}{
		{"the order date", in.OrderDate, &row.orderDate},
		{"the received date", in.ReceivedDate, &row.receivedDate},
		{"the warranty end", in.WarrantyEnd, &row.warrantyEnd},
	}
	for _, d := range dates {
		v, err := CheckDate(d.what, d.value)
		if err != nil {
			return nil, err
		}
		*d.into = nonEmpty(v)
	}
	if row.currency, err = money.NormalizeCurrency(in.Currency); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidPurchase, err)
	}
	if price := strings.TrimSpace(in.Price); price != "" {
		if row.currency == "" {
			return nil, fmt.Errorf("%w: a price needs its currency", ErrInvalidPurchase)
		}
		minor, err := money.Parse(price, row.currency)
		if err != nil {
			return nil, fmt.Errorf("%w: the price: %v", ErrInvalidPurchase, err)
		}
		row.priceMinor = &minor
	}
	row.orderNumber = strings.TrimSpace(in.OrderNumber)
	if err := textcheck.Check("the order number", row.orderNumber, textcheck.MaxName); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidPurchase, err)
	}
	row.vendorID = nonEmpty(strings.TrimSpace(in.VendorID))
	row.invoiceID = nonEmpty(strings.TrimSpace(in.InvoiceID))
	if row.vendorID == nil && row.invoiceID == nil && row.orderDate == nil && row.receivedDate == nil && row.warrantyEnd == nil &&
		row.priceMinor == nil && row.currency == "" && row.orderNumber == "" {
		return nil, nil
	}
	return &row, nil
}

// CheckPurchase validates purchase details without keeping them, as an
// import checks its rows first.
func CheckPurchase(in PurchaseInput) error {
	_, err := in.check()
	return err
}

// setPurchaseTx replaces an asset's purchase details within tx. The vendor
// must exist and the invoice be one of the asset's attachments.
func (s *Store) setPurchaseTx(tx *sql.Tx, assetID string, in PurchaseInput) error {
	row, err := in.check()
	if err != nil {
		return err
	}
	if _, err := s.TxExec(tx, "DELETE FROM asset_purchases WHERE asset_id = ?", assetID); err != nil {
		return fmt.Errorf("replacing purchase details: %w", err)
	}
	if row == nil {
		return nil
	}
	if row.vendorID != nil {
		var n int
		if err := s.TxQueryRow(tx, "SELECT COUNT(*) FROM vendors WHERE id = ?", *row.vendorID).Scan(&n); err != nil {
			return fmt.Errorf("reading the vendor: %w", err)
		}
		if n == 0 {
			return fmt.Errorf("%w: %s", ErrVendorNotFound, *row.vendorID)
		}
	}
	if row.invoiceID != nil {
		var n int
		if err := s.TxQueryRow(tx, "SELECT COUNT(*) FROM asset_attachments WHERE id = ? AND asset_id = ?", *row.invoiceID, assetID).Scan(&n); err != nil {
			return fmt.Errorf("reading the invoice: %w", err)
		}
		if n == 0 {
			return ErrInvoiceNotAttached
		}
	}
	_, err = s.TxExec(tx, `INSERT INTO asset_purchases (asset_id, vendor_id, order_date, received_date, price_minor, currency, order_number, warranty_end, invoice_attachment_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, assetID, row.vendorID, row.orderDate, row.receivedDate, row.priceMinor, row.currency, row.orderNumber, row.warrantyEnd, row.invoiceID)
	if err != nil {
		return fmt.Errorf("keeping purchase details: %w", err)
	}
	return nil
}

const purchaseSelect = `SELECT p.asset_id, p.vendor_id, v.name, p.order_date, p.received_date, p.price_minor, p.currency, p.order_number, p.warranty_end,
	p.invoice_attachment_id, i.name
	FROM asset_purchases p LEFT JOIN vendors v ON v.id = p.vendor_id LEFT JOIN asset_attachments i ON i.id = p.invoice_attachment_id`

type purchaseQuerier interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

// readPurchases reads purchase details, by asset.
func readPurchases(q purchaseQuerier, where string, args ...any) (map[string]models.Purchase, error) {
	rows, err := q.Query(purchaseSelect+where, args...)
	if err != nil {
		return nil, fmt.Errorf("reading purchase details: %w", err)
	}
	defer rows.Close()
	out := map[string]models.Purchase{}
	for rows.Next() {
		var (
			assetID                                                     string
			p                                                           models.Purchase
			vendorID, vendorName, ordered, received, ends, invoice, inv sql.NullString
			minor                                                       sql.NullInt64
		)
		if err := rows.Scan(&assetID, &vendorID, &vendorName, &ordered, &received, &minor, &p.Currency, &p.OrderNumber, &ends, &invoice, &inv); err != nil {
			return nil, fmt.Errorf("scanning purchase details: %w", err)
		}
		p.VendorID, p.VendorName = vendorID.String, vendorName.String
		p.OrderDate, p.ReceivedDate, p.WarrantyEnd = ordered.String, received.String, ends.String
		p.InvoiceID, p.InvoiceName = invoice.String, inv.String
		if minor.Valid {
			p.Price = money.Format(minor.Int64, p.Currency)
		}
		out[assetID] = p
	}
	return out, rows.Err()
}

// purchaseDetails fills in an asset's purchase details, from every asset's
// when ctx serves a list.
func (s *Store) purchaseDetails(a *models.AssetWithDetails, ctx *assetContext) error {
	if ctx.list {
		if ctx.purchases == nil {
			all, err := readPurchases(s, "")
			if err != nil {
				return err
			}
			ctx.purchases = all
		}
		a.Purchase = ctx.purchases[a.Asset.ID]
		return nil
	}
	own, err := readPurchases(s, " WHERE p.asset_id = ?", a.Asset.ID)
	if err != nil {
		return err
	}
	a.Purchase = own[a.Asset.ID]
	return nil
}

// purchaseTx reads an asset's purchase details within tx, for its history.
func (s *Store) purchaseTx(tx *sql.Tx, assetID string) (models.Purchase, error) {
	own, err := readPurchases(txQuerier{s, tx}, " WHERE p.asset_id = ?", assetID)
	return own[assetID], err
}

// diffPurchases lists how an asset's purchase details changed, for its
// history: the vendor and invoice by name, the price with its currency.
func diffPurchases(before, after models.Purchase) []models.EventChange {
	var changes []models.EventChange
	add := func(what, from, to string) {
		if from != to {
			changes = append(changes, models.EventChange{What: what, From: from, To: to})
		}
	}
	if before.VendorID != after.VendorID {
		changes = append(changes, models.EventChange{What: "vendor", Key: after.VendorID, From: before.VendorName, To: after.VendorName})
	}
	add("order_date", before.OrderDate, after.OrderDate)
	add("received_date", before.ReceivedDate, after.ReceivedDate)
	add("price", priceText(before), priceText(after))
	add("order_number", before.OrderNumber, after.OrderNumber)
	add("warranty_end", before.WarrantyEnd, after.WarrantyEnd)
	if before.InvoiceID != after.InvoiceID {
		changes = append(changes, models.EventChange{What: "invoice", Key: after.InvoiceID, From: before.InvoiceName, To: after.InvoiceName})
	}
	return changes
}

// priceText is a price with its currency, as history shows it.
func priceText(p models.Purchase) string {
	return strings.TrimSpace(p.Price + " " + p.Currency)
}

// PriceChanges are the kinds of change in an asset's history that only
// purchases.read sees.
var PriceChanges = map[string]bool{"price": true, "order_number": true, "invoice": true}

// clearInvoiceTx forgets an attachment as its asset's invoice, as it is
// removed, giving the change for the asset's history when it was.
func (s *Store) clearInvoiceTx(tx *sql.Tx, assetID string, a models.Attachment) ([]models.EventChange, error) {
	res, err := s.TxExec(tx, "UPDATE asset_purchases SET invoice_attachment_id = NULL WHERE asset_id = ? AND invoice_attachment_id = ?", assetID, a.ID)
	if err != nil {
		return nil, fmt.Errorf("forgetting the invoice: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, nil
	}
	return []models.EventChange{{What: "invoice", From: a.Name}}, nil
}

// Warranties (docs/notifications.md): an asset's warranty.expiring event is
// recorded once for each end its warranty has, a number of days before it.

// DefaultWarrantyNoticeDays is how many days before a warranty ends its
// event is recorded, unless the configuration says otherwise.
const DefaultWarrantyNoticeDays = 30

// RecordExpiringWarranties records warranty.expiring for each active asset
// whose warranty ends within days of now, today included, and has not had
// one for that end. Each is claimed by inserting it into warranty_notices,
// so servers sharing the database record it once. A warranty that ended
// before it was first seen is not announced. It returns how many it
// recorded.
func (s *Store) RecordExpiringWarranties(now time.Time, days int) (int, error) {
	if days < 0 {
		return 0, nil
	}
	today := now.UTC().Format(DateLayout)
	until := now.UTC().AddDate(0, 0, days).Format(DateLayout)
	rows, err := s.Query(`SELECT p.asset_id, p.warranty_end FROM asset_purchases p
		JOIN assets a ON a.id = p.asset_id
		WHERE a.archived_at IS NULL AND p.warranty_end IS NOT NULL AND p.warranty_end >= ? AND p.warranty_end <= ?
		AND NOT EXISTS (SELECT 1 FROM warranty_notices n WHERE n.asset_id = p.asset_id AND n.warranty_end = p.warranty_end)
		ORDER BY p.warranty_end, p.asset_id`, today, until)
	if err != nil {
		return 0, fmt.Errorf("finding warranties ending: %w", err)
	}
	type ending struct{ asset, end string }
	var due []ending
	for rows.Next() {
		var e ending
		if err := rows.Scan(&e.asset, &e.end); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scanning a warranty ending: %w", err)
		}
		due = append(due, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	recorded := 0
	for _, e := range due {
		ok, err := s.recordWarranty(e.asset, e.end, now)
		if err != nil {
			return recorded, err
		}
		if ok {
			recorded++
		}
	}
	return recorded, nil
}

// recordWarranty claims and records one asset's warranty.expiring,
// answering false when another server claimed it first.
func (s *Store) recordWarranty(assetID, end string, now time.Time) (bool, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return false, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := s.TxExec(tx, "INSERT INTO warranty_notices (asset_id, warranty_end, sent_at) VALUES (?, ?, ?)", assetID, end, dbTime(now)); err != nil {
		if IsUniqueViolation(err) {
			return false, nil
		}
		return false, fmt.Errorf("claiming the warranty notice: %w", err)
	}
	at, err := time.Parse(DateLayout, end)
	if err != nil {
		return false, fmt.Errorf("reading the warranty end: %w", err)
	}
	if err := s.recordAssetEvent(tx, models.EventWarrantyExpiring, assetID, models.EventPayload{Trigger: models.TriggerSchedule, DueAt: &at}); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// overlayPurchase is had with each detail in given that is not empty.
func overlayPurchase(had, given PurchaseInput) PurchaseInput {
	for _, f := range []struct {
		into *string
		v    string
	}{
		{&had.VendorID, given.VendorID}, {&had.OrderDate, given.OrderDate}, {&had.ReceivedDate, given.ReceivedDate},
		{&had.WarrantyEnd, given.WarrantyEnd}, {&had.Price, given.Price}, {&had.Currency, given.Currency},
		{&had.OrderNumber, given.OrderNumber}, {&had.InvoiceID, given.InvoiceID},
	} {
		if f.v != "" {
			*f.into = f.v
		}
	}
	return had
}
