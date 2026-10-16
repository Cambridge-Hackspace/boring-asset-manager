package store

import (
	"errors"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

func TestCategoriesTotalWeights(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	// Materials counts in kg, so the grams below add nothing to it.
	materials, err := st.CreateCategoryFrom(CategoryInput{Name: "Materials", IsConsumable: true, Unit: "kg", LowStockThreshold: intp(1)})
	if err != nil {
		t.Fatal(err)
	}
	filament, err := st.CreateCategoryFrom(CategoryInput{Name: "Filament", ParentID: &materials.ID, IsConsumable: true, Unit: " g ", LowStockThreshold: intp(1000)})
	if err != nil || filament.Unit != "g" {
		t.Fatalf("created %+v, %v", filament, err)
	}
	pla, err := st.CreateCategoryFrom(CategoryInput{Name: "PLA", ParentID: &filament.ID})
	if err != nil {
		t.Fatal(err)
	}
	spool := func(name string, grams int, unit string) string {
		t.Helper()
		a, err := st.CreateAssetWithStock(name, []string{pla.ID}, nil, nil, &StockInput{Quantity: intp(grams), Unit: unit})
		if err != nil {
			t.Fatal(err)
		}
		return a.Asset.ID
	}
	blue, red := spool("Blue PLA", 1000, "g"), spool("Red PLA", 2000, "g")
	spool("Odd spool", 5, "kg")
	asset(t, st, "Unweighed spool", pla.ID)

	total := func(c *models.Category) int {
		t.Helper()
		n, err := st.CategoryTotal(c.ID, c.Unit)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if got := total(filament); got != 3000 {
		t.Errorf("filament totals %d g", got)
	}
	if got := total(materials); got != 5 {
		t.Errorf("materials total %d kg", got)
	}
	if n, _ := st.ActiveCount(filament.ID); n != 4 {
		t.Errorf("filament counts %d items", n)
	}
	if flag(t, st, filament.ID) {
		t.Fatal("flagged at 3000 g, above 1000 g")
	}

	// Using a spool lowers the total; crossing the threshold flags it.
	adjust(t, st, red, -2000)
	if flag(t, st, filament.ID) {
		t.Error("flagged at exactly the threshold")
	}
	adjust(t, st, blue, -1)
	if !flag(t, st, filament.ID) {
		t.Error("not flagged below the threshold")
	}
	list := events(t, st)
	e := list[len(list)-1]
	if e.Kind != models.EventRunningLow || *e.CategoryID != filament.ID || *e.Payload.ActiveCount != 999 || e.Payload.Unit != "g" {
		t.Errorf("running low: %+v", e.Payload)
	}

	// Only consumable categories total in a unit, and units are short.
	for what, in := range map[string]CategoryInput{
		"not consumable": {Name: "Tools", Unit: "g"},
		"a long unit":    {Name: "Tape", IsConsumable: true, Unit: strings.Repeat("m", MaxStockUnit+1)},
	} {
		if _, err := st.CreateCategoryFrom(in); err == nil {
			t.Errorf("%s was accepted", what)
		}
	}
	if _, err := st.CreateCategoryFrom(CategoryInput{Name: "Tools", Unit: "g"}); !errors.Is(err, ErrUnitRequiresConsumable) {
		t.Errorf("a unit on a category that is not consumable: %v", err)
	}
}

func TestLendingAWeighedSpool(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	filament, err := st.CreateCategoryFrom(CategoryInput{Name: "Filament", IsConsumable: true, IsLendable: true, Unit: "g", LowStockThreshold: intp(500)})
	if err != nil {
		t.Fatal(err)
	}
	bo, err := st.CreateNamedUser("bo@hackspace.org", "Bo", "a long enough password", true)
	if err != nil {
		t.Fatal(err)
	}
	a, err := st.CreateAssetWithStock("Blue PLA", []string{filament.ID}, nil, nil, &StockInput{Quantity: intp(1000), Unit: "g"})
	if err != nil {
		t.Fatal(err)
	}
	spool := a.Asset.ID
	// Grams of something not weighed in grams is a pooled quantity.
	pooled, err := st.CreateAssetWithStock("Loose pellets", []string{filament.ID}, nil, nil, &StockInput{Quantity: intp(1000), Unit: "pcs"})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := st.GetAssetByID(spool); !got.Weighed {
		t.Error("the spool is not weighed")
	}
	if got, _ := st.GetAssetByID(pooled.Asset.ID); got.Weighed {
		t.Error("pellets counted in pieces are weighed")
	}
	if _, err := st.CheckOut(pooled.Asset.ID, bo.ID, nil); !errors.Is(err, ErrLentByQuantity) {
		t.Errorf("lending part of a pooled quantity: %v", err)
	}

	if _, err := st.CheckOut(spool, bo.ID, nil); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []int{-1, MaxQuantity + 1} {
		if _, err := st.CheckIn(spool, "", intp(bad)); !errors.Is(err, ErrQuantityOutOfRange) {
			t.Errorf("%d left: %v", bad, err)
		}
	}
	// A refused check-in leaves it lent.
	if got, _ := st.GetAssetByID(spool); got.Loan == nil {
		t.Fatal("a refused check-in ended the loan")
	}
	got, err := st.As(bo.ID).CheckIn(spool, bo.ID, intp(300))
	if err != nil || got.Loan != nil || got.Asset.Stock.Quantity != 300 {
		t.Fatalf("checked in %+v, %v", got, err)
	}
	h := history(t, st, spool)
	if h[0].Kind != models.EventQuantityChanged || *h[0].Payload.Change != -700 || h[1].Kind != models.EventAssetCheckedIn {
		t.Errorf("history: %s", kinds(h))
	}
	if !flag(t, st, filament.ID) {
		t.Error("coming back with 300 g left did not flag the category, below 500 g")
	}
	// Checking in what is not counted is refused.
	drill := asset(t, st, "Drill", filament.ID)
	if _, err := st.CheckOut(drill, bo.ID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CheckIn(drill, "", intp(1)); !errors.Is(err, ErrNotStocked) {
		t.Errorf("an amount for an asset that has none: %v", err)
	}
}
