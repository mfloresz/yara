<template>
  <article v-for="i in count" :key="i" class="library-card" aria-hidden="true">
    <n-skeleton class="library-cover-skeleton" :border-radius="16" />
    <n-skeleton text style="width: 80%; height: 0.875rem" :border-radius="6" />
    <n-skeleton text style="width: 40%; height: 0.75rem" :border-radius="6" />
  </article>
</template>

<script setup lang="ts">
import { NSkeleton } from "naive-ui";

withDefaults(defineProps<{ count?: number }>(), { count: 12 });
</script>

<style scoped>
.library-cover-skeleton {
  aspect-ratio: 2 / 3;
}
</style>
