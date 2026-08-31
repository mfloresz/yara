<template>
  <section v-if="active" class="stack-md tab-panel" aria-labelledby="tab-chapters">
    <h2 id="tab-chapters" class="sr-only">Capítulos</h2>
    <ChapterList
      :chapters="chapters"
      :total="total"
      :loading="loading"
      :page="page"
      :page-size="pageSize"
      :selected="selected"
      :is-owner="isOwner"
      :gaps="gaps"
      @update:selected="(s) => emit('update:selected', s)"
      @delete="(payload) => emit('delete', payload)"
      @bulk-delete="(event) => emit('bulk-delete', event)"
      @create="emit('create')"
      @import="emit('import')"
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
  selected: ChapterSummary[];
  isOwner: boolean;
  gaps: ChapterGap[];
}>();

const emit = defineEmits<{
  (e: "update:page", page: number): void;
  (e: "update:selected", selected: ChapterSummary[]): void;
  (e: "delete", payload: { event: Event; chapter: ChapterSummary }): void;
  (e: "bulk-delete", event: Event): void;
  (e: "create"): void;
  (e: "import"): void;
}>();
</script>
