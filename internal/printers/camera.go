package printers

import (
	"fmt"
	"image"
	"math"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/vision"
)

// Camera alignment (docs/printing.md#camera-alignment): a laser with a
// camera engraves calibration marks on scrap, through a job like any other,
// started by a person at the machine; BAM! finds them in a photo and fits
// the camera's pixels to the bed, at the height the scrap's surface stands.
// Then a photo finds a workpiece wherever it lies, and the label is placed
// on it.

// MarkLayout is where a model's calibration marks are engraved.
func MarkLayout(m OneShotModel) vision.MarkLayout {
	return vision.LayoutFor(m.WorkWidthMM, m.WorkHeightMM)
}

// CalibrationJob is the job that engraves a model's calibration marks on
// scrap of a material: dark dots, at the material's engraving settings.
// Nothing is cut.
func CalibrationJob(m OneShotModel, material Material) (*Job, vision.MarkLayout, error) {
	if !m.Camera {
		return nil, vision.MarkLayout{}, fmt.Errorf("%w: the %s has no camera to calibrate", ErrDoesNotFitWorkpiece, m.ID)
	}
	l := MarkLayout(m)
	r := l.OriginDiameter / 2
	x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, p := range l.Marks {
		x0, y0, x1, y1 = math.Min(x0, p.X-r), math.Min(y0, p.Y-r), math.Max(x1, p.X+r), math.Max(y1, p.Y+r)
	}
	area := RectMM{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
	w, h := LabelDots(area.Width, area.Height, material.Engrave.LPI)
	dots := image.NewGray(image.Rect(0, 0, w, h))
	sx, sy := float64(w)/area.Width, float64(h)/area.Height
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			px, py := area.X+(float64(x)+0.5)/sx, area.Y+(float64(y)+0.5)/sy
			v := uint8(0xff)
			for i, p := range l.Marks {
				d := l.Diameter
				if i == 0 {
					d = l.OriginDiameter
				}
				if math.Hypot(px-p.X, py-p.Y) <= d/2 {
					v = 0
					break
				}
			}
			dots.Pix[y*dots.Stride+x] = v
		}
	}
	// Engraved from the back or not, the marks are where the camera sees
	// them: never mirrored.
	material.FromBack = false
	scrap := RectMM{X: l.Scrap[0], Y: l.Scrap[1], Width: l.Scrap[2], Height: l.Scrap[3]}
	return &Job{Model: m.ID, Material: material, Workpiece: scrap, Placed: area, Label: area, Engrave: dots, Calibration: true}, l, nil
}
