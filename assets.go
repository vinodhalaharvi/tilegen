package main

import (
	"embed"
	"github.com/vinodhalaharvi/tilegen/internal/serve"
)

// What the binary carries: the studio's page and libraries. They are
// embedded here, at the root of the module, because that is where they
// live; the packages that serve them are given them at start.
//
//go:embed web/src web/vendor
var embeddedStudio embed.FS

func init() { serve.StudioFiles = embeddedStudio }
