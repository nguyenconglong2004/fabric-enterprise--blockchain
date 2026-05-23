// Package web embeds the compiled Vite frontend (web/dist/) for the orchestrator binary.
// Run `npm run build` inside this directory to populate dist/ before building the orchestrator.
package web

import "embed"

//go:embed all:dist
var Dist embed.FS
