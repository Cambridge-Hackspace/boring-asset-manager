// Package appicon draws the icons an installed BAM! shows on a phone's home
// screen (docs/installing.md#on-a-phone): a logo, centered on a square of
// the background color, at each size the web app manifest names. The logo
// is the one an admin chose or, until one is, BAM!'s own.
package appicon

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg" // logos may be JPEG
	"image/png"
	"strconv"
	"strings"

	xdraw "golang.org/x/image/draw"
)

//go:embed bam.png
var bamPNG []byte

// BAM decodes BAM!'s own logo, the burst.
func BAM() image.Image {
	img, err := png.Decode(bytes.NewReader(bamPNG))
	if err != nil {
		panic("appicon: the embedded logo does not decode: " + err.Error())
	}
	return img
}

// An Icon is one of the files the manifest lists.
type Icon struct {
	// File is its name under /app-icons/.
	File string
	Size int
	// Maskable icons are cut to a circle, a squircle or another shape by
	// the launcher, so the logo keeps to the middle; the rest show whole.
	Maskable bool
}

// Icons are those the manifest lists, and the one iOS reads from
// <link rel="apple-touch-icon"> (180 px, its home screen's size).
var Icons = []Icon{
	{File: "icon-192.png", Size: 192},
	{File: "icon-512.png", Size: 512},
	{File: "maskable-192.png", Size: 192, Maskable: true},
	{File: "maskable-512.png", Size: 512, Maskable: true},
	{File: "apple-touch-icon.png", Size: 180},
}

// Named finds an icon by its file name.
func Named(file string) (Icon, bool) {
	for _, ic := range Icons {
		if ic.File == file {
			return ic, true
		}
	}
	return Icon{}, false
}

// The share of the square a logo may fill: most of it for an icon shown
// whole, and for a maskable one what stays inside the circle of the
// smallest mask, 40% of the side in radius, whatever the logo's shape.
const (
	fillWhole    = 0.8
	fillMaskable = 0.56
)

// Draw draws icon: logo, scaled to fit with its shape kept, centered on a
// square of background, encoded as PNG.
func Draw(logo image.Image, icon Icon, background color.Color) ([]byte, error) {
	if icon.Size < 1 {
		return nil, fmt.Errorf("appicon: size %d", icon.Size)
	}
	dst := image.NewRGBA(image.Rect(0, 0, icon.Size, icon.Size))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(background), image.Point{}, draw.Src)

	fill := fillWhole
	if icon.Maskable {
		fill = fillMaskable
	}
	b := logo.Bounds()
	box := float64(icon.Size) * fill
	scale := min(box/float64(b.Dx()), box/float64(b.Dy()))
	w, h := max(1, int(float64(b.Dx())*scale+0.5)), max(1, int(float64(b.Dy())*scale+0.5))
	x, y := (icon.Size-w)/2, (icon.Size-h)/2
	// Over, so a logo's transparent parts show the background.
	xdraw.CatmullRom.Scale(dst, image.Rect(x, y, x+w, y+h), logo, b, xdraw.Over, nil)

	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ParseColor reads a color as #rrggbb.
func ParseColor(s string) (color.RGBA, bool) {
	if len(s) != 7 || s[0] != '#' || strings.ContainsAny(s[1:], "+-") {
		return color.RGBA{}, false
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return color.RGBA{}, false
	}
	return color.RGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}, true
}
