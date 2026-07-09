<template>
  <a href="#main-content" class="skip-link">Saltar al contenido principal</a>

  <div class="app-shell">
    <header class="app-topbar">
      <div class="page-container app-topbar-inner">
        <div class="app-topbar-start">
          <slot name="back-button" />
          <RouterLink to="/" class="app-brand" aria-label="Yara - Inicio">
            <span class="app-brand-icon" aria-hidden="true">
              <n-icon :size="20"><BookOutline /></n-icon>
            </span>
            <span class="app-brand-text">Yara</span>
          </RouterLink>
        </div>

        <div class="app-topbar-end">
          <n-button
            :secondary="hasActive"
            :quaternary="!hasActive"
            circle
            size="small"
            class="touch-target"
            :aria-label="hasActive ? 'Hay trabajos activos' : 'No hay trabajos activos'"
            :aria-expanded="jobsOpen"
            aria-controls="jobs-drawer"
            @click="jobsOpen = true"
          >
            <template #icon>
              <n-badge dot :show="hasActive" :offset="[-4, 4]">
                <n-icon><TimeOutline /></n-icon>
              </n-badge>
            </template>
          </n-button>

          <n-button
            quaternary
            circle
            size="small"
            class="touch-target"
            aria-label="Operaciones"
            @click="router.push('/operations')"
          >
            <template #icon>
              <n-icon><FlashOutline /></n-icon>
            </template>
          </n-button>

          <n-button
            quaternary
            circle
            size="small"
            class="touch-target"
            :aria-label="`Cambiar tema (actual: ${themeLabel})`"
            @click="cycleTheme"
          >
            <template #icon>
              <n-icon>
                <DesktopOutline v-if="theme === 'system'" />
                <SunnyOutline v-else-if="theme === 'light'" />
                <MoonOutline v-else />
              </n-icon>
            </template>
          </n-button>

          <n-dropdown
            trigger="click"
            :options="userMenuDropdownItems"
            @select="handleUserMenuSelect"
          >
            <n-button
              quaternary
              circle
              size="small"
              class="touch-target"
              aria-label="Menú de usuario"
            >
              <template #icon>
                <n-icon><PersonOutline /></n-icon>
              </template>
            </n-button>
          </n-dropdown>

          <n-button
            class="mobile-menu-btn touch-target"
            quaternary
            circle
            size="small"
            aria-label="Abrir menú de navegación"
            :aria-expanded="mobileNavOpen"
            aria-controls="mobile-nav-drawer"
            style="display: none"
            @click="mobileNavOpen = !mobileNavOpen"
          >
            <template #icon>
              <n-icon><MenuOutline /></n-icon>
            </template>
          </n-button>
        </div>
      </div>
    </header>

    <n-drawer v-model:show="mobileNavOpen" :width="280" placement="left">
      <n-drawer-content :native-scrollbar="false" body-content-style="padding: 0.5rem;">
        <template #header>
          <div style="display: flex; align-items: center; justify-content: space-between; width: 100%">
            <span class="app-brand-text">Yara</span>
            <n-button quaternary circle size="small" class="touch-target" aria-label="Cerrar menú" @click="mobileNavOpen = false">
              <template #icon><n-icon><CloseOutline /></n-icon></template>
            </n-button>
          </div>
        </template>
        <n-button text block class="mobile-nav-item touch-target" @click="handleMobileNav(() => jobsOpen = true)">
          <template #icon><n-icon :size="20"><BriefcaseOutline /></n-icon></template>
          <span>Trabajos</span>
          <span v-if="hasActive" class="mobile-nav-badge">•</span>
        </n-button>
        <n-button text block class="mobile-nav-item touch-target" @click="handleMobileNav(cycleTheme)">
          <template #icon>
            <n-icon :size="20">
              <DesktopOutline v-if="theme === 'system'" />
              <SunnyOutline v-else-if="theme === 'light'" />
              <MoonOutline v-else />
            </n-icon>
          </template>
          <span>Tema: {{ themeLabel }}</span>
        </n-button>
        <n-button text block class="mobile-nav-item touch-target" @click="handleMobileNav(() => router.push('/settings'))">
          <template #icon><n-icon :size="20"><SettingsOutline /></n-icon></template>
          <span>Configuración</span>
        </n-button>
        <n-button text block class="mobile-nav-item touch-target" @click="handleMobileNav(() => router.push('/operations'))">
          <template #icon><n-icon :size="20"><FlashOutline /></n-icon></template>
          <span>Operaciones</span>
        </n-button>
        <n-divider style="margin: 0.5rem 0;" />
        <n-button text block class="mobile-nav-item mobile-nav-item--danger touch-target" @click="handleMobileNav(() => doLogout())">
          <template #icon><n-icon :size="20"><LogOutOutline /></n-icon></template>
          <span>Cerrar sesión</span>
        </n-button>
      </n-drawer-content>
    </n-drawer>

    <main id="main-content" class="page-container" tabindex="-1">
      <slot />
    </main>

    <JobsDrawer id="jobs-drawer" v-model:visible="jobsOpen" />
  </div>
</template>

<script setup lang="ts">
import { computed, ref, h } from "vue";
import { RouterLink, useRouter } from "vue-router";
import { NButton, NIcon, NDropdown, NDrawer, NDrawerContent, NDivider, NBadge } from "naive-ui";
import {
  BookOutline,
  TimeOutline,
  FlashOutline,
  DesktopOutline,
  SunnyOutline,
  MoonOutline,
  PersonOutline,
  MenuOutline,
  CloseOutline,
  BriefcaseOutline,
  SettingsOutline,
  LogOutOutline,
} from "@vicons/ionicons5";
import JobsDrawer from "@/components/JobsDrawer.vue";
import { applyTheme, getStoredTheme } from "@/app/auth";
import { useActiveJobStatus } from "@/composables/useActiveJobStatus";
import { useAppServices } from "@/app/services";

const router = useRouter();
const { hasActive } = useActiveJobStatus();
const { auth, logout } = useAppServices();
const jobsOpen = ref(false);
const mobileNavOpen = ref(false);

const theme = ref<"light" | "dark" | "system">(getStoredTheme());

const themeCycle: Array<"light" | "dark" | "system"> = ["system", "light", "dark"];

const themeLabels: Record<"light" | "dark" | "system", string> = {
  system: "Sistema",
  light: "Claro",
  dark: "Oscuro",
};

const themeLabel = computed(() => themeLabels[theme.value]);

const userMenuDropdownItems = computed(() => [
  { label: auth.user.value?.email ?? "", key: "email", disabled: true },
  { type: "divider", key: "d1" },
  { label: "Configuración", key: "settings" },
  { label: "Cerrar sesión", key: "logout" },
]);

function handleUserMenuSelect(key: string) {
  if (key === "settings") router.push("/settings");
  else if (key === "logout") doLogout();
}

function cycleTheme() {
  const nextIndex = (themeCycle.indexOf(theme.value) + 1) % themeCycle.length;
  theme.value = themeCycle[nextIndex];
  applyTheme(theme.value);
}

async function doLogout() {
  await logout();
  await router.push("/login");
}

function handleMobileNav(command?: () => void) {
  mobileNavOpen.value = false;
  command?.();
}
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

.app-topbar {
  position: sticky;
  top: 0;
  z-index: 50;
  background: color-mix(in oklab, var(--surface-elevated) 92%, transparent);
  backdrop-filter: blur(12px);
  border-bottom: 1px solid var(--divide);
}

.app-topbar-inner {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  min-height: 56px;
  padding-top: 0.5rem;
  padding-bottom: 0.5rem;
}

.app-topbar-start,
.app-topbar-end {
  display: flex;
  align-items: center;
  gap: 0.375rem;
}

.app-brand {
  display: inline-flex;
  align-items: center;
  gap: 0.5rem;
  font-weight: 700;
  font-size: 1.0625rem;
  color: var(--foreground);
  padding: 0.25rem 0.5rem;
  border-radius: var(--radius-md);
}

.app-brand:hover {
  background: var(--mock-row);
}

.app-brand-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 1.875rem;
  height: 1.875rem;
  border-radius: var(--radius-sm);
  background: var(--btn-primary-bg);
  color: var(--btn-primary-fg);
}

.app-brand-text {
  font-weight: 700;
}

@media (max-width: 768px) {
  .app-topbar-inner {
    min-height: 52px;
  }

  .app-brand-text {
    display: none;
  }
}
</style>

<style>
.mobile-nav-item {
  justify-content: flex-start;
  gap: 0.875rem;
  font-size: 0.9375rem;
  text-align: left;
}

.mobile-nav-item--danger {
  color: #dc2626;
}

.mobile-nav-badge {
  margin-left: auto;
  background: var(--accent-link);
  color: var(--btn-primary-fg);
  font-size: 0.75rem;
  font-weight: 600;
  padding: 0.125rem 0.5rem;
  border-radius: var(--radius-pill);
}
</style>
