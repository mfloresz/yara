package api

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	pbrouter "github.com/pocketbase/pocketbase/tools/router"
	"translator-server/internal/epubexport"
	"translator-server/internal/epubimport"
	"translator-server/internal/store"
)

func registerV1EpubExportRoutes(api *pbrouter.RouterGroup[*core.RequestEvent], s *Server) {
	api.POST("/epubs/build", buildEpubHandler(s))
}

// errEpubNoContent marks a source variant with no exportable chapter content;
// the HTTP handler renders it 400, the agent tool as tool error.
var errEpubNoContent = errors.New("no chapters with content for the selected source")

// buildEpubForNovel generates and stores the EPUB export of the novel for the
// requested source variant (original|translated|refined — validated by the
// caller). Shared by POST /epubs/build and the agent's build_epub tool.
//
// It materializes every chapter body plus the cover and the generated file
// before the epub is stored, so callers bound the input first: the agent's
// build_epub tool refuses novels whose estimated text exceeds its size
// ceiling, while the HTTP handler keeps the unbounded UI behavior.
func (s *Server) buildEpubForNovel(userID, novelID, source string) (*store.Epub, error) {
	novel, err := s.Store.GetOwnedNovel(userID, novelID)
	if err != nil {
		return nil, err
	}

	chapters, err := s.Store.ListChaptersAccessible(userID, novelID)
	if err != nil {
		return nil, err
	}

	meta := buildEpubMeta(novel, source)

	var coverBytes []byte
	var coverMime string
	if novel.CoverFile != "" {
		if novelRecord, nErr := s.Store.App.FindRecordById(store.NovelsCollection, novel.ID); nErr == nil {
			fsys, fErr := s.Store.App.NewFilesystem()
			if fErr == nil {
				defer fsys.Close()
				fileKey := novelRecord.BaseFilesPath() + "/" + novel.CoverFile
				if reader, rErr := fsys.GetReader(fileKey); rErr == nil {
					defer reader.Close()
					if data, bErr := epubexport.ReadCloserToBytes(reader); bErr == nil {
						coverBytes = data
						coverMime = epubexport.DetectImageMime(coverBytes)
					} else {
						slog.Warn("read novel cover for epub export", "novel", novel.ID, "error", bErr)
					}
				}
			}
		}
	}
	// Novels without a stored cover export with the bundled default so
	// the EPUB always has a portada instead of none.
	if len(coverBytes) == 0 {
		coverBytes = DefaultCoverBytes
		coverMime = DefaultCoverMime
	}

	epubChapters, err := buildEpubChapters(chapters, source, novelID, s)
	if err != nil {
		return nil, newImportStoreError("failed to load novel images", err)
	}
	if len(epubChapters) == 0 {
		return nil, errEpubNoContent
	}

	epubBytes, err := epubexport.GenerateEpubFile(meta, epubChapters, coverBytes, coverMime)
	if err != nil {
		return nil, newImportStoreError("failed to generate epub", err)
	}

	fileName := sanitizeFileName(meta.Title) + ".epub"
	fileKind := "translated"
	if source == "original" {
		fileKind = "original"
	}
	sourceVariant := source

	item, err := s.Store.UpsertEpub(userID, &store.Epub{
		NovelID:       novelID,
		FileKind:      fileKind,
		SourceVariant: sourceVariant,
		Label:         fmt.Sprintf("source=%s", sourceVariant),
	}, fileName, "application/epub+zip", epubBytes)
	if err != nil {
		return nil, err
	}
	return item, nil
}

func buildEpubHandler(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		body := struct {
			NovelID string `json:"novelId"`
			Source  string `json:"source"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("invalid body", err)
		}

		body.Source = strings.ToLower(strings.TrimSpace(body.Source))
		switch body.Source {
		case "original", "translated", "refined":
		default:
			return e.BadRequestError("source must be original, translated, or refined", nil)
		}

		item, err := s.buildEpubForNovel(e.Auth.Id, body.NovelID, body.Source)
		if err != nil {
			var storeErr *importStoreError
			switch {
			case errors.As(err, &storeErr):
				return e.InternalServerError(storeErr.msg, storeErr.err)
			case errors.Is(err, errEpubNoContent):
				return e.BadRequestError(err.Error(), nil)
			}
			return notFoundOrForbidden(e, err)
		}

		e.Response.Header().Set("Location", "/api/v1/epubs/"+item.ID+"/download")
		return v1Respond(e, http.StatusCreated, epubRecord(*item), nil, nil)
	}
}

func buildEpubMeta(novel *store.Novel, source string) epubexport.EpubMetadata {
	title := novel.SourceTitle
	author := novel.SourceAuthor
	description := novel.SourceDescription
	language := novel.SourceLanguage
	series := novel.SourceSeries
	number := novel.SourceNumber

	if source == "translated" || source == "refined" {
		if novel.TargetTitle != "" {
			title = novel.TargetTitle
		}
		if novel.TargetAuthor != "" {
			author = novel.TargetAuthor
		}
		if novel.TargetDescription != "" {
			description = novel.TargetDescription
		}
		if novel.TargetLanguage != "" {
			language = novel.TargetLanguage
		}
		if novel.TargetSeries != "" {
			series = novel.TargetSeries
		}
		if novel.TargetNumber != "" {
			number = novel.TargetNumber
		}
	}

	if title == "" {
		title = "Untitled"
	}
	if author == "" {
		author = "Unknown"
	}
	if language == "" {
		language = "es"
	}

	return epubexport.EpubMetadata{
		Title:       title,
		Author:      author,
		Description: description,
		Language:    language,
		Publisher:   "NovelTranslator",
		Series:      series,
		Number:      number,
	}
}

// buildEpubChapters shapes the export chapters and attaches the novel's
// stored inline images, matched by chapter ID and only when the selected
// source variant's content actually contains [[IMG-n]] tokens (a variant
// that lost its tokens must not grow orphan manifest entries). Image order
// matches token order because both follow the stored num.
func buildEpubChapters(chapters []store.Chapter, source, novelID string, s *Server) ([]epubexport.ChapterData, error) {
	var imageBlobs []store.NovelImageBlob
	for _, ch := range chapters {
		if epubimport.ImageTokenRe.MatchString(selectChapterContent(ch, source)) {
			if len(imageBlobs) == 0 {
				var err error
				if imageBlobs, err = s.Store.GetNovelImageBlobs(novelID); err != nil {
					return nil, err
				}
			}
		}
	}
	blobsByChapter := make(map[string][]store.NovelImageBlob)
	for _, img := range imageBlobs {
		blobsByChapter[img.ChapterID] = append(blobsByChapter[img.ChapterID], img)
	}

	var result []epubexport.ChapterData
	for _, ch := range chapters {
		var content string
		var title string

		switch source {
		case "original":
			content = ch.OriginalContent
			title = ch.Title
		case "translated":
			content = ch.TranslatedContent
			title = ch.TranslatedTitle
			if title == "" {
				title = ch.Title
			}
		case "refined":
			content = ch.RefinedContent
			if content == "" {
				content = ch.TranslatedContent
			}
			if content == "" {
				content = ch.OriginalContent
			}
			title = ch.TranslatedTitle
			if title == "" {
				title = ch.Title
			}
		}

		content = strings.TrimSpace(content)
		if content == "" {
			continue
		}

		if title == "" {
			pos := ch.Position
			if pos == 0 {
				pos = ch.ChapterOrder
			}
			title = fmt.Sprintf("Chapter %d", pos)
		}

		data := epubexport.ChapterData{
			Title:   title,
			Content: content,
		}
		for _, blob := range blobsByChapter[ch.ID] {
			data.Images = append(data.Images, epubexport.ImageFile{
				Alt:      blob.Alt,
				MimeType: blob.MimeType,
				Blob:     blob.Blob,
			})
		}
		result = append(result, data)
	}
	return result, nil
}

// selectChapterContent picks the content a source variant would export,
// including the refined→translated→original fallbacks.
func selectChapterContent(ch store.Chapter, source string) string {
	switch source {
	case "translated":
		if ch.TranslatedContent != "" {
			return ch.TranslatedContent
		}
		return ch.OriginalContent
	case "refined":
		if ch.RefinedContent != "" {
			return ch.RefinedContent
		}
		if ch.TranslatedContent != "" {
			return ch.TranslatedContent
		}
		return ch.OriginalContent
	default:
		return ch.OriginalContent
	}
}

func sanitizeFileName(title string) string {
	if title == "" {
		return "libro"
	}
	r := strings.NewReplacer(
		"\\", "_", "/", "_", ":", "_", "*", "_",
		"?", "_", "\"", "_", "<", "_", ">", "_", "|", "_",
	)
	clean := r.Replace(title)
	if len(clean) > 120 {
		clean = clean[:120]
	}
	return clean
}
