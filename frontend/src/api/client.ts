import type { Ref } from "vue";
import { createHttpClient, unwrapCollection } from "@/api/http";
import type {
  AuthResponse,
  BatchCheckResponse,
  BatchUpdateResponse,
  BatchUpdateSelection,
  BatchTranslateResponse,
  BatchTranslateSelection,
  BatchTranslateStartResponse,
  ChapterSummary,
  ChapterSummaryPage,
  CleanPreviewBulkResponse,
  CleanPreviewResponse,
  EpubPreviewResult,
  GeneralPromptKey,
  GeneralPromptRecord,
  ImportEpubResult,
  ImportUrlResult,
  ImportZipResult,
  NovelEpubRecord,
  PaginatedResult,
  PreviewUrlResult,
  ProvidersResponse,
  ReadingProgress,
  RedownloadFromUrlResult,
  ServerSettings,
  UpdateUrlPreviewResult,
  UpdateUrlResult,
  WorkerToken,
} from "@/api/types";
import type {
  Chapter,
  ChapterUpsertInput,
  CreateNovelInput,
  Novel,
  TranslationJob,
  TranslationJobOptions,
  UpdateNovelInput,
} from "@/domain";
import {
  normalizePromptSettings,
  type ServerDefaults,
  type ServerTranslationDefaults,
  type GlossaryGenerationOptions,
} from "@/domain/project-settings";
import { getApiBaseUrl } from "@/utils/api-base-url";
import { ensureGlossaryIds } from "@/utils/project-settings";

function normalizeNovel(
  novel: Novel,
  translationDefaults?: ServerTranslationDefaults,
): Novel {
  return {
    ...novel,
    sourceTitle: typeof novel.sourceTitle === "string" ? novel.sourceTitle : "",
    sourceAuthor:
      typeof novel.sourceAuthor === "string" ? novel.sourceAuthor : "",
    sourceDescription:
      typeof novel.sourceDescription === "string"
        ? novel.sourceDescription
        : "",
    sourceSeries:
      typeof novel.sourceSeries === "string" ? novel.sourceSeries : "",
    sourceNumber:
      typeof novel.sourceNumber === "string" ? novel.sourceNumber : "",
    targetTitle: typeof novel.targetTitle === "string" ? novel.targetTitle : "",
    targetAuthor:
      typeof novel.targetAuthor === "string" ? novel.targetAuthor : "",
    targetDescription:
      typeof novel.targetDescription === "string"
        ? novel.targetDescription
        : "",
    targetSeries:
      typeof novel.targetSeries === "string" ? novel.targetSeries : "",
    targetNumber:
      typeof novel.targetNumber === "string" ? novel.targetNumber : "",
    glossary: ensureGlossaryIds(novel.glossary),
    prompts: normalizePromptSettings(novel.prompts),
    notes: typeof novel.notes === "string" ? novel.notes : "",
    aiOptions: {
      provider: novel.aiOptions?.provider ?? "",
      model: novel.aiOptions?.model ?? "",
      timeoutMs: novel.aiOptions?.timeoutMs ?? undefined,
      titleEnabled: novel.aiOptions?.titleEnabled ?? null,
      titleProvider: novel.aiOptions?.titleProvider ?? "",
      titleModel: novel.aiOptions?.titleModel ?? "",
    },
    translationOptions: {
      ...(translationDefaults ?? {}),
      ...(novel.translationOptions ?? {}),
    },
    cleanupRules: Array.isArray(novel.cleanupRules) ? novel.cleanupRules : [],
    url: typeof novel.url === "string" ? novel.url : "",
    customCommands:
      typeof novel.customCommands === "string" ? novel.customCommands : "",
    canUpdate: Boolean(novel.canUpdate),
    requiresBrowser: Boolean(novel.requiresBrowser),
    status:
      novel.status === "completed" ||
      novel.status === "hiatus" ||
      novel.status === "cancelled"
        ? novel.status
        : "ongoing",
    tags: Array.isArray(novel.tags)
      ? novel.tags.filter((tag): tag is string => typeof tag === "string")
      : [],
    ownerId: typeof novel.ownerId === "string" ? novel.ownerId : "",
    isPublic: Boolean(novel.isPublic),
    chapterCount: Number.isFinite(novel.chapterCount) ? novel.chapterCount : 0,
    translatedCount: Number.isFinite(novel.translatedCount)
      ? novel.translatedCount
      : 0,
    completedCount: Number.isFinite(novel.completedCount)
      ? novel.completedCount
      : 0,
    originalCharCount: Number.isFinite(novel.originalCharCount)
      ? novel.originalCharCount
      : 0,
    translatedCharCount: Number.isFinite(novel.translatedCharCount)
      ? novel.translatedCharCount
      : 0,
    refinedCharCount: Number.isFinite(novel.refinedCharCount)
      ? novel.refinedCharCount
      : 0,
    totalCharCount: Number.isFinite(novel.totalCharCount)
      ? novel.totalCharCount
      : 0,
    maxChapterOrder: Number.isFinite(novel.maxChapterOrder)
      ? novel.maxChapterOrder
      : 0,
  };
}

// Sparse fieldset for the dashboard / library list view. Excludes heavy
// fields (glossary, tags, aiOptions, translationOptions, cleanupRules,
// descriptions, notes) so the list payload stays small. The detail page
// fetches a full novel via `GET /api/v1/novels/{id}` (no fields filter).
const NOVEL_LIST_FIELDS =
  "id,sourceTitle,sourceAuthor,targetTitle,targetAuthor,status,chapterCount,translatedCount,completedCount,coverPath,createdAt,updatedAt,canUpdate,requiresBrowser,lastCheckedAt,lastCheckNewChapters,ownerId,isPublic,sourceLanguage,targetLanguage,url,glossaryCount";

function buildQuery(
  params: Record<string, string | number | undefined | null>,
): string {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === null || value === "") continue;
    search.set(key, String(value));
  }
  return search.size > 0 ? `?${search.toString()}` : "";
}

export function createApiClient(defaultsRef: Ref<ServerDefaults | null>) {
  const http = createHttpClient({ baseUrl: getApiBaseUrl() });
  const withDefaults = (novel: Novel) =>
    normalizeNovel(novel, defaultsRef.value?.translation);

  return {
    auth: {
      register(input: { email: string; password: string; name?: string }) {
        return http.post<AuthResponse>("/api/v1/auth/register", input);
      },
      login(input: { email: string; password: string }) {
        return http.post<AuthResponse>("/api/v1/auth/login", input);
      },
      refresh() {
        return http.post<AuthResponse>("/api/v1/auth/refresh");
      },
      logout() {
        return http.post<void>("/api/v1/auth/logout");
      },
    },
    defaults: {
      async get(): Promise<ServerDefaults> {
        return http.get<ServerDefaults>("/api/v1/defaults");
      },
    },
    settings: {
      async get(): Promise<ServerSettings> {
        return http.get<ServerSettings>("/api/v1/settings");
      },
      async update(payload: ServerSettings): Promise<ServerSettings> {
        return http.put<ServerSettings>("/api/v1/settings", payload);
      },
    },
    providers: {
      async list(): Promise<ProvidersResponse> {
        const result = await http.get<{
          providers: Array<{
            provider: string;
            label: string;
            baseUrl: string;
            model: string;
            models?: string[];
            kind: string;
            apiKeyConfigured?: boolean;
            apiKeyUpdatedAt?: string;
            enabled?: boolean;
            concurrency?: number;
            timeoutMs?: number;
          }>;
        }>("/api/v1/providers");
        return {
          providers: result.providers.map((provider) => ({
            id: provider.provider,
            name: provider.label,
            baseUrl: provider.baseUrl,
            models: provider.models ?? [],
            defaultModel: provider.model,
            openaiCompat: provider.kind === "openai-compatible",
            apiKeyConfigured: provider.apiKeyConfigured,
            apiKeyUpdatedAt: provider.apiKeyUpdatedAt,
            enabled: provider.enabled,
            concurrency: provider.concurrency ?? 1,
            timeoutMs: provider.timeoutMs,
          })),
        };
      },
      async update(
        providerKey: string,
        payload: { model: string; baseUrl: string; timeoutMs?: number; concurrency?: number },
      ) {
        return http.put(`/api/v1/providers/${providerKey}`, payload);
      },
      async replaceKey(providerKey: string, apiKey: string) {
        return http.put(`/api/v1/providers/${providerKey}/key`, { apiKey });
      },
      async deleteKey(providerKey: string) {
        return http.delete(`/api/v1/providers/${providerKey}/key`);
      },
    },
    novels: {
      async previewFromUrl(url: string): Promise<PreviewUrlResult> {
        return http.post<PreviewUrlResult>("/api/v1/novels/preview-from-url", {
          url,
        });
      },
      async importFromUrl(input: {
        url: string;
        sourceLanguage?: string;
        targetLanguage?: string;
        startChapter?: number;
        endChapter?: number;
      }): Promise<ImportUrlResult> {
        const result = await http.post<ImportUrlResult>(
          "/api/v1/novels/import-from-url",
          input,
        );
        return { ...result, novel: withDefaults(result.novel) };
      },
      async updateFromUrl(
        novelId: string,
        input: { startChapter?: number; endChapter?: number },
      ): Promise<UpdateUrlResult> {
        return http.post<UpdateUrlResult>(
          `/api/v1/novels/${novelId}/update-from-url`,
          input,
        );
      },
      async redownloadFromUrl(
        novelId: string,
        input: {
          startChapter?: number;
          endChapter?: number;
          confirm?: boolean;
        },
      ): Promise<RedownloadFromUrlResult> {
        return http.post<RedownloadFromUrlResult>(
          `/api/v1/novels/${novelId}/redownload-from-url`,
          input,
        );
      },
      async updatePreviewFromUrl(
        novelId: string,
      ): Promise<UpdateUrlPreviewResult> {
        // v1: was GET, now POST (writes lastCheckedAt; not idempotent).
        return http.post<UpdateUrlPreviewResult>(
          `/api/v1/novels/${novelId}/check-preview`,
        );
      },
      async importFromEpub(input: {
        file: Blob;
        fileName: string;
        sourceLanguage?: string;
        targetLanguage: string;
      }) {
        const form = new FormData();
        form.set(
          "file",
          new File([input.file], input.fileName, {
            type: "application/epub+zip",
          }),
        );
        if (input.sourceLanguage)
          form.set("sourceLanguage", input.sourceLanguage);
        form.set("targetLanguage", input.targetLanguage);
        const result = await http.post<ImportEpubResult>(
          "/api/v1/novels/import-epub",
          form,
        );
        return { ...result, novel: withDefaults(result.novel) };
      },
      async importFromZip(file: Blob, fileName: string): Promise<ImportZipResult> {
        const form = new FormData();
        form.set("file", new File([file], fileName, { type: "application/zip" }));
        const result = await http.post<ImportZipResult>(
          "/api/v1/novels/import-zip",
          form,
        );
        return { ...result, novel: withDefaults(result.novel) };
      },
      async list(
        params: {
          limit?: number;
          offset?: number;
          page?: number;
          perPage?: number;
          select?: string[];
          fields?: string;
          q?: string;
          sort?: "title" | "created" | "lastRead";
          order?: "asc" | "desc";
        } = {},
      ): Promise<PaginatedResult<Novel>> {
        // List views pass a sparse fieldset to keep payloads small; callers
        // that need a full novel fetch by id. `select` is a frontend-only
        // signal consumed by useNovels.ts to control field-level merging;
        // it does not affect the wire query.
        const fields = params.fields ?? (params.select ? undefined : NOVEL_LIST_FIELDS);
        const query = buildQuery({
          limit: params.limit,
          offset: params.offset,
          page: params.page,
          per_page: params.perPage,
          fields,
          q: params.q,
          sort: params.sort,
          order: params.order,
        });
        const result = await http.get<unknown>(`/api/v1/novels${query}`);
        const { data, meta } = unwrapCollection<Novel[]>(result);
        const items = (Array.isArray(data) ? data : []).map(withDefaults);
        return {
          items,
          hasMore: meta?.has_more,
          total: meta?.total,
          page: meta?.page,
          perPage: meta?.per_page,
        };
      },
      async get(novelId: string): Promise<Novel | null> {
        const novel = await http.get<Novel | null>(`/api/v1/novels/${novelId}`);
        return novel ? withDefaults(novel) : null;
      },
      async getFull(novelId: string): Promise<{ novel: Novel; chapters: Chapter[] } | null> {
        // /full returns the {novel,chapters} composite in a single envelope;
        // http auto-unwraps to the inner object.
        const result = await http.get<{ novel: Novel; chapters: Chapter[] } | null>(
          `/api/v1/novels/${novelId}/full`,
        );
        if (!result) return null;
        return { novel: withDefaults(result.novel), chapters: result.chapters };
      },
      async listTagSuggestions(query = "", limit = 100): Promise<string[]> {
        const suffix = buildQuery({ q: query.trim(), limit: limit > 0 ? limit : undefined });
        const result = await http.get<unknown>(
          `/api/v1/novels/tags/suggestions${suffix}`,
        );
        const { data } = unwrapCollection<string[]>(result);
        return Array.isArray(data)
          ? data.filter((item): item is string => typeof item === "string")
          : [];
      },
      async listSeriesSuggestions(query = "", limit = 100): Promise<string[]> {
        const suffix = buildQuery({ q: query.trim(), limit: limit > 0 ? limit : undefined });
        const result = await http.get<unknown>(
          `/api/v1/novels/series/suggestions${suffix}`,
        );
        const { data } = unwrapCollection<string[]>(result);
        return Array.isArray(data)
          ? data.filter((item): item is string => typeof item === "string")
          : [];
      },
      async create(data: CreateNovelInput): Promise<Novel> {
        const novel = await http.post<Novel>("/api/v1/novels", data);
        return withDefaults(novel);
      },
      async update(novelId: string, patch: UpdateNovelInput): Promise<Novel> {
        const novel = await http.patch<Novel>(
          `/api/v1/novels/${novelId}`,
          patch,
        );
        return withDefaults(novel);
      },
      async remove(novelId: string): Promise<void> {
        // v1: returns 204 No Content.
        await http.delete<void>(`/api/v1/novels/${novelId}`);
      },
      async uploadCover(novelId: string, file: File): Promise<Novel> {
        const form = new FormData();
        form.set("cover", file);
        const novel = await http.post<Novel>(
          `/api/v1/novels/${novelId}/cover`,
          form,
        );
        return withDefaults(novel);
      },
      async copy(novelId: string): Promise<Novel> {
        const novel = await http.post<Novel>(`/api/v1/novels/${novelId}/clone`);
        return withDefaults(novel);
      },
      async updateVisibility(
        novelId: string,
        isPublic: boolean,
      ): Promise<Novel> {
        const novel = await http.patch<Novel>(
          `/api/v1/novels/${novelId}/visibility`,
          { isPublic },
        );
        return withDefaults(novel);
      },
      async checkBatchUpdates(): Promise<BatchCheckResponse> {
        // v1: was GET /check-batch-updates, now POST /batch-check.
        return http.post<BatchCheckResponse>("/api/v1/novels/batch-check", {});
      },
      async batchCheck(novelIds: string[]): Promise<{ jobs: { novelId: string; jobId: string }[] }> {
        return http.post("/api/v1/novels/batch-check-scheduled", { novelIds });
      },
      async batchUpdateFromUrl(
        selections: BatchUpdateSelection[],
      ): Promise<BatchUpdateResponse> {
        return http.post<BatchUpdateResponse>(
          "/api/v1/novels/batch-update",
          { selections },
        );
      },
      async batchTranslatePreview(): Promise<BatchTranslateResponse> {
        // v1: was GET, now POST (the request triggers enqueueable work).
        return http.post<BatchTranslateResponse>(
          "/api/v1/novels/batch-translate-preview",
          {},
        );
      },
      async batchTranslate(
        selections: BatchTranslateSelection[],
      ): Promise<BatchTranslateStartResponse> {
        return http.post<BatchTranslateStartResponse>(
          "/api/v1/novels/batch-translate",
          { selections },
        );
      },
      async generateGlossary(
        novelId: string,
        options: GlossaryGenerationOptions,
      ): Promise<{ jobId: string; status: string; operation: string }> {
        return http.post(`/api/v1/novels/${novelId}/glossary/generate`, options);
      },
      async estimateGlossaryTokens(
        novelId: string,
        from: number,
        to: number,
      ): Promise<{ totalTokens: number; chapterCount: number }> {
        const params = new URLSearchParams({ from: String(from) });
        if (to > 0) params.set("to", String(to));
        return http.get(`/api/v1/novels/${novelId}/glossary/estimate-tokens?${params}`);
      },
    },
    chapters: {
      async list(novelId: string): Promise<ChapterSummary[]> {
        const result = await http.get<unknown>(`/api/v1/novels/${novelId}/chapters`);
        const { data } = unwrapCollection<ChapterSummary[]>(result);
        return Array.isArray(data) ? data : [];
      },
      async listEligible(novelId: string, operation: "translate" | "refine"): Promise<ChapterSummary[]> {
        const result = await http.get<unknown>(
          `/api/v1/novels/${novelId}/chapters/eligible?operation=${operation}`,
        );
        const { data } = unwrapCollection<ChapterSummary[]>(result);
        return Array.isArray(data) ? data : [];
      },
      // Full chapter array (with content). v1 has no /chapters/full route;
      // use ?includeContent=true on the /chapters list.
      async listFull(novelId: string): Promise<Chapter[]> {
        const result = await http.get<unknown>(
          `/api/v1/novels/${novelId}/chapters?includeContent=true`,
        );
        const { data } = unwrapCollection<Chapter[]>(result);
        return Array.isArray(data) ? data : [];
      },
      listSummaries(
        novelId: string,
        params: { page?: number; perPage?: number; limit?: number; offset?: number } = {},
      ) {
        const suffix = buildQuery({
          page: params.page,
          per_page: params.perPage,
          limit: params.limit,
          offset: params.offset,
        });
        return http
          .get<unknown>(`/api/v1/novels/${novelId}/chapter-summaries${suffix}`)
          .then((result) => {
            const { data, meta } = unwrapCollection<ChapterSummary[]>(result);
            const items = Array.isArray(data) ? data : [];
            const perPage = meta?.per_page ?? params.perPage ?? items.length;
            const total = meta?.total ?? items.length;
            const offset = meta?.offset ?? params.offset ?? 0;
            return { items, total, limit: perPage, offset } satisfies ChapterSummaryPage;
          });
      },
      async gaps(novelId: string): Promise<{ gaps: Array<{ from: number; to: number; count: number }> }> {
        const result = await http.get<{ gaps: Array<{ from: number; to: number; count: number }> }>(
          `/api/v1/novels/${novelId}/chapters/gaps`,
        );
        return { gaps: Array.isArray(result?.gaps) ? result.gaps : [] };
      },

      get(novelId: string, chapterId: string) {
        return http.get<Chapter | null>(
          `/api/v1/novels/${novelId}/chapters/${chapterId}`,
        );
      },
      upsert(novelId: string, chapter: ChapterUpsertInput) {
        return http.post<Chapter>(
          `/api/v1/novels/${novelId}/chapters`,
          chapter,
        );
      },
      async remove(novelId: string, chapterId: string) {
        // v1: returns 204 No Content.
        await http.delete<void>(
          `/api/v1/novels/${novelId}/chapters/${chapterId}`,
        );
      },
      async bulkRemove(novelId: string, ids: string[]) {
        const result = await http.post<{ deleted?: number; requested?: number }>(
          `/api/v1/novels/${novelId}/chapters/bulk-delete`,
          { ids },
        );
        return {
          deleted: result?.deleted ?? 0,
          requested: result?.requested ?? ids.length,
        };
      },
      clean(
        novelId: string,
        input: {
          chapterIds: string[];
          mode: string;
          searchText: string;
          replaceText?: string;
          caseSensitive: boolean;
          useRegex: boolean;
          applyTo: string;
        },
      ) {
        return http.post<{
          modified: number;
          total: number;
          skipped: number;
          notFound: number;
          failed: number;
        }>(`/api/v1/novels/${novelId}/chapters/clean`, input);
      },
      cleanPreview(
        novelId: string,
        input: {
          chapterId: string;
          mode: string;
          searchText: string;
          replaceText?: string;
          caseSensitive: boolean;
          useRegex: boolean;
          applyTo: string;
        },
      ) {
        return http.post<CleanPreviewResponse>(
          `/api/v1/novels/${novelId}/chapters/clean-preview`,
          input,
        );
      },
      cleanPreviewBulk(
        novelId: string,
        input: {
          chapterIds: string[];
          mode: string;
          searchText: string;
          replaceText?: string;
          caseSensitive: boolean;
          useRegex: boolean;
          applyTo: string;
        },
      ) {
        return http.post<CleanPreviewBulkResponse>(
          `/api/v1/novels/${novelId}/chapters/clean-preview-bulk`,
          input,
        );
      },
      async updateStatus(
        novelId: string,
        chapterId: string,
        status: Chapter["status"],
        errorMessage?: string,
      ) {
        await http.patch<Chapter>(
          `/api/v1/novels/${novelId}/chapters/${chapterId}/status`,
          { status, errorMessage },
        );
      },
    },
    jobs: {
      create(
        novelId: string,
        chapterIds: string[],
        options: TranslationJobOptions = {},
      ) {
        return http.post<TranslationJob>(
          `/api/v1/novels/${novelId}/jobs`,
          {
            chapterIds,
            operation: options.operation,
            options: {
              provider: options.provider,
              model: options.model,
            },
          },
        );
      },
      async list(
        novelId: string,
        options: { failedOnly?: boolean; page?: number; perPage?: number } = {},
      ): Promise<TranslationJob[]> {
        const suffix = buildQuery({
          failedOnly: options.failedOnly ? "1" : undefined,
          page: options.page,
          per_page: options.perPage,
        });
        const result = await http.get<unknown>(
          `/api/v1/novels/${novelId}/jobs${suffix}`,
        );
        const { data } = unwrapCollection<TranslationJob[]>(result);
        return Array.isArray(data) ? data : [];
      },
      // v1 returns hasActive alongside the active jobs list, so a single
      // /jobs/active fetch powers both `status` and `listActive` callers.
      async status(): Promise<{ hasActive: boolean }> {
        const result = await http.get<{ data?: { hasActive?: boolean } }>(
          "/api/v1/jobs/active",
        );
        return { hasActive: Boolean(result.data?.hasActive) };
      },
      async listActive(): Promise<TranslationJob[]> {
        const result = await http.get<{ data?: { jobs?: TranslationJob[] } }>(
          "/api/v1/jobs/active",
        );
        return Array.isArray(result.data?.jobs) ? result.data.jobs : [];
      },
      async update(jobId: string, patch: Partial<TranslationJob>): Promise<TranslationJob> {
        // v1 split PATCH /jobs/{jobId} into two explicit sub-routes: cancel
        // and retry. Any other status is a no-op; we just refetch the job.
        const status = (patch as { status?: string }).status;
        if (status === "cancelled") {
          return http.post<TranslationJob>(`/api/v1/jobs/${jobId}/cancel`);
        }
        if (status === "pending") {
          return http.post<TranslationJob>(`/api/v1/jobs/${jobId}/retry`);
        }
        return http.get<TranslationJob>(`/api/v1/jobs/${jobId}`);
      },
    },
    prompts: {
      async list() {
        const result = await http.get<unknown>("/api/v1/prompts");
        const { data } = unwrapCollection<GeneralPromptRecord[]>(result);
        return (Array.isArray(data) ? data : []) as GeneralPromptRecord[];
      },
      upsert(input: {
        key: GeneralPromptKey;
        label?: string;
        description?: string;
        prompt: { systemPrompt?: string; userPrompt?: string };
        active?: boolean;
      }) {
        return http.put<GeneralPromptRecord>(
          `/api/v1/prompts/${input.key}`,
          input,
        );
      },
    },
    readingProgress: {
      async get(novelId: string): Promise<ReadingProgress | null> {
        try {
          return await http.get<ReadingProgress>(
            `/api/v1/novels/${novelId}/reading-progress`,
          );
        } catch {
          return null;
        }
      },
      async update(
        novelId: string,
        data: { chapterId: string; scrollPercent: number },
      ): Promise<ReadingProgress> {
        return http.put<ReadingProgress>(
          `/api/v1/novels/${novelId}/reading-progress`,
          data,
        );
      },
    },
    epubs: {
      async listByNovel(novelId: string): Promise<NovelEpubRecord[]> {
        const result = await http.get<unknown>(
          `/api/v1/epubs?novelId=${encodeURIComponent(novelId)}`,
        );
        const { data } = unwrapCollection<NovelEpubRecord[]>(result);
        return Array.isArray(data) ? data : [];
      },
      build(input: {
        novelId: string;
        source: "original" | "translated" | "refined";
      }) {
        return http.post<NovelEpubRecord>("/api/v1/epubs/build", input);
      },
      save(input: {
        novelId: string;
        fileKind: "original" | "translated";
        sourceVariant?: "original" | "translated" | "refined";
        fileName: string;
        blob: Blob;
      }) {
        const form = new FormData();
        form.set("novelId", input.novelId);
        form.set("fileKind", input.fileKind);
        if (input.sourceVariant) form.set("sourceVariant", input.sourceVariant);
        form.set(
          "file",
          new File([input.blob], input.fileName, {
            type: input.blob.type || "application/epub+zip",
          }),
        );
        return http.post<NovelEpubRecord>("/api/v1/epubs", form);
      },
      preview(file: Blob, fileName: string) {
        const form = new FormData();
        form.set(
          "file",
          new File([file], fileName, { type: "application/epub+zip" }),
        );
        return http.post<EpubPreviewResult>("/api/v1/epubs/preview", form);
      },
      download(id: string, cacheBust?: string) {
        const suffix = cacheBust ? `?v=${encodeURIComponent(cacheBust)}` : "";
        return http.downloadBlob(`/api/v1/epubs/${id}/download${suffix}`);
      },
    },
    workerTokens: {
      async list(): Promise<WorkerToken[]> {
        const result = await http.get<{ tokens?: WorkerToken[] }>(
          "/api/v1/worker-auth/tokens",
        );
        return Array.isArray(result?.tokens) ? result.tokens : [];
      },
      async revoke(tokenId: string): Promise<void> {
        await http.post<void>(`/api/v1/worker-auth/revoke/${tokenId}`);
      },
      async delete(tokenId: string): Promise<void> {
        await http.post<void>(`/api/v1/worker-auth/delete/${tokenId}`);
      },
    },
  };
}

export type ApiClient = ReturnType<typeof createApiClient>;
