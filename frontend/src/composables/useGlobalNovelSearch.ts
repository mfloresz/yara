import { ref, watch } from "vue";
import type { Novel } from "@/domain";
import { useAppServices } from "@/app/services";

export type GlobalSearchCriterion = "title" | "author" | "series" | "tags";

export const GLOBAL_SEARCH_CRITERIA: Array<{
  key: GlobalSearchCriterion;
  label: string;
  placeholder: string;
}> = [
  { key: "title", label: "Título", placeholder: "Buscar por título..." },
  { key: "author", label: "Autor", placeholder: "Buscar por autor..." },
  { key: "series", label: "Serie", placeholder: "Buscar por serie..." },
  { key: "tags", label: "Tags", placeholder: "Buscar por tags..." },
];

// Sparse fieldset for dropdown results: identity + display fields + cover.
const SEARCH_RESULT_FIELDS =
  "id,sourceTitle,targetTitle,sourceAuthor,targetAuthor,sourceSeries,targetSeries,coverPath";

const MIN_QUERY_CHARS = 2;
const DEBOUNCE_MS = 300;
const NOVEL_RESULT_LIMIT = 8;
const TAG_SUGGESTION_LIMIT = 20;
const TAG_NOVEL_LIMIT = 20;

export function criterionMeta(criterion: GlobalSearchCriterion) {
  return (
    GLOBAL_SEARCH_CRITERIA.find((item) => item.key === criterion) ??
    GLOBAL_SEARCH_CRITERIA[0]
  );
}

export function useGlobalNovelSearch() {
  const { api } = useAppServices();

  const criterion = ref<GlobalSearchCriterion>("title");
  const query = ref("");
  const open = ref(false);
  const loading = ref(false);
  const searched = ref(false);
  const results = ref<Novel[]>([]);
  const tagMatches = ref<string[]>([]);
  const selectedTag = ref<string | null>(null);
  const tagNovels = ref<Novel[]>([]);

  let debounceTimer: ReturnType<typeof setTimeout> | null = null;
  let requestSeq = 0;
  // Set when close()/clear() mutate query/criterion: the watcher must skip
  // that reaction, otherwise resetting the criterion to "title" on close
  // would immediately fire a new search and reopen the dropdown.
  let suppressNextWatch = false;

  function resetCriterion() {
    criterion.value = "title";
    selectedTag.value = null;
  }

  function resetResults() {
    results.value = [];
    tagMatches.value = [];
    tagNovels.value = [];
    searched.value = false;
  }

  // Close the dropdown. Per spec the criterion never persists between search
  // sessions: closing always resets it to Título, and stale results are
  // discarded so reopening never shows another criterion's list.
  function close() {
    requestSeq++;
    if (debounceTimer) clearTimeout(debounceTimer);
    loading.value = false;
    // Only arm the guard when the reset below actually mutates a watched
    // ref; otherwise a stale flag would swallow the next keystroke.
    if (criterion.value !== "title") suppressNextWatch = true;
    open.value = false;
    resetResults();
    resetCriterion();
  }

  function clear() {
    if (query.value !== "" || criterion.value !== "title") {
      suppressNextWatch = true;
    }
    query.value = "";
    selectedTag.value = null;
    tagNovels.value = [];
    close();
  }

  async function runNovelSearch(
    trimmed: string,
    field: GlobalSearchCriterion,
    token: number,
  ) {
    try {
      const res = await api.novels.list({
        q: trimmed,
        field: field === "title" || field === "author" || field === "series" ? field : undefined,
        limit: NOVEL_RESULT_LIMIT,
        offset: 0,
        fields: SEARCH_RESULT_FIELDS,
      });
      if (token !== requestSeq) return;
      results.value = res.items;
      searched.value = true;
      open.value = true;
    } finally {
      if (token === requestSeq) loading.value = false;
    }
  }

  async function runTagSearch(trimmed: string, token: number) {
    try {
      const tags = await api.novels.listTagSuggestions(
        trimmed,
        TAG_SUGGESTION_LIMIT,
      );
      if (token !== requestSeq) return;
      tagMatches.value = tags;
      results.value = [];
      if (selectedTag.value && !tags.includes(selectedTag.value)) {
        selectedTag.value = null;
        tagNovels.value = [];
      }
      searched.value = true;
      open.value = true;
    } finally {
      if (token === requestSeq) loading.value = false;
    }
  }

  async function selectTag(tag: string) {
    selectedTag.value = tag;
    const token = ++requestSeq;
    loading.value = true;
    try {
      const res = await api.novels.list({
        tag,
        limit: TAG_NOVEL_LIMIT,
        offset: 0,
        fields: SEARCH_RESULT_FIELDS,
      });
      if (token !== requestSeq) return;
      tagNovels.value = res.items;
      open.value = true;
    } finally {
      if (token === requestSeq) loading.value = false;
    }
  }

  watch([query, criterion], () => {
    if (suppressNextWatch) {
      suppressNextWatch = false;
      return;
    }
    if (debounceTimer) clearTimeout(debounceTimer);
    const trimmed = query.value.trim();
    selectedTag.value = null;
    tagNovels.value = [];
    if (trimmed.length < MIN_QUERY_CHARS) {
      requestSeq++;
      loading.value = false;
      resetResults();
      open.value = false;
      return;
    }
    // Open immediately so the loading state has a home while debouncing.
    loading.value = true;
    open.value = true;
    debounceTimer = setTimeout(() => {
      const token = ++requestSeq;
      if (criterion.value === "tags") {
        void runTagSearch(trimmed, token);
      } else {
        void runNovelSearch(trimmed, criterion.value, token);
      }
    }, DEBOUNCE_MS);
  });

  return {
    criterion,
    query,
    open,
    loading,
    searched,
    results,
    tagMatches,
    selectedTag,
    tagNovels,
    close,
    clear,
    selectTag,
  };
}

export type GlobalNovelSearch = ReturnType<typeof useGlobalNovelSearch>;
