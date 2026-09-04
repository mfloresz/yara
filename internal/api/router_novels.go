package api

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	pbrouter "github.com/pocketbase/pocketbase/tools/router"
	"translator-server/internal/store"
)

// sharedNovelHandlers exposes the novel resource operations as plain handler
// funcs so the canonical /api/v1/novels routes can register them. Each funcs
// takes the *Server and returns the core handler.
type sharedNovelHandlers struct{}

var sharedNovels = sharedNovelHandlers{}

// listNovels: GET /novels. Supports ?q, ?sort, ?order, ?limit, ?offset (and
// ?page&per_page on v1). Optional filters: ?tag (exact match,
// case/accent-insensitive), ?shared (all|own|shared), ?progress
// (all|translated|completed|ongoing). Invalid filter values fall back to
// "no filter". The ?select= param is accepted as an alias for ?fields=.
func (sharedNovelHandlers) list(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		q := e.Request.URL.Query()
		page, perPage, limit, offset := parsePagination(q)
		fieldsParam := firstQuery(q, "fields")
		if fieldsParam == "" {
			fieldsParam = firstQuery(q, "select")
		}
		searchQuery := firstQuery(q, "q")
		sortParam := firstQuery(q, "sort")
		orderParam := firstQuery(q, "order")
		opts := store.ListNovelOptions{
			Tag:      firstQuery(q, "tag"),
			Shared:   firstQuery(q, "shared"),
			Progress: firstQuery(q, "progress"),
		}

		var list []store.Novel
		var hasMore bool
		var err error

		if searchQuery != "" {
			list, hasMore, err = s.Store.SearchNovels(e.Auth.Id, searchQuery, limit, offset, sortParam, orderParam, opts)
		} else {
			list, hasMore, err = s.Store.ListNovels(e.Auth.Id, limit, offset, sortParam, orderParam, opts)
		}
		if err != nil {
			return e.InternalServerError("failed to list novels", err)
		}
		items := make([]map[string]any, 0, len(list))
		if fieldsParam != "" {
			fields := strings.Split(fieldsParam, ",")
			wantCanUpdate := false
			wantRequiresBrowser := false
			for _, f := range fields {
				field := strings.TrimSpace(f)
				if field == "canUpdate" {
					wantCanUpdate = true
				}
				if field == "requiresBrowser" {
					wantRequiresBrowser = true
				}
			}
			for i := range list {
				item := parseJSONFieldsSubset(&list[i], fields)
				if wantCanUpdate {
					item["canUpdate"] = s.DownloaderFactory(e.Auth.Id).IsSupportedURL(list[i].URL)
				}
				if wantRequiresBrowser {
					item["requiresBrowser"] = s.DownloaderFactory(e.Auth.Id).RequiresBrowser(list[i].URL)
				}
				items = append(items, item)
			}
		} else {
			for i := range list {
				items = append(items, s.novelResponse(&list[i]))
			}
		}
		// v1 uses page/per_page; convert offset-based hasMore to page count.
		total := offset + len(items)
		if hasMore {
			total = offset + len(items) + 1
		}
		return v1RespondList(e, http.StatusOK, items, page, perPage, total, hasMore, e.Request.URL.Path)
	}
}

func (sharedNovelHandlers) create(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		var in struct {
			SourceLang         string `json:"sourceLanguage"`
			TargetLang         string `json:"targetLanguage"`
			SourceTitle        string `json:"sourceTitle"`
			SourceAuthor       string `json:"sourceAuthor"`
			SourceDescription  string `json:"sourceDescription"`
			SourceSeries       string `json:"sourceSeries"`
			SourceNumber       string `json:"sourceNumber"`
			TargetTitle        string `json:"targetTitle"`
			TargetAuthor       string `json:"targetAuthor"`
			TargetDescription  string `json:"targetDescription"`
			TargetSeries       string `json:"targetSeries"`
			TargetNumber       string `json:"targetNumber"`
			Glossary           any    `json:"glossary"`
			Prompts            any    `json:"prompts"`
			Notes              string `json:"notes"`
			AIOptions          any    `json:"aiOptions"`
			TranslationOptions any    `json:"translationOptions"`
			CleanupRules       any    `json:"cleanupRules"`
			URL                string `json:"url"`
			CustomCommands     string `json:"customCommands"`
			Tags               any    `json:"tags"`
		}
		if err := e.BindBody(&in); err != nil {
			return e.BadRequestError("invalid body", err)
		}
		sourceTitle := strings.TrimSpace(in.SourceTitle)
		if sourceTitle == "" {
			return e.BadRequestError("sourceTitle is required", nil)
		}
		sourceAuthor := strings.TrimSpace(in.SourceAuthor)
		sourceDescription := strings.TrimSpace(in.SourceDescription)
		promptOverrides := store.ParseNovelPromptOverrides(in.Prompts)
		novel := &store.Novel{
			SourceLanguage:          in.SourceLang,
			TargetLanguage:          in.TargetLang,
			SourceTitle:             sourceTitle,
			SourceAuthor:            sourceAuthor,
			SourceDescription:       sourceDescription,
			SourceSeries:            in.SourceSeries,
			SourceNumber:            in.SourceNumber,
			TargetTitle:             in.TargetTitle,
			TargetAuthor:            in.TargetAuthor,
			TargetDescription:       in.TargetDescription,
			TargetSeries:            in.TargetSeries,
			TargetNumber:            in.TargetNumber,
			Glossary:                jsonString(in.Glossary, "[]"),
			TranslationSystemPrompt: promptOverrides.Translation.SystemPrompt,
			TranslationUserPrompt:   promptOverrides.Translation.UserPrompt,
			TitleSystemPrompt:       promptOverrides.Title.SystemPrompt,
			TitleUserPrompt:         promptOverrides.Title.UserPrompt,
			RefineSystemPrompt:      promptOverrides.Refine.SystemPrompt,
			RefineUserPrompt:        promptOverrides.Refine.UserPrompt,
			CheckSystemPrompt:       promptOverrides.Check.SystemPrompt,
			CheckUserPrompt:         promptOverrides.Check.UserPrompt,
			Notes:                   in.Notes,
			AIOptions:               jsonString(in.AIOptions, "{}"),
			TranslationOptions:      jsonString(in.TranslationOptions, "{}"),
			CleanupRules:            jsonString(in.CleanupRules, "[]"),
			URL:                     in.URL,
			CustomCommands:          in.CustomCommands,
			Status:                  "ongoing",
			Tags:                    jsonString(in.Tags, "[]"),
		}
		if err := s.Store.CreateNovel(e.Auth.Id, novel); err != nil {
			return e.InternalServerError("failed to create novel", err)
		}
		e.Response.Header().Set("Location", "/api/v1/novels/"+novel.ID)
		return v1Respond(e, http.StatusCreated, s.novelResponse(novel), nil, nil)
	}
}

func (sharedNovelHandlers) get(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		novel, err := s.Store.GetNovelAccessible(e.Auth.Id, e.Request.PathValue("id"))
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		q := e.Request.URL.Query()
		fieldsParam := firstQuery(q, "fields")
		if fieldsParam == "" {
			fieldsParam = firstQuery(q, "select")
		}
		if fieldsParam != "" {
			fields := strings.Split(fieldsParam, ",")
			item := parseJSONFieldsSubset(novel, fields)
			if containsField(fields, "canUpdate") {
				item["canUpdate"] = s.DownloaderFactory(e.Auth.Id).IsSupportedURL(novel.URL)
			}
			if containsField(fields, "requiresBrowser") {
				item["requiresBrowser"] = s.DownloaderFactory(e.Auth.Id).RequiresBrowser(novel.URL)
			}
			return v1Respond(e, http.StatusOK, item, nil, nil)
		}
		return v1Respond(e, http.StatusOK, s.novelResponse(novel), nil, nil)
	}
}

func (sharedNovelHandlers) patch(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		patch := map[string]any{}
		if err := e.BindBody(&patch); err != nil {
			return e.BadRequestError("invalid body", err)
		}
		novel, err := s.Store.UpdateNovel(e.Auth.Id, e.Request.PathValue("id"), patch)
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		return v1Respond(e, http.StatusOK, s.novelResponse(novel), nil, nil)
	}
}

func (sharedNovelHandlers) delete(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		if err := s.Store.DeleteNovel(e.Auth.Id, e.Request.PathValue("id")); err != nil {
			return notFoundOrForbidden(e, err)
		}
		return e.NoContent(http.StatusNoContent)
	}
}

func (sharedNovelHandlers) copyNovel(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		novel, err := s.Store.CopyNovel(e.Auth.Id, e.Request.PathValue("id"))
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		e.Response.Header().Set("Location", "/api/v1/novels/"+novel.ID)
		return v1Respond(e, http.StatusCreated, s.novelResponse(novel), nil, nil)
	}
}

func (sharedNovelHandlers) recalculateStats(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		novel, err := s.Store.GetOwnedNovel(e.Auth.Id, e.Request.PathValue("id"))
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		if err := s.Store.RecalculateNovelStats(novel.ID); err != nil {
			return e.InternalServerError("failed to recalculate stats", err)
		}
		reloaded, err := s.Store.GetOwnedNovel(e.Auth.Id, novel.ID)
		if err != nil {
			return e.InternalServerError("failed to reload novel", err)
		}
		return v1Respond(e, http.StatusOK, s.novelResponse(reloaded), nil, nil)
	}
}

func (sharedNovelHandlers) setVisibility(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		body := struct {
			IsPublic bool `json:"isPublic"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("invalid body", err)
		}
		novel, err := s.Store.SetNovelVisibility(e.Auth.Id, e.Request.PathValue("id"), body.IsPublic)
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		return v1Respond(e, http.StatusOK, s.novelResponse(novel), nil, nil)
	}
}

func (sharedNovelHandlers) cover(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		if err := e.Request.ParseMultipartForm(32 << 20); err != nil {
			return e.BadRequestError("failed to parse form", err)
		}
		file, header, err := e.Request.FormFile("cover")
		if err != nil {
			return e.BadRequestError("cover file required", err)
		}
		defer file.Close()
		blob, err := io.ReadAll(file)
		if err != nil {
			return e.InternalServerError("failed to read file", err)
		}
		mimeType := header.Header.Get("Content-Type")
		if mimeType == "" {
			mimeType = "image/jpeg"
		}
		novel, err := s.Store.UpdateNovelCover(e.Auth.Id, e.Request.PathValue("id"), blob, mimeType)
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		return v1Respond(e, http.StatusOK, s.novelResponse(novel), nil, nil)
	}
}

// coverImage serves the stored cover/thumbnail file. cover and thumbnail are
// Protected file fields, so PocketBase's native /api/files route requires a
// file token; this cookie-authenticated handler is the replacement the
// frontend's coverPath points at.
func (sharedNovelHandlers) coverImage(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		record, fileName, err := s.Store.GetNovelCoverFile(e.Auth.Id, e.Request.PathValue("id"))
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		fsys, err := e.App.NewFilesystem()
		if err != nil {
			return e.InternalServerError("filesystem init failure", err)
		}
		defer fsys.Close()
		// Covers are replaced in place under the same URL; allow short-lived
		// caching but never serve a stale copy for long.
		e.Response.Header().Set("Cache-Control", "private, max-age=60")
		return fsys.Serve(e.Response, e.Request, record.BaseFilesPath()+"/"+fileName, fileName)
	}
}

func (sharedNovelHandlers) full(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		novel, err := s.Store.GetNovelAccessible(e.Auth.Id, e.Request.PathValue("id"))
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		chapters, err := s.Store.ListChaptersAccessible(e.Auth.Id, e.Request.PathValue("id"))
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		return v1Respond(e, http.StatusOK, map[string]any{
			"novel":    s.novelResponse(novel),
			"chapters": chapterRecords(chapters),
		}, nil, nil)
	}
}

func (sharedNovelHandlers) tagSuggestions(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		limit, _ := strconv.Atoi(e.Request.URL.Query().Get("limit"))
		query := e.Request.URL.Query().Get("q")
		tags, err := s.Store.ListNovelTagSuggestions(e.Auth.Id, query, limit)
		if err != nil {
			return e.InternalServerError("failed to list tag suggestions", err)
		}
		return v1RespondList(e, http.StatusOK, tags, 1, len(tags), len(tags), false, e.Request.URL.Path)
	}
}

func (sharedNovelHandlers) seriesSuggestions(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		limit, _ := strconv.Atoi(e.Request.URL.Query().Get("limit"))
		query := e.Request.URL.Query().Get("q")
		series, err := s.Store.ListNovelSeriesSuggestions(e.Auth.Id, query, limit)
		if err != nil {
			return e.InternalServerError("failed to list series suggestions", err)
		}
		return v1RespondList(e, http.StatusOK, series, 1, len(series), len(series), false, e.Request.URL.Path)
	}
}

func containsField(fields []string, target string) bool {
	for _, f := range fields {
		if strings.TrimSpace(f) == target {
			return true
		}
	}
	return false
}

func registerV1NovelRoutes(api *pbrouter.RouterGroup[*core.RequestEvent], s *Server) {
	api.GET("/novels", sharedNovels.list(s))
	api.POST("/novels", sharedNovels.create(s))
	api.GET("/novels/tags/suggestions", sharedNovels.tagSuggestions(s))
	api.GET("/novels/series/suggestions", sharedNovels.seriesSuggestions(s))
	api.GET("/novels/{id}", sharedNovels.get(s))
	api.POST("/novels/{id}/recalculate-stats", sharedNovels.recalculateStats(s))
	api.PATCH("/novels/{id}", sharedNovels.patch(s))
	api.POST("/novels/{id}/cover", sharedNovels.cover(s))
	api.GET("/novels/{id}/cover", sharedNovels.coverImage(s))
	api.DELETE("/novels/{id}", sharedNovels.delete(s))
	api.POST("/novels/{id}/clone", sharedNovels.copyNovel(s))
	api.PATCH("/novels/{id}/visibility", sharedNovels.setVisibility(s))
	api.GET("/novels/{id}/full", sharedNovels.full(s))
}
