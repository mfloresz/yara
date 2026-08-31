import { computed, ref, watch, type Ref } from "vue";
import type { ChapterSummary } from "@/api/types";
import { resolvedChapterStatus } from "@/composables/useChapterStatus";

export type TranslateOperation = "translate" | "refine";
export type TranslateSelectionMode = "todos" | "rango" | null;

export function useTranslateSelection(
  novelId: Ref<string>,
  operation: Ref<TranslateOperation>,
  allSummaries: Ref<ChapterSummary[]>,
  translateAllSummaries: Ref<ChapterSummary[]>,
) {
  const showAll = ref(false);
  const selectedIds = ref<Set<string>>(new Set());
  const submitting = ref(false);
  const selectionMode = ref<TranslateSelectionMode>(null);
  const rangeFrom = ref<number | null>(null);
  const rangeTo = ref<number | null>(null);

  const sourceSummaries = computed(() => (showAll.value ? translateAllSummaries.value : allSummaries.value));

  const eligibleChapters = computed(() =>
    sourceSummaries.value.filter((chapter) => {
      const status = resolvedChapterStatus(chapter);
      if (operation.value === "translate") {
        return chapter.hasOriginalContent && (status === "pending" || status === "failed");
      }
      return chapter.hasTranslatedContent && (status === "translated" || status === "failed");
    }),
  );

  function isChapterSelectable(chapter: ChapterSummary): boolean {
    return operation.value === "translate" ? chapter.hasOriginalContent : chapter.hasTranslatedContent;
  }

  const selectableChapters = computed(() => sourceSummaries.value.filter(isChapterSelectable));
  const selectableChapterIds = computed(() => new Set(selectableChapters.value.map((c) => c.id)));

  function toggle(id: string, checked: boolean) {
    const next = new Set(selectedIds.value);
    if (checked) next.add(id);
    else next.delete(id);
    selectedIds.value = next;
  }

  function selectAll() {
    selectionMode.value = "todos";
    selectedIds.value = new Set(selectableChapters.value.map((chapter) => chapter.id));
  }

  function clear() {
    selectedIds.value = new Set();
  }

  function applyRange(warn: (text: string) => void) {
    if (rangeFrom.value == null || rangeTo.value == null) {
      warn("Indica el capítulo inicial y final del rango");
      return;
    }
    const from = Math.min(rangeFrom.value, rangeTo.value);
    const to = Math.max(rangeFrom.value, rangeTo.value);
    const matched = selectableChapters.value.filter((chapter) => chapter.chapterOrder >= from && chapter.chapterOrder <= to);
    if (matched.length === 0) {
      warn(`No hay capítulos seleccionables entre #${from} y #${to}`);
      return;
    }
    selectedIds.value = new Set(matched.map((chapter) => chapter.id));
  }

  function reset() {
    showAll.value = false;
    selectedIds.value = new Set();
    selectionMode.value = null;
    rangeFrom.value = null;
    rangeTo.value = null;
    submitting.value = false;
  }

  function clearSelection() {
    selectedIds.value = new Set();
  }

  function removeFromSelection(ids: string[]) {
    const idSet = new Set(ids);
    selectedIds.value = new Set(
      Array.from(selectedIds.value).filter((id) => !idSet.has(id)),
    );
  }

  function onOperationChanged() {
    selectedIds.value = new Set();
  }

  watch(selectionMode, (mode) => {
    if (mode !== "rango") return;
    const orders = sourceSummaries.value.map((chapter) => chapter.chapterOrder);
    if (orders.length === 0) return;
    const sorted = [...orders].sort((a, b) => a - b);
    if (rangeFrom.value == null) rangeFrom.value = sorted[0];
    if (rangeTo.value == null) rangeTo.value = sorted[sorted.length - 1];
  });

  watch(novelId, () => {
    reset();
  });

  return {
    operation,
    showAll,
    selectedIds,
    submitting,
    selectionMode,
    rangeFrom,
    rangeTo,
    eligibleChapters,
    sourceSummaries,
    selectableChapters,
    selectableChapterIds,
    isChapterSelectable,
    toggle,
    selectAll,
    clear,
    applyRange,
    clearSelection,
    removeFromSelection,
    onOperationChanged,
    reset,
  };
}
