package api

import (
	"github.com/pocketbase/pocketbase/core"
	pbrouter "github.com/pocketbase/pocketbase/tools/router"
	"translator-server/internal/store"
)

func registerV1NovelImageRoutes(api *pbrouter.RouterGroup[*core.RequestEvent], s *Server) {
	api.GET("/novels/{id}/images/{imageId}", novelImage(s))
}

// novelImage serves one stored inline chapter image. The file field is
// Protected, so PocketBase's native /api/files route would demand a file
// token; this authenticated handler is the replacement the image URLs in
// chapter responses point at.
func novelImage(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		novelID := e.Request.PathValue("id")
		if _, err := s.Store.GetNovelAccessible(e.Auth.Id, novelID); err != nil {
			return notFoundOrForbidden(e, err)
		}
		record, fileName, err := s.Store.GetNovelImageFile(novelID, e.Request.PathValue("imageId"))
		if err != nil {
			if err == store.ErrNotFound {
				return e.NotFoundError("image not found", nil)
			}
			return notFoundOrForbidden(e, err)
		}
		fsys, err := e.App.NewFilesystem()
		if err != nil {
			return e.InternalServerError("filesystem init failure", err)
		}
		defer fsys.Close()
		// Chapter images are immutable after import, so they can be cached
		// long without any invalidation token.
		e.Response.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
		return fsys.Serve(e.Response, e.Request, record.BaseFilesPath()+"/"+fileName, fileName)
	}
}
