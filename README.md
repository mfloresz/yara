# translator-server

The all-in-one literary translation platform powering book translation projects.

Build a library of AI-translated novels with full multi-user support, flexible import from multiple sources, EPUB generation, and a powerful dashboard to manage the entire translation pipeline.

## Features

- **AI literary translation** — Translate complete novels using AI providers (Venice, OpenCode Go, Groq, LM Studio, Google Gemma) with customizable prompts
- **Multi-user management** — Each user has their own private collection of novels, chapters, jobs, and EPUBs with fine-grained access control
- **Flexible import** — Import novels from URLs (69shuba, CherryMist, EmpireNovel, FenrirRealm, Fictioneer, FloraeGarden, NovelBin, NovelFire, SkyNovels), EPUB files, or ZIP archives with metadata
- **EPUB generation** — Create original, translated, and refined EPUBs ready for publication
- **Progress tracking** — Monitor in-progress translations, automated jobs, and reading progress
- **In-process workers** — Concurrent download and translation workers that run automatically in the background
- **Android support** — Runs on Termux and Android devices for on-the-go translation
- **Cloudflare bypass** — Chrome extension proxies requests through a real browser for protected sites

## Typical workflows

### 1. Import and translate a novel

```
In the frontend:
1. Click "Import from URL"
2. Paste the novel site URL
3. Select source and target language
4. Click "Preview"
5. Review chapters
6. Click "Translate" or "Import"
```

The system downloads chapters, segments long ones, translates them using AI, and saves everything with tracked progress.

### 2. Manage a translation team

```
In the frontend:
1. Go to Dashboard
2. Click any novel
3. Click "Jobs"
4. Create a batch translation job
5. Assign an AI provider and model
6. Monitor progress in real time
```

Multiple jobs can run simultaneously — the system downloads chapters in parallel while translation proceeds in the background.

### 3. Export for publication

```
In the frontend:
1. Go to any novel
2. Click "EPUBs"
3. Click "Generate translated EPUB"
4. Download the file
```

Generate professional EPUBs in three variants: original, translated, and refined.

## Quick reference

- **Dashboard** — View all novels, quick stats
- **Settings** — AI providers, prompts, language
- **Novels** — Manage collections, chapters, metadata
- **Operations** — Batch jobs, import, refresh
- **Reader** — Read translations with saved progress
- **Login/Register** — Secure multi-user access

## Tech stack

- **Backend** — Go with embedded PocketBase, in-process workers
- **Frontend** — Vue 3 + Naive UI + TypeScript, SPA with router
- **AI** — OpenAI-compatible providers (Venice, OpenCode Go, Groq, LM Studio) + Google Gemma, with encrypted API keys
- **Storage** — SQLite with multi-user isolation, AES-GCM encryption for API keys
- **Mobile** — Built for Android with Termux support

## Setup and run

### Development (local mode)

```bash
# Terminal 1: Frontend (Vite)
cd frontend && npm run dev

# Terminal 2: Backend (Go)
go run ./cmd/server --addr :5176 --data-dir ./data
```

### Build for production

```bash
make build
```

The resulting binary (`./bin/translator-server-linux-amd64-<version>`) runs standalone without dependencies.

### Build for Android

```bash
make android
```

Copy the binary to your phone and run:
```bash
chmod +x translator-server-android-arm64-<version>
./translator-server-android-arm64-<version> --addr 127.0.0.1:5176 --data-dir ./data
```

### Cross-compile all platforms

```bash
make all     # linux-amd64, linux-arm64, linux-armv7, android-arm64, android-armv7
make compress  # Compress all binaries with UPX
```

### Configuration

All options via binary flags:
- `--addr` / `--port` — Listen address
- `--data-dir` — Data storage directory
- `--static-dir` — Serve frontend from disk (development)
- `--migrate-db` — Run legacy schema migration (one-time)
- `--migrate-thumbnails` — Generate thumbnails for existing covers
- `--version` — Print version and exit

Or environment variables:
- `APP_ENCRYPTION_KEY` — Key for API key encryption (32 bytes, base64 or hex)
- `DOWNLOAD_MIN_DELAY_MS` / `DOWNLOAD_MAX_DELAY_MS` — Delay between downloads
- `ADDR`, `PORT`, `DATA_DIR` — Override flag defaults
- `STATIC_DIR` — Override embedded frontend (development)
- `VITE_API_URL` — Frontend API base URL override

## Data flow

```
User → Browser → API Backend → SQLite Database + Workers
                     ↓                    ↓
             Auth (PocketBase)     Download: noveldownloader
                                       ↓
                                   Translate: ai.Provider
```

- **Download** — Fetches chapters from web novels using Go parsers (8 sites supported)
- **Translation** — Sends to AI providers, auto-segments long chapters
- **Refinement** — Reviews and improves translations automatically
- **Storage** — Saves everything with encryption, stats, and tracked progress

## Project structure

- **`cmd/server/`** — Entry point
- **`cmd/debug-proxy/`** — Standalone debug proxy for Cloudflare bypass
- **`internal/api/`** — HTTP endpoints, in-process workers (30+ files)
- **`internal/store/`** — Persistence layer with 11 PocketBase collections
- **`internal/ai/`** — AI providers (5 registered: venice, opencode-go, groq, lmstudio, google)
- **`internal/secure/`** — AES-GCM encryption for API keys
- **`internal/noveldownloader/`** — Web parsers for 8+ sites
- **`internal/epubimport/`** — EPUB file parser
- **`internal/epubexport/`** — EPUB generator
- **`frontend/`** — Vue 3 SPA with 8 pages, 7 components, 8 composables
- **`frontend_embed.go`** — Embeds `frontend/dist/` into the Go binary
- **`browser-worker/`** — Chrome extension (production)
- **`browser-worker-debug/`** — Chrome extension (debug, no auth required)

## Key concepts

- **Collections** — 11 PocketBase collections: users, providers, user settings, novels, chapters, jobs, EPUBs, reading progress, worker tokens
- **Jobs** — Automated tasks (download, translate, refine, check) running in the background
- **Workers** — Two goroutines with buffered queues (download + translate)
- **AI providers** — Venice (default), OpenCode Go, Groq, LM Studio, Google Gemma
- **Browser proxy** — Chrome extension for Cloudflare-protected sites
- **Debug proxy** — Standalone micro-server (port 5177) for parser development with Cloudflare bypass

## Architecture documentation

See [docs/CODEMAPS/INDEX.md](docs/CODEMAPS/INDEX.md) for detailed architecture:

- [Backend codemap](docs/CODEMAPS/backend.md) — Detailed API architecture
- [Frontend codemap](docs/CODEMAPS/frontend.md) — Vue 3 SPA in depth
- [Database codemap](docs/CODEMAPS/database.md) — 11-collection schema
- [Workers codemap](docs/CODEMAPS/workers.md) — In-process job processing
- [Integrations codemap](docs/CODEMAPS/integrations.md) — AI providers, scrapers, EPUB

## Testing

```bash
go test ./...              # All tests (unit + integration)
go test -short ./...       # Skip live-URL scraper tests
npm run build              # Frontend typecheck (vue-tsc + vite build)
```
