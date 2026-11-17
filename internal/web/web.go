// SPDX-License-Identifier: GPL-3.0-only

package web

import (
	"embed"
	"io/fs"
)

// The frontend build writes into static/. The directory is kept in the repository by
// its placeholder file (see spa.go), because //go:embed needs at least one file and
// the backend jobs (vet, test, build) run before the frontend is built.
//
//go:embed static
var content embed.FS

// FS returns the embedded frontend static files.
func FS() fs.FS {
	sub, err := fs.Sub(content, "static")
	if err != nil {
		panic(err)
	}
	return sub
}
