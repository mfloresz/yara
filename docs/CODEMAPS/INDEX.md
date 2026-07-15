# Codebase Overview

**Last Updated:** 2026-07-14
**Entry Points:** `cmd/server/main.go`, `frontend/src/main.ts`
**Language:** Go 1.26.4 (backend), TypeScript/Vue 3 (frontend)
**Module Path:** `translator-server`

## Overview

translator-server is a single Go binary that embeds PocketBase, ships a Vue 3 frontend inside the binary, and exposes a JSON API for translating literary novels with configurable AI providers.

### What it does

1. **Import** novels from the web, EPUB files, or ZIP archives
2. **Preview** chapters and translate them with AI of your choice
3. **Monitor** automated translations in real time
4. **Export** translated novels as EPUB for publishing
5. **Collaborate** with teams using multi-user support
6. **Automate** batch translation, refinement, and content checking

### Architecture

```
──────────────────────────────────────────────────────────┐
│                     HTTP :5176                          │
│  ┌───────────────────────────────────────────────────┐  │
│  │                  PocketBase                        │  │
│  │  ┌──────────┐  ┌──────────┐  ┌────────────────┐  │  │
│  │  │  Auth    │  │  Schema  │  │  File Storage  │  │  │
│  │  └──────────┘  └──────────┘  └────────────────┘  │  │
│  └───────────────────────────────────────────────────┘  │
│                           │                              │
│  ┌───────────────────────────────────────────────────┐  │
│  │              internal/api                         │  │
│  │  ┌──────────┐  ┌──────────┐  ┌────────────────┐  │  │
│  │  │  Routes  │  │ Handlers │  │   Workers      │  │  │
│  │  │router.go │  │router_*.go│  │runtime_*.go    │  │  │
│  │  └──────────┘  └──────────┘  └────────────────┘  │  │
│  └───────────────────────────────────────────────────┘  │
│                           │                              │
│  ┌───────────┬───────────┬┴──────────┬──────────────┐   │
│  │           │           │           │              │   │
│  ▼           ▼           ▼           ▼              ▼   │
│ store/     ai/       secure/   epubimport/  noveldown.  │
│ (PB CRUD)  (prov.)   (AES-GCM) (EPUB parse) (scraper)  │
│                                                         │
│  ┌───────────────────────────────────────────────────┐  │
│  │            Frontend Embed (frontend/dist)          │  │
│  │  Vue 3 + Naive UI + vue-router → SPA fallback    │  │
│  └───────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────┘
```

### Data flow

```
User → HTTP → PocketBase Auth → api.Server → store.Store → SQLite
                                      │
                           ┌──────────┴──────────┐
                           ▼                     ▼
                    downloadQueue         translateQueue
                    (goroutine)           (goroutine)
                           │                     │
                           ▼                     ▼
                    noveldownloader          ai.Provider
                    (8 site parsers)         (5 AI providers)
```

## Key modules

| Module | Purpose | Key files | Dependencies |
|--------|---------|-----------|--------------|
| `cmd/server` | Entry point | `main.go` | config, secure, store, api |
| `internal/api` | HTTP server + routes + workers | 30+ files in `router_*.go`, `runtime_*.go` | store, ai, config |
| `internal/store` | Persistence layer | 18 files, 11 PB collections | pocketbase, secure |
| `internal/ai` | AI providers | `registry.go`, `openai.go`, `provider.go` | goai |
| `internal/secure` | Encryption | `encryption.go` | crypto stdlib |
| `internal/epubimport` | EPUB parser | `parser.go`, `manifest.go`, etc. | goquery, html-to-markdown |
| `internal/noveldownloader` | Web scrapers | 8 site parsers + downloader | goquery, html-to-markdown |
| `frontend/` | Vue 3 SPA | 8 pages, 6 components, 8 composables | vue, naive-ui |
| `cmd/debug-proxy` | Debug proxy micro-server | `main.go` | gorilla/websocket |
| `browser-worker/` | Chrome extension (production) | `service-worker.js` | — |
| `browser-worker-debug/` | Chrome extension (debug, no auth) | `service-worker.js` | — |

## Related codemaps

- [Backend](backend.md) — HTTP routes, handlers, store layer
- [Frontend](frontend.md) — Vue 3 SPA structure
- [Database](database.md) — 11 PocketBase collections
- [Workers](workers.md) — In-process job processing
- [Integrations](integrations.md) — AI providers, web scrapers, EPUB import

## Build and run

```bash
make build                   # Build frontend + Go binary (CGO disabled)
make android                 # Cross-compile for Android/arm64
make dev                     # Print two-terminal dev instructions
go run ./cmd/server          # Run from source (default :5176, ./data)
```

## Testing

```bash
go test ./...                # All tests (unit + integration)
go test -short ./...         # Skip live-URL scraper tests
npm run build                # Frontend typecheck (vue-tsc + vite build)
```

## Ports and configuration

| Variable | Default | Purpose |
|----------|---------|---------|
| `ADDR` | `:5176` | Listen address |
| `DATA_DIR` | `./data` (next to binary) | PB data, uploads, app.key (AES-GCM) |
| `APP_ENCRYPTION_KEY` | auto (app.key) | 32-byte base64/hex encryption key |
| `DOWNLOAD_MIN/MAX_DELAY_MS` | 5000/10000 | Rate limiting for URL imports |
