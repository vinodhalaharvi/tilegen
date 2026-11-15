//go:build js

package mcpserver

import "io"

// Cmd is the command line's; a page has no standard input to serve on.
func Cmd([]string, io.Writer) error { return nil }
