package api

import (
	"encoding/json"
	"strings"

	"translator-server/internal/store"
)

func coverResponsePath(n *store.Novel) string {
	if n.ThumbnailPath != "" {
		return n.ThumbnailPath
	}
	return n.CoverPath
}

func parseJSONFields(n *store.Novel) map[string]any {
	m := map[string]any{
		"id": n.ID, "ownerId": n.OwnerID,
		"sourceLanguage": n.SourceLanguage, "targetLanguage": n.TargetLanguage,
		"sourceTitle": n.SourceTitle, "sourceAuthor": n.SourceAuthor, "sourceDescription": n.SourceDescription,
		"sourceSeries": n.SourceSeries, "sourceNumber": n.SourceNumber,
		"targetTitle": n.TargetTitle, "targetAuthor": n.TargetAuthor, "targetDescription": n.TargetDescription,
		"targetSeries": n.TargetSeries, "targetNumber": n.TargetNumber,
		"url": n.URL, "customCommands": n.CustomCommands, "status": n.Status, "coverPath": coverResponsePath(n),
		"isPublic":     n.IsPublic,
		"chapterCount": n.ChapterCount, "translatedCount": n.TranslatedCount, "completedCount": n.CompletedCount,
		"originalCharCount": n.OriginalCharCount, "translatedCharCount": n.TranslatedCharCount,
		"refinedCharCount": n.RefinedCharCount, "totalCharCount": n.TotalCharCount,
		"maxChapterOrder": n.MaxChapterOrder, "lastCheckedAt": n.LastCheckedAt,
		"lastCheckNewChapters": n.LastCheckNewChapters, "lastReadAt": n.LastReadAt,
		"readingStatus": n.ReadingStatus,
		"createdAt": n.CreatedAt, "updatedAt": n.UpdatedAt,
	}
	var gl, tags, aio, tro, cr any
	_ = json.Unmarshal([]byte(n.Glossary), &gl)
	_ = json.Unmarshal([]byte(n.Tags), &tags)
	_ = json.Unmarshal([]byte(n.AIOptions), &aio)
	_ = json.Unmarshal([]byte(n.TranslationOptions), &tro)
	_ = json.Unmarshal([]byte(n.CleanupRules), &cr)
	m["glossary"] = gl
	m["tags"] = tags
	m["prompts"] = store.BuildNovelPromptOverridesMap(n)
	m["aiOptions"] = aio
	m["translationOptions"] = tro
	m["cleanupRules"] = cr
	m["notes"] = n.Notes
	return m
}

// novelResponse shapes a novel for the frontend and adds whether it can be
// updated from its source URL. The check is server-authoritative: it uses the
// same parser catalog that backs the check/redownload jobs, so the frontend
// does not need to duplicate the list of supported domains.
func (s *Server) novelResponse(n *store.Novel) map[string]any {
	m := parseJSONFields(n)
	m["canUpdate"] = s.parserSupportsURL(n.OwnerID, n.URL)
	m["requiresBrowser"] = s.parserURLRequiresBrowser(n.OwnerID, n.URL)
	return m
}

func parseJSONFieldsSubset(n *store.Novel, fields []string) map[string]any {
	m := make(map[string]any, len(fields))
	for _, f := range fields {
		f = strings.TrimSpace(f)
		switch f {
		case "id":
			m["id"] = n.ID
		case "ownerId":
			m["ownerId"] = n.OwnerID
		case "sourceLanguage":
			m["sourceLanguage"] = n.SourceLanguage
		case "targetLanguage":
			m["targetLanguage"] = n.TargetLanguage
		case "sourceTitle":
			m["sourceTitle"] = n.SourceTitle
		case "sourceAuthor":
			m["sourceAuthor"] = n.SourceAuthor
		case "sourceDescription":
			m["sourceDescription"] = n.SourceDescription
		case "sourceSeries":
			m["sourceSeries"] = n.SourceSeries
		case "sourceNumber":
			m["sourceNumber"] = n.SourceNumber
		case "targetTitle":
			m["targetTitle"] = n.TargetTitle
		case "targetAuthor":
			m["targetAuthor"] = n.TargetAuthor
		case "targetDescription":
			m["targetDescription"] = n.TargetDescription
		case "targetSeries":
			m["targetSeries"] = n.TargetSeries
		case "targetNumber":
			m["targetNumber"] = n.TargetNumber
		case "url":
			m["url"] = n.URL
		case "customCommands":
			m["customCommands"] = n.CustomCommands
		case "status":
			m["status"] = n.Status
		case "coverPath":
			m["coverPath"] = coverResponsePath(n)
		case "isPublic":
			m["isPublic"] = n.IsPublic
		case "chapterCount":
			m["chapterCount"] = n.ChapterCount
		case "translatedCount":
			m["translatedCount"] = n.TranslatedCount
		case "completedCount":
			m["completedCount"] = n.CompletedCount
		case "originalCharCount":
			m["originalCharCount"] = n.OriginalCharCount
		case "translatedCharCount":
			m["translatedCharCount"] = n.TranslatedCharCount
		case "refinedCharCount":
			m["refinedCharCount"] = n.RefinedCharCount
		case "totalCharCount":
			m["totalCharCount"] = n.TotalCharCount
		case "maxChapterOrder":
			m["maxChapterOrder"] = n.MaxChapterOrder
		case "lastCheckedAt":
			m["lastCheckedAt"] = n.LastCheckedAt
		case "lastCheckNewChapters":
			m["lastCheckNewChapters"] = n.LastCheckNewChapters
		case "lastReadAt":
			m["lastReadAt"] = n.LastReadAt
		case "readingStatus":
			m["readingStatus"] = n.ReadingStatus
		case "createdAt":
			m["createdAt"] = n.CreatedAt
		case "updatedAt":
			m["updatedAt"] = n.UpdatedAt
		case "glossary":
			var v any
			_ = json.Unmarshal([]byte(n.Glossary), &v)
			m["glossary"] = v
		case "tags":
			var v any
			_ = json.Unmarshal([]byte(n.Tags), &v)
			m["tags"] = v
		case "prompts":
			m["prompts"] = store.BuildNovelPromptOverridesMap(n)
		case "aiOptions":
			var v any
			_ = json.Unmarshal([]byte(n.AIOptions), &v)
			m["aiOptions"] = v
		case "translationOptions":
			var v any
			_ = json.Unmarshal([]byte(n.TranslationOptions), &v)
			m["translationOptions"] = v
		case "cleanupRules":
			var v any
			_ = json.Unmarshal([]byte(n.CleanupRules), &v)
			m["cleanupRules"] = v
		case "notes":
			m["notes"] = n.Notes
		}
	}
	return m
}

func promptToResponse(p store.Prompt) map[string]any {
	return map[string]any{
		"id":          p.Key,
		"key":         p.Key,
		"label":       p.Label,
		"description": p.Description,
		"prompt": map[string]any{
			"systemPrompt": p.SystemPrompt,
			"userPrompt":   p.UserPrompt,
		},
		"active":    p.Active == 1,
		"updatedAt": p.UpdatedAt,
	}
}

func promptsToResponse(items []store.Prompt) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, promptToResponse(item))
	}
	return out
}

func chapterRecord(c store.Chapter) map[string]any {
	return map[string]any{
		"id":                c.ID,
		"novelId":           c.NovelID,
		"chapterOrder":      c.ChapterOrder,
		"position":          c.Position,
		"excluded":          c.Excluded,
		"title":             c.Title,
		"translatedTitle":   c.TranslatedTitle,
		"originalContent":   c.OriginalContent,
		"translatedContent": c.TranslatedContent,
		"refinedContent":    c.RefinedContent,
		"status":            c.Status,
		"errorMessage":      c.ErrorMessage,
		"createdAt":         c.CreatedAt,
		"updatedAt":         c.UpdatedAt,
	}
}

func chapterRecords(chapters []store.Chapter) []map[string]any {
	result := make([]map[string]any, len(chapters))
	for i, c := range chapters {
		result[i] = chapterRecord(c)
	}
	return result
}

// imageRecord shapes a stored inline chapter image. url points at the
// authenticated serve route; the frontend renders it directly in <img> tags.
func imageRecord(novelID string, i store.NovelImage) map[string]any {
	return map[string]any{
		"id":        i.ID,
		"chapterId": i.ChapterID,
		"token":     i.ImageToken(),
		"num":       i.Num,
		"alt":       i.Alt,
		"mime":      i.MimeType,
		"url":       "/api/v1/novels/" + novelID + "/images/" + i.ID,
	}
}

func imageRecords(novelID string, images []store.NovelImage) []map[string]any {
	out := make([]map[string]any, 0, len(images))
	for _, i := range images {
		out = append(out, imageRecord(novelID, i))
	}
	return out
}

// withImages attaches the novel's inline images relevant to a full chapter
// record, resolved against the chapter's [[IMG-n]] tokens. An empty list
// omits the key entirely so tokenless chapters keep the exact response shape
// they always had.
func withImages(chapter map[string]any, novelID string, images []store.NovelImage) map[string]any {
	if len(images) == 0 {
		return chapter
	}
	chapterID, _ := chapter["id"].(string)
	chapterImages := make([]store.NovelImage, 0, len(images))
	for _, i := range images {
		if i.ChapterID == chapterID {
			chapterImages = append(chapterImages, i)
		}
	}
	if len(chapterImages) == 0 {
		return chapter
	}
	chapter["images"] = imageRecords(novelID, chapterImages)
	return chapter
}

func chapterSummaryRecord(c store.ChapterSummary) map[string]any {
	return map[string]any{
		"id":                   c.ID,
		"novelId":              c.NovelID,
		"chapterOrder":         c.ChapterOrder,
		"position":             c.Position,
		"excluded":             c.Excluded,
		"title":                c.Title,
		"translatedTitle":      c.TranslatedTitle,
		"status":               c.Status,
		"errorMessage":         c.ErrorMessage,
		"hasOriginalContent":   c.HasOriginalContent,
		"hasTranslatedContent": c.HasTranslatedContent,
		"hasRefinedContent":    c.HasRefinedContent,
		"originalChars":        c.OriginalChars,
		"translatedChars":      c.TranslatedChars,
		"refinedChars":         c.RefinedChars,
		"createdAt":            c.CreatedAt,
		"updatedAt":            c.UpdatedAt,
	}
}

func chapterStatsRecord(s *store.ChapterStats) map[string]any {
	if s == nil {
		return map[string]any{
			"totalChapters":        0,
			"completedChapters":    0,
			"translatedChapters":   0,
			"originalCharacters":   0,
			"translatedCharacters": 0,
			"refinedCharacters":    0,
			"totalCharacters":      0,
			"maxChapterOrder":      0,
		}
	}
	return map[string]any{
		"totalChapters":        s.TotalChapters,
		"completedChapters":    s.CompletedChapters,
		"translatedChapters":   s.TranslatedChapters,
		"originalCharacters":   s.OriginalCharacters,
		"translatedCharacters": s.TranslatedCharacters,
		"refinedCharacters":    s.RefinedCharacters,
		"totalCharacters":      s.TotalCharacters,
		"maxChapterOrder":      s.MaxChapterOrder,
	}
}

func jobRecord(j store.Job) map[string]any {
	chapterIDs := []string{}
	if trimmed := strings.TrimSpace(j.ChapterIDs); trimmed != "" {
		if err := json.Unmarshal([]byte(trimmed), &chapterIDs); err != nil {
			chapterIDs = []string{}
		}
	}
	return map[string]any{
		"id":                        j.ID,
		"novelId":                   j.NovelID,
		"status":                    j.Status,
		"operation":                 j.Operation,
		"provider":                  j.Provider,
		"model":                     j.Model,
		"totalChapters":             j.TotalChapters,
		"completedChapters":         j.CompletedChapters,
		"failedChapters":            j.FailedChapters,
		"errorMessage":              j.ErrorMessage,
		"createdAt":                 j.CreatedAt,
		"updatedAt":                 j.UpdatedAt,
		"novelTitle":                j.NovelTitle,
		"chapterIds":                chapterIDs,
		"autoSegmentEnabled":        j.AutoSegmentEnabled,
		"autoSegmentActive":         j.AutoSegmentActive,
		"autoSegmentCount":          j.AutoSegmentCount,
		"autoSegmentCurrentIndex":   j.AutoSegmentCurrentIndex,
		"autoSegmentCompletedCount": j.AutoSegmentCompletedCount,
		"autoSegmentChapterId":      j.AutoSegmentChapterID,
		"autoSegmentChapterTitle":   j.AutoSegmentChapterTitle,
		"newChapters":               j.NewChapters,
	}
}

func readingProgressRecord(rp store.ReadingProgress) map[string]any {
	return map[string]any{
		"id":            rp.ID,
		"userId":        rp.UserID,
		"novelId":       rp.NovelID,
		"chapterId":     rp.ChapterID,
		"scrollPercent": rp.ScrollPercent,
		"status":        rp.Status,
		"createdAt":     rp.CreatedAt,
		"updatedAt":     rp.UpdatedAt,
	}
}

func epubRecord(e store.Epub) map[string]any {
	return map[string]any{
		"id":            e.ID,
		"novelId":       e.NovelID,
		"fileKind":      e.FileKind,
		"sourceVariant": e.SourceVariant,
		"label":         e.Label,
		"fileName":      e.FileName,
		"url":           e.URL,
		"createdAt":     e.CreatedAt,
		"updatedAt":     e.UpdatedAt,
	}
}

// ---------------------------------------------------------------------------
// Comics shapers (mirror the novel shapers for the comics media tree).
// ---------------------------------------------------------------------------

func (s *Server) comicResponse(c *store.Comic) map[string]any {
	var tags any
	if err := json.Unmarshal([]byte(c.Tags), &tags); err != nil {
		tags = []any{}
	}
	var sourceDescription any
	if strings.TrimSpace(c.SourceDescription) != "" {
		sourceDescription = c.SourceDescription
	}
	var targetDescription any
	if strings.TrimSpace(c.TargetDescription) != "" {
		targetDescription = c.TargetDescription
	}
	m := map[string]any{
		"id": c.ID, "ownerId": c.OwnerID,
		"sourceLanguage": c.SourceLanguage, "targetLanguage": c.TargetLanguage,
		"sourceTitle": c.SourceTitle, "sourceAuthor": c.SourceAuthor, "sourceDescription": sourceDescription,
		"sourceSeries": c.SourceSeries, "sourceNumber": c.SourceNumber,
		"targetTitle": c.TargetTitle, "targetAuthor": c.TargetAuthor, "targetDescription": targetDescription,
		"targetSeries": c.TargetSeries, "targetNumber": c.TargetNumber,
		"notes": c.Notes, "url": c.URL, "status": c.Status, "tags": tags,
		"coverPath": c.CoverPath, "thumbnailPath": c.ThumbnailPath,
		"isPublic":     c.IsPublic,
		"chapterCount": c.ChapterCount, "downloadedCount": c.DownloadedCount,
		"maxChapterOrder": c.MaxChapterOrder,
		"lastCheckedAt":   c.LastCheckedAt, "lastCheckNewChapters": c.LastCheckNewChapters,
		"lastReadAt": c.LastReadAt, "readingStatus": c.ReadingStatus,
		"createdAt":  c.CreatedAt, "updatedAt": c.UpdatedAt,
	}
	m["canUpdate"] = s.parserSupportsURL(c.OwnerID, c.URL)
	m["requiresBrowser"] = s.parserURLRequiresBrowser(c.OwnerID, c.URL)
	return m
}

// comicChapterSummaryRecord shapes one chapter list entry: no page bodies,
// just the counters and status the list renders.
func comicChapterSummaryRecord(ch store.ComicChapterSummary) map[string]any {
	return map[string]any{
		"id":           ch.ID,
		"comicId":      ch.ComicID,
		"chapterOrder": ch.ChapterOrder,
		"position":     ch.Position,
		"excluded":     ch.Excluded,
		"title":        ch.Title,
		"status":       ch.Status,
		"errorMessage": ch.ErrorMessage,
		"pageCount":    ch.PageCount,
		"hasPages":     ch.HasPages,
		"createdAt":    ch.CreatedAt,
		"updatedAt":    ch.UpdatedAt,
	}
}

func comicChapterSummaryRecords(chapters []store.ComicChapterSummary) []map[string]any {
	out := make([]map[string]any, 0, len(chapters))
	for _, ch := range chapters {
		out = append(out, comicChapterSummaryRecord(ch))
	}
	return out
}

// comicChapterRecord shapes a full chapter with its pages (the comic analogue
// of the novel chapter's content bodies).
func comicChapterRecord(ch store.ComicChapter, pages []store.ComicPage) map[string]any {
	m := comicChapterSummaryRecord(store.ComicChapterSummary{
		ID: ch.ID, ComicID: ch.ComicID, ChapterOrder: ch.ChapterOrder,
		Position: ch.Position, Excluded: ch.Excluded, Title: ch.Title,
		Status: ch.Status, ErrorMessage: ch.ErrorMessage,
		PageCount: ch.PageCount, HasPages: ch.HasPages,
		CreatedAt: ch.CreatedAt, UpdatedAt: ch.UpdatedAt,
	})
	m["sourceKey"] = ch.SourceKey
	m["pages"] = comicPageRecords(pages)
	return m
}

func comicPageRecords(pages []store.ComicPage) []map[string]any {
	out := make([]map[string]any, 0, len(pages))
	for _, p := range pages {
		out = append(out, map[string]any{
			"id":        p.ID,
			"chapterId": p.ChapterID,
			"comicId":   p.ComicID,
			"pageOrder": p.PageOrder,
			"sourceUrl": p.SourceURL,
			"url":       p.URL,
		})
	}
	return out
}

func comicChapterStatsRecord(stats *store.ComicChapterStats) map[string]any {
	return map[string]any{
		"totalChapters":      stats.TotalChapters,
		"downloadedChapters": stats.DownloadedChapters,
		"failedChapters":     stats.FailedChapters,
		"maxChapterOrder":    stats.MaxChapterOrder,
	}
}
