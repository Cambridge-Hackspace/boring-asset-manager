package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/archive"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// instance is a migrated instance of its own, with its configuration file.
func instance(t *testing.T) (string, *store.Store) {
	t.Helper()
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
	t.Cleanup(func() { st.Close() })
	return configPath, st
}

func scalar(t *testing.T, st *store.Store, query string, args ...any) string {
	t.Helper()
	var s string
	if err := st.QueryRow(query, args...).Scan(&s); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return s
}

// TestImportMergesAPackage: --check changes nothing; --anchor names the
// category its top categories go under, and --choose what to do with what
// clashes, by name or key; what names nothing is refused.
func TestImportMergesAPackage(t *testing.T) {
	srcConfig, src := instance(t)
	tools, err := src.CreateCategory("Tools", nil, "#123456", false, false)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := src.CreateField("serial", "Serial", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := src.AssignCategoryField(tools.ID, serial.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := src.CreateAsset("Lathe", []string{tools.ID}, nil, map[string]string{"serial": "L-1"}); err != nil {
		t.Fatal(err)
	}
	pkg := filepath.Join(t.TempDir(), "tools.bam.tar.gz")
	if err := Export(srcConfig, pkg, ExportOptions{Select: []string{"subtree:Tools", "assets"}, Info: archive.PackageInfo{Name: "Tools"}}); err != nil {
		t.Fatal(err)
	}

	dstConfig, dst := instance(t)
	if _, err := dst.CreateCategory("Lab", nil, "#654321", false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := dst.CreateField("serial", "Serial number", "text", nil); err != nil {
		t.Fatal(err)
	}
	if err := Import(dstConfig, pkg, ImportOptions{Anchor: "Lab", Check: true}); err != nil {
		t.Fatal(err)
	}
	if n := scalar(t, dst, "SELECT COUNT(*) FROM assets"); n != "0" {
		t.Fatalf("--check imported %s assets", n)
	}
	for _, c := range []struct {
		opts ImportOptions
		want string
	}{
		{ImportOptions{Anchor: "Nowhere"}, "no category"},
		{ImportOptions{Choose: []string{"field:colour=copy"}}, "holds no field"},
		{ImportOptions{Choose: []string{"field:serial=maybe"}}, "existing, copy or skip"},
		{ImportOptions{Choose: []string{"category:Tools=skip"}}, "clashes with nothing"},
	} {
		if err := Import(dstConfig, pkg, c.opts); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: %v", c.opts, err)
		}
	}

	if err := Import(dstConfig, pkg, ImportOptions{Anchor: "Lab", Choose: []string{"field:serial=existing"}}); err != nil {
		t.Fatal(err)
	}
	if got := scalar(t, dst, "SELECT p.name FROM categories c JOIN categories p ON p.id = c.parent_id WHERE c.name = 'Tools'"); got != "Lab" {
		t.Errorf("Tools is under %q", got)
	}
	if got := scalar(t, dst, "SELECT f.label FROM asset_field_values v JOIN fields f ON f.id = v.field_id JOIN assets a ON a.id = v.asset_id WHERE a.name = 'Lathe'"); got != "Serial number" {
		t.Errorf("the lathe's serial is in %q", got)
	}
	if n := scalar(t, dst, "SELECT COUNT(*) FROM fields"); n != "1" {
		t.Errorf("%s fields", n)
	}

	bad := filepath.Join(t.TempDir(), "bad.bam.tar.gz")
	b, err := os.ReadFile(pkg)
	if err != nil {
		t.Fatal(err)
	}
	b[len(b)/2] ^= 0xff
	if err := os.WriteFile(bad, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Import(dstConfig, bad, ImportOptions{}); err == nil {
		t.Error("imported a corrupted package")
	}
}

// TestIconsExportAsALibraryFolder: bam export of the icons alone writes a
// zipped isolex library folder, and bam import takes one back.
func TestIconsExportAsALibraryFolder(t *testing.T) {
	c := isolex.Default()
	if !c.Available() {
		if os.Getenv("BAM_REQUIRE_ISOLEX") != "" {
			t.Fatal("isolex is not available")
		}
		t.Skip("isolex is not available")
	}
	configPath, st := instance(t)
	if _, _, err := st.CreateIconLibraryFile(context.Background(), c, "shared/crates.ilx", `base "crate-base" { category "storage"; footprint 1 1 1; part "box" { box 0 0 0 1 1 1 } }`); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "icons.zip")
	if err := Export(configPath, dest, ExportOptions{Select: []string{"icons"}, Info: archive.PackageInfo{Name: "Icons"}}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(dest)
	if err != nil || !archive.IsIconSet(b) {
		t.Fatalf("not a zip: %v", err)
	}
	if err := Import(configPath, dest, ImportOptions{Check: true}); err != nil {
		t.Fatal(err)
	}
	if err := Import(configPath, dest, ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := Import(configPath, dest, ImportOptions{Anchor: "Nowhere"}); err == nil {
		t.Error("an anchor for an icon set was taken")
	}
}
