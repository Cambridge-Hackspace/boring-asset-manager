package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/textcheck"
)

// Limits on an asset's stock (docs/stock-and-lending.md). Quantities fit the 32-bit
// INTEGER every database has.
const (
	MaxQuantity  = 1_000_000_000
	MaxStockUnit = 32
)

var (
	ErrNotStocked          = errors.New("the asset's stock is not counted by quantity")
	ErrQuantityOutOfRange  = fmt.Errorf("quantity must be between 0 and %d", MaxQuantity)
	ErrStockThresholdRange = fmt.Errorf("low stock threshold must be between 1 and %d", MaxQuantity)
	ErrNotEnoughStock      = errors.New("not enough stock")
	ErrAssetArchived       = errors.New("cannot update an archived asset")
)

// StockInput is an asset's stock as a caller sets it. The running-low flag
// is not part of it: the threshold rule or SetAssetRunningLow sets that.
type StockInput struct {
	// Quantity sets the quantity; nil keeps the asset's current one, or 0
	// for an asset whose stock was not counted.
	Quantity          *int
	Unit              string
	LowStockThreshold *int
}

// normalize trims and checks a stock.
func (in *StockInput) normalize() error {
	in.Unit = strings.TrimSpace(in.Unit)
	if err := textcheck.Check("unit", in.Unit, MaxStockUnit); err != nil {
		return err
	}
	if q := in.Quantity; q != nil && (*q < 0 || *q > MaxQuantity) {
		return ErrQuantityOutOfRange
	}
	if t := in.LowStockThreshold; t != nil && (*t < 1 || *t > MaxQuantity) {
		return ErrStockThresholdRange
	}
	return nil
}

// stockColumns lists an asset's stock columns, in scanStock's order.
const stockColumns = "stock_quantity, stock_unit, low_stock_threshold, is_running_low"

// stockScan holds an asset's stock columns as read.
type stockScan struct {
	quantity, threshold sql.NullInt64
	unit                string
	runningLow          bool
}

func (s *stockScan) dest() []any {
	return []any{&s.quantity, &s.unit, &s.threshold, &s.runningLow}
}

// stock is the asset's stock, or nil when it is not counted by quantity.
func (s *stockScan) stock() *models.AssetStock {
	if !s.quantity.Valid {
		return nil
	}
	st := &models.AssetStock{Quantity: int(s.quantity.Int64), Unit: s.unit, IsRunningLow: s.runningLow}
	if s.threshold.Valid {
		t := int(s.threshold.Int64)
		st.LowStockThreshold = &t
	}
	return st
}

// assetLevels reads every active asset with a threshold, with its quantity.
func (s *Store) assetLevels(tx *sql.Tx) (map[string]stock, error) {
	rows, err := s.TxQuery(tx, `SELECT id, stock_quantity, low_stock_threshold, is_running_low FROM assets
		WHERE archived_at IS NULL AND stock_quantity IS NOT NULL AND low_stock_threshold IS NOT NULL`)
	if err != nil {
		return nil, fmt.Errorf("reading asset stock: %w", err)
	}
	defer rows.Close()
	levels := map[string]stock{}
	for rows.Next() {
		var (
			id string
			l  stock
		)
		if err := rows.Scan(&id, &l.count, &l.threshold, &l.runningLow); err != nil {
			return nil, fmt.Errorf("scanning asset stock: %w", err)
		}
		levels[id] = l
	}
	return levels, rows.Err()
}

// applyAssetThresholds flags or clears each asset whose quantity crossed its
// threshold during the change, unless the change set its flag by hand.
func (s *Store) applyAssetThresholds(ic *inventoryChange, before, after map[string]stock) error {
	for _, id := range crossings(before, after, ic.manualAssets) {
		now := after[id]
		if err := s.setAssetRunningLowTx(ic.tx, id, now.below()); err != nil {
			return err
		}
		quantity, threshold := now.count, now.threshold
		kind := models.EventAssetRestocked
		if now.below() {
			kind = models.EventAssetRunningLow
		}
		if err := s.recordAssetEvent(ic.tx, kind, id, models.EventPayload{
			Trigger:   models.TriggerThreshold,
			Quantity:  &quantity,
			Threshold: &threshold,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) setAssetRunningLowTx(tx *sql.Tx, id string, low bool) error {
	_, err := s.TxExec(tx, "UPDATE assets SET is_running_low = ?, updated_at = ? WHERE id = ?", low, time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("setting running low: %w", err)
	}
	return nil
}

// readStockTx reads an active asset's stock within tx.
func (s *Store) readStockTx(tx *sql.Tx, id string) (*models.AssetStock, error) {
	var (
		sc       stockScan
		archived sql.NullTime
	)
	err := s.TxQueryRow(tx, "SELECT "+stockColumns+", archived_at FROM assets WHERE id = ?", id).Scan(append(sc.dest(), &archived)...)
	if err == sql.ErrNoRows {
		return nil, ErrAssetNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("reading asset stock: %w", err)
	}
	if archived.Valid {
		return nil, ErrAssetArchived
	}
	return sc.stock(), nil
}

// setStockTx replaces an asset's stock within tx, given what it was; a nil
// in stops counting it. A quantity that changes, or starts being counted,
// belongs in the asset's history: setStockTx returns the function that
// records it, or nil, for the caller to run once it has recorded the edit
// that changed it.
func (s *Store) setStockTx(tx *sql.Tx, id string, was *models.AssetStock, in *StockInput) (func() error, error) {
	if in == nil {
		_, err := s.TxExec(tx, `UPDATE assets SET stock_quantity = NULL, stock_unit = '', low_stock_threshold = NULL, is_running_low = ? WHERE id = ?`, false, id)
		if err != nil {
			return nil, fmt.Errorf("clearing stock: %w", err)
		}
		return nil, nil
	}
	quantity := 0
	if was != nil {
		quantity = was.Quantity
	}
	if in.Quantity != nil {
		quantity = *in.Quantity
	}
	_, err := s.TxExec(tx, `UPDATE assets SET stock_quantity = ?, stock_unit = ?, low_stock_threshold = ? WHERE id = ?`,
		quantity, in.Unit, in.LowStockThreshold, id)
	if err != nil {
		return nil, fmt.Errorf("setting stock: %w", err)
	}
	if was != nil && was.Quantity == quantity {
		return nil, nil
	}
	return func() error { return s.recordQuantityTx(tx, id, was, quantity) }, nil
}

// recordQuantityTx records a change of an asset's quantity in its history.
func (s *Store) recordQuantityTx(tx *sql.Tx, id string, was *models.AssetStock, quantity int) error {
	p := models.EventPayload{Trigger: models.TriggerManual, Quantity: &quantity}
	if was != nil {
		change := quantity - was.Quantity
		p.Change = &change
	}
	return s.recordAssetEvent(tx, models.EventQuantityChanged, id, p)
}

// AdjustQuantity changes an asset's quantity: by delta when set is nil (use
// is negative, restocking positive), or to *set. Using more than there is
// fails with ErrNotEnoughStock and changes nothing.
func (s *Store) AdjustQuantity(id string, delta int, set *int) (*models.AssetWithDetails, error) {
	err := s.withInventory(func(ic *inventoryChange) error {
		was, err := s.readStockTx(ic.tx, id)
		if err != nil {
			return err
		}
		if was == nil {
			return ErrNotStocked
		}
		quantity := was.Quantity
		switch {
		case set != nil:
			quantity = *set
		case delta < -was.Quantity:
			return fmt.Errorf("%w: %d left", ErrNotEnoughStock, was.Quantity)
		default:
			quantity += delta
		}
		if quantity < 0 || quantity > MaxQuantity {
			return ErrQuantityOutOfRange
		}
		if quantity == was.Quantity {
			return nil
		}
		if _, err := s.TxExec(ic.tx, "UPDATE assets SET stock_quantity = ?, updated_at = ? WHERE id = ?", quantity, time.Now().UTC(), id); err != nil {
			return fmt.Errorf("setting quantity: %w", err)
		}
		return s.recordQuantityTx(ic.tx, id, was, quantity)
	})
	if err != nil {
		return nil, err
	}
	return s.GetAssetByID(id)
}

// SetAssetRunningLow flags or clears an asset by hand. It reports whether
// the flag changed; setting it to what it already is records nothing.
func (s *Store) SetAssetRunningLow(id string, low bool) (*models.AssetWithDetails, bool, error) {
	changed := false
	err := s.withInventory(func(ic *inventoryChange) error {
		was, err := s.readStockTx(ic.tx, id)
		if err != nil {
			return err
		}
		if was == nil {
			return ErrNotStocked
		}
		ic.manualAssets[id] = true
		if was.IsRunningLow == low {
			return nil
		}
		if err := s.setAssetRunningLowTx(ic.tx, id, low); err != nil {
			return err
		}
		changed = true
		kind := models.EventAssetRestocked
		if low {
			kind = models.EventAssetRunningLow
		}
		quantity := was.Quantity
		return s.recordAssetEvent(ic.tx, kind, id, models.EventPayload{Trigger: models.TriggerManual, Quantity: &quantity})
	})
	if err != nil {
		return nil, false, err
	}
	a, err := s.GetAssetByID(id)
	return a, changed, err
}
