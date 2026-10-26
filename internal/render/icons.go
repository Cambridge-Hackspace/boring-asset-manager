package render

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strconv"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex/ir"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex/raster"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
)

// IconSource gives the icons labels draw (README §3.7), when an image
// source does: the icon drawn by a name, the unknown icon when nothing
// draws it, or nil when there are no icons at all; and the palette's
// light colors, by token.
type IconSource interface {
	LabelIcon(name string) *ir.Icon
	IconColors() map[string]string
}

// categoryToken paints in the asset's category's color.
const categoryToken = "category"

// placeIcon draws an icon element in its box: the asset's icon or the one
// named, nothing when there is none or no icons to draw it from. Shaded,
// each token prints as the gray of its light color; as an outline, the
// strokes alone print, in black.
func (s *Scene) placeIcon(e *labels.Element, box image.Rectangle, data labels.Data, images ImageSource) error {
	name, tint := e.Icon, ""
	if name == labels.IconOfAsset {
		name, tint = data.Icon, data.IconColor
		if e.State && data.StateIcon != "" {
			name = data.StateIcon
		}
	}
	icons, ok := images.(IconSource)
	if name == "" || !ok {
		return nil
	}
	icon := icons.LabelIcon(name)
	if icon == nil || !(icon.Viewbox[2] > 0 && icon.Viewbox[3] > 0) {
		return nil
	}
	palette := icons.IconColors()
	colors := func(token string) string {
		c := palette[token]
		if token == categoryToken {
			c = tint
			if c == "" {
				c = palette["ink-muted"]
			}
		}
		return gray(c)
	}

	// as large as fits the box, its shape kept, at the dots it prints at
	aspect := icon.Viewbox[2] / icon.Viewbox[3]
	h := min(box.Dy(), int(math.Floor(float64(box.Dx())/aspect)))
	h = min(max(h, 1), raster.MaxHeight)
	img, err := raster.DrawWith(icon, colors, h, raster.Options{Outline: e.Shading == labels.ShadingOutline, Ink: color.NRGBA{A: 255}})
	if err != nil {
		return err
	}
	var drawn image.Image = img
	if e.Mirror != "" {
		drawn = mirrored(img, e.Mirror != labels.MirrorDown, e.Mirror != labels.MirrorAcross)
	}
	w := img.Bounds().Dx()
	x := box.Min.X + (box.Dx()-w)/2
	switch e.Align {
	case labels.AlignLeft:
		x = box.Min.X
	case labels.AlignRight:
		x = box.Max.X - w
	}
	s.Items = append(s.Items, Item{Image: &PlacedImage{X: x, Y: box.Min.Y + (box.Dy()-h)/2, W: w, H: h, Image: drawn}})
	return nil
}

// gray is a color as the gray of its luminance (Rec. 709), keeping its
// alpha; anything not a color stays as it is, and paints nothing.
func gray(c string) string {
	if len(c) < 4 || c[0] != '#' {
		return c
	}
	h := c[1:]
	if len(h) == 3 {
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	}
	alpha := "ff"
	if len(h) == 8 {
		h, alpha = h[:6], h[6:]
	}
	v, err := strconv.ParseUint(h, 16, 32)
	if len(h) != 6 || err != nil {
		return c
	}
	r, g, b := float64(v>>16&0xff), float64(v>>8&0xff), float64(v&0xff)
	y := uint8(math.Round(0.2126*r + 0.7152*g + 0.0722*b))
	return fmt.Sprintf("#%02x%02x%02x%s", y, y, y, alpha)
}
