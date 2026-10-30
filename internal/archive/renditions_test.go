package archive

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/filestore"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// A category's label icon, and an asset's own, travel with their
// renditions: in a backup's folder and icons.json, restored as they were,
// and in a package, imported with the asset's own icon renamed as the
// category's is.
func TestLabelIconsTravelWithTheirRenditions(t *testing.T) {
	c := compiler(t)
	ctx := context.Background()
	src := setupStore(t)
	blobs := filestore.NewLocal(t.TempDir())
	storage := must[*models.Category](t)(src.CreateCategory("Storage", nil, "#123456", false, false))
	for rendition, s := range map[string][2]string{
		models.RenditionScreen: {"crate", `icon "crate" { category "storage"; footprint 1 1 1; part "box" { box 0 0 0 1 1 1 } }`},
		models.RenditionLabel:  {"crate-label", `icon "crate-label" { category "storage"; footprint 1 1 1; part "lid" { tile 0 0 1 1 1 } }`},
	} {
		if save, err := src.SaveCategoryIcon(ctx, c, storage.ID, rendition, s[0], s[1]); err != nil || !save.Published {
			t.Fatalf("%+v %v", save, err)
		}
	}
	bin := must[*models.AssetWithDetails](t)(src.CreateAsset("Bin", []string{storage.ID}, nil, nil))
	if _, err := src.UpdateAssetWithIcon(bin.Asset.ID, "", nil, nil, nil, nil, &store.IconUpdate{Name: "crate", Rendition: models.RenditionLabel}); err != nil {
		t.Fatal(err)
	}

	// a backup's folder has both, and icons.json says which is which
	b := backup(t, src, blobs, Options{Secrets: true})
	r := readArchive(t, b)
	if _, ok := r.members[IconsDir+"categories/crate-label.ilx"]; !ok {
		t.Error("the label icon is not in the folder")
	}
	var index IconsIndexFile
	if err := json.Unmarshal(r.members[IconsIndex], &index); err != nil {
		t.Fatal(err)
	}
	renditions := map[string]string{}
	for _, i := range index.Categories {
		renditions[i.Icon] = i.Rendition
	}
	if renditions["crate"] != models.RenditionScreen || renditions["crate-label"] != models.RenditionLabel {
		t.Errorf("icons.json's categories: %+v", index.Categories)
	}
	if len(index.Assets) != 1 || index.Assets[0].Icon != "crate" || index.Assets[0].Rendition != models.RenditionLabel {
		t.Errorf("icons.json's assets: %+v", index.Assets)
	}

	// restored as they were
	dst := emptyStore(t, envDialect())
	if _, err := Restore(ctx, dst, filestore.NewLocal(t.TempDir()), opener(b), RestoreOptions{Secrets: true}); err != nil {
		t.Fatal(err)
	}
	if got, err := dst.GetCategoryIcon(storage.ID, models.RenditionLabel); err != nil || got.IconName != "crate-label" || got.Source == nil {
		t.Errorf("the label icon restored: %+v %v", got, err)
	}
	if got := idOf(t, dst, "SELECT label_icon_name FROM assets WHERE id = ?", bin.Asset.ID); got != "crate" {
		t.Errorf("the asset's own label icon restored: %q", got)
	}

	// imported where crate is another's: copied, the asset's own label
	// icon following the rename
	pkg := exportPackage(t, src, blobs, Selection{Subtrees: []string{storage.ID}, Assets: &AssetsSelection{}})
	into := setupStore(t)
	bins := must[*models.Category](t)(into.CreateCategory("Bins", nil, "#654321", false, false))
	if save, err := into.SaveCategoryIcon(ctx, c, bins.ID, "", "crate", `icon "crate" { category "storage"; footprint 2 2 2; part "box" { box 0 0 0 2 2 2 } }`); err != nil || !save.Published {
		t.Fatalf("%+v %v", save, err)
	}
	plan, err := Import(ctx, into, filestore.NewLocal(t.TempDir()), opener(pkg), ImportOptions{Icons: c})
	if err != nil {
		t.Fatalf("%v: %+v", err, plan)
	}
	if it := itemOf(t, plan, ItemCategoryIcon, "crate"); it.As != "crate-2" {
		t.Errorf("the screen icon: %+v", it)
	}
	if it := itemOf(t, plan, ItemCategoryIcon, "crate-label"); it.ID != storage.ID+"/label" {
		t.Errorf("the label icon: %+v", it)
	}
	imported := idOf(t, into, "SELECT id FROM categories WHERE name = 'Storage'")
	if got, err := into.GetCategoryIcon(imported, models.RenditionLabel); err != nil || got.IconName != "crate-label" {
		t.Errorf("the label icon imported: %+v %v", got, err)
	}
	if got, err := into.GetCategoryIcon(imported, ""); err != nil || got.IconName != "crate-2" {
		t.Errorf("the screen icon imported: %+v %v", got, err)
	}
	if got := idOf(t, into, "SELECT label_icon_name FROM assets WHERE name = 'Bin'"); got != "crate-2" {
		t.Errorf("the asset's own label icon imported: %q", got)
	}
}

// A row from an archive made before renditions has none, and is the
// screen's; a screen icon's item is its category's ID, as before.
func TestRowsBeforeRenditionsAreTheScreens(t *testing.T) {
	if got := rowRendition(map[string]any{"category_id": "c1"}); got != models.RenditionScreen {
		t.Errorf("no rendition: %q", got)
	}
	if got := rowRendition(map[string]any{"category_id": "c1", "rendition": "label"}); got != models.RenditionLabel {
		t.Errorf("label: %q", got)
	}
	if iconItemID("c1", "") != "c1" || iconItemID("c1", models.RenditionScreen) != "c1" || iconItemID("c1", models.RenditionLabel) != "c1/label" {
		t.Error("item IDs")
	}
}
