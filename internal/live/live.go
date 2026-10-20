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

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/preview"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/render"
)

// Renderer draws labels with the images it has been given.
type Renderer struct {
	images map[string]image.Image
}

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
