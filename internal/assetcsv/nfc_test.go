package assetcsv

import (
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/generators"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// An NFC tag's serial number goes out and comes back as any code does, and
// comes in however it is written.
func TestNFCTagsImportAndExport(t *testing.T) {
	w := newWorld(t)
	if _, err := w.st.CreateAsset("Lathe", []string{w.tools}, []models.AssetGeneratorKey{
		{GeneratorType: generators.TypeCode128, AssignedCode: "L1"},
		{GeneratorType: generators.TypeNFC, AssignedCode: "04:a2:3b:1c:5d:80:00"},
	}, nil); err != nil {
		t.Fatal(err)
	}

	before := mustList(t, w.st)
	cats, _ := w.st.ListAllCategories()
	var b strings.Builder
	if err := Export(&b, before, NewCategories(cats), generators.CodeTypes(), NewAssetRefs(before)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "code.nfc") || !strings.Contains(b.String(), "04:A2:3B:1C:5D:80:00") {
		t.Fatalf("export leaves the tag out:\n%s", b.String())
	}
	if plan := w.prepare(t, b.String(), nil); plan.ProblemCount != 0 || plan.Unchanged != len(before) {
		t.Fatalf("round trip %+v", plan)
	}

	// Written as a phone or a tag app shows it, it is the same tag.
	plan := w.prepare(t, "name,code.nfc\nSaw,04a23b1c5d8000\nGlue,not a tag\n", nil)
	if !hasProblem(plan, 2, "code.nfc", `is also assigned to asset "Lathe"`) || !hasProblem(plan, 3, "code.nfc", "serial number") {
		t.Errorf("%+v", plan.Problems)
	}

	w.apply(t, "name,code.nfc\nSaw,de ad be ef\n")
	saw, err := w.st.ListAssets("", "Saw")
	if err != nil || len(saw) != 1 || len(saw[0].GeneratorKeys) != 1 || saw[0].GeneratorKeys[0].AssignedCode != "DE:AD:BE:EF" {
		t.Errorf("saw %+v %v", saw, err)
	}
}
