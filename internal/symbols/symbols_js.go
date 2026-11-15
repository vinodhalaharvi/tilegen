//go:build js

package symbols

import "io"

// Cmd is the command line's; a page has no go tool to ask.
func Cmd([]string, io.Writer, io.Writer) error { return nil }

// Read is nil in a page: there is no go tool to resolve a module with.
var Read func(mod, version string) (*ModuleSymbols, error)
