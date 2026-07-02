# API Endpoints — Novel Translator

> Archivo generado automáticamente. Describe todas las peticiones API gestionadas desde la UI.

Generado el: 2026-06-30

Total de endpoints documentados: **49**

---

## Auth

### POST `/api/auth/login`

**Método en cliente:** `api.auth.login()`

**Descripción:** Login

**Cuerpo de la petición:**
```typescript
input
```

**Tipo de respuesta:**
```typescript
AuthResponse
```

**Estructura `AuthResponse`:**
```typescript
type AuthResponse = {
  token: string;
  user: AuthUser;
};
```

---

### POST `/api/auth/logout`

**Método en cliente:** `api.auth.logout()`

**Descripción:** Logout

---

### POST `/api/auth/refresh`

**Método en cliente:** `api.auth.refresh()`

**Descripción:** Refresh

**Tipo de respuesta:**
```typescript
AuthResponse
```

**Estructura `AuthResponse`:**
```typescript
type AuthResponse = {
  token: string;
  user: AuthUser;
};
```

---

### POST `/api/auth/register`

**Método en cliente:** `api.auth.register()`

**Descripción:** Register

**Cuerpo de la petición:**
```typescript
input
```

**Tipo de respuesta:**
```typescript
AuthResponse
```

**Estructura `AuthResponse`:**
```typescript
type AuthResponse = {
  token: string;
  user: AuthUser;
};
```

---

## Chapters

### GET `/api/db/novels/:novelId/chapter-summaries`

**Método en cliente:** `api.chapters.listSummaries()`

**Descripción:** List Summaries

**Tipo de respuesta:**
```typescript
ChapterSummaryPage
```

**Estructura `ChapterSummaryPage`:**
```typescript
type ChapterSummaryPage = {
  items: ChapterSummary[];
  total: number;
  limit: number;
  offset: number;
};
```

---

### GET `/api/db/novels/:novelId/chapters`

**Método en cliente:** `api.chapters.list()`

**Descripción:** List

**Tipo de respuesta:**
```typescript
ChapterSummary[]
```

**Estructura `ChapterSummary`:**
```typescript
type ChapterSummary = {
  id: string;
  novelId: string;
  chapterOrder: number;
  title: string;
  translatedTitle: string;
  status: Chapter["status"];
  errorMessage: string;
  hasOriginalContent: boolean;
  hasTranslatedContent: boolean;
  hasRefinedContent: boolean;
  originalChars: number;
  translatedChars: number;
  refinedChars: number;
  createdAt: string;
  updatedAt: string;
};
```

---

### POST `/api/db/novels/:novelId/chapters`

**Método en cliente:** `api.chapters.upsert()`

**Descripción:** Upsert

**Cuerpo de la petición:**
```typescript
chapter
```

**Tipo de respuesta:**
```typescript
Chapter
```

**Estructura `Chapter`:**
```typescript
type Chapter = {
  id: string;
  novelId: string;
  chapterOrder: number;
  title: string;
  translatedTitle: string;
  originalContent: string;
  translatedContent: string;
  refinedContent: string;
  status: ChapterStatus;
  errorMessage: string;
  createdAt: string;
  updatedAt: string;
};
```

---

### GET `/api/db/novels/:novelId/chapters/:chapterId`

**Método en cliente:** `api.chapters.get()`

**Descripción:** Get

**Tipo de respuesta:**
```typescript
Chapter | null
```

**Estructura `Chapter`:**
```typescript
type Chapter = {
  id: string;
  novelId: string;
  chapterOrder: number;
  title: string;
  translatedTitle: string;
  originalContent: string;
  translatedContent: string;
  refinedContent: string;
  status: ChapterStatus;
  errorMessage: string;
  createdAt: string;
  updatedAt: string;
};
```

---

### DELETE `/api/db/novels/:novelId/chapters/:chapterId`

**Método en cliente:** `api.chapters.remove()`

**Descripción:** Remove

**Tipo de respuesta:**
```typescript
{ ok: boolean }
```

---

### PATCH `/api/db/novels/:novelId/chapters/:chapterId/status`

**Método en cliente:** `api.chapters.updateStatus()`

**Descripción:** Update Status

**Cuerpo de la petición:**
```typescript
{ status, errorMessage }
```

**Tipo de respuesta:**
```typescript
Chapter
```

**Estructura `Chapter`:**
```typescript
type Chapter = {
  id: string;
  novelId: string;
  chapterOrder: number;
  title: string;
  translatedTitle: string;
  originalContent: string;
  translatedContent: string;
  refinedContent: string;
  status: ChapterStatus;
  errorMessage: string;
  createdAt: string;
  updatedAt: string;
};
```

---

### POST `/api/db/novels/:novelId/chapters/bulk-delete`

**Método en cliente:** `api.chapters.bulkRemove()`

**Descripción:** Bulk Remove

**Cuerpo de la petición:**
```typescript
{ ids }
```

**Tipo de respuesta:**
```typescript
{ deleted: number; requested: number }
```

---

### POST `/api/db/novels/:novelId/chapters/clean`

**Método en cliente:** `api.chapters.clean()`

**Descripción:** Clean

**Cuerpo de la petición:**
```typescript
input
```

**Tipo de respuesta:**
```typescript
{
          modified: number;
          total: number;
          skipped: number;
          notFound: number;
          failed: number;
        }
```

---

### POST `/api/db/novels/:novelId/chapters/clean-preview`

**Método en cliente:** `api.chapters.cleanPreview()`

**Descripción:** Clean Preview

**Cuerpo de la petición:**
```typescript
input
```

**Tipo de respuesta:**
```typescript
CleanPreviewResponse
```

**Estructura `CleanPreviewResponse`:**
```typescript
type CleanPreviewResponse = {
  chapterTitle: string;
  original: string;
  cleaned: string;
  changed: boolean;
  removedLines: number;
};
```

---

### GET `/api/db/novels/:novelId/chapters/full`

**Método en cliente:** `api.chapters.listFull()`

**Descripción:** List Full

**Tipo de respuesta:**
```typescript
Chapter[]
```

**Estructura `Chapter`:**
```typescript
type Chapter = {
  id: string;
  novelId: string;
  chapterOrder: number;
  title: string;
  translatedTitle: string;
  originalContent: string;
  translatedContent: string;
  refinedContent: string;
  status: ChapterStatus;
  errorMessage: string;
  createdAt: string;
  updatedAt: string;
};
```

---

## Defaults

### GET `/api/defaults`

**Método en cliente:** `api.defaults.get()`

**Descripción:** Get

**Tipo de respuesta:**
```typescript
ServerDefaults
```

**Estructura `ServerDefaults`:**
```typescript
type ServerDefaults = {
  translation: ServerTranslationDefaults;
};
```

---

## Epubs

### GET `/api/epubs`

**Método en cliente:** `api.epubs.listByNovel()`

**Descripción:** List By Novel

**Tipo de respuesta:**
```typescript
NovelEpubRecord[]
```

**Estructura `NovelEpubRecord`:**
```typescript
type NovelEpubRecord = {
  id: string;
  novelId: string;
  fileKind: "original" | "translated";
  sourceVariant: "original" | "translated" | "refined";
  fileName: string;
  url: string;
  createdAt: string;
  updatedAt: string;
};
```

---

### POST `/api/epubs`

**Método en cliente:** `api.epubs.save()`

**Descripción:** Save

**Cuerpo de la petición:**
```typescript
FormData (multipart/form-data)
```

**Tipo de respuesta:**
```typescript
NovelEpubRecord
```

**Estructura `NovelEpubRecord`:**
```typescript
type NovelEpubRecord = {
  id: string;
  novelId: string;
  fileKind: "original" | "translated";
  sourceVariant: "original" | "translated" | "refined";
  fileName: string;
  url: string;
  createdAt: string;
  updatedAt: string;
};
```

---

### POST `/api/epubs/preview`

**Método en cliente:** `api.epubs.preview()`

**Descripción:** Preview

**Cuerpo de la petición:**
```typescript
FormData (multipart/form-data)
```

**Tipo de respuesta:**
```typescript
EpubPreviewResult
```

**Estructura `EpubPreviewResult`:**
```typescript
type EpubPreviewResult = {
  title: string;
  author: string;
  description: string;
  language: string;
  series: string;
  number: string;
  chapters: EpubPreviewChapter[];
};
```

---

## Jobs

### POST `/api/db/novels/:novelId/translation-jobs`

**Método en cliente:** `api.jobs.create()`

**Descripción:** Create

**Cuerpo de la petición:**
```typescript
{
            chapterIds,
            operation: options.operation,
            options: {
              provider: options.provider,
              mod...
```

**Tipo de respuesta:**
```typescript
TranslationJob
```

**Estructura `TranslationJob`:**
```typescript
type TranslationJob = {
  id: string;
  novelId: string;
  status: TranslationJobStatus;
  operation: "translate" | "refine" | "download";
  provider: string;
  model: string;
  totalChapters: number;
  completedChapters: number;
  failedChapters: number;
  errorMessage: string;
  chapterIds: string[];
  autoSegmentEnabled: boolean;
  autoSegmentActive: boolean;
  autoSegmentCount: number;
  autoSegmentCurrentIndex: number;
  autoSegmentCompletedCount: number;
  autoSegmentChapterId: string;
  autoSegmentChapterTitle: string;
  createdAt: string;
  updatedAt: string;
  novelTitle: string;
};
```

---

### GET `/api/db/novels/:novelId/translation-jobs`

**Método en cliente:** `api.jobs.list()`

**Descripción:** List

**Tipo de respuesta:**
```typescript
TranslationJob[]
```

**Estructura `TranslationJob`:**
```typescript
type TranslationJob = {
  id: string;
  novelId: string;
  status: TranslationJobStatus;
  operation: "translate" | "refine" | "download";
  provider: string;
  model: string;
  totalChapters: number;
  completedChapters: number;
  failedChapters: number;
  errorMessage: string;
  chapterIds: string[];
  autoSegmentEnabled: boolean;
  autoSegmentActive: boolean;
  autoSegmentCount: number;
  autoSegmentCurrentIndex: number;
  autoSegmentCompletedCount: number;
  autoSegmentChapterId: string;
  autoSegmentChapterTitle: string;
  createdAt: string;
  updatedAt: string;
  novelTitle: string;
};
```

---

### PATCH `/api/db/translation-jobs/:jobId`

**Método en cliente:** `api.jobs.update()`

**Descripción:** Update

**Cuerpo de la petición:**
```typescript
patch
```

**Tipo de respuesta:**
```typescript
TranslationJob
```

**Estructura `TranslationJob`:**
```typescript
type TranslationJob = {
  id: string;
  novelId: string;
  status: TranslationJobStatus;
  operation: "translate" | "refine" | "download";
  provider: string;
  model: string;
  totalChapters: number;
  completedChapters: number;
  failedChapters: number;
  errorMessage: string;
  chapterIds: string[];
  autoSegmentEnabled: boolean;
  autoSegmentActive: boolean;
  autoSegmentCount: number;
  autoSegmentCurrentIndex: number;
  autoSegmentCompletedCount: number;
  autoSegmentChapterId: string;
  autoSegmentChapterTitle: string;
  createdAt: string;
  updatedAt: string;
  novelTitle: string;
};
```

---

### GET `/api/db/translation-jobs/active`

**Método en cliente:** `api.jobs.listActive()`

**Descripción:** List Active

**Tipo de respuesta:**
```typescript
TranslationJob[]
```

**Estructura `TranslationJob`:**
```typescript
type TranslationJob = {
  id: string;
  novelId: string;
  status: TranslationJobStatus;
  operation: "translate" | "refine" | "download";
  provider: string;
  model: string;
  totalChapters: number;
  completedChapters: number;
  failedChapters: number;
  errorMessage: string;
  chapterIds: string[];
  autoSegmentEnabled: boolean;
  autoSegmentActive: boolean;
  autoSegmentCount: number;
  autoSegmentCurrentIndex: number;
  autoSegmentCompletedCount: number;
  autoSegmentChapterId: string;
  autoSegmentChapterTitle: string;
  createdAt: string;
  updatedAt: string;
  novelTitle: string;
};
```

---

### GET `/api/db/translation-jobs/active/status`

**Método en cliente:** `api.jobs.status()`

**Descripción:** Status

**Tipo de respuesta:**
```typescript
{ hasActive: boolean }
```

---

## Novels

### POST `/api/db/novels`

**Método en cliente:** `api.novels.create()`

**Descripción:** Create

**Cuerpo de la petición:**
```typescript
data
```

**Tipo de respuesta:**
```typescript
Novel
```

**Estructura `Novel`:**
```typescript
type Novel = {
  id: string;
  ownerId: string;
  sourceLanguage: string;
  targetLanguage: string;
  sourceTitle: string;
  sourceAuthor: string;
  sourceDescription: string;
  sourceSeries: string;
  sourceNumber: string;
  targetTitle: string;
  targetAuthor: string;
  targetDescription: string;
  targetSeries: string;
  targetNumber: string;
  glossary: GlossaryEntry[];
  prompts: PromptSettings;
  notes: string;
  aiOptions: NovelAIOptions;
  translationOptions: NovelTranslationOptions;
  cleanupRules: CleanupRule[];
  url: string;
  customCommands: string;
  status: NovelStatus;
  tags: string[];
  coverPath: string;
  isPublic: boolean;
  chapterCount: number;
  translatedCount: number;
  completedCount: number;
  originalCharCount: number;
  translatedCharCount: number;
  refinedCharCount: number;
  totalCharCount: number;
  maxChapterOrder: number;
  createdAt: string;
  updatedAt: string;
};
```

---

### GET `/api/db/novels/:novelId`

**Método en cliente:** `api.novels.get()`

**Descripción:** Get

**Tipo de respuesta:**
```typescript
Novel | null
```

**Estructura `Novel`:**
```typescript
type Novel = {
  id: string;
  ownerId: string;
  sourceLanguage: string;
  targetLanguage: string;
  sourceTitle: string;
  sourceAuthor: string;
  sourceDescription: string;
  sourceSeries: string;
  sourceNumber: string;
  targetTitle: string;
  targetAuthor: string;
  targetDescription: string;
  targetSeries: string;
  targetNumber: string;
  glossary: GlossaryEntry[];
  prompts: PromptSettings;
  notes: string;
  aiOptions: NovelAIOptions;
  translationOptions: NovelTranslationOptions;
  cleanupRules: CleanupRule[];
  url: string;
  customCommands: string;
  status: NovelStatus;
  tags: string[];
  coverPath: string;
  isPublic: boolean;
  chapterCount: number;
  translatedCount: number;
  completedCount: number;
  originalCharCount: number;
  translatedCharCount: number;
  refinedCharCount: number;
  totalCharCount: number;
  maxChapterOrder: number;
  createdAt: string;
  updatedAt: string;
};
```

---

### DELETE `/api/db/novels/:novelId`

**Método en cliente:** `api.novels.remove()`

**Descripción:** Remove

**Tipo de respuesta:**
```typescript
{ ok: boolean }
```

---

### PATCH `/api/db/novels/:novelId`

**Método en cliente:** `api.novels.update()`

**Descripción:** Update

**Cuerpo de la petición:**
```typescript
patch
```

**Tipo de respuesta:**
```typescript
Novel
```

**Estructura `Novel`:**
```typescript
type Novel = {
  id: string;
  ownerId: string;
  sourceLanguage: string;
  targetLanguage: string;
  sourceTitle: string;
  sourceAuthor: string;
  sourceDescription: string;
  sourceSeries: string;
  sourceNumber: string;
  targetTitle: string;
  targetAuthor: string;
  targetDescription: string;
  targetSeries: string;
  targetNumber: string;
  glossary: GlossaryEntry[];
  prompts: PromptSettings;
  notes: string;
  aiOptions: NovelAIOptions;
  translationOptions: NovelTranslationOptions;
  cleanupRules: CleanupRule[];
  url: string;
  customCommands: string;
  status: NovelStatus;
  tags: string[];
  coverPath: string;
  isPublic: boolean;
  chapterCount: number;
  translatedCount: number;
  completedCount: number;
  originalCharCount: number;
  translatedCharCount: number;
  refinedCharCount: number;
  totalCharCount: number;
  maxChapterOrder: number;
  createdAt: string;
  updatedAt: string;
};
```

---

### POST `/api/db/novels/:novelId/copy`

**Método en cliente:** `api.novels.copy()`

**Descripción:** Copy

**Tipo de respuesta:**
```typescript
Novel
```

**Estructura `Novel`:**
```typescript
type Novel = {
  id: string;
  ownerId: string;
  sourceLanguage: string;
  targetLanguage: string;
  sourceTitle: string;
  sourceAuthor: string;
  sourceDescription: string;
  sourceSeries: string;
  sourceNumber: string;
  targetTitle: string;
  targetAuthor: string;
  targetDescription: string;
  targetSeries: string;
  targetNumber: string;
  glossary: GlossaryEntry[];
  prompts: PromptSettings;
  notes: string;
  aiOptions: NovelAIOptions;
  translationOptions: NovelTranslationOptions;
  cleanupRules: CleanupRule[];
  url: string;
  customCommands: string;
  status: NovelStatus;
  tags: string[];
  coverPath: string;
  isPublic: boolean;
  chapterCount: number;
  translatedCount: number;
  completedCount: number;
  originalCharCount: number;
  translatedCharCount: number;
  refinedCharCount: number;
  totalCharCount: number;
  maxChapterOrder: number;
  createdAt: string;
  updatedAt: string;
};
```

---

### POST `/api/db/novels/:novelId/cover`

**Método en cliente:** `api.novels.uploadCover()`

**Descripción:** Upload Cover

**Cuerpo de la petición:**
```typescript
FormData (multipart/form-data)
```

**Tipo de respuesta:**
```typescript
Novel
```

**Estructura `Novel`:**
```typescript
type Novel = {
  id: string;
  ownerId: string;
  sourceLanguage: string;
  targetLanguage: string;
  sourceTitle: string;
  sourceAuthor: string;
  sourceDescription: string;
  sourceSeries: string;
  sourceNumber: string;
  targetTitle: string;
  targetAuthor: string;
  targetDescription: string;
  targetSeries: string;
  targetNumber: string;
  glossary: GlossaryEntry[];
  prompts: PromptSettings;
  notes: string;
  aiOptions: NovelAIOptions;
  translationOptions: NovelTranslationOptions;
  cleanupRules: CleanupRule[];
  url: string;
  customCommands: string;
  status: NovelStatus;
  tags: string[];
  coverPath: string;
  isPublic: boolean;
  chapterCount: number;
  translatedCount: number;
  completedCount: number;
  originalCharCount: number;
  translatedCharCount: number;
  refinedCharCount: number;
  totalCharCount: number;
  maxChapterOrder: number;
  createdAt: string;
  updatedAt: string;
};
```

---

### POST `/api/db/novels/:novelId/update-from-url`

**Método en cliente:** `api.novels.updateFromUrl()`

**Descripción:** Update From Url

**Cuerpo de la petición:**
```typescript
input
```

**Tipo de respuesta:**
```typescript
UpdateUrlResult
```

**Estructura `UpdateUrlResult`:**
```typescript
type UpdateUrlResult = {
  chaptersAdded: number;
  chapters: Chapter[];
  totalChapters: number;
  pendingChapters: number;
  downloadJobId: string;
  message: string;
};
```

---

### GET `/api/db/novels/:novelId/update-preview`

**Método en cliente:** `api.novels.updatePreviewFromUrl()`

**Descripción:** Update Preview From Url

**Tipo de respuesta:**
```typescript
UpdateUrlPreviewResult
```

**Estructura `UpdateUrlPreviewResult`:**
```typescript
type UpdateUrlPreviewResult = {
  title: string;
  author: string;
  description: string;
  coverURL: string;
  sourceURL: string;
  currentChapters: number;
  totalChapters: number;
  newChapters: number;
  firstNewChapter: number;
  lastNewChapter: number;
};
```

---

### PATCH `/api/db/novels/:novelId/visibility`

**Método en cliente:** `api.novels.updateVisibility()`

**Descripción:** Update Visibility

**Cuerpo de la petición:**
```typescript
{ isPublic }
```

**Tipo de respuesta:**
```typescript
Novel
```

**Estructura `Novel`:**
```typescript
type Novel = {
  id: string;
  ownerId: string;
  sourceLanguage: string;
  targetLanguage: string;
  sourceTitle: string;
  sourceAuthor: string;
  sourceDescription: string;
  sourceSeries: string;
  sourceNumber: string;
  targetTitle: string;
  targetAuthor: string;
  targetDescription: string;
  targetSeries: string;
  targetNumber: string;
  glossary: GlossaryEntry[];
  prompts: PromptSettings;
  notes: string;
  aiOptions: NovelAIOptions;
  translationOptions: NovelTranslationOptions;
  cleanupRules: CleanupRule[];
  url: string;
  customCommands: string;
  status: NovelStatus;
  tags: string[];
  coverPath: string;
  isPublic: boolean;
  chapterCount: number;
  translatedCount: number;
  completedCount: number;
  originalCharCount: number;
  translatedCharCount: number;
  refinedCharCount: number;
  totalCharCount: number;
  maxChapterOrder: number;
  createdAt: string;
  updatedAt: string;
};
```

---

### POST `/api/db/novels/batch-translate`

**Método en cliente:** `api.novels.batchTranslate()`

**Descripción:** Batch Translate

**Cuerpo de la petición:**
```typescript
{ selections }
```

**Tipo de respuesta:**
```typescript
BatchTranslateStartResponse
```

**Estructura `BatchTranslateStartResponse`:**
```typescript
type BatchTranslateStartResponse = {
  jobs: BatchTranslateJobResult[];
  totalPending: number;
};
```

---

### GET `/api/db/novels/batch-translate-preview`

**Método en cliente:** `api.novels.batchTranslatePreview()`

**Descripción:** Batch Translate Preview

**Tipo de respuesta:**
```typescript
BatchTranslateResponse
```

**Estructura `BatchTranslateResponse`:**
```typescript
type BatchTranslateResponse = {
  results: BatchTranslateNovelResult[];
  totalNovels: number;
  withPending: number;
};
```

---

### POST `/api/db/novels/batch-update-from-url`

**Método en cliente:** `api.novels.batchUpdateFromUrl()`

**Descripción:** Batch Update From Url

**Cuerpo de la petición:**
```typescript
{ selections }
```

**Tipo de respuesta:**
```typescript
BatchUpdateResponse
```

**Estructura `BatchUpdateResponse`:**
```typescript
type BatchUpdateResponse = {
  jobs: BatchUpdateJobResult[];
  totalPending: number;
};
```

---

### GET `/api/db/novels/check-batch-updates`

**Método en cliente:** `api.novels.checkBatchUpdates()`

**Descripción:** Check Batch Updates

**Tipo de respuesta:**
```typescript
BatchCheckResponse
```

**Estructura `BatchCheckResponse`:**
```typescript
type BatchCheckResponse = {
  results: BatchCheckNovelResult[];
  checked: number;
  withUpdates: number;
  errors: number;
};
```

---

### POST `/api/db/novels/import-epub`

**Método en cliente:** `api.novels.importFromEpub()`

**Descripción:** Import From Epub

**Cuerpo de la petición:**
```typescript
FormData (multipart/form-data)
```

**Tipo de respuesta:**
```typescript
ImportEpubResult
```

**Estructura `ImportEpubResult`:**
```typescript
type ImportEpubResult = {
  novel: Novel;
  epub: {
    id: string;
  novelId: string;
  fileKind: string;
  fileName: string;
  createdAt: string;
  updatedAt: string;
};
```

---

### POST `/api/db/novels/import-from-url`

**Método en cliente:** `api.novels.importFromUrl()`

**Descripción:** Import From Url

**Cuerpo de la petición:**
```typescript
input
```

**Tipo de respuesta:**
```typescript
ImportUrlResult
```

**Estructura `ImportUrlResult`:**
```typescript
type ImportUrlResult = {
  novel: Novel;
  chaptersImported: number;
  totalChapters: number;
  downloadJob: {
    id: string;
  totalChapters: number;
};
```

---

### POST `/api/db/novels/preview-from-url`

**Método en cliente:** `api.novels.previewFromUrl()`

**Descripción:** Preview From Url

**Cuerpo de la petición:**
```typescript
{
          url,
        }
```

**Tipo de respuesta:**
```typescript
PreviewUrlResult
```

**Estructura `PreviewUrlResult`:**
```typescript
type PreviewUrlResult = {
  title: string;
  author: string;
  description: string;
  coverURL: string;
  totalChapters: number;
  sourceURL: string;
};
```

---

### GET `/api/db/novels/series/suggestions`

**Método en cliente:** `api.novels.listSeriesSuggestions()`

**Descripción:** List Series Suggestions

**Tipo de respuesta:**
```typescript
{ items?: string[] }
```

---

### GET `/api/db/novels/tags/suggestions`

**Método en cliente:** `api.novels.listTagSuggestions()`

**Descripción:** List Tag Suggestions

**Tipo de respuesta:**
```typescript
{ items?: string[] }
```

---

## Prompts

### PUT `/api/user/prompts/:key`

**Método en cliente:** `api.prompts.upsert()`

**Descripción:** Upsert

**Cuerpo de la petición:**
```typescript
input
```

**Tipo de respuesta:**
```typescript
GeneralPromptRecord
```

**Estructura `GeneralPromptRecord`:**
```typescript
type GeneralPromptRecord = {
  id: string;
  key: GeneralPromptKey;
  label: string;
  description: string;
  prompt: {
    systemPrompt?: string;
  userPrompt: string;
};
```

---

## Providers

### PUT `/api/user/providers/:providerKey`

**Método en cliente:** `api.providers.update()`

**Descripción:** Update

**Cuerpo de la petición:**
```typescript
payload
```

---

### DELETE `/api/user/providers/:providerKey/key`

**Método en cliente:** `api.providers.deleteKey()`

**Descripción:** Delete Key

---

### PUT `/api/user/providers/:providerKey/key`

**Método en cliente:** `api.providers.replaceKey()`

**Descripción:** Replace Key

**Cuerpo de la petición:**
```typescript
{ apiKey }
```

---

## ReadingProgress

### GET `/api/user/novels/:novelId/reading-progress`

**Método en cliente:** `api.readingProgress.get()`

**Descripción:** Get

**Tipo de respuesta:**
```typescript
ReadingProgress
```

**Estructura `ReadingProgress`:**
```typescript
type ReadingProgress = {
  id: string;
  userId: string;
  novelId: string;
  chapterId: string;
  scrollPercent: number;
  createdAt: string;
  updatedAt: string;
};
```

---

### PUT `/api/user/novels/:novelId/reading-progress`

**Método en cliente:** `api.readingProgress.update()`

**Descripción:** Update

**Cuerpo de la petición:**
```typescript
data
```

**Tipo de respuesta:**
```typescript
ReadingProgress
```

**Estructura `ReadingProgress`:**
```typescript
type ReadingProgress = {
  id: string;
  userId: string;
  novelId: string;
  chapterId: string;
  scrollPercent: number;
  createdAt: string;
  updatedAt: string;
};
```

---

## Settings

### GET `/api/user/settings`

**Método en cliente:** `api.settings.get()`

**Descripción:** Get

**Tipo de respuesta:**
```typescript
ServerSettings
```

**Estructura `ServerSettings`:**
```typescript
type ServerSettings = {
  theme: "light" | "dark" | "system";
  ai: {
    provider: string;
  baseUrl: string;
  model: string;
  timeoutMs: number;
};
```

---

### PUT `/api/user/settings`

**Método en cliente:** `api.settings.update()`

**Descripción:** Update

**Cuerpo de la petición:**
```typescript
payload
```

**Tipo de respuesta:**
```typescript
ServerSettings
```

**Estructura `ServerSettings`:**
```typescript
type ServerSettings = {
  theme: "light" | "dark" | "system";
  ai: {
    provider: string;
  baseUrl: string;
  model: string;
  timeoutMs: number;
};
```

---

## Tipos Compartidos (Enums)

### ChapterStatus
```typescript
type ChapterStatus = "pending" | "processing" | "translated" | "refined" | "done" | "failed";
```

### TranslationJobStatus
```typescript
type TranslationJobStatus = "pending" | "running" | "done" | "cancelled" | "failed";
```

### NovelStatus
```typescript
type NovelStatus = "ongoing" | "completed" | "hiatus" | "cancelled";
```
