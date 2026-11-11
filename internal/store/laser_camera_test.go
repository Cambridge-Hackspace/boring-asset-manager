package store

import (
	"errors"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers"
)

// TestALasersCalibrationIsKeptForItsModel: saved again it is replaced;
// it is not used once the printer is another model; it goes with the
// printer.
func TestALasersCalibrationIsKeptForItsModel(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	p := laser(t, st, "Camera laser", "camera-laser.test")
	if _, err := st.LaserCalibration(p.ID); !errors.Is(err, ErrNoCalibration) {
		t.Fatalf("before calibrating: %v", err)
	}
	user, err := st.CreateUser("ada@test.org", "UserSecret123!", true)
	if err != nil {
		t.Fatal(err)
	}
	c := models.LaserCalibration{PrinterID: p.ID, Homography: [9]float64{0.25, 0.01, -17.5, -0.002, 0.26, -13, 1e-5, -2e-5, 1}, ImageWidth: 800, ImageHeight: 600, ZMM: 3, Marks: 9, RMSMM: 0.12, MaxMM: 0.2}
	if _, err := st.As(user.ID).SaveLaserCalibration(c); err != nil {
		t.Fatal(err)
	}
	c.ZMM = 6.5
	if _, err := st.As(user.ID).SaveLaserCalibration(c); err != nil {
		t.Fatal(err)
	}
	got, err := st.LaserCalibration(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ZMM != 6.5 || got.Homography != c.Homography || got.Model != printers.TestLaserPanel || got.CalibratedBy != user.ID || got.ImageWidth != 800 || got.Marks != 9 || got.RMSMM != 0.12 {
		t.Errorf("calibration %+v", got)
	}

	// Another model: its work area differs, so the calibration is not used.
	p.Model = printers.TestLaserButton
	if _, err := st.UpdatePrinter(*p); err != nil {
		t.Fatal(err)
	}
	if _, err := st.LaserCalibration(p.ID); !errors.Is(err, ErrNoCalibration) {
		t.Errorf("as another model: %v", err)
	}
	if err := st.DeleteLaserCalibration(p.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := st.QueryRow("SELECT COUNT(*) FROM laser_calibrations WHERE printer_id = ?", p.ID).Scan(&n); err != nil || n != 0 {
		t.Errorf("%d left, %v", n, err)
	}

	// Deleting the printer deletes its calibration.
	p.Model = printers.TestLaserPanel
	if _, err := st.UpdatePrinter(*p); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveLaserCalibration(c); err != nil {
		t.Fatal(err)
	}
	if err := st.DeletePrinter(p.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.QueryRow("SELECT COUNT(*) FROM laser_calibrations").Scan(&n); err != nil || n != 0 {
		t.Errorf("%d left after the printer, %v", n, err)
	}
}

func TestLaserCameraSettings(t *testing.T) {
	st := setupTestStore(t)
	defer st.Close()
	got, err := st.LaserCameraSettings()
	if err != nil || got.MaxErrorMM != DefaultLaserCameraMaxError || got.HeightToleranceMM != DefaultLaserCameraHeightTolerance {
		t.Fatalf("defaults %+v %v", got, err)
	}
	if err := st.SetLaserCameraSettings(LaserCameraSettings{MaxErrorMM: 0.8, HeightToleranceMM: 0}); err != nil {
		t.Fatal(err)
	}
	if got, _ = st.LaserCameraSettings(); got.MaxErrorMM != 0.8 || got.HeightToleranceMM != 0 {
		t.Errorf("saved %+v", got)
	}
	for _, bad := range []LaserCameraSettings{{MaxErrorMM: 0, HeightToleranceMM: 1}, {MaxErrorMM: 1, HeightToleranceMM: -1}, {MaxErrorMM: 11, HeightToleranceMM: 1}} {
		if err := st.SetLaserCameraSettings(bad); !errors.Is(err, ErrInvalidSetting) {
			t.Errorf("%+v: %v", bad, err)
		}
	}
}
