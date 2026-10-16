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
		// Unknown /api/* paths must never fall back to the SPA: that turns
		// every scanner probe (/api/openapi.yaml, /api/_ ...) into a 200
		// and hides real 404s behind index.html.
		if strings.HasPrefix(e.Request.URL.Path, "/api/") || e.Request.URL.Path == "/api" {
			return e.NotFoundError("", nil)
		}
		filename := e.Request.PathValue("path")
		filename = path.Clean(strings.TrimPrefix(filename, "/"))
		if filename == "" || filename == "." {
			filename = "index.html"
		}

		// Dotfiles/dot-dirs (/.git/HEAD, /.env, /.well-known/...) are never
		// part of the SPA bundle. 404 them instead of serving index.html.
		if strings.HasPrefix(filename, ".") || strings.Contains(filename, "/.") {
			return e.NotFoundError("", nil)
		}

		if ext := path.Ext(filename); ext != "" {
			f, err := fs.Stat(fsys, filename)
			if err == nil && !f.IsDir() {
				if strings.HasPrefix(filename, "assets/") {
					// Hashed build output: the filename is the version.
					e.Response.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					// Unhashed files (sw.js, theme-init.js, manifest, icons):
					// always revalidate so a new deploy takes effect without
					// a hard refresh. sw.js as immutable pinned the old
					// service worker forever.
					e.Response.Header().Set("Cache-Control", "no-cache")
				}
				return e.FileFS(fsys, filename)
			}
			// A path with an extension that is not a real bundled asset
			// (/openapi.json, /favicon-xyz.png, ...) is a miss, not the SPA.
			return e.NotFoundError("", nil)
		}

		// index.html is the chunk manifest: it points at hashed assets that
		// only exist in the current build, so it must never be cached.
		e.Response.Header().Set("Cache-Control", "no-cache")
		return e.FileFS(fsys, "index.html")
	}
	router.GET("/{path...}", static)
	router.GET("/{$}", static)
}
