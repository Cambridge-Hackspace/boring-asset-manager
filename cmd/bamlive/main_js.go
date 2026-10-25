//go:build js && wasm

// Command bamlive is the template editor's live renderer: internal/live,
// compiled to WebAssembly and run in a Web Worker in the browser. It sets
// globalThis.bamlive to an object with:
//
//	addImage(id, bytes)                        -> null, or an error message
//	hasImage(id)                               -> whether it was added
//	setIcons(icons)                            -> null, or an error message
//	render(template, data, placeholders, hidden) -> {width, height, pixels, description, problems}
//	font(family, weight)                       -> the font labels are printed in, as TTF bytes
//
// where template, data and icons (live.Icons) are JSON strings, pixels a Uint8ClampedArray of
// RGBA dots, and description and problems JSON strings (or null).
//
// tools/web/build.sh builds it into the web interface.
package main

import (
	"encoding/json"
	"syscall/js"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/live"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/gofont/goregular"
)

// fonts are those labels are printed in (internal/render), for the editor
// to show text being typed in the font it prints in.
var fonts = map[string][]byte{
	"sans normal": goregular.TTF,
	"sans bold":   gobold.TTF,
	"mono normal": gomono.TTF,
	"mono bold":   gomonobold.TTF,
}

func main() {
	r := live.New()
	api := js.Global().Get("Object").New()
	api.Set("addImage", js.FuncOf(func(_ js.Value, args []js.Value) any {
		data := make([]byte, args[1].Get("length").Int())
		js.CopyBytesToGo(data, args[1])
		if err := r.AddImage(args[0].String(), data); err != nil {
			return err.Error()
		}
		return nil
	}))
	api.Set("hasImage", js.FuncOf(func(_ js.Value, args []js.Value) any {
		return r.HasImage(args[0].String())
	}))
	api.Set("setIcons", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if err := r.SetIcons([]byte(args[0].String())); err != nil {
			return err.Error()
		}
		return nil
	}))
	api.Set("render", js.FuncOf(func(_ js.Value, args []js.Value) any {
		// The elements left out of the dots, as a JSON array of indices.
		var hidden []int
		if len(args) > 3 && args[3].Type() == js.TypeString {
			_ = json.Unmarshal([]byte(args[3].String()), &hidden)
		}
		res := r.Render([]byte(args[0].String()), []byte(args[1].String()), args[2].Bool(), hidden...)
		out := js.Global().Get("Object").New()
		out.Set("width", res.Width)
		out.Set("height", res.Height)
		pixels := js.Global().Get("Uint8ClampedArray").New(len(res.Pixels))
		js.CopyBytesToJS(pixels, res.Pixels)
		out.Set("pixels", pixels)
		out.Set("description", jsonOrNull(res.Description))
		out.Set("problems", jsonOrNull(res.Problems))
		return out
	}))
	api.Set("font", js.FuncOf(func(_ js.Value, args []js.Value) any {
		ttf, ok := fonts[args[0].String()+" "+args[1].String()]
		if !ok {
			return nil
		}
		out := js.Global().Get("Uint8Array").New(len(ttf))
		js.CopyBytesToJS(out, ttf)
		return out
	}))
	js.Global().Set("bamlive", api)
	select {}
}

func jsonOrNull[T any](v T) any {
	b, err := json.Marshal(v)
	if err != nil || string(b) == "null" {
		return nil
	}
	return string(b)
}
