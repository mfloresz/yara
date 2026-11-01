import type {
  CleanupRule,
  GlossaryEntry,
  NovelAIOptions,
  NovelTranslationOptions,
  PromptRoleTemplate,
  PromptSettings,
} from "@/domain/project-settings";

export type {
  CleanupRule,
  GlossaryEntry,
  NovelAIOptions,
  NovelTranslationOptions,
  PromptRoleTemplate,
  PromptSettings,
} from "@/domain/project-settings";

export type NovelStatus = "ongoing" | "completed" | "hiatus" | "cancelled";

export type Novel = {
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
  /** Whether the novel can be updated from its source URL (server-authoritative). */
  canUpdate: boolean;
  /** Whether the source site requires a browser worker (Cloudflare bypass). */
  requiresBrowser: boolean;
  tags: string[];
  coverPath?: string;
  isPublic: boolean;
  chapterCount: number;
  translatedCount: number;
  completedCount: number;
  originalCharCount: number;
  translatedCharCount: number;
  refinedCharCount: number;
  totalCharCount: number;
  maxChapterOrder: number;
  lastCheckedAt?: string;
  lastCheckNewChapters?: number;
  lastReadAt?: string;
  createdAt: string;
  updatedAt: string;
};

export type ChapterStatus =
  | "pending"
  | "processing"
  | "translated"
  | "refined"
  | "done"
  | "failed";

/** One inline image of a chapter, behind the [[IMG-n]] tokens in its content. */
export type ChapterImage = {
  id: string;
  chapterId: string;
  /** Exact placeholder string as it appears in the chapter content. */
  token: string;
  num: number;
  alt?: string;
  mime?: string;
  /** Authenticated URL serving the image bytes. */
  url: string;
};

export type Chapter = {
  id: string;
  novelId: string;
  chapterOrder: number;
  position?: number;
  excluded?: boolean;
  title: string;
  translatedTitle?: string;
  originalContent?: string;
  translatedContent?: string;
  refinedContent?: string;
  status: ChapterStatus;
  errorMessage?: string;
  images?: ChapterImage[];
  createdAt: string;
  updatedAt: string;
};

export type TranslationJobStatus =
  | "pending"
  | "running"
  | "done"
  | "cancelled"
  | "failed";

export type TranslationJob = {
  id: string;
  novelId: string;
  status: TranslationJobStatus;
  operation?: "translate" | "refine" | "download" | "check" | "generate-glossary";
  provider?: string;
  model?: string;
  totalChapters: number;
  completedChapters: number;
  failedChapters: number;
  errorMessage?: string;
  chapterIds?: string[];
  autoSegmentEnabled?: boolean;
  autoSegmentActive?: boolean;
  autoSegmentCount?: number;
  autoSegmentCurrentIndex?: number;
  autoSegmentCompletedCount?: number;
  autoSegmentChapterId?: string;
  autoSegmentChapterTitle?: string;
  newChapters?: number;
  createdAt: string;
  updatedAt: string;
  novelTitle?: string;
};

export type CreateNovelInput = {
  sourceTitle: string;
  sourceAuthor?: string;
  sourceDescription?: string;
  sourceLanguage: string;
  targetLanguage: string;
  sourceSeries?: string;
  sourceNumber?: string;
  targetTitle?: string;
  targetAuthor?: string;
  targetDescription?: string;
  targetSeries?: string;
  targetNumber?: string;
  glossary?: GlossaryEntry[];
  prompts?: PromptSettings;
  notes?: string;
  aiOptions?: Partial<NovelAIOptions>;
  translationOptions?: Partial<NovelTranslationOptions>;
  cleanupRules?: CleanupRule[];
  url?: string;
  customCommands?: string;
  status?: NovelStatus;
  tags?: string[];
};

export type UpdateNovelInput = Partial<CreateNovelInput>;

export type ChapterUpsertInput = {
  id?: string;
  chapterOrder?: number;
  position?: number;
  title?: string;
  translatedTitle?: string;
  originalContent?: string;
  translatedContent?: string;
  refinedContent?: string;
  status?: ChapterStatus;
  errorMessage?: string;
};

export type TranslationJobOptions = {
  operation?: "translate" | "refine" | "download" | "check" | "generate-glossary";
  provider?: string;
  model?: string;
};

export function getNovelDisplayTitle(
  novel: Pick<Novel, "targetTitle" | "sourceTitle">,
): string {
  return novel.targetTitle || novel.sourceTitle;
}

export function getNovelDisplayAuthor(
  novel: Pick<Novel, "targetAuthor" | "sourceAuthor">,
): string {
  return novel.targetAuthor || novel.sourceAuthor;
}

export function getNovelDisplayDescription(
  novel: Pick<Novel, "targetDescription" | "sourceDescription">,
): string {
  return novel.targetDescription || novel.sourceDescription;
}

export function getNovelDisplaySeries(
  novel: Pick<Novel, "targetSeries" | "sourceSeries">,
): string {
  return (novel.targetSeries || novel.sourceSeries || "").trim();
}

export function getNovelDisplayNumber(
  novel: Pick<Novel, "targetNumber" | "sourceNumber">,
): string {
  return (novel.targetNumber || novel.sourceNumber || "").trim();
}

/**
 * User-controlled reading/display/export order. Falls back to the source
 * number when the position migration has not run yet (position is 0/undefined).
 */
export function chapterPosition(ch: { position?: number; chapterOrder: number }): number {
  return ch.position && ch.position > 0 ? ch.position : ch.chapterOrder;
}

// ---------------------------------------------------------------------------
// Comics domain (media: comic — page-image chapters).
// ---------------------------------------------------------------------------

export type ComicStatus = "ongoing" | "completed" | "hiatus" | "cancelled";

export type Comic = {
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
  notes: string;
  url: string;
  status: ComicStatus;
  canUpdate: boolean;
  requiresBrowser: boolean;
  tags: string[];
  coverPath?: string;
  isPublic: boolean;
  chapterCount: number;
  downloadedCount: number;
  maxChapterOrder: number;
  lastCheckedAt?: string;
  lastCheckNewChapters?: number;
  lastReadAt?: string;
  createdAt?: string;
  updatedAt?: string;
};

export type ComicChapterStatus =
  | "pending"
  | "processing"
  | "done"
  | "failed"
  | "partial";

export type ComicChapterSummary = {
  id: string;
  comicId: string;
  chapterOrder: number;
  position: number;
  excluded: boolean;
  title: string;
  status: ComicChapterStatus;
  errorMessage: string;
  pageCount: number;
  hasPages: boolean;
  createdAt?: string;
  updatedAt?: string;
};

export type ComicPage = {
  id: string;
  chapterId: string;
  comicId: string;
  pageOrder: number;
  sourceUrl: string;
  url: string;
};

export type ComicChapter = ComicChapterSummary & {
  sourceKey: string;
  pages: ComicPage[];
};

export type ComicChapterStats = {
  totalChapters: number;
  downloadedChapters: number;
  failedChapters: number;
  maxChapterOrder: number;
};

export type ComicReadingProgress = {
  comicId: string;
  chapterId: string;
  scrollPercent: number;
  updatedAt?: string;
};

export function getComicDisplayTitle(
  comic: Pick<Comic, "targetTitle" | "sourceTitle">,
): string {
  return comic.targetTitle || comic.sourceTitle;
}

export function getComicDisplayAuthor(
  comic: Pick<Comic, "targetAuthor" | "sourceAuthor">,
): string {
  return comic.targetAuthor || comic.sourceAuthor;
}

export function getComicDisplaySeries(
  comic: Pick<Comic, "targetSeries" | "sourceSeries">,
): string {
  return (comic.targetSeries || comic.sourceSeries || "").trim();
}

export function getComicDisplayNumber(
  comic: Pick<Comic, "targetNumber" | "sourceNumber">,
): string {
  return (comic.targetNumber || comic.sourceNumber || "").trim();
}
