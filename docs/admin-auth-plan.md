# Admin panel, invitation-only registration & shared API keys

Plan to replicate keryx-app's auth/admin model in Yara so the app can be safely
exposed to the internet. Written after a full audit of both codebases
(`keryx-app` as the reference implementation, `yara` as the target).

## Decisions (confirmed with the product owner)

1. **Admin panel scope**: core (users, invitations, provider keys) **plus**
   global prompt overrides that act as defaults for users who have not
   customized their own prompts. Settings gets a "reset prompt" action that
   deletes the user's override so the admin global applies. Per-novel custom
   prompts keep precedence over everything.
2. **API keys**: per-provider sharing toggle. A user's own key always wins.
   If the provider is marked shared and the user has no own key, the admin's
   key is used. If the provider is not shared, the user must configure their
   own key to use it.
3. **Exposure**: Cloudflare Tunnel (TLS terminates at Cloudflare edge). If a
   VPS is ever used, TLS is terminated by an independent reverse proxy — Yara
   itself stays HTTP.

## Current state of Yara (verified)

| Concern | Status |
|---|---|
| Registration | **Open** to anyone via `POST /api/v1/auth/register` (`internal/api/router_auth.go`) |
| Roles | None. Single `users` PB auth collection, no `role` field (`internal/store/store_schema.go:83-102`) |
| Data isolation | Solid: PB collection rules (`owner = @request.auth.id`) + store-level `GetNovelAccessible`/`GetOwnedNovel` masking non-owner access as 404 |
| Provider keys | Per-user, AES-256-GCM encrypted in `user_provider_settings` (`internal/store/store_providers.go`) |
| Admin concept | None in app code; PB superuser surface (`/_/`, `/api/collections`, `/api/settings`, `/api/backups`, `/api/logs`, `/api/batch`, `/api/sql`) **is mounted** by `apis.NewRouter` (PocketBase v0.39.4, `apis/base.go:39-56` + `apis/extensions.go:19-27`) despite what AGENTS.md claims |
| PB superuser protection | `superuserIPsWhitelist` middleware exists but does nothing unless `Settings().SuperuserIPs` is configured (default: empty) |
| Rate limiting | None (no `ratelimit.go`, no per-IP/per-account throttles anywhere) |
| Security headers | None beyond the auth cookie (`HttpOnly`, `SameSite=Strict`, `Secure` behind TLS/proxy) |
| CORS | PB defaults (same-origin SPA; acceptable) |
| Password policy | None (PB defaults only) |
| Prompts | `ListPrompts(userID)` (`internal/store/store.go:102-133`): 5 built-in keys (translation/title/refine/check/glossary) with embedded Go defaults, overridden per-user by `user_prompt_settings` rows. Per-novel prompts are a separate layer applied at runtime. |

Keryx reference points worth copying: first-user-becomes-admin bootstrap behind
a mutex; invitation tokens stored **hashed** (SHA-256) with single-use marking
behind a mutex; last-admin demotion guard; in-memory token-bucket rate
limiter; security-headers middleware; audit `slog` lines on every admin
mutation; invitation validate endpoint returns `{valid:false}` without leaking
*why*.

Keryx weaknesses to **fix, not copy**: invitation expiry hard-coded to year
9999 (use a real default, e.g. 7 days); logout does not invalidate the token
server-side (mitigate by rotating the PB token key on password change); login
throttle is the only brute-force defense (acceptable for this scale, flagged).

## Threat model for internet exposure

Ranked by likelihood/impact for this deployment:

1. **PB superuser surface exposed** — if a superuser account ever exists
   (e.g. `superuser upsert` for maintenance), `/_/` + `/api/settings`,
   `/api/backups`, `/api/sql`, `/api/batch` give full DB control from the
   internet. Must be blocked at the Go router regardless.
2. **Credential stuffing / brute force on login** — open login endpoint, no
   throttle. Needs per-IP rate limiting.
3. **Open registration abuse** — anyone can create accounts and consume AI
   quota / disk. Invitation gate fixes this.
4. **API key theft** — keys are encrypted at rest with a single deployment
   key (`APP_ENCRYPTION_KEY` / `data/app.key`). Never log keys, never return
   them in responses (already the case: only `apiKeyConfigured` flag).
5. **Invite token brute force** — tokens must be long, high-entropy
   (`crypto/rand`), stored hashed; validate/accept endpoints rate-limited.
6. **Race conditions** — first-user bootstrap and invitation redemption are
   check-then-act; both must be serialized (mutex) like keryx does.
7. **CSRF** — mitigated by `SameSite=Strict` cookie + JSON-only API. No extra
   CSRF tokens needed (same conclusion as keryx).
8. **Request smuggling / oversized payloads** — apply `http.MaxBytesReader`
   (1 MB) on JSON handlers; PB's `BodyLimit` default already exists but the
   1 MB cap on our own endpoints is stricter.

## Phases

Each phase is independently shippable and ends with `go test -short ./...`
green + `go vet ./...` clean.

### Phase 0 — Lock down the PocketBase surface (do first)

Goal: no PB admin/management surface reachable when exposed via tunnel.

- New middleware in `internal/api/router.go` (bound early, before routes):
  - Return **404** for any path starting `/_/` (superuser UI).
  - Reject requests that carry superuser auth with 404 as well
    (`e.HasSuperuserAuth()`).
- Belt-and-braces: on startup, if `Settings().SuperuserIPs` is empty, set it to
  `["127.0.0.1"]` so PB's own middleware also blocks remote superuser calls
  (covers routes we might not enumerate).
- Keep PB record CRUD `/api/collections/...` reachable: Yara's per-collection
  rules already gate everything to `owner = @request.auth.id`, and the SPA
  relies on some PB defaults. Verify with an integration test that an
  anonymous request to `/api/collections/novels/records` returns 4xx and that
  a non-owner record read returns 404.
- Files: `internal/api/router.go` (middleware), `cmd/server/main.go`
  (superuser IPs), `internal/api/router_integration_test.go` (tests).

### Phase 1 — Roles + first-admin bootstrap

- Schema: add `role` select field (`admin` | `user`) to `users` in
  `ensureUsersCollection` via `ensureField` (idempotent; no manual migration
  flag needed — additive field).
- Store: `GetUserRole(userID)`, `UpdateUserRole(userID, role)` with the
  last-admin guard (refuse to demote/remove the last admin) in
  `internal/store/store_auth.go` or a new `store_users.go`.
- Registration change in `handleAuthRegister` (`router_auth.go`):
  - Under a `bootstrapMu` mutex: if `userCount == 0`, create the user and
    promote to `admin`, return its token.
  - Otherwise the user is created as `user` **only via invitation** (Phase 2);
    until Phase 2 lands, keep registration open creating `role=user` users so
    this phase is safe to ship alone.
- Auth responses: `me`/`login`/`register` responses include `role`
  (`userRecord` shaper in `router_responses.go`).
- Admin guard: `requireAdmin` helper in `internal/api/router_helpers.go`
  (map to `403` + v1 problem+json via `writeV1Error`).
- Existing installs: new CLI flag `-promote-admin <email>` in
  `internal/config/config.go` + wiring in `cmd/server/main.go` (promotes the
  user and exits, following the migration-flag pattern). Fresh installs get
  the automatic first-user promotion.
- Tests: bootstrap race (two concurrent registers — only one admin), promote
  flag, last-admin guard, `role` present in `/api/v1/auth/me`.

### Phase 2 — Invitation-only registration

- New collection `invitations` (admin-only PB rules, mirroring keryx):
  `email`, `token_hash` (unique index), `role`, `expires_at`, `used_at`,
  `created_by`. Schema in `internal/store/store_schema.go`; CRUD in new
  `internal/store/store_invitations.go`.
- Token generation: `crypto/rand` (32 bytes, base64url ≈ 43 chars) — never
  `math/rand`, never UUID v4 (lower entropy). Only the SHA-256 hash is stored.
  Default expiry **7 days** (configurable later via settings if needed).
- Endpoints (v1 envelope, `v1Respond`/`writeV1Error`):
  - `POST /api/v1/admin/invitations` (admin) — body `{email, role}`; 409 if
    email already registered; returns `{data: {invitation, invitationUrl}}`
    with the **raw** token (shown once).
  - `GET /api/v1/admin/invitations` (admin) — list, token hash stripped.
  - `DELETE /api/v1/admin/invitations/{id}` (admin) — revoke unused invite.
  - `POST /api/v1/auth/invitations/validate` (public) — `{valid, email}` only;
    `valid:false` for missing/used/expired without distinguishing.
  - `POST /api/v1/auth/invitations/accept` (public) — `{token, password}`;
    under `invitationMu` mutex; marks `used_at`; creates the user with the
    invited role; password min 8 chars; does **not** auto-login (client then
    calls `/login`, same as keryx).
- Registration gate: after Phase 2, `handleAuthRegister` rejects with 403
  ("registration requires an invitation") whenever `userCount > 0`.
- Audit: `slog.Info("invitation.created", ...)` / `"invitation.redeemed"` /
  `"user.role_changed"` with actor IDs — no emails of invitees in a way that
  leaks, no tokens ever logged (log a hash prefix at most).
- Tests: create/list/revoke (envelope + 201/403/409), validate happy path,
  expired, already-used, accept single-use race (two concurrent accepts —
  exactly one succeeds), wrong-role rejection, gate on open register.

### Phase 3 — Shared provider keys (admin shares / user brings own)

- New collection `shared_provider_keys`: `provider` (unique),
  `api_key_encrypted`, `api_key_configured`, `shared` (bool), `updated_at`,
  `updated_by`. Admin-only PB rules. Reuse `internal/secure` encryptor
  (same `v1:` AES-GCM format).
- Store: `internal/store/store_shared_providers.go` with
  `UpsertSharedProviderKey`, `DeleteSharedProviderKey`,
  `ListSharedProviders`, `GetDecryptedSharedKey(provider)` — guarded by
  `s.Encryptor != nil`.
- Resolution order (modify `ResolveProviderAISettings` in
  `store_providers.go`):
  1. user's own configured key (unchanged, wins),
  2. shared key **if** the provider row has `shared = true`,
  3. error → handlers must surface a clear 409/422 "provider X is not
     configured for this account" instead of failing mid-job; job enqueue
     paths (`runtime_worker.go`) must reject early with the same message.
- Admin endpoints (`internal/api/router_admin.go`, new file):
  - `GET /api/v1/admin/provider-keys` — catalog from `internal/ai/registry.go`
    + `configured` + `shared` flags (never the key).
  - `PUT /api/v1/admin/provider-keys/{provider}` — `{apiKey, shared}` (empty
    key rejected).
  - `DELETE /api/v1/admin/provider-keys/{provider}` — 204.
- User-facing: `GET /api/v1/providers` gains per-provider
  `sharedKeyAvailable` / `usingSharedKey` flags so the settings page can show
  "using the admin's shared key". The user can still POST their own key at any
  time (existing `PUT /providers/{key}/key` unchanged).
- Audit: `provider_key.upsert/delete`, `provider_key.shared_toggled`.
- Tests: resolution precedence (own > shared > error), non-shared provider
  without user key rejects job creation, admin CRUD envelope, key never
  appears in any response body.

### Phase 4 — Global prompt overrides + user reset

- New collection `prompt_overrides`: `key` (one of the 5 built-in prompt
  keys), `system_prompt`, `user_prompt`, `updated_by`. Admin-only rules.
- Resolution in `ListPrompts(userID)` (`internal/store/store.go`):
  `embedded default < admin global override < user row`. Implementation: load
  overrides once per call and treat them as the "defaults" layer before the
  user's records are merged.
- Reset semantics: `DELETE /api/v1/prompts/{key}` (authed, user) deletes the
  user's `user_prompt_settings` row for that key → the global (or embedded)
  default applies again. Frontend adds a "Reset to default" button in
  Settings next to each prompt card.
- Per-novel prompts are untouched: they are applied at runtime above the
  user/global layer and keep winning (verify during implementation with a
  runtime test: novel prompt > user prompt > global > embedded).
- Admin endpoints: `GET /api/v1/admin/prompt-overrides`,
  `PUT /api/v1/admin/prompt-overrides/{key}`, `DELETE .../{key}` (reset to
  embedded). Length clamps: system prompt ≤ 20 000 chars (same clamp as
  user prompts).
- Tests: precedence chain in `ListPrompts`, reset removes user row only,
  admin CRUD, novel-level precedence preserved.

### Phase 5 — Admin panel frontend

- New page `frontend/src/pages/AdminPage.vue` + route `/admin` guarded by a
  new `meta.requiresAdmin` in `frontend/src/router/index.ts` (verify role from
  the auth store; the backend guard is the real gate — this is UX only).
- Tabs: **Users** (list, promote/demote with last-admin guard surfaced from
  API errors), **Invitations** (create → show copyable URL once; list with
  status used/expired/pending; revoke), **Provider keys** (per-provider:
  configure key, toggle `shared`, show `usingSharedKey` count feedback),
  **Global prompts** (edit the 5 keys, reset to embedded).
- API client: extend `frontend/src/api/client.ts` with an `admin` block.
- `frontend/src/app/auth.ts` stores `role` from login/me responses.
- Settings page: "using shared key" badge on providers + prompt reset button
  (Phase 4 UI).
- Register flow: the Register page becomes "accept invitation"
  (`/invite/:token` route calling validate then accept). No open-signup form
  in the UI.
- Respect DESIGN.md ("The Quiet Shelf") — this is normal product UI, use
  existing PrimeVue components/patterns; run the `impeccable` skill review on
  the new page before shipping.
- Verify: `npm run build` (vue-tsc) green.

### Phase 6 — Internet hardening

- **Rate limiting** — port keryx's token-bucket `ratelimit.go` (no external
  deps) into `internal/api/ratelimit.go`:
  - login: 5/min per client IP (honor `CF-Connecting-IP` then
    `X-Forwarded-For` then `RemoteAddr` — safe behind Cloudflare Tunnel which
    always sets `CF-Connecting-IP`),
  - invitation validate/accept: 10/min per IP,
  - authed mutation surfaces: 60/min per user ID,
  - admin routes: 30/min per user ID,
  - bucket sweep for idle entries (memory bound),
  - 429 + `Retry-After: 60`.
- **Security headers middleware** (all responses): `X-Content-Type-Options:
  nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`,
  `Permissions-Policy` locked down, CSP `default-src 'self'` tuned for the SPA
  (PrimeVue needs `style-src 'self' 'unsafe-inline'` — documented exception),
  HSTS (`max-age=63072000; includeSubDomains`) only when
  `X-Forwarded-Proto: https` (set by cloudflared).
- **Body cap**: `http.MaxBytesReader` 1 MB on JSON handlers (new helper in
  `router_helpers.go`).
- **Password change rotates the PB token key** so all existing tokens
  (including stolen ones) die on password change — store-level change in
  `store_auth.go` using PB's token-key rotation.
- **Login hardening**: keep the generic 400 "invalid credentials" (already the
  case); rate limit is the brute-force defense. Per-account lockout is
  **deferred** (flagged, not built — this scale doesn't justify it).
- **Deployment doc**: `docs/exposure.md` — cloudflared config example, what
  Cloudflare features to enable (WAF managed rules optional, always-on TLS),
  `APP_ENCRYPTION_KEY` must be set explicitly in production (not the
  auto-generated `app.key`), backups of `data/` before upgrades.
- Tests: rate limiter unit tests (bucket refill, per-key isolation), headers
  present on `/api/v1/*` and SPA, 404 on `/_/`, login 429 after limit.

### Phase 7 — Docs

- Update `docs/api/openapi.yaml` + `docs/api/README.md` with all new
  endpoints (admin, invitations, shared keys, prompt overrides, role in auth
  responses).
- Update the root `AGENTS.md` operational gotchas: correct the "no `/_/`
  admin UI" claim, document `role`, invitation flow, shared keys, and the
  `-promote-admin` flag.
- Update `frontend` PRODUCT.md/DESIGN.md only if the admin page introduces
  new patterns.

## Conventions that apply to every phase

- New routes: `registerV1<Resource>Routes` wired from `registerV1Routes`
  (`router_v1.go`); responses via `v1Respond`/`v1RespondList`; errors via
  `writeV1Error`; store errors mapped with `notFoundOrForbidden`.
- Store layer: collections defined in `store_schema.go` + `ensureField` for
  additive changes; new collection constants in `store.go`.
- Logging: `slog` plain key-value, error key `"error"`, no secrets/tokens in
  logs, audit lines for every admin mutation.
- Crypto: `crypto/rand` only. Token comparison via SHA-256 hash lookup.
- No new dependencies (token bucket and headers are stdlib-implementable, as
  keryx proves).

## Explicitly deferred

- Email delivery of invitations (admin copies the URL manually, as keryx).
- Per-account lockout / CAPTCHA.
- Persistent audit-log collection (slog lines only; operator ships logs).
- Fine-grained model allow-lists per user (keryx's `user_model_access`) —
  revisit if shared keys make quota abuse a real problem; a cheap v2 is
  per-user monthly token accounting in job stats.
