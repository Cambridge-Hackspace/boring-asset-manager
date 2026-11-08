package assetcsv

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

func catalogImport() CatalogImport {
	hundred := 100
	return CatalogImport{
		VendorID: "v1",
		Entries: map[string]models.CatalogEntry{
			"e1": {ID: "e1", VendorID: "v1", CategoryID: "screws", LinkedName: "Screws", PartNumber: "SC-100", PackSize: &hundred, PackUnit: "screws",
				Price: "4.50", Currency: "GBP", Preferred: true},
		},
		Categories: NewCategories([]models.Category{{ID: "screws", Name: "Screws"}, {ID: "glue", Name: "Glue"}}),
		Assets:     NewAssetRefs([]models.AssetWithDetails{{Asset: models.Asset{ID: "a1", Name: "Solder"}}, {Asset: models.Asset{ID: "a2", Name: "Twin"}}, {Asset: models.Asset{ID: "a3", Name: "Twin"}}}),
		Prices:     true,
	}
}

// TestCatalogCSVReadsBackAsWritten: an exported catalog plans no change,
// and a formula-like cell reads back as written.
func TestCatalogCSVReadsBackAsWritten(t *testing.T) {
	in := catalogImport()
	e := in.Entries["e1"]
	e.Description = "=SUM(A1)"
	in.Entries["e1"] = e
	var b bytes.Buffer
	if err := ExportCatalog(&b, []models.CatalogEntry{e}, in.Categories, in.Assets); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), ",'=SUM(A1),") {
		t.Errorf("not defused: %q", b.String())
	}
	plan, err := PlanCatalog(b.Bytes(), in)
	if err != nil || plan.Unchanged != 1 || plan.ProblemCount != 0 || len(plan.Writes) != 0 {
		t.Errorf("read back: %+v %v", plan, err)
	}
}

// TestCatalogCSVProblems: each problem is found, on its line and column,
// and a file with any writes nothing.
func TestCatalogCSVProblems(t *testing.T) {
	for _, c := range []struct{ file, want string }{
		{"part_number,colour\nX,red\n", `"colour" is not a catalog column`},
		{"part_number\nX\n", "needs a category or an asset column"},
		{"id,category,part_number\nnope,Screws,X\n", `no entry of this vendor has the ID "nope"`},
		{"id,part_number\ne1,A\ne1,B\n", "also on line 2"},
		{"category,part_number\nNowhere,X\n", `unknown category "Nowhere"`},
		{"asset,part_number\nTwin,X\n", "2 assets are \"Twin\""},
		{"asset,part_number\nNobody,X\n", `no asset is "Nobody"`},
		{"category,asset,part_number\nGlue,Solder,X\n", "not both"},
		{"category,part_number,pack_size\nGlue,X,many\n", "whole number"},
		{"category,part_number,preferred\nGlue,X,maybe\n", "preferred is yes"},
		{"category,part_number,preferred\nGlue,X,yes\nGlue,Y,yes\n", "line 2 prefers"},
		{"category,description\nGlue,\n", "a part number or a description"},
		{"category,part_number,price\nGlue,X,4\n", "needs its currency"},
	} {
		plan, err := PlanCatalog([]byte(c.file), catalogImport())
		if err != nil {
			t.Fatalf("%q: %v", c.file, err)
		}
		found := false
		for _, p := range plan.Problems {
			found = found || strings.Contains(p.Message, c.want)
		}
		if !found || len(plan.Writes) != 0 {
			t.Errorf("%q: %+v, want %q", c.file, plan.Problems, c.want)
		}
	}
}

// TestCatalogCSVKeepsWhatItLeavesOut: a column the file leaves out keeps
// what an entry has, and an importer who may not see prices keeps them,
// and may not give one.
func TestCatalogCSVKeepsWhatItLeavesOut(t *testing.T) {
	plan, err := PlanCatalog([]byte("id,part_number\ne1,SC-101\n"), catalogImport())
	if err != nil || plan.Update != 1 || len(plan.Writes) != 1 {
		t.Fatalf("planned: %+v %v", plan, err)
	}
	w := plan.Writes[0]
	if w.ID != "e1" || w.In.PartNumber != "SC-101" || w.In.CategoryID != "screws" || w.In.Price != "4.50" || *w.In.PackSize != 100 || !w.In.Preferred {
		t.Errorf("written: %+v", w)
	}
	in := catalogImport()
	in.Prices = false
	plan, err = PlanCatalog([]byte("id,part_number,price,currency\ne1,SC-101,,\n"), in)
	if err != nil || plan.ProblemCount != 0 || len(plan.Writes) != 1 || plan.Writes[0].In.Price != "4.50" || plan.Writes[0].In.Currency != "GBP" {
		t.Errorf("without prices: %+v %v", plan, err)
	}
	plan, _ = PlanCatalog([]byte("id,price,currency\ne1,5,GBP\n"), in)
	if plan.ProblemCount != 2 || !strings.Contains(plan.Problems[0].Message, "purchases.read") {
		t.Errorf("giving a price without purchases.read: %+v", plan)
	}
	// A new row of an asset, by name.
	plan, err = PlanCatalog([]byte("asset,part_number,preferred\nSolder,S-1,yes\n"), catalogImport())
	if err != nil || plan.Create != 1 || plan.Writes[0].In.AssetID != "a1" || plan.Writes[0].In.VendorID != "v1" || !plan.Writes[0].In.Preferred {
		t.Errorf("an asset's: %+v %v", plan, err)
	}
}
