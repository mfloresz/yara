# Changelog

## [v0.30.2] - 2026-09-12

### Fixes

- Fixed the `floraegarden` parser listing premium/locked chapters: it now skips chapters flagged `_premium` and returns only free ones (e.g. `kingdom-of-the-abyss` lists 77 instead of 282).

## [v0.30.1] - 2026-09-10

### Fixes

- Fixed the `opencode-go` provider model catalog: replaced the stale `deepseek-v4-flash` / `deepseek-flash` entries with `deepseek-v4.1-flash`.

## [v0.30.0] - 2026-09-10

### What's new

- Added opt-in `?neighbors=true` to `GET /api/v1/novels/{id}/chapters/{chapterId}`: the response gains `neighbors: {prev, next}` chapter summaries in reading order (`position`) resolved with two indexed queries, so prev/next navigation no longer needs the full chapter list. Excluded chapters are never neighbors, and legacy rows with `position = 0` fall back to `chapter_order`. Fully backward compatible — the shape is unchanged without the parameter.
- Reworked the reader layout: the chapter sidebar/drawer and its background batch loading are gone, replaced by a bottom navigation bar (previous / chapter list / next). The full chapter list now loads on demand in a modal, and keyboard navigation (arrows, Escape) is preserved. The chapter edit page also navigates via `?neighbors=true` instead of fetching the whole list.

### Fixes

- Fixed the import-from-URL confirm dialog showing a broken cover image: failed cover loads now fall back to the placeholder.

## [v0.29.1] - 2026-09-10

### What's new

- Added the `deepseek-flash` model to the provider catalog; removed the `ox-alpha-free` provider.
- The version shown in the UI no longer carries the `v` prefix (displays `0.29.1` instead of `v0.29.1`).

### Housekeeping

- Documented the release steps in `AGENTS.md` (annotated tags, push, GitHub Release via `gh`).

## [v0.29.0] - 2026-09-09

### What's new

- New Yara branding: a `Y` logo (`docs/yara.svg`), new favicon, regenerated PNG icons, and fresh icons for all four browser-worker extensions; the app shell uses the new mark.
- Shared cover fallback: novels without a cover now resolve to the cacheable `/no_cover.jpg` instead of N authenticated requests, with graceful fallback on image errors (cards, sidebar, settings).
- Browser tab titles: pages now set `Yara - <title>`, using the novel name on detail, chapter, and reader pages.
- Operations page: split `Estado` into `Actualización` and `Traducción` columns with tooltips and counts, same-language (`origen=destino`) handling, and translation-ratio sorting.
- Chapter preview drawer shows the translated title with the original as subtitle when they differ; the clean tab keeps a single Preview → Apply flow.
- Added `gaydemon.com` to the browser-worker extension site lists; Firefox manifest renamed to `Yara Browser Worker`.

## [v0.28.1] - 2026-09-09

### What's new

- Added Mistral `Ministral 8B` / `14B` and `Mistral Small 2603` to the OpenRouter provider catalog.

### Fixes

- Fixed the JobsDrawer content not being closable.

## [v0.28.0] - 2026-09-08

### What's new

- The server version is now exposed via `GET /healthz` (`{ok, version}`, injected at build time) and shown in the UI user menu and mobile nav.
- Added the `gaydemon` novel downloader parser.
- Novels without a cover now get a default cover fallback.
- Updated the `muse-spark` model to `1.3` on the `opencode-go` provider.

## [v0.27.0] - 2026-09-08

### What's new

- Browser-worker extensions (all four variants) gained an `Añadir historia a Yara` context-menu item that deep-links the dashboard import dialog via `?importUrl=`.

## [v0.26.0] - 2026-09-07

### What's new

- Added the `InferX` OpenAI-compatible AI provider.

## [v0.25.1] - 2026-09-06

### Fixes

- Fixed the Empirenovel parser taking the chapter title from the first content line.

## [v0.25.0] - 2026-09-06

### What's new

- Added a chapter preview drawer opened from the chapter list.
- More truthful job progress: glossary jobs report batch-based progress, download jobs show the in-flight chapter title, and the Operations page splits the active refine count into its own badge.
- Added prev/next chapter navigation on the chapter edit page.

## [v0.24.0] - 2026-09-05

### ⚠️ Breaking changes

- Registration is now invitation-only: on a fresh install the first registrant is promoted to admin; afterwards new users need an invitation link. Bootstrap pre-existing installs with `./translator-server -promote-admin <email>`.

### What's new

- User roles (`admin` | `user`) with first-admin bootstrap and a locked-down PocketBase superuser surface.
- Admin panel (`/admin`): users, invitations, shared provider keys, and global prompt overrides; invite redemption page (`/invite/:token`).
- Admin-shared provider API keys with a per-provider sharing toggle (own key wins, then the shared key).
- Admin global prompt overrides with per-user reset (precedence: default < admin global < user < per-novel).
- Account management: password reset, user blocking, and deletion; `logout-all` revokes every session.
- Security hardening: rate limiting on auth endpoints, security headers (CSP, HSTS, …), protected cover file fields behind an ownership-checked `/cover` route, admin-only backup export, browser-worker connection caps, 25 MB zip-bomb caps on EPUB/cover imports, and HTTP server timeouts.
- Added Wattpad and Inkitt novel downloader parsers.
- More reliable backup downloads and worker-auth auto-redirect.

### Fixes

- Closed admin self-promotion and remediated auth review findings.

### Housekeeping

- Documented roles, invitations, shared keys, and the admin surface in `docs/api/`.

## [v0.23.0] - 2026-09-02

### ⚠️ Breaking changes

- Only `/api/v1/*` remains: the legacy aliases (`/api/db/*`, `/api/user/*`, `/api/epubs/*`, `/api/backup/*`, `/api/browser-workers`, `/api/proxy/*`, `/api/defaults`, `/api/translation-jobs/*`) were removed. Reload the SPA to pick up the new frontend.
- Chapter deletes are now logical exclusions, restorable via the visibility endpoint. Run once with `--migrate-chapter-positions` to backfill the new `position` field on existing chapters.

### What's new

- Canonical versioned REST API: `/api/v1/*` with the `{data, meta, links}` envelope, `application/problem+json` errors, `201 + Location` on creates, `204` on deletes, `202` on async jobs, `?page&per_page` pagination, and `?fields=` sparse fieldsets; OpenAPI 3.1 spec plus human docs under `docs/api/`.
- Chapter ordering: a `position` field with atomic append/reorder endpoints (rejected while jobs are active) and logical exclusion with restore.
- Library filters on `GET /api/v1/novels`: `tag`, `shared`, `progress`; novel detail tags act as clickable filters.
- Operations page: bulk delete, owner filter, and translated/total chapter counts with tooltips in the status column.
- Dashboard library header with inline search; novel detail page split into composables.
- Project README rewritten as Yara with a Spanish translation and screenshots; AGPL-3.0 license added.

## [v0.22.0] - 2026-08-24

### What's new

- Operations page status column now shows translated/total chapter counts with tooltips.

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

[Unreleased]: https://github.com/mfloresz/yara/compare/v0.30.1...HEAD
[v0.30.2]: https://github.com/mfloresz/yara/compare/v0.30.1...v0.30.2
[v0.30.1]: https://github.com/mfloresz/yara/compare/v0.30.0...v0.30.1
[v0.30.0]: https://github.com/mfloresz/yara/compare/v0.29.1...v0.30.0
[v0.29.1]: https://github.com/mfloresz/yara/compare/v0.29.0...v0.29.1
[v0.29.0]: https://github.com/mfloresz/yara/compare/v0.28.1...v0.29.0
[v0.28.1]: https://github.com/mfloresz/yara/compare/v0.28.0...v0.28.1
[v0.28.0]: https://github.com/mfloresz/yara/compare/v0.27.0...v0.28.0
[v0.27.0]: https://github.com/mfloresz/yara/compare/v0.26.0...v0.27.0
[v0.26.0]: https://github.com/mfloresz/yara/compare/v0.25.1...v0.26.0
[v0.25.1]: https://github.com/mfloresz/yara/compare/v0.25.0...v0.25.1
[v0.25.0]: https://github.com/mfloresz/yara/compare/v0.24.0...v0.25.0
[v0.24.0]: https://github.com/mfloresz/yara/compare/v0.23.0...v0.24.0
[v0.23.0]: https://github.com/mfloresz/yara/compare/v0.22.0...v0.23.0
[v0.22.0]: https://github.com/mfloresz/yara/compare/v0.21.0...v0.22.0
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
