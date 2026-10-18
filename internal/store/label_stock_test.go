package store

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers"
)

func TestLabelStock(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	bins, err := st.CreateLabelStock(printers.Stock{Name: " Bin labels ", WidthMM: 60, LengthMM: 40, GapMM: 3})
	if err != nil {
		t.Fatal(err)
	}
	if bins.Name != "Bin labels" || bins.WidthMM != 60 || bins.LengthMM != 40 || bins.GapMM != 3 || len(bins.Printers) != 0 || bins.ID == "" {
		t.Errorf("created: %+v", bins)
	}
	if _, err := st.CreateLabelStock(printers.Stock{Name: "Bin labels", WidthMM: 50}); !errors.Is(err, ErrLabelStockNameExists) {
		t.Errorf("a taken name: %v", err)
	}
	if _, err := st.CreateLabelStock(printers.Stock{Name: "Tape", WidthMM: 50, GapMM: 2}); !errors.Is(err, printers.ErrInvalidPrinter) {
		t.Errorf("a continuous roll with a gap: %v", err)
	}

	// A Rollo takes it; its printer says what it has loaded.
	rollo, err := st.CreatePrinter(models.Printer{Name: "Rollo", Driver: "tspl", Model: "Rollo X1038",
		Connection: printers.Connection{Type: printers.ConnTCP, Address: "10.0.0.7"}, Media: bins.ID})
	if err != nil {
		t.Fatal(err)
	}
	want := &printers.MediaInfo{ID: bins.ID, Name: "Bin labels", WidthMM: 60, LengthMM: 40, DieCut: true, Custom: true}
	if rollo.Stock == nil || rollo.Stock.GapMM != 3 || !reflect.DeepEqual(rollo.Loaded, want) {
		t.Errorf("created printer: stock %+v, loaded %+v", rollo.Stock, rollo.Loaded)
	}
	got, err := st.GetPrinter(rollo.ID)
	if err != nil || got.Stock == nil || *got.Stock != bins.Stock || !reflect.DeepEqual(got.Loaded, want) {
		t.Errorf("read printer: %+v, %v", got, err)
	}
	list, _ := st.ListPrinters()
	if len(list) != 1 || list[0].Stock == nil || list[0].Stock.ID != bins.ID {
		t.Errorf("listed printers: %+v", list)
	}
	// A built-in roll is described as well.
	ql := newPrinter(t, st, "QL", printers.ConnTCP)
	if ql.Stock != nil || ql.Loaded == nil || ql.Loaded.Name != "DK-22205 62 mm continuous" || ql.Loaded.Custom {
		t.Errorf("a built-in roll: %+v, %+v", ql.Stock, ql.Loaded)
	}
	// A Brother printer takes only Brother's rolls.
	ql.Media = bins.ID
	if _, err := st.UpdatePrinter(*ql); !errors.Is(err, printers.ErrInvalidPrinter) || !strings.Contains(err.Error(), "own rolls") {
		t.Errorf("custom stock on a Brother: %v", err)
	}

	listed, _ := st.ListLabelStock()
	if len(listed) != 1 || !reflect.DeepEqual(listed[0].Printers, []string{"Rollo"}) {
		t.Errorf("listed stock: %+v", listed)
	}

	// Changing the stock checks the printers that have it loaded.
	if _, err := st.UpdateLabelStock(printers.Stock{ID: bins.ID, Name: "Bin labels", WidthMM: 150, LengthMM: 40, GapMM: 3}); !errors.Is(err, printers.ErrInvalidPrinter) || !strings.Contains(err.Error(), "Rollo") {
		t.Errorf("too wide for the Rollo: %v", err)
	}
	if again, _ := st.GetLabelStock(bins.ID); again.WidthMM != 60 {
		t.Errorf("a refused change was kept: %+v", again)
	}
	updated, err := st.UpdateLabelStock(printers.Stock{ID: bins.ID, Name: "Shelf labels", WidthMM: 80})
	if err != nil || updated.Name != "Shelf labels" || updated.DieCut() || updated.GapMM != 0 {
		t.Errorf("updated: %+v, %v", updated, err)
	}
	if p, _ := st.GetPrinter(rollo.ID); p.Loaded == nil || p.Loaded.Name != "Shelf labels" || p.Loaded.DieCut {
		t.Errorf("the printer after the change: %+v", p.Loaded)
	}
	if _, err := st.UpdateLabelStock(printers.Stock{ID: "nope", Name: "X", WidthMM: 50}); !errors.Is(err, ErrLabelStockNotFound) {
		t.Errorf("updating a missing: %v", err)
	}

	// Loaded stock stays until no printer has it.
	if err := st.DeleteLabelStock(bins.ID); !errors.Is(err, ErrLabelStockInUse) || !strings.Contains(err.Error(), "Rollo") {
		t.Errorf("deleting loaded stock: %v", err)
	}
	rollo.Media = "4x6"
	if _, err := st.UpdatePrinter(*rollo); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteLabelStock(bins.ID); err != nil {
		t.Errorf("deleting: %v", err)
	}
	if err := st.DeleteLabelStock(bins.ID); !errors.Is(err, ErrLabelStockNotFound) {
		t.Errorf("deleting again: %v", err)
	}
}
