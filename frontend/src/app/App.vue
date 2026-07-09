<template>
  <n-config-provider
    :theme="currentNaiveTheme"
    :theme-overrides="currentThemeOverrides"
  >
    <n-global-style />
    <n-message-provider>
      <n-dialog-provider>
        <n-notification-provider>
          <RouterView />
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
import {
  currentNaiveTheme,
  currentThemeOverrides,
} from "@/app/auth";
</script>
