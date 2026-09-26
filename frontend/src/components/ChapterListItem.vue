<template>
  <article
    class="chapter-list-item"
    :class="{ 'chapter-list-item--selected': isSelected }"
  >
    <n-checkbox
      v-if="isOwner"
      :checked="isSelected"
      class="chapter-list-checkbox"
      :aria-label="`Seleccionar capítulo ${chapterPosition(chapter)}`"
      @update:checked="toggleSelected"
    />

    <!-- Plain anchor (not RouterLink): RouterLink composes its own navigate
         handler first, so @click.prevent still ends up navigating. Middle-click
         / open-in-new-tab keeps working via the href. -->
    <a
      :href="`/novels/${chapter.novelId}/chapters/${chapter.id}`"
      class="chapter-list-link"
      :aria-label="`Ver capítulo ${chapterPosition(chapter)}: ${chapter.title}`"
      @click.prevent="emit('open', chapter)"
    >
      <span class="chapter-list-order mono small muted">#{{ String(chapterPosition(chapter)).padStart(2, "0") }}</span>
      <span
        v-if="chapterPosition(chapter) !== chapter.chapterOrder"
        class="chapter-list-source mono small muted"
        :title="`Número de capítulo fuente: ${chapter.chapterOrder}`"
        >Nº {{ chapter.chapterOrder }}</span
      >
      <span class="chapter-list-title line-clamp-2">{{ chapter.title }}</span>
    </a>

    <n-tag
      :type="chapterTagType(resolvedStatus(chapter))"
      size="small"
      round
      class="chapter-list-status"
    >
      {{ chapterStatusLabel(resolvedStatus(chapter)) }}
    </n-tag>

    <div v-if="isOwner" class="chapter-list-item-actions">
      <n-popconfirm @positive-click="emit('delete', { event: $event, chapter })">
        <template #trigger>
          <n-button
            quaternary
            circle
            size="tiny"
            class="chapter-list-action-btn chapter-list-action-btn--delete touch-target"
            aria-label="Excluir"
          >
            <template #icon><n-icon :size="14"><TrashOutline /></n-icon></template>
          </n-button>
        </template>
        ¿Excluir el capítulo "{{ chapter.title }}"? Se conservará y podrás restaurarlo.
      </n-popconfirm>
    </div>
  </article>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { NButton, NCheckbox, NIcon, NPopconfirm, NTag } from "naive-ui";
import { TrashOutline } from "@vicons/ionicons5";
import type { ChapterSummary } from "@/api/types";
import { chapterPosition } from "@/domain";
import { chapterStatusLabel, chapterTagType, resolvedChapterStatus as resolvedStatus } from "@/composables/useChapterStatus";

// `selectionSet` is the shared reactive Set of selected ids. `has(id)` runs in
// THIS component's render, so Vue tracks the specific `id` key on the
// collection: a toggle only re-renders the rows whose id was added/removed.
// The parent must never read the Set in its own template, or it would join
// the reactive graph and re-render every row vnode on each click.
const props = defineProps<{
  chapter: ChapterSummary;
  selectionSet: Set<string>;
  isOwner: boolean;
}>();

const emit = defineEmits<{
  (e: "open", chapter: ChapterSummary): void;
  (e: "delete", payload: { event: Event; chapter: ChapterSummary }): void;
}>();

const isSelected = computed(() => props.selectionSet.has(props.chapter.id));

function toggleSelected(checked: boolean) {
  // Mutate, never reassign: replacing the Set would notify every reader.
  if (checked) props.selectionSet.add(props.chapter.id);
  else props.selectionSet.delete(props.chapter.id);
}
</script>

<style scoped>
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
  min-width: 4.5ch;
  font-size: 0.8125rem;
}

.chapter-list-source {
  flex-shrink: 0;
  font-size: 0.6875rem;
  padding: 0.0625rem 0.375rem;
  border: 1px solid var(--divide);
  border-radius: 999px;
  white-space: nowrap;
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
