import { computed, ref } from "vue";
import type { CreateNovelInput, Novel, UpdateNovelInput } from "@/domain";
import type {
  ImportEpubResult,
  ImportUrlResult,
  ImportZipResult,
  PreviewUrlResult,
} from "@/api/types";
import { useAppServices } from "@/app/services";

const novels = ref<Novel[]>([]);
const fullNovelIds = new Set<string>();
const hasMore = ref(true);
const loadingMore = ref(false);
// Track the current offset for pagination (starts at 0 after each fresh load)
let currentOffset = 0;
let lastSelect: string[] | undefined;
const PAGE_SIZE = 100;
const maxListLimit = 1000;

export type NovelSortField = "title" | "created" | "lastRead";
export type NovelSortOrder = "asc" | "desc";

// Library filters (GET /api/v1/novels ?shared/?progress/?tag). They travel with
// every list call so pagination and search honor the active filters, and they
// take part in the list signature: changing a filter forces a fresh page-0 load.
export type NovelListFilters = {
  shared?: "all" | "own" | "shared";
  progress?: "all" | "translated" | "completed" | "ongoing";
  tag?: string | null;
};

// Track the sort/order used for the last list load so pagination (loadMore) and
// search reuse the same ordering. Changing the sort changes the list signature,
// which forces a fresh page-0 load.
let lastSort: NovelSortField = "title";
let lastOrder: NovelSortOrder = "asc";
let lastFilters: NovelListFilters = {};
// Signature currently reflected by novels.value. A previously loaded signature
// that differs from this one is stale (the state belongs to another filter/sort
// combination) and must be refetched, not served from state.
let currentListSignature = "";

function filterKey(filters: NovelListFilters) {
  return `${filters.shared ?? "all"}:${filters.progress ?? "all"}:${filters.tag ?? ""}`;
}

export function useNovels() {
  const { api } = useAppServices();
  const loading = ref(false);

  function listSignature(select?: string[], sort?: NovelSortField, order?: NovelSortOrder, filters?: NovelListFilters) {
    const s = sort ?? lastSort;
    const o = order ?? lastOrder;
    const f = filters ?? lastFilters;
    const key = `${s}:${o}:${filterKey(f)}`;
    if (!select || select.length === 0) return `${key}:__full__`;
    return `${key}:${[...select].sort().join(",")}`;
  }

  function markNovelsFull(items: Novel[]) {
    items.forEach((item) => fullNovelIds.add(item.id));
  }

  function mergeListedNovel(
    existing: Novel | undefined,
    incoming: Novel,
    select?: string[],
  ) {
    if (!existing || !select || select.length === 0) return incoming;
    const next = { ...existing } as Record<string, unknown>;
    const source = incoming as Record<string, unknown>;
    for (const field of select) {
      next[field] = source[field];
    }
    next.id = incoming.id;
    return next as Novel;
  }

  function mergeNovelList(items: Novel[], select?: string[]) {
    const existingById = new Map(novels.value.map((item) => [item.id, item]));
    return items.map((item) =>
      mergeListedNovel(existingById.get(item.id), item, select),
    );
  }

  async function listNovels(
    force = false,
    select?: string[],
    sort?: NovelSortField,
    order?: NovelSortOrder,
    filters?: NovelListFilters,
  ) {
    const nextSort = sort ?? lastSort;
    const nextOrder = order ?? lastOrder;
    lastSort = nextSort;
    lastOrder = nextOrder;
    if (filters) lastFilters = filters;
    const signature = listSignature(select, nextSort, nextOrder);
    if (!force && currentListSignature === signature) return novels.value;
    loading.value = true;
    currentOffset = 0;
    lastSelect = select;
    hasMore.value = true;
    try {
      const result = await api.novels.list({
        select,
        sort: nextSort,
        order: nextOrder,
        shared: lastFilters.shared,
        progress: lastFilters.progress,
        tag: lastFilters.tag ?? undefined,
        limit: PAGE_SIZE,
        offset: 0,
      });
      novels.value = mergeNovelList(result.items, select);
      hasMore.value = result.hasMore ?? false;
      currentOffset = result.items.length;
      currentListSignature = signature;
      // The list endpoint now always uses a sparse fieldset (NOVEL_LIST_FIELDS);
      // do not mark these as full — getNovel(novelId) will refetch the
      // complete record when a component needs the heavy fields.
      return novels.value;
    } finally {
      loading.value = false;
    }
  }

  async function loadMoreNovels() {
    if (!hasMore.value || loadingMore.value) return;
    loadingMore.value = true;
    try {
      const result = await api.novels.list({
        select: lastSelect,
        sort: lastSort,
        order: lastOrder,
        shared: lastFilters.shared,
        progress: lastFilters.progress,
        tag: lastFilters.tag ?? undefined,
        limit: PAGE_SIZE,
        offset: currentOffset,
      });
      const merged = mergeNovelList(result.items, lastSelect);
      novels.value = [...novels.value, ...merged];
      hasMore.value = result.hasMore ?? false;
      currentOffset += result.items.length;
      // List responses are sparse; getNovel() will refetch the full record
      // when a component actually needs the heavy fields.
    } finally {
      loadingMore.value = false;
    }
  }

  // Track the last search query so stale responses are ignored
  let lastSearchQuery = "";

  async function searchNovels(query: string) {
    if (!query.trim()) return;
    lastSearchQuery = query;
    loadingMore.value = true;
    try {
      const result = await api.novels.list({
        q: query,
        select: lastSelect,
        sort: lastSort,
        order: lastOrder,
        shared: lastFilters.shared,
        progress: lastFilters.progress,
        tag: lastFilters.tag ?? undefined,
        limit: maxListLimit,
      });
      // Ignore stale response if query changed while request was in flight
      if (lastSearchQuery !== query) return;
      // Merge results: only add novels not already in the list
      const existingIds = new Set(novels.value.map((n) => n.id));
      const newItems = result.items.filter((item) => !existingIds.has(item.id));
      if (newItems.length > 0) {
        const merged = mergeNovelList(newItems, lastSelect);
        novels.value = [...novels.value, ...merged];
      }
      // List responses are sparse; getNovel() will refetch the full record.
    } catch {
      // ignore search errors; the UI keeps the current list
    } finally {
      loadingMore.value = false;
    }
  }

  async function importNovelFromEpub(input: {
    file: Blob;
    fileName: string;
    sourceLanguage?: string;
    targetLanguage: string;
  }): Promise<ImportEpubResult> {
    const result = await api.novels.importFromEpub(input);
    novels.value = [
      result.novel,
      ...novels.value.filter((item) => item.id !== result.novel.id),
    ];
    fullNovelIds.add(result.novel.id);
    return result;
  }

  async function importNovelFromZip(file: File): Promise<ImportZipResult> {
    const result = await api.novels.importFromZip(file, file.name);
    novels.value = [
      result.novel,
      ...novels.value.filter((item) => item.id !== result.novel.id),
    ];
    fullNovelIds.add(result.novel.id);
    return result;
  }

  async function importNovelFromUrl(input: {
    url: string;
    sourceLanguage?: string;
    targetLanguage?: string;
    startChapter?: number;
    endChapter?: number;
  }): Promise<ImportUrlResult> {
    const result = await api.novels.importFromUrl(input);
    novels.value = [
      result.novel,
      ...novels.value.filter((item) => item.id !== result.novel.id),
    ];
    fullNovelIds.add(result.novel.id);
    return result;
  }

  async function previewNovelFromUrl(url: string): Promise<PreviewUrlResult> {
    return api.novels.previewFromUrl(url);
  }

  async function getNovel(novelId: string, force = true) {
    const cached = novels.value.find((item) => item.id === novelId);
    if (cached && !force && fullNovelIds.has(novelId)) return cached;
    const novel = await api.novels.get(novelId);
    if (novel) {
      replaceNovelInList(novel);
    }
    return novel;
  }

  async function createNovel(data: CreateNovelInput) {
    const novel = await api.novels.create(data);
    novels.value = [
      novel,
      ...novels.value.filter((item) => item.id !== novel.id),
    ];
    fullNovelIds.add(novel.id);
    return novel;
  }

  async function updateNovel(novelId: string, patch: UpdateNovelInput) {
    const updated = await api.novels.update(novelId, patch);
    novels.value = novels.value.map((item) =>
      item.id === novelId ? updated : item,
    );
    fullNovelIds.add(updated.id);
    return updated;
  }

  async function deleteNovel(novelId: string) {
    await api.novels.remove(novelId);
    novels.value = novels.value.filter((item) => item.id !== novelId);
    fullNovelIds.delete(novelId);
  }

  function replaceNovelInList(updated: Novel) {
    const index = novels.value.findIndex((item) => item.id === updated.id);
    if (index >= 0) {
      novels.value[index] = updated;
      novels.value = [...novels.value];
    } else {
      novels.value = [updated, ...novels.value];
    }
    fullNovelIds.add(updated.id);
  }

  function hydrateCachedNovels(items: Novel[]) {
    const cachedById = new Map(items.map((item) => [item.id, item]));
    novels.value = [
      ...items,
      ...novels.value.filter((item) => !cachedById.has(item.id)),
    ];
    markNovelsFull(items);
  }

  const items = computed(() => novels.value);

  return {
    novels: items,
    loading,
    hasMore,
    loadingMore,
    listNovels,
    loadMoreNovels,
    searchNovels,
    importNovelFromEpub,
    importNovelFromZip,
    importNovelFromUrl,
    previewNovelFromUrl,
    getNovel,
    createNovel,
    updateNovel,
    deleteNovel,
    replaceNovelInList,
    hydrateCachedNovels,
  };
}
