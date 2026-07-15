# Backend Codemap

**Last Updated:** 2026-07-14
**Entry Points:** `cmd/server/main.go`, `internal/api/router.go`
**Package:** `translator-server`

## Architecture

```
cmd/server/main.go
  └── config.Load()           ← flags + env
  └── secure.NewEncryptor()   ← AES-GCM key
  └── pocketbase.New()        ← embedded PB
  └── store.New()             ← persistence layer
  └── store.EnsureSchema()    ← 11 collections
  └── api.New()               ← Server + workers
  └── api.Router()            ← HTTP mux
  └── http.ListenAndServe()
```

## Key modules

### Entrypoint — `cmd/server/main.go`

| Dependency | Purpose |
|------------|---------|
| `pocketbase` | Framework bootstrap |
| `internal/config` | Config from flags/env |
| `internal/secure` | Encryption for API keys |
| `internal/store` | Persistence, schema |
| `internal/api` | HTTP server, routes, workers |

### HTTP Layer — `internal/api/` (30+ files)

| File | Purpose |
|------|---------|
| `router.go` | `Server` struct, `Router()`, `registerRoutes()`, job cancel registry |
| `router_auth.go` | Register, login, me, refresh, logout |
| `router_backup.go` | Download data directory backup ZIP |
| `router_browser_worker.go` | Browser worker WebSocket handler |
| `router_chapters.go` | Chapter CRUD, clean, status, gaps, summaries |
| `router_epub_export.go` | Build EPUB from novel chapters |
| `router_epubs.go` | Upload, list, preview, download EPUB files |
| `router_helpers.go` | `notFoundOrForbidden()`, response helpers |
| `router_import.go` | Import from EPUB/ZIP/URL, batch operations |
| `router_jobs.go` | Create jobs, list, cancel, active status |
| `router_novels.go` | Novel CRUD, cover, copy, visibility, batch |
| `router_prompts.go` | List, upsert prompt settings |
| `router_providers.go` | Provider settings, API key management |
| `router_proxy.go` | Proxy status, fetch through browser worker |
| `router_reading_progress.go` | Get/save reading progress |
| `router_responses.go` | Response shapers (`novelRecord`, `jobRecord`, etc.) |
| `router_settings.go` | Global user settings, defaults |
| `router_worker_auth.go` | Worker token auth (authorize, validate, approve, revoke) |
| `static.go` | Serves embedded frontend or static dir |
| `runtime_worker.go` | 2 goroutines (downloadQueue, translateQueue), enqueue logic |
| `runtime_translate.go` | `runTranslateChapterDetailed()`, segment pipeline |
| `runtime_refine.go` | `runRefineChapter()` |
| `runtime_config.go` | `resolveJobConfig()`, effective model, defaults |
| `runtime_prompts.go` | Prompt resolution for jobs |
| `runtime_types.go` | Shared runtime types |
| `runtime.go` | `dynamicSystemPrompt()`, formatting helpers |
| `segmentation.go` | Long chapter splitting |
| `cleaner.go` | Post-translation cleanup rules |
| `cleaner_test.go` | Cleanup tests |
| `refine_apply.go` | Apply refinement output |
| `*_test.go` | Integration tests |
| `browser_worker_fallback.go` | Fallback HTTP client for browser worker |
| `proxy_http_client.go` | HTTP client for proxy requests |

### Store Layer — `internal/store/` (18 files)

| File | Purpose |
|------|---------|
| `store.go` | Collection constants, `Store` struct, `EnsureSchema()` |
| `store_schema.go` | 11 collection definitions + field migrations |
| `store_auth.go` | Auth helpers, user CRUD |
| `store_novels.go` | Novel CRUD, stats, batch operations |
| `store_chapters.go` | Chapter CRUD, status updates, reorder, stats |
| `store_jobs.go` | Job CRUD, status transitions, progress |
| `store_epubs.go` | EPUB CRUD |
| `store_providers.go` | Provider settings + API key CRUD |
| `store_settings.go` | Settings CRUD, defaults |
| `store_reading_progress.go` | Reading progress CRUD |
| `store_helpers.go` | Utility functions |
| `store_mapping.go` | `Record` → struct mapping |
| `store_db_migrations.go` | Legacy data migration |
| `thumbnails.go` | Thumbnail generation for covers |
| `prompt_overrides.go` | Novel-level prompt overrides |
| `store_test.go` | Store tests |
| `prompt_overrides_test.go` | Prompt override tests |
| `settings.go` | Domain types: `AISettings`, `TranslationSettings`, etc. |

### AI Layer — `internal/ai/` (8 files)

| File | Purpose |
|------|---------|
| `provider.go` | `Provider` interface (Translate, Refine, Check) |
| `openai.go` | `OpenAIProvider` implementation |
| `registry.go` | `knownProviders` (venice, opencode-go, groq, lmstudio, google) |
| `translation_schema.go` | JSON schemas for AI responses |
| `*_test.go` | Tests |

### Config — `internal/config/config.go`

| Field | Source | Default |
|-------|--------|---------|
| `Addr` | `--addr` / `ADDR` | `:5176` |
| `Port` | `--port` / `PORT` | — |
| `DataDir` | `--data-dir` / `DATA_DIR` | `./data` next to binary |
| `StaticDir` | `--static-dir` / `STATIC_DIR` | — |
| `AppEncryptionKey` | `APP_ENCRYPTION_KEY` | — |
| `DownloadMinDelayMs` | `DOWNLOAD_MIN_DELAY_MS` | 5000 |
| `DownloadMaxDelayMs` | `DOWNLOAD_MAX_DELAY_MS` | 10000 |
| `MigrateDB` | `--migrate-db` | false |

## API route tree

```
/healthz                                    [public]
/api/auth/register                          [public]
/api/auth/login                             [public]
/api/auth/me                                [auth]
/api/auth/refresh                           [auth]
/api/auth/logout                            [auth]
/api/worker-auth/authorize                  [public]
/api/worker-auth/validate                   [public]
/api/worker-auth/callback                   [public]
/api/worker-auth/approve                    [auth]
/api/worker-auth/revoke/{id}                [auth]
/api/worker-auth/delete/{id}                [auth]
/api/worker-auth/tokens                     [auth]
/api/user/settings                          [auth]
/api/user/providers                         [auth]
/api/user/providers/{key}/key               [auth]
/api/user/prompts                           [auth]
/api/user/prompts/{key}                     [auth]
/api/user/novels/{novelId}/reading-progress [auth]
/api/defaults                               [auth]
/api/db/novels                              [auth]
/api/db/novels/{id}                         [auth]
/api/db/novels/{id}/cover                   [auth]
/api/db/novels/{id}/copy                    [auth]
/api/db/novels/{id}/visibility              [auth]
/api/db/novels/{id}/update-preview          [auth]
/api/db/novels/{id}/update-from-url         [auth]
/api/db/novels/tags/suggestions             [auth]
/api/db/novels/series/suggestions           [auth]
/api/db/novels/import-epub                  [auth]
/api/db/novels/import-from-zip              [auth]
/api/db/novels/preview-from-url             [auth]
/api/db/novels/import-from-url              [auth]
/api/db/novels/check-batch-updates          [auth]
/api/db/novels/batch-update-from-url        [auth]
/api/db/novels/batch-translate-preview      [auth]
/api/db/novels/batch-translate              [auth]
/api/db/novels/batch-check                  [auth]
/api/db/novels/{novelId}/chapters                 [auth]
/api/db/novels/{novelId}/chapters/{chapterId}     [auth]
/api/db/novels/{novelId}/chapters/clean           [auth]
/api/db/novels/{novelId}/chapters/clean-preview   [auth]
/api/db/novels/{novelId}/chapters/eligible        [auth]
/api/db/novels/{novelId}/chapters/full            [auth]
/api/db/novels/{novelId}/chapters/bulk-delete     [auth]
/api/db/novels/{novelId}/chapters/gaps            [auth]
/api/db/novels/{novelId}/chapter-summaries        [auth]
/api/db/novels/{novelId}/chapter-stats            [auth]
/api/db/novels/{novelId}/translation-jobs         [auth]
/api/db/translation-jobs/active/status            [auth]
/api/db/translation-jobs/active                   [auth]
/api/db/translation-jobs/{jobId}                  [auth]
/api/epubs                                        [auth]
/api/epubs/preview                                [auth]
/api/epubs/build                                  [auth]
/api/epubs/{id}/download                          [auth]
/api/proxy/fetch                                  [auth]
/api/proxy/status                                 [auth]
/api/browser-workers                              [auth]
/api/backup/download                              [auth]
/ws/browser-worker                       [public]
/{path...}                               [SPA fallback]
```

## Data flow

```
Request → PocketBase auth middleware → handler
  → store.Store method → PocketBase DAO → SQLite
  ↓ (if job)
  → enqueueJob() → channel → worker goroutine
      → download: noveldownloader → store
      → translate: ai.Provider → store
```

## Tests

| File | Type |
|------|------|
| `router_integration_test.go` | Integration (boots real PB via `newAPITestEnv`) |
| `import_url_test.go` | Integration |
| `runtime_config_test.go` | Unit |
| `refine_test.go` | Unit |
| `segmentation_test.go` | Unit |
| `cleaner_test.go` | Unit |
| `store_test.go` | Unit |

## Related codemaps

- [Database](database.md) — All collections and schema
- [Workers](workers.md) — Job processing details
- [Integrations](integrations.md) — External services
