<template>
  <n-tooltip trigger="hover" :disabled="!tip">
    <template #trigger>
      <n-tag :type="type" size="tiny" round>{{ text }}</n-tag>
    </template>
    {{ tip }}
  </n-tooltip>
</template>

<script setup lang="ts">
import { NTag, NTooltip } from "naive-ui";
import type { NovelOriginStatus } from "@/composables/useOperationDisplay";

defineProps<NovelOriginStatus>();
</script>
