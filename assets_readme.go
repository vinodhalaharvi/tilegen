//go:build !js

package main

import (
	_ "embed"
	"github.com/vinodhalaharvi/tilegen/internal/mcpserver"
)

// The README is what the MCP server's reference tool answers from. It is
// embedded here, at the root of the module, because that is where it
// lives; the server is given it at start.
//
//go:embed README.md
var embeddedReadme string

func init() { mcpserver.Readme = embeddedReadme }
