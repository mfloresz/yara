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

      <!-- La búsqueda vive en el sidebar y abre el palette;
           es un botón, no un input. El medio lo da la sección activa. -->
      <button
        type="button"
        class="shell-search"
        :aria-label="`Buscar ${searchMediaLabel}`"
        aria-keyshortcuts="Control+K Meta+K"
        @click="searchOpen = true"
      >
        <n-icon :size="17"><SearchOutline /></n-icon>
        <span v-if="!rail" class="shell-search-label">Buscar</span>
        <span v-if="!rail" class="shell-search-kbd" aria-hidden="true">Ctrl K</span>
      </button>

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
        <!-- Cuenta: menú con tema y cierre de sesión (lo que antes vivía en
             el header). Ajustes/Administración siguen siendo secciones. -->
        <n-dropdown
          trigger="click"
          placement="top-start"
          :options="userMenuOptions"
          @select="handleUserMenuSelect"
        >
          <button
            type="button"
            class="shell-user"
            :aria-label="`Cuenta: ${userEmail}`"
          >
            <n-icon :size="20"><PersonCircleOutline /></n-icon>
            <span v-if="!rail" class="shell-user-mail no-wrap">{{ userEmail }}</span>
          </button>
        </n-dropdown>
        <button
          class="shell-rail-toggle touch-target"
          :aria-label="rail ? 'Expandir barra lateral' : 'Colapsar barra lateral'"
          :title="rail ? 'Expandir' : 'Colapsar'"
          @click="toggleRail"
        >
          <n-icon :size="18"><component :is="rail ? ChevronForwardOutline : ChevronBackOutline" /></n-icon>
        </button>
      </div>
    </aside>

    <div class="shell-body">
      <!-- Móvil: no hay sidebar, así que la búsqueda y la cuenta cuelgan de
           una barra fina sobre el contenido. -->
      <div class="shell-mobilebar">
        <button
          type="button"
          class="shell-mobilebar-button touch-target"
          :aria-label="`Buscar ${searchMediaLabel}`"
          @click="searchOpen = true"
        >
          <n-icon :size="20"><SearchOutline /></n-icon>
        </button>
        <n-dropdown
          trigger="click"
          placement="bottom-end"
          :options="userMenuOptions"
          @select="handleUserMenuSelect"
        >
          <button
            type="button"
            class="shell-mobilebar-button touch-target"
            aria-label="Menú de usuario"
          >
            <n-icon :size="22"><PersonCircleOutline /></n-icon>
          </button>
        </n-dropdown>
      </div>
      <main id="main-content" class="app-main" tabindex="-1">
        <n-scrollbar ref="scrollbarRef" class="app-scroll" content-class="page-container">
          <slot />
        </n-scrollbar>
      </main>
    </div>

    <!-- Móvil: 5 pestañas planas. Buscar vive en la barra superior y
         Ajustes/Administración en el menú de la cuenta, así que la barra no
         necesita un "Más" que duplique entradas. -->
    <nav class="shell-tabs" aria-label="Navegación principal">
      <span
        v-if="mobilePillIndex >= 0"
        class="shell-tab-pill"
        aria-hidden="true"
        :style="{ '--pill-index': mobilePillIndex }"
      />
      <RouterLink
        v-for="section in mobileSections"
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

    <MediaSearchModal v-model:show="searchOpen" />
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch, type Component } from "vue";
import { RouterLink, useRoute } from "vue-router";
import { NDropdown, NIcon, NScrollbar } from "naive-ui";
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
  PersonCircleOutline,
  SearchOutline,
} from "@vicons/ionicons5";
import MediaSearchModal from "@/components/MediaSearchModal.vue";
import { useActiveJobStatus } from "@/composables/useActiveJobStatus";
import { useUserMenu } from "@/composables/useUserMenu";
import { useAppServices } from "@/app/services";

const route = useRoute();
const scrollbarRef = ref<InstanceType<typeof NScrollbar> | null>(null);
const { hasActive } = useActiveJobStatus();
const { auth } = useAppServices();
const { userMenuOptions, handleUserMenuSelect } = useUserMenu();

const userEmail = computed(() => auth.user.value?.email ?? "Cuenta");

// El buscador respeta el medio de la sección activa (cómics en /comics,
// novelas en el resto); la etiqueta audible lo nombra.
const searchMediaLabel = computed(() =>
  route.path.startsWith("/comics") ? "cómics" : "novelas",
);

// Palette de búsqueda: sidebar, barra móvil y Ctrl/Cmd+K abren el mismo modal.
const searchOpen = ref(false);

function onGlobalKeydown(event: KeyboardEvent) {
  if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "k") {
    event.preventDefault();
    searchOpen.value = true;
  }
}

onMounted(() => window.addEventListener("keydown", onGlobalKeydown));
onBeforeUnmount(() => window.removeEventListener("keydown", onGlobalKeydown));

const RAIL_KEY = "yara-shell-rail";
// localStorage, no sessionStorage: la preferencia sobrevive cerrar la ventana.
const rail = ref(localStorage.getItem(RAIL_KEY) === "1");

function toggleRail() {
  rail.value = !rail.value;
  localStorage.setItem(RAIL_KEY, rail.value ? "1" : "0");
}

interface ShellSection {
  to: string;
  label: string;
  short?: string;
  icon: Component;
}

// Secciones: cada medio con su propia entrada, y un divisor
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

// Barra móvil: las tres secciones principales + Operaciones y Asistente
// (5 en total). Buscar vive en la barra superior y Ajustes/Administración
// en el menú de la cuenta: sin "Más" que duplique entradas.
const mobileSections = computed<ShellSection[]>(() => [
  ...topSections.value,
  ...bottomSections.value.filter(
    (s) => s.to === "/operations" || s.to === "/chat",
  ),
]);

// Slot del pill (0..4); en rutas sin pestaña propia (ajustes, admin) el
// pill se oculta: su entrada vive en el menú de la cuenta.
const mobilePillIndex = computed(() =>
  mobileSections.value.findIndex((s) => isActive(s)),
);

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
/* Chrome = --surface-strong: el paso que en claro queda por debajo del
   contenido y en oscuro por encima (la misma relación en ambos temas). */
.shell-sidebar {
  display: flex;
  flex-direction: column;
  width: 216px;
  flex-shrink: 0;
  background: var(--surface-strong);
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

/* Botón-buscador: parece un input, abre el palette. La
   superficie del campo contrasta con el chrome que lo rodea. */
.shell-search {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  width: 100%;
  min-height: 40px;
  padding: 0 0.625rem;
  background: var(--surface-elevated);
  border: 1px solid var(--divide);
  border-radius: var(--radius-md);
  color: var(--text-secondary);
  font: inherit;
  font-size: 0.875rem;
  cursor: pointer;
  transition: background 0.15s ease, color 0.15s ease;
}

.shell-search:hover {
  background: var(--surface-muted);
  color: var(--foreground);
}

.shell-search:focus-visible {
  outline: 2.5px solid var(--accent-link);
  outline-offset: 2px;
}

.shell--rail .shell-search {
  justify-content: center;
  padding: 0;
}

.shell-search-kbd {
  margin-left: auto;
  padding: 0.0625rem 0.375rem;
  border: 1px solid var(--divide);
  border-radius: 6px;
  background: var(--page-bg);
  font-size: 0.6875rem;
  line-height: 1.4;
  color: var(--text-tertiary);
  white-space: nowrap;
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

/* Tinte alfa (no paso sólido): sobre el chrome fuerte el activo debe leer
   más oscuro en claro y más claro en oscuro. */
.shell-nav-item.is-active {
  background: var(--mock-row-strong);
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

.shell-user {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  flex: 1;
  min-width: 0;
  min-height: 44px;
  padding: 0 0.5rem;
  border: none;
  border-radius: var(--radius-md);
  background: transparent;
  color: var(--foreground);
  font: inherit;
  font-size: 0.875rem;
  cursor: pointer;
  transition: background 0.15s ease;
}

.shell-user:hover {
  background: var(--mock-row);
}

.shell-user:focus-visible {
  outline: 2.5px solid var(--accent-link);
  outline-offset: 2px;
}

.shell--rail .shell-user {
  flex: none;
  justify-content: center;
  padding: 0;
  width: 44px;
}

.shell-user-mail {
  max-width: 100%;
  font-size: 0.8125rem;
  color: var(--text-secondary);
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

.shell-mobilebar {
  display: none;
}

.shell-mobilebar-button {
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

.shell-mobilebar-button:hover {
  background: var(--mock-row);
}

.shell-mobilebar-button:focus-visible {
  outline: 2.5px solid var(--accent-link);
  outline-offset: 2px;
}

.shell-tabs {
  display: none;
}

/* --- Mobile: barra fina arriba (búsqueda + cuenta) y pestañas abajo --- */
@media (max-width: 900px) {
  .shell {
    flex-direction: column;
  }

  .shell-sidebar {
    display: none;
  }

  .shell-mobilebar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-shrink: 0;
    min-height: 52px;
    padding: 0 0.375rem;
    background: var(--surface-strong);
    border-bottom: 1px solid var(--divide);
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
    height: 60px;
    background: var(--surface-strong);
    border-top: 1px solid var(--divide);
    padding-bottom: env(safe-area-inset-bottom);
  }

  /* Pill deslizante: una sola pieza que viaja al slot activo
     (transform, no width). 5 slots = 5 pestañas. */
  .shell-tab-pill {
    position: absolute;
    top: 5px;
    width: calc(100% / 5);
    height: 46px;
    border-radius: 999px;
    background: var(--mock-row-strong);
    transform: translateX(calc(var(--pill-index, 0) * 100%));
    transition: transform 0.2s cubic-bezier(0.2, 0, 0, 1);
    pointer-events: none;
  }

  .shell-tab {
    position: relative;
    z-index: 1;
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 0.125rem;
    border: none;
    background: transparent;
    font: inherit;
    cursor: pointer;
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
