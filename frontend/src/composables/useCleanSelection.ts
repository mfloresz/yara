import { computed, ref, watch, type Ref } from "vue";
import type { ChapterSummary, CleanPreviewItem } from "@/api/types";
import type { CleanMode } from "@/utils/cleaner";

export type CleanApplyTo = "original" | "translated" | "refined" | "all";

export function useCleanSelection(
  novelId: Ref<string>,
  cleanAllSummaries: Ref<ChapterSummary[]>,
) {
  const mode = ref<CleanMode>("search_replace");
  const applyTo = ref<CleanApplyTo>("translated");
  const searchText = ref("");
  const replaceText = ref("");
  const caseSensitive = ref(true);
  const useRegex = ref(false);
  const selectedIds = ref<Set<string>>(new Set());
  const applying = ref(false);

  const previewOpen = ref(false);
  const previewLoading = ref(false);
  const previewItems = ref<CleanPreviewItem[]>([]);
  const previewTotal = ref(0);

  const eligibleChapters = computed(() =>
    cleanAllSummaries.value.filter((chapter) => {
      if (applyTo.value === "all") {
        return chapter.hasOriginalContent || chapter.hasTranslatedContent || chapter.hasRefinedContent;
      }
      if (applyTo.value === "original") return chapter.hasOriginalContent;
      if (applyTo.value === "translated") return chapter.hasTranslatedContent;
      return chapter.hasRefinedContent;
    }),
  );

  function toggle(id: string, checked: boolean) {
    const next = new Set(selectedIds.value);
    if (checked) next.add(id);
    else next.delete(id);
    selectedIds.value = next;
  }

  function selectAll() {
    selectedIds.value = new Set(eligibleChapters.value.map((chapter) => chapter.id));
  }

  function clear() {
    selectedIds.value = new Set();
  }

  function reset() {
    selectedIds.value = new Set();
    previewOpen.value = false;
    previewItems.value = [];
  }

  function buildInput(chapterIds: string[]) {
    return {
      chapterIds,
      mode: mode.value,
      searchText: searchText.value,
      replaceText: replaceText.value,
      caseSensitive: caseSensitive.value,
      useRegex: useRegex.value,
      applyTo: applyTo.value,
    };
  }

  watch(novelId, () => {
    reset();
  });

  return {
    mode,
    applyTo,
    searchText,
    replaceText,
    caseSensitive,
    useRegex,
    selectedIds,
    applying,
    previewOpen,
    previewLoading,
    previewItems,
    previewTotal,
    eligibleChapters,
    toggle,
    selectAll,
    clear,
    reset,
    buildInput,
  };
}
