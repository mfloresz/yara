<template>
  <n-modal
    :show="show"
    preset="card"
    :style="{ width: 'min(640px, 96vw)' }"
    :closable="false"
    :auto-focus="false"
    role="dialog"
    aria-modal="true"
    :aria-label="`Búsqueda de ${mediaLabel}`"
    content-style="padding: 0; display: flex; flex-direction: column; max-height: min(68vh, 540px);"
    @update:show="onModalUpdate"
  >
    <div class="msm-input-row">
      <n-icon :size="18" class="msm-loupe" aria-hidden="true"><SearchOutline /></n-icon>
      <n-input
        ref="inputRef"
        v-model:value="query"
        class="msm-input"
        size="large"
        :bordered="false"
        :placeholder="placeholder"
        clearable
        :input-props="inputProps"
        @focus="onFocus"
        @keydown="onKeydown"
        @clear="onClear"
      >
        <template v-if="media === 'novels'" #suffix>
          <span class="msm-criterion" aria-hidden="true">{{ activeMeta.label }}</span>
          <n-popover trigger="click" placement="bottom-end" :show-arrow="false">
            <template #trigger>
              <n-button
                quaternary
                circle
                size="tiny"
                class="msm-filter"
                :aria-label="`Criterio de búsqueda: ${activeMeta.label}. Cambiar criterio`"
                @click.stop
              >
                <template #icon>
                  <n-icon><OptionsOutline /></n-icon>
                </template>
              </n-button>
            </template>
            <div role="menu" aria-label="Criterio de búsqueda" class="msm-menu">
              <n-button
                v-for="item in GLOBAL_SEARCH_CRITERIA"
                :key="item.key"
                text
                block
                class="msm-menu-item touch-target"
                :class="{ 'msm-menu-item--active': criterion === item.key }"
                role="menuitemradio"
                :aria-checked="criterion === item.key"
                @click="setCriterion(item.key)"
              >
                {{ item.label }}
              </n-button>
            </div>
          </n-popover>
        </template>
      </n-input>
    </div>

    <div v-if="panelOpen" id="msm-listbox" class="msm-results" role="listbox" :aria-label="`Resultados de ${mediaLabel}`">
      <div v-if="showLoading" class="msm-loading" role="status" aria-label="Buscando">
        <n-skeleton text :repeat="3" />
      </div>

      <!-- Cómics: el backend filtra por título, autor o serie con q. -->
      <template v-else-if="media === 'comics'">
        <p v-if="comicSearched && comicResults.length === 0" class="msm-empty" role="status">
          No se encontraron cómics para «{{ query.trim() }}»
        </p>
        <button
          v-for="(comic, index) in visibleComics"
          :id="`msm-opt-${index}`"
          :key="comic.id"
          type="button"
          role="option"
          :aria-selected="activeIndex === index"
          class="msm-item"
          :class="{ 'msm-item--active': activeIndex === index }"
          @click="goToComic(comic.id)"
          @mousemove="activeIndex = index"
        >
          <img :src="coverSrc(comic.coverPath)" alt="" class="msm-cover" @error="onCoverError" />
          <span class="msm-item-text">
            <span class="msm-item-title">{{ getComicDisplayTitle(comic) }}</span>
            <span class="msm-item-sub">{{ comicSub(comic) }}</span>
          </span>
        </button>
      </template>

      <!-- Novelas por título. -->
      <template v-else-if="criterion === 'title'">
        <p v-if="searched && results.length === 0" class="msm-empty" role="status">
          No se encontraron novelas para «{{ query.trim() }}»
        </p>
        <button
          v-for="(novel, index) in visibleResults"
          :id="`msm-opt-${index}`"
          :key="novel.id"
          type="button"
          role="option"
          :aria-selected="activeIndex === index"
          class="msm-item"
          :class="{ 'msm-item--active': activeIndex === index }"
          @click="goToNovel(novel.id)"
          @mousemove="activeIndex = index"
        >
          <img :src="coverSrc(novel.coverPath)" alt="" class="msm-cover" @error="onCoverError" />
          <span class="msm-item-text">
            <span class="msm-item-title">{{ getNovelDisplayTitle(novel) }}</span>
            <span class="msm-item-sub">{{ getNovelDisplayAuthor(novel) }}</span>
          </span>
        </button>
      </template>

      <!-- Novelas por faceta: chips coincidentes y novelas de la faceta elegida. -->
      <template v-else>
        <div class="msm-tags" role="group" :aria-label="`${activeMeta.label} coincidentes`">
          <p v-if="facetMatches.length === 0" class="msm-empty" role="status">
            Sin {{ activeMeta.label.toLowerCase() }} coincidentes
          </p>
          <div v-else class="msm-tags-row">
            <n-tag
              v-for="(facet, index) in facetMatches"
              :id="`msm-opt-${index}`"
              :key="facet"
              round
              checkable
              :checked="selectedFacet === facet"
              role="option"
              :aria-selected="selectedFacet === facet"
              class="msm-tag"
              :class="{ 'msm-tag--active': activeIndex === index }"
              @click="selectFacetAndFocus(facet)"
            >
              {{ facet }}
            </n-tag>
          </div>
        </div>
        <div v-if="selectedFacet" class="msm-facet-novels">
          <div v-if="loading" class="msm-loading" role="status" aria-label="Cargando novelas">
            <n-spin size="small" />
          </div>
          <p v-else-if="facetNovels.length === 0" class="msm-empty" role="status">
            No se encontraron novelas
          </p>
          <button
            v-for="(novel, index) in visibleFacetNovels"
            :id="`msm-opt-${facetMatches.length + index}`"
            :key="novel.id"
            type="button"
            role="option"
            :aria-selected="activeIndex === facetMatches.length + index"
            class="msm-item"
            :class="{ 'msm-item--active': activeIndex === facetMatches.length + index }"
            @click="goToNovel(novel.id)"
            @mousemove="activeIndex = facetMatches.length + index"
          >
            <img :src="coverSrc(novel.coverPath)" alt="" class="msm-cover" @error="onCoverError" />
            <span class="msm-item-text">
              <span class="msm-item-title">{{ getNovelDisplayTitle(novel) }}</span>
              <span class="msm-item-sub">{{ getNovelDisplayAuthor(novel) }}</span>
            </span>
          </button>
        </div>
      </template>
    </div>

    <div v-else class="msm-idle">
      <p class="msm-idle-title">Busca en tu biblioteca de {{ mediaLabel }}.</p>
      <p class="msm-idle-sub muted">
        {{
          media === "novels"
            ? "Por título, autor, serie o tags — el criterio se cambia junto al campo."
            : "Por título, autor o serie."
        }}
      </p>
    </div>

    <div v-if="panelOpen && (flatCount > 0 || viewAllHref)" class="msm-footer">
      <p v-if="flatCount > 0" class="msm-hint" aria-hidden="true">
        ↑↓ navegar · Enter abrir · Esc cerrar
      </p>
      <span v-if="hiddenCount > 0" class="msm-more">+{{ hiddenCount }} más</span>
      <n-button v-if="viewAllHref" text size="tiny" class="msm-view-all" @click="viewAll">
        Ver todos
      </n-button>
    </div>
  </n-modal>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { NButton, NIcon, NInput, NModal, NPopover, NSkeleton, NSpin, NTag } from "naive-ui";
import { OptionsOutline, SearchOutline } from "@vicons/ionicons5";
import {
  getComicDisplayAuthor,
  getComicDisplaySeries,
  getComicDisplayTitle,
  getNovelDisplayAuthor,
  getNovelDisplayTitle,
  type Comic,
} from "@/domain";
import { coverSrc, onCoverError } from "@/utils/cover";
import {
  GLOBAL_SEARCH_CRITERIA,
  criterionMeta,
  facetParam,
  useGlobalNovelSearch,
  type GlobalSearchCriterion,
} from "@/composables/useGlobalNovelSearch";
import { useAppServices } from "@/app/services";

const props = defineProps<{ show: boolean }>();
const emit = defineEmits<{ (e: "update:show", value: boolean): void }>();

const route = useRoute();
const router = useRouter();
const { api } = useAppServices();

// El buscador hereda el medio de la sección activa: cómics en /comics,
// novelas en el resto.
const media = computed<"comics" | "novels">(() =>
  route.path.startsWith("/comics") ? "comics" : "novels",
);
const mediaLabel = computed(() => (media.value === "comics" ? "cómics" : "novelas"));

const search = useGlobalNovelSearch({
  enabled: computed(() => media.value === "novels"),
});
const { criterion, query, open, loading, searched, results, facetMatches, selectedFacet, facetNovels } =
  search;

const inputRef = ref<{ focus: () => void } | null>(null);
const activeIndex = ref(-1);

const MAX_VISIBLE = 8;
const MIN_QUERY_CHARS = 2;
const DEBOUNCE_MS = 300;
const COMIC_RESULT_LIMIT = 12;

const activeMeta = computed(() => criterionMeta(criterion.value));

const placeholder = computed(() =>
  media.value === "comics"
    ? "Buscar cómics por título, autor o serie…"
    : activeMeta.value.placeholder,
);

// --- Búsqueda de cómics (q sobre el listado, con el mismo debounce). ---
const comicResults = ref<Comic[]>([]);
const comicLoading = ref(false);
const comicSearched = ref(false);

let comicTimer: ReturnType<typeof setTimeout> | null = null;
let comicSeq = 0;

watch([query, media], () => {
  if (media.value !== "comics") return;
  const trimmed = query.value.trim();
  if (comicTimer) clearTimeout(comicTimer);
  if (trimmed.length < MIN_QUERY_CHARS) {
    comicSeq++;
    comicLoading.value = false;
    comicSearched.value = false;
    comicResults.value = [];
    return;
  }
  comicLoading.value = true;
  comicTimer = setTimeout(async () => {
    const token = ++comicSeq;
    try {
      const page = await api.comics.list({
        q: trimmed,
        sort: "title",
        order: "asc",
        limit: COMIC_RESULT_LIMIT,
      });
      if (token !== comicSeq) return;
      comicResults.value = page.items;
      comicSearched.value = true;
    } finally {
      if (token === comicSeq) comicLoading.value = false;
    }
  }, DEBOUNCE_MS);
});

// --- Estado de panel y conteos por medio. ---
const panelOpen = computed(() =>
  media.value === "comics" ? comicLoading.value || comicSearched.value : open.value,
);

const showLoading = computed(() =>
  media.value === "comics" ? comicLoading.value && !comicSearched.value : loading.value && !searched.value,
);

const visibleComics = computed(() => comicResults.value.slice(0, MAX_VISIBLE));
const visibleResults = computed(() => results.value.slice(0, MAX_VISIBLE));
const visibleFacetNovels = computed(() => facetNovels.value.slice(0, MAX_VISIBLE));

const hiddenCount = computed(() => {
  if (media.value === "comics") {
    return Math.max(0, comicResults.value.length - visibleComics.value.length);
  }
  if (criterion.value === "title") {
    return Math.max(0, results.value.length - visibleResults.value.length);
  }
  return Math.max(0, facetNovels.value.length - visibleFacetNovels.value.length);
});

const flatCount = computed(() => {
  if (media.value === "comics") return visibleComics.value.length;
  return criterion.value === "title"
    ? visibleResults.value.length
    : facetMatches.value.length + visibleFacetNovels.value.length;
});

// Solo novelas: el listado completo filtrado lo renderiza la biblioteca vía
// query params (los cómics no leen ?q en su página).
const viewAllHref = computed<{ path: string; query: Record<string, string> } | null>(() => {
  if (media.value !== "novels") return null;
  if (criterion.value === "title") {
    const q = query.value.trim();
    if (results.value.length === 0 || q.length < 2) return null;
    return { path: "/novels", query: { q } };
  }
  if (!selectedFacet.value) return null;
  return { path: "/novels", query: { [facetParam(criterion.value)]: selectedFacet.value } };
});

const activeDescendantId = computed(() =>
  activeIndex.value >= 0 && activeIndex.value < flatCount.value
    ? `msm-opt-${activeIndex.value}`
    : undefined,
);

const inputProps = computed(() => ({
  type: "search" as const,
  inputmode: "search" as const,
  enterkeyhint: "search" as const,
  autocomplete: "off" as const,
  autocapitalize: "off" as const,
  spellcheck: false as const,
  role: "combobox" as const,
  "aria-expanded": panelOpen.value,
  "aria-controls": "msm-listbox",
  "aria-activedescendant": activeDescendantId.value,
}));

// El set de opciones cambia con cada tecla: nunca mantener un highlight que
// apunta a otra fila.
watch(flatCount, () => {
  activeIndex.value = -1;
});

watch(
  () => props.show,
  (shown) => {
    if (shown) {
      activeIndex.value = -1;
      nextTick(() => inputRef.value?.focus());
    } else {
      fullReset();
    }
  },
);

function fullReset() {
  search.clear();
  if (comicTimer) {
    clearTimeout(comicTimer);
    comicTimer = null;
  }
  comicSeq++;
  comicLoading.value = false;
  comicSearched.value = false;
  comicResults.value = [];
  activeIndex.value = -1;
}

function onModalUpdate(value: boolean) {
  emit("update:show", value);
}

function setCriterion(next: GlobalSearchCriterion) {
  criterion.value = next;
  activeIndex.value = -1;
  nextTick(() => inputRef.value?.focus());
}

function selectFacetAndFocus(facet: string) {
  activeIndex.value = -1;
  void search.selectFacet(facet);
  nextTick(() => inputRef.value?.focus());
}

function comicSub(comic: Comic) {
  return getComicDisplayAuthor(comic) || getComicDisplaySeries(comic);
}

function goToNovel(novelId: string) {
  requestClose();
  void router.push({ name: "novel-detail", params: { novelId } });
}

function goToComic(comicId: string) {
  requestClose();
  void router.push({ name: "comic-detail", params: { comicId } });
}

function viewAll() {
  const target = viewAllHref.value;
  if (!target) return;
  requestClose();
  void router.push(target);
}

function requestClose() {
  emit("update:show", false);
  fullReset();
}

function onFocus() {
  if (media.value !== "novels") return;
  if (query.value.trim().length >= 2 && searched.value && !open.value) {
    open.value = true;
  }
}

function onClear() {
  fullReset();
}

function selectFlat(index: number) {
  if (media.value === "comics") {
    const comic = visibleComics.value[index];
    if (comic) goToComic(comic.id);
    return;
  }
  if (criterion.value !== "title" && index < facetMatches.value.length) {
    const facet = facetMatches.value[index];
    if (facet !== undefined) selectFacetAndFocus(facet);
    return;
  }
  const novel =
    criterion.value === "title"
      ? visibleResults.value[index]
      : visibleFacetNovels.value[index - facetMatches.value.length];
  if (novel) goToNovel(novel.id);
}

function onKeydown(event: KeyboardEvent) {
  switch (event.key) {
    case "ArrowDown":
    case "ArrowUp": {
      const count = flatCount.value;
      if (count === 0) return;
      event.preventDefault();
      const delta = event.key === "ArrowDown" ? 1 : -1;
      activeIndex.value = ((activeIndex.value + delta) % count + count) % count;
      document
        .getElementById(`msm-opt-${activeIndex.value}`)
        ?.scrollIntoView({ block: "nearest" });
      break;
    }
    case "Enter": {
      if (activeIndex.value >= 0) {
        event.preventDefault();
        selectFlat(activeIndex.value);
      }
      break;
    }
    case "Escape": {
      event.preventDefault();
      requestClose();
      break;
    }
  }
}
</script>

<style scoped>
.msm-input-row {
  display: flex;
  align-items: center;
  gap: 0.625rem;
  padding: 0.25rem 0.625rem 0.25rem 1rem;
  border-bottom: 1px solid var(--divide);
}

.msm-loupe {
  flex-shrink: 0;
  color: var(--text-tertiary);
}

.msm-input {
  flex: 1;
  min-width: 0;
}

.msm-input :deep(.n-input__input-el) {
  font-size: 1.0625rem;
}

.msm-criterion {
  font-size: 0.75rem;
  color: var(--text-tertiary);
  white-space: nowrap;
}

.msm-filter {
  margin-left: 0.25rem;
}

.msm-menu {
  display: flex;
  flex-direction: column;
  gap: 0.125rem;
  min-width: 10rem;
}

.msm-menu-item {
  justify-content: flex-start;
  text-align: left;
  border-radius: var(--radius-sm);
  padding: 0.375rem 0.625rem;
}

.msm-menu-item--active {
  font-weight: 700;
  background: var(--mock-row);
}

.msm-results {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 0.375rem;
}

.msm-idle {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
  padding: 1.25rem 1rem 1.5rem;
}

.msm-idle-title {
  margin: 0;
  font-weight: 600;
}

.msm-idle-sub {
  margin: 0;
  font-size: 0.875rem;
}

.msm-loading {
  padding: 0.75rem;
}

.msm-empty {
  padding: 0.875rem 0.75rem;
  font-size: 0.875rem;
  color: var(--text-secondary);
  text-align: center;
}

.msm-item {
  display: flex;
  align-items: center;
  gap: 0.625rem;
  width: 100%;
  min-height: 52px;
  padding: 0.375rem 0.5rem;
  background: transparent;
  border: 0;
  border-radius: var(--radius-sm);
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: pointer;
}

.msm-item:hover,
.msm-item--active {
  background: var(--mock-row);
}

.msm-cover {
  width: 2rem;
  height: 3rem;
  flex-shrink: 0;
  object-fit: cover;
  border-radius: 4px;
  background: var(--surface-muted);
}

.msm-item-text {
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.msm-item-title {
  font-weight: 600;
  font-size: 0.9375rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.msm-item-sub {
  font-size: 0.8125rem;
  color: var(--text-secondary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.msm-tags {
  padding: 0.25rem 0.125rem;
}

.msm-tags-row {
  display: flex;
  flex-wrap: wrap;
  gap: 0.375rem;
  padding: 0.25rem;
}

.msm-tag--active {
  outline: 2px solid var(--accent-link);
  outline-offset: 1px;
}

.msm-facet-novels {
  border-top: 1px solid var(--divide);
  margin-top: 0.25rem;
  padding-top: 0.25rem;
}

.msm-footer {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  border-top: 1px solid var(--divide);
  padding: 0.375rem 0.75rem;
}

.msm-hint {
  margin: 0;
  font-size: 0.75rem;
  color: var(--text-tertiary);
  flex: 1;
}

.msm-more {
  margin-left: auto;
  font-size: 0.75rem;
  color: var(--text-tertiary);
  white-space: nowrap;
}

.msm-view-all {
  flex-shrink: 0;
  margin-left: auto;
}

@media (prefers-reduced-motion: reduce) {
  .msm-card,
  .msm-results,
  .msm-item {
    transition: none !important;
  }
}
</style>
