//go:build js

package main

import "io"

// In a browser there is no keychain and no key: asking Claude is a thing
// the server does.
func anthropicKey() (key, from string) { return "", "" }

// keyCmd is the command line's; a page has none.
func keyCmd([]string, io.Reader, io.Writer) error { return nil }
