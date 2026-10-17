package web

import (
	"embed"
	"io/fs"
)

// Build the frontend before building the Go application.
//
//go:embed dist
var assets embed.FS

func Assets() fs.FS {
	files, err := fs.Sub(assets, "dist")
	if err != nil {
		panic(err)
	}
	return files
}
