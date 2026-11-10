package labels

import (
	"image/color"
	"slices"
	"strings"
)

// Inks are the colors a template draws its text, shapes and codes in: a
// small set, named, so that a template means the same on every device
// (docs/labels.md#inks). White paints over what is beneath it.
const (
	InkBlack  = PaintBlack
	InkWhite  = PaintWhite
	InkRed    = "red"
	InkYellow = "yellow"
	InkGreen  = "green"
	InkBlue   = "blue"
	InkOrange = "orange"
)

// Inks lists every ink, in the order palettes list them.
var Inks = []string{InkBlack, InkWhite, InkRed, InkYellow, InkGreen, InkBlue, InkOrange}

// inkColors are the colors inks are shown in, and the colors images are
// dithered towards: near what e-paper panels and red-and-black labels show.
var inkColors = map[string]color.RGBA{
	InkBlack:  {0x00, 0x00, 0x00, 0xFF},
	InkWhite:  {0xFF, 0xFF, 0xFF, 0xFF},
	InkRed:    {0xCC, 0x00, 0x00, 0xFF},
	InkYellow: {0xFF, 0xD2, 0x00, 0xFF},
	InkGreen:  {0x00, 0x8C, 0x3C, 0xFF},
	InkBlue:   {0x00, 0x46, 0xC8, 0xFF},
	InkOrange: {0xFF, 0x80, 0x00, 0xFF},
}

// ValidInk reports whether name is an ink.
func ValidInk(name string) bool {
	_, ok := inkColors[name]
	return ok
}

// InkColor is the color an ink is shown in; black for a name that is not
// an ink.
func InkColor(ink string) color.RGBA {
	if c, ok := inkColors[ink]; ok {
		return c
	}
	return inkColors[InkBlack]
}

// Palette is the inks a printer or display has (docs/labels.md#inks): a
// driver declares one for each model, and for the roll loaded where that
// matters. Every palette has black and white.
type Palette string

// The palettes: black and white; with red, yellow or both, as e-paper
// panels and red-and-black labels have; and six and seven colors, as
// Spectra 6 and ACeP panels have.
const (
	PaletteBW      Palette = "bw"
	PaletteBWR     Palette = "bwr"
	PaletteBWY     Palette = "bwy"
	PaletteBWRY    Palette = "bwry"
	PaletteBWRYGB  Palette = "bwrygb"
	PaletteBWRYGBO Palette = "bwrygbo"
)

// Palettes lists every palette, the fewest inks first.
var Palettes = []Palette{PaletteBW, PaletteBWR, PaletteBWY, PaletteBWRY, PaletteBWRYGB, PaletteBWRYGBO}

var paletteInks = map[Palette][]string{
	PaletteBW:      {InkBlack, InkWhite},
	PaletteBWR:     {InkBlack, InkWhite, InkRed},
	PaletteBWY:     {InkBlack, InkWhite, InkYellow},
	PaletteBWRY:    {InkBlack, InkWhite, InkRed, InkYellow},
	PaletteBWRYGB:  {InkBlack, InkWhite, InkRed, InkYellow, InkGreen, InkBlue},
	PaletteBWRYGBO: {InkBlack, InkWhite, InkRed, InkYellow, InkGreen, InkBlue, InkOrange},
}

// Valid reports whether p is one of the palettes.
func (p Palette) Valid() bool {
	_, ok := paletteInks[p]
	return ok
}

// Inks lists the palette's inks, black and white first; a palette that is
// not one has black and white alone.
func (p Palette) Inks() []string {
	if inks, ok := paletteInks[p]; ok {
		return slices.Clone(inks)
	}
	return slices.Clone(paletteInks[PaletteBW])
}

// Has reports whether the palette has an ink.
func (p Palette) Has(ink string) bool { return slices.Contains(p.Inks(), ink) }

// Resolve is the ink a device with the palette draws ink in: the ink
// itself, or black, which every device has, when it lacks it.
func (p Palette) Resolve(ink string) string {
	if p.Has(ink) {
		return ink
	}
	return InkBlack
}

// Missing lists the inks of those the palette lacks, in the order of Inks.
func (p Palette) Missing(inks []string) []string {
	var out []string
	for _, ink := range Inks {
		if slices.Contains(inks, ink) && !p.Has(ink) {
			out = append(out, ink)
		}
	}
	return out
}

// Colors are the colors of the palette's inks, in its order.
func (p Palette) Colors() []color.RGBA {
	inks := p.Inks()
	out := make([]color.RGBA, len(inks))
	for i, ink := range inks {
		out[i] = InkColor(ink)
	}
	return out
}

// Describe names the palette's inks for messages: "black and white" or
// "black, white and red".
func (p Palette) Describe() string { return JoinAnd(p.Inks()) }

// PaletteFor is the palette with the fewest inks that has every one of
// inks: the one a template is drawn in when no device is chosen.
func PaletteFor(inks []string) Palette {
	for _, p := range Palettes {
		if len(p.Missing(inks)) == 0 {
			return p
		}
	}
	return PaletteBWRYGBO
}

// Inks lists the inks the template's elements draw in, alternatives
// included, in the order of Inks: black for any code or text without a
// fill, and every shape's stroke and fill but none. Images and icons are
// dithered into whatever palette they are drawn in, and use no ink.
func (t *Template) Inks() []string {
	used := map[string]bool{}
	add := func(e *Element) {
		switch e.Type {
		case ElementText, ElementGenerator:
			if e.Fill == "" {
				used[InkBlack] = true
			} else {
				used[e.Fill] = true
			}
		case ElementShape:
			used[e.Stroke] = true
			used[e.Fill] = true
		}
	}
	for i := range t.Elements {
		add(&t.Elements[i])
		if alt := t.Elements[i].Alternative; alt != nil {
			add(alt)
		}
	}
	var out []string
	for _, ink := range Inks {
		if used[ink] {
			out = append(out, ink)
		}
	}
	return out
}

// JoinAnd joins words as a list in a sentence: "a", "a and b", "a, b and c".
func JoinAnd(words []string) string {
	switch len(words) {
	case 0:
		return ""
	case 1:
		return words[0]
	}
	return strings.Join(words[:len(words)-1], ", ") + " and " + words[len(words)-1]
}
