//go:build js

package main

import "io"

// symbolsCmd is the command line's; a page has no go tool to ask.
func symbolsCmd([]string, io.Writer, io.Writer) error { return nil }

// readSymbols is nil in a page: there is no go tool to resolve a module with.
var readSymbols func(mod, version string) (*ModuleSymbols, error)
