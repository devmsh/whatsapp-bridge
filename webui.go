package main

import (
	"embed"
	"io/fs"
)

// webDistFS holds the built React app. The frontend lives in web/ and builds to
// web/dist; `pnpm --dir web build` regenerates it before `go build`.
//
//go:embed all:web/dist
var webDistFS embed.FS

// webUI returns the embedded web UI rooted at the dist directory, or nil if it
// could not be opened (in which case the API still runs without a GUI).
func webUI() fs.FS {
	sub, err := fs.Sub(webDistFS, "web/dist")
	if err != nil {
		return nil
	}
	return sub
}
