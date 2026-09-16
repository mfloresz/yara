<template>
  <section :id="id" class="settings-section" :aria-labelledby="`${id}-title`">
    <div class="settings-section-head">
      <h2 :id="`${id}-title`">{{ title }}</h2>
      <p v-if="description" class="small muted settings-section-desc">{{ description }}</p>
    </div>
    <n-card size="small" :class="{ 'settings-section-card--danger': danger }">
      <slot />
    </n-card>
  </section>
</template>

<script setup lang="ts">
import { NCard } from "naive-ui";

withDefaults(
  defineProps<{
    id: string;
    title: string;
    description?: string;
    danger?: boolean;
  }>(),
  { description: undefined, danger: false },
);
</script>

<style scoped>
.settings-section {
  scroll-margin-top: 148px;
}

.settings-section-head {
  margin-bottom: 0.625rem;
}

.settings-section-head h2 {
  margin: 0;
  font-size: 1.0625rem;
  font-weight: 700;
  letter-spacing: -0.01em;
}

.settings-section-desc {
  margin: 0.25rem 0 0;
}
</style>

<style>
.settings-section-card--danger {
  border-color: color-mix(in oklab, var(--danger) 45%, transparent) !important;
  background: var(--surface-muted) !important;
}
</style>
