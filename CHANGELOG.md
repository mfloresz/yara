# Changelog

## [v0.43.1] - 2026-10-06

### Fixes

- **SPA fallback now returns 404 for `/api/*` paths and dotfile/dot-dir requests** instead of serving `index.html`. Unknown API paths (e.g. `/api/openapi.yaml`, `/api/_`) and hidden files (`.git/HEAD`, `.env`, `.well-known/…`) are correctly rejected with 404 rather than being hidden behind the SPA shell.
- Paths with file extensions that don't match a bundled asset (`/openapi.json`, `/favicon-xyz.png`, …) now 404 instead of falling back to `index.html`.

## [v0.43.0] - 2026-10-05

### What's new

- **Five new site parsers:** brightnovels, flyonthewalls, foxaholic, vritrascans, and mistminthaven. The parser catalog now covers more sites, and the browser-worker extensions' supported-sites lists are updated accordingly.
- **Setup token for first registration.** On a fresh install, the first account must present a setup token (`-bootstrap-secret` / `BOOTSTRAP_SECRET` / auto-generated file at `<data-dir>/setup.key`) to gain admin access. The token file is removed after bootstrap completes.
- **Per-account login rate limiting.** Failed login attempts are now tracked per normalized email (10 per 15 min → 429 + `Retry-After: 900`), closing the multi-IP brute-force gap that IP-keyed limiters alone could not see.
- **Worker token expiry.** Browser-worker authentication tokens now expire, reducing the window of exposure for long-lived tokens.
- **Operations page responsive.** The OperationsPage now renders as mobile-friendly cards and supports sparse fieldsets for lighter list payloads.

### Fixes

- **Brightnovels parser improvements.** Chapters are now filtered by `unlocked_at` date, and premium-locked chapters are skipped at the TOC and gated on access flags during chapter fetch.

### Housekeeping

- Go dependencies updated.

## [v0.42.0] - 2026-10-04

### What's new

- **New source: novelping.** Paste a novelping.com URL and Yara reads its title, author, synopsis, cover, genre and full chapter list. The visible page only ships the first 30 chapters, so the parser pulls the complete catalog from the same request the site's own chapter-archive tab uses.

### Fixes

- **Update checks are fast again.** Verifying a novel against its source took 15–20s more than it used to, and on very long novels it failed outright. The 5–10s anti-scraping gap was being applied to every single request a parser makes, so the two or three catalog pages behind one check were spaced out like chapters — and a novel whose catalog paginates ran past the check's time limit and reported a vague parser error instead of listing its chapters. The gap now applies only between chapter downloads, exactly as before. A check of one novel is back to about two seconds.
- A failed check no longer costs twice as long as it used to. Hitting the parser's time limit is not evidence of an outdated parser script, so it no longer triggers the "download the published script and run the whole check again" retry that ended at the same error.
- The parser's time limit for reading a chapter catalog is raised from 2 to 5 minutes, so a slow site or a browser-worker round-trip on a very long novel no longer lands on the error.

## [v0.41.0] - 2026-10-04

### ⚠️ Breaking changes

- Cleaning a novel's chapters now answers `409` (`novel_busy`) while that novel has an active download/translate/refine job, instead of racing the job over the same content fields. If you drive the API yourself, wait for the jobs to settle before cleaning.

### What's new

- **The assistant proposes cleanups instead of applying them.** Ask it to clean a rule over a novel or an order range and it ends the turn with an interactive approval card: the interpreted rule, its scope, and the full per-chapter diff. The cleanup only runs if you approve it there — there is deliberately no tool that applies a cleanup on its own. This replaces the previous `apply_chapter_cleanup` tool.
- Cleanup proposals can span a whole novel: the approval card fetches the diff hunks without the full chapter texts, so proposing a rule over thousands of chapters no longer transfers tens of megabytes.
- **Operations: filter the update checks by site.** The *Actualizables* chip is now a split button with a per-site dropdown ("Novelfire · 42"), so the pending source checks for one site no longer hide inside the full list — and one click goes back to all sites.
- Chat: a running tool is now a brief shimmer instead of a chip that stays in the transcript with a "ver resultado" toggle; once it finishes, only the assistant's message remains.

### Fixes

- A failed tool no longer ends the assistant's turn: the error is fed back to the model, which corrects itself and can retry within the same turn. Previously a failing terminal tool (asking you a question, proposing a cleanup) left you with no answer, no card and no retry until you typed again.
- The assistant stopped answering with LaTeX math (`$\rightarrow$`, `$\times$`) in a renderer that cannot draw it; it uses Unicode symbols (→, ×) now.
- Import ZIP: a `cover.*` entry is now only the novel cover. It used to be loaded a second time as an inline chapter image, counting against the inline-image budget.

### Housekeeping

- Extracted the cleanup diff renderer shared by the novel's *Limpiar* tab and the chat's approval card into one component, and moved the cleanup rule labels into `frontend/src/utils/cleaner.ts`.
- Refreshed `docs/api/README.md` (the new `proposal` stream event, `propose_cleanup`, the `409` on clean) and `template/import/README.md` (cover vs. inline images, fuller import walkthrough).
- Added tests for the proposal payload, terminal-tool failure semantics, the uncapped cleanup scope, the light `clean-preview-bulk` response and the ZIP cover.

## [v0.40.0] - 2026-10-03

### What's new

- **The library assistant can now act on your library.** Twenty new tools join the chat assistant's catalog (32 in total), all scoped to your own novels: create, cancel and retry translation/refine/check jobs; cancel or retry a job from a past answer; manage the glossary of a novel (view, edit, or generate a fresh draft); trigger source-site update checks and re-downloads; dry-run and apply chapter cleanup; bulk-set chapter status or exclusion across an order range; read reading progress; build EPUB exports; and translate a novel description to try it out before saving.
- **Ask for catalog overviews.** The assistant can list the tags, authors and series in your library with partial, accent-insensitive matching, and search novels by any specific field (title, author, series, tags, status) directly in the query.
- **Long answers without truncation.** Novel listings are now paged (previously nothing past the first 50 novels was reachable through the tool), chapter reads can count lines from the end (read a chapter's ending in one call), and a one-line probe reveals a chapter's exact length before reading it. The assistant is instructed to page through SQL analytics with `COUNT(*)` + `LIMIT`/`OFFSET` instead of presenting a single page as the complete answer.
- **Smarter job creation from analysis.** The assistant can turn a `query_library` finding — e.g. "translations 40% shorter than the original" — directly into re-translation jobs for exactly those chapters, overwriting the previous translation.
- Excluded chapters no longer pollute analyses: they are treated as logically deleted and only surface when you explicitly ask about them.

### Fixes

- A cancelled job can no longer be resurrected to failed/running by a status write that raced the cancellation: the guard now lives in the UPDATE's WHERE clause (mirroring the fast progress path), with benign bookkeeping writes still unconditional. The agent's retry/cancel flow exposed this as an intermittent failure.

### Housekeeping

- Materialized both agent analytics views and added a covering index for chapter stats, so analytical questions stop recomputing joins per query.
- Expanded test coverage for the new assistant tools, projections, guarded job updates and the analytics views; refreshed `docs/api/README.md` for the new tool conventions.

## [v0.39.1] - 2026-10-02

### Fixes

- Restored the Android (armv7) build: a JSON library pulled in through the eino AI framework refuses to compile on 32-bit platforms. It is now substituted with a behaviorally identical `encoding/json` shim, and Android binaries build and run again on armv7 devices.

## [v0.39.0] - 2026-10-02

### ⚠️ Breaking changes

- The AI layer was migrated to [eino](https://github.com/cloudwego/eino), which speaks Chat Completions only: the OpenAI **Responses API is no longer supported**. Providers configured against a Responses-API-only endpoint must switch to one that exposes `/chat/completions` (the old `useResponsesAPI` provider option is gone).
- The `muse-spark-1.3-contributor` and `muse-spark-1.3-contributor-free` models were **removed from the OpenCode Go and OpenCode Zen catalogs**: they only support the Responses API and cannot work over Chat Completions. `muse-spark-1.2-contributor` remains available on the Meta provider. Any user config still pointing at the removed models must be switched to another model.
- Site scrapers are no longer compiled into the binary. The built-in Go scrapers were replaced by editable JavaScript parsers (`parsers/*.js`) that live in your parsers directory and are fetched from a signed release manifest. Existing installations bootstrap the catalog on first use; from now on a parser can be fixed or added without waiting for a server update.
- **Support for `wtr-lab.com` was removed.** Its chapter bodies are only available through a POST request with AES-GCM encrypted payloads, which the parser sandbox cannot express, and the site never parsed successfully after the JavaScript migration. Novels already downloaded from it are unaffected; to keep adding to that library, import it as an EPUB or ZIP. The site can be brought back as a hybrid host-side helper — see `docs/parsers.md`.
- Chapters now carry a stable identity (`source_key`) used to tell new chapters from already-downloaded ones when syncing. The first sync after upgrading backfills identities from your existing library; if that backfill cannot be trusted the server falls back to the previous title/position heuristic instead of re-downloading the whole novel as duplicates.

### What's new

- **Agentic library chat.** A new `/chat` page lets you ask questions about your library in plain language and get answers streamed back. The assistant can list and inspect novels and chapters, search inside chapter text, read aggregate progress statistics, and edit novels and chapters (titles, translations, refined text, status, exclusion) through a scoped tool catalog. It can also answer analytical questions by running a single read-only SQL query over two curated views of your own data — "which novels are within ten chapters of being complete" is one question, not a crawl. When a question is ambiguous it can stop and ask you to pick from options.
- The assistant only ever sees your own library: novels you do not own answer as if they did not exist, and the analytics sandbox runs each query against a private database containing only your rows.
- **Inline images in imported novels.** EPUBs and import zips can carry images. Chapter text keeps them through translation and refinement, the reader and editor render them, and EPUB export embeds them back into the output file.
- Cover URLs now change whenever a cover is re-uploaded, so clients can cache covers for a year instead of re-downloading them.
- Parser scripts are documented in `docs/parsers.md`, including the site contract, the fetch/size limits and the hybrid helpers.

### Fixes

- AI calls have a per-call deadline again. A provider that stops responding no longer leaves a translate or refine job running indefinitely; chat streams instead time out only when they go silent, so a long answer is never cut off mid-flight.
- Transient provider failures are retried again after the AI layer migration (rate limits, 5xx, dropped connections), including errors delivered inside an otherwise successful stream. Genuinely bad requests still fail immediately instead of being retried.
- A stalled stream no longer leaks a goroutine and a held connection per chat turn, and the Gemini provider gained the retries it had lost.
- Chat: fixed concurrent turns losing history, the novel picker never returning results, answers rendering twice, clarification cards losing their options after a reload, and tool chips spinning forever when a result never arrived.
- Tool output and chapter bodies are truncated on character boundaries, so CJK and accented text is no longer corrupted, and the documented size caps are actually enforced. Long chapters are now read in blocks the assistant controls instead of being silently cut off.
- Chat analytics: fixed empty results when a query touched both views, per-user isolation is now structural rather than prompt-dependent, oversized queries are rejected by the engine instead of after the fact, and the sandbox works on targets without a `/tmp` directory (Termux).
- Chat sessions: fixed history trimming that rejected saves for CJK-heavy conversations, and the storage cap that made real conversations fail to save at all.
- Parser fetching: HTTP error responses now fail the fetch instead of being stored as chapter text, so a deleted chapter is no longer reported as a broken parser; private and internal URLs are blocked on every path, including requests routed through the browser worker; response bodies are read through a size limit instead of being buffered without one; and every redirect hop is re-validated.
- Parser syncing: fixed chapter identities being computed from a stale script after a parser auto-update (which could re-download an entire library as duplicates), and sync/check jobs misreporting partially migrated novels as fully new.
- The `chrysanthemumgarden` parser now decodes its obfuscation font properly, and fails loudly instead of storing scrambled text when the protection cannot be decoded.

### Housekeeping

- Removed the built-in Go scrapers and the browser-worker fallback client (~14,000 lines) in favor of the JavaScript parser catalog.
- Added test coverage for the parser engine and its guards, the chat tools, the analytics sandbox and image import/export.
- Refreshed the API documentation for the new image and chat endpoints, parser fetch limits and ownership rules.

## [v0.38.0] - 2026-09-29

### What's new

- Reworked the `cherrymist.cafe` parser: the site is a client-rendered React SPA backed by a JSON API, so novel info, chapter lists and chapter content are now fetched through that API, decoding the cipher that maps Private Use Area codepoints back to readable text using per-seed lookup tables. The browser-worker extensions now share a common `isSupportedUrl` helper.
- AVIF images are now detected in the browser-worker proxy and in EPUB export, so novels with AVIF covers are handled correctly.
- Global novel search now also matches novel tags (`?field=tags`).

### Fixes

- Novels whose source and target languages match are now considered translated: they show up under the `progress=translated` filter even with zero translated chapters, and `sourceLanguage`/`targetLanguage` are normalized to lowercase so the filter matches regardless of case.
- FenrirRealm novel descriptions are stripped of HTML before storing, preserving paragraph breaks as blank lines.

### Housekeeping

- Bumped Go to 1.27 and updated dependencies (PocketBase, goai, golang.org/x/*).
- Job progress updates are now flushed at most twice per second and written with a narrow UPDATE that avoids rewriting full job rows — less database churn on large novels.
- Updated the API documentation (tag search, list projection and sparse fieldset behavior).

## [v0.37.0] - 2026-09-26

### ⚠️ Breaking changes

- Creating a job for a novel that already has a `pending` or `running` job now returns `409 Conflict` instead of queueing a second concurrent job. This applies to `POST /api/v1/jobs`, glossary generation, update-from-url and the batch endpoints.

### What's new

- Jobs now run in parallel across novels instead of one at a time. The two single-consumer queues were replaced by an in-process scheduler that allows one active job per novel, one per AI provider (globally, across users) and one per source site origin, with separate capacity limits for AI and web jobs. A job whose provider or site is busy simply stays `pending` until the resource frees up — it no longer blocks unrelated jobs behind it, and the `503` response is now reserved for a genuinely full queue.
- Cancelling a job that is still waiting in the queue now removes it immediately without running it.
- Added `author` and `series` filters to `GET /api/v1/novels`: exact, case-insensitive, matching source or target values, combinable with the existing filters.
- Added `GET /api/v1/novels/authors/suggestions` for author autocomplete.
- The global novel search now suggests matches by author and series, and the dashboard exposes author/series filters.

### Housekeeping

- Added `docs/job-concurrency-plan.md` describing the scheduling policy and its guarantees.
- Updated the API documentation with the new novel filters, the suggestions endpoint and the scheduling rules.

## [v0.36.1] - 2026-09-24

### What's new

- Added the `glm-5.3-flash` model to the `InferX` provider catalog.
- The EPUB import dialog now accepts drag-and-drop: drop zone with `.epub` validation, file-size display, and support for re-selecting the same file.

### Fixes

- Fixed the chapter preview drawer title overflowing the drawer width on long titles (ellipsis + tooltip, header clamped to drawer width).
- Labeled the debug browser-worker context menu entry as "Añadir historia a Yara (Debug)" so it is distinguishable from the production extension.

## [v0.36.0] - 2026-09-23

### What's new

- Added global novel search in the app header: jump to any novel from anywhere with field-scoped matching (title, author, series, or all) and keyboard navigation.
- Added the `?field=` filter to `GET /api/v1/novels` (`all` | `title` | `author` | `series`, default `all`): scopes `?q` matching to the selected fields. Invalid values fall back to `all` and it is ignored without `?q`.
- Redesigned the auth pages (login, invite, reset password) with a split login-shell layout and a visual hero panel.
- Added a theme switcher to the user menu.

### Housekeeping

- Extracted `AppHeader` into a standalone component with new `useTheme` and `useUserMenu` composables.
- Removed the client-side search from the dashboard: the novel list now always shows all novels, filtered only by the library filters.

## [v0.35.0] - 2026-09-22

### What's new

- Added `POST /api/v1/novels/{id}/translate-description`: synchronous AI translation of the novel synopsis using the project's resolved AI settings, translation prompt and glossary. Nothing is persisted — the project settings dialog fills the target description draft via a new "Traducir" button and the user saves normally.
- The reader now falls back to the original content (session-only, with a warning toast) when the requested translation is not available yet, instead of showing an empty state.

### Fixes

- Blocked PocketBase native auth routes (`/api/collections/*/auth-*`, `/api/oauth2-redirect`) with 404, so `/api/v1/auth/*` is the only auth surface.
- Added a global rate-limit backstop (600 req/min/IP with `429` + `Retry-After`) on every route.

## [v0.34.0] - 2026-09-21

### What's new

- Added the `getinkspired.com` novel downloader parser: story metadata and the chapter list come from the page's JSON-LD (with HTML fallbacks for the header, author row and table of contents), and chapter bodies are extracted paragraph by paragraph so inline formatting survives the markdown conversion. The site sits behind Cloudflare, so it needs the browser-worker extension. The site now appears in the import dialog list and in all four browser-worker extensions.

## [v0.33.1] - 2026-09-21

### Fixes

- Added the `deepseek-v4.1-flash` model to the `inferx` provider catalog.

## [v0.33.0] - 2026-09-18

### What's new

- Reworked the chapter page into a single-surface workspace with tabs, dirty tracking, and a bottom action bar.
- Restructured the settings page into a sectioned layout with navigation, dirty-state tracking, a save indicator, improved token management, and a mobile save bar.
- Translation output no longer forbids Markdown, so source formatting (emphasis and structure) is preserved instead of stripped.
- The chapter list now shows a source-number badge (`Nº`) when the source number differs from the reading position.

### Fixes

- Fixed gap detection to use source numbering (`chapterOrder`) instead of reading position: gaps stay correct after reordering, phantom inline gap rows are suppressed when orderings diverge (missing ranges are shown as a badge instead), and page rendering is clamped to backend-computed gaps.
- Fixed the `inkitt` parser dropping chapters with no `<p>` tags: `<br>`-separated bodies are now split into paragraphs.
- Fixed the reader chapter-list modal stealing focus on open.

### Housekeeping

- Refactored the operations page into reusable components (`OperationsActionBar`, `OperationsCard`, `OperationsRowActions`, `OperationsTranslation`, `OperationsOriginTag`) with composable-driven display logic.
- Extracted reusable settings components (`ProviderKeyField`, `SettingsRow`, `SettingsSection`).

## [v0.32.0] - 2026-09-15

### What's new

- Added a rich text editor to the chapter page: edit chapter content with a formatting toolbar alongside the existing plain-text and Markdown modes (content is saved back as Markdown).

### Fixes

- Fixed duplicate `chapter_order` collisions when importing multi-part source chapters (parts sharing one site number no longer fail on the unique index; new chapters claim distinct orders).

## [v0.31.1] - 2026-09-13

### Fixes

- Fixed the `webnovel` parser misclassifying chapter URLs prefixed with a U+FEFF marker (`%EF%BB%BF`): such links are now recognized as chapters instead of falling back to book info.

## [v0.31.0] - 2026-09-13

### What's new

- Added the `chrysanthemumgarden` novel downloader parser (`chrysanthemumgarden.com/novel-tl/`): supports import-from-URL and chapter downloads, including its font-obfuscated content, and registers the site in all browser-worker extensions.
- Added the `webnovel` novel downloader parser (`webnovel.com`): supports import-from-URL and chapter downloads and appears in the import dialog site list.

## [v0.30.4] - 2026-09-12

### Fixes

- Added the `inception/mercury-2.5` model to the OpenRouter provider catalog.

## [v0.30.3] - 2026-09-12

### Fixes

- Fixed the `gaydemon` parser dropping POV/scene headings (e.g. `#### Julian`): headings, blockquotes, lists, and section breaks are now preserved in document order instead of extracting paragraphs only.
- Fixed chapter cleaning stripping a leading heading unconditionally: it now strips it only when the heading text matches the chapter title.

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

[v0.42.0]: https://github.com/mfloresz/yara/compare/v0.41.0...v0.42.0
[v0.41.0]: https://github.com/mfloresz/yara/compare/v0.40.0...v0.41.0
[v0.40.0]: https://github.com/mfloresz/yara/compare/v0.39.1...v0.40.0
[v0.39.1]: https://github.com/mfloresz/yara/compare/v0.39.0...v0.39.1
[v0.39.0]: https://github.com/mfloresz/yara/compare/v0.38.0...v0.39.0
[v0.38.0]: https://github.com/mfloresz/yara/compare/v0.37.0...v0.38.0
[v0.37.0]: https://github.com/mfloresz/yara/compare/v0.36.1...v0.37.0
[v0.36.1]: https://github.com/mfloresz/yara/compare/v0.36.0...v0.36.1
[v0.36.0]: https://github.com/mfloresz/yara/compare/v0.35.0...v0.36.0
[v0.35.0]: https://github.com/mfloresz/yara/compare/v0.34.0...v0.35.0
[v0.34.0]: https://github.com/mfloresz/yara/compare/v0.33.1...v0.34.0
[v0.33.1]: https://github.com/mfloresz/yara/compare/v0.33.0...v0.33.1
[v0.33.0]: https://github.com/mfloresz/yara/compare/v0.32.0...v0.33.0
[v0.32.0]: https://github.com/mfloresz/yara/compare/v0.31.1...v0.32.0
[v0.31.1]: https://github.com/mfloresz/yara/compare/v0.31.0...v0.31.1
[v0.31.0]: https://github.com/mfloresz/yara/compare/v0.30.4...v0.31.0
[v0.30.4]: https://github.com/mfloresz/yara/compare/v0.30.3...v0.30.4
[v0.30.3]: https://github.com/mfloresz/yara/compare/v0.30.2...v0.30.3
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
[v0.43.1]: https://github.com/mfloresz/yara/compare/v0.43.0...v0.43.1
[Previous release]: https://github.com/mfloresz/yara/releases/tag/v0.43.0
