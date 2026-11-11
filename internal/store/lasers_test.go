package store

import (
	"errors"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers"
)

func laser(t *testing.T, st *Store, name, address string) *models.Printer {
	t.Helper()
	printers.RegisterTestLaser()
	p, err := st.CreatePrinter(models.Printer{Name: name, Driver: printers.DriverTestLaser, Model: printers.TestLaserPanel, Connection: printers.Connection{Type: printers.ConnTCP, Address: address}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestALasersMaterialsAreItsPresetsTunedAndItsOwn(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	p := laser(t, st, "Workshop laser", "laser.test")

	list, err := st.ListMaterials(p.ID)
	if err != nil || len(list) != 3 || !list[0].Preset || list[0].Tuned || list[0].UpdatedAt != nil {
		t.Fatalf("presets: %+v %v", list, err)
	}
	tuned := list[0].Material
	tuned.Engrave.Power, tuned.Cut = 42.5, nil
	if _, err := st.SaveMaterial(p.ID, tuned); err != nil {
		t.Fatal(err)
	}
	own := printers.Material{ID: "oak-6mm", Name: "Oak, 6 mm", Engrave: printers.EngraveSetting{Power: 40, Speed: 120, Passes: 2, LPI: 300}, Cut: &printers.CutSetting{Power: 100, Speed: 3.5, Passes: 4}, FromBack: true}
	if _, err := st.SaveMaterial(p.ID, own); err != nil {
		t.Fatal(err)
	}
	// Saved again, it is changed, not doubled.
	own.Name = "Oak, 6 mm, oiled"
	if _, err := st.SaveMaterial(p.ID, own); err != nil {
		t.Fatal(err)
	}
	list, _ = st.ListMaterials(p.ID)
	if len(list) != 4 || !list[0].Tuned || list[0].Engrave.Power != 42.5 || list[0].Cut != nil || list[0].UpdatedAt == nil {
		t.Fatalf("tuned: %+v", list[0])
	}
	got := list[3]
	if got.ID != "oak-6mm" || got.Name != "Oak, 6 mm, oiled" || got.Preset || !got.FromBack || got.Cut == nil || got.Cut.Speed != 3.5 || got.Engrave.LPI != 300 {
		t.Fatalf("own: %+v", got)
	}
	if _, err := st.SaveMaterial(p.ID, printers.Material{ID: "Bad ID", Name: "x", Engrave: tuned.Engrave}); !errors.Is(err, printers.ErrInvalidMaterial) {
		t.Errorf("invalid: %v", err)
	}

	// Deleting puts a preset back, and removes one of its own.
	if err := st.DeleteMaterial(p.ID, tuned.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteMaterial(p.ID, "oak-6mm"); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteMaterial(p.ID, "oak-6mm"); !errors.Is(err, ErrMaterialNotFound) {
		t.Errorf("deleted twice: %v", err)
	}
	if list, _ = st.ListMaterials(p.ID); len(list) != 3 || list[0].Tuned || list[0].Engrave.Power != 30 {
		t.Errorf("after deleting: %+v", list)
	}

	// A printer that is not a laser has none.
	ql, err := st.CreatePrinter(models.Printer{Name: "QL", Driver: printers.DriverBrotherQL, Model: "QL-810W", Media: "dk-22205", Connection: printers.Connection{Type: printers.ConnTCP, Address: "ql.test"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ListMaterials(ql.ID); !errors.Is(err, ErrNotOneShot) {
		t.Errorf("a QL's materials: %v", err)
	}
	// Deleting the laser deletes its materials.
	if _, err := st.SaveMaterial(p.ID, own); err != nil {
		t.Fatal(err)
	}
	if err := st.DeletePrinter(p.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := st.QueryRow("SELECT COUNT(*) FROM laser_materials").Scan(&n); err != nil || n != 0 {
		t.Errorf("materials left: %d %v", n, err)
	}
}

func TestALasersJobsAreItsHistory(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	p := laser(t, st, "Workshop laser", "laser-jobs.test")
	user, err := st.CreateUser("ada@test.org", "UserSecret123!", true)
	if err != nil {
		t.Fatal(err)
	}
	drill := asset(t, st, "Drill")
	material := printers.Material{ID: "birch", Name: "Birch", Engrave: printers.EngraveSetting{Power: 30, Speed: 150, Passes: 1, LPI: 254}}
	job := models.LaserJob{PrinterID: &p.ID, PrinterName: p.Name, TemplateName: "Tag", AssetID: &drill, AssetName: "Drill", Material: material,
		Placement: models.LaserPlacement{Workpiece: printers.RectMM{Width: 60, Height: 30}, Label: printers.RectMM{X: 5, Y: 5, Width: 50, Height: 20}, Cuts: 1}}

	first, err := st.As(user.ID).CreateLaserJob(job)
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.As(user.ID).CreateLaserJob(job)
	if err != nil {
		t.Fatal(err)
	}
	// One prepared job a printer: the first is discarded.
	if j, _ := st.GetLaserJob(first.ID); j.State != models.LaserJobDiscarded || j.EndedAt == nil {
		t.Errorf("first: %+v", j)
	}
	started, err := st.As(user.ID).StartLaserJob(second.ID, models.LaserJobRunning)
	if err != nil || started.State != models.LaserJobRunning || started.StartedBy != user.ID || started.StartedByToken || started.StartedAt == nil {
		t.Fatalf("started: %+v %v", started, err)
	}
	if _, err := st.StartLaserJob(second.ID, models.LaserJobRunning); !errors.Is(err, ErrLaserJobState) {
		t.Errorf("started twice: %v", err)
	}
	if changed, err := st.SetLaserJobState(second.ID, models.LaserJobDone, "Finished"); !changed || err != nil {
		t.Fatalf("done: %v %v", changed, err)
	}
	// Ended, it stays as it ended.
	if changed, _ := st.SetLaserJobState(second.ID, models.LaserJobFailed, "late"); changed {
		t.Error("an ended job changed")
	}
	j, err := st.GetLaserJob(second.ID)
	if err != nil || j.State != models.LaserJobDone || j.Message != "Finished" || j.Material.Engrave.Power != 30 || j.Placement.Label.X != 5 || j.PreparedBy != user.ID || *j.AssetID != drill {
		t.Fatalf("job: %+v %v", j, err)
	}
	list, err := st.ListLaserJobs(p.ID, "", 10)
	if err != nil || len(list) != 2 {
		t.Fatalf("list: %d %v", len(list), err)
	}
	if byAsset, _ := st.ListLaserJobs("", drill, 10); len(byAsset) != 2 {
		t.Errorf("by asset: %d", len(byAsset))
	}
	if err := st.RecordLabelEngraved(drill, "Tag", p.Name, "Birch"); err != nil {
		t.Fatal(err)
	}

	// The history outlives the printer.
	if err := st.DeletePrinter(p.ID); err != nil {
		t.Fatal(err)
	}
	if j, err := st.GetLaserJob(second.ID); err != nil || j.PrinterID != nil || j.PrinterName != "Workshop laser" {
		t.Errorf("after the printer went: %+v %v", j, err)
	}
	if _, err := st.GetLaserJob("nope"); !errors.Is(err, ErrLaserJobNotFound) {
		t.Errorf("unknown: %v", err)
	}
}
