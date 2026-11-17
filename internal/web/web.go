// SPDX-License-Identifier: GPL-3.0-only

package web

import (
	"embed"
	"io/fs"
)

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
