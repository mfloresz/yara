<template>
  <a href="#main-content" class="skip-link">Saltar al contenido principal</a>

  <div class="app-shell">
    <AppHeader>
      <template #back-button>
        <slot name="back-button" />
      </template>
    </AppHeader>

    <main id="main-content" class="app-main" tabindex="-1">
      <n-scrollbar ref="scrollbarRef" class="app-scroll" content-class="page-container">
        <slot />
      </n-scrollbar>
    </main>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from "vue";
import { useRoute } from "vue-router";
import { NScrollbar } from "naive-ui";
import AppHeader from "@/components/AppHeader.vue";

const route = useRoute();
const scrollbarRef = ref<InstanceType<typeof NScrollbar> | null>(null);

// Cada página instancia su propio AppLayout, así que al cambiar de vista el
// scrollbar interno arranca en 0. Este watch cubre el reuso del mismo
// componente con otros params (ej. /novels/A → /novels/B).
watch(
  () => route.fullPath,
  () => {
    scrollbarRef.value?.scrollTo({ top: 0 });
  },
);
</script>

<style scoped>
.skip-link {
  position: absolute;
  top: 0;
  left: 0;
  z-index: 10000;
  padding: 0.5rem 1rem;
  background: var(--btn-primary-bg);
  color: var(--btn-primary-fg);
  font-weight: 600;
  border-radius: 0 0 var(--radius-md) 0;
  clip: rect(0, 0, 0, 0);
  clip-path: inset(50%);
  height: 1px;
  overflow: hidden;
  white-space: nowrap;
  width: 1px;
}

.skip-link:focus {
  clip: auto;
  clip-path: none;
  height: auto;
  overflow: visible;
  width: auto;
}
</style>
