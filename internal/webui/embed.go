package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:static
var embedded embed.FS

func staticFS() fs.FS {
	sub, err := fs.Sub(embedded, "static")
	if err != nil {
		return embedded
	}
	return sub
}

func readIndex() ([]byte, error) {
	return embedded.ReadFile("static/index.html")
}
