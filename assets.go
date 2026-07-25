package assets

import (
	"embed"
	"io/fs"
)

//go:embed web
var embedded embed.FS

func WebFS() fs.FS {
	web, err := fs.Sub(embedded, "web")
	if err != nil {
		panic(err)
	}
	return web
}
