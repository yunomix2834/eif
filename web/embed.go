package web

import (
	"embed"
	"io/fs"
)

//go:embed all:static
var staticFiles embed.FS

func GetStaticFS() fs.FS {
	staticFS, err := fs.Sub(
		staticFiles,
		"static",
	)
	if err != nil {
		panic(err)
	}
	return staticFS
}
