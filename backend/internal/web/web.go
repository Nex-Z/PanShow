package web

import "embed"

// Dist contains the compiled frontend assets copied from frontend/dist before a production build.
//
//go:embed dist
var Dist embed.FS
