<template>
  <div class="chapter-list">
    <div class="chapter-list-toolbar">
      <div class="chapter-list-selection">
        <n-button size="small" quaternary @click="selectAll">Todos</n-button>
        <n-button size="small" quaternary @click="clearSelection">Ninguno</n-button>
        <span v-if="totalMissingChapters > 0" class="chapter-list-gap-badge">
          <n-icon :size="14"><WarningOutline /></n-icon>
          {{ totalMissingChapters === 1 ? 'Falta 1 capítulo' : `Faltan ${totalMissingChapters} capítulos` }}
        </span>
        <span v-if="selected.length > 0" class="small muted">{{ selected.length }} seleccionados</span>
      </div>
      <div v-if="isOwner" class="chapter-list-actions">
        <n-button
          v-if="selected.length > 1"
          size="small"
          type="error"
          secondary
          @click="emit('bulk-delete', $event)"
        >
          <template #icon><n-icon><TrashOutline /></n-icon></template>
          Eliminar {{ selected.length }}
        </n-button>
        <n-button size="small" secondary @click="emit('import')">
          <template #icon><n-icon><CloudUploadOutline /></n-icon></template>
          Importar
        </n-button>
        <n-button size="small" type="primary" @click="emit('create')">
          <template #icon><n-icon><AddOutline /></n-icon></template>
          Nuevo
        </n-button>
      </div>
    </div>

    <n-card v-if="loading && chapters.length === 0" size="small">
      <div class="chapter-list-skeleton">
        <div v-for="i in 8" :key="i" class="chapter-list-item chapter-list-item--skeleton">
          <n-skeleton style="width: 1.25rem; height: 1.25rem" :border-radius="4" />
          <n-skeleton style="width: 2rem; height: 0.875rem" :border-radius="4" />
          <n-skeleton style="width: 55%; height: 1rem" :border-radius="4" />
          <n-skeleton style="width: 5rem; height: 1.25rem; border-radius: 999px" />
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
        <article
          v-else
          class="chapter-list-item"
          :class="{ 'chapter-list-item--selected': isSelected(item.chapter) }"
        >
          <n-checkbox
            v-if="isOwner"
            :checked="isSelected(item.chapter)"
            class="chapter-list-checkbox"
            :aria-label="`Seleccionar capítulo ${item.chapter.chapterOrder}`"
            @update:checked="toggleSelected(item.chapter, $event)"
          />

          <RouterLink
            :to="`/novels/${item.chapter.novelId}/chapters/${item.chapter.id}`"
            class="chapter-list-link"
            :aria-label="`Editar capítulo ${item.chapter.chapterOrder}: ${item.chapter.title}`"
          >
            <span class="chapter-list-order mono small muted">#{{ String(item.chapter.chapterOrder).padStart(2, "0") }}</span>
            <span class="chapter-list-title line-clamp-2">{{ item.chapter.title }}</span>
          </RouterLink>

          <n-tag
            :type="chapterTagType(resolvedStatus(item.chapter))"
            size="small"
            round
            class="chapter-list-status"
          >
            {{ chapterStatusLabel(resolvedStatus(item.chapter)) }}
          </n-tag>

          <div v-if="isOwner" class="chapter-list-item-actions">
            <n-button
              quaternary
              circle
              size="tiny"
              class="chapter-list-action-btn chapter-list-action-btn--delete touch-target"
              aria-label="Eliminar"
              @click="emit('delete', { event: $event, chapter: item.chapter })"
            >
              <template #icon><n-icon :size="14"><TrashOutline /></n-icon></template>
            </n-button>
          </div>
        </article>
      </template>
    </div>

    <div class="chapter-list-footer">
      <span class="small muted">
        Mostrando {{ total === 0 ? 0 : page * pageSize + 1 }}-{{ Math.min((page + 1) * pageSize, total) }} de {{ total }} capítulos
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
import { RouterLink } from "vue-router";
import { NButton, NCard, NCheckbox, NPagination, NSkeleton, NTag, NIcon } from "naive-ui";
import {
  TrashOutline,
  CloudUploadOutline,
  AddOutline,
  WarningOutline,
  DocumentTextOutline,
} from "@vicons/ionicons5";
import type { ChapterSummary } from "@/api/types";
import type { Chapter } from "@/domain";

const props = defineProps<{
  chapters: ChapterSummary[];
  total: number;
  loading: boolean;
  page: number;
  pageSize: number;
  selected: ChapterSummary[];
  isOwner: boolean;
  gaps?: Array<{ from: number; to: number; count: number }>;
}>();

const emit = defineEmits<{
  (e: "update:page", page: number): void;
  (e: "update:selected", selected: ChapterSummary[]): void;
  (e: "delete", payload: { event: Event; chapter: ChapterSummary }): void;
  (e: "bulk-delete", event: Event): void;
  (e: "create"): void;
  (e: "import"): void;
}>();

const selectedIds = computed(() => new Set(props.selected.map((item) => item.id)));

const totalMissingChapters = computed(() => {
  if (!props.gaps || props.gaps.length === 0) return 0;
  return props.gaps.reduce((acc, g) => acc + g.count, 0);
});

const pageCount = computed(() => Math.max(1, Math.ceil(props.total / props.pageSize)));

type MergedItem =
  | { type: "chapter"; chapter: ChapterSummary; key: string }
  | { type: "gap"; gap: { from: number; to: number; count: number }; key: string };

const mergedItems = computed<MergedItem[]>(() => {
  if (!props.gaps || props.gaps.length === 0) {
    return props.chapters.map((ch) => ({ type: "chapter", chapter: ch, key: ch.id }));
  }
  const items: MergedItem[] = [];
  const sortedGaps = [...props.gaps].sort((a, b) => a.from - b.from);
  let lastOrder = 0;

  for (const chapter of props.chapters) {
    for (const gap of sortedGaps) {
      if (gap.from >= lastOrder + 1 && gap.from <= chapter.chapterOrder) {
        items.push({ type: "gap", gap, key: `gap-${gap.from}` });
        lastOrder = gap.to;
      }
    }
    if (chapter.chapterOrder > lastOrder + 1) {
      items.push({
        type: "gap",
        gap: { from: lastOrder + 1, to: chapter.chapterOrder - 1, count: chapter.chapterOrder - lastOrder - 1 },
        key: `gap-${lastOrder + 1}`,
      });
    }
    items.push({ type: "chapter", chapter, key: chapter.id });
    lastOrder = chapter.chapterOrder;
  }

  return items;
});

function resolvedStatus(chapter: ChapterSummary): Chapter["status"] {
  if (chapter.status === "processing") return "processing";
  return chapter.status;
}

function chapterStatusLabel(status: Chapter["status"]) {
  return {
    pending: "Pendiente",
    processing: "Procesando",
    translated: "Traducido",
    refined: "Refinado",
    done: "Completado",
    failed: "Error",
  }[status] || status;
}

function chapterTagType(status: Chapter["status"]) {
  return ({
    pending: "default",
    processing: "warning",
    translated: "success",
    refined: "info",
    done: "success",
    failed: "error",
  }[status] || "default") as "default" | "info" | "warning" | "success" | "error";
}

function isSelected(chapter: ChapterSummary) {
  return selectedIds.value.has(chapter.id);
}

function toggleSelected(chapter: ChapterSummary, checked: boolean) {
  const next = new Set(selectedIds.value);
  if (checked) next.add(chapter.id);
  else next.delete(chapter.id);
  emit("update:selected", props.chapters.filter((item) => next.has(item.id)));
}

function selectAll() {
  emit("update:selected", [...props.chapters]);
}

function clearSelection() {
  emit("update:selected", []);
}
</script>

<style scoped>
.chapter-list {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.chapter-list-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  flex-wrap: wrap;
}

.chapter-list-selection {
  display: flex;
  align-items: center;
  gap: 0.25rem;
}

.chapter-list-actions {
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

.chapter-list-gap-badge {
  display: inline-flex;
  align-items: center;
  gap: 0.375rem;
  padding: 0.25rem 0.625rem;
  border-radius: var(--radius-md);
  background: color-mix(in oklab, #a16207 10%, transparent);
  color: #a16207;
  font-size: 0.75rem;
  font-weight: 500;
  white-space: nowrap;
}

.chapter-list-gap-text {
  display: inline-flex;
  align-items: center;
  gap: 0.375rem;
}

.chapter-list-item {
  display: grid;
  grid-template-columns: auto 1fr auto auto;
  align-items: center;
  gap: 0.375rem;
  padding: 0.25rem 0.5rem;
  border-bottom: 1px solid var(--divide);
  transition: background 0.12s ease;
  font-size: 0.875rem;
}

.chapter-list-item:last-child {
  border-bottom: none;
}

.chapter-list-item:hover,
.chapter-list-item--selected {
  background: var(--mock-row);
}

.chapter-list-item--skeleton {
  grid-template-columns: auto auto 1fr auto;
}

.chapter-list-link {
  display: flex;
  align-items: center;
  gap: 0.375rem;
  min-width: 0;
  color: var(--foreground);
  border-radius: var(--radius-sm);
}

.chapter-list-link:hover {
  color: var(--accent-link);
}

.chapter-list-order {
  font-variant-numeric: tabular-nums;
  flex-shrink: 0;
  width: 1.75rem;
  font-size: 0.8125rem;
}

.chapter-list-title {
  font-weight: 500;
  min-width: 0;
}

.chapter-list-status {
  flex-shrink: 0;
  font-size: 0.6875rem;
}

.chapter-list-item-actions {
  display: flex;
  align-items: center;
  gap: 0;
  flex-shrink: 0;
}

.chapter-list-action-btn--delete {
  color: #dc2626 !important;
}

.chapter-list-action-btn--delete:hover {
  background: color-mix(in oklab, #dc2626 10%, transparent) !important;
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
  .chapter-list-item {
    grid-template-columns: auto 1fr auto;
    gap: 0.5rem;
    padding: 0.5rem;
  }

  .chapter-list-status {
    grid-column: 3;
    grid-row: 1;
  }

  .chapter-list-item-actions {
    grid-column: 1 / -1;
    justify-content: flex-end;
  }

  .chapter-list-link {
    flex-direction: row;
    align-items: center;
    gap: 0.375rem;
    min-width: 0;
  }

  .chapter-list-order {
    display: none;
  }

  .chapter-list-title {
    display: block;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
}
</style>
