package vision_test

import (
	"errors"
	"image"
	"image/color"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/simcamera"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/vision"
)

// TestFitRecoversAKnownTransform maps points through a known homography,
// with and without noise, and fits it back.
func TestFitRecoversAKnownTransform(t *testing.T) {
	known := vision.Homography{0.31, -0.02, -18, 0.015, 0.29, -12, 0.00004, -0.00003, 1}
	var px, mm []vision.Point
	for y := 0.0; y <= 600; y += 150 {
		for x := 0.0; x <= 800; x += 200 {
			p := vision.Point{X: x, Y: y}
			px = append(px, p)
			mm = append(mm, known.Apply(p))
		}
	}
	fit, err := vision.FitHomography(px, mm)
	if err != nil {
		t.Fatal(err)
	}
	if fit.RMS > 1e-9 {
		t.Errorf("exact points fit with an error of %g", fit.RMS)
	}
	for i := range fit.H {
		if math.Abs(fit.H[i]-known[i]) > 1e-9*math.Max(1, math.Abs(known[i])) {
			t.Fatalf("H = %v, want %v", fit.H, known)
		}
	}

	// With noise, least squares over every point lands close.
	r := rand.New(rand.NewPCG(1, 2))
	noisy := make([]vision.Point, len(mm))
	for i, p := range mm {
		noisy[i] = vision.Point{X: p.X + 0.1*r.NormFloat64(), Y: p.Y + 0.1*r.NormFloat64()}
	}
	fit, err = vision.FitHomography(px, noisy)
	if err != nil {
		t.Fatal(err)
	}
	if fit.RMS < 0.03 || fit.RMS > 0.2 || fit.Max < fit.RMS {
		t.Errorf("RMS %g, max %g: want about the noise, 0.1 mm", fit.RMS, fit.Max)
	}
	for _, p := range []vision.Point{{X: 400, Y: 300}, {X: 10, Y: 590}} {
		got, want := fit.H.Apply(p), known.Apply(p)
		if d := math.Hypot(got.X-want.X, got.Y-want.Y); d > 0.15 {
			t.Errorf("%v maps %g mm from where it should", p, d)
		}
	}

	inv, err := known.Inverse()
	if err != nil {
		t.Fatal(err)
	}
	if p := inv.Apply(known.Apply(vision.Point{X: 123, Y: 45})); math.Abs(p.X-123) > 1e-6 || math.Abs(p.Y-45) > 1e-6 {
		t.Errorf("the inverse maps back to %v", p)
	}
}

// TestFitRefusesDegeneratePoints: fewer than four, or all in a line.
func TestFitRefusesDegeneratePoints(t *testing.T) {
	three := []vision.Point{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 0, Y: 1}}
	if _, err := vision.FitHomography(three, three); !errors.Is(err, vision.ErrDegenerate) {
		t.Errorf("three points: %v", err)
	}
	line := []vision.Point{{X: 0, Y: 0}, {X: 1, Y: 1}, {X: 2, Y: 2}, {X: 3, Y: 3}, {X: 4, Y: 4}}
	if _, err := vision.FitHomography(line, line); !errors.Is(err, vision.ErrDegenerate) {
		t.Errorf("points in a line: %v", err)
	}
}

func decode(t *testing.T, name string) image.Image {
	t.Helper()
	img, err := vision.Decode(simcamera.Photo(name))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// TestCalibrateFromThePhoto finds the marks in the calibration photo, and
// the fit maps the photo onto the bed as the camera does.
func TestCalibrateFromThePhoto(t *testing.T) {
	layout := vision.LayoutFor(simcamera.WorkWidthMM, simcamera.WorkHeightMM)
	cal, err := vision.Calibrate(decode(t, simcamera.Calibration), layout)
	if err != nil {
		t.Fatal(err)
	}
	if cal.Marks != len(layout.Marks) || cal.Width != 800 || cal.Height != 600 {
		t.Errorf("found %d of %d marks in a %d × %d photo", cal.Marks, cal.Expected, cal.Width, cal.Height)
	}
	if cal.RMS > 0.3 {
		t.Errorf("the marks fit within %g mm, RMS", cal.RMS)
	}
	truth := simcamera.Camera(800, 600)
	for _, mm := range []vision.Point{{X: 10, Y: 10}, {X: 100, Y: 75}, {X: 190, Y: 140}, {X: 20, Y: 130}} {
		got := cal.H.Apply(truth.Apply(mm))
		if d := math.Hypot(got.X-mm.X, got.Y-mm.Y); d > 0.5 {
			t.Errorf("%v on the bed is measured %g mm away, at %v", mm, d, got)
		}
	}
}

// TestCalibrateRefusesAPhotoWithoutMarks: the workpiece photo has none.
func TestCalibrateRefusesAPhotoWithoutMarks(t *testing.T) {
	layout := vision.LayoutFor(simcamera.WorkWidthMM, simcamera.WorkHeightMM)
	if _, err := vision.Calibrate(decode(t, simcamera.Workpiece), layout); !errors.Is(err, vision.ErrMarksNotFound) {
		t.Errorf("err = %v, want ErrMarksNotFound", err)
	}
}

// TestFindTheWorkpiece measures each workpiece photo, through the fit made
// from the calibration photo (scaled for the photo of another size), and
// lands within a millimeter and a degree of where it lies.
func TestFindTheWorkpiece(t *testing.T) {
	layout := vision.LayoutFor(simcamera.WorkWidthMM, simcamera.WorkHeightMM)
	cal, err := vision.Calibrate(decode(t, simcamera.Calibration), layout)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{simcamera.Workpiece, simcamera.WorkpieceLarge} {
		t.Run(name, func(t *testing.T) {
			img := decode(t, name)
			b := img.Bounds()
			h := cal.H
			if b.Dx() != cal.Width {
				h = h.Scaled(cal.Width, cal.Height, b.Dx(), b.Dy())
			}
			got, err := vision.FindWorkpiece(img, h, simcamera.WorkWidthMM, simcamera.WorkHeightMM)
			if err != nil {
				t.Fatal(err)
			}
			want := simcamera.Pieces[name]
			if d := math.Hypot(got.Center.X-want.Center.X, got.Center.Y-want.Center.Y); d > 1 {
				t.Errorf("center %v, want %v (%g mm off)", got.Center, want.Center, d)
			}
			if math.Abs(got.Width-want.Width) > 1 || math.Abs(got.Height-want.Height) > 1 {
				t.Errorf("size %g × %g, want %g × %g", got.Width, got.Height, want.Width, want.Height)
			}
			if math.Abs(got.AngleDeg-want.AngleDeg) > 1 {
				t.Errorf("turned %g°, want %g°", got.AngleDeg, want.AngleDeg)
			}
		})
	}
}

// TestNoWorkpieceOnAnEmptyBed: a dark, even photo shows none.
func TestNoWorkpieceOnAnEmptyBed(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 400, 300))
	for i := range img.Pix {
		img.Pix[i] = 40
	}
	img.SetGray(10, 10, color.Gray{Y: 41})
	_, err := vision.FindWorkpiece(img, vision.Homography{0.5, 0, 0, 0, 0.5, 0, 0, 0, 1}, 200, 150)
	if !errors.Is(err, vision.ErrNoWorkpiece) {
		t.Errorf("err = %v, want ErrNoWorkpiece", err)
	}
}

// TestRectify draws the work area from above: the calibration photo's
// origin mark lands where it was engraved.
func TestRectify(t *testing.T) {
	layout := vision.LayoutFor(simcamera.WorkWidthMM, simcamera.WorkHeightMM)
	img := decode(t, simcamera.Calibration)
	cal, err := vision.Calibrate(img, layout)
	if err != nil {
		t.Fatal(err)
	}
	scale := vision.RectifyScale(simcamera.WorkWidthMM, simcamera.WorkHeightMM, 800)
	out, err := vision.Rectify(img, cal.H, simcamera.WorkWidthMM, simcamera.WorkHeightMM, scale)
	if err != nil {
		t.Fatal(err)
	}
	if out.Bounds().Dx() != 800 || out.Bounds().Dy() != 600 {
		t.Fatalf("rectified to %v", out.Bounds())
	}
	o := layout.Marks[0]
	if c := out.RGBAAt(int(o.X*scale), int(o.Y*scale)); c.R > 120 {
		t.Errorf("the origin mark is drawn %v, not dark", c)
	}
	between := vision.Point{X: (layout.Marks[0].X + layout.Marks[1].X) / 2, Y: layout.Marks[0].Y + 6}
	if c := out.RGBAAt(int(between.X*scale), int(between.Y*scale)); c.R < 140 {
		t.Errorf("the scrap between marks is drawn %v, not light", c)
	}
}

func TestMeanBrightness(t *testing.T) {
	if b := vision.MeanBrightness(image.NewGray(image.Rect(0, 0, 50, 50))); b != 0 {
		t.Errorf("a black frame is %g bright", b)
	}
	if b := vision.MeanBrightness(decode(t, simcamera.Workpiece)); b < 40 {
		t.Errorf("a photo is %g bright", b)
	}
}
