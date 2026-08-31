import { ref, watch, type Ref } from "vue";
import type { Chapter } from "@/domain";
import type { ChapterSummary } from "@/api/types";
import { useAppServices } from "@/app/services";
import type { CachedNovel } from "@/composables/useOfflineCache";

export type ChapterGap = { from: number; to: number; count: number };

export type ChapterSummariesResult = {
  items: ChapterSummary[];
  total: number;
};

export type OfflineDeps = {
  isOnline: Ref<boolean>;
  getCachedNovel: (id: string) => Promise<CachedNovel | null>;
};

export function useChapterSummaries(
  novelId: Ref<string>,
  chapterPage: Ref<number>,
  chapterPageSize: number,
  offline: OfflineDeps,
) {
  const { api } = useAppServices();
  const { isOnline, getCachedNovel } = offline;

  const chapterSummaries = ref<ChapterSummary[]>([]);
  const chapterSummaryTotal = ref(0);
  const chapterSummariesLoading = ref(false);
  const chapterGaps = ref<ChapterGap[]>([]);

  const allSummaries = ref<ChapterSummary[]>([]);
  const allSummariesLoading = ref(false);
  const allSummariesLoaded = ref(false);
  const allSummariesDirty = ref(false);

  const cleanAllSummaries = ref<ChapterSummary[]>([]);
  const cleanAllSummariesLoading = ref(false);
  const cleanAllSummariesLoaded = ref(false);
  const cleanAllSummariesDirty = ref(false);

  const translateAllSummaries = ref<ChapterSummary[]>([]);
  const translateAllLoaded = ref(false);
  const translateAllLoading = ref(false);

  const fullChaptersLoaded = ref(false);
  const fullChapters = ref<Chapter[]>([]);

  let chapterSummariesInflight: Promise<void> | null = null;

  function chapterToSummary(chapter: Chapter): ChapterSummary {
    return {
      id: chapter.id,
      novelId: chapter.novelId,
      chapterOrder: chapter.chapterOrder,
      title: chapter.title,
      translatedTitle: chapter.translatedTitle,
      status: chapter.status,
      errorMessage: chapter.errorMessage,
      hasOriginalContent: !!chapter.originalContent,
      hasTranslatedContent: !!chapter.translatedContent,
      hasRefinedContent: !!chapter.refinedContent,
      originalChars: chapter.originalContent?.length || 0,
      translatedChars: chapter.translatedContent?.length || 0,
      refinedChars: chapter.refinedContent?.length || 0,
      createdAt: chapter.createdAt,
      updatedAt: chapter.updatedAt,
    };
  }

  function computeGaps(chapters: Chapter[]): ChapterGap[] {
    const sorted = [...chapters].sort((a, b) => a.chapterOrder - b.chapterOrder);
    const gaps: ChapterGap[] = [];
    if (sorted.length === 0) return gaps;
    let expected = 1;
    let gapStart: number | null = null;
    for (const ch of sorted) {
      if (ch.chapterOrder > expected) {
        if (gapStart === null) gapStart = expected;
      } else if (gapStart !== null && ch.chapterOrder === expected) {
        gaps.push({ from: gapStart, to: expected - 1, count: expected - gapStart });
        gapStart = null;
      }
      expected = ch.chapterOrder + 1;
    }
    if (gapStart !== null) {
      gaps.push({ from: gapStart, to: expected - 1, count: expected - gapStart });
    }
    return gaps;
  }

  function shallowSummaryEquals(a: ChapterSummary, b: ChapterSummary): boolean {
    return (
      a.id === b.id &&
      a.novelId === b.novelId &&
      a.chapterOrder === b.chapterOrder &&
      a.title === b.title &&
      a.translatedTitle === b.translatedTitle &&
      a.status === b.status &&
      a.errorMessage === b.errorMessage &&
      a.hasOriginalContent === b.hasOriginalContent &&
      a.hasTranslatedContent === b.hasTranslatedContent &&
      a.hasRefinedContent === b.hasRefinedContent &&
      a.originalChars === b.originalChars &&
      a.translatedChars === b.translatedChars &&
      a.refinedChars === b.refinedChars &&
      a.createdAt === b.createdAt &&
      a.updatedAt === b.updatedAt
    );
  }

  function mergeChapterSummaries(fresh: ChapterSummary[]) {
    const current = chapterSummaries.value;
    if (current.length === 0) {
      chapterSummaries.value = [...fresh].sort((a, b) => a.chapterOrder - b.chapterOrder);
      return;
    }
    const currentById = new Map(current.map((item) => [item.id, item]));
    const sortedFresh = [...fresh].sort((a, b) => a.chapterOrder - b.chapterOrder);
    const next: ChapterSummary[] = [];
    let mutated = false;
    for (const item of sortedFresh) {
      const existing = currentById.get(item.id);
      if (existing && shallowSummaryEquals(existing, item)) {
        next.push(existing);
      } else {
        next.push(item);
        mutated = true;
      }
    }
    if (next.length !== current.length) mutated = true;
    if (mutated) {
      chapterSummaries.value = next;
    }
  }

  function patchSummaryStatus(
    items: ChapterSummary[],
    chapterIds: string[],
    status: Chapter["status"],
    errorMessage = "",
  ) {
    if (items.length === 0 || chapterIds.length === 0) return items;
    const idSet = new Set(chapterIds);
    let mutated = false;
    const next = items.map((chapter) => {
      if (!idSet.has(chapter.id)) return chapter;
      if (chapter.status === status && (chapter.errorMessage || "") === errorMessage) return chapter;
      mutated = true;
      return { ...chapter, status, errorMessage };
    });
    return mutated ? next : items;
  }

  function markAllSummariesDirty() {
    allSummariesDirty.value = true;
    cleanAllSummariesDirty.value = true;
  }

  function resetAll() {
    fullChaptersLoaded.value = false;
    fullChapters.value = [];
    allSummaries.value = [];
    allSummariesLoaded.value = false;
    allSummariesDirty.value = false;
    cleanAllSummaries.value = [];
    cleanAllSummariesLoaded.value = false;
    cleanAllSummariesDirty.value = false;
    translateAllSummaries.value = [];
    translateAllLoaded.value = false;
    chapterSummaries.value = [];
    chapterSummaryTotal.value = 0;
  }

  function loadChapterSummaries(): Promise<void> {
    if (chapterSummariesInflight) return chapterSummariesInflight;
    chapterSummariesInflight = runLoadChapterSummaries().finally(() => {
      chapterSummariesInflight = null;
    });
    return chapterSummariesInflight;
  }

  async function runLoadChapterSummaries() {
    if (!novelId.value) {
      chapterSummaries.value = [];
      chapterSummaryTotal.value = 0;
      chapterGaps.value = [];
      return;
    }
    const targetNovelId = novelId.value;
    const targetPage = chapterPage.value;
    chapterSummariesLoading.value = true;
    try {
      let result: ChapterSummariesResult | null = null;
      let gapsResult: { gaps: ChapterGap[] } | null = null;

      if (!isOnline.value) {
        const cached = await getCachedNovel(targetNovelId);
        if (cached) {
          const summaries = cached.chapters.map(chapterToSummary);
          result = { items: summaries, total: summaries.length };
          gapsResult = { gaps: computeGaps(cached.chapters) };
        }
      }

      if (!result || !gapsResult) {
        const [apiResult, apiGapsResult] = await Promise.all([
          api.chapters.listSummaries(targetNovelId, {
            limit: chapterPageSize,
            offset: targetPage * chapterPageSize,
          }),
          api.chapters.gaps(targetNovelId),
        ]);
        result = apiResult;
        gapsResult = apiGapsResult;
      }

      // Discard stale response if novelId or page changed during the await.
      if (novelId.value !== targetNovelId || chapterPage.value !== targetPage) return;

      chapterSummaryTotal.value = result.total;
      mergeChapterSummaries(result.items);
      chapterGaps.value = gapsResult.gaps;
    } finally {
      if (novelId.value === targetNovelId) {
        chapterSummariesLoading.value = false;
      }
    }
  }

  async function loadAllSummaries(force: boolean, operation: "translate" | "refine") {
    if (!novelId.value) {
      allSummaries.value = [];
      allSummariesLoaded.value = false;
      allSummariesDirty.value = false;
      return;
    }
    if (!force && allSummariesLoaded.value && !allSummariesDirty.value) {
      return;
    }
    allSummariesLoading.value = true;
    try {
      let summaries: ChapterSummary[] = [];
      if (!isOnline.value) {
        const cached = await getCachedNovel(novelId.value);
        if (cached) summaries = cached.chapters.map(chapterToSummary);
      }
      if (summaries.length === 0 || isOnline.value) {
        summaries = await api.chapters.listEligible(novelId.value, operation);
      }
      allSummaries.value = summaries;
      allSummariesLoaded.value = true;
      allSummariesDirty.value = false;
    } finally {
      allSummariesLoading.value = false;
    }
  }

  async function loadCleanAllSummaries(force = false) {
    if (!novelId.value) {
      cleanAllSummaries.value = [];
      cleanAllSummariesLoaded.value = false;
      cleanAllSummariesDirty.value = false;
      return;
    }
    if (!force && cleanAllSummariesLoaded.value && !cleanAllSummariesDirty.value) {
      return;
    }
    cleanAllSummariesLoading.value = true;
    try {
      let summaries: ChapterSummary[] = [];
      if (!isOnline.value) {
        const cached = await getCachedNovel(novelId.value);
        if (cached) summaries = cached.chapters.map(chapterToSummary);
      }
      if (summaries.length === 0 || isOnline.value) {
        summaries = await api.chapters.list(novelId.value);
      }
      cleanAllSummaries.value = summaries;
      cleanAllSummariesLoaded.value = true;
      cleanAllSummariesDirty.value = false;
    } finally {
      cleanAllSummariesLoading.value = false;
    }
  }

  async function loadTranslateAll(force = false) {
    if (!novelId.value) return;
    if (!force && translateAllLoaded.value) return;
    translateAllLoading.value = true;
    try {
      let summaries: ChapterSummary[] = [];
      if (!isOnline.value) {
        const cached = await getCachedNovel(novelId.value);
        if (cached) summaries = cached.chapters.map(chapterToSummary);
      }
      if (summaries.length === 0 || isOnline.value) {
        summaries = await api.chapters.list(novelId.value);
      }
      translateAllSummaries.value = summaries;
      translateAllLoaded.value = true;
    } finally {
      translateAllLoading.value = false;
    }
  }

  async function ensureFullChaptersLoaded(force = false, listChapters: () => Promise<Chapter[]>) {
    if (!novelId.value) return [] as Chapter[];
    if (fullChaptersLoaded.value && !force) return fullChapters.value;

    let items: Chapter[] = [];
    if (!isOnline.value) {
      const cached = await getCachedNovel(novelId.value);
      if (cached) items = cached.chapters;
    }
    if (items.length === 0 || isOnline.value) {
      items = await listChapters();
    }
    fullChapters.value = items;
    fullChaptersLoaded.value = true;
    return items;
  }

  watch(novelId, () => {
    resetAll();
  });

  return {
    chapterSummaries,
    chapterSummaryTotal,
    chapterSummariesLoading,
    chapterGaps,
    allSummaries,
    allSummariesLoading,
    allSummariesLoaded,
    cleanAllSummaries,
    cleanAllSummariesLoading,
    cleanAllSummariesLoaded,
    translateAllSummaries,
    translateAllLoaded,
    translateAllLoading,
    fullChaptersLoaded,
    loadChapterSummaries,
    loadAllSummaries,
    loadCleanAllSummaries,
    loadTranslateAll,
    ensureFullChaptersLoaded,
    patchSummaryStatus,
    markAllSummariesDirty,
    resetAll,
  };
}
