package api

import (
	"io/fs"
	"os"
	"path"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	pbrouter "github.com/pocketbase/pocketbase/tools/router"
	translatorserver "translator-server"
)

func registerStaticHandler(router *pbrouter.Router[*core.RequestEvent], staticDir string) {
	var fsys fs.FS
	if staticDir != "" {
		fsys = os.DirFS(staticDir)
	} else {
		sub, err := fs.Sub(translatorserver.FrontendFS, "frontend/dist")
		if err != nil {
			panic(err)
		}
		fsys = sub
	}
	static := func(e *core.RequestEvent) error {
		filename := e.Request.PathValue("path")
		filename = path.Clean(strings.TrimPrefix(filename, "/"))
		if filename == "" || filename == "." {
			filename = "index.html"
		}

		if ext := path.Ext(filename); ext != "" {
			f, err := fs.Stat(fsys, filename)
			if err == nil && !f.IsDir() {
				if strings.HasSuffix(filename, ".js") || strings.HasSuffix(filename, ".css") {
					e.Response.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				return e.FileFS(fsys, filename)
			}
		}

		return e.FileFS(fsys, "index.html")
	}
	router.GET("/{path...}", static)
	router.GET("/{$}", static)
}
