// Package simcamera is the simulated laser's camera (docs/printing.md#lasers):
// synthetic photos of its bed, made by Render from known scenes and checked
// in, so tests know exactly where the marks and the workpiece are. They
// stand in for recorded photos from a real machine, which are still
// wanted.
//
// Regenerate the photos with
//
//	go test ./internal/printers/simcamera -run TestPhotos -update
package simcamera

import (
	"embed"
	"fmt"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/vision"
)

// The work area every photo is of: the test laser's panel model's.
const (
	WorkWidthMM  = 200
	WorkHeightMM = 150
)

// The photos.
const (
	// Calibration shows the marks engraved on scrap.
	Calibration = "calibration"
	// Workpiece shows a workpiece, a little turned.
	Workpiece = "workpiece"
	// WorkpieceLarge shows another, turned the other way, in a photo of
	// another size, as some cameras send.
	WorkpieceLarge = "workpiece-large"
)

// Truth is a workpiece where it really lies: its center and size in mm,
// and how far it is turned clockwise, in degrees.
type Truth struct {
	Center   vision.Point
	Width    float64
	Height   float64
	AngleDeg float64
}

// Pieces are the workpieces the photos show.
var Pieces = map[string]Truth{
	Workpiece:      {Center: vision.Point{X: 92, Y: 70}, Width: 72, Height: 38, AngleDeg: 9},
	WorkpieceLarge: {Center: vision.Point{X: 130, Y: 95}, Width: 50, Height: 30, AngleDeg: -13},
}

// Camera maps millimeters on the bed to the pixels of a photo w × h: a
// camera looking down at an angle, a little turned, its view the same at
// every size.
func Camera(w, h int) vision.Homography {
	sx, sy := float64(w)/800, float64(h)/600
	bed := []vision.Point{{X: 0, Y: 0}, {X: WorkWidthMM, Y: 0}, {X: WorkWidthMM, Y: WorkHeightMM}, {X: 0, Y: WorkHeightMM}}
	px := []vision.Point{{X: 70 * sx, Y: 52 * sy}, {X: 735 * sx, Y: 40 * sy}, {X: 760 * sx, Y: 560 * sy}, {X: 48 * sx, Y: 548 * sy}}
	fit, err := vision.FitHomography(bed, px)
	if err != nil {
		panic(err)
	}
	return fit.H
}

// Scenes are what each photo shows.
func Scenes() map[string]Scene {
	layout := vision.LayoutFor(WorkWidthMM, WorkHeightMM)
	scrap := layout.Scrap
	piece, large := Pieces[Workpiece], Pieces[WorkpieceLarge]
	return map[string]Scene{
		Calibration:    {Width: 800, Height: 600, Camera: Camera(800, 600), Scrap: &scrap, Marks: &layout, Seed: 1},
		Workpiece:      {Width: 800, Height: 600, Camera: Camera(800, 600), Piece: &piece, Seed: 2},
		WorkpieceLarge: {Width: 1024, Height: 768, Camera: Camera(1024, 768), Piece: &large, Seed: 3},
	}
}

//go:embed *.jpg
var photos embed.FS

// Photo is one of the photos, as a camera sends it.
func Photo(name string) []byte {
	b, err := photos.ReadFile(name + ".jpg")
	if err != nil {
		panic(fmt.Sprintf("simcamera: no photo %q", name))
	}
	return b
}
