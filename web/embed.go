// Package web holds the compiled front ends embedded in the binary.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:builder/dist
var builder embed.FS

// Builder returns the built builder files.
func Builder() fs.FS {
	sub, err := fs.Sub(builder, "builder/dist")
	if err != nil {
		panic(err)
	}
	return sub
}
