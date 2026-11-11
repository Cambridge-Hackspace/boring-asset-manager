package store

import (
	"errors"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers"
)

func display(t *testing.T, st *Store, name string, conn printers.Connection, driver, model string) *models.Printer {
	t.Helper()
	p, err := st.CreatePrinter(models.Printer{Name: name, Driver: driver, Model: model, Media: model, Connection: conn})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestADisplayRemembersWhatItShowsAndItsImage(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	p := display(t, st, "Bin A3", printers.Connection{Type: printers.ConnPull}, printers.DriverDisplay, "2.9in-296x128")
	user, err := st.CreateUser("ada@test.org", "UserSecret123!", true)
	if err != nil {
		t.Fatal(err)
	}
	drill := asset(t, st, "Drill")

	if _, err := st.GetDisplay(p.ID); !errors.Is(err, ErrDisplayNotFound) {
		t.Fatalf("shown nothing: %v", err)
	}
	if _, _, _, err := st.DisplayImage(p.ID); !errors.Is(err, ErrDisplayNotFound) {
		t.Fatalf("no image: %v", err)
	}
	if err := st.As(user.ID).ShowOnDisplay(p.ID, nil, &drill); err != nil {
		t.Fatal(err)
	}
	etag, err := st.SetDisplayImage(p.ID, []byte("\x89PNG one"), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	d, err := st.GetDisplay(p.ID)
	if err != nil || d.AssetID == nil || *d.AssetID != drill || d.AssetName != "Drill" || d.TemplateID != nil || d.ShownBy != user.ID || d.ShownByToken || d.ETag != etag || d.ShownAt == nil {
		t.Fatalf("display %+v, %v", d, err)
	}
	img, contentType, tag, err := st.DisplayImage(p.ID)
	if err != nil || string(img) != "\x89PNG one" || contentType != "image/png" || tag != etag {
		t.Fatalf("image %q %s %s %v", img, contentType, tag, err)
	}
	// Another image, another tag; the same one, the same.
	other, _ := st.SetDisplayImage(p.ID, []byte("\x89PNG two"), "image/png")
	same, _ := st.SetDisplayImage(p.ID, []byte("\x89PNG two"), "image/png")
	if other == etag || same != other {
		t.Fatalf("tags %s %s %s", etag, other, same)
	}
	// Deleting the asset leaves the display showing nothing to refresh;
	// deleting the printer forgets the display.
	if _, err := st.Exec("DELETE FROM assets WHERE id = ?", drill); err != nil {
		t.Fatal(err)
	}
	if d, _ := st.GetDisplay(p.ID); d.AssetID != nil {
		t.Fatalf("asset %v", *d.AssetID)
	}
	if err := st.RefreshDisplay(p.ID, time.Now()); !errors.Is(err, ErrDisplayNotFound) {
		t.Fatalf("refreshing nothing: %v", err)
	}
	if err := st.DeletePrinter(p.ID); err != nil {
		t.Fatal(err)
	}
	if list, err := st.ListDisplays(); err != nil || len(list) != 0 {
		t.Fatalf("displays %+v, %v", list, err)
	}
}

func TestDisplayTokensAreHashedAndScopedToTheirDisplay(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	p := display(t, st, "Bin A3", printers.Connection{Type: printers.ConnPull}, printers.DriverDisplay, "2.9in-296x128")
	printer, err := st.CreatePrinter(models.Printer{Name: "QL", Driver: printers.DriverBrotherQL, Model: "QL-810W", Media: "dk-11201", Connection: printers.Connection{Type: printers.ConnTCP, Address: "10.0.0.5"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CreateDisplayToken(printer.ID, "x"); !errors.Is(err, ErrNotADisplay) {
		t.Fatalf("a printer's token: %v", err)
	}
	if _, _, err := st.CreateDisplayToken(p.ID, " "); !errors.Is(err, ErrInvalidDisplayToken) {
		t.Fatalf("no name: %v", err)
	}
	tok, secret, err := st.CreateDisplayToken(p.ID, "Frame")
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.AuthenticateDisplayToken(secret)
	if err != nil || got.ID != tok.ID || got.PrinterID != p.ID || got.LastUsedAt == nil {
		t.Fatalf("authenticated %+v, %v", got, err)
	}
	// It is no API token, nor is an API token one.
	if _, _, err := st.AuthenticateToken(secret); !errors.Is(err, ErrTokenNotFound) {
		t.Fatalf("as an API token: %v", err)
	}
	if _, err := st.AuthenticateDisplayToken("bam_" + secret[len(DisplayTokenPrefix):]); !errors.Is(err, ErrDisplayTokenNotFound) {
		t.Fatalf("an API token's prefix: %v", err)
	}
	if err := st.RevokeDisplayToken(printer.ID, tok.ID); !errors.Is(err, ErrDisplayTokenNotFound) {
		t.Fatalf("revoked through another printer: %v", err)
	}
	if err := st.RevokeDisplayToken(p.ID, tok.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AuthenticateDisplayToken(secret); !errors.Is(err, ErrDisplayTokenNotFound) {
		t.Fatalf("revoked: %v", err)
	}
}

func TestRefreshesComeOncePerBurstForWhatIsInAnAsset(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	contains := relation(t, st, "contains")
	shelf, bin, screws, kettle := asset(t, st, "Shelf"), asset(t, st, "Bin"), asset(t, st, "Screws"), asset(t, st, "Kettle")
	link(t, st, shelf, contains, bin, true)
	link(t, st, bin, contains, screws, true)
	shows := display(t, st, "Screws", printers.Connection{Type: printers.ConnPull}, printers.DriverDisplay, "2.9in-296x128")
	elsewhere := display(t, st, "Kettle", printers.Connection{Type: printers.ConnPull}, printers.DriverDisplay, "2.9in-296x128")
	tpl, err := st.CreateTemplate(&labels.Template{Name: "Bin", Units: labels.UnitsMM, Width: 67.12857142857143, Height: 29.02857142857143, DPI: 112})
	if err != nil {
		t.Fatal(err)
	}
	for id, a := range map[string]string{shows.ID: screws, elsewhere.ID: kettle} {
		if err := st.ShowOnDisplay(id, &tpl.ID, &a); err != nil {
			t.Fatal(err)
		}
	}

	// The screws are in the bin, which is on the shelf.
	for asset, want := range map[string]int{screws: 1, bin: 1, shelf: 1, kettle: 1} {
		got, err := st.displaysShowing(asset)
		if err != nil || len(got) != want {
			t.Errorf("displays showing %s: %v, %v", asset, got, err)
		}
	}
	if got, _ := st.displaysShowing(shelf); len(got) != 1 || got[0] != shows.ID {
		t.Fatalf("the shelf's: %v", got)
	}

	now := time.Now().UTC()
	window := time.Duration(DefaultDisplayRefreshWindow) * time.Second
	for i := range 3 {
		tx, _ := st.DB.Begin()
		if err := st.refreshDisplaysTx(tx, []string{shows.ID}, now.Add(window+time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	if due, _ := st.DueDisplayRefreshes(now); len(due) != 0 {
		t.Fatalf("due now: %+v", due)
	}
	due, err := st.DueDisplayRefreshes(now.Add(time.Hour))
	if err != nil || len(due) != 1 || due[0].PrinterID != shows.ID || !due[0].RefreshAt.Equal(now.Add(window).Truncate(time.Microsecond)) {
		t.Fatalf("due %+v, %v", due, err)
	}
	ok, err := st.ClaimDisplayRefresh(shows.ID, *due[0].RefreshAt)
	if err != nil || !ok {
		t.Fatalf("claimed %v, %v", ok, err)
	}
	if ok, _ := st.ClaimDisplayRefresh(shows.ID, *due[0].RefreshAt); ok {
		t.Fatal("claimed twice")
	}
	// Asked for by hand, it is due when asked.
	if err := st.RefreshDisplay(elsewhere.ID, now); err != nil {
		t.Fatal(err)
	}
	if due, _ := st.DueDisplayRefreshes(now); len(due) != 1 || due[0].PrinterID != elsewhere.ID {
		t.Fatalf("by hand: %+v", due)
	}
}

func TestATRMNLIsFoundByItsMACAddress(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	p := display(t, st, "Workshop", printers.Connection{Type: printers.ConnPull, Address: "aa-bb-cc-dd-ee-ff"}, printers.DriverTRMNL, "og")
	for _, mac := range []string{"AA:BB:CC:DD:EE:FF", "aa:bb:cc:dd:ee:ff", "aabbccddeeff"} {
		if got, err := st.DisplayByMAC(printers.DriverTRMNL, mac); err != nil || got.ID != p.ID {
			t.Errorf("%s: %v", mac, err)
		}
	}
	for _, mac := range []string{"", "11:22:33:44:55:66", "nonsense"} {
		if _, err := st.DisplayByMAC(printers.DriverTRMNL, mac); !errors.Is(err, ErrPrinterNotFound) {
			t.Errorf("%q: %v", mac, err)
		}
	}
	if _, err := st.DisplayByMAC(printers.DriverDisplay, "AA:BB:CC:DD:EE:FF"); !errors.Is(err, ErrPrinterNotFound) {
		t.Error("found under another driver")
	}
}

func TestAHooksActionsAreKept(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	h, err := st.SaveHook("", HookInput{Key: "refresh", Label: "Refresh", Actions: []string{models.HookActionRefreshDisplays}})
	if err != nil || len(h.Actions) != 1 || h.Actions[0] != models.HookActionRefreshDisplays {
		t.Fatalf("saved %+v, %v", h, err)
	}
	h, err = st.SaveHook(h.ID, HookInput{Key: "refresh", Label: "Refresh"})
	if err != nil || len(h.Actions) != 0 {
		t.Fatalf("cleared %+v, %v", h, err)
	}
	for _, bad := range [][]string{{"reboot"}, {models.HookActionRefreshDisplays, models.HookActionRefreshDisplays}} {
		if _, err := st.SaveHook("", HookInput{Key: "x", Label: "X", Actions: bad}); !errors.Is(err, ErrInvalidHook) {
			t.Errorf("%v: %v", bad, err)
		}
	}
}
