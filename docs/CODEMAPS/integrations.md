# Integrations Codemap

**Last Updated:** 2026-07-14
**Entry Points:** `internal/ai/registry.go`, `internal/parserhost/`, `parsers/`

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

## Web novel parsers

Site-specific logic lives in editable JavaScript under `parsers/`, one CommonJS
file per site, executed by the embedded goja engine in `internal/parserhost/`.
The contract (and the error taxonomy) is documented in
[`docs/parsers.md`](../../docs/parsers.md); `internal/parserhost/contract.go` is
the source of truth.

### Supported sites

| Site | Parser script |
|------|---------------|
| NovelFire | `novelfire.js` |
| Fenrir Realm | `fenrirealm.js` |
| Florae Garden | `floraegarden.js` |
| Cherry Mist | `cherrymist.js` |
| Empire Novel | `empirenovel.js` |
| 69shuba | `69shuba.js` (GBK pages) |
| Sky Novels | `skynovels.js` (JSON API; requires a `Referer` header) |
| SkyDemonOrder | `skydemonorder.js` (Livewire/JSON catalog) |
| Literotica | `literotica.js` |
| NovelArrow | `novelarrow.js` |
| Wattpad | `wattpad.js` |
| Webnovel | `webnovel.js` |
| Inkitt | `inkitt.js` |
| GayDemon | `gaydemon.js` |
| ChrysanthemumGarden | `chrysanthemumgarden.js` |
| Inkspired | `inkspired.js` |

### Engine — `internal/parserhost/`

| Feature | Detail |
|---------|--------|
| Runtime | `goja` VM, a fresh one per invocation; module state never leaks between jobs |
| Load | `LoadFile` / `LoadDir` — the directory is re-read per job (no cache), so edits are hot-reloaded |
| Entry points | `probe(url)`, `toc(ctx,url)`, `chapter(ctx,url)`, optional `chapterKey(ref)` |
| Bindings | `ctx.get/css/css1/resolveUrl/fail/log` in `bindings.go` |
| Limits | 30s wall-clock, 200 fetches, 5 MiB response body per invocation |
| Errors | `ScriptError` taxonomy: `not_my_site`, `site_layout_changed`, `blocked`, `parser_timeout`, `script_error` |

### Host side — `internal/api/parser_*.go`

| File | Purpose |
|------|---------|
| `parser_engine.go` | Script loading/selection, TOC snapshot, new/missing diff, `canUpdate`/`requiresBrowser` |
| `parser_fetcher.go` | `parserhost.Fetcher` impl: throttling, direct HTTP, browser-worker routing |
| `parser_cgfont.go` | Hybrid helpers: host-side response post-processing a script cannot do (chrysanthemumgarden obfuscation-font decoding) |
| `parser_check.go` | `-check-parser` / `-check-url` mode — prints the snapshot as JSON, touches no store |
| `parser_http_compat.go` | GBK charset decoding and host-keyed request headers |
| `chapter_markdown.go` | HTML→Markdown conversion and title/whitespace cleanup |

| Feature | Detail |
|---------|--------|
| Rate limiting | Random delay between fetches, `DOWNLOAD_MIN_DELAY_MS` (5s) / `DOWNLOAD_MAX_DELAY_MS` (10s); shared process-wide |
| Charset | GBK / GB2312 / GB18030 decoded from `Content-Type` or `<meta charset>` |
| Cloudflare bypass | Direct HTTP first; on transport error, 4xx/5xx or a challenge page, relay through the owner's browser worker. `requiresBrowser: true` routes straight to the worker |
| Parser selection | Per-script `probe()`, first match wins; no match is `not_my_site` |
| Runtime dir | `-parsers-dir` / `PARSERS_DIR`, default `<data-dir>/parsers` |

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
Provider UI → PUT /api/v1/providers/{key}/key → store (encrypted)
                                                    ↓
Job → resolveJobConfig() → aiOptions.provider + model
  → registry.ProviderByID() → baseURL, goai options
  → openai.NewProvider() → goai.Client → HTTP → external API
                                                    ↑
Download Job → parserhost engine + parsers/<site>.js
  → probe(url) → LoadDir selects the matching script
  → toc(ctx,url) → chapter(ctx,url) per new chapter
  → HTML → html-to-markdown → Markdown → store (chapters)
```

## Related codemaps

- [Database](database.md) — Encrypted API keys in `user_provider_settings`
- [Workers](workers.md) — How providers/downloaders are invoked from jobs
- [Backend](backend.md) — Provider config resolution in `runtime_config.go`
