package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// Lasers' cameras (docs/printing.md#camera-alignment): each laser's latest
// calibration, and the instance's limits on them.

// ErrNoCalibration is returned for a laser whose camera has not been
// calibrated, or was calibrated as another model.
var ErrNoCalibration = errors.New("the laser's camera has not been calibrated")

// The instance's camera settings.
const (
	// LaserCameraMaxErrorSetting is the largest RMS error, in mm, a
	// calibration is kept with.
	LaserCameraMaxErrorSetting = "lasers.camera_max_error_mm"
	// LaserCameraHeightToleranceSetting is how far, in mm, a job's height
	// may be from its calibration's before calibrating again is asked for.
	LaserCameraHeightToleranceSetting = "lasers.camera_height_tolerance_mm"
)

// Their defaults and bounds, in mm.
const (
	DefaultLaserCameraMaxError        = 0.5
	DefaultLaserCameraHeightTolerance = 1.0
	MinLaserCameraMaxError            = 0.01
	MaxLaserCameraMaxError            = 10
	MaxLaserCameraHeightTolerance     = 50
)

// LaserCameraSettings are the instance's limits on cameras.
type LaserCameraSettings struct {
	MaxErrorMM        float64 `json:"max_error_mm"`
	HeightToleranceMM float64 `json:"height_tolerance_mm"`
}

// LaserCameraSettings reads them, each its default when unset.
func (s *Store) LaserCameraSettings() (LaserCameraSettings, error) {
	out := LaserCameraSettings{MaxErrorMM: DefaultLaserCameraMaxError, HeightToleranceMM: DefaultLaserCameraHeightTolerance}
	for _, v := range []struct {
		key      string
		to       *float64
		min, max float64
	}{{LaserCameraMaxErrorSetting, &out.MaxErrorMM, MinLaserCameraMaxError, MaxLaserCameraMaxError}, {LaserCameraHeightToleranceSetting, &out.HeightToleranceMM, 0, MaxLaserCameraHeightTolerance}} {
		text, err := s.Setting(v.key, "")
		if err != nil {
			return out, err
		}
		if f, err := strconv.ParseFloat(text, 64); err == nil && f >= v.min && f <= v.max {
			*v.to = f
		}
	}
	return out, nil
}

// SetLaserCameraSettings saves them.
func (s *Store) SetLaserCameraSettings(in LaserCameraSettings) error {
	if !(in.MaxErrorMM >= MinLaserCameraMaxError && in.MaxErrorMM <= MaxLaserCameraMaxError) {
		return fmt.Errorf("%w: the largest calibration error must be %g to %g mm", ErrInvalidSetting, float64(MinLaserCameraMaxError), float64(MaxLaserCameraMaxError))
	}
	if !(in.HeightToleranceMM >= 0 && in.HeightToleranceMM <= MaxLaserCameraHeightTolerance) {
		return fmt.Errorf("%w: the height tolerance must be 0 to %g mm", ErrInvalidSetting, float64(MaxLaserCameraHeightTolerance))
	}
	return s.withTx(func(tx *sql.Tx) error {
		if err := s.setSettingTx(tx, LaserCameraMaxErrorSetting, strconv.FormatFloat(in.MaxErrorMM, 'f', -1, 64)); err != nil {
			return err
		}
		return s.setSettingTx(tx, LaserCameraHeightToleranceSetting, strconv.FormatFloat(in.HeightToleranceMM, 'f', -1, 64))
	})
}

// SaveLaserCalibration keeps a laser's calibration in place of any before,
// as calibrated by whoever the store acts as.
func (s *Store) SaveLaserCalibration(c models.LaserCalibration) (*models.LaserCalibration, error) {
	p, _, err := s.oneShotPrinter(c.PrinterID)
	if err != nil {
		return nil, err
	}
	for _, v := range c.Homography {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, errors.New("the calibration's homography is not finite")
		}
	}
	h, err := json.Marshal(c.Homography)
	if err != nil {
		return nil, err
	}
	c.Model, c.CreatedAt = p.Model, dbTime(time.Now())
	c.CalibratedBy, c.CalibratedByToken = "", false
	if id, ok := s.actorID().(string); ok {
		c.CalibratedBy, c.CalibratedByToken = id, s.actor == ""
	}
	var by any
	if c.CalibratedBy != "" {
		by = c.CalibratedBy
	}
	err = s.withTx(func(tx *sql.Tx) error {
		if _, err := s.TxExec(tx, "DELETE FROM laser_calibrations WHERE printer_id = ?", c.PrinterID); err != nil {
			return err
		}
		_, err := s.TxExec(tx, "INSERT INTO laser_calibrations (printer_id, model, homography, image_width, image_height, z_mm, marks, rms_mm, max_mm, calibrated_by, calibrated_by_token, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			c.PrinterID, c.Model, string(h), c.ImageWidth, c.ImageHeight, c.ZMM, c.Marks, c.RMSMM, c.MaxMM, by, c.CalibratedByToken, c.CreatedAt)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("saving the calibration: %w", err)
	}
	return &c, nil
}

// LaserCalibration reads a laser's calibration: ErrNoCalibration when it
// has none, or one made as another model.
func (s *Store) LaserCalibration(printerID string) (*models.LaserCalibration, error) {
	p, _, err := s.oneShotPrinter(printerID)
	if err != nil {
		return nil, err
	}
	var c models.LaserCalibration
	var h string
	var by sql.NullString
	err = s.QueryRow("SELECT printer_id, model, homography, image_width, image_height, z_mm, marks, rms_mm, max_mm, calibrated_by, calibrated_by_token, created_at FROM laser_calibrations WHERE printer_id = ?", printerID).
		Scan(&c.PrinterID, &c.Model, &h, &c.ImageWidth, &c.ImageHeight, &c.ZMM, &c.Marks, &c.RMSMM, &c.MaxMM, &by, &c.CalibratedByToken, &c.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoCalibration
	}
	if err != nil {
		return nil, fmt.Errorf("reading the calibration: %w", err)
	}
	if c.Model != p.Model {
		return nil, ErrNoCalibration
	}
	if err := json.Unmarshal([]byte(h), &c.Homography); err != nil {
		return nil, fmt.Errorf("reading the calibration's homography: %w", err)
	}
	c.CalibratedBy, c.CreatedAt = by.String, c.CreatedAt.UTC()
	return &c, nil
}

// DeleteLaserCalibration forgets a laser's calibration, if it has one.
func (s *Store) DeleteLaserCalibration(printerID string) error {
	if _, _, err := s.oneShotPrinter(printerID); err != nil {
		return err
	}
	_, err := s.Exec("DELETE FROM laser_calibrations WHERE printer_id = ?", printerID)
	return err
}
