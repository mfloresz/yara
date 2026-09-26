<template>
  <div class="chapter-list">
    <ChapterSelectionToolbar
      :chapters="chapters"
      :gaps="gaps"
      :selection-set="selectionSet"
      :is-owner="isOwner"
      @create="emit('create')"
      @import="emit('import')"
      @bulk-delete="(event) => emit('bulk-delete', event)"
      @bulk-translate="(event) => emit('bulk-translate', event)"
      @bulk-refine="(event) => emit('bulk-refine', event)"
    />

    <div
      v-if="statusCounts"
      class="chapter-list-filters"
      role="group"
      aria-label="Filtrar capítulos por estado"
    >
      <n-button
        v-for="opt in statusFilterOptions"
        :key="opt.value"
        size="small"
        :type="statusFilter === opt.value ? (opt.value === 'failed' ? 'error' : 'primary') : 'default'"
        :secondary="statusFilter !== opt.value"
        :aria-pressed="statusFilter === opt.value"
        @click="emit('update:statusFilter', opt.value)"
      >
        {{ opt.label }} ({{ statusCounts[opt.value] ?? 0 }})
      </n-button>
    </div>

    <n-card v-if="loading && chapters.length === 0" size="small">
      <div class="chapter-list-skeleton">
        <div v-for="i in 8" :key="i" class="chapter-list-skeleton-row">
          <n-skeleton style="width: 1.25rem; height: 1.25rem" :border-radius="4" />
          <n-skeleton style="width: 2rem; height: 0.875rem" :border-radius="4" />
          <n-skeleton style="width: 55%; height: 1rem" :border-radius="4" />
          <n-skeleton style="width: 5rem; height: 1.25rem; border-radius: 999px" />
        </div>
      </div>
    </n-card>

    <n-card v-else-if="chapters.length === 0 && total === 0 && statusFilter !== 'all'" size="small">
      <div class="empty-state">
        <div class="empty-state-icon">
          <n-icon :size="20"><DocumentTextOutline /></n-icon>
        </div>
        <div>
          <h2 class="empty-state-title">Sin capítulos con este estado</h2>
          <p class="muted empty-state-body">Prueba con otro filtro o vuelve a ver todos los capítulos.</p>
        </div>
      </div>
    </n-card>

    <n-card v-else-if="chapters.length === 0 && total === 0" size="small">
      <div class="empty-state">
        <div class="empty-state-icon">
          <n-icon :size="20"><DocumentTextOutline /></n-icon>
        </div>
        <div>
          <h2 class="empty-state-title">Sin capítulos</h2>
          <p class="muted empty-state-body">Crea un capítulo manualmente o importa desde EPUB/TXT/Markdown.</p>
        </div>
        <div v-if="isOwner" class="empty-state-actions">
          <n-button type="primary" @click="emit('create')">
            <template #icon><n-icon><AddOutline /></n-icon></template>
            Nuevo capítulo
          </n-button>
          <n-button secondary @click="emit('import')">
            <template #icon><n-icon><CloudUploadOutline /></n-icon></template>
            Importar
          </n-button>
        </div>
      </div>
    </n-card>

    <div v-else class="chapter-list-items">
      <template v-for="item in mergedItems" :key="item.key">
        <div v-if="item.type === 'gap'" class="chapter-list-gap-row">
          <n-icon :size="14" class="chapter-list-gap-icon"><WarningOutline /></n-icon>
          <span class="chapter-list-gap-text">
            {{ item.gap.count === 1 ? 'Falta 1 capítulo' : `Faltan ${item.gap.count} capítulos` }}
          </span>
        </div>
        <ChapterListItem
          v-else
          :chapter="item.chapter"
          :selection-set="selectionSet"
          :is-owner="isOwner"
          @open="(chapter) => emit('open', chapter)"
          @delete="(payload) => emit('delete', payload)"
        />
      </template>
    </div>

    <div class="chapter-list-footer">
      <span class="small muted">
        Mostrando {{ footerFrom }}-{{ footerTo }} de {{ total }} capítulos
      </span>
      <n-pagination
        v-if="total > 0"
        :page="page + 1"
        :page-count="pageCount"
        :page-slot="7"
        size="small"
        @update:page="emit('update:page', $event - 1)"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { NButton, NCard, NIcon, NPagination, NSkeleton } from "naive-ui";
import {
  AddOutline,
  CloudUploadOutline,
  DocumentTextOutline,
  WarningOutline,
} from "@vicons/ionicons5";
import ChapterListItem from "@/components/ChapterListItem.vue";
import ChapterSelectionToolbar from "@/components/ChapterSelectionToolbar.vue";
import type { ChapterSummary } from "@/api/types";
import { chapterPosition } from "@/domain";
import { chapterStatusLabel } from "@/composables/useChapterStatus";

// This component deliberately never reads `selectionSet` beyond forwarding it:
// selection lives in a reactive Set consumed by ChapterListItem (rows) and
// ChapterSelectionToolbar (counts). Reading it here — even for a `v-if` —
// would pull the whole list into the Set's reactive graph and rebuild every
// row vnode on each toggle.
const props = defineProps<{
  chapters: ChapterSummary[];
  total: number;
  loading: boolean;
  page: number;
  pageSize: number;
  selectionSet: Set<string>;
  isOwner: boolean;
  gaps?: Array<{ from: number; to: number; count: number }>;
  statusFilter?: string;
  statusCounts?: Record<string, number> | null;
}>();

const emit = defineEmits<{
  (e: "update:page", page: number): void;
  (e: "update:statusFilter", value: string): void;
  (e: "delete", payload: { event: Event; chapter: ChapterSummary }): void;
  (e: "bulk-delete", event: Event): void;
  (e: "bulk-translate", event: Event): void;
  (e: "bulk-refine", event: Event): void;
  (e: "create"): void;
  (e: "import"): void;
  (e: "open", chapter: ChapterSummary): void;
}>();

const FILTERABLE_STATUSES = ["pending", "processing", "translated", "refined", "done", "failed"] as const;

const statusFilterOptions = computed(() => {
  if (!props.statusCounts) return [];
  return [
    { value: "all" as const, label: "Todos" },
    ...FILTERABLE_STATUSES.map((value) => ({ value, label: chapterStatusLabel(value) })),
  ];
});

const pageCount = computed(() => Math.max(1, Math.ceil(props.total / props.pageSize)));

// The filtered view renders the whole matching subset at once (page=0), so
// the footer must not pretend there is a 50-per-page window there.
const footerFrom = computed(() => {
  if (props.statusFilter && props.statusFilter !== "all") return props.total === 0 ? 0 : 1;
  return props.total === 0 ? 0 : props.page * props.pageSize + 1;
});
const footerTo = computed(() => {
  if (props.statusFilter && props.statusFilter !== "all") return props.total;
  return Math.min((props.page + 1) * props.pageSize, props.total);
});

/**
 * Reading order (position) vs source numbering (chapterOrder) diverge as soon
 * as the user reorders or inserts a late import mid-list. Source gaps only
 * make sense in source-number space, so inline gap rows are rendered only
 * when both numberings agree on this page. Otherwise the global badge plus
 * the exact missing source ranges in the toolbar cover it, with no phantom rows.
 */
const isDiverged = computed(() => props.chapters.some((c) => chapterPosition(c) !== c.chapterOrder));

type MergedItem =
  | { type: "chapter"; chapter: ChapterSummary; key: string }
  | { type: "gap"; gap: { from: number; to: number; count: number }; key: string };

const mergedItems = computed<MergedItem[]>(() => {
  const chaptersOnly = () =>
    props.chapters.map((ch) => ({ type: "chapter", chapter: ch, key: ch.id }) as MergedItem);
  if (!props.gaps || props.gaps.length === 0 || isDiverged.value) {
    return chaptersOnly();
  }
  // Here reading order == source order, so the page items arrive sorted by
  // chapterOrder. Only backend gaps (computed over the full table) are
  // rendered — never invented from the page slice. The cursor starts at the
  // first item so later pages don't re-print earlier gaps, and gaps
  // straddling the page start are clamped to the visible remainder.
  const items: MergedItem[] = [];
  const sortedGaps = [...props.gaps].sort((a, b) => a.from - b.from);
  const firstOrder = props.chapters[0]?.chapterOrder ?? 1;
  let lastOrder = firstOrder - 1;

  for (const chapter of props.chapters) {
    for (const gap of sortedGaps) {
      if (gap.to < lastOrder + 1) continue;
      if (gap.from <= chapter.chapterOrder) {
        const from = Math.max(gap.from, lastOrder + 1, firstOrder);
        if (from <= gap.to) {
          items.push({
            type: "gap",
            gap: { from, to: gap.to, count: gap.to - from + 1 },
            key: `gap-${gap.from}`,
          });
          lastOrder = gap.to;
        }
      }
    }
    items.push({ type: "chapter", chapter, key: chapter.id });
    lastOrder = Math.max(lastOrder, chapter.chapterOrder);
  }

  return items;
});
</script>

<style scoped>
.chapter-list {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.chapter-list-filters {
  display: flex;
  align-items: center;
  gap: 0.375rem;
  flex-wrap: wrap;
}

.chapter-list-items {
  border: 1px solid var(--divide);
  border-radius: var(--radius-md);
  background: var(--surface-base);
  overflow: hidden;
}

.chapter-list-gap-row {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  padding: 0.5rem 0.75rem;
  background: color-mix(in oklab, #a16207 5%, var(--surface-base));
  border-bottom: 1px solid var(--divide);
  font-size: 0.8125rem;
  color: #a16207;
}

.chapter-list-gap-icon {
  color: #a16207;
}

.chapter-list-gap-text {
  display: inline-flex;
  align-items: center;
  gap: 0.375rem;
}

.chapter-list-skeleton-row {
  display: grid;
  grid-template-columns: auto auto 1fr auto;
  align-items: center;
  gap: 0.375rem;
  padding: 0.25rem 0.5rem;
  border-bottom: 1px solid var(--divide);
}

.chapter-list-skeleton-row:last-child {
  border-bottom: none;
}

.chapter-list-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  flex-wrap: wrap;
  padding-top: 0.25rem;
}

.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
  gap: 0.75rem;
  padding: 1.5rem 1rem;
}

.empty-state-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 2.5rem;
  height: 2.5rem;
  border-radius: var(--radius-md);
  background: var(--surface-muted);
  color: var(--text-secondary);
}

.empty-state-title {
  margin: 0 0 0.25rem;
  font-size: 1rem;
}

.empty-state-body {
  margin: 0;
  max-width: 48ch;
  font-size: 0.875rem;
}

.empty-state-actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: center;
  gap: 0.5rem;
}

@media (max-width: 640px) {
  .chapter-list-skeleton-row {
    grid-template-columns: auto 1fr auto;
    gap: 0.5rem;
    padding: 0.5rem;
  }
}
</style>
