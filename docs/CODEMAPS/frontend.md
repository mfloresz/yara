# Frontend Codemap

**Last Updated:** 2026-07-14
**Entry Points:** `frontend/src/main.ts`, `frontend/src/app/App.vue`
**Framework:** Vue 3 + TypeScript + Vite + Naive UI
**Dev Port:** 5175 (proxies `/api` and `/ai` to `127.0.0.1:5176`)

## Architecture

```
frontend/
├── index.html
├── vite.config.ts            ← proxy /api, /ai → :5176
├── tsconfig.json
├── package.json
└── src/
    ├── main.ts               ← createApp, Naive UI, router mount
    ├── vite-env.d.ts
    ├── app/                  ← App.vue, auth, services, styles
    ├── api/                  ← HTTP transport, API client, types
    ├── components/           ← Reusable UI components (6)
    ├── composables/          ← Domain composables (8)
    ├── config/               ← Language options
    ├── domain/               ← TS types and domain models
    ├── features/             ← Feature-specific dialogs
    ├── pages/                ← Route pages (8)
    ├── router/               ← Vue Router config
    ├── theme/                ← Naive UI theme overrides
    └── utils/                ← Utilities (markdown, cleaner, EPUB)
```

## Key modules

### App Shell — `src/app/`

| File | Purpose |
|------|---------|
| `App.vue` | Root component |
| `auth.ts` | Auth state (token, user, theme), login/logout/restore |
| `services.ts` | `AppServices` provide/inject pattern (api, auth, media) |
| `styles.css` | Global styles |

### API Layer — `src/api/`

| File | Purpose |
|------|---------|
| `http.ts` | `HttpTransport` — fetch wrapper with auth header injection |
| `client.ts` | `ApiClient` — all endpoint methods grouped by domain |
| `types.ts` | TypeScript types for API responses |

### Pages — `src/pages/` (8)

| Route | Component | Purpose |
|-------|-----------|---------|
| `/` | `DashboardPage.vue` | Novel list, quick stats |
| `/settings` | `SettingsPage.vue` | Global settings, providers, prompts |
| `/operations` | `OperationsPage.vue` | Batch operations, job monitoring |
| `/novels/:novelId` | `NovelDetailPage.vue` | Novel metadata, chapter list, actions |
| `/novels/:novelId/chapters/:chapterId` | `ChapterPage.vue` | Chapter editor (original + translated) |
| `/novels/:novelId/read` | `ReaderPage.vue` | Reading view with scroll progress |
| `/login` | `LoginPage.vue` | Login form |
| `/register` | `RegisterPage.vue` | Registration form |

### Components — `src/components/` (6)

| Component | Purpose |
|-----------|---------|
| `AppLayout.vue` | Sidebar + topbar layout shell |
| `ChapterList.vue` | Chapter list with status indicators |
| `FieldNumber.vue` | Number input with validation |
| `JobsDrawer.vue` | Slide-out drawer for active jobs |
| `LibrarySkeleton.vue` | Loading skeleton for novel library |
| `NovelCard.vue` | Novel card with cover, title, stats |
| `PromptRoleEditor.vue` | Editor for system/user prompts |

### Composables — `src/composables/` (8)

| Composable | Purpose |
|------------|---------|
| `useNovels.ts` | Novel CRUD operations |
| `useChapters.ts` | Chapter CRUD + reorder + clean |
| `useTranslationJobs.ts` | Job creation, status polling |
| `useActiveJobs.ts` | Active job list with live updates |
| `useActiveJobStatus.ts` | Single job status polling |
| `useProjectSettings.ts` | Project-level settings (glossary, prompts, AI) |
| `useProviders.ts` | Provider listing + API key management |
| `useReadingProgress.ts` | Reading progress tracking |

### Router — `src/router/index.ts`

8 routes with auth guards (`requiresAuth`, `guestOnly` meta fields) and redirect logic.

### Theme — `src/theme/naive-theme.ts`

Custom Naive UI theme overrides ("pixeo" theme) with light and dark variants.

### Utils — `src/utils/`

| File | Purpose |
|------|---------|
| `api-base-url.ts` | Base URL resolution |
| `cleaner.ts` | Text cleaning rules |
| `epub-importer.ts` | EPUB file import utility |
| `job-events.ts` | Job event helpers |
| `markdown.ts` | Markdown rendering |
| `project-settings.ts` | Settings normalization |

### Features — `src/features/`

| File | Purpose |
|------|---------|
| `novels/BulkImportDialog.vue` | Bulk import dialog |
| `novels/ImportUrlConfirmDialog.vue` | URL import confirmation |
| `novels/ImportUrlDialog.vue` | URL import dialog |
| `novels/UpdateUrlDialog.vue` | URL update dialog |
| `projects/ProjectSettingsDialog.vue` | Project settings dialog |

## Data flow

```
Page → Composable → ApiClient → HttpTransport → HTTP → Backend
                                                    ↓
AppServices (provide/inject) ← authState ← localStorage (token)
```

## Build

```bash
cd frontend && npm install && npm run build
# vue-tsc -b && vite build → frontend/dist/
```

## Dependencies

| Package | Version | Purpose |
|---------|---------|---------|
| vue | ^3.5.18 | Framework |
| vue-router | ^4.5.1 | Routing |
| naive-ui | ^2.44.1 | UI components |
| @vicons/ionicons5 | ^0.13.0 | Icons |
| jszip | ^3.10.1 | EPUB generation |
| marked | ^18.0.6 | Markdown rendering |
| dompurify | ^3.4.12 | HTML sanitization |
| vite | ^5.4.19 | Bundler |
| vue-tsc | ^2.2.12 | Type checker |
| typescript | ^5.8.3 | Language |

## Related codemaps

- [Backend](backend.md) — API endpoints this frontend consumes
- [Database](database.md) — Data model reflected in frontend types
