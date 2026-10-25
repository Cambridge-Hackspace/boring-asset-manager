// Package live draws labels for the template editor's live preview. It is
// compiled to WebAssembly (cmd/bamlive) and run in the browser, so the
// editor redraws a label exactly as it will print at every frame of a drag,
// with the same code the server prints with. The server stays the authority
// on what prints; this only shows it sooner.
package live

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // the decoders the server's images are uploaded in
	_ "image/png"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex/ir"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/preview"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/render"
)

// Renderer draws labels with the images and icons it has been given.
type Renderer struct {
	images map[string]image.Image
	// icons are the build's, by name, unknown the icon drawn for a name it
	// does not draw, and colors the palette's light colors (SetIcons).
	icons   map[string]*ir.Icon
	unknown *ir.Icon
	colors  map[string]string
}

// Icons is what SetIcons takes: the build's icons, as its IR lists them,
// the unknown icon, and the palette's light colors, by token.
type Icons struct {
	Icons   []ir.Icon         `json:"icons"`
	Unknown *ir.Icon          `json:"unknown"`
	Colors  map[string]string `json:"colors"`
}

// SetIcons gives the renderer the icons labels draw (README §3.7),
// replacing any it had.
func (r *Renderer) SetIcons(data []byte) error {
	var in Icons
	if err := json.Unmarshal(data, &in); err != nil {
		return fmt.Errorf("reading icons: %w", err)
	}
	r.icons = make(map[string]*ir.Icon, len(in.Icons))
	for i := range in.Icons {
		r.icons[in.Icons[i].Name] = &in.Icons[i]
	}
	r.unknown, r.colors = in.Unknown, in.Colors
	return nil
}

// LabelIcon gives the renderer an icon by name, or the unknown icon for
// a name it has none by (render.IconSource).
func (r *Renderer) LabelIcon(name string) *ir.Icon {
	if icon := r.icons[name]; icon != nil {
		return icon
	}
	return r.unknown
}

// IconColors gives the renderer the palette's light colors.
func (r *Renderer) IconColors() map[string]string { return r.colors }

// New returns a Renderer with no images.
func New() *Renderer {
	return &Renderer{images: map[string]image.Image{}}
}

// AddImage decodes an uploaded image, as the server stores it, for the
// templates that place it.
func (r *Renderer) AddImage(id string, data []byte) error {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("decoding image: %w", err)
	}
	r.images[id] = img
	return nil
}

// HasImage reports whether the image has been added.
func (r *Renderer) HasImage(id string) bool {
	_, ok := r.images[id]
	return ok
}

// LoadImage gives the renderer an added image.
func (r *Renderer) LoadImage(id string) (image.Image, error) {
	if img, ok := r.images[id]; ok {
		return img, nil
	}
	return nil, errors.New("image is missing from storage")
}

// Result is one drawn label: its dots, as a printer prints them, and the
// description the server's JSON preview gives. A template that cannot be
// drawn has Problems instead, as the server's preview would answer.
type Result struct {
	Width  int `json:"width"`
	Height int `json:"height"`
	// Pixels are RGBA, four bytes a dot, row by row: black or white, as a
	// thermal printer prints them, images dithered.
	Pixels      []byte               `json:"-"`
	Description *preview.Description `json:"description,omitempty"`
	Problems    []labels.Problem     `json:"problems,omitempty"`
}

// Render draws a template with data, as JSON in the forms the server takes
// and gives: the template as the editor holds it, and labels.Data as
// POST /api/v1/templates/preview-data answers. With placeholders, every
// field the template binds is filled with its key in brackets, as the
// server's preview of no asset is.
//
// The elements hidden, by index, are left out of the dots but not of the
// description: the editor hides elements while editing, and text being typed
// on the label, whose size it still needs.
//
// The label is drawn as designed, the right way round and level: a
// template that prints mirrored or turned is edited as it is read once it
// is on.
func (r *Renderer) Render(templateJSON, dataJSON []byte, placeholders bool, hidden ...int) Result {
	doc, err := preview.WithDefaults(templateJSON)
	if err != nil {
		return problem("", "template must be a JSON object")
	}
	t, err := labels.Parse(doc)
	if err != nil {
		var ve *labels.ValidationError
		if errors.As(err, &ve) {
			return Result{Problems: ve.Problems}
		}
		return problem("", err.Error())
	}
	t.PrintMirror, t.PrintRotation = "", 0
	var data labels.Data
	if err := json.Unmarshal(dataJSON, &data); err != nil {
		return problem("", "data must be the preview data the server gives")
	}
	if placeholders {
		data = preview.WithFields(data, t)
	}
	scene, err := render.Layout(t, data, r)
	if err != nil {
		var le *render.LayoutError
		if errors.As(err, &le) {
			return problem(le.Path, le.Message)
		}
		return problem("", err.Error())
	}
	drawn := scene
	if len(hidden) > 0 {
		left := map[int]bool{}
		for _, i := range hidden {
			left[i] = true
		}
		shown := *t
		shown.Elements = nil
		for i, e := range t.Elements {
			if !left[i] {
				shown.Elements = append(shown.Elements, e)
			}
		}
		if drawn, err = render.Layout(&shown, data, r); err != nil {
			return problem("", err.Error())
		}
	}
	gray := drawn.Raster(true)
	b := gray.Bounds()
	pixels := make([]byte, 0, b.Dx()*b.Dy()*4)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		row := gray.Pix[(y-b.Min.Y)*gray.Stride:]
		for x := 0; x < b.Dx(); x++ {
			v := row[x]
			pixels = append(pixels, v, v, v, 0xff)
		}
	}
	d := preview.Describe(t, data, scene)
	return Result{Width: b.Dx(), Height: b.Dy(), Pixels: pixels, Description: &d}
}

func problem(path, message string) Result {
	return Result{Problems: []labels.Problem{{Path: path, Message: message}}}
}
