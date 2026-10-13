package store

import (
	"errors"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
)

func TestPrinterLifecycle(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()

	p, err := st.CreatePrinter(models.Printer{Name: " Front desk ", Driver: "brother_ql", Model: "QL-810W", Host: "192.168.1.40", Media: "dk-11201"})
	if err != nil {
		t.Fatalf("CreatePrinter: %v", err)
	}
	if p.Name != "Front desk" || p.Port != printers.DefaultPort {
		t.Errorf("name/port not normalized: %+v", p)
	}

	if _, err := st.CreatePrinter(models.Printer{Name: "Front desk", Driver: "brother_ql", Model: "QL-810W", Host: "10.0.0.2", Media: "dk-22210"}); !errors.Is(err, ErrPrinterNameExists) {
		t.Errorf("duplicate name: got %v, want ErrPrinterNameExists", err)
	}
	if _, err := st.CreatePrinter(models.Printer{Name: "Bad", Driver: "brother_ql", Model: "QL-810W", Host: "10.0.0.2", Media: "dk-0"}); !errors.Is(err, printers.ErrInvalidPrinter) {
		t.Errorf("bad media: got %v, want ErrInvalidPrinter", err)
	}

	got, err := st.GetPrinter(p.ID)
	if err != nil || got.Host != "192.168.1.40" || got.Media != "dk-11201" {
		t.Fatalf("GetPrinter() = %+v, %v", got, err)
	}

	time.Sleep(10 * time.Millisecond)
	p.Media, p.Port = "dk-22205", 9101
	updated, err := st.UpdatePrinter(*p)
	if err != nil {
		t.Fatalf("UpdatePrinter: %v", err)
	}
	if !updated.UpdatedAt.After(got.UpdatedAt) || !updated.CreatedAt.Equal(got.CreatedAt) {
		t.Errorf("timestamps not maintained: %+v", updated)
	}
	if reloaded, _ := st.GetPrinter(p.ID); reloaded.Media != "dk-22205" || reloaded.Port != 9101 {
		t.Errorf("update not stored: %+v", reloaded)
	}

	// Options are validated by the driver, stored compactly and read back.
	p.Options = []byte(`{ "cut" : false }`)
	if _, err := st.UpdatePrinter(*p); err != nil {
		t.Fatalf("setting options: %v", err)
	}
	if reloaded, _ := st.GetPrinter(p.ID); string(reloaded.Options) != `{"cut":false}` {
		t.Errorf("options stored as %q", reloaded.Options)
	}
	p.Options = []byte(`{"speed":9}`)
	if _, err := st.UpdatePrinter(*p); !errors.Is(err, printers.ErrInvalidPrinter) {
		t.Errorf("unknown option: got %v, want ErrInvalidPrinter", err)
	}
	p.Options = []byte("null")
	if _, err := st.UpdatePrinter(*p); err != nil {
		t.Fatal(err)
	}
	if reloaded, _ := st.GetPrinter(p.ID); reloaded.Options != nil {
		t.Errorf("null options stored as %q", reloaded.Options)
	}

	list, err := st.ListPrinters()
	if err != nil || len(list) != 1 {
		t.Errorf("ListPrinters() = %v, %v", list, err)
	}

	if err := st.DeletePrinter(p.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.DeletePrinter(p.ID); !errors.Is(err, ErrPrinterNotFound) {
		t.Errorf("second delete: got %v", err)
	}
	if _, err := st.UpdatePrinter(*p); !errors.Is(err, ErrPrinterNotFound) {
		t.Errorf("update deleted: got %v", err)
	}
}

func TestAdminRoleHoldsLabelsPrint(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	admin, err := st.CreateAdminUser("admin@example.org", "AdminSecret123!")
	if err != nil {
		t.Fatal(err)
	}
	perms, err := st.GetUserPermissions(admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !perms[rbac.PermLabelsPrint] {
		t.Errorf("admin permissions %v lack %s", perms, rbac.PermLabelsPrint)
	}
}
