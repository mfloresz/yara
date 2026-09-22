<template>
  <a href="#main-content" class="skip-link">Saltar al contenido principal</a>

  <div class="app-shell">
    <AppHeader>
      <template #back-button>
        <slot name="back-button" />
      </template>
    </AppHeader>

    <main id="main-content" class="page-container" tabindex="-1">
      <slot />
    </main>
  </div>
</template>

<script setup lang="ts">
import AppHeader from "@/components/AppHeader.vue";
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
