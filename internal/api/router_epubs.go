package api

import (
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/pocketbase/pocketbase/core"
	pbrouter "github.com/pocketbase/pocketbase/tools/router"
	"translator-server/internal/epubimport"
	"translator-server/internal/store"
)

type sharedEpubHandlers struct{}

var sharedEpubs = sharedEpubHandlers{}

// previewEpub: POST /epubs:preview — parse the uploaded file and return its
// metadata + chapter titles; do not persist.
func (sharedEpubHandlers) preview(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		if err := e.Request.ParseMultipartForm(64 << 20); err != nil {
			return e.BadRequestError("invalid multipart", err)
		}
		file, header, err := e.Request.FormFile("file")
		if err != nil {
			return e.BadRequestError("file required", err)
		}
		defer file.Close()
		blob, err := io.ReadAll(file)
		if err != nil {
			return e.InternalServerError("failed to read file", err)
		}
		parsed, err := epubimport.Parse(blob, header.Filename)
		if err != nil {
			return e.BadRequestError("parse error", err)
		}
		chapters := make([]map[string]any, len(parsed.Chapters))
		for i, ch := range parsed.Chapters {
			chapters[i] = map[string]any{"title": ch.Title, "content": ch.Content}
		}
		body := map[string]any{
			"title": parsed.Title, "author": parsed.Author, "description": parsed.Description,
			"language": parsed.Language, "series": parsed.Series, "number": parsed.Number,
			"chapters": chapters,
		}
		return v1Respond(e, http.StatusOK, body, nil, nil)
	}
}

func (sharedEpubHandlers) list(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		novelID := e.Request.URL.Query().Get("novelId")
		items, err := s.Store.ListEpubs(e.Auth.Id, novelID)
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		out := make([]map[string]any, 0, len(items))
		for _, item := range items {
			out = append(out, epubRecord(item))
		}
		return v1RespondList(e, http.StatusOK, out, 1, len(out), len(out), false, e.Request.URL.Path)
	}
}

func (sharedEpubHandlers) create(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		if err := e.Request.ParseMultipartForm(64 << 20); err != nil {
			return e.BadRequestError("invalid multipart", err)
		}
		novelID := e.Request.FormValue("novelId")
		f, h, err := e.Request.FormFile("file")
		if err != nil {
			return e.BadRequestError("file required", err)
		}
		defer f.Close()
		blob, err := io.ReadAll(f)
		if err != nil {
			return e.InternalServerError("failed to read file", err)
		}
		item, err := s.Store.UpsertEpub(e.Auth.Id, &store.Epub{NovelID: novelID, FileKind: e.Request.FormValue("fileKind"), SourceVariant: e.Request.FormValue("sourceVariant"), Label: e.Request.FormValue("label")}, h.Filename, h.Header.Get("Content-Type"), blob)
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		body := epubRecord(*item)
		e.Response.Header().Set("Location", "/api/v1/epubs/"+item.ID+"/download")
		return v1Respond(e, http.StatusCreated, body, nil, nil)
	}
}

func (sharedEpubHandlers) download(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		// Exports are one-shot: the first download consumes and deletes the
		// stored copy (see TakeEpubFile), so caching is meaningless anyway.
		blob, fileName, err := s.Store.TakeEpubFile(e.Auth.Id, e.Request.PathValue("id"))
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		e.Response.Header().Set("Cache-Control", "no-store")
		e.Response.Header().Set("Content-Type", "application/epub+zip")
		e.Response.Header().Set("Content-Length", strconv.Itoa(len(blob)))
		e.Response.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": fileName}))
		_, err = e.Response.Write(blob)
		return err
	}
}

func registerV1EpubRoutes(api *pbrouter.RouterGroup[*core.RequestEvent], s *Server) {
	api.POST("/epubs/preview", sharedEpubs.preview(s))
	api.GET("/epubs", sharedEpubs.list(s))
	// /novels/{id}/epubs is the canonical collection path.
	api.GET("/novels/{id}/epubs", sharedEpubs.list(s))
	api.POST("/novels/{id}/epubs", sharedEpubs.create(s))
	api.POST("/epubs", sharedEpubs.create(s))
	api.GET("/epubs/{id}/download", sharedEpubs.download(s))
}
