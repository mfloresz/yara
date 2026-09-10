<template>
  <aside class="novel-sidebar">
    <div class="novel-cover-large">
      <img :src="coverSrc(novel.coverPath)" :alt="`Portada de ${getNovelDisplayTitle(novel)}`" loading="lazy" @error="onCoverError" />
    </div>

    <div class="novel-sidebar-actions">
      <n-button type="primary" block @click="emit('read')">
        <template #icon><n-icon><BookOutline /></n-icon></template>
        Leer
      </n-button>
      <n-button v-if="isOwner" secondary block @click="emit('open-settings')">
        <template #icon><n-icon><SettingsOutline /></n-icon></template>
        Configuración
      </n-button>
      <n-button v-else secondary block @click="emit('copy-novel')">
        <template #icon><n-icon><CopyOutline /></n-icon></template>
        Copiar novela
      </n-button>
      <n-button v-if="isOwner" secondary block @click="emit('toggle-visibility')">
        <template #icon><n-icon><GlobeOutline /></n-icon></template>
        {{ novel.isPublic ? 'Compartiendo' : 'Compartir' }}
      </n-button>
      <n-button v-if="isOwner && novel.url" secondary block @click="emit('open-update-url')">
        <template #icon><n-icon><RefreshOutline /></n-icon></template>
        Actualizar
      </n-button>
      <n-button secondary block :loading="downloadingOffline" @click="emit('toggle-offline')">
        <template #icon>
          <n-icon><CloudDoneOutline v-if="isNovelCached" /><CloudDownloadOutline v-else /></n-icon>
        </template>
        {{ isNovelCached ? 'Guardado Offline' : 'Guardar Offline' }}
      </n-button>
    </div>

    <div class="novel-sidebar-tags">
      <n-tag :type="novelStatusType(novel.status)" size="small" round>{{ novelStatusLabel(novel.status) }}</n-tag>
      <n-tag size="small" round>{{ totalChapters }} capítulos</n-tag>
      <n-tag size="small" round>{{ translatedChapters }} traducidos</n-tag>
      <n-tag type="success" size="small" round>{{ novel.sourceLanguage }} → {{ novel.targetLanguage }}</n-tag>
    </div>
  </aside>
</template>

<script setup lang="ts">
import { NButton, NIcon, NTag } from "naive-ui";
import {
  BookOutline,
  CloudDoneOutline,
  CloudDownloadOutline,
  CopyOutline,
  GlobeOutline,
  RefreshOutline,
  SettingsOutline,
} from "@vicons/ionicons5";
import { getNovelDisplayTitle, type Novel, type NovelStatus } from "@/domain";
import { coverSrc, onCoverError } from "@/utils/cover";

defineProps<{
  novel: Novel;
  isOwner: boolean;
  isNovelCached: boolean;
  downloadingOffline: boolean;
  totalChapters: number;
  translatedChapters: number;
}>();

const emit = defineEmits<{
  (e: "read"): void;
  (e: "open-settings"): void;
  (e: "copy-novel"): void;
  (e: "toggle-visibility"): void;
  (e: "open-update-url"): void;
  (e: "toggle-offline"): void;
}>();

function novelStatusLabel(status: NovelStatus) {
  switch (status) {
    case "completed":
      return "Completada";
    case "hiatus":
      return "Hiatus";
    case "cancelled":
      return "Cancelada";
    default:
      return "En curso";
  }
}

function novelStatusType(status: NovelStatus) {
  switch (status) {
    case "completed":
      return "info";
    case "hiatus":
      return "warning";
    case "cancelled":
      return "error";
    default:
      return "success";
  }
}
</script>

<style scoped>
.novel-sidebar {
  display: flex;
  flex-direction: column;
  gap: 0.875rem;
}

.novel-cover-large {
  border-radius: var(--radius-md);
  overflow: hidden;
  border: 1px solid var(--divide);
  background: var(--surface-muted);
}

.novel-cover-large img {
  width: 100%;
  height: auto;
  aspect-ratio: 2 / 3;
  object-fit: cover;
  display: block;
}

.novel-sidebar-actions {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.novel-sidebar-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}

@media (max-width: 768px) {
  .novel-sidebar {
    display: grid;
    grid-template-columns: 100px 1fr;
    gap: 0.75rem;
    align-items: start;
  }

  .novel-cover-large {
    max-width: 100px;
  }

  .novel-sidebar-actions {
    gap: 0.375rem;
  }

  .novel-sidebar-tags {
    grid-column: 1 / -1;
  }
}
</style>
