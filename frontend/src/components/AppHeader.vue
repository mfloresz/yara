<template>
  <header class="app-topbar">
    <div class="page-container app-topbar-inner">
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
      </div>
    </div>
  </header>

  <JobsDrawer id="jobs-drawer" v-model:visible="jobsOpen" />
</template>

<script setup lang="ts">
import { ref } from "vue";
import { NButton, NIcon, NDropdown, NBadge } from "naive-ui";
import { TimeOutline, PersonOutline } from "@vicons/ionicons5";
import JobsDrawer from "@/components/JobsDrawer.vue";
import GlobalNovelSearch from "@/components/GlobalNovelSearch.vue";
import { useActiveJobStatus } from "@/composables/useActiveJobStatus";
import { useUserMenu } from "@/composables/useUserMenu";
import { useAppServices } from "@/app/services";

const { hasActive } = useActiveJobStatus();
const { auth } = useAppServices();
const { userMenuOptions, handleUserMenuSelect } = useUserMenu();
const jobsOpen = ref(false);
void auth;
</script>

<style scoped>
.app-topbar {
  position: relative;
  z-index: 50;
  flex-shrink: 0;
  background: var(--surface-elevated);
  border-bottom: 1px solid var(--divide);
}

.app-topbar-inner {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 1rem;
  min-height: 56px;
  padding-top: 0.5rem;
  padding-bottom: 0.5rem;
}

.app-topbar-center {
  flex: 1;
  min-width: 0;
  max-width: 34rem;
}

.app-topbar-end {
  display: flex;
  align-items: center;
  gap: 0.375rem;
  flex-shrink: 0;
}

@media (max-width: 768px) {
  .app-topbar-inner {
    min-height: 52px;
  }

  .app-topbar-center {
    max-width: none;
  }
}
</style>
