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
const FACET_SUGGESTION_LIMIT = 20;
const FACET_NOVEL_LIMIT = 20;

export function criterionMeta(criterion: GlobalSearchCriterion) {
  return (
    GLOBAL_SEARCH_CRITERIA.find((item) => item.key === criterion) ??
    GLOBAL_SEARCH_CRITERIA[0]
  );
}

// Facet criteria work like tags: the query matches facet values (chips) first,
// and selecting one lists the novels carrying that value.
function isFacetCriterion(criterion: GlobalSearchCriterion): boolean {
  return criterion !== "title";
}

// Which query param lists the novels of a selected facet value.
export function facetParam(criterion: GlobalSearchCriterion): "tag" | "author" | "series" {
  if (criterion === "author") return "author";
  if (criterion === "series") return "series";
  return "tag";
}

export function useGlobalNovelSearch() {
  const { api } = useAppServices();

  const criterion = ref<GlobalSearchCriterion>("title");
  const query = ref("");
  const open = ref(false);
  const loading = ref(false);
  const searched = ref(false);
  const results = ref<Novel[]>([]);
  const facetMatches = ref<string[]>([]);
  const selectedFacet = ref<string | null>(null);
  const facetNovels = ref<Novel[]>([]);

  let debounceTimer: ReturnType<typeof setTimeout> | null = null;
  let requestSeq = 0;
  // Set when close()/clear() mutate query/criterion: the watcher must skip
  // that reaction, otherwise resetting the criterion to "title" on close
  // would immediately fire a new search and reopen the dropdown.
  let suppressNextWatch = false;

  function resetCriterion() {
    criterion.value = "title";
    selectedFacet.value = null;
  }

  function resetResults() {
    results.value = [];
    facetMatches.value = [];
    facetNovels.value = [];
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
    selectedFacet.value = null;
    facetNovels.value = [];
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

  async function runFacetSearch(trimmed: string, token: number) {
    try {
      const kind = criterion.value;
      const suggestions =
        kind === "author"
          ? await api.novels.listAuthorSuggestions(trimmed, FACET_SUGGESTION_LIMIT)
          : kind === "series"
            ? await api.novels.listSeriesSuggestions(trimmed, FACET_SUGGESTION_LIMIT)
            : await api.novels.listTagSuggestions(trimmed, FACET_SUGGESTION_LIMIT);
      if (token !== requestSeq) return;
      facetMatches.value = suggestions;
      results.value = [];
      if (selectedFacet.value && !suggestions.includes(selectedFacet.value)) {
        selectedFacet.value = null;
        facetNovels.value = [];
      }
      searched.value = true;
      open.value = true;
    } finally {
      if (token === requestSeq) loading.value = false;
    }
  }

  async function selectFacet(value: string) {
    selectedFacet.value = value;
    const token = ++requestSeq;
    loading.value = true;
    try {
      const res = await api.novels.list({
        [facetParam(criterion.value)]: value,
        limit: FACET_NOVEL_LIMIT,
        offset: 0,
        fields: SEARCH_RESULT_FIELDS,
      });
      if (token !== requestSeq) return;
      facetNovels.value = res.items;
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
    selectedFacet.value = null;
    facetNovels.value = [];
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
      if (isFacetCriterion(criterion.value)) {
        void runFacetSearch(trimmed, token);
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
    facetMatches,
    selectedFacet,
    facetNovels,
    close,
    clear,
    selectFacet,
  };
}

export type GlobalNovelSearch = ReturnType<typeof useGlobalNovelSearch>;
