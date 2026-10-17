package store

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers"
)

// TestCategoriesNameLabelTemplates checks a category's label template
// passes down to its subcategories' assets, ties between categories as
// close are conflicts, and a template categories name cannot be deleted.
func TestCategoriesNameLabelTemplates(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	small, err := st.CreateTemplate(sampleTemplate(""))
	if err != nil {
		t.Fatal(err)
	}
	large, err := st.CreateTemplate(sampleTemplate(""))
	if err != nil {
		t.Fatal(err)
	}
	bins, _ := st.CreateCategory("Bins", nil, "", false, false)
	small1, _ := st.CreateCategory("Small bins", &bins.ID, "", false, false)
	tools, _ := st.CreateCategory("Tools", nil, "", false, false)
	if _, err := st.SetCategoryLabelTemplate(bins.ID, &small.ID); err != nil {
		t.Fatal(err)
	}
	if c, err := st.SetCategoryLabelTemplate(tools.ID, &large.ID); err != nil || c.LabelTemplateID == nil || *c.LabelTemplateID != large.ID {
		t.Fatalf("setting a label template: %+v, %v", c, err)
	}
	missing := "no-such-template"
	if _, err := st.SetCategoryLabelTemplate(tools.ID, &missing); !errors.Is(err, ErrTemplateNotFound) {
		t.Errorf("naming a missing template: %v", err)
	}

	bin, _ := st.CreateAsset("Bin A1", []string{small1.ID}, nil, nil)
	if !reflect.DeepEqual(bin.LabelTemplate, models.TemplateChoice{TemplateID: small.ID, SourceCategoryID: bins.ID}) {
		t.Errorf("inherited label template: %+v", bin.LabelTemplate)
	}
	plain, _ := st.CreateCategory("Plain", nil, "", false, false)
	if a, _ := st.CreateAsset("Rag", []string{plain.ID}, nil, nil); a.LabelTemplate.TemplateID != "" {
		t.Errorf("an asset no category names a template for has %+v", a.LabelTemplate)
	}
	both, _ := st.CreateAsset("Toolbox", []string{bins.ID, tools.ID}, nil, nil)
	if !both.LabelTemplate.Conflict {
		t.Errorf("two categories as close naming different templates: %+v", both.LabelTemplate)
	}
	conflicts, err := st.LabelConflicts("", nil)
	if err != nil || conflicts[bins.ID] != 1 || conflicts[tools.ID] != 1 {
		t.Errorf("label conflicts: %v, %v", conflicts, err)
	}
	// What naming the same template for tools would do: no conflict.
	if after, err := st.LabelConflicts(tools.ID, &small.ID); err != nil || len(after) != 0 {
		t.Errorf("conflicts after settling: %v, %v", after, err)
	}
	// Scan template conflicts are a separate matter.
	if scan, _ := st.ScanConflicts("", nil); len(scan) != 0 {
		t.Errorf("scan conflicts from label templates: %v", scan)
	}

	if err := st.DeleteTemplate(large.ID); !errors.Is(err, ErrTemplateInUse) {
		t.Errorf("deleting a template a category names: %v", err)
	}
	if _, err := st.SetCategoryLabelTemplate(tools.ID, nil); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteTemplate(large.ID); err != nil {
		t.Errorf("deleting a template no category names: %v", err)
	}
}

func TestSpoolLifecycle(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	user, err := st.CreateUser("ada@test.org", "Secret123!", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetUserName(user.ID, "Ada"); err != nil {
		t.Fatal(err)
	}
	ada := st.As(user.ID)
	tmpl, _ := st.CreateTemplate(sampleTemplate(""))
	other, _ := st.CreateTemplate(sampleTemplate(""))
	front, err := st.CreatePrinter(models.Printer{Name: "Front desk", Driver: "brother_ql", Model: "QL-810W", Connection: printers.Connection{Type: printers.ConnTCP, Address: "10.0.0.9"}, Media: "dk-22205"})
	if err != nil {
		t.Fatal(err)
	}
	saw, _ := st.CreateAsset("Saw", nil, nil, nil)
	drill, _ := st.CreateAsset("Drill", nil, nil, nil)
	toFront := models.DestinationPrinterPrefix + front.ID

	added, already, err := ada.AddToSpool([]SpoolAdd{
		{AssetID: saw.Asset.ID, TemplateID: &tmpl.ID, Destination: toFront, Copies: 2},
		{AssetID: saw.Asset.ID, TemplateID: &tmpl.ID, Copies: 1},      // the same label twice in one go
		{AssetID: saw.Asset.ID, TemplateID: &other.ID, Copies: 1},     // another template: another label
		{AssetID: drill.Asset.ID, Destination: "download", Copies: 1}, // no template yet
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 3 || !reflect.DeepEqual(already, []string{saw.Asset.ID}) {
		t.Fatalf("added %d, already %v", len(added), already)
	}
	first := added[0]
	if first.AssetName != "Saw" || *first.TemplateID != tmpl.ID || first.Destination != toFront || first.Copies != 2 ||
		first.AddedBy.ID != user.ID || first.AddedBy.Name != "Ada" || first.AddedBy.Token || first.PrintedAt != nil {
		t.Errorf("entry as added: %+v", first)
	}
	// Adding the same again adds nothing.
	if added, already, err := ada.AddToSpool([]SpoolAdd{{AssetID: drill.Asset.ID, Copies: 1}}); err != nil || len(added) != 0 || len(already) != 1 {
		t.Errorf("adding a waiting label again: %d added, %v, %v", len(added), already, err)
	}

	for name, add := range map[string]SpoolAdd{
		"no copies":         {AssetID: saw.Asset.ID, Copies: 0},
		"too many copies":   {AssetID: saw.Asset.ID, Copies: MaxSpoolCopies + 1},
		"a bad destination": {AssetID: saw.Asset.ID, Destination: "fax", Copies: 1},
		"a missing printer": {AssetID: saw.Asset.ID, Destination: "printer:nope", Copies: 1},
		"a missing asset":   {AssetID: "nope", Copies: 1},
	} {
		if _, _, err := st.AddToSpool([]SpoolAdd{add}); err == nil {
			t.Errorf("%s: added", name)
		}
	}
	missing := "nope"
	if _, _, err := st.AddToSpool([]SpoolAdd{{AssetID: saw.Asset.ID, TemplateID: &missing, Copies: 1}}); !errors.Is(err, ErrTemplateNotFound) {
		t.Errorf("a missing template: %v", err)
	}

	waiting, err := st.ListSpool(SpoolFilter{})
	if err != nil || len(waiting) != 3 {
		t.Fatalf("waiting: %d, %v", len(waiting), err)
	}
	for i := range waiting {
		if waiting[i].ID != added[i].ID {
			t.Errorf("waiting entry %d is %s, added %s: not in the order added", i, waiting[i].AssetName, added[i].AssetName)
		}
	}
	if len(waiting[0].AssetScopes) == 0 {
		t.Errorf("entries carry their assets' scopes: %+v", waiting[0])
	}
	if mine, _ := st.ListSpool(SpoolFilter{AddedBy: "someone else"}); len(mine) != 0 {
		t.Errorf("another's entries: %d", len(mine))
	}

	// Bulk changes, all or none, clearing problems.
	drillEntry := added[2]
	if err := st.SetSpoolProblems(map[string]string{drillEntry.ID: "Could not be produced"}); err != nil {
		t.Fatal(err)
	}
	copies := 4
	if err := st.UpdateSpool([]string{drillEntry.ID, added[1].ID}, SpoolUpdate{SetTemplate: true, TemplateID: &tmpl.ID, SetDestination: true, Destination: toFront, Copies: &copies}); err != nil {
		t.Fatal(err)
	}
	got, _ := st.ListSpool(SpoolFilter{IDs: []string{drillEntry.ID}})
	if len(got) != 1 || *got[0].TemplateID != tmpl.ID || got[0].Destination != toFront || got[0].Copies != 4 || got[0].Problem != "" {
		t.Errorf("after the update: %+v", got)
	}
	if err := st.UpdateSpool([]string{drillEntry.ID, "nope"}, SpoolUpdate{Copies: &copies}); !errors.Is(err, ErrSpoolEntryNotFound) {
		t.Errorf("updating a missing entry: %v", err)
	}
	if err := st.UpdateSpool([]string{drillEntry.ID}, SpoolUpdate{SetTemplate: true}); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.ListSpool(SpoolFilter{IDs: []string{drillEntry.ID}}); got[0].TemplateID != nil {
		t.Errorf("clearing the template: %+v", got[0])
	}

	// Printing moves entries to the Printed list and into history.
	if err := ada.MarkSpoolPrinted([]string{first.ID}, "Shelf label", "Front desk"); err != nil {
		t.Fatal(err)
	}
	printed, _ := st.ListSpool(SpoolFilter{Printed: true})
	if len(printed) != 1 || printed[0].PrintedAt == nil || printed[0].PrintedBy.Name != "Ada" || printed[0].PrintedTemplate != "Shelf label" || printed[0].PrintedOn != "Front desk" {
		t.Fatalf("printed: %+v", printed)
	}
	if recent, _ := st.ListSpool(SpoolFilter{Printed: true, PrintedSince: time.Now().Add(time.Hour)}); len(recent) != 0 {
		t.Errorf("printed since an hour from now: %d", len(recent))
	}
	if err := st.MarkSpoolPrinted([]string{first.ID}, "Shelf label", ""); !errors.Is(err, ErrSpoolEntryNotFound) {
		t.Errorf("printing a printed entry again: %v", err)
	}
	if err := st.RemoveFromSpool([]string{first.ID}); !errors.Is(err, ErrSpoolEntryNotFound) {
		t.Errorf("removing a printed entry: %v", err)
	}
	if err := ada.MarkSpoolPrinted([]string{added[1].ID}, "Shelf label", ""); err != nil {
		t.Fatal(err)
	}
	events, _, err := st.ListAssetEvents(saw.Asset.ID, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	var labels []models.EventLabel
	for _, e := range events {
		if e.Kind == models.EventAssetLabelPrinted {
			if e.Payload.Actor == nil || e.Payload.Actor.ID != user.ID {
				t.Errorf("label printed by %+v", e.Payload.Actor)
			}
			labels = append(labels, *e.Payload.Label)
		}
	}
	want := []models.EventLabel{{Template: "Shelf label", Download: true, Copies: 4}, {Template: "Shelf label", Printer: "Front desk", Copies: 2}}
	if !reflect.DeepEqual(labels, want) {
		t.Errorf("history: %+v", labels)
	}

	// Printing outside the spool is recorded too.
	if err := st.RecordLabelsPrinted([]string{drill.Asset.ID}, "Shelf label", "Front desk", 3); err != nil {
		t.Fatal(err)
	}
	events, _, _ = st.ListAssetEvents(drill.Asset.ID, "", 1)
	if len(events) != 1 || events[0].Kind != models.EventAssetLabelPrinted || events[0].Payload.Label.Copies != 3 {
		t.Errorf("a direct print's history: %+v", events)
	}

	// A deleted printer or template leaves its entries without.
	if err := st.UpdateSpool([]string{drillEntry.ID}, SpoolUpdate{SetTemplate: true, TemplateID: &other.ID}); err != nil {
		t.Fatal(err)
	}
	if err := st.DeletePrinter(front.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteTemplate(other.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.ListSpool(SpoolFilter{IDs: []string{drillEntry.ID}}); got[0].Destination != "" || got[0].TemplateID != nil {
		t.Errorf("after deleting its printer and template: %+v", got[0])
	}
	if err := st.RemoveFromSpool([]string{drillEntry.ID}); err != nil {
		t.Fatal(err)
	}
	if left, _ := st.ListSpool(SpoolFilter{}); len(left) != 0 {
		t.Errorf("left waiting: %+v", left)
	}
}

// TestSpoolIsBounded lowers the bound and checks an add over it adds
// nothing.
func TestSpoolIsBounded(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	saved := MaxSpoolWaiting
	t.Cleanup(func() { MaxSpoolWaiting = saved })
	MaxSpoolWaiting = 2
	var adds []SpoolAdd
	for _, name := range []string{"A", "B", "C"} {
		a, _ := st.CreateAsset(name, nil, nil, nil)
		adds = append(adds, SpoolAdd{AssetID: a.Asset.ID, Copies: 1})
	}
	if _, _, err := st.AddToSpool(adds); !errors.Is(err, ErrSpoolFull) {
		t.Errorf("three into two: %v", err)
	}
	if waiting, _ := st.ListSpool(SpoolFilter{}); len(waiting) != 0 {
		t.Errorf("a full spool took %d", len(waiting))
	}
	if added, _, err := st.AddToSpool(adds[:2]); err != nil || len(added) != 2 {
		t.Errorf("two into two: %d, %v", len(added), err)
	}
}

// TestCategoryAssetIDs lists a category's active assets, with its
// subcategories' when asked.
func TestCategoryAssetIDs(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	bins, _ := st.CreateCategory("Bins", nil, "", false, false)
	small, _ := st.CreateCategory("Small", &bins.ID, "", false, false)
	tiny, _ := st.CreateCategory("Tiny", &small.ID, "", false, false)
	a, _ := st.CreateAsset("A", []string{bins.ID}, nil, nil)
	b, _ := st.CreateAsset("B", []string{tiny.ID}, nil, nil)
	gone, _ := st.CreateAsset("Gone", []string{bins.ID}, nil, nil)
	if err := st.DeleteAsset(gone.Asset.ID); err != nil {
		t.Fatal(err)
	}
	if ids, err := st.CategoryAssetIDs(bins.ID, false); err != nil || !reflect.DeepEqual(ids, []string{a.Asset.ID}) {
		t.Errorf("bins alone: %v, %v", ids, err)
	}
	if ids, err := st.CategoryAssetIDs(bins.ID, true); err != nil || len(ids) != 2 || !(ids[0] == b.Asset.ID || ids[1] == b.Asset.ID) {
		t.Errorf("bins and below: %v, %v", ids, err)
	}
	if _, err := st.CategoryAssetIDs("nope", true); !errors.Is(err, ErrCategoryNotFound) {
		t.Errorf("a missing category: %v", err)
	}
	if got, err := st.AssetsByIDs([]string{b.Asset.ID, "nope", a.Asset.ID}); err != nil || len(got) != 2 || got[0].Asset.ID != b.Asset.ID {
		t.Errorf("assets by ID: %d, %v", len(got), err)
	}
}

// TestTokensAddToTheSpool checks an entry added by an API token names it.
func TestTokensAddToTheSpool(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	admin, _ := st.CreateAdminUser("admin@test.org", "AdminSecret123!")
	token, _, err := st.CreateToken("Intake script", nil, nil, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := st.CreateAsset("Saw", nil, nil, nil)
	added, _, err := st.AsToken(token.ID).AddToSpool([]SpoolAdd{{AssetID: a.Asset.ID, Copies: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if by := added[0].AddedBy; by.ID != token.ID || by.Name != "Intake script" || !by.Token {
		t.Errorf("added by %+v", by)
	}
}
