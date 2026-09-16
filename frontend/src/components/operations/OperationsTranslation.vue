<template>
  <n-flex vertical :size="4" align="center" justify="center" class="ops-translation">
    <n-tooltip trigger="hover" :disabled="!status.tagTip">
      <template #trigger>
        <n-tag :type="status.tagType" size="tiny" round>{{ status.tagText }}</n-tag>
      </template>
      {{ status.tagTip }}
    </n-tooltip>
    <n-tooltip v-if="status.showProgress" trigger="hover" :disabled="!status.progressTip">
      <template #trigger>
        <div class="ops-progress">
          <n-progress
            type="line"
            :percentage="status.progressPct"
            :show-indicator="false"
            :height="5"
            class="ops-progress-bar"
            aria-hidden="true"
          />
          <n-text depth="3" class="ops-progress-label">
            {{ status.translatedCount }} / {{ status.chapterCount }}
          </n-text>
        </div>
      </template>
      {{ status.progressTip }}
    </n-tooltip>
  </n-flex>
</template>

<script setup lang="ts">
import { NFlex, NProgress, NTag, NText, NTooltip } from "naive-ui";
import type { NovelTranslationStatus } from "@/composables/useOperationDisplay";

defineProps<{
  status: NovelTranslationStatus;
}>();
</script>

<style scoped>
.ops-translation {
  padding: 2px 0;
}

.ops-progress {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 2px;
  min-width: 96px;
}

.ops-progress-bar {
  width: 96px;
}

.ops-progress-label {
  font-size: 11px;
  line-height: 1.2;
  white-space: nowrap;
}
</style>
