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
  - [Agent](#agent)
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
- `GET /healthz` — `{ok: true, version: "<build>"}`, público y sin auth. `version` es `main.Version` (ldflags) con fallback `"dev".
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

Use `?fields=id,sourceTitle,status` to request only specific fields. `?select=...` is accepted as an alias. The list endpoints serve a lightweight projection: `glossary`, `prompts`, `notes`, `aiOptions`, `translationOptions`, `cleanupRules` and `customCommands` are not populated there (requesting them returns empty placeholders) — fetch them from `GET /api/v1/novels/{id}`, which returns the full record.

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
| `author` | any string | — | Exact author match, case-insensitive, across source/target author. Combinable with the other filters (AND). |
| `series` | any string | — | Exact series match, case-insensitive, across source/target series. Combinable with the other filters (AND). |
| `field` | `all` \| `title` \| `author` \| `series` \| `tags` | `all` | Scopes `?q` matching (`title` = source/target title, `author` = source/target author, `series` = source/target series, `tags` = tag list). Invalid values fall back to `all`. Ignored without `?q`. |
| `shared` | `all` \| `own` \| `shared` | `all` | `own` = only novels owned by the caller; `shared` = only foreign public novels. Invalid values fall back to `all`. |
| `progress` | `all` \| `translated` \| `completed` \| `ongoing` | `all` | `translated` = `chapter_count > 0 && (source_language = target_language \|\| translated_count = chapter_count)` (0-chapter novels excluded). A novel whose source and target languages match needs no translation, so it qualifies without any translated chapters. `completed`/`ongoing` match the novel `status` (a manual editorial flag, independent of translation progress). Invalid values fall back to `all`. |

Filters combine with AND and with `?q` (which searches title/author/series/tags by default, or the subset selected by `?field=`). With `?tag`/`?author`/`?series` the matching novels are computed in memory before sorting/pagination, so `meta.total` and page navigation always reflect the filtered set.

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
| 409 | Conflict (e.g. `POST /jobs/{id}/retry` on an active job, or creating a job for a novel that already has a pending/running job) |
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
| `POST` | `/api/v1/auth/password-reset/validate` | Validate a reset token. Public. `{ valid, email?, expiresAt? }` (never explains why invalid). |
| `POST` | `/api/v1/auth/password-reset/accept` | Redeem a reset token (`{ token, password≥8 }`) → 200 `{ email }`. Kills all sessions + extension tokens; client clears auth and redirects to login. Public. |
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
| `GET` | `/api/v1/novels/authors/suggestions` | Distinct author values for autocomplete. |
| `GET` | `/api/v1/novels/{id}` | Get one novel. Supports `?fields`/`?select`. |
| `PATCH` | `/api/v1/novels/{id}` | Partial update. |
| `DELETE` | `/api/v1/novels/{id}` | Delete the novel + all its chapters. Status 204. |
| `POST` | `/api/v1/novels/{id}/clone` | Duplicate the novel (translations, glossary, options). Returns 201 + `Location`. |
| `PATCH` | `/api/v1/novels/{id}/visibility` | Body `{ "isPublic": true\|false }`. |
| `POST` | `/api/v1/novels/{id}/cover` | `multipart/form-data` with `cover` field. Returns the updated novel. |
| `GET` | `/api/v1/novels/{id}/cover` | Download the stored cover (thumbnail when present). Cookie-authenticated; the underlying file fields are protected so PocketBase's native `/api/files` route is not usable for covers. Access follows novel visibility (owner or `isPublic`). Novels without a stored cover get `coverPath === ""` and the frontend shows its bundled default image; the endpoint still serves the bundled default for those per-novel URLs as a compat fallback. |
| `POST` | `/api/v1/novels/{id}/recalculate-stats` | Recompute chapter counts and char counts. |
| `POST` | `/api/v1/novels/{id}/translate-description` | Body `{ "sourceText": "..." }` (≤6000 chars). Synchronously translates the description with the project's AI provider (effective prompt + glossary) and returns `{ "translatedText": "..." }`. Nothing is persisted — save through `PATCH /api/v1/novels/{id}`. 502 + `ai_not_configured`/`ai_request_failed` when no provider is usable or the upstream call fails. |
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

`sourceLanguage` and `targetLanguage` are trimmed and lowercased before storage, so `"ES"` is stored as `"es"`. Setting both to the same value marks a novel that needs no translation: it appears under `?progress=translated` regardless of how many chapters have been translated.

### Chapters

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/novels/{id}/chapters` | List chapters. Returns **summaries** by default. `?includeContent=true` returns full records. |
| `GET` | `/api/v1/novels/{id}/chapters/eligible?operation=translate\|refine` | Summaries eligible for that operation. |
| `GET` | `/api/v1/novels/{id}/chapter-summaries?page=&per_page=` | Lightweight, paginated variant. |
| `GET` | `/api/v1/novels/{id}/chapter-stats` | Aggregate char counts. |
| `GET` | `/api/v1/novels/{id}/chapters/gaps` | Detect missing chapter numbers. Also returns `excludedOrders`. |
| `GET` | `/api/v1/novels/{id}/chapters/excluded` | List logically excluded chapters. |
| `GET` | `/api/v1/novels/{id}/chapters/{chapterId}` | Get one chapter (full record). `?neighbors=true` adds `neighbors: {prev, next}` summaries in reading order (`position`) for prev/next navigation without the full list. |
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
| `POST` | `/api/v1/novels/{id}/jobs` | Create a translation/refine/download job. Returns 201 + `Location`. Returns 409 if the novel already has a pending/running job. |
| `GET` | `/api/v1/novels/{id}/jobs` | List jobs for a novel. `?failedOnly=1` filters. |
| `GET` | `/api/v1/jobs/active` | List the user's currently active jobs. |
| `GET` | `/api/v1/jobs/{id}` | Get one job. |
| `POST` | `/api/v1/jobs/{id}/cancel` | Cancel a running or pending job. |
| `POST` | `/api/v1/jobs/{id}/retry` | Re-queue a failed or cancelled job. Returns 409 if already active. |

Jobs are scheduled by an in-process dispatcher with exclusive resource keys: at most one job per novel, at most one job per AI provider (globally, across users) and per source-site origin (scheme + host + port of the job's URLs), plus bounded per-class capacity (AI and web). A job waiting for a busy resource stays `pending`; the 503 is reserved for a saturated queue.

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
| `POST` | `/api/v1/novels/import-zip` | `multipart/form-data`: a project zip with `originals/`, `translated/`, `metadata.json`. Returns 201. Language codes come from `metadata.json` and are trimmed and lowercased. |
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

#### Inline images (EPUB and import-zip)

Novels imported from an EPUB (or a project zip) can carry inline images.
Chapter content never stores image data or URLs — it stores opaque tokens:

- **EPUB import**: every `<img>` whose `src` resolves to a manifest image is
  extracted and replaced in the chapter content by `[[IMG-1]]`, `[[IMG-2]]`…
  (numbered per chapter by order of first appearance). Unresolvable sources
  are left as dead references, as before. Blobs are capped at 20 MB per
  image and 64 MB per book.
- **import-zip**: add an `images/` folder to the project zip (jpg, jpeg, png,
  gif, webp, svg) and reference files from the chapter text with
  `[[IMG:file.jpg]]` markers. On import they are rewritten to `[[IMG-n]]`
  tokens — `originals/` is canonical for the numbering; the same marker in
  `translated/` maps to the same token, and markers unknown to the original
  are dropped. A marker referencing a missing file fails the import (400).
- **Rendering**: full chapter responses (`?includeContent=true`, single
  chapter get, `?neighbors=true`) carry an `images` array (`{id, token, num,
  alt, mime, url}`) when the novel has images; `url` points at the
  authenticated `GET /api/v1/novels/{id}/images/{imageId}` endpoint.
- **Translation**: the model receives the tokens as opaque text and is
  instructed to preserve them verbatim; every translated segment is
  validated (same token multiset as the source) and retried otherwise, and
  refine results that lose or mutate tokens are discarded.
- **EPUB export**: tokens in the exported variant's content are embedded
  back as real images under `OEBPS/images/`; tokens without a stored image
  are dropped.

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
| `GET` | `/api/v1/admin/users` | List every user (id, email, name, role, blocked, dates). |
| `PATCH` | `/api/v1/admin/users/{userId}` | `{ "role": "admin" \| "user" }`. Demoting the last admin returns 409. |
| `GET` | `/api/v1/admin/users/{userId}/stats` | Novel counts + owned novel list for the admin drawer (`ownedCount`, `sharedCount`, `novels[]`). |
| `POST` | `/api/v1/admin/users/{userId}/block` | Block login/API/extensions and kill sessions. Cannot self-block or block the last admin. |
| `POST` | `/api/v1/admin/users/{userId}/unblock` | Unblock a user. |
| `DELETE` | `/api/v1/admin/users/{userId}` | `{ "mode": "with-novels" \| "transfer", "transferToUserId"? }` → 204. 409 for last admin or active jobs. |
| `POST` | `/api/v1/admin/users/{userId}/password-resets` | Issue a single-use reset link → 201 `{ resetUrl, expiresAt }` (24h TTL). 409 `user_blocked` if the user is blocked. |
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

### Agent

AI library assistant over chat. Requires the user's configured AI provider to
be OpenAI-compatible (the Google provider does not support the tool loop and
answers with `400 provider_unsupported`). Tool calls run server-side against
the requesting user's **own** library only; mutations go through the same store
validation as the REST endpoints.

**Ownership is enforced by the backend, not by the model's cooperation.** Every
tool resolves its target through an owner-scoped store method, so a novel
belonging to another user is refused with 404 even when that novel is public
(the REST API does expose public novels, but the assistant does not). Requests
for a foreign `novelId` or `sessionId` are masked as `404`, not `403`, so the
chat never confirms that an id exists in another user's data. Chat sessions are
per user — one session each, enforced by a unique index on the owner.

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/v1/agent/chat` | Run one assistant turn. Body `{ sessionId?, novelId?, message }`. Response is an NDJSON stream of events: `session`, `text_delta`, `tool_call`, `tool_result`, `question`, `done`, `error`. Without `sessionId` the latest session is reused (created when none). Rate limited per IP (burst 10, 20/min). |
| `GET` | `/api/v1/agent/session` | Latest session with its parsed message trail, or `data: null` when the user has none. |
| `DELETE` | `/api/v1/agent/session` | Reset the chat: delete every session of the user → 204. |

Tools the assistant may call: `list_novels` (search + optional `field` scoping
to title/author/series/tags + `offset`/`limit` paging + `hasDescription`/`hasSourceDescription` flags),
`list_tags` and `list_authors` (distinct catalog values with partial,
accent-insensitive matching — "Cris" also finds "TM Cris"), `list_series`
(per-series chapter progress; `complete=complete` answers "which series are
fully translated"), `get_novel`, `get_novel_stats`, `get_novel_chapters`
(summaries, paged by `offset`/`limit` **or** selected as a contiguous block
with `fromOrder`/`toOrder` over `chapter_order`), `get_chapter` (body text as a
line window: `startLine` + `lineCount`, default 60 / max 400, returning
`totalLines` and `nextStartLine` so long chapters are paged rather than cut; a
**negative `startLine` counts from the end**, and `lineCount: 1` probes the
chapter's exact length without pulling its text),
`search_chapters` (case-sensitive literal search over titles and bodies with
snippets), `query_library` (one read-only analytics SELECT over the
`v_agent_novel_progress` / `v_agent_chapter_overview` tables — aggregates like
"novels missing fewer than 10 chapters" in a single call), `get_active_jobs`
and `get_novel_jobs` (narrow job overviews: progress counters, no chapter id
lists), `create_job` (enqueue `translate`/`refine`/`check` over explicit ids
or an order range; one active job per novel), `cancel_job` and `retry_job`,
`get_glossary` / `update_glossary` / `generate_glossary` (read, upsert/remove
by source term, and enqueue generation of a novel's term pairs),
`check_novel_updates` (read-only TOC diff) and `update_novel_from_url`
(enqueues the download of new chapters), `preview_chapter_cleanup` (dry run
with change samples) and `apply_chapter_cleanup` (at most 100 chapters per
call), `get_reading_progress` / `set_reading_progress`, `list_novel_epubs` and
`build_epub` (large novels are refused; the export UI has no cap),
`translate_novel_description` (returns the translated synopsis without saving
it), `update_novel` (target title / author / series / tags / status /
description / notes), `update_chapter` (titles + translated/refined body
replacement; refused while the novel has active jobs), `set_chapter_status`
(`pending|translated|refined|done|failed`), `set_chapter_excluded`,
`bulk_set_chapter_status` and `bulk_set_chapter_excluded` (one call per
`fromOrder`/`toOrder` range; excluded and processing chapters are skipped),
and `ask_user` (clarifying question with clickable options; ends the turn and
the picked option's value arrives as the user's next message).

Every tool result is persisted into the session trail and replayed to the
model on later steps, so the tools return narrow projections with hard caps
(list limits, chapter-id caps of 500 for jobs and 100 for cleanups, truncated
error messages) instead of full records. Complete answers come from paging,
not from bigger caps: `list_novels` pages with `offset`, and `query_library`
lets the model's own `LIMIT ... OFFSET ...` survive inside the wrapped query
(the response reports `truncated` when more rows exist), so a full sweep is a
COUNT followed by raised-offset pages. Job listings read a dedicated
projected query (`chapter_ids` / `options_json` are never loaded), bulk writes
are single conditional UPDATEs, and cleanup preview processes one chapter at a
time — tool peaks stay independent of library size.

Chapter bodies are never silently truncated: the model controls how much it
reads (`startLine`/`lineCount`) and is told whether more remains, so it can
fetch a slice, inspect it and continue. A fixed character cut destroyed the
tail of a chapter with no way for the model to notice or page past it.

#### Turn timeouts

A turn is bounded by two limits, and neither truncates an answer that is still
arriving:

- **Idle gap (60s)** — the provider has 60 seconds to deliver the next stream
  chunk. A provider that accepts the request and then goes silent produces no
  error and no output, which would otherwise leave the chat hanging; this
  catches that case and surfaces it as an `error` event with code
  `provider_stalled`. It bounds the gap *between* chunks, not the total, so a
  slow but active response is never cut off.
- **Whole turn (8 min)** — an upper bound on the turn, including tool
  executions.

Transient provider failures (429, 5xx, dropped connections) are retried twice
with exponential backoff. A retry only happens while the step has emitted
nothing, so a resumed stream can never duplicate text already delivered.

The agent deliberately does **not** use the per-provider timeout from Settings:
a turn is up to 8 sequential model calls, so a deadline sized for a single
translation would abort a legitimate long turn. Translation and refine jobs are
unaffected and keep using their own Settings (or per-novel) timeout, applied as
a per-call context deadline.

#### `query_library` isolation

The assistant's SQL never touches the application database. Each call is
served from a private SQLite sandbox built per request from the requesting
owner's novels and chapters. It contains nothing else: no `users`, no
`_superusers`, no provider keys, no `agent_sessions`, and no other user's
novels. Two layers back that up:

1. **Structural** — the sandbox holds only the two analytics tables, so a
   subquery, `JOIN` or `sqlite_master` read finds nothing beyond those two
   views' own definitions; any other relation fails at the engine with "no
   such table". The rows are selected by owner server-side, and the sandbox
   carries no owner column, so there is no filter for the model to omit or
   override. The sandbox is materialised by attaching `data.db` **read-only**
   and running `CREATE TABLE ... AS SELECT` with a server-written `WHERE
   owner = ?`; the builder then closes its handle, so the attachment does not
   outlive the build and cannot be named by the model's own statement.
2. **Validation** — the query must be a single comment-free `SELECT`/`WITH`
   that reads at least one of the two tables. Statement keywords, `;` and
   comments are rejected, and the check runs against the query with string
   literals blanked out, so a `LIKE '%update%'` pattern is not mistaken for the
   `UPDATE` keyword. The validator deliberately does *not* police which
   relations a query names: an earlier allowlist scanned every `FROM`/`JOIN`
   target, but SQLite's legacy comma join is neither keyword, so
   `FROM v_agent_novel_progress, "sqlite_master"` slipped past it. Layer 1
   covers what the scan could not.

Chapter bodies are never loaded into the sandbox; `get_chapter` serves those.
Both views are always materialised. An earlier version built only the ones the
query named, but SQLite's legacy comma join (`FROM a, b`) is not a FROM/JOIN
keyword, so a missed view was silently emptied and the query returned an empty
result as if it were the truth — a wrong answer is worse than a slower one, and
the covering index keeps the copy cheap either way.

The sandbox is a per-query file under `<data-dir>/agent-sandbox/`, removed when
the query returns. It deliberately does **not** use the system temp dir:
`os.TempDir()` falls back to `/tmp` on any `GOOS=linux` build — which is what
the Termux target is — and `/tmp` does not exist on Android, so `MkdirTemp`
fails and the tool stops working on the project's primary mobile target. The
data dir is already resolved, already writable, and keeps the copy off
tmpfs, where it would otherwise occupy RAM. If the sandbox cannot be created
the tool reports that analytics is unavailable and points the model at the
other tools, rather than failing the turn.

Two further bounds apply at the engine, on the connection that runs the
statement, because the payload limits alone are applied only after the value
has already been built:

- `SQLITE_LIMIT_LENGTH` caps any single string/BLOB at 1 MB. Without it a
  four-byte novel title is enough to write `SELECT printf('%.'||20000000||
  'd',1)`, which allocated ~1.1 GB before Go ever saw a byte and then arrived
  as a 400-rune truncated cell.
- The query runs under a 5 s deadline, and the sandbox is opened
  `query_only`, so the blocklist is a second line of defence rather than the
  only one.

Row results are capped (`limit`, default 50, max 200) and cells are truncated
on a rune boundary. The message trail persisted per session is trimmed to fit
the `messages` field cap by its **JSON-encoded** size, since JSON escapes
expand and a raw-length budget undercounts.

## WebSocket

| Path | Auth | Description |
|---|---|---|
| `ws://host/ws/browser-worker` | in-band (worker sends a `register` message with its token after connect) | Persistent connection for the browser-worker extension. The server dispatches `fetch_page` jobs over this socket. Unauthenticated workers never receive jobs (the `register` message is validated against `ValidateWorkerToken`). |
