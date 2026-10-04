<template>
  <div class="clean-preview-list">
    <div
      v-for="item in display"
      :key="item.chapterId"
      class="clean-preview-item"
    >
      <div class="row-between" style="margin-bottom: 0.75rem">
        <span class="small muted">#{{ item.chapterOrder }} · {{ item.chapterTitle }}</span>
        <n-tag size="small" round type="warning">−{{ item.removedLines }} líneas</n-tag>
      </div>
      <div v-if="item.changes.length === 0" class="small muted">Sin cambios de líneas.</div>
      <div v-else class="clean-preview-hunks">
        <div
          v-for="(hunk, hunkIndex) in item.changes"
          :key="hunkIndex"
          class="clean-preview-hunk"
        >
          <div
            v-for="(line, lineIndex) in hunk.before"
            :key="`b-${hunkIndex}-${lineIndex}`"
            class="clean-diff-line clean-diff-before"
          >− {{ line }}</div>
          <div v-if="hunk.beforeHidden > 0" class="small muted" style="padding: 0.25rem 0.75rem">… y {{ hunk.beforeHidden }} líneas eliminadas más</div>
          <div
            v-for="(line, lineIndex) in hunk.after"
            :key="`a-${hunkIndex}-${lineIndex}`"
            class="clean-diff-line clean-diff-after"
          >+ {{ line }}</div>
          <div v-if="hunk.afterHidden > 0" class="small muted" style="padding: 0.25rem 0.75rem">… y {{ hunk.afterHidden }} líneas añadidas más</div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { NTag } from "naive-ui";
import type { CleanPreviewItem } from "@/api/types";

// Caps how many diff lines render per hunk side, so one pathological hunk
// cannot turn the preview into an unbounded wall of text.
const MAX_LINES_PER_HUNK = 40;

const props = defineProps<{
  items: CleanPreviewItem[];
}>();

const display = computed(() =>
  props.items.map((item) => ({
    chapterId: item.chapterId,
    chapterOrder: item.chapterOrder,
    chapterTitle: item.chapterTitle,
    removedLines: item.removedLines,
    changes: item.changes.map((hunk) => {
      const before = hunk.before ?? [];
      const after = hunk.after ?? [];
      return {
        before: before.length > MAX_LINES_PER_HUNK ? before.slice(0, MAX_LINES_PER_HUNK) : before,
        after: after.length > MAX_LINES_PER_HUNK ? after.slice(0, MAX_LINES_PER_HUNK) : after,
        beforeHidden: Math.max(0, before.length - MAX_LINES_PER_HUNK),
        afterHidden: Math.max(0, after.length - MAX_LINES_PER_HUNK),
      };
    }),
  })),
);
</script>

<style scoped>
.clean-preview-list {
  display: flex;
  flex-direction: column;
  gap: 1rem;
  max-height: 62vh;
  overflow: auto;
}

.clean-preview-item {
  border: 1px solid var(--divide);
  border-radius: 12px;
  padding: 0.875rem 1rem;
}

.clean-preview-hunks {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.clean-preview-hunk {
  border: 1px solid var(--divide);
  border-radius: 8px;
  overflow: hidden;
}

.clean-diff-line {
  font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, monospace);
  font-size: 0.8125rem;
  line-height: 1.45;
  padding: 0.125rem 0.75rem;
  white-space: pre-wrap;
  word-break: break-word;
}

.clean-diff-before {
  color: color-mix(in oklab, var(--danger) 82%, var(--text-primary));
  background: color-mix(in oklab, var(--danger) 9%, transparent);
}

.clean-diff-after {
  color: color-mix(in oklab, var(--success) 82%, var(--text-primary));
  background: color-mix(in oklab, var(--success) 9%, transparent);
}
</style>
