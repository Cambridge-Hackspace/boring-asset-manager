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
			placed, err := s.placeSymbol(e, payload, box)
			if err != nil {
				return nil, fail("%v", err)
			}
			if s.Codes == nil {
				s.Codes = map[int]CodePlacement{}
			}
			s.Codes[i] = placed

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

// CodePlacement is how a code was laid out in its box, in dots: its module,
// the space between it and each side of its box as drawn, and its
// standard quiet zone there. Sides run top, right, bottom, left.
type CodePlacement struct {
	Module   int
	Margin   [4]int
	Standard [4]int
}

// Narrow names the sides with less space than the code's standard quiet
// zone.
func (c CodePlacement) Narrow() []string {
	var sides []string
	for i, m := range c.Margin {
		if m < c.Standard[i] {
			sides = append(sides, labels.SideNames[i])
		}
	}
	return sides
}

// placeSymbol encodes payload and draws it in box at the largest whole
// number of dots per module that fits with its margins, placed as its
// alignment says, centered by default. A side's margin is its standard
// quiet zone unless the element gives it, in modules or in length; margins
// are whole dots.
func (s *Scene) placeSymbol(e *labels.Element, payload string, box image.Rectangle) (CodePlacement, error) {
	g, err := generators.Lookup(e.GeneratorType)
	if err != nil {
		return CodePlacement{}, err
	}
	sym, err := g.Encode(payload, e.ErrorCorrection)
	if err != nil {
		return CodePlacement{}, err
	}
	linear := sym.Rows == 1

	// The code's standard quiet zone; bars fill a linear code's height,
	// with none above or below.
	q := float64(sym.QuietZone)
	standard := [4]float64{q, q, q, q}
	if linear {
		standard[0], standard[2] = 0, 0
	}
	// Each side's margin: modules, growing with the module, or fixed dots.
	// quiet_zone, from before margins, sets every side the standard has.
	mods := standard
	if e.QuietZone != nil {
		for i := range mods {
			if standard[i] > 0 {
				mods[i] = float64(*e.QuietZone)
			}
		}
	}
	var fixed [4]int
	for i, side := range e.Margin.Sides() {
		switch {
		case side == nil:
		case e.MarginUnit == labels.MarginLength:
			mods[i], fixed[i] = 0, round(*side*s.dotsPerUnit())
		default:
			mods[i] = *side
		}
	}

	bw, bh := box.Dx(), box.Dy()
	fit := func(room int, modules float64) int {
		if room <= 0 {
			return 0
		}
		return int(math.Floor(float64(room)/modules + 1e-9))
	}
	module := fit(bw-fixed[1]-fixed[3], float64(sym.Cols)+mods[1]+mods[3])
	if !linear {
		module = min(module, fit(bh-fixed[0]-fixed[2], float64(sym.Rows)+mods[0]+mods[2]))
	}
	// The least module: two dots, or its own minimum at the scene's DPI.
	least := MinModuleDots
	if e.MinModuleMM > 0 && s.DPI > 0 {
		least = max(least, int(math.Ceil(e.MinModuleMM*float64(s.DPI)/25.4-1e-9)))
	}
	if module < least {
		needW := int(math.Ceil((float64(sym.Cols)+mods[1]+mods[3])*float64(least)-1e-9)) + fixed[1] + fixed[3]
		if linear {
			return CodePlacement{}, fmt.Errorf("a %d-module symbol needs a box at least %d dots wide (got %d)", sym.Cols, needW, bw)
		}
		needH := int(math.Ceil((float64(sym.Rows)+mods[0]+mods[2])*float64(least)-1e-9)) + fixed[0] + fixed[2]
		return CodePlacement{}, fmt.Errorf("a %d×%d-module symbol needs a box of at least %d×%d dots (got %d×%d)",
			sym.Cols, sym.Rows, needW, needH, bw, bh)
	}
	var margin [4]int
	for i := range margin {
		margin[i] = fixed[i] + round(mods[i]*float64(module))
	}

	// What is left over goes before the code as its alignment says.
	place := func(left int, align string) int {
		left = max(0, left)
		switch align {
		case labels.AlignLeft, labels.AlignTop:
			return 0
		case labels.AlignRight, labels.AlignBottom:
			return left
		}
		return left / 2
	}
	rowHeight := module
	x0 := box.Min.X + margin[3] + place(bw-margin[1]-margin[3]-sym.Cols*module, e.Align)
	y0 := box.Min.Y + margin[0] + place(bh-margin[0]-margin[2]-sym.Rows*module, e.VerticalAlign)
	if linear {
		rowHeight, y0 = bh-margin[0]-margin[2], box.Min.Y+margin[0]
		if rowHeight < 1 {
			return CodePlacement{}, fmt.Errorf("its top and bottom margins leave no room for the bars in a box %d dots high", bh)
		}
	}

	var rects []Rect
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

	placed := CodePlacement{Module: module}
	height := sym.Rows * module
	if linear {
		height = rowHeight
	}
	placed.Margin = [4]int{y0 - box.Min.Y, box.Max.X - x0 - sym.Cols*module, box.Max.Y - y0 - height, x0 - box.Min.X}
	for i := range standard {
		placed.Standard[i] = round(standard[i] * float64(module))
	}
	return placed, nil
}

// dotsPerUnit is how many dots make one of the template's units.
func (s *Scene) dotsPerUnit() float64 {
	if s.Units == labels.UnitsIn {
		return float64(s.DPI)
	}
	return float64(s.DPI) / 25.4
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
