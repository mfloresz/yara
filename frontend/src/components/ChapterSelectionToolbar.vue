<template>
  <div class="chapter-list-toolbar">
    <div class="chapter-list-selection">
      <n-button size="small" quaternary @click="selectAll">Todos</n-button>
      <n-button size="small" quaternary @click="clearSelection">Ninguno</n-button>
      <span v-if="totalMissingChapters > 0" class="chapter-list-gap-badge">
        <n-icon :size="14"><WarningOutline /></n-icon>
        {{ totalMissingChapters === 1 ? 'Falta 1 capítulo' : `Faltan ${totalMissingChapters} capítulos` }}
      </span>
      <span v-if="showSourceGaps" class="small muted" :title="missingSourceRangesTitle">
        {{ missingSourceRanges }}
      </span>
      <span v-if="counts.selected > 0" class="small muted">{{ counts.selected }} seleccionados</span>
    </div>
    <div v-if="isOwner" class="chapter-list-actions">
      <n-button
        v-if="counts.selected > 0"
        size="small"
        type="primary"
        secondary
        :disabled="counts.translatable === 0"
        @click="emit('bulk-translate', $event)"
      >
        <template #icon><n-icon><PlayOutline /></n-icon></template>
        Traducir {{ counts.translatable }}
      </n-button>
      <n-button
        v-if="counts.selected > 0"
        size="small"
        type="info"
        secondary
        :disabled="counts.refinable === 0"
        @click="emit('bulk-refine', $event)"
      >
        <template #icon><n-icon><BrushOutline /></n-icon></template>
        Refinar {{ counts.refinable }}
      </n-button>
      <n-button
        v-if="counts.selected > 1"
        size="small"
        type="error"
        secondary
        @click="emit('bulk-delete', $event)"
      >
        <template #icon><n-icon><TrashOutline /></n-icon></template>
        Excluir {{ counts.selected }}
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
</template>

<script setup lang="ts">
import { computed } from "vue";
import { NButton, NIcon } from "naive-ui";
import {
  AddOutline,
  BrushOutline,
  CloudUploadOutline,
  PlayOutline,
  TrashOutline,
  WarningOutline,
} from "@vicons/ionicons5";
import type { ChapterSummary } from "@/api/types";
import type { ChapterGap } from "@/composables/useChapterSummaries";
import { chapterPosition } from "@/domain";
import { resolvedChapterStatus as resolvedStatus } from "@/composables/useChapterStatus";

// Isolated from the chapter list on purpose: this component iterates the
// selection Set (via has() over `chapters`), so it re-renders on every
// selection mutation. It is small enough that this is cheap — but if it grew,
// it must not be merged back into ChapterList, or the whole list would join
// the Set's reactive graph and every row vnode would be rebuilt per click.
const props = defineProps<{
  chapters: ChapterSummary[];
  gaps?: ChapterGap[];
  selectionSet: Set<string>;
  isOwner: boolean;
}>();

const emit = defineEmits<{
  (e: "bulk-delete", event: Event): void;
  (e: "bulk-translate", event: Event): void;
  (e: "bulk-refine", event: Event): void;
  (e: "create"): void;
  (e: "import"): void;
}>();

// Mirrors the eligibility rules in useTranslateSelection so the contextual
// actions only count chapters the job would actually pick up.
const counts = computed(() => {
  let selected = 0;
  let translatable = 0;
  let refinable = 0;
  for (const chapter of props.chapters) {
    if (!props.selectionSet.has(chapter.id)) continue;
    selected++;
    const status = resolvedStatus(chapter);
    if (chapter.hasOriginalContent && (status === "pending" || status === "failed")) translatable++;
    if (chapter.hasTranslatedContent && (status === "translated" || status === "failed")) refinable++;
  }
  return { selected, translatable, refinable };
});

function selectAll() {
  // add() on an already-present id is a no-op that triggers no effects.
  for (const chapter of props.chapters) props.selectionSet.add(chapter.id);
}

function clearSelection() {
  props.selectionSet.clear();
}

const totalMissingChapters = computed(() => {
  if (!props.gaps || props.gaps.length === 0) return 0;
  return props.gaps.reduce((acc, g) => acc + g.count, 0);
});

/**
 * Source gaps only make sense in source-number space; they are shown when
 * reading order (position) has diverged from source numbering (chapterOrder).
 */
const isDiverged = computed(() => props.chapters.some((c) => chapterPosition(c) !== c.chapterOrder));

const showSourceGaps = computed(
  () => isDiverged.value && !!props.gaps && props.gaps.length > 0,
);

const missingSourceRanges = computed(() =>
  [...(props.gaps ?? [])]
    .sort((a, b) => a.from - b.from)
    .map((g) => (g.from === g.to ? `Nº ${g.from}` : `Nº ${g.from}–${g.to}`))
    .join(" · "),
);

const missingSourceRangesTitle = computed(
  () => `Números de capítulo fuente ausentes: ${missingSourceRanges.value}`,
);
</script>

<style scoped>
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
</style>
