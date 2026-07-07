# AGENTS.md

## Design Context

This project uses the `impeccable` skill for frontend work. Before touching UI in `server/frontend`, read:

- `PRODUCT.md` — register (`product`), users, purpose, brand personality, anti-references, design principles.
- `DESIGN.md` — visual system: colors, typography, elevation, components, do's/don'ts (North Star: "The Quiet Shelf").
- `.impeccable/design.json` — machine-readable sidecar (tonal ramps, component snippets) extending DESIGN.md.

## What this is

`translator-server` — single Go binary that embeds PocketBase, ships a Vue frontend inside the binary, and exposes a JSON API for translating literary novels with configurable AI providers. Module path is `translator-server` (Go 1.26.4).

Layout:

- `cmd/server/main.go` — entrypoint. Wires config → encryptor → PocketBase → `store.Store` → `api.Server` → `http.ListenAndServe`.
- `internal/api/` — HTTP layer. `router.go` mounts everything; per-domain files (`router_auth.go`, `router_novels.go`, `router_chapters.go`, `router_jobs.go`, `router_epubs.go`, `router_import.go`, `router_settings.go`, `router_providers.go`, `router_prompts.go`, `router_responses.go`, `router_helpers.go`). `runtime_*.go` contains the in-process job worker and per-job translate/refine/config logic.
- `internal/store/` — PocketBase-backed persistence. `store.go` defines collection name constants; per-domain files (`store_novels.go`, `store_chapters.go`, `store_jobs.go`, `store_epubs.go`, `store_providers.go`, `store_settings.go`, `store_auth.go`, `store_helpers.go`, `store_mapping.go`, `store_schema.go`, `store_db_migrations.go`). All collections are created/seeded by `Store.EnsureSchema()`.
- `internal/ai/` — `Provider` interface plus a single `OpenAIProvider` implementation backed by `github.com/zendev-sh/goai`. The provider catalog lives in `registry.go` (currently: `venice`, `opencode-go`).
- `internal/secure/encryption.go` — AES-GCM encryptor for provider API keys. Key comes from `APP_ENCRYPTION_KEY` (base64 or hex, must decode to 32 bytes) or is auto-generated to `<data-dir>/app.key`.
- `internal/epubimport/`, `internal/noveldownloader/` — pure parsers/scrappers with no HTTP or store dependencies.
- `frontend/` — Vue 3 + Vite + PrimeVue SPA. Vite dev port is fixed at 5175 and proxies `/api` and `/ai` to the Go backend on `127.0.0.1:5176`.
- `frontend_embed.go` — `package translatorserver`, `//go:embed all:frontend/dist`. The Go import alias `translatorserver "translator-server"` (note: matches the module name, NOT the kebab-case path) is what makes the embed reachable from `internal/api`.
- `browser-worker/` — Chrome extension (Manifest V3) that proxies HTTP requests through a real browser to bypass Cloudflare. Requires user authentication via `/api/worker-auth/`.
- `browser-worker-debug/` — Debug version of the browser worker extension. **No authentication required**. Uses a standalone debug proxy server (port 5177). For development/testing with Cloudflare-protected sites. Install as unpacked extension in Chrome developer mode.
- `cmd/debug-proxy/` — Standalone micro-server for debug browser worker. Listens on `:5177`, accepts WebSocket connections without auth, and exposes `POST /api/proxy/fetch` to relay requests through the connected extension. Used for parser development against Cloudflare-protected sites.
- `docs/` — historical planning notes (`pocketbase-multiuser-plan.md`, `go-backend-refactor-plan.md`). Treat as context, not current truth.
- `test/` — gitignored fixtures (EPUBs, chapter text) used by some manual tests. Not used by `go test`.
- `data/` — runtime PocketBase SQLite + uploaded files. Gitignored.

## Build & run

All commands are run from the repo root unless noted.

- `make build` — builds the frontend (`npm install && npm run build`) then compiles `bin/translator-server` with `CGO_ENABLED=0` and `-trimpath -ldflags="-s -w"`. The build is CGO-disabled on purpose so it can be cross-compiled.
- `make android` — same, with `GOOS=android GOARCH=arm64`, output `bin/translator-server-android-arm64`. For Termux; pair with `--data-dir $HOME/data` and a high port (e.g. 5176).
- `make compress` — wraps the built binary with UPX (must be installed).
- `make dev` — prints the two-terminal instructions; does not start anything.
- Run the server: `./bin/translator-server` (defaults: `:5176`, `./data` next to binary) or `go run ./cmd/server --addr :5176 --data-dir ./data`.
- Dev loop — terminal 1 `cd frontend && npm run dev` (port 5175, proxies `/api` and `/ai` to `127.0.0.1:5176`); terminal 2 `go run ./cmd/server --addr :5176 --data-dir ./data`.
- If you change the frontend, re-run `make build` (or `npm run build` in `frontend/`) so `frontend/dist/` reflects your changes. `frontend_embed.go` embeds that directory; stale builds silently serve the old SPA.

## Debug proxy for Cloudflare-protected sites

When adding or debugging parsers for sites protected by Cloudflare, use the debug proxy workflow. This lets the agent fetch real HTML through the user's browser without needing auth tokens.

### When to use

- **Adding a new site** that is behind Cloudflare (detectable by "Just a moment", "Checking your browser", Turnstile challenges, etc.)
- **Debugging a parser** that fails with HTTP errors on Cloudflare-protected sites
- The user explicitly mentions a site is behind Cloudflare

### Workflow

1. **Start the debug proxy** (agent runs this):
   ```bash
   go run ./cmd/debug-proxy &
   ```
   Output shows: `Debug proxy listening on :5177`

2. **User opens Chrome** with the `browser-worker-debug` extension installed. The extension auto-connects to `ws://localhost:5177/ws/browser-worker-debug`. Verify connection:
   ```bash
   curl -s http://localhost:5177/api/workers
   ```

3. **Fetch pages** through the proxy:
   ```bash
   curl -s -X POST http://localhost:5177/api/proxy/fetch \
     -H "Content-Type: application/json" \
     -d '{"url": "https://example.com/novel/", "timeout": 120}'
   ```
   Returns: `{ "status": "ok", "data": { "html": "...", "title": "...", "url": "..." } }`

4. **If Cloudflare challenge appears**, the extension opens a background tab. The user solves the challenge once. Subsequent fetches to the same origin use cached cookies automatically.

5. **When done**, kill the proxy: `pkill -9 -f debug-proxy`

### Key details

- Debug proxy runs on port **5177** (separate from the main server on 5176)
- The `browser-worker-debug` extension uses separate storage (`yara_browser_worker_debug`) — no conflict with the production extension
- The proxy accepts connections without auth — it's a standalone dev tool
- After debugging, the user does `make build` and tests with the production extension (which requires auth)

### If no parser exists yet

1. Fetch the novel info page via proxy to get the HTML structure
2. Inspect the HTML to understand the site's layout (chapter list, pagination, etc.)
3. Write the parser in `internal/noveldownloader/`
4. Register it in the parser catalog
5. Test with: `go test -short ./internal/noveldownloader/...`

### If a parser already exists but fails

1. Fetch the page via proxy to see what the real HTML looks like
2. Compare with what the parser expects
3. Fix the parser's selectors/regex
4. Test again

## Release preparation

The project follows Semantic Versioning (SemVer). Current development stage: **0.x**.

### Version bump policy

- **PATCH** — bug fixes, performance improvements, internal refactoring, dependency updates, documentation-only releases.
- **MINOR** — new user-facing features, new scrapers, new AI providers, new import/export capabilities, significant UI/UX improvements. Before 1.0.0, breaking changes may also be released as MINOR.
- **MAJOR** — breaking changes, incompatible project format, configuration format changes, public API incompatibilities. Not applicable until 1.0.0+.

When uncertain whether a release should be PATCH or MINOR, ask instead of assuming.

### Definition

"Prepare a release" means:
- determine the next version
- review commits since the previous release
- write the changelog
- commit the version bump (if the project has a version reference)
- create the release tag

It does **not** mean:
- pushing commits or tags
- creating the GitHub Release
- merging branches

Those actions require explicit user confirmation.

## Releases & tagging

- Tags must use the `v` prefix (e.g. `v0.1.0`, `v1.2.3`) to trigger the CI release workflow in `.github/workflows/build.yml`.
- The workflow pattern is `v*` — tags like `0.1.0` (without `v`) will **not** trigger the build/release pipeline.
- The workflow builds binaries for linux-amd64, linux-arm64, linux-armv7, android-arm64, and android-armv7, then creates a GitHub Release with all artifacts attached.
- The version number must already have been updated before creating the tag.
- Use annotated tags only: `git tag -a vX.Y.Z -m "Release vX.Y.Z"`. Never create lightweight tags.

### Release workflow (ask the agent)

When asked "create release vX.Y.Z", the agent should:

1. **Check state** — `git status`, `git log --oneline -5`, and verify `git diff vX.Y.Z-1..HEAD` captures the intended changes.
2. **Build frontend** — Run `npm run build` in `frontend/` so `frontend/dist/` is up to date.
3. **Stage & commit** — `git add -A`, then commit with an appropriate conventional-commit prefix (`feat:`, `chore:`, `fix:`).
4. **Tag** — `git tag -a vX.Y.Z -m "Release vX.Y.Z"`.
5. **Push** — `git push origin main --tags`.
6. **Generate changelog** — See `## Changelog` below.

## Changelog

Release notes should use the following sections when applicable, in this order:

- **⚠️ Breaking changes** (prefixed with ⚠️) — any migration steps or config changes required.
- **## What's new** — user-facing features and improvements.
- **## Fixes** — bug fixes.
- **## Housekeeping** — internal refactoring, dependency updates, docs removal, CI changes.

Do not create empty sections. Keep entries concise and user-focused. Group related changes into a single bullet when appropriate. Avoid implementation details unless they affect users.

Every item must correspond to an actual code change — do not invent release notes.

When generating the changelog, run `git log --oneline vPREV..HEAD` and `git diff vPREV..HEAD --stat` against the previous tag. Reference the previous tag URL at the bottom (e.g. `https://github.com/mfloresz/yara/releases/tag/vPREV`).

## Tests & verification

- Backend: `go test ./...`. Integration tests live next to handlers (`internal/api/router_integration_test.go`, `import_url_test.go`, `runtime_config_test.go`, `refine_test.go`, `segmentation_test.go`, `cleaner_test.go`) and `internal/store/store_test.go`. They boot a real PocketBase against `t.TempDir()` via the shared `newAPITestEnv` helper — there is no in-memory mock.
- Frontend: `npm run build` (which runs `vue-tsc -b && vite build`) is the typecheck. There is no separate `npm test`.
- The `realtest_test.go` files in `internal/noveldownloader/` hit live URLs. They are gated by `if testing.Short() { t.Skip(...) }`, so use `go test -short ./...` in CI / local loops and full `go test ./...` only when you specifically want to exercise the scrapers.
- No linter is configured in the repo. `go vet ./...` is the minimum sanity check used by the planning docs.
- The planning docs (`docs/pocketbase-multiuser-plan.md`, `docs/go-backend-refactor-plan.md`) mention `rtk err go build ./cmd/server` / `rtk test go test ./...` as their validation steps — `rtk` is a third-party CLI wrapper for ripgrep-style output. Plain `go build ./cmd/server` and `go test ./...` work too.

## Operational gotchas

- PocketBase is in-process. There is no external PB process, no separate admin port, and no `_/` admin UI exposed by this binary. The HTTP server only serves `/healthz`, `/api/...` (and PocketBase's own `/api/collections/...` routes that the embedded app registers), plus the SPA fallback.
- The embedded `frontend/dist` is only used when `STATIC_DIR` env / `--static-dir` is empty. Set `STATIC_DIR` in dev only if you want the Go binary to serve files from disk instead of the embed; the normal Vite dev workflow does not need it.
- API keys for AI providers are stored encrypted with AES-GCM. The encryptor prefers `APP_ENCRYPTION_KEY` (base64 or hex, exactly 32 bytes decoded). If unset, it generates a random key at `<data-dir>/app.key` on first start. To rotate, set the env var; existing data encrypted with a previous key will be unreadable.
- API keys are write-only: the UI sends them to `PUT /api/user/providers/{key}/key`; `GET /api/user/providers` returns an `apiKeyConfigured` flag and never the secret. Tests assert on that flag, not the value.
- The server refuses to start if it detects a legacy novel schema. If you see `legacy novel schema/data detected; run ./translator-server --migrate-db before starting the server`, run the binary once with `--migrate-db`, then restart normally.
- `EnsureSchema()` no longer backfills chapter char counts or novel stats on boot. Those are kept current per-operation via `RecalculateNovelStats`, called after translate/refine/download jobs, chapter upsert/delete/bulk-delete, import, and copy (see `internal/store/store_chapters.go`, `internal/api/runtime_worker.go`). Don't reintroduce a boot-time full-table backfill; it made startup time scale with total library size instead of with what changed.
- `--data-dir` is resolved to an absolute path at startup. Pass an absolute path (or one relative to the binary's CWD) — the binary does not chdir.
- The job worker (`internal/api/runtime_worker.go`) is in-process with two buffered queues (`downloadQueue` cap 128, `translateQueue` cap 128) and one goroutine each. The `Concurrency` setting on `AISettings` is **persisted but not yet wired into execution** (deliberately per `docs/go-backend-refactor-plan.md`); translation and refine jobs run sequentially per job. Don't add new code that relies on concurrency being honored.
- The downloader supports throttling via `DOWNLOAD_MIN_DELAY_MS` / `DOWNLOAD_MAX_DELAY_MS` env vars (random delay between chapter fetches). They only apply to the import-from-URL flow; they are not exposed as flags.

## Code conventions worth knowing

- HTTP handlers live in `internal/api` and follow one-file-per-resource. Add a new resource by creating `router_<thing>.go` with a `register<Thing>Routes(api, s)` function, then wire it from `registerProtectedRoutes` in `router.go`. Public (unauthenticated) routes go via `registerAuthRoutes` or directly on `router` in `registerRoutes`.
- Store layer returns `store.ErrNotFound` / `store.ErrForbidden` for permission/missing cases. Map them in handlers with `notFoundOrForbidden(e, err)` (in `router_helpers.go`) — don't inline the switch.
- Response shaping is bespoke: handlers return `map[string]any` or call small `*Record(...)` helpers (e.g. `novelRecord`, `jobRecord`, `epubRecord`, `parseJSONFields`) instead of serializing structs directly. The frontend expects this exact shape. Tests in `router_integration_test.go` assert on field names, so changing them is a breaking change.
- All PocketBase collections are defined in code (see `store_schema.go`) and seeded in `EnsureSchema`. There are no JSON migration files. If you add a field, add it to the relevant `ensure*Collection` and use `ensureField` for idempotent migration.
- The `translatorserver` import alias in `internal/api/router.go` and `static.go` is the **module-name alias** for the `translator-server` module — its only job is to expose the `FrontendFS` embed declared in `frontend_embed.go`. The package name on that file is `translatorserver` (single word), which is why the alias matches.
- Frontend uses `vue-router` and the `appServicesKey` provide/inject pattern (`frontend/src/app/services.ts`) for cross-page state. New composables live in `frontend/src/composables/`; new pages in `frontend/src/pages/`. The dev proxy in `frontend/vite.config.ts` proxies `/api` and `/ai` to the Go backend — both are required because some routes are mounted at the root level by PocketBase.
- Don't add `//nolint`, doc-comments explaining obvious code, or new top-level `cmd/...` binaries without checking with the user — the project ships a single binary and the planning docs flag god-object growth as the main risk.

## Database migrations

When a new feature breaks backward compatibility with existing data (schema changes, data backfills, field renames, etc.), use a **manual migration flag** instead of adding auto-migration logic to `EnsureSchema()`.

### When to create a migration flag

- New required fields that need default values for existing records
- Data transformations (e.g., splitting fields, restructuring JSON)
- Renaming/removing collections or fields
- Any change that would break the server if old data is present

### Implementation pattern

1. **Add flag** to `internal/config/config.go`:
   ```go
   MigrateX bool
   // ...
   flag.BoolVar(&cfg.MigrateX, "migrate-x", false, "description of what this migration does")
   ```

2. **Create migration function** in `internal/store/store_db_migrations.go`:
   ```go
   func (s *Store) RunXMigration() error {
       // migration logic
   }
   ```

3. **Handle flag in main.go** (after `EnsureSchema`, before server start):
   ```go
   if *migrateX {
       slog.Info("running X migration")
       if err := st.RunXMigration(); err != nil {
           slog.Error("X migration failed", "error", err)
           os.Exit(1)
       }
       slog.Info("X migration finished, exiting")
       os.Exit(0)
   }
   ```

4. **Document in changelog** under "⚠️ Breaking changes":
   ```
   - Run `./bin/translator-server --migrate-x` before starting the server
   ```

### User workflow

```bash
# Stop old server, start new version with migration flag
./bin/translator-server --migrate-x
# Output: "X migration finished, exiting"

# Start server normally
./bin/translator-server
```

This keeps migration code isolated, avoids boot-time overhead for non-migration runs, and gives users explicit control over when data transformations happen.

## Frontend is a pure consumer

All logic lives in the Go backend. The frontend (`frontend/`) is a thin Vue SPA that only renders state and fires HTTP requests — it does not run jobs, parse EPUBs, call AI providers, or own any business rules. Anything that feels like "real work" (translation, refinement, cleaning, scoring, scheduling, downloading) belongs in `internal/api` / `internal/store` / `internal/ai` / `internal/noveldownloader` / `internal/epubimport`. When extending a feature, push the logic into a new backend handler/store method and have the frontend call it; do not duplicate the logic in TypeScript.

## Where to look first when changing X

- New HTTP route → `internal/api/router.go` (wire-in) + a `router_*.go` file (handler).
- New persistence field → `internal/store/store_schema.go` (collection def) + relevant `store_*.go` (record mapping in `store_mapping.go` and persistence) + `internal/store/settings.go` (struct type if it's a domain object).
- New AI provider → `internal/ai/registry.go` (catalog entry; sets `GoAIOptions` like `useResponsesAPI` and `strictJsonSchema`) and verify `internal/ai/openai.go` honors those options.
- New job operation → extend the switch in `internal/api/runtime_worker.go` (`enqueueJob`) and add a `runtime_*.go` workflow file. Status transitions live in `store_jobs.go`; the worker respects `cancelled` / `done` / `failed` short-circuits.
- Schema/migration change → prefer `ensureField` over touching raw collection JSON; for breaking changes with existing data, use the manual migration flag pattern (see `## Database migrations`).
- Anything that touches the persisted collection names in `internal/store/store.go` (e.g. `NovelsCollection`) is a breaking change for existing `data/` directories.

