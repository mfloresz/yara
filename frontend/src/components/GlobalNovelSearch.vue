<template>
  <div ref="rootEl" class="global-search">
    <n-button
      ref="loupeRef"
      class="gs-loupe touch-target"
      quaternary
      circle
      size="small"
      aria-label="Buscar novelas"
      :aria-expanded="mobileOpen"
      @click="openMobile"
    >
      <template #icon>
        <n-icon><SearchOutline /></n-icon>
      </template>
    </n-button>

    <div class="gs-box" :class="{ 'gs-mobile-open': mobileOpen }">
      <n-button
        v-if="mobileOpen"
        class="gs-back touch-target"
        quaternary
        circle
        size="small"
        aria-label="Cerrar búsqueda"
        @click="closeMobile"
      >
        <template #icon>
          <n-icon><CloseOutline /></n-icon>
        </template>
      </n-button>

      <div class="gs-input-wrap">
        <n-input
          ref="inputRef"
          v-model:value="query"
          :placeholder="activeMeta.placeholder"
          clearable
          class="gs-input"
          :input-props="inputProps"
          aria-label="Búsqueda global de novelas"
          @focus="onFocus"
          @keydown="onKeydown"
          @clear="onClear"
        >
          <template #prefix>
            <n-icon><SearchOutline /></n-icon>
          </template>
          <template #suffix>
            <span class="gs-criterion" aria-hidden="true">{{ activeMeta.label }}</span>
            <n-popover trigger="click" placement="bottom-end" :show-arrow="false">
              <template #trigger>
                <n-button
                  quaternary
                  circle
                  size="tiny"
                  class="gs-filter"
                  :aria-label="`Criterio de búsqueda: ${activeMeta.label}. Cambiar criterio`"
                  @click.stop
                >
                  <template #icon>
                    <n-icon><OptionsOutline /></n-icon>
                  </template>
                </n-button>
              </template>
              <div role="menu" aria-label="Criterio de búsqueda" class="gs-menu">
                <n-button
                  v-for="item in GLOBAL_SEARCH_CRITERIA"
                  :key="item.key"
                  text
                  block
                  class="gs-menu-item touch-target"
                  :class="{ 'gs-menu-item--active': criterion === item.key }"
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

        <div v-if="open" class="gs-panel">
          <div
            id="gs-listbox"
            class="gs-listbox"
            role="listbox"
            aria-label="Resultados de búsqueda"
          >
            <div v-if="loading && !searched" class="gs-loading" role="status" aria-label="Buscando">
              <n-skeleton text :repeat="3" />
            </div>

            <template v-if="criterion === 'title'">
              <p v-if="searched && results.length === 0" class="gs-empty" role="status">
                No se encontraron novelas para «{{ query.trim() }}»
              </p>
              <button
                v-for="(novel, index) in results"
                :id="`gs-opt-${index}`"
                :key="novel.id"
                type="button"
                role="option"
                :aria-selected="activeIndex === index"
                class="gs-item"
                :class="{ 'gs-item--active': activeIndex === index }"
                @click="goToNovel(novel.id)"
                @mousemove="activeIndex = index"
              >
                <img :src="coverSrc(novel.coverPath)" alt="" class="gs-cover" @error="onCoverError" />
                <span class="gs-item-text">
                  <span class="gs-item-title">{{ getNovelDisplayTitle(novel) }}</span>
                  <span class="gs-item-sub">{{ getNovelDisplayAuthor(novel) }}</span>
                </span>
              </button>
            </template>

            <template v-else>
              <div class="gs-tags" role="group" :aria-label="`${activeMeta.label} coincidentes`">
                <p v-if="facetMatches.length === 0" class="gs-empty" role="status">
                  Sin {{ activeMeta.label.toLowerCase() }} coincidentes
                </p>
                <div v-else class="gs-tags-row">
                  <n-tag
                    v-for="(facet, index) in facetMatches"
                    :id="`gs-opt-${index}`"
                    :key="facet"
                    round
                    checkable
                    :checked="selectedFacet === facet"
                    role="option"
                    :aria-selected="selectedFacet === facet"
                    class="gs-tag"
                    :class="{ 'gs-tag--active': activeIndex === index }"
                    @click="selectFacetAndFocus(facet)"
                  >
                    {{ facet }}
                  </n-tag>
                </div>
              </div>
              <div v-if="selectedFacet" class="gs-facet-novels">
                <div v-if="loading" class="gs-loading" role="status" aria-label="Cargando novelas">
                  <n-spin size="small" />
                </div>
                <p v-else-if="facetNovels.length === 0" class="gs-empty" role="status">
                  No se encontraron novelas
                </p>
                <button
                  v-for="(novel, index) in facetNovels"
                  :id="`gs-opt-${facetMatches.length + index}`"
                  :key="novel.id"
                  type="button"
                  role="option"
                  :aria-selected="activeIndex === facetMatches.length + index"
                  class="gs-item"
                  :class="{ 'gs-item--active': activeIndex === facetMatches.length + index }"
                  @click="goToNovel(novel.id)"
                  @mousemove="activeIndex = facetMatches.length + index"
                >
                  <img :src="coverSrc(novel.coverPath)" alt="" class="gs-cover" @error="onCoverError" />
                  <span class="gs-item-text">
                    <span class="gs-item-title">{{ getNovelDisplayTitle(novel) }}</span>
                    <span class="gs-item-sub">{{ getNovelDisplayAuthor(novel) }}</span>
                  </span>
                </button>
              </div>
            </template>
          </div>

          <div v-if="hasNavigableItems || viewAllHref" class="gs-footer">
            <p v-if="hasNavigableItems" class="gs-hint" aria-hidden="true">
              ↑↓ navegar · Enter abrir · Esc cerrar
            </p>
            <n-button
              v-if="viewAllHref"
              text
              size="tiny"
              class="gs-view-all"
              @click="viewAll"
            >
              Ver todos
            </n-button>
          </div>
        </div>
      </div>
    </div>

    <div v-if="mobileOpen" class="gs-scrim" @click="closeMobile" />
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useRouter } from "vue-router";
import {
  NButton,
  NIcon,
  NInput,
  NPopover,
  NSkeleton,
  NSpin,
  NTag,
} from "naive-ui";
import {
  CloseOutline,
  OptionsOutline,
  SearchOutline,
} from "@vicons/ionicons5";
import { getNovelDisplayAuthor, getNovelDisplayTitle } from "@/domain";
import { coverSrc, onCoverError } from "@/utils/cover";
import {
  GLOBAL_SEARCH_CRITERIA,
  criterionMeta,
  facetParam,
  useGlobalNovelSearch,
  type GlobalSearchCriterion,
} from "@/composables/useGlobalNovelSearch";

const router = useRouter();
const search = useGlobalNovelSearch();
const {
  criterion,
  query,
  open,
  loading,
  searched,
  results,
  facetMatches,
  selectedFacet,
  facetNovels,
} = search;

const rootEl = ref<HTMLElement | null>(null);
const inputRef = ref<{ focus: () => void } | null>(null);
const loupeRef = ref<{ $el?: HTMLElement } | null>(null);
const mobileOpen = ref(false);
const activeIndex = ref(-1);

const activeMeta = computed(() => criterionMeta(criterion.value));

const flatCount = computed(() =>
  criterion.value === "title"
    ? results.value.length
    : facetMatches.value.length + facetNovels.value.length,
);

const hasNavigableItems = computed(() => flatCount.value > 0);

// Target for the "Ver todos" button: hand the active filter to the Dashboard
// via URL query so it renders the full match list (same flow as clicking a
// tag in the novel detail page).
const viewAllHref = computed<{ path: string; query: Record<string, string> } | null>(() => {
  if (criterion.value === "title") {
    const q = query.value.trim();
    if (results.value.length === 0 || q.length < 2) return null;
    return { path: "/", query: { q } };
  }
  if (!selectedFacet.value) return null;
  return { path: "/", query: { [facetParam(criterion.value)]: selectedFacet.value } };
});

const activeDescendantId = computed(() =>
  activeIndex.value >= 0 && activeIndex.value < flatCount.value
    ? `gs-opt-${activeIndex.value}`
    : undefined,
);

const inputProps = computed(() => ({
  type: "search",
  inputmode: "search" as const,
  enterkeyhint: "search" as const,
  autocomplete: "off",
  autocapitalize: "off",
  spellcheck: false as const,
  role: "combobox",
  "aria-expanded": open.value,
  "aria-controls": "gs-listbox",
  "aria-activedescendant": activeDescendantId.value,
}));

// The option set changes with every keystroke: never keep a highlight that
// points at another row.
watch(flatCount, () => {
  activeIndex.value = -1;
});

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

function goToNovel(novelId: string) {
  search.clear();
  mobileOpen.value = false;
  activeIndex.value = -1;
  void router.push({ name: "novel-detail", params: { novelId } });
}

function viewAll() {
  const target = viewAllHref.value;
  if (!target) return;
  search.clear();
  mobileOpen.value = false;
  activeIndex.value = -1;
  void router.push(target);
}

function onFocus() {
  if (query.value.trim().length >= 2 && searched.value && !open.value) {
    open.value = true;
  }
}

function onClear() {
  search.clear();
  activeIndex.value = -1;
}

function selectFlat(index: number) {
  if (criterion.value !== "title" && index < facetMatches.value.length) {
    const facet = facetMatches.value[index];
    if (facet !== undefined) selectFacetAndFocus(facet);
    return;
  }
  const novel =
    criterion.value === "title"
      ? results.value[index]
      : facetNovels.value[index - facetMatches.value.length];
  if (novel) goToNovel(novel.id);
}

function onKeydown(event: KeyboardEvent) {
  switch (event.key) {
    case "ArrowDown":
    case "ArrowUp": {
      if (!open.value) {
        if (hasNavigableItems.value) open.value = true;
        else return;
      }
      event.preventDefault();
      const delta = event.key === "ArrowDown" ? 1 : -1;
      const count = flatCount.value;
      if (count === 0) return;
      activeIndex.value = ((activeIndex.value + delta) % count + count) % count;
      document
        .getElementById(`gs-opt-${activeIndex.value}`)
        ?.scrollIntoView({ block: "nearest" });
      break;
    }
    case "Enter": {
      if (open.value && activeIndex.value >= 0) {
        event.preventDefault();
        selectFlat(activeIndex.value);
      }
      break;
    }
    case "Escape": {
      if (mobileOpen.value) {
        closeMobile();
      } else if (open.value) {
        search.close();
        activeIndex.value = -1;
      }
      break;
    }
  }
}

function openMobile() {
  mobileOpen.value = true;
  nextTick(() => inputRef.value?.focus());
}

function closeMobile() {
  mobileOpen.value = false;
  search.close();
  activeIndex.value = -1;
  const el = loupeRef.value?.$el as HTMLElement | undefined;
  el?.focus?.();
}

function onDocumentClick(event: MouseEvent) {
  const target = event.target as HTMLElement | null;
  // The criterion popover is teleported to <body> by naive-ui, so it lives
  // outside rootEl: selecting a criterion must not count as an outside click.
  if (target?.closest?.(".n-popover")) return;
  if (rootEl.value && target && !rootEl.value.contains(target)) {
    if (mobileOpen.value) {
      closeMobile();
    } else if (open.value) {
      search.close();
      activeIndex.value = -1;
    }
  }
}

onMounted(() => {
  document.addEventListener("click", onDocumentClick);
});

onBeforeUnmount(() => {
  document.removeEventListener("click", onDocumentClick);
});
</script>

<style scoped>
.global-search {
  width: 100%;
  min-width: 0;
}

.gs-loupe {
  display: none;
}

.gs-box {
  position: relative;
  width: 100%;
  min-width: 0;
}

.gs-back {
  display: none;
}

.gs-input-wrap {
  position: relative;
  min-width: 0;
}

.gs-input {
  min-width: 0;
}

.gs-input :deep(.n-input-wrapper) {
  min-width: 0;
}

.gs-input :deep(.n-input__input-el) {
  min-width: 0;
  width: 100%;
}

.gs-criterion {
  font-size: 0.75rem;
  color: var(--muted, #8a8a8a);
  white-space: nowrap;
}

.gs-filter {
  margin-left: 0.25rem;
}

.gs-menu {
  display: flex;
  flex-direction: column;
  gap: 0.125rem;
  min-width: 10rem;
}

.gs-menu-item {
  justify-content: flex-start;
  text-align: left;
  border-radius: var(--radius-sm);
  padding: 0.375rem 0.625rem;
}

.gs-menu-item--active {
  font-weight: 700;
  background: var(--mock-row);
}

.gs-panel {
  position: absolute;
  top: calc(100% + 0.5rem);
  left: 0;
  right: 0;
  z-index: 70;
  max-height: 60vh;
  overflow-y: auto;
  background: var(--surface-elevated);
  border: 1px solid var(--divide);
  border-radius: var(--radius-md);
  padding: 0.375rem;
}

.gs-footer {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}

.gs-view-all {
  flex-shrink: 0;
  margin-left: auto;
}

.gs-loading {
  padding: 0.75rem;
}

.gs-empty {
  padding: 0.875rem 0.75rem;
  font-size: 0.875rem;
  color: var(--muted, #8a8a8a);
  text-align: center;
}

.gs-item {
  display: flex;
  align-items: center;
  gap: 0.625rem;
  width: 100%;
  min-height: 44px;
  padding: 0.375rem 0.5rem;
  background: transparent;
  border: 0;
  border-radius: var(--radius-sm);
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: pointer;
}

.gs-item:hover,
.gs-item--active {
  background: var(--mock-row);
}

.gs-cover {
  width: 2rem;
  height: 3rem;
  flex-shrink: 0;
  object-fit: cover;
  border-radius: 4px;
  background: var(--surface-muted);
}

.gs-item-text {
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.gs-item-title {
  font-weight: 600;
  font-size: 0.875rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.gs-item-sub {
  font-size: 0.75rem;
  color: var(--muted, #8a8a8a);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.gs-tags-row {
  display: flex;
  gap: 0.375rem;
  overflow-x: auto;
  padding: 0.5rem 0.25rem;
}

.gs-tag--active {
  outline: 2px solid var(--accent-link);
  outline-offset: 1px;
}

.gs-facet-novels {
  border-top: 1px solid var(--divide);
  margin-top: 0.25rem;
  padding-top: 0.25rem;
}

.gs-hint {
  padding: 0.375rem 0.5rem 0.25rem;
  font-size: 0.75rem;
  color: var(--muted, #8a8a8a);
  text-align: center;
  flex: 1;
}

.gs-scrim {
  display: none;
}

/* The placeholder already names the active criterion, so the inline label
   is the first thing to go when the header gets crowded. */
@media (max-width: 900px) {
  .gs-criterion {
    display: none;
  }
}

@media (max-width: 768px) {
  .gs-box {
    display: none;
  }
  .gs-box.gs-mobile-open {
    display: flex;
    align-items: center;
    gap: 0.375rem;
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    z-index: 80;
    /* Same rhythm as the closed header (page-container inset) so the row
       lines up instead of bleeding to the screen edges. */
    padding: 0.5rem 1.25rem;
    background: color-mix(in oklab, var(--surface-elevated) 96%, transparent);
    backdrop-filter: blur(12px);
    border-bottom: 1px solid var(--divide);
  }

  .gs-box.gs-mobile-open .gs-input-wrap {
    flex: 1;
    min-width: 0;
  }

  .gs-box.gs-mobile-open .gs-panel {
    max-height: calc(100dvh - 7rem);
  }

  .gs-back {
    display: inline-flex;
    flex-shrink: 0;
  }

  .gs-loupe {
    display: inline-flex;
  }

  .gs-hint {
    display: none;
  }

  .gs-scrim {
    display: block;
    position: fixed;
    inset: 0;
    z-index: 75;
    background: var(--surface-overlay, rgba(20, 20, 19, 0.44));
  }
}

@media (prefers-reduced-motion: reduce) {
  .global-search * {
    transition: none !important;
  }
}
</style>
