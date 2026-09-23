<template>
  <header class="app-topbar">
    <div class="page-container app-topbar-inner">
      <div class="app-topbar-start">
        <slot name="back-button" />
        <RouterLink to="/" class="app-brand" aria-label="Yara - Inicio">
          <span class="app-brand-icon" aria-hidden="true">
            <img src="/favicon.svg" alt="" width="30" height="30" />
          </span>
          <span class="app-brand-text">Yara</span>
        </RouterLink>
      </div>

      <div class="app-topbar-center">
        <GlobalNovelSearch />
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

        <n-dropdown
          trigger="click"
          :options="userMenuOptions"
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
      <div class="mobile-nav-theme">
        <span class="mobile-nav-theme-label">Tema</span>
        <ThemeSwitcher />
      </div>
      <n-button text block class="mobile-nav-item touch-target" @click="handleMobileNav(() => router.push('/settings'))">
        <template #icon><n-icon :size="20"><SettingsOutline /></n-icon></template>
        <span>Configuración</span>
      </n-button>
      <n-button text block class="mobile-nav-item touch-target" @click="handleMobileNav(() => router.push('/operations'))">
        <template #icon><n-icon :size="20"><FlashOutline /></n-icon></template>
        <span>Operaciones</span>
      </n-button>
      <n-button v-if="auth.isAdmin.value" text block class="mobile-nav-item touch-target" @click="handleMobileNav(() => router.push('/admin'))">
        <template #icon><n-icon :size="20"><ShieldOutline /></n-icon></template>
        <span>Administración</span>
      </n-button>
      <n-divider style="margin: 0.5rem 0;" />
      <n-button text block class="mobile-nav-item mobile-nav-item--danger touch-target" @click="handleMobileNav(() => doLogout())">
        <template #icon><n-icon :size="20"><LogOutOutline /></n-icon></template>
        <span>Cerrar sesión</span>
      </n-button>
      <n-divider style="margin: 0.5rem 0;" />
      <div class="mobile-nav-version">{{ serverVersion ? `Yara ${serverVersion}` : "Yara…" }}</div>
    </n-drawer-content>
  </n-drawer>

  <JobsDrawer id="jobs-drawer" v-model:visible="jobsOpen" />
</template>

<script setup lang="ts">
import { ref } from "vue";
import { RouterLink, useRouter } from "vue-router";
import { NButton, NIcon, NDropdown, NDrawer, NDrawerContent, NDivider, NBadge } from "naive-ui";
import {
  TimeOutline,
  FlashOutline,
  PersonOutline,
  MenuOutline,
  CloseOutline,
  BriefcaseOutline,
  SettingsOutline,
  ShieldOutline,
  LogOutOutline,
} from "@vicons/ionicons5";
import JobsDrawer from "@/components/JobsDrawer.vue";
import GlobalNovelSearch from "@/components/GlobalNovelSearch.vue";
import ThemeSwitcher from "@/components/ThemeSwitcher.vue";
import { useActiveJobStatus } from "@/composables/useActiveJobStatus";
import { useServerVersion } from "@/composables/useServerVersion";
import { useUserMenu } from "@/composables/useUserMenu";
import { useAppServices } from "@/app/services";

const router = useRouter();
const { hasActive } = useActiveJobStatus();
const { version: serverVersion } = useServerVersion();
const { auth } = useAppServices();
const { userMenuOptions, handleUserMenuSelect, doLogout } = useUserMenu();
const jobsOpen = ref(false);
const mobileNavOpen = ref(false);

function handleMobileNav(command?: () => void) {
  mobileNavOpen.value = false;
  command?.();
}
</script>

<style scoped>
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
  flex-shrink: 0;
}

.app-topbar-center {
  flex: 1;
  min-width: 0;
  max-width: 30rem;
  margin-inline: auto;
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
  overflow: hidden;
}

.app-brand-icon img {
  display: block;
  width: 100%;
  height: 100%;
}

.app-brand-text {
  font-weight: 700;
}

.mobile-nav-theme {
  padding: 0.5rem 0.75rem;
}

.mobile-nav-theme-label {
  display: block;
  font-size: 0.75rem;
  color: var(--muted, #8a8a8a);
  margin-bottom: 0.375rem;
}

@media (max-width: 900px) {
  .app-topbar-inner {
    gap: 0.5rem;
  }

  .app-topbar-center {
    max-width: none;
  }
}

@media (max-width: 768px) {
  .app-topbar-inner {
    min-height: 52px;
  }

  .app-brand-text {
    display: none;
  }

  .app-topbar-center {
    flex: 0 0 auto;
    min-width: 0;
    max-width: none;
    /* margin-left:auto (with margin-right reset from the desktop
       margin-inline:auto) pushes the loupe right, directly before the
       action buttons. No order swap: DOM order already places it there. */
    margin: 0 0 0 auto;
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
  color: var(--danger);
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

.mobile-nav-version {
  padding: 0.25rem 0.75rem 0.5rem;
  font-size: 0.75rem;
  color: var(--muted, #8a8a8a);
  text-align: center;
}
</style>
