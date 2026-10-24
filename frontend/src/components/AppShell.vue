<template>
  <a href="#main-content" class="skip-link">Saltar al contenido principal</a>

  <div class="shell" :class="{ 'shell--rail': rail }">
    <aside class="shell-sidebar" aria-label="Navegación principal">
      <RouterLink to="/" class="shell-brand" aria-label="Yara - Inicio">
        <span class="shell-brand-icon" aria-hidden="true">
          <img src="/favicon.svg" alt="" width="26" height="26" />
        </span>
        <span v-if="!rail" class="shell-brand-text">Yara</span>
      </RouterLink>

      <nav class="shell-nav">
        <RouterLink
          v-for="section in topSections"
          :key="section.to"
          :to="section.to"
          class="shell-nav-item"
          :class="{ 'is-active': isActive(section) }"
          :title="section.label"
        >
          <n-icon :size="21"><component :is="section.icon" /></n-icon>
          <span v-if="!rail" class="shell-nav-label">{{ section.label }}</span>
        </RouterLink>

        <span class="shell-nav-divider" aria-hidden="true" />

        <RouterLink
          v-for="section in bottomSections"
          :key="section.to"
          :to="section.to"
          class="shell-nav-item"
          :class="{ 'is-active': isActive(section) }"
          :title="section.label"
        >
          <n-icon :size="21"><component :is="section.icon" /></n-icon>
          <span v-if="!rail" class="shell-nav-label">{{ section.label }}</span>
          <span v-if="section.to === '/operations' && hasActive" class="shell-nav-dot" />
        </RouterLink>
      </nav>

      <div class="shell-sidebar-foot">
        <button
          class="shell-rail-toggle touch-target"
          :aria-label="rail ? 'Expandir barra lateral' : 'Colapsar barra lateral'"
          :title="rail ? 'Expandir' : 'Colapsar'"
          @click="toggleRail"
        >
          <n-icon :size="18"><component :is="rail ? ChevronForwardOutline : ChevronBackOutline" /></n-icon>
        </button>
        <div v-if="!rail" class="shell-version">{{ serverVersion ? `Yara ${serverVersion}` : "Yara" }}</div>
      </div>
    </aside>

    <div class="shell-body">
      <AppHeader />
      <main id="main-content" class="app-main" tabindex="-1">
        <n-scrollbar ref="scrollbarRef" class="app-scroll" content-class="page-container">
          <slot />
        </n-scrollbar>
      </main>
    </div>

    <nav class="shell-tabs" aria-label="Navegación principal">
      <RouterLink
        v-for="section in sections"
        :key="section.to"
        :to="section.to"
        class="shell-tab touch-target"
        :class="{ 'is-active': isActive(section) }"
        :aria-label="section.label"
      >
        <n-icon :size="22"><component :is="section.icon" /></n-icon>
        <span class="shell-tab-label">{{ section.short || section.label }}</span>
        <span v-if="section.to === '/operations' && hasActive" class="shell-nav-dot" />
      </RouterLink>
    </nav>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch, type Component } from "vue";
import { RouterLink, useRoute } from "vue-router";
import { NIcon, NScrollbar } from "naive-ui";
import {
  HomeOutline,
  ImagesOutline,
  BookOutline,
  FlashOutline,
  ChatbubbleEllipsesOutline,
  SettingsOutline,
  ShieldOutline,
  ChevronBackOutline,
  ChevronForwardOutline,
} from "@vicons/ionicons5";
import AppHeader from "@/components/AppHeader.vue";
import { useActiveJobStatus } from "@/composables/useActiveJobStatus";
import { useServerVersion } from "@/composables/useServerVersion";
import { useAppServices } from "@/app/services";

const route = useRoute();
const scrollbarRef = ref<InstanceType<typeof NScrollbar> | null>(null);
const { hasActive } = useActiveJobStatus();
const { version: serverVersion } = useServerVersion();
const { auth } = useAppServices();

const RAIL_KEY = "yara-shell-rail";
const rail = ref(sessionStorage.getItem(RAIL_KEY) === "1");

function toggleRail() {
  rail.value = !rail.value;
  sessionStorage.setItem(RAIL_KEY, rail.value ? "1" : "0");
}

interface ShellSection {
  to: string;
  label: string;
  short?: string;
  icon: Component;
}

// Secciones a lo scancore: cada medio con su propia entrada, y un divisor
// antes de las utilidades.
const topSections = computed<ShellSection[]>(() => [
  { to: "/", label: "Inicio", short: "Inicio", icon: HomeOutline },
  { to: "/comics", label: "Cómics", short: "Cómics", icon: ImagesOutline },
  { to: "/novels", label: "Novelas", short: "Novelas", icon: BookOutline },
]);

const bottomSections = computed<ShellSection[]>(() => {
  const list: ShellSection[] = [
    { to: "/operations", label: "Operaciones", short: "Oper.", icon: FlashOutline },
    { to: "/chat", label: "Asistente", short: "Chat", icon: ChatbubbleEllipsesOutline },
    { to: "/settings", label: "Ajustes", short: "Ajustes", icon: SettingsOutline },
  ];
  if (auth.isAdmin.value) {
    list.push({ to: "/admin", label: "Administración", short: "Admin", icon: ShieldOutline });
  }
  return list;
});

const sections = computed(() => [...topSections.value, ...bottomSections.value]);

// Resalta la sección dueña de la ruta actual: /novels/abc enciende "Novelas".
// La raíz se compara exacta para no marcar siempre Inicio.
function isActive(section: ShellSection) {
  if (section.to === "/") return route.path === "/";
  return route.path === section.to || route.path.startsWith(`${section.to}/`);
}

// Cada navegación arranca el scroll del área de contenido en 0 (antes lo
// hacía el AppLayout por página).
watch(
  () => route.fullPath,
  () => {
    scrollbarRef.value?.scrollTo({ top: 0 });
  },
  { flush: "post" },
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

.shell {
  display: flex;
  height: 100dvh;
  background: var(--page-bg);
  color: var(--foreground);
}

/* --- Sidebar (desktop) --- */
.shell-sidebar {
  display: flex;
  flex-direction: column;
  width: 216px;
  flex-shrink: 0;
  background: var(--surface-elevated);
  border-right: 1px solid var(--divide);
  padding: 0.75rem 0.625rem;
  gap: 0.75rem;
  transition: width 0.15s ease;
}

.shell--rail .shell-sidebar {
  width: 64px;
}

.shell-brand {
  display: flex;
  align-items: center;
  gap: 0.625rem;
  padding: 0.375rem 0.5rem;
  border-radius: var(--radius-md);
  color: var(--foreground);
  font-weight: 700;
  font-size: 1.0625rem;
}

.shell-brand:hover {
  background: var(--mock-row);
}

.shell--rail .shell-brand {
  justify-content: center;
}

.shell-brand-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 1.75rem;
  height: 1.75rem;
  border-radius: var(--radius-sm);
  overflow: hidden;
  flex-shrink: 0;
}

.shell-brand-icon img {
  width: 100%;
  height: 100%;
}

.shell-nav {
  display: flex;
  flex-direction: column;
  gap: 0.125rem;
  flex: 1;
}

.shell-nav-item {
  position: relative;
  display: flex;
  align-items: center;
  gap: 0.75rem;
  min-height: 44px;
  padding: 0 0.75rem;
  border-radius: var(--radius-md);
  color: var(--foreground);
  font-size: 0.9375rem;
  text-decoration: none;
  transition: background 0.15s ease, color 0.15s ease;
}

.shell--rail .shell-nav-item {
  justify-content: center;
  padding: 0;
}

.shell-nav-item:hover {
  background: var(--mock-row);
}

.shell-nav-item.is-active {
  background: var(--surface-strong);
  font-weight: 600;
}

.shell-nav-divider {
  height: 1px;
  margin: 0.5rem 0.625rem;
  background: var(--divide);
}

.shell-nav-dot {
  position: absolute;
  right: 0.625rem;
  width: 8px;
  height: 8px;
  border-radius: 999px;
  background: var(--accent-link);
}

.shell--rail .shell-nav-dot {
  right: 0.5rem;
  top: 0.5rem;
}

.shell-sidebar-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
  padding: 0 0.25rem;
}

.shell--rail .shell-sidebar-foot {
  flex-direction: column;
}

.shell-rail-toggle {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 44px;
  height: 44px;
  border: none;
  border-radius: var(--radius-md);
  background: transparent;
  color: var(--foreground);
  cursor: pointer;
}

.shell-rail-toggle:hover {
  background: var(--mock-row);
}

.shell-version {
  font-size: 0.75rem;
  color: var(--muted-foreground, var(--foreground));
  opacity: 0.7;
}

/* --- Body (header + content) --- */
.shell-body {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-width: 0;
}

.app-main {
  flex: 1;
  min-height: 0;
}

.app-scroll {
  height: 100%;
}

.shell-tabs {
  display: none;
}

/* --- Mobile: bottom tabs, no sidebar --- */
@media (max-width: 900px) {
  .shell {
    flex-direction: column;
  }

  .shell-sidebar {
    display: none;
  }

  .shell-body {
    flex: 1;
    min-height: 0;
    padding-bottom: 60px;
  }

  .shell-tabs {
    position: fixed;
    left: 0;
    right: 0;
    bottom: 0;
    z-index: 60;
    display: flex;
    justify-content: space-around;
    height: 60px;
    background: var(--surface-elevated);
    border-top: 1px solid var(--divide);
    padding-bottom: env(safe-area-inset-bottom);
  }

  .shell-tab {
    position: relative;
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 0.125rem;
    color: var(--foreground);
    text-decoration: none;
    font-size: 0.6875rem;
    opacity: 0.75;
  }

  .shell-tab.is-active {
    opacity: 1;
    font-weight: 600;
  }

  .shell-tab .shell-nav-dot {
    position: absolute;
    top: 0.5rem;
    right: calc(50% - 1.6rem);
  }
}
</style>
