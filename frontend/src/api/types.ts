import type { Novel, Chapter, TranslationJob } from "@/domain";

export type V1CollectionMeta = {
  total?: number;
  page?: number;
  per_page?: number;
  limit?: number;
  offset?: number;
  has_more?: boolean;
  next_page?: number;
};

export type V1Links = {
  self?: string;
  next?: string;
  prev?: string;
  first?: string;
  last?: string;
};

export type V1ErrorBody = {
  code: string;
  message: string;
  details?: Array<{ field?: string; message: string; code?: string }>;
};

export type V1Envelope = {
  data?: unknown;
  meta?: V1CollectionMeta;
  links?: V1Links;
  error?: V1ErrorBody;
};

// Paginated result that maps the v1 list envelope to the legacy
// {items,hasMore} contract consumers still expect. Construct it from
// `{data,meta}` returned by `http.get` after unwrapping — see client.ts.
export type PaginatedResult<T> = {
  items: T[];
  hasMore?: boolean;
  total?: number;
  page?: number;
  perPage?: number;
};

export type AuthUser = {
  id: string;
  email: string;
  name?: string;
  theme: "light" | "dark" | "system";
  createdAt?: string;
  updatedAt?: string;
};

export type AuthResponse = {
  token: string;
  user: AuthUser;
};

export type ImportEpubResult = {
  novel: Novel;
  epub: {
    id: string;
    novelId: string;
    fileKind: string;
    fileName: string;
    createdAt: string;
    updatedAt: string;
  };
  chaptersImported: number;
};

export type ImportZipResult = {
  novel: Novel;
  chaptersImported: number;
};

export type GeneralPromptKey = "translation" | "title" | "refine" | "check" | "glossary";

export type GeneralPromptRecord = {
  id: string;
  key: GeneralPromptKey;
  label?: string;
  description?: string;
  prompt: {
    systemPrompt?: string;
    userPrompt?: string;
  };
  active: boolean;
  updatedAt?: string;
};

export type NovelEpubRecord = {
  id: string;
  novelId: string;
  fileKind: "original" | "translated";
  sourceVariant?: "original" | "translated" | "refined";
  fileName?: string;
  url?: string;
  createdAt: string;
  updatedAt: string;
};

export type EpubPreviewChapter = {
  title: string;
  content: string;
};

export type EpubPreviewResult = {
  title: string;
  author: string;
  description: string;
  language: string;
  series: string;
  number: string;
  chapters: EpubPreviewChapter[];
};

export type ServerSettings = {
  theme: "light" | "dark" | "system";
  ai: {
    provider: string;
    baseUrl: string;
    model: string;
    timeoutMs: number;
    concurrency: number;
  };
  titleProvider: string;
  titleModel: string;
  translation: {
    autoSegment: boolean;
    thresholdChars: number;
    maxChars: number;
    minChars: number;
    maxRetries: number;
    enableCheck: boolean;
    includePreviousChapterTitles: boolean;
    concurrency: number;
  };
};

export type ProviderInfo = {
  id: string;
  name: string;
  baseUrl: string;
  models: string[];
  defaultModel: string;
  openaiCompat: boolean;
  apiKeyConfigured?: boolean;
  apiKeyUpdatedAt?: string;
  enabled?: boolean;
  concurrency?: number;
  timeoutMs?: number;
};

export type ProvidersResponse = {
  providers: ProviderInfo[];
};

export type ApiErrorPayload = {
  // v1 envelope error shape: {error: {code, message, details?}}
  error?: {
    code?: string;
    message?: string;
  };
  // Legacy PocketBase shape: {message, code, data}
  message?: string;
  code?: number;
  data?: Record<string, unknown>;
};

export type ImportUrlResult = {
  novel: Novel;
  chaptersImported: number;
  totalChapters: number;
  downloadJob?: {
    id: string;
    totalChapters: number;
  };
};

export type PreviewUrlResult = {
  title: string;
  author?: string;
  description?: string;
  coverURL?: string;
  totalChapters: number;
  sourceURL: string;
};

export type UpdateUrlResult = {
  chaptersAdded: number;
  chapters: Chapter[];
  totalChapters: number;
  pendingChapters?: number;
  downloadJobId?: string;
  message?: string;
};

export type RedownloadMismatch = {
  order: number;
  sourceTitle: string;
  storedTitle: string;
};

export type RedownloadFromUrlResult = {
  pendingChapters: number;
  downloadJobId?: string;
  message?: string;
  /** Present when the source titles no longer match the stored ones and the user must confirm before the job is created. */
  needsConfirmation?: boolean;
  titleMismatches?: number;
  chapters?: RedownloadMismatch[];
};

export type UpdateUrlPreviewResult = {
  title: string;
  author?: string;
  description?: string;
  coverURL?: string;
  sourceURL: string;
  currentChapters: number;
  totalChapters: number;
  newChapters: number;
  firstNewChapter: number;
  lastNewChapter: number;
};

export type ChapterSummary = {
  id: string;
  novelId: string;
  chapterOrder: number;
  title: string;
  translatedTitle?: string;
  status: Chapter["status"];
  errorMessage?: string;
  hasOriginalContent: boolean;
  hasTranslatedContent: boolean;
  hasRefinedContent: boolean;
  originalChars: number;
  translatedChars: number;
  refinedChars: number;
  createdAt: string;
  updatedAt: string;
};

export type ChapterSummaryPage = {
  items: ChapterSummary[];
  total: number;
  limit: number;
  offset: number;
};

export type ChapterStats = {
  totalChapters: number;
  completedChapters: number;
  translatedChapters: number;
  originalCharacters: number;
  translatedCharacters: number;
  refinedCharacters: number;
  totalCharacters: number;
  maxChapterOrder: number;
};

export type CleanPreviewResponse = {
  chapterTitle: string;
  changes: CleanDiffHunk[];
  original: string;
  cleaned: string;
  changed: boolean;
  removedLines: number;
};

export type CleanDiffHunk = {
  before: string[];
  after: string[];
};

export type CleanPreviewItem = {
  chapterId: string;
  chapterOrder: number;
  chapterTitle: string;
  changes: CleanDiffHunk[];
  original: string;
  cleaned: string;
  changed: boolean;
  removedLines: number;
};

export type CleanPreviewBulkResponse = {
  items: CleanPreviewItem[];
  total: number;
  changed: number;
};

export type TranslationJobPatch = Partial<TranslationJob>;
export type ChapterList = Chapter[];

export type BatchCheckNovelResult = {
  novelId: string;
  sourceTitle: string;
  sourceAuthor?: string;
  coverUrl?: string;
  newChapters: number;
  firstNewChapter: number;
  lastNewChapter: number;
  startOrder: number;
  currentChapters: number;
  totalChapters: number;
  newChapterInfo: { url: string; title: string }[];
  error?: string;
};

export type BatchCheckResponse = {
  results: BatchCheckNovelResult[];
  checked: number;
  withUpdates: number;
  errors: number;
};

export type BatchUpdateSelection = {
  novelId: string;
  startOrder: number;
  startChapter?: number;
  endChapter?: number;
  newChapterInfo: { url: string; title: string }[];
};

export type BatchUpdateJobResult = {
  novelId: string;
  jobId: string;
  pendingChapters: number;
};

export type BatchUpdateResponse = {
  jobs: BatchUpdateJobResult[];
  totalPending: number;
};

export type BatchTranslateNovelResult = {
  novelId: string;
  sourceTitle: string;
  sourceAuthor?: string;
  coverUrl?: string;
  pendingChapters: number;
  totalChapters: number;
  translatedCount: number;
  completedCount: number;
  hasOriginalContent: boolean;
};

export type BatchTranslateResponse = {
  results: BatchTranslateNovelResult[];
  totalNovels: number;
  withPending: number;
};

export type BatchTranslateSelection = {
  novelId: string;
  chapterIds?: string[];
};

export type BatchTranslateJobResult = {
  novelId: string;
  jobId: string;
  pendingChapters: number;
};

export type ReadingProgress = {
  id: string;
  userId: string;
  novelId: string;
  chapterId: string;
  scrollPercent: number;
  createdAt: string;
  updatedAt: string;
};

export type BatchTranslateStartResponse = {
  jobs: BatchTranslateJobResult[];
  totalPending: number;
};

export type WorkerToken = {
  id: string;
  userId: string;
  extensionId: string;
  label: string;
  lastUsedAt?: string;
  createdAt?: string;
  revoked: boolean;
};
