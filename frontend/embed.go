package frontend

import "embed"

// Assets contains the production React bundle. Running `npm run build` in
// frontend refreshes these files before the Go application is compiled.
//
//go:embed dist/* dist/assets/*
var Assets embed.FS
