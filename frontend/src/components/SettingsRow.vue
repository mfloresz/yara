<template>
  <div class="settings-row">
    <div class="settings-row-text">
      <div class="settings-row-title">{{ title }}</div>
      <div v-if="description" class="small muted">{{ description }}</div>
    </div>
    <div class="settings-row-control">
      <slot />
    </div>
  </div>
</template>

<script setup lang="ts">
withDefaults(
  defineProps<{
    title: string;
    description?: string;
  }>(),
  { description: undefined },
);
</script>

<style scoped>
.settings-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  min-height: 56px;
  padding: 0.375rem 0;
}

.settings-row-text {
  flex: 1;
  min-width: 0;
}

.settings-row-title {
  font-weight: 600;
  font-size: 0.9375rem;
}

.settings-row-control {
  flex-shrink: 0;
  display: flex;
  align-items: center;
}
</style>
