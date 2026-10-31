<template>
  <n-config-provider
    :theme="currentNaiveTheme"
    :theme-overrides="currentThemeOverrides"
  >
    <n-global-style />
    <n-message-provider>
      <n-dialog-provider>
        <n-notification-provider>
          <RouterView v-slot="{ Component, route }">
            <AppShell v-if="isAuthenticated && !route.meta.immersive">
              <Component :is="Component" />
            </AppShell>
            <Component :is="Component" v-else />
          </RouterView>
        </n-notification-provider>
      </n-dialog-provider>
    </n-message-provider>
  </n-config-provider>
</template>

<script setup lang="ts">
import { RouterView } from "vue-router";
import {
  NConfigProvider,
  NGlobalStyle,
  NMessageProvider,
  NDialogProvider,
  NNotificationProvider,
} from "naive-ui";
import AppShell from "@/components/AppShell.vue";
import {
  authState,
  currentNaiveTheme,
  currentThemeOverrides,
} from "@/app/auth";

// Top-level so the computed unwraps in the template. The shell (sidebar +
// header) only exists inside a session; guest pages render bare. The router
// is attached after restoreSession (main.ts), so this is already resolved on
// first paint.
const isAuthenticated = authState.isAuthenticated;
</script>
