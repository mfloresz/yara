# Integrations Codemap

**Last Updated:** 2026-07-14
**Entry Points:** `internal/ai/registry.go`, `internal/noveldownloader/`

## AI Providers

Provider catalog in `internal/ai/registry.go` with `ProviderInfo` struct. All OpenAI-compatible providers use `github.com/zendev-sh/goai`. Non-OpenAI providers (Google) use a direct API approach.

### Registered providers

| ID | Name | Base URL | Models | Default | OpenAI Compat | GoAI Options |
|----|------|----------|--------|---------|---------------|--------------|
| `venice` | Venice | `https://api.venice.ai/api/v1` | deepseek-v4-flash, mistral-small-3-2-24b-instruct, google-gemma-4-31b-it, e2ee-gpt-oss-20b-p, aion-labs-aion-3-0-mini, e2ee-gemma-4-26b-a4b-uncensored-p, google-gemma-4-26b-a4b-it | deepseek-v4-flash | true | `useResponsesAPI: false`, `strictJsonSchema: true` |
| `opencode-go` | OpenCode Go | `https://opencode.ai/zen/go/v1` | mimo-v2.5, deepseek-v4-flash | mimo-v2.5 | true | `useResponsesAPI: false`, `strictJsonSchema: true` |
| `lmstudio` | LM Studio | `http://localhost:1234/v1` | local-model | local-model | true | `useResponsesAPI: false`, `strictJsonSchema: false` |
| `google` | Google Gemma | `https://generativelanguage.googleapis.com` | gemma-4-26b-a4b-it, gemma-4-31b-it | gemma-4-31b-it | false | — |

### Provider interface — `internal/ai/provider.go`

```go
type Provider interface {
    TranslateTitle(ctx, input) (string, error)
    TranslateText(ctx, input) (string, error)
    Refine(ctx, input) (RefineOutput, error)
    Check(ctx, input) (CheckOutput, error)
}
```

### Implementation — `internal/ai/openai.go`

- Single `OpenAIProvider` struct implementing `Provider`
- Uses `goai.Client` for API calls
- JSON mode with configurable `useResponsesAPI` and `strictJsonSchema`
- Timeout configurable per request
- Google provider uses direct HTTP calls (non-OpenAI)

### Key files

| File | Purpose |
|------|---------|
| `registry.go` | `knownProviders` slice, `Providers()`, `ProviderByID()`, `DefaultProvider()` |
| `provider.go` | `Provider` interface + input/output types |
| `openai.go` | `OpenAIProvider` full implementation |
| `translation_schema.go` | JSON schemas for structured output |

## Web novel scrapers

Module: `internal/noveldownloader/` (30+ files)

### Supported sites

| Site | Parser | Features |
|------|--------|----------|
| NovelFire | `novelfire.go`, `novelfire_metadata.go`, `novelfire_chapters.go`, `novelfire_content.go` | Metadata, chapter list, content |
| Fenrir Realm | `fenrirealm.go`, `fenrirealm_metadata.go`, `fenrirealm_content.go` | Metadata, chapter list, content |
| Florae Garden | `floraegarden.go` | Metadata, chapter list, content |
| Cherry Mist | `cherrymist.go` | Metadata, chapter list, content |
| Empire Novel | `empirenovel.go` | Metadata, chapter list, content |
| 69shuba | `69shuba.go`, `69shuba_metadata.go`, `69shuba_chapters.go` | Metadata, chapter list, content |
| Sky Novels | `skynovels.go`, `skynovels_metadata.go`, `skynovels_chapters.go` | Metadata, chapter list, content |
| Fictioneer | `fictioneer.go` | Generic Fictioneer-based sites |
| RSS | `rss.go` | RSS feed-based content |

### Cloudflare bypass

| File | Purpose |
|------|---------|
| `browser_worker_provider.go` | Fetches content through browser extension |
| `browser_required.go` | Lists domains requiring browser bypass |
| `fallback_client.go` | Falls back to browser worker on HTTP errors |

### Downloader — `internal/noveldownloader/downloader.go`

| Feature | Detail |
|---------|--------|
| Rate limiting | Random delay between `MinChapterDelay` (5s) and `MaxChapterDelay` (10s) |
| Env config | `DOWNLOAD_MIN_DELAY_MS`, `DOWNLOAD_MAX_DELAY_MS` |
| HTML→Markdown | Via `html-to-markdown/v2` |
| Parser selection | URL-based pattern matching in `FindParser()` |

### Key files

| File | Purpose |
|------|---------|
| `downloader.go` | `Downloader` struct, `DownloadChapters()`, `SleepBetweenChapters()` |
| `client.go` | HTTP client setup |
| `parser.go` | `Parser` interface |
| `models.go` | `ChapterURL`, `Chapter` types |
| `url_helpers.go` | URL parsing and normalization |
| `html_helpers.go` | HTML cleaning utilities |

## EPUB import

Module: `internal/epubimport/` (10+ files)

| File | Purpose |
|------|---------|
| `parser.go` | EPUB zip parsing → `Result` structure |
| `container.go` | `META-INF/container.xml` parsing |
| `manifest.go` | OPF manifest parsing |
| `metadata.go` | OPF metadata parsing |
| `ncx.go` | NCX navigation parsing |
| `types.go` | `Result`, `Metadata` types |
| `chapter_extract.go` | Chapter content extraction from XHTML |
| `normalize.go` | Content normalization |
| `zip.go` | ZIP file helpers |

## EPUB export

Module: `internal/epubexport/` (5+ files)

| File | Purpose |
|------|---------|
| `generator.go` | EPUB generation from novel chapters |
| `text_processor.go` | Text transformation for output |
| `generator_test.go` | Tests |
| `text_processor_test.go` | Tests |

## Data flow

```
Provider UI → PUT /api/user/providers/{key}/key → store (encrypted)
                                                    ↓
Job → resolveJobConfig() → aiOptions.provider + model
  → registry.ProviderByID() → baseURL, goai options
  → openai.NewProvider() → goai.Client → HTTP → external API
                                                    ↑
Download Job → noveldownloader.Downloader
  → FindParser(url) → Parser matching domain
  → HTTP GET → HTML → goquery → html-to-markdown → Markdown
  → store (chapters)
```

## Related codemaps

- [Database](database.md) — Encrypted API keys in `user_provider_settings`
- [Workers](workers.md) — How providers/downloaders are invoked from jobs
- [Backend](backend.md) — Provider config resolution in `runtime_config.go`
