package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/archive"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// TestExportWritesAPackage: --select names things by name, key or slug,
// or by ID; what names nothing, or more than one, is refused; the package
// written checks out, holding what was chosen; and --plan writes nothing.
func TestExportWritesAPackage(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	cfg := config.DefaultConfig()
	cfg.Database.DSN = filepath.Join(dir, "bam.db")
	cfg.Storage.LocalPath = filepath.Join(dir, "uploads")
	if err := cfg.SaveTOML(configPath); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(configPath); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(&cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	tools, err := st.CreateCategory("Tools", nil, "#123456", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateCategory("Twin", nil, "#123456", false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateCategory("Twin", &tools.ID, "#123456", false, false); err != nil {
		t.Fatal(err)
	}
	serial, err := st.CreateField("serial", "Serial", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	sel, err := ParseSelection(st, []string{"subtree:Tools,field:serial", "assets"})
	if err != nil {
		t.Fatal(err)
	}
	if want := (archive.Selection{Subtrees: []string{tools.ID}, Fields: []string{serial.ID}, Assets: &archive.AssetsSelection{}}); !reflect.DeepEqual(sel, want) {
		t.Errorf("selection %+v", sel)
	}
	if sel, err := ParseSelection(st, []string{"category:" + tools.ID, "icons", "assets:all"}); err != nil || sel.Categories[0] != tools.ID || !sel.Icons || !sel.Assets.All {
		t.Errorf("by ID: %+v %v", sel, err)
	}
	for item, want := range map[string]string{"category:Nowhere": "no category", "category:Twin": "names 2", "colour:red": "choose", "flow:": "choose"} {
		if _, err := ParseSelection(st, []string{item}); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", item, err)
		}
	}
	st.Close()

	dest := filepath.Join(dir, "tools.bam.tar.gz")
	if err := Export(configPath, dest, ExportOptions{Select: []string{"subtree:Tools"}}); err == nil || !strings.Contains(err.Error(), "--name") {
		t.Errorf("without a name: %v", err)
	}
	if err := Export(configPath, dest, ExportOptions{Select: []string{"field:serial"}, Photos: true, Info: archive.PackageInfo{Name: "x"}}); err == nil {
		t.Error("--photos without assets was taken")
	}
	if err := Export(configPath, dest, ExportOptions{Select: []string{"subtree:Tools"}, Plan: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("--plan wrote %s: %v", dest, err)
	}
	if err := Export(configPath, dest, ExportOptions{Select: []string{"subtree:Tools", "field:serial"}, Info: archive.PackageInfo{Name: "Tools", License: "CC0-1.0"}}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	checked, err := archive.Check(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if checked.Header.Type != "package" || checked.Header.Package.Name != "Tools" || checked.Rows["categories"] != 2 || checked.Rows["fields"] != 1 || checked.Rows["users"] != 0 {
		t.Errorf("header %+v, rows %v", checked.Header, checked.Rows)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*.partial")); len(left) != 0 {
		t.Errorf("left behind: %v", left)
	}
}
