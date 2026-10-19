package render

import (
	"fmt"
	"image"
	"math"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/generators"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
)

// MinModuleDots is the smallest barcode module drawn. Below two dots a single
// misprinted dot can swallow a whole module.
const MinModuleDots = 2

// ImageSource loads uploaded images by ID.
type ImageSource interface {
	LoadImage(id string) (image.Image, error)
}

// Layout places a template's elements for one asset. A template must already
// be valid; layout fails with a *LayoutError when the asset's data cannot fill
// it, such as a missing code or a payload too large for its box.
func Layout(t *labels.Template, data labels.Data, images ImageSource) (*Scene, error) {
	dotsPerUnit := float64(t.DPI) * t.InchesPerUnit()
	s := &Scene{
		Width:  max(1, round(t.Width*dotsPerUnit)),
		Height: max(1, round(t.Height*dotsPerUnit)),
		DPI:    t.DPI,
		Units:  t.Units,
	}
	canvas := image.Rect(0, 0, s.Width, s.Height)

	for _, i := range t.DrawingOrder() {
		e := &t.Elements[i]
		path := fmt.Sprintf("elements[%d]", i)
		fail := func(format string, args ...any) error {
			return &LayoutError{Path: path, Message: fmt.Sprintf(format, args...)}
		}

		box := image.Rect(
			round(e.X*dotsPerUnit), round(e.Y*dotsPerUnit),
			round((e.X+e.Width)*dotsPerUnit), round((e.Y+e.Height)*dotsPerUnit),
		)
		// A turned element's box may reach off the canvas before it is
		// turned; what it draws is clipped as it is drawn.
		if e.Rotation == 0 {
			box = box.Intersect(canvas)
		}
		if box.Empty() {
			return nil, fail("box is smaller than one dot at %d DPI", t.DPI)
		}
		first := len(s.Items)

		switch e.Type {
		case labels.ElementGenerator:
			binding, err := e.Binding()
			if err != nil {
				return nil, fail("%v", err)
			}
			payload := binding.Resolve(data)
			if payload == "" {
				if e.ContentBinding == "" {
					return nil, fail("asset has no %s code", e.GeneratorType)
				}
				return nil, fail("content binding resolved to an empty payload")
			}
			if err := s.placeSymbol(e, payload, box); err != nil {
				return nil, fail("%v", err)
			}

		case labels.ElementText:
			binding, err := e.Binding()
			if err != nil {
				return nil, fail("%v", err)
			}
			p, err := layoutText(e, binding.Resolve(data), box, t.DPI)
			if err != nil {
				return nil, fail("%v", err)
			}
			if len(p) > 0 {
				s.Items = append(s.Items, Item{Path: p})
			}

		case labels.ElementStaticImage:
			if images == nil {
				return nil, fail("no image source available")
			}
			img, err := images.LoadImage(e.ImageID)
			if err != nil {
				return nil, fail("loading image %s: %v", e.ImageID, err)
			}
			placed := fitImage(img, box)
			s.Items = append(s.Items, Item{Image: &placed})

		default:
			return nil, fail("unknown element type %q", e.Type)
		}
		s.turn(s.Items[first:], box, e.Rotation)
	}
	return s, nil
}

// placeSymbol encodes payload and draws it centered in box at the largest
// whole number of dots per module that fits, quiet zone included.
func (s *Scene) placeSymbol(e *labels.Element, payload string, box image.Rectangle) error {
	g, err := generators.Lookup(e.GeneratorType)
	if err != nil {
		return err
	}
	sym, err := g.Encode(payload, e.ErrorCorrection)
	if err != nil {
		return err
	}
	quiet := sym.QuietZone
	if e.QuietZone != nil {
		quiet = *e.QuietZone
	}

	bw, bh := box.Dx(), box.Dy()
	linear := sym.Rows == 1
	module := bw / (sym.Cols + 2*quiet)
	if !linear {
		module = min(module, bh/(sym.Rows+2*quiet))
	}
	// The least module: two dots, or its own minimum at the scene's DPI.
	least := MinModuleDots
	if e.MinModuleMM > 0 && s.DPI > 0 {
		least = max(least, int(math.Ceil(e.MinModuleMM*float64(s.DPI)/25.4-1e-9)))
	}
	if module < least {
		needW := (sym.Cols + 2*quiet) * least
		if linear {
			return fmt.Errorf("a %d-module symbol needs a box at least %d dots wide (got %d)", sym.Cols, needW, bw)
		}
		needH := (sym.Rows + 2*quiet) * least
		return fmt.Errorf("a %d×%d-module symbol needs a box of at least %d×%d dots (got %d×%d)",
			sym.Cols, sym.Rows, needW, needH, bw, bh)
	}

	var rects []Rect
	rowHeight := module
	x0 := box.Min.X + (bw-sym.Cols*module)/2
	y0 := box.Min.Y + (bh-sym.Rows*module)/2
	if linear {
		rowHeight, y0 = bh, box.Min.Y
	}
	for r := 0; r < sym.Rows; r++ {
		for c := 0; c < sym.Cols; {
			if !sym.Dark[r*sym.Cols+c] {
				c++
				continue
			}
			start := c
			for c < sym.Cols && sym.Dark[r*sym.Cols+c] {
				c++
			}
			rects = append(rects, Rect{X: x0 + start*module, Y: y0 + r*module, W: (c - start) * module, H: rowHeight})
		}
	}
	if len(rects) > 0 {
		s.Items = append(s.Items, Item{Rects: rects})
	}
	return nil
}

// fitImage scales img to fit box with its aspect ratio kept, centered.
func fitImage(img image.Image, box image.Rectangle) PlacedImage {
	iw, ih := img.Bounds().Dx(), img.Bounds().Dy()
	bw, bh := box.Dx(), box.Dy()
	w, h := bw, max(1, round(float64(ih)*float64(bw)/float64(iw)))
	if h > bh {
		w, h = max(1, round(float64(iw)*float64(bh)/float64(ih))), bh
	}
	return PlacedImage{
		X: box.Min.X + (bw-w)/2, Y: box.Min.Y + (bh-h)/2,
		W: w, H: h,
		Image: img,
	}
}
