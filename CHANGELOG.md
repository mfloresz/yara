# Changelog

## [v0.21.0] - 2026-08-24

### What's new

- Added per-novel title prompt overrides (`title_system_prompt` / `title_user_prompt`) with full-stack support: schema, store mapping, `NovelPromptOverrides.Title`, API (`promptOverrides.title`), and frontend prompt types. The project settings dialog now includes a dedicated "Título" prompt editor alongside translation/refine/check.
- Added `follow-global` toggle for per-project title model configuration: when enabled (default, `titleEnabled: null`) the project inherits the global title provider/model and shows the current global value in an info alert; when disabled, a distinct title provider/model can be configured per project with automatic cleanup of stale values on save.
- Added `tencent/hy-mt2-1.8b` model to the OpenRouter provider catalog.

### Fixes

- Shortened the `check` job operation label to "Comprobando.." for a more compact jobs UI (`useJobHelpers`).

## [v0.20.0] - 2026-08-23

### What's new

- Added `requiresBrowser` field to novels (API, domain, and downloader) with per-parser `RequiresBrowser()` detection for Cloudflare and JavaScript-rendered sites; the flag is now selectable via the novel list API (`select=requiresBrowser`).
- Overhauled the Operations page with search, refined filters (updates/active), a bulk action toolbar, and a sticky selection bar.

### Fixes

- Routed `check` jobs through the download queue instead of the translate queue so source-site checks no longer get blocked behind long-running AI translate/refine jobs.

### Housekeeping

- Replaced the centralized `BrowserRequiredSites` map (`browser_required.go`) with per-parser `RequiresBrowser()` implementations on every parser.
- Updated integration and worker documentation (`AGENTS.md`, `docs/CODEMAPS/`) to reflect the new parser pattern and queue routing.

## [v0.19.0] - 2026-08-23

### What's new

- Added `OpenCode Zen` provider (`opencode-zen`) at `https://opencode.ai/zen/v1` with three free models: `x-preview-f-free` (default), `mimo-v2.5-free`, and `muse-spark-1.2-contributor-free`. The `muse-spark` variant uses the OpenAI Responses API via per-model `ModelOptions` override.

## [v0.18.0] - 2026-08-23

### What's new

- Added `novelarrow.com` parser and downloader support (metadata + chapter content extraction with dedicated test coverage).
- Added range-based chapter selection ("Rango" mode) in the novel detail page, with selectable checkboxes gated by operation type (translate vs refine) and auto-cleared selection on mode change.
- Added per-entry enable/disable toggle and `includeExisting` option for glossary generation: disabled entries are skipped when extracting terms and formatting glossary prompts; the "Existing Glossary" section is omitted when no terms are present.
- Added concurrent translation support per AI provider: configurable `concurrency` (1..10, default 1) wired via `errgroup.SetLimit`; concurrent mode auto-disables `includePreviousTitleHints` (sequential requirement) with WARN logging.
- Added `tencent/hy-mt2-30b-a3b` model to the OpenRouter provider catalog.

### Housekeeping

- Removed stray `CLARIFY_BEFORE_AFTER.md` and `CLARIFY_CHANGES.md` artifacts.

## [v0.17.1] - 2026-08-19

### What's new

- Added the `muse-spark-1.2-contributor` model to the OpenCode Go provider. This model uses the OpenAI Responses API instead of chat completions.

## [v0.17.0] - 2026-08-18

### What's new

- Added selectable `gpt-5.6-luna` reasoning variants for the OpenCode Go provider.
- Added a server-authoritative `canUpdate` field so the dashboard's update filter stays aligned with the supported parser catalog.

### Fixes

- Fixed ZIP imports treating empty translated chapter files as translated content.

## [v0.16.0] - 2026-08-10

### What's new

- Added ZIP import to the dashboard: novels can now be imported from a `.zip` archive (with `metadata.json`, cover, and `originals/`/`translated/` chapter folders) directly from the UI.

## [v0.15.0] - 2026-08-10

### What's new

- Added sorting to the novels dashboard: sort by title, creation date, or last-read across listings and search, with the order preserved while browsing pages.
- Added shared-novel indicators and improved mobile search controls on the dashboard.

### Fixes

- Streamlined chapter cleaning result feedback in the novel detail page.

## [v0.14.2] - 2026-08-09

### Fixes

- Fixed Novelfire chapter ordering for novels with decimal-numbered chapters (e.g. "92.1", "92.2") by using the sequential URL numbers as the canonical order, avoiding duplicate order collisions on import.
- Improved job queue rejection handling: jobs are now rejected with clear feedback when the queue is full, and chapter statuses are reconciled for rejected jobs.
- Added redownload conflict detection to prevent concurrent re-downloads of the same novel.

## [v0.14.1] - 2026-08-08

### Fixes

- Fixed the bulk clean preview failing when a diff hunk has empty before/after content: line arrays are now serialized as `[]` instead of `null` and the frontend display guards against missing arrays.

## [v0.14.0] - 2026-08-08

### What's new

- Added bulk clean preview with line-level diffs: select any number of chapters, see exactly which lines will change, and apply the cleaning directly from the preview.
- Added re-download of chapters from the source URL (novel settings). Only the original content is replaced; existing translations and refinements are preserved. A confirmation step warns when the source chapter titles no longer match the stored ones.
- OpenRouter `luna` models now use the flex tier, roughly halving cost at higher latency.

### Fixes

- Fixed job titles overflowing in the jobs drawer.

### Housekeeping

- Removed stray agent session artifacts from the repository.

## [v0.13.0] - 2026-08-07

### What's new

- Added the WTR Lab parser (`wtr-lab.com`) for novel downloading, targeting the raw "web" reading mode and supporting AES-GCM decryption of chapter content.

### Fixes

- Fixed the FenrirRealm parser to skip premium (paywalled) chapters instead of failing with a cryptic TipTap content parse error.

## [v0.12.0] - 2026-08-06

### What's new

- Added Firefox browser extension support for Cloudflare bypass, including both production (authenticated) and debug (unauthenticated) variants that mirror the existing Chrome extensions.

### Fixes

- Updated Livewire catalog loading to prioritize direct component-snapshot requests over scrolling.
- Updated chapter order extraction to prefer parser-provided episode numbers over title-based heuristics.

### Housekeeping

- Reorganized browser extension directory structure for consistency across Chrome and Firefox variants.

## [v0.11.1] - 2026-08-05

## Fixes

- Fixed SkyDemonOrder Livewire catalog loading: browser worker now keeps the catalog tab active, uses viewport-based scroll steps, directly fetches the Livewire component snapshot, and waits for the chapter catalog marker before extraction.
- Fixed browser worker WebSocket read limit (32 MB) to accommodate large Livewire catalog responses.
- Fixed Go WebSocket read deadline handling for browser worker connections.

## [v0.11.0] - 2026-08-05

## What's new

- Added the SkyDemonOrder parser for novel downloading, with full Livewire chapter-catalog support.
- Added `fetch_livewire` browser-worker operation to render JavaScript-dependent project pages through the browser.
- Added browser worker URL helpers (`browserWorkerWebSocketURL`, `browserWorkerHTTPURL`) for consistent protocol handling across extensions.
- Improved footnote rendering in the reader: footnote references render as [1], [2], etc., and the footnotes section is styled distinctly from body text.
- Improved markdown processing for the reader page (footnote support in `markdownToHtml`).
- SkyDemonOrder project pages now detect missing chapter catalogs in direct HTTP responses and retry through the browser worker automatically.

## Fixes

- Fixed fallback client to detect SkyDemonOrder 200-but-not-rendered responses and retry through the browser before falling back to chapter-walking.
- Fixed browser worker reconnect logic and URL construction to handle `ws://`, `wss://`, `http://`, and `https://` server addresses correctly.

[Unreleased]: https://github.com/mfloresz/yara/compare/v0.21.0...HEAD
[v0.21.0]: https://github.com/mfloresz/yara/compare/v0.20.0...v0.21.0
[v0.20.0]: https://github.com/mfloresz/yara/compare/v0.19.0...v0.20.0
[v0.19.0]: https://github.com/mfloresz/yara/compare/v0.18.0...v0.19.0
[v0.18.0]: https://github.com/mfloresz/yara/compare/v0.17.1...v0.18.0
[v0.17.1]: https://github.com/mfloresz/yara/compare/v0.17.0...v0.17.1
[v0.17.0]: https://github.com/mfloresz/yara/compare/v0.16.0...v0.17.0
[v0.16.0]: https://github.com/mfloresz/yara/compare/v0.15.0...v0.16.0
[v0.15.0]: https://github.com/mfloresz/yara/compare/v0.14.2...v0.15.0
[v0.14.2]: https://github.com/mfloresz/yara/compare/v0.14.1...v0.14.2
[v0.14.1]: https://github.com/mfloresz/yara/compare/v0.14.0...v0.14.1
[v0.14.0]: https://github.com/mfloresz/yara/compare/v0.13.1...v0.14.0
[v0.13.0]: https://github.com/mfloresz/yara/compare/v0.12.0...v0.13.0
[v0.12.0]: https://github.com/mfloresz/yara/compare/v0.11.1...v0.12.0
[v0.11.1]: https://github.com/mfloresz/yara/compare/v0.11.0...v0.11.1
[v0.11.0]: https://github.com/mfloresz/yara/compare/v0.10.0...v0.11.0
[v0.10.0]: https://github.com/mfloresz/yara/compare/v0.9.0...v0.10.0
[Previous release]: https://github.com/mfloresz/yara/releases/tag/v0.10.0
