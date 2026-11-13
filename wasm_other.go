//go:build !(js && wasm)

package main

// wasmMain reports whether tilegen is running in a browser, where it
// serves the page instead of a command line. Here it is not.
func wasmMain() bool { return false }
