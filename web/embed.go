// Package web embeds the built frontend (web/dist, produced by `make web`).
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS returns the built UI rooted at index.html.
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}

// Built reports whether the frontend was embedded (a plain `go build`
// without `make web` produces a binary without it).
func Built() bool {
	_, err := fs.Stat(FS(), "index.html")
	return err == nil
}
