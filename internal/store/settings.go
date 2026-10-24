package store

import "fmt"

type TranslationDefaults struct {
	AutoSegment               bool `json:"autoSegment"`
	ThresholdChars            int  `json:"thresholdChars"`
	MaxChars                  int  `json:"maxChars"`
	MinChars                  int  `json:"minChars"`
	MaxRetries                int  `json:"maxRetries"`
	EnableCheck               bool `json:"enableCheck"`
	IncludePreviousTitleHints bool `json:"includePreviousChapterTitles"`
	Concurrency               int  `json:"concurrency"`
}

type AISettings struct {
	Provider      string `json:"provider"`
	APIKey        string `json:"apiKey,omitempty"`
	BaseURL       string `json:"baseUrl"`
	Model         string `json:"model,omitempty"`
	TimeoutMs     int    `json:"timeoutMs,omitempty"`
	Concurrency   int    `json:"concurrency,omitempty"`
	CustomBaseURL string `json:"customBaseUrl,omitempty"`
	CustomModel   string `json:"customModel,omitempty"`
}

type AppSettings struct {
	AI            AISettings          `json:"ai"`
	TitleProvider string              `json:"titleProvider,omitempty"`
	TitleModel    string              `json:"titleModel,omitempty"`
	AgentProvider string              `json:"agentProvider,omitempty"`
	AgentModel    string              `json:"agentModel,omitempty"`
	Translation   TranslationDefaults `json:"translation"`
}

type User struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name,omitempty"`
	Role      string `json:"role,omitempty"`
	Blocked   bool   `json:"blocked,omitempty"`
	Theme     string `json:"theme"`
	CreatedAt string `json:"createdAt,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

type Prompt struct {
	Key          string `json:"key,omitempty"`
	Label        string `json:"label,omitempty"`
	Description  string `json:"description,omitempty"`
	SystemPrompt string `json:"systemPrompt,omitempty"`
	UserPrompt   string `json:"userPrompt,omitempty"`
	Active       int    `json:"active,omitempty"`
	CreatedAt    string `json:"createdAt,omitempty"`
	UpdatedAt    string `json:"updatedAt,omitempty"`
}

type Invitation struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	ExpiresAt string `json:"expiresAt,omitempty"`
	UsedAt    string `json:"usedAt,omitempty"`
	CreatedBy string `json:"createdBy,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
}

type SharedProviderKey struct {
	Provider        string `json:"provider"`
	Configured      bool   `json:"configured"`
	Shared          bool   `json:"shared"`
	APIKeyUpdatedAt string `json:"apiKeyUpdatedAt,omitempty"`
}

type ProviderSetting struct {
	Provider           string   `json:"provider"`
	Label              string   `json:"label"`
	BaseURL            string   `json:"baseUrl"`
	Model              string   `json:"model"`
	Models             []string `json:"models,omitempty"`
	Kind               string   `json:"kind"`
	APIKeyConfigured   bool     `json:"apiKeyConfigured"`
	APIKeyUpdatedAt    string   `json:"apiKeyUpdatedAt,omitempty"`
	Enabled            bool     `json:"enabled"`
	TimeoutMs          int      `json:"timeoutMs,omitempty"`
	Concurrency        int      `json:"concurrency,omitempty"`
	SharedKeyAvailable bool     `json:"sharedKeyAvailable,omitempty"`
	UsingSharedKey     bool     `json:"usingSharedKey,omitempty"`
}

type Novel struct {
	ID                      string `json:"id,omitempty"`
	OwnerID                 string `json:"ownerId,omitempty"`
	SourceLanguage          string `json:"sourceLanguage,omitempty"`
	TargetLanguage          string `json:"targetLanguage,omitempty"`
	SourceTitle             string `json:"sourceTitle,omitempty"`
	SourceAuthor            string `json:"sourceAuthor,omitempty"`
	SourceDescription       string `json:"sourceDescription,omitempty"`
	SourceSeries            string `json:"sourceSeries,omitempty"`
	SourceNumber            string `json:"sourceNumber,omitempty"`
	TargetTitle             string `json:"targetTitle,omitempty"`
	TargetAuthor            string `json:"targetAuthor,omitempty"`
	TargetDescription       string `json:"targetDescription,omitempty"`
	TargetSeries            string `json:"targetSeries,omitempty"`
	TargetNumber            string `json:"targetNumber,omitempty"`
	Glossary                string `json:"glossary,omitempty"`
	TranslationSystemPrompt string `json:"translationSystemPrompt,omitempty"`
	TranslationUserPrompt   string `json:"translationUserPrompt,omitempty"`
	TitleSystemPrompt       string `json:"titleSystemPrompt,omitempty"`
	TitleUserPrompt         string `json:"titleUserPrompt,omitempty"`
	RefineSystemPrompt      string `json:"refineSystemPrompt,omitempty"`
	RefineUserPrompt        string `json:"refineUserPrompt,omitempty"`
	CheckSystemPrompt       string `json:"checkSystemPrompt,omitempty"`
	CheckUserPrompt         string `json:"checkUserPrompt,omitempty"`
	Notes                   string `json:"notes,omitempty"`
	AIOptions               string `json:"aiOptions,omitempty"`
	TranslationOptions      string `json:"translationOptions,omitempty"`
	CleanupRules            string `json:"cleanupRules,omitempty"`
	URL                     string `json:"url,omitempty"`
	CustomCommands          string `json:"customCommands,omitempty"`
	Status                  string `json:"status,omitempty"`
	Tags                    string `json:"tags,omitempty"`
	CoverPath               string `json:"coverPath,omitempty"`
	CoverFile               string `json:"coverFile,omitempty"`
	ThumbnailPath           string `json:"thumbnailPath,omitempty"`
	ThumbnailFile           string `json:"thumbnailFile,omitempty"`
	IsPublic                bool   `json:"isPublic,omitempty"`
	ChapterCount            int    `json:"chapterCount,omitempty"`
	TranslatedCount         int    `json:"translatedCount,omitempty"`
	CompletedCount          int    `json:"completedCount,omitempty"`
	OriginalCharCount       int    `json:"originalCharCount,omitempty"`
	TranslatedCharCount     int    `json:"translatedCharCount,omitempty"`
	RefinedCharCount        int    `json:"refinedCharCount,omitempty"`
	TotalCharCount          int    `json:"totalCharCount,omitempty"`
	MaxChapterOrder         int    `json:"maxChapterOrder,omitempty"`
	LastCheckedAt           string `json:"lastCheckedAt,omitempty"`
	LastCheckNewChapters    int    `json:"lastCheckNewChapters,omitempty"`
	LastReadAt              string `json:"lastReadAt,omitempty"`
	CreatedAt               string `json:"createdAt,omitempty"`
	UpdatedAt               string `json:"updatedAt,omitempty"`
}

type Chapter struct {
	ID           string `json:"id,omitempty"`
	NovelID      string `json:"novelId,omitempty"`
	ChapterOrder int    `json:"chapterOrder,omitempty"`
	Position     int    `json:"position,omitempty"`
	Excluded     bool   `json:"excluded,omitempty"`
	Title        string `json:"title,omitempty"`
	// SourceKey is the parser script's stable identity for this chapter
	// (chapterKey export, defaulting to the source chapter URL). Empty for
	// rows created before the field existed, which is why the source-sync
	// diff falls back to the title/order heuristic for those.
	SourceKey         string `json:"sourceKey,omitempty"`
	TranslatedTitle   string `json:"translatedTitle,omitempty"`
	OriginalContent   string `json:"originalContent,omitempty"`
	TranslatedContent string `json:"translatedContent,omitempty"`
	RefinedContent    string `json:"refinedContent,omitempty"`
	Status            string `json:"status,omitempty"`
	ErrorMessage      string `json:"errorMessage,omitempty"`
	CreatedAt         string `json:"createdAt,omitempty"`
	UpdatedAt         string `json:"updatedAt,omitempty"`
}

type ChapterSummary struct {
	ID                   string `json:"id,omitempty"`
	NovelID              string `json:"novelId,omitempty"`
	ChapterOrder         int    `json:"chapterOrder,omitempty"`
	Position             int    `json:"position,omitempty"`
	Excluded             bool   `json:"excluded,omitempty"`
	Title                string `json:"title,omitempty"`
	TranslatedTitle      string `json:"translatedTitle,omitempty"`
	Status               string `json:"status,omitempty"`
	ErrorMessage         string `json:"errorMessage,omitempty"`
	HasOriginalContent   bool   `json:"hasOriginalContent,omitempty"`
	HasTranslatedContent bool   `json:"hasTranslatedContent,omitempty"`
	HasRefinedContent    bool   `json:"hasRefinedContent,omitempty"`
	OriginalChars        int    `json:"originalChars,omitempty"`
	TranslatedChars      int    `json:"translatedChars,omitempty"`
	RefinedChars         int    `json:"refinedChars,omitempty"`
	CreatedAt            string `json:"createdAt,omitempty"`
	UpdatedAt            string `json:"updatedAt,omitempty"`
}

type ChapterStats struct {
	TotalChapters        int `json:"totalChapters"`
	CompletedChapters    int `json:"completedChapters"`
	TranslatedChapters   int `json:"translatedChapters"`
	OriginalCharacters   int `json:"originalCharacters"`
	TranslatedCharacters int `json:"translatedCharacters"`
	RefinedCharacters    int `json:"refinedCharacters"`
	TotalCharacters      int `json:"totalCharacters"`
	MaxChapterOrder      int `json:"maxChapterOrder"`
}

type Job struct {
	ID                        string `json:"id,omitempty"`
	OwnerID                   string `json:"ownerId,omitempty"`
	NovelID                   string `json:"novelId,omitempty"`
	ComicID                   string `json:"comicId,omitempty"`
	Status                    string `json:"status,omitempty"`
	Operation                 string `json:"operation,omitempty"`
	Provider                  string `json:"provider,omitempty"`
	Model                     string `json:"model,omitempty"`
	ChapterIDs                string `json:"chapterIds,omitempty"`
	OptionsJSON               string `json:"optionsJson,omitempty"`
	ErrorMessage              string `json:"errorMessage,omitempty"`
	TotalChapters             int    `json:"totalChapters,omitempty"`
	CompletedChapters         int    `json:"completedChapters,omitempty"`
	FailedChapters            int    `json:"failedChapters,omitempty"`
	AutoSegmentEnabled        bool   `json:"autoSegmentEnabled,omitempty"`
	AutoSegmentActive         bool   `json:"autoSegmentActive,omitempty"`
	AutoSegmentCount          int    `json:"autoSegmentCount,omitempty"`
	AutoSegmentCurrentIndex   int    `json:"autoSegmentCurrentIndex,omitempty"`
	AutoSegmentCompletedCount int    `json:"autoSegmentCompletedCount,omitempty"`
	AutoSegmentChapterID      string `json:"autoSegmentChapterId,omitempty"`
	AutoSegmentChapterTitle   string `json:"autoSegmentChapterTitle,omitempty"`
	NewChapters               int    `json:"newChapters,omitempty"`
	CreatedAt                 string `json:"createdAt,omitempty"`
	UpdatedAt                 string `json:"updatedAt,omitempty"`
	NovelTitle                string `json:"novelTitle,omitempty"`
	ComicTitle                string `json:"comicTitle,omitempty"`
}

// JobOverview is the narrow projection the library assistant's job tools
// consume: no chapter_ids/options_json (which can reach many KB per row) and
// the novel title resolved by JOIN inside the query, not with N+1 lookups.
type JobOverview struct {
	ID                string `json:"id"`
	NovelID           string `json:"novelId"`
	NovelTitle        string `json:"novelTitle"`
	Operation         string `json:"operation"`
	Status            string `json:"status"`
	Provider          string `json:"provider"`
	Model             string `json:"model"`
	TotalChapters     int    `json:"totalChapters"`
	CompletedChapters int    `json:"completedChapters"`
	FailedChapters    int    `json:"failedChapters"`
	ErrorMessage      string `json:"errorMessage"`
	CreatedAt         string `json:"createdAt"`
	UpdatedAt         string `json:"updatedAt"`
}

// SeriesProgress is one series row of the assistant's catalog overview:
// chapter counters aggregated over every non-excluded chapter of the series'
// novels. Status semantics mirror v_agent_novel_progress (translated =
// translated/refined/done).
type SeriesProgress struct {
	Series             string `json:"series"`
	Novels             int    `json:"novels"`
	ChaptersTotal      int    `json:"chaptersTotal"`
	ChaptersTranslated int    `json:"chaptersTranslated"`
	ChaptersPending    int    `json:"chaptersPending"`
	FullyTranslated    bool   `json:"fullyTranslated"`
}

type Epub struct {
	ID            string `json:"id,omitempty"`
	NovelID       string `json:"novelId,omitempty"`
	FileKind      string `json:"fileKind,omitempty"`
	SourceVariant string `json:"sourceVariant,omitempty"`
	Label         string `json:"label,omitempty"`
	FileName      string `json:"fileName,omitempty"`
	FileSize      int64  `json:"fileSize,omitempty"`
	MimeType      string `json:"mimeType,omitempty"`
	URL           string `json:"url,omitempty"`
	CreatedAt     string `json:"createdAt,omitempty"`
	UpdatedAt     string `json:"updatedAt,omitempty"`
}

// ImportedNovelImage is one inline chapter image produced by an import
// (EPUB extraction or the import-zip image convention). Images[i] always
// corresponds to the [[IMG-(i+1)]] token of its chapter.
type ImportedNovelImage struct {
	Alt      string
	MimeType string
	FileName string
	Blob     []byte
}

// ImportedChapterImages pairs a persisted chapter ID with the images
// referenced by its content.
type ImportedChapterImages struct {
	ChapterID string
	Images    []ImportedNovelImage
}

// NovelImage is a stored inline image of a novel. Num mirrors the n in the
// [[IMG-n]] token inside the owning chapter's content.
type NovelImage struct {
	ID        string `json:"id,omitempty"`
	NovelID   string `json:"novelId,omitempty"`
	ChapterID string `json:"chapterId,omitempty"`
	Num       int    `json:"num,omitempty"`
	Alt       string `json:"alt,omitempty"`
	MimeType  string `json:"mime,omitempty"`
	FileName  string `json:"fileName,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
}

// ImageToken returns the exact placeholder string for this image as it
// appears (or must appear) in its chapter's content.
func (i NovelImage) ImageToken() string {
	return fmt.Sprintf("[[IMG-%d]]", i.Num)
}

type ImportedEpubChapter struct {
	Title   string
	Content string
	Images  []ImportedNovelImage
}

type ImportEpubNovelInput struct {
	OwnerID           string
	FileName          string
	FileBlob          []byte
	MimeType          string
	SourceTitle       string
	SourceAuthor      string
	SourceDescription string
	SourceLanguage    string
	TargetLanguage    string
	SourceSeries      string
	SourceNumber      string
	CoverMime         string
	CoverBlob         []byte
	Chapters          []ImportedEpubChapter
}

type ImportEpubNovelResult struct {
	Novel            Novel
	Epub             Epub
	ChaptersImported int
}

type ImportUrlNovelInput struct {
	OwnerID           string
	URL               string
	SourceLanguage    string
	TargetLanguage    string
	SourceTitle       string
	SourceAuthor      string
	SourceDescription string
	StartChapter      int
	EndChapter        int
}

type ImportUrlNovelResult struct {
	Novel            Novel
	ChaptersImported int
}

type UpdateUrlNovelInput struct {
	OwnerID      string
	NovelID      string
	StartChapter int
	EndChapter   int
}

type UpdateUrlNovelResult struct {
	ChaptersAdded int
	Chapters      []Chapter
}

type ReadingProgress struct {
	ID            string  `json:"id,omitempty"`
	UserID        string  `json:"userId,omitempty"`
	NovelID       string  `json:"novelId,omitempty"`
	ChapterID     string  `json:"chapterId,omitempty"`
	ScrollPercent float64 `json:"scrollPercent,omitempty"`
	CreatedAt     string  `json:"createdAt,omitempty"`
	UpdatedAt     string  `json:"updatedAt,omitempty"`
}

type DownloadChapterInfo struct {
	URL       string `json:"url"`
	Title     string `json:"title"`
	Order     int    `json:"order"`
	ChapterID string `json:"chapterId,omitempty"` // id of an existing chapter when re-downloading
	// SourceKey is the parser script's chapter identity (chapterKey export,
	// defaulting to the chapter URL). Persisted as chapters.source_key so the
	// next new/missing diff can match on identity instead of title heuristics.
	SourceKey string `json:"sourceKey,omitempty"`
}

type BatchCheckNovelResult struct {
	NovelID         string                `json:"novelId"`
	SourceTitle     string                `json:"sourceTitle"`
	SourceAuthor    string                `json:"sourceAuthor,omitempty"`
	CoverURL        string                `json:"coverUrl,omitempty"`
	NewChapters     int                   `json:"newChapters"`
	FirstNewChapter int                   `json:"firstNewChapter"`
	LastNewChapter  int                   `json:"lastNewChapter"`
	StartOrder      int                   `json:"startOrder"`
	CurrentChapters int                   `json:"currentChapters"`
	TotalChapters   int                   `json:"totalChapters"`
	NewChapterInfo  []DownloadChapterInfo `json:"newChapterInfo"`
	Error           string                `json:"error,omitempty"`
}

type BatchCheckResponse struct {
	Results     []BatchCheckNovelResult `json:"results"`
	Checked     int                     `json:"checked"`
	WithUpdates int                     `json:"withUpdates"`
	Errors      int                     `json:"errors"`
}

type BatchUpdateSelection struct {
	NovelID        string                `json:"novelId"`
	StartOrder     int                   `json:"startOrder"`
	StartChapter   int                   `json:"startChapter,omitempty"`
	EndChapter     int                   `json:"endChapter,omitempty"`
	NewChapterInfo []DownloadChapterInfo `json:"newChapterInfo"`
}

type BatchUpdateJobResult struct {
	NovelID         string `json:"novelId"`
	JobID           string `json:"jobId"`
	PendingChapters int    `json:"pendingChapters"`
	EnqueueFailed   bool   `json:"enqueueFailed"`
}

type BatchUpdateResponse struct {
	Jobs         []BatchUpdateJobResult `json:"jobs"`
	TotalPending int                    `json:"totalPending"`
}

type BatchTranslateNovelResult struct {
	NovelID            string `json:"novelId"`
	SourceTitle        string `json:"sourceTitle"`
	SourceAuthor       string `json:"sourceAuthor,omitempty"`
	CoverURL           string `json:"coverUrl,omitempty"`
	PendingChapters    int    `json:"pendingChapters"`
	TotalChapters      int    `json:"totalChapters"`
	TranslatedCount    int    `json:"translatedCount"`
	CompletedCount     int    `json:"completedCount"`
	HasOriginalContent bool   `json:"hasOriginalContent"`
}

type BatchTranslateResponse struct {
	Results     []BatchTranslateNovelResult `json:"results"`
	TotalNovels int                         `json:"totalNovels"`
	WithPending int                         `json:"withPending"`
}

type BatchTranslateSelection struct {
	NovelID    string   `json:"novelId"`
	ChapterIDs []string `json:"chapterIds,omitempty"`
}

type BatchTranslateJobResult struct {
	NovelID         string `json:"novelId"`
	JobID           string `json:"jobId"`
	PendingChapters int    `json:"pendingChapters"`
	EnqueueFailed   bool   `json:"enqueueFailed"`
}

type BatchTranslateStartResponse struct {
	Jobs         []BatchTranslateJobResult `json:"jobs"`
	TotalPending int                       `json:"totalPending"`
}

type ImportedZipChapter struct {
	Order             int
	Title             string
	TranslatedTitle   string
	OriginalContent   string
	TranslatedContent string
	Images            []ImportedNovelImage
}

type ImportZipNovelInput struct {
	OwnerID      string
	FileName     string
	FileBlob     []byte
	MetadataJSON string
	CoverBlob    []byte
	CoverMime    string
	Chapters     []ImportedZipChapter
}

type ImportZipNovelResult struct {
	Novel            Novel
	ChaptersImported int
}

const MaxProviderConcurrency = 10
const MinProviderConcurrency = 1

func NormalizeProviderConcurrency(v int) int {
	if v < MinProviderConcurrency {
		return MinProviderConcurrency
	}
	if v > MaxProviderConcurrency {
		return MaxProviderConcurrency
	}
	return v
}

var DefaultAISettings = AISettings{
	Provider:    "venice",
	BaseURL:     "https://api.venice.ai/api/v1",
	Model:       "deepseek-v4-flash",
	TimeoutMs:   120000,
	Concurrency: 1,
}

var DefaultTranslationDefaults = TranslationDefaults{
	AutoSegment:               true,
	ThresholdChars:            20000,
	MaxChars:                  10000,
	MinChars:                  500,
	MaxRetries:                2,
	EnableCheck:               false,
	IncludePreviousTitleHints: false,
	Concurrency:               1,
}

// ---------------------------------------------------------------------------
// Comics domain (media: comic). Mirrors the novel types above with a page
// payload instead of text: chapters carry page images, not content bodies.
// Text-translation fields are deliberately absent until the comic translation
// flow lands; adding them later is additive (ensureField).
// ---------------------------------------------------------------------------

// Comic is one comic/manhwa work in the library.
type Comic struct {
	ID                   string `json:"id,omitempty"`
	OwnerID              string `json:"ownerId,omitempty"`
	SourceLanguage       string `json:"sourceLanguage,omitempty"`
	TargetLanguage       string `json:"targetLanguage,omitempty"`
	SourceTitle          string `json:"sourceTitle,omitempty"`
	SourceAuthor         string `json:"sourceAuthor,omitempty"`
	SourceDescription    string `json:"sourceDescription,omitempty"`
	SourceSeries         string `json:"sourceSeries,omitempty"`
	SourceNumber         string `json:"sourceNumber,omitempty"`
	TargetTitle          string `json:"targetTitle,omitempty"`
	TargetAuthor         string `json:"targetAuthor,omitempty"`
	TargetDescription    string `json:"targetDescription,omitempty"`
	TargetSeries         string `json:"targetSeries,omitempty"`
	TargetNumber         string `json:"targetNumber,omitempty"`
	Notes                string `json:"notes,omitempty"`
	URL                  string `json:"url,omitempty"`
	Status               string `json:"status,omitempty"`
	Tags                 string `json:"tags,omitempty"`
	CoverPath            string `json:"coverPath,omitempty"`
	CoverFile            string `json:"coverFile,omitempty"`
	ThumbnailPath        string `json:"thumbnailPath,omitempty"`
	ThumbnailFile        string `json:"thumbnailFile,omitempty"`
	IsPublic             bool   `json:"isPublic,omitempty"`
	ChapterCount         int    `json:"chapterCount,omitempty"`
	DownloadedCount      int    `json:"downloadedCount,omitempty"`
	MaxChapterOrder      int    `json:"maxChapterOrder,omitempty"`
	LastCheckedAt        string `json:"lastCheckedAt,omitempty"`
	LastCheckNewChapters int    `json:"lastCheckNewChapters,omitempty"`
	LastReadAt           string `json:"lastReadAt,omitempty"`
	CreatedAt            string `json:"createdAt,omitempty"`
	UpdatedAt            string `json:"updatedAt,omitempty"`
}

// ComicChapterStatus values. Download-oriented only; the translation
// lifecycle statuses arrive with the comic translation flow.
const (
	ComicChapterPending    = "pending"
	ComicChapterProcessing = "processing"
	ComicChapterDone       = "done"
	ComicChapterFailed     = "failed"
	// ComicChapterPartial is reserved for partially downloaded chapters
	// (some pages failed); the downloader marks it when pages are missing.
	ComicChapterPartial = "partial"
)

// ComicChapter is one chapter of a comic: metadata plus the fact of its pages
// (page bodies live in ComicPage rows).
type ComicChapter struct {
	ID           string `json:"id,omitempty"`
	ComicID      string `json:"comicId,omitempty"`
	ChapterOrder int    `json:"chapterOrder,omitempty"`
	Position     int    `json:"position,omitempty"`
	Excluded     bool   `json:"excluded,omitempty"`
	Title        string `json:"title,omitempty"`
	// SourceKey is the parser script's stable identity for this chapter
	// (chapterKey export, defaulting to the source chapter URL). The
	// source-sync diff matches on it before falling back to URL/title.
	SourceKey    string `json:"sourceKey,omitempty"`
	PageCount    int    `json:"pageCount,omitempty"`
	HasPages     bool   `json:"hasPages,omitempty"`
	Status       string `json:"status,omitempty"`
	ErrorMessage string `json:"errorMessage,omitempty"`
	CreatedAt    string `json:"createdAt,omitempty"`
	UpdatedAt    string `json:"updatedAt,omitempty"`
}

// ComicChapterSummary is the list-shaped projection of a comic chapter.
type ComicChapterSummary struct {
	ID           string `json:"id,omitempty"`
	ComicID      string `json:"comicId,omitempty"`
	ChapterOrder int    `json:"chapterOrder,omitempty"`
	Position     int    `json:"position,omitempty"`
	Excluded     bool   `json:"excluded,omitempty"`
	Title        string `json:"title,omitempty"`
	Status       string `json:"status,omitempty"`
	ErrorMessage string `json:"errorMessage,omitempty"`
	PageCount    int    `json:"pageCount,omitempty"`
	HasPages     bool   `json:"hasPages,omitempty"`
	CreatedAt    string `json:"createdAt,omitempty"`
	UpdatedAt    string `json:"updatedAt,omitempty"`
}

// ComicChapterStats aggregates a comic's chapter download state.
type ComicChapterStats struct {
	TotalChapters      int `json:"totalChapters"`
	DownloadedChapters int `json:"downloadedChapters"`
	FailedChapters     int `json:"failedChapters"`
	MaxChapterOrder    int `json:"maxChapterOrder"`
}

// ComicPage is one downloaded page image of a comic chapter, in reading order
// (PageOrder is 1-based). The binary lives on disk as a PocketBase file; the
// DB row only holds metadata + ordering.
type ComicPage struct {
	ID        string `json:"id,omitempty"`
	ChapterID string `json:"chapterId,omitempty"`
	ComicID   string `json:"comicId,omitempty"`
	PageOrder int    `json:"pageOrder,omitempty"`
	SourceURL string `json:"sourceUrl,omitempty"`
	FileName  string `json:"fileName,omitempty"`
	URL       string `json:"url,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

// ComicPageUpload is one image to persist for a chapter. Data is the raw
// image bytes (never base64); Order is 1-based reading order.
type ComicPageUpload struct {
	Data      []byte
	MimeType  string
	SourceURL string
	Order     int
}

// ComicReadingProgress is the per-user resume point of a comic.
type ComicReadingProgress struct {
	ID            string  `json:"id,omitempty"`
	UserID        string  `json:"userId,omitempty"`
	ComicID       string  `json:"comicId,omitempty"`
	ChapterID     string  `json:"chapterId,omitempty"`
	ScrollPercent float64 `json:"scrollPercent,omitempty"`
	CreatedAt     string  `json:"createdAt,omitempty"`
	UpdatedAt     string  `json:"updatedAt,omitempty"`
}

// DownloadComicChapterInfo describes one chapter to download, as produced by
// the TOC snapshot diff.
type DownloadComicChapterInfo struct {
	URL       string `json:"url"`
	Title     string `json:"title"`
	Order     int    `json:"order"`
	ChapterID string `json:"chapterId,omitempty"` // id of an existing chapter when re-downloading
	// SourceKey is the parser script's chapter identity (chapterKey export,
	// defaulting to the chapter URL). Persisted as comic_chapters.source_key.
	SourceKey string `json:"sourceKey,omitempty"`
}

// BatchCheckComicResult is one comic's outcome of the batch update check.
type BatchCheckComicResult struct {
	ComicID         string                     `json:"comicId"`
	SourceTitle     string                     `json:"sourceTitle"`
	SourceAuthor    string                     `json:"sourceAuthor,omitempty"`
	CoverURL        string                     `json:"coverUrl,omitempty"`
	NewChapters     int                        `json:"newChapters"`
	FirstNewChapter int                        `json:"firstNewChapter"`
	LastNewChapter  int                        `json:"lastNewChapter"`
	StartOrder      int                        `json:"startOrder"`
	CurrentChapters int                        `json:"currentChapters"`
	TotalChapters   int                        `json:"totalChapters"`
	NewChapterInfo  []DownloadComicChapterInfo `json:"newChapterInfo"`
	Error           string                     `json:"error,omitempty"`
}

// ImportedComicImage is one page image produced by a ZIP import, held in
// memory until its chapter's pages are persisted.
type ImportedComicImage struct {
	Alt      string
	MimeType string
	FileName string
	Blob     []byte
}

// ImportedComicZipChapter is one chapter of a ZIP import: images become the
// chapter's pages, in array order.
type ImportedComicZipChapter struct {
	Order  int
	Title  string
	Images []ImportedComicImage
}

// ImportZipComicInput carries a whole ZIP-of-images comic import.
type ImportZipComicInput struct {
	OwnerID      string
	FileName     string
	FileBlob     []byte
	MetadataJSON string
	CoverBlob    []byte
	CoverMime    string
	Chapters     []ImportedComicZipChapter
}

// ImportZipComicResult reports the created comic.
type ImportZipComicResult struct {
	Comic            Comic
	ChaptersImported int
}

// ImportUrlComicInput creates a comic from a source URL (parser TOC).
type ImportUrlComicInput struct {
	OwnerID           string
	URL               string
	SourceLanguage    string
	TargetLanguage    string
	SourceTitle       string
	SourceAuthor      string
	SourceDescription string
	StartChapter      int
	EndChapter        int
}

// ImportUrlComicResult reports the created comic.
type ImportUrlComicResult struct {
	Comic            Comic
	ChaptersImported int
}
