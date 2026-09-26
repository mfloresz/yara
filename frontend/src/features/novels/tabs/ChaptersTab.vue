<template>
  <section v-if="active" class="stack-md tab-panel" aria-labelledby="tab-chapters">
    <h2 id="tab-chapters" class="sr-only">Capítulos</h2>
    <ChapterList
      :chapters="chapters"
      :total="total"
      :loading="loading"
      :page="page"
      :page-size="pageSize"
      :selection-set="selectionSet"
      :is-owner="isOwner"
      :gaps="gaps"
      :status-filter="statusFilter"
      :status-counts="statusCounts"
      @update:status-filter="(value) => emit('update:statusFilter', value)"
      @delete="(payload) => emit('delete', payload)"
      @bulk-delete="(event) => emit('bulk-delete', event)"
      @bulk-translate="(event) => emit('bulk-translate', event)"
      @bulk-refine="(event) => emit('bulk-refine', event)"
      @create="emit('create')"
      @import="emit('import')"
      @open="(chapter) => emit('open', chapter)"
      @update:page="(p) => emit('update:page', p)"
    />
  </section>
</template>

<script setup lang="ts">
import ChapterList from "@/components/ChapterList.vue";
import type { ChapterSummary } from "@/api/types";
import type { ChapterGap } from "@/composables/useChapterSummaries";

defineProps<{
  active: boolean;
  chapters: ChapterSummary[];
  total: number;
  loading: boolean;
  page: number;
  pageSize: number;
  selectionSet: Set<string>;
  isOwner: boolean;
  gaps: ChapterGap[];
  statusFilter: string;
  statusCounts: Record<string, number> | null;
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
</script>
