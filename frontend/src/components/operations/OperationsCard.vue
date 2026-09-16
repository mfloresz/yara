<template>
  <n-card size="small" class="ops-card" content-style="padding: 12px;">
    <div class="ops-card-top" role="listitem">
      <n-checkbox
        :checked="checked"
        :aria-label="`Seleccionar ${novel.sourceTitle}`"
        class="ops-card-check"
        @update:checked="$emit('update:checked', $event)"
      />
      <n-avatar
        v-if="novel.coverPath"
        :size="44"
        :src="novel.coverPath"
        class="ops-card-cover"
      />
      <n-avatar v-else :size="44" class="ops-card-cover ops-card-cover--fallback">
        {{ novel.sourceTitle.slice(0, 2).toUpperCase() }}
      </n-avatar>
      <div class="ops-card-meta">
        <RouterLink :to="`/novels/${novel.id}`" class="ops-card-title">
          {{ novel.sourceTitle }}
          <n-icon v-if="novel.requiresBrowser === true" :size="13" class="ops-card-browser" aria-label="Requiere extensión de navegador">
            <GlobeOutline />
          </n-icon>
        </RouterLink>
        <p class="muted small ops-card-author">{{ novel.sourceAuthor }}</p>
      </div>
    </div>

    <div class="ops-card-facts">
      <div class="ops-fact">
        <span class="ops-fact-label">Origen</span>
        <OperationsOriginTag :type="status.origin.type" :text="status.origin.text" :tip="status.origin.tip" />
      </div>
      <div class="ops-fact">
        <span class="ops-fact-label">Traducción</span>
        <OperationsTranslation :status="status.translation" />
      </div>
    </div>

    <div class="ops-card-actions">
      <n-button
        size="medium"
        secondary
        :loading="status.checkBusy"
        :disabled="status.checkDisabled"
        class="ops-card-btn"
        @click="$emit('check')"
      >
        <template #icon><n-icon><RefreshOutline /></n-icon></template>
        {{ status.checkLabel }}
      </n-button>
      <n-button
        size="medium"
        :type="status.canDownload ? 'warning' : 'default'"
        :secondary="!status.canDownload || status.downloadBusy"
        :loading="status.downloadBusy"
        :disabled="status.downloadDisabled"
        class="ops-card-btn"
        @click="$emit('download')"
      >
        <template #icon><n-icon><DownloadOutline /></n-icon></template>
        {{ status.downloadLabel }}
      </n-button>
      <n-button
        size="medium"
        :type="status.canTranslate ? 'primary' : 'default'"
        :secondary="!status.canTranslate"
        :loading="status.translateBusy"
        :disabled="status.translateDisabled"
        class="ops-card-btn"
        @click="$emit('translate')"
      >
        <template #icon><n-icon><PlayOutline /></n-icon></template>
        {{ status.translateLabel }}
      </n-button>
    </div>
  </n-card>
</template>

<script setup lang="ts">
import { RouterLink } from "vue-router";
import { NAvatar, NButton, NCard, NCheckbox, NIcon } from "naive-ui";
import { DownloadOutline, GlobeOutline, PlayOutline, RefreshOutline } from "@vicons/ionicons5";
import type { NovelOperationStatus } from "@/composables/useOperationDisplay";
import type { Novel } from "@/domain";
import OperationsOriginTag from "@/components/operations/OperationsOriginTag.vue";
import OperationsTranslation from "@/components/operations/OperationsTranslation.vue";

defineProps<{
  novel: Novel;
  checked: boolean;
  status: NovelOperationStatus;
}>();

defineEmits<{
  "update:checked": [value: boolean];
  check: [];
  download: [];
  translate: [];
}>();
</script>

<style scoped>
.ops-card-top {
  display: flex;
  align-items: center;
  gap: 0.625rem;
  min-width: 0;
}

.ops-card-check {
  flex-shrink: 0;
}

.ops-card-cover {
  border-radius: var(--radius-sm);
  flex-shrink: 0;
}

.ops-card-cover--fallback {
  font-size: 12px;
}

.ops-card-meta {
  min-width: 0;
  flex: 1;
}

.ops-card-title {
  display: inline-flex;
  align-items: center;
  gap: 0.25rem;
  font-weight: 600;
  font-size: 0.9375rem;
  line-height: 1.35;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 100%;
}

.ops-card-browser {
  color: var(--warning);
  flex-shrink: 0;
}

.ops-card-author {
  margin: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ops-card-facts {
  display: flex;
  gap: 1.25rem;
  padding: 0.625rem 0 0.625rem 2.125rem;
}

.ops-fact {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 4px;
}

.ops-fact-label {
  font-size: 11px;
  color: var(--text-tertiary);
}

.ops-card-actions {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.ops-card-btn {
  width: 100%;
  min-height: 44px;
  justify-content: flex-start;
}

@media (prefers-reduced-motion: reduce) {
  .ops-card-btn {
    transition: none;
  }
}
</style>
