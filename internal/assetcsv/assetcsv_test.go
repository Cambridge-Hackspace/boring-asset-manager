package assetcsv

import (
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

func cat(id, name, parent string) models.Category {
	c := models.Category{ID: id, Name: name}
	if parent != "" {
		c.ParentID = &parent
	}
	return c
}

var tree = []models.Category{
	cat("uncategorized", "Uncategorized", ""),
	cat("tools", "Tools", ""),
	cat("drills", "Drills", "tools"),
	cat("bits", "Bits", "drills"),
	// Two "Spare" categories under Tools: their path is ambiguous.
	cat("spare1", "Spare", "tools"),
	cat("spare2", "Spare", "tools"),
	cat("odd", "Nuts; bolts", ""),
	cat("arrow", "A>B", ""),
	cat("padded", " Padded", ""),
	// Named like another category's ID.
	cat("tricky", "drills", ""),
}

func TestCategoryRefs(t *testing.T) {
	c := NewCategories(tree)
	for id, want := range map[string]string{
		"tools":  "Tools",
		"drills": "Tools > Drills",
		"bits":   "Tools > Drills > Bits",
		"spare1": "spare1",
		"odd":    "odd",
		"arrow":  "arrow",
		"padded": "padded",
		"tricky": "tricky",
		"gone":   "gone",
	} {
		if got := c.Ref(id); got != want {
			t.Errorf("Ref(%s) = %q, want %q", id, got, want)
		}
	}
	got := c.Refs([]models.Category{tree[3], tree[0], tree[1], tree[6]})
	if got != "odd; Tools; Tools > Drills > Bits" {
		t.Errorf("Refs = %q", got)
	}
}

// TestEveryRefResolvesToItsCategory is the round trip: whatever Ref writes,
// Resolve reads back as the same category.
func TestEveryRefResolvesToItsCategory(t *testing.T) {
	c := NewCategories(tree)
	for _, cat := range tree[1:] {
		id, err := c.Resolve(c.Ref(cat.ID))
		if err != nil || id != cat.ID {
			t.Errorf("%s: Ref %q resolved to %q (%v)", cat.ID, c.Ref(cat.ID), id, err)
		}
	}
}

func TestResolve(t *testing.T) {
	c := NewCategories(tree)
	for ref, want := range map[string]string{
		"Tools>Drills":              "drills",
		"  Tools >  Drills > Bits ": "bits",
		"bits":                      "bits",
	} {
		if id, err := c.Resolve(ref); err != nil || id != want {
			t.Errorf("Resolve(%q) = %q, %v", ref, id, err)
		}
	}
	for ref, msg := range map[string]string{
		"Tools > Spare":  "ambiguous",
		"Tools > Hammer": "unknown",
		"Uncategorized":  "unknown",
		"Drills":         "unknown",
	} {
		if _, err := c.Resolve(ref); err == nil || !strings.Contains(err.Error(), msg) {
			t.Errorf("Resolve(%q): %v, want %s", ref, err, msg)
		}
	}
	ids, errs := c.ResolveAll("Tools; ; tools;Tools > Hammer; bits")
	if len(ids) != 2 || ids[0] != "tools" || ids[1] != "bits" || len(errs) != 1 {
		t.Errorf("ResolveAll: %v %v", ids, errs)
	}
}

func TestDefuse(t *testing.T) {
	for in, want := range map[string]string{
		"":              "",
		"Drill":         "Drill",
		"=HYPERLINK(1)": "'=HYPERLINK(1)",
		"+cmd":          "'+cmd",
		"-cmd":          "'-cmd",
		"@SUM(A1)":      "'@SUM(A1)",
		"\tx":           "'\tx",
		"\rx":           "'\rx",
		"'quoted":       "''quoted",
		"-5":            "-5",
		"+3.25":         "+3.25",
		"2026-10-14":    "2026-10-14",
		"Drill = good":  "Drill = good",
	} {
		got := Defuse(in)
		if got != want {
			t.Errorf("Defuse(%q) = %q, want %q", in, got, want)
		}
		if back := Undefuse(got); back != in {
			t.Errorf("Undefuse(Defuse(%q)) = %q", in, back)
		}
	}
	// A typed apostrophe before ordinary text is kept.
	if Undefuse("'tis") != "'tis" {
		t.Error("Undefuse removed an ordinary apostrophe")
	}
}

func TestExport(t *testing.T) {
	assets := []models.AssetWithDetails{
		{
			Asset:         models.Asset{ID: "a1", Name: "Drill, cordless"},
			Categories:    []models.Category{tree[2], tree[6]},
			GeneratorKeys: []models.AssetGeneratorKey{{GeneratorType: "code128", AssignedCode: "D-1"}},
			FieldValues:   map[string]string{"serial": "=1+1", "notes": "line 1\nline \"2\""},
		},
		{
			Asset:       models.Asset{ID: "a2", Name: "Loose"},
			Categories:  []models.Category{tree[0]},
			FieldValues: map[string]string{"serial": "", "color": "red"},
		},
	}
	var b strings.Builder
	if err := Export(&b, assets, NewCategories(tree), []string{"qr", "code128"}); err != nil {
		t.Fatal(err)
	}
	want := BOM +
		"id,name,categories,code.qr,code.code128,field.color,field.notes,field.serial\r\n" +
		"a1,\"Drill, cordless\",odd; Tools > Drills,,D-1,,\"line 1\r\nline \"\"2\"\"\",'=1+1\r\n" +
		"a2,Loose,,,,red,,\r\n"
	if b.String() != want {
		t.Errorf("got\n%q\nwant\n%q", b.String(), want)
	}
}
