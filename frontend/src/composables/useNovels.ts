import { computed, ref } from "vue";
import type { CreateNovelInput, Novel, UpdateNovelInput } from "@/domain";
import type {
  ImportEpubResult,
  ImportUrlResult,
  PreviewUrlResult,
} from "@/api/types";
import { useAppServices } from "@/app/services";

const novels = ref<Novel[]>([]);
const loadedListSignatures = new Set<string>();
const fullNovelIds = new Set<string>();
const hasMore = ref(true);
const loadingMore = ref(false);
// Track the current offset for pagination (starts at 0 after each fresh load)
let currentOffset = 0;
let lastSelect: string[] | undefined;
const PAGE_SIZE = 100;
const maxListLimit = 1000;

export function useNovels() {
  const { api } = useAppServices();
  const loading = ref(false);

  function listSignature(select?: string[]) {
    if (!select || select.length === 0) return "__full__";
    return [...select].sort().join(",");
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

  async function listNovels(force = false, select?: string[]) {
    const signature = listSignature(select);
    if (loadedListSignatures.has(signature) && !force) return novels.value;
    loading.value = true;
    currentOffset = 0;
    lastSelect = select;
    hasMore.value = true;
    try {
      const result = await api.novels.list({ select, limit: PAGE_SIZE, offset: 0 });
      novels.value = mergeNovelList(result.items, select);
      hasMore.value = result.hasMore ?? false;
      currentOffset = result.items.length;
      loadedListSignatures.add(signature);
      if (!select || select.length === 0) {
        markNovelsFull(result.items);
      }
      return novels.value;
    } finally {
      loading.value = false;
    }
  }

  async function loadMoreNovels() {
    if (!hasMore.value || loadingMore.value) return;
    loadingMore.value = true;
    try {
      const result = await api.novels.list({ select: lastSelect, limit: PAGE_SIZE, offset: currentOffset });
      const merged = mergeNovelList(result.items, lastSelect);
      novels.value = [...novels.value, ...merged];
      hasMore.value = result.hasMore ?? false;
      currentOffset += result.items.length;
      if (!lastSelect || lastSelect.length === 0) {
        markNovelsFull(result.items);
      }
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
      const result = await api.novels.list({ q: query, select: lastSelect, limit: maxListLimit });
      // Ignore stale response if query changed while request was in flight
      if (lastSearchQuery !== query) return;
      // Merge results: only add novels not already in the list
      const existingIds = new Set(novels.value.map((n) => n.id));
      const newItems = result.items.filter((item) => !existingIds.has(item.id));
      if (newItems.length > 0) {
        const merged = mergeNovelList(newItems, lastSelect);
        novels.value = [...novels.value, ...merged];
      }
      if (!lastSelect || lastSelect.length === 0) {
        markNovelsFull(result.items);
      }
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
