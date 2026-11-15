//go:build js

package serve

import "io"

// In a browser there is no keychain and no key: asking Claude is a thing
// the server does.
func anthropicKey() (key, from string) { return "", "" }

// KeyCmd is the command line's; a page has none.
func KeyCmd([]string, io.Reader, io.Writer) error { return nil }
