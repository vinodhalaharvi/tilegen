//go:build js

package main

import "io"

// mcpCmd is the command line's; a page has no standard input to serve on.
func mcpCmd([]string, io.Writer) error { return nil }
