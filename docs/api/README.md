# translator-server API (`/api/v1`)

This is the canonical reference for the **Yara** translator-server HTTP API.

The API has a single surface: `/api/v1/*` — REST-shaped, envelope-wrapped, semantically correct status codes. There are no legacy aliases.

The machine-readable spec is [`openapi.yaml`](./openapi.yaml) (OpenAPI 3.1). When the two diverge, the running server is the source of truth.

## Table of contents

- [Base URL & versioning](#base-url--versioning)
- [Authentication](#authentication)
- [Envelope, pagination, fields](#envelope-pagination-fields)
- [Status codes](#status-codes)
- [Errors](#errors)
- [Resources](#resources)
  - [Auth](#auth)
  - [Novels](#novels)
  - [Chapters](#chapters)
  - [Jobs](#jobs)
  - [EPUBs](#epubs)
  - [Glossary](#glossary)
  - [Prompts](#prompts)
  - [Providers](#providers)
  - [Settings](#settings)
  - [Reading progress](#reading-progress)
  - [Imports / downloads](#imports--downloads)
  - [Backup](#backup)
  - [Browser workers & proxy](#browser-workers--proxy)
  - [Worker auth](#worker-auth)
  - [Admin](#admin)
- [WebSocket](#websocket)

## Base URL & versioning

| Environment | Base URL |
|---|---|
| Local dev (Vite proxies to Go) | `http://127.0.0.1:5175/api/v1` |
| Direct (Go binary) | `http://127.0.0.1:5176/api/v1` |
| Android / Termux | same binary, configurable via `--addr` |

Every v1 response carries `X-API-Version: v1`.

## Authentication

The API uses PocketBase-compatible auth tokens. Two ways to send a token:

1. **HttpOnly cookie** — `auth.token=<token>`. This is the default after login. `Path=/`, `SameSite=Strict`, `Secure` when the request is HTTPS.
2. **Authorization header** — `Authorization: Bearer <token>`. Accepted for callers that already hold a token (e.g. issued out-of-band); the browser-worker extension uses its own worker tokens instead.

Login, register and refresh deliver the session token **only** in the `auth.token` cookie — never in the response body — so it stays unreadable to JavaScript. After authenticating, send the token in either form on every request to a non-public endpoint. The server checks the cookie first (if no Authorization header is present) via the `loadAuthFromCookie` middleware in `router_auth.go`.

Public endpoints (no auth required):

- `POST /api/v1/auth/register`
- `POST /api/v1/auth/login`
- `GET /api/worker-auth/authorize` and `/api/worker-auth/callback` (the extension OAuth flow)
- `GET /healthz`
- `GET /ws/browser-worker` (WebSocket, authenticates in-band)

## Envelope, pagination, fields

### Single resource

```json
{ "data": { "id": "...", "sourceTitle": "..." } }
```

### Collection (paginated)

```json
{
  "data": [ { "id": "...", "title": "..." } ],
  "meta": {
    "total": 3,
    "page": 1,
    "per_page": 50,
    "limit": 50,
    "offset": 0,
    "has_more": true,
    "next_page": 2
  },
  "links": {
    "self":  "/api/v1/novels?page=1&per_page=50",
    "next":  "/api/v1/novels?page=2&per_page=50",
    "prev":  "/api/v1/novels?page=1&per_page=50",
    "first": "/api/v1/novels?page=1&per_page=50",
    "last":  "/api/v1/novels?page=1&per_page=50"
  }
}
```

### Pagination params

| Param | Default | Max | Notes |
|---|---|---|---|
| `page` | `1` | — | canonical offset pagination |
| `per_page` | `50` | `200` | canonical page size |
| `limit` | — | `200` | compat (same effect as `per_page`) |
| `offset` | `0` | — | compat (same effect as `(page-1)*per_page`) |
| `cursor` | — | — | reserved; currently treated as `limit` token |

If both `page`/`per_page` and `limit`/`offset` are sent, the canonical form wins.

### Sparse fieldsets

Use `?fields=id,sourceTitle,status` to request only specific fields. `?select=...` is accepted as an alias. Heavy fields excluded by default in sparse mode: `coverPath`, `glossary`, `tags`, `aiOptions`, `translationOptions`, `cleanupRules`, `sourceDescription`, `targetDescription`, `notes`.

**Example (lightweight list):**

```http
GET /api/v1/novels?fields=id,sourceTitle,status,chapterCount
```

**Example (full single novel):**

```http
GET /api/v1/novels/abc123
```

### Library filters on `GET /api/v1/novels`

| Param | Values | Default | Notes |
|---|---|---|---|
| `tag` | any string | — | Exact tag match, case-insensitive and accent-insensitive (`fantasia` matches `Fantasía`, `FANTASÍA`, `Ação` ↔ `acao`). Novels without tags are excluded. Combinable with the other filters (AND). |
| `shared` | `all` \| `own` \| `shared` | `all` | `own` = only novels owned by the caller; `shared` = only foreign public novels. Invalid values fall back to `all`. |
| `progress` | `all` \| `translated` \| `completed` \| `ongoing` | `all` | `translated` = `chapter_count > 0 && translated_count = chapter_count` (0-chapter novels excluded); `completed`/`ongoing` match the novel `status`. Invalid values fall back to `all`. |

Filters combine with AND and with `?q` (which still searches title/author/series only — not tags). With `?tag` the matching novels are computed in memory before sorting/pagination, so `meta.total` and page navigation always reflect the filtered set.

**Example:**

```http
GET /api/v1/novels?tag=fantasia&shared=own&progress=ongoing
```

## Status codes

| Code | Meaning |
|---|---|
| 200 | OK |
| 201 | Created — `Location: <v1 resource URL>` header is set on the response |
| 202 | Accepted — async work started (download, batch translate, batch check) |
| 204 | No Content — delete succeeded |
| 400 | Bad request (malformed body, missing required field) |
| 401 | Unauthorized |
| 403 | Forbidden (resource belongs to another user) |
| 404 | Not found |
| 409 | Conflict (e.g. `POST /jobs/{id}/retry` on an active job) |
| 422 | Validation failure (reserved; current handlers map validation to 400) |
| 500 | Internal error |
| 503 | Job queue full — response carries `Retry-After: 30` and the message `jobQueueFullMessage` |

## Errors

v1 errors return `Content-Type: application/problem+json`:

```json
{
  "error": {
    "code": "validation_failed",
    "message": "sourceTitle is required",
    "details": [
      { "field": "sourceTitle", "message": "must not be empty", "code": "required" }
    ]
  }
}
```

| `code` | HTTP status |
|---|---|
| `bad_request` | 400 |
| `unauthorized` | 401 |
| `forbidden` | 403 |
| `not_found` | 404 |
| `conflict` | 409 |
| `queue_full` | 503 |
| `internal_error` | 500 |

## Resources

### Auth

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/v1/auth/register` | Create the **first** user (becomes admin). Status 201. Returns `403` once any user exists — registration is invitation-only afterwards. |
| `GET` | `/api/v1/auth/setup-status` | `{ needsSetup }` — true while the install has no users. Public. |
| `POST` | `/api/v1/auth/invitations/validate` | Validate an invitation token. Public. `{ valid, email?, role?, expiresAt? }`. |
| `POST` | `/api/v1/auth/invitations/accept` | Redeem an invitation (`{ token, password }`), create the invited user. Status 201. Public. |
| `POST` | `/api/v1/auth/login` | Exchange email + password for a session. Returns `{ user }` + sets `auth.token` cookie. Status 200. |
| `GET` | `/api/v1/auth/me` | Return the authenticated user (includes `role`). |
| `POST` | `/api/v1/auth/refresh` | Refresh the current session. Returns `{ user }` + re-issues the cookie. |
| `POST` | `/api/v1/auth/logout` | Clear the cookie. Status 204. The token itself stays valid until expiry — use logout-all to revoke it. |
| `POST` | `/api/v1/auth/logout-all` | Invalidate every session of the calling user (all devices) by rotating the server-side token key, and clear the cookie. Status 204. |

Register, login and the invitation endpoints are rate-limited per client IP
(register/login: 5/min, invitations: 10/min; 429 + `Retry-After: 60` beyond
that) and cap request bodies at 16 KB. Forwarded-IP headers
(`CF-Connecting-IP`, `X-Forwarded-For`) are only honored for connections from
loopback — the supported cloudflared-on-localhost deployment — so a direct
caller cannot rotate its rate-limit key by spoofing them.

`GET /api/v1/auth/me` returns the standard single-resource envelope:

```json
{ "data": { "id": "...", "email": "alice@example.com", "name": "Alice", "role": "user", "theme": "system" } }
```

```json
// POST /api/v1/auth/register
// request
{ "email": "alice@example.com", "password": "secret123", "name": "Alice" }
// response (201) — the session token is only set as the auth.token cookie
{ "data": { "user": { "id": "...", "email": "alice@example.com", "name": "Alice", "role": "admin", "theme": "system" } } }
```

### Novels

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/novels` | List the user's novels. Supports `?q`, `?sort`, `?order`, `?fields`/`?select`, pagination. |
| `POST` | `/api/v1/novels` | Create a novel. Returns 201 + `Location`. |
| `GET` | `/api/v1/novels/tags/suggestions` | Distinct tag values for autocomplete. |
| `GET` | `/api/v1/novels/series/suggestions` | Distinct series values for autocomplete. |
| `GET` | `/api/v1/novels/{id}` | Get one novel. Supports `?fields`/`?select`. |
| `PATCH` | `/api/v1/novels/{id}` | Partial update. |
| `DELETE` | `/api/v1/novels/{id}` | Delete the novel + all its chapters. Status 204. |
| `POST` | `/api/v1/novels/{id}/clone` | Duplicate the novel (translations, glossary, options). Returns 201 + `Location`. |
| `PATCH` | `/api/v1/novels/{id}/visibility` | Body `{ "isPublic": true\|false }`. |
| `POST` | `/api/v1/novels/{id}/cover` | `multipart/form-data` with `cover` field. Returns the updated novel. |
| `GET` | `/api/v1/novels/{id}/cover` | Download the stored cover (thumbnail when present). Cookie-authenticated; the underlying file fields are protected so PocketBase's native `/api/files` route is not usable for covers. Access follows novel visibility (owner or `isPublic`). |
| `POST` | `/api/v1/novels/{id}/recalculate-stats` | Recompute chapter counts and char counts. |
| `GET` | `/api/v1/novels/{id}/full` | Return the novel + all chapters (heavy). |

```json
// GET /api/v1/novels?fields=id,sourceTitle,status,chapterCount&page=1&per_page=20
{
  "data": [
    { "id": "abc123", "sourceTitle": "Reverend Insanity", "status": "ongoing", "chapterCount": 1174 }
  ],
  "meta": { "total": 1, "page": 1, "per_page": 20, "limit": 20, "offset": 0, "has_more": false },
  "links": { "self": "/api/v1/novels?page=1&per_page=20" }
}
```

```json
// POST /api/v1/novels
// request
{
  "sourceTitle": "Reverend Insanity",
  "sourceAuthor": "Gu Zhen Re",
  "sourceLanguage": "en",
  "targetLanguage": "es",
  "url": "https://example.com/novel/reverend-insanity"
}
// response (201) — also sets Location: /api/v1/novels/<id>
{ "data": { "id": "abc123", "sourceTitle": "Reverend Insanity", "status": "ongoing", "chapterCount": 0, "canUpdate": true, "requiresBrowser": false, ... } }
```

### Chapters

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/novels/{id}/chapters` | List chapters. Returns **summaries** by default. `?includeContent=true` returns full records. |
| `GET` | `/api/v1/novels/{id}/chapters/eligible?operation=translate\|refine` | Summaries eligible for that operation. |
| `GET` | `/api/v1/novels/{id}/chapter-summaries?page=&per_page=` | Lightweight, paginated variant. |
| `GET` | `/api/v1/novels/{id}/chapter-stats` | Aggregate char counts. |
| `GET` | `/api/v1/novels/{id}/chapters/gaps` | Detect missing chapter numbers. Also returns `excludedOrders`. |
| `GET` | `/api/v1/novels/{id}/chapters/excluded` | List logically excluded chapters. |
| `GET` | `/api/v1/novels/{id}/chapters/{chapterId}` | Get one chapter (full record). |
| `POST` | `/api/v1/novels/{id}/chapters` | Upsert a chapter. Accepts an optional `position`. Returns 201 + `Location`. |
| `PATCH` | `/api/v1/novels/{id}/chapters/order` | Reorder chapters. Body `{ "chapterIds": ["id1", "id2"] }`. 409 if jobs are active on the novel. |
| `PATCH` | `/api/v1/novels/{id}/chapters/{chapterId}/visibility` | Toggle logical exclusion. Body `{ "excluded": true \| false }`. |
| `PATCH` | `/api/v1/novels/{id}/chapters/{chapterId}/status` | Body `{ "status": "pending", "errorMessage": "" }`. |
| `POST` | `/api/v1/novels/{id}/chapters/bulk-delete` | Body `{ "ids": ["id1", "id2"] }`. Logical exclusion — chapters are hidden, not removed. |
| `DELETE` | `/api/v1/novels/{id}/chapters/{chapterId}` | Logical exclusion — the chapter is hidden but retained. Status 204. |
| `POST` | `/api/v1/novels/{id}/chapters/clean` | Apply cleaning rules to a list of chapters. |
| `POST` | `/api/v1/novels/{id}/chapters/clean-preview` | Preview a clean operation without persisting. |
| `POST` | `/api/v1/novels/{id}/chapters/clean-preview-bulk` | Bulk preview. |

```json
// GET /api/v1/novels/abc123/chapter-summaries?page=1&per_page=10
{
  "data": [
    {
      "id": "ch1", "novelId": "abc123", "chapterOrder": 1,
      "title": "Chapter 1", "translatedTitle": "Capítulo 1",
      "status": "completed", "errorMessage": "",
      "hasOriginalContent": true, "hasTranslatedContent": true, "hasRefinedContent": false,
      "originalChars": 1820, "translatedChars": 1942, "refinedChars": 0
    }
  ],
  "meta": { "total": 1, "page": 1, "per_page": 10, "limit": 10, "offset": 0, "has_more": false },
  "links": { "self": "/api/v1/novels/abc123/chapter-summaries?page=1&per_page=10" }
}
```

```json
// GET /api/v1/novels/abc123/chapter-stats
{ "data": { "totalChapters": 1174, "completedChapters": 200, "translatedChapters": 200, "originalCharacters": 5234210, "translatedCharacters": 5431000, "refinedCharacters": 0, "totalCharacters": 10665210, "maxChapterOrder": 1174 } }
```

### Jobs

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/v1/novels/{id}/jobs` | Create a translation/refine/download job. Returns 201 + `Location`. |
| `GET` | `/api/v1/novels/{id}/jobs` | List jobs for a novel. `?failedOnly=1` filters. |
| `GET` | `/api/v1/jobs/active` | List the user's currently active jobs. |
| `GET` | `/api/v1/jobs/{id}` | Get one job. |
| `POST` | `/api/v1/jobs/{id}/cancel` | Cancel a running or pending job. |
| `POST` | `/api/v1/jobs/{id}/retry` | Re-queue a failed or cancelled job. Returns 409 if already active. |

```json
// POST /api/v1/novels/abc123/jobs
// request
{
  "operation": "translate",
  "chapterIds": ["ch1", "ch2"],
  "options": { "provider": "opencode-go", "model": "kimi-k2", "concurrency": 1 }
}
// response (201) — Location: /api/v1/jobs/<jobId>
{ "data": { "id": "job1", "novelId": "abc123", "status": "pending", "operation": "translate", "provider": "opencode-go", "model": "kimi-k2", "totalChapters": 2, "completedChapters": 0, "failedChapters": 0, "chapterIds": ["ch1", "ch2"], "autoSegmentEnabled": false, "autoSegmentActive": false, "autoSegmentCount": 0, "autoSegmentCompletedCount": 0, "newChapters": 0 } }
```

### EPUBs

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/novels/{id}/epubs` | List EPUBs for a novel. |
| `POST` | `/api/v1/novels/{id}/epubs` | Upload an EPUB file. Returns 201 + `Location`. |
| `POST` | `/api/v1/epubs` | Flat upload (no novel in path). |
| `GET` | `/api/v1/epubs/{id}/download` | Download the EPUB binary. `Cache-Control: no-store`. |
| `POST` | `/api/v1/epubs/preview` | Parse an EPUB without persisting it. |
| `POST` | `/api/v1/epubs/build` | Build an EPUB from a novel's existing chapters. Body `{ "novelId": "...", "source": "original"\|"translated"\|"refined" }`. Returns 201. |

```json
// GET /api/v1/novels/abc123/epubs
{ "data": [ { "id": "ep1", "novelId": "abc123", "fileKind": "translated", "sourceVariant": "translated", "label": "source=translated", "fileName": "reverend-insanity.epub", "url": "/api/v1/epubs/ep1/download" } ], "meta": { "total": 1, "has_more": false }, "links": { "self": "/api/v1/novels/abc123/epubs" } }
```

### Glossary

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/v1/novels/{id}/glossary/generate` | Generate a glossary. Returns 202 + `Location` to the created job. |
| `GET` | `/api/v1/novels/{id}/glossary/estimate-tokens?from=N&to=M` | Estimate token cost before generating. |

```json
// POST /api/v1/novels/abc123/glossary/generate
// request
{ "chapterFrom": 1, "chapterTo": 50, "mode": "together", "maxTokensPerBatch": 8000, "provider": "opencode-go", "model": "kimi-k2", "includeExisting": true }
// response (202) — Location: /api/v1/jobs/<jobId>
{ "data": { "jobId": "job1", "status": "pending", "operation": "generate-glossary" } }
```

```json
// GET /api/v1/novels/abc123/glossary/estimate-tokens?from=1&to=50
{ "data": { "totalTokens": 124000, "chapterCount": 50 } }
```

### Prompts

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/prompts` | List the user's prompts. |
| `PUT` | `/api/v1/prompts/{key}` | Create or update a prompt. Body: `{ "label", "description", "prompt": { "systemPrompt", "userPrompt" }, "active" }`. |
| `DELETE` | `/api/v1/prompts/{key}` | Reset the user's own prompt (idempotent): deletes the personal override so the admin global (or embedded default) applies again. Per-novel prompts are untouched. Status 200 with the effective prompt. |

```json
// PUT /api/v1/prompts/translation
// request
{
  "label": "Translation v2",
  "description": "Tone-preserving translation",
  "prompt": {
    "systemPrompt": "You are a literary translator...",
    "userPrompt": "Translate the following chapter to {{.TargetLanguage}}:\n\n{{.OriginalContent}}"
  },
  "active": true
}
```

### Providers

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/providers` | List the user's provider settings (API key is never returned, only `apiKeyConfigured: bool`). |
| `PUT` | `/api/v1/providers/{providerKey}` | Update `model`, `baseUrl`, `timeoutMs`, `concurrency`. |
| `PUT` | `/api/v1/providers/{providerKey}/key` | Body `{ "apiKey": "..." }` — write-only, encrypted at rest. |
| `DELETE` | `/api/v1/providers/{providerKey}/key` | Delete the stored API key. Status 204. |

```json
// GET /api/v1/providers
{
  "data": {
    "providers": [
      { "key": "venice", "model": "llama-3.3-70b", "baseUrl": "", "timeoutMs": 60000, "concurrency": 1, "apiKeyConfigured": true },
      { "key": "opencode-go", "model": "kimi-k2", "baseUrl": "", "timeoutMs": 120000, "concurrency": 1, "apiKeyConfigured": false }
    ]
  }
}
```

### Settings

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/defaults` | Global default translation values. |
| `GET` | `/api/v1/settings` | Get the user's app settings (`theme`, `ai`, `titleProvider`, `titleModel`, `translation`). |
| `PUT` | `/api/v1/settings` | Update settings (same body shape). |

```json
// GET /api/v1/settings
{
  "data": {
    "theme": "dark",
    "ai": { "provider": "opencode-go", "model": "kimi-k2", "concurrency": 1, "timeoutMs": 120000 },
    "titleProvider": "opencode-go",
    "titleModel": "kimi-k2",
    "translation": { "includePreviousTitleHints": false, "includePreviousContentHints": true, "autoSegment": false }
  }
}
```

### Reading progress

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/novels/{id}/reading-progress` | Get the user's last-read chapter and scroll position. |
| `PUT` | `/api/v1/novels/{id}/reading-progress` | Body `{ "chapterId": "...", "scrollPercent": 0.42 }`. |

### Imports / downloads

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/v1/novels/import-epub` | `multipart/form-data`: `file` (EPUB), `sourceLanguage`, `targetLanguage`. Returns 201 + `Location`. |
| `POST` | `/api/v1/novels/import-zip` | `multipart/form-data`: a project zip with `originals/`, `translated/`, `metadata.json`. Returns 201. |
| `POST` | `/api/v1/novels/preview-from-url` | Body `{ "url": "..." }` — fetch and return novel metadata + chapter list (cached for 30 min). |
| `POST` | `/api/v1/novels/import-from-url` | Body `{ "url", "sourceLanguage", "targetLanguage", "startChapter", "endChapter" }`. Creates a novel, downloads the first chapter synchronously, and enqueues a download job for the rest. Returns 201. |
| `POST` | `/api/v1/novels/{id}/check-preview` | Re-fetch the source, count new chapters, update `lastCheckedAt`. |
| `POST` | `/api/v1/novels/{id}/update-from-url` | Body `{ "startChapter": 0, "endChapter": 0 }` — enqueue a download for new chapters. Returns 202. |
| `POST` | `/api/v1/novels/{id}/redownload-from-url` | Body `{ "startChapter", "endChapter", "confirm": false\|true }`. First call returns the plan; second call (with `confirm: true`) re-queues. Returns 202 on accept. |
| `POST` | `/api/v1/novels/batch-check` | Body `{ "novelIds": ["..."] }` — enqueue one check job per novel. Returns 202. |
| `POST` | `/api/v1/novels/batch-update` | Body `{ "selections": [...] }` — same as `redownload-from-url` for many novels. Returns 202. |
| `POST` | `/api/v1/novels/batch-translate-preview` | Pre-flight: returns the per-novel pending chapter count. |
| `POST` | `/api/v1/novels/batch-translate` | Body `{ "selections": [{ "novelId", "chapterIds": [...] }] }`. Returns 202. |
| `POST` | `/api/v1/novels/batch-check-scheduled` | Same shape as `batch-check`. Returns 202. |

```json
// POST /api/v1/novels/import-from-url
// request
{ "url": "https://example.com/novel/reverend-insanity/", "sourceLanguage": "en", "targetLanguage": "es", "startChapter": 1, "endChapter": 1174 }
// response (201)
{
  "data": {
    "novel": { "id": "abc123", "sourceTitle": "Reverend Insanity", ... },
    "chaptersImported": 1,
    "totalChapters": 1174,
    "downloadJob": { "id": "job1", "totalChapters": 1173 }
  }
}
```

### Backup

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/v1/admin/backups/export` | **Admin role required.** Stream a `backup-YYYYMMDD-HHMMSS.zip` of the entire `data-dir` (includes the app encryption key). `Content-Type: application/zip`. |

`POST` (not `GET`) because generating a fresh archive is not idempotent in the GET sense. The endpoint lives under `/admin` because the archive contains every user's data.

### Browser workers & proxy

These routes drive the connected browser-worker extension (used to bypass Cloudflare / Turnstile on the user's behalf).

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/browser-workers` | List the user's connected browser workers. |
| `POST` | `/api/v1/proxy/fetch` | Body `{ "url": "...", "timeout": 120 }` — fetch through the browser. Returns 400 if no worker is connected. |

```json
// GET /api/v1/browser-workers
{ "data": { "count": 1, "workers": [ { "id": "w1", "browser": "chrome", "version": "1.0.0", "state": "connected", "capabilities": ["fetch_page"], "connectedAt": "2026-01-01T00:00:00Z", "lastHeartbeat": "2026-01-01T00:00:30Z" } ] } }
```

### Worker auth

Browser-worker extension OAuth-style flow. The HTML pages are not part of the JSON envelope; the JSON endpoints are wrapped on v1.

| Method | Path | Auth | Description |
|---|---|---|---|
| `GET` | `/api/worker-auth/authorize?extension_id=...` | cookie | Renders the consent page. |
| `GET` | `/api/worker-auth/validate?token=...` or `Authorization: Bearer ...` | none | Validate a worker token. Returns `{ valid, userId, extensionId, label }`. |
| `GET` | `/api/worker-auth/callback?token=...&user=...` | none | Final page that closes the popup. |
| `POST` | `/api/v1/worker-auth/approve` | user | Form submit: `{ "state": "..." }`. Returns HTML, not JSON. |
| `GET` | `/api/v1/worker-auth/tokens` | user | List the user's worker tokens. |
| `POST` | `/api/v1/worker-auth/revoke/{tokenId}` | user | Revoke (disconnect, keep record). |
| `POST` | `/api/v1/worker-auth/delete/{tokenId}` | user | Delete the record. |

### Admin

All `/api/v1/admin/*` routes require the authenticated user to have the
`admin` role; everyone else gets `403` (`problem+json`).

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/admin/users` | List every user (id, email, name, role, dates). |
| `PATCH` | `/api/v1/admin/users/{userId}` | `{ "role": "admin" \| "user" }`. Demoting the last admin returns 409. |
| `GET` | `/api/v1/admin/invitations` | List invitations (email, role, expiry, used status). |
| `POST` | `/api/v1/admin/invitations` | `{ "email", "role" }` → 201 with the shareable `invitationUrl` (raw token shown **once**; only its SHA-256 hash is stored; expires in 7 days). 409 if the email is already registered. |
| `DELETE` | `/api/v1/admin/invitations/{invitationId}` | Revoke an unused invitation. 204. |
| `GET` | `/api/v1/admin/provider-keys` | Provider catalog with `configured` / `shared` flags. Never returns key material. |
| `PUT` | `/api/v1/admin/provider-keys/{providerKey}` | `{ "apiKey"?, "shared" }`. `apiKey` is required on first configuration; omit it to toggle sharing only. |
| `DELETE` | `/api/v1/admin/provider-keys/{providerKey}` | Delete the shared key. 204. |
| `GET` | `/api/v1/admin/prompts` | Effective prompts (embedded default + global override) with `hasOverride` flag. Same precedence the user sees, minus the per-user layer. |
| `GET` | `/api/v1/admin/prompt-overrides` | List global prompt overrides. |
| `PUT` | `/api/v1/admin/prompt-overrides/{promptKey}` | `{ "prompt": { "systemPrompt", "userPrompt" } }` for keys `translation`, `title`, `refine`, `check`, `glossary` (≤ 20 000 chars). |
| `DELETE` | `/api/v1/admin/prompt-overrides/{promptKey}` | Remove the override so the embedded default applies. 204. |

**Shared provider key resolution order:** the user's own configured key wins;
when the user has none and the provider is marked `shared`, the admin's key
is used. `GET /api/v1/providers` exposes `sharedKeyAvailable` and
`usingSharedKey` per provider so clients can display it.

**Prompt precedence:** embedded default < admin global override < user
setting < per-novel prompt.

## WebSocket

| Path | Auth | Description |
|---|---|---|
| `ws://host/ws/browser-worker` | in-band (worker sends a `register` message with its token after connect) | Persistent connection for the browser-worker extension. The server dispatches `fetch_page` jobs over this socket. Unauthenticated workers never receive jobs (the `register` message is validated against `ValidateWorkerToken`). |
