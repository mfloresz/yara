<template>
  <n-flex justify="end" :size="6" :wrap="false" align="center">
    <n-tooltip trigger="hover">
      <template #trigger>
        <n-button
          size="small"
          secondary
          circle
          :loading="checkBusy"
          :disabled="checkDisabled"
          :aria-label="checkLabel"
          @click="$emit('check')"
        >
          <template v-if="!checkBusy" #icon><n-icon><RefreshOutline /></n-icon></template>
        </n-button>
      </template>
      {{ checkLabel }}
    </n-tooltip>

    <n-tooltip trigger="hover">
      <template #trigger>
        <n-button
          size="small"
          :type="canDownload ? 'warning' : 'default'"
          :secondary="!canDownload || downloadBusy"
          circle
          :loading="downloadBusy"
          :disabled="downloadDisabled"
          :aria-label="downloadLabel"
          @click="$emit('download')"
        >
          <template v-if="!downloadBusy" #icon><n-icon><DownloadOutline /></n-icon></template>
        </n-button>
      </template>
      {{ downloadLabel }}
    </n-tooltip>

    <n-tooltip trigger="hover">
      <template #trigger>
        <n-button
          size="small"
          :type="canTranslate ? 'primary' : 'default'"
          :secondary="!canTranslate"
          circle
          :loading="translateBusy"
          :disabled="translateDisabled"
          :aria-label="translateLabel"
          @click="$emit('translate')"
        >
          <template v-if="!translateBusy" #icon><n-icon><PlayOutline /></n-icon></template>
        </n-button>
      </template>
      {{ translateLabel }}
    </n-tooltip>
  </n-flex>
</template>

<script setup lang="ts">
import { NButton, NFlex, NIcon, NTooltip } from "naive-ui";
import { DownloadOutline, PlayOutline, RefreshOutline } from "@vicons/ionicons5";

defineProps<{
  canDownload: boolean;
  canTranslate: boolean;
  checkLabel: string;
  downloadLabel: string;
  translateLabel: string;
  checkBusy: boolean;
  downloadBusy: boolean;
  translateBusy: boolean;
  checkDisabled: boolean;
  downloadDisabled: boolean;
  translateDisabled: boolean;
}>();

defineEmits<{
  check: [];
  download: [];
  translate: [];
}>();
</script>
