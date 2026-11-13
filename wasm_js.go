//go:build js && wasm

package main

import (
	"encoding/json"
	"syscall/js"
)

// In a browser there is no command line: tilegen is one function, taking
// a spec, config and policy as text and returning what the server's
// endpoints return, as JSON. Nothing touches a file system; the compiler
// already works on text and the emitter on a plan.
func wasmMain() bool {
	js.Global().Set("tilegenCompile", js.FuncOf(func(_ js.Value, args []js.Value) any {
		req := specRequest{}
		for i, dst := range []*string{&req.Spec, &req.Config, &req.Policy} {
			if i < len(args) && args[i].Type() == js.TypeString {
				*dst = args[i].String()
			}
		}
		b, _ := json.Marshal(compileAll(req))
		return string(b)
	}))
	js.Global().Set("tilegenTiles", js.FuncOf(func(js.Value, []js.Value) any {
		b, _ := json.Marshal(tilesJSON(Registry()))
		return string(b)
	}))
	js.Global().Set("tilegenGoMods", js.FuncOf(func(_ js.Value, args []js.Value) any {
		var files []string
		for _, a := range args {
			if a.Type() == js.TypeString {
				files = append(files, a.String())
			}
		}
		b, _ := json.Marshal(scanGoMods(files))
		return string(b)
	}))
	js.Global().Set("tilegenVersion", version)
	if ready := js.Global().Get("tilegenReady"); ready.Type() == js.TypeFunction {
		ready.Invoke()
	}
	select {} // stay loaded: the page calls in
}
