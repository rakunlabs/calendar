package ui

import "embed"

// Dist is built with pnpm build before compiling the Go service.
//
//go:embed all:dist
var Dist embed.FS
