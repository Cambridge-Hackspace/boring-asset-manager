//go:build js && wasm

// Command bamlive is the template editor's live renderer: internal/live,
// compiled to WebAssembly and run in a Web Worker in the browser. It sets
// globalThis.bamlive to an object with:
//
//	addImage(id, bytes)                        -> null, or an error message
//	hasImage(id)                               -> whether it was added
//	render(template, data, placeholders)       -> {width, height, pixels, description, problems}
//
// where template and data are JSON strings, pixels a Uint8ClampedArray of
// RGBA dots, and description and problems JSON strings (or null).
//
// tools/web/build.sh builds it into the web interface.
package main

import (
	"encoding/json"
	"syscall/js"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/live"
)

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
	api.Set("render", js.FuncOf(func(_ js.Value, args []js.Value) any {
		res := r.Render([]byte(args[0].String()), []byte(args[1].String()), args[2].Bool())
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
