//go:build !(js && wasm)

// Command bamlive is the template editor's live renderer, and runs only in
// the browser, as WebAssembly: see main_js.go.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "bamlive runs in the browser: build it with GOOS=js GOARCH=wasm (tools/web/build.sh does)")
	os.Exit(2)
}
