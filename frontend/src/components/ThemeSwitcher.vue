<template>
  <n-button-group size="small" class="theme-switch" role="group" aria-label="Selector de tema">
    <n-button
      v-for="option in options"
      :key="option.mode"
      :type="theme === option.mode ? 'primary' : 'default'"
      :title="option.label"
      :aria-label="option.label"
      :aria-pressed="theme === option.mode"
      class="touch-target"
      @click="setTheme(option.mode)"
    >
      <template #icon>
        <n-icon>
          <DesktopOutline v-if="option.mode === 'system'" />
          <SunnyOutline v-else-if="option.mode === 'light'" />
          <MoonOutline v-else />
        </n-icon>
      </template>
    </n-button>
  </n-button-group>
</template>

<script setup lang="ts">
import { NButton, NButtonGroup, NIcon } from "naive-ui";
import { DesktopOutline, MoonOutline, SunnyOutline } from "@vicons/ionicons5";
import { themeLabels, useTheme, type ThemeMode } from "@/composables/useTheme";

const { theme, setTheme } = useTheme();

const options: Array<{ mode: ThemeMode; label: string }> = (
  Object.keys(themeLabels) as ThemeMode[]
).map((mode) => ({ mode, label: themeLabels[mode] }));
</script>

<style scoped>
.theme-switch {
  display: flex;
  width: 100%;
}

.theme-switch :deep(.n-button) {
  flex: 1;
}
</style>

<style>
.user-menu-theme {
  padding: 0.375rem 0.625rem;
  min-width: 12.5rem;
}

.user-menu-theme-label {
  font-size: 0.75rem;
  color: var(--muted, #8a8a8a);
  margin-bottom: 0.375rem;
}
</style>
