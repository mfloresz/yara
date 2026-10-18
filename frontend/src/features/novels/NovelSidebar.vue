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
      <span v-if="isOwner" class="sidebar-actions-group" role="group" aria-label="Acciones de la novela">
        <n-button
          class="sidebar-actions-main"
          secondary
          @click="emit('open-settings')"
        >
          <template #icon><n-icon><SettingsOutline /></n-icon></template>
          Configuración
        </n-button>
        <n-dropdown trigger="click" placement="bottom-end" :options="sidebarMenuOptions" @select="handleSidebarMenuSelect">
          <n-button
            class="sidebar-actions-arrow"
            secondary
            aria-label="Más acciones"
            aria-haspopup="menu"
          >
            <template #icon><n-icon :size="14"><ChevronDownOutline /></n-icon></template>
          </n-button>
        </n-dropdown>
      </span>
      <n-button v-else secondary block @click="emit('copy-novel')">
        <template #icon><n-icon><CopyOutline /></n-icon></template>
        Copiar novela
      </n-button>
      <n-button v-if="!isOwner" secondary block :loading="downloadingOffline" @click="emit('toggle-offline')">
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
import { NButton, NDropdown, NIcon, NTag } from "naive-ui";
import {
  BookOutline,
  ChevronDownOutline,
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

const sidebarMenuOptions = [
  { label: "Compartir", key: "share" },
  { label: "Actualizar", key: "update" },
  { label: "Guardar Offline", key: "offline" },
];

function handleSidebarMenuSelect(key: string) {
  switch (key) {
    case "share":
      emit("toggle-visibility");
      break;
    case "update":
      emit("open-update-url");
      break;
    case "offline":
      emit("toggle-offline");
      break;
  }
}

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

.sidebar-actions-group {
  display: flex;
  width: 100%;
}

.sidebar-actions-main {
  flex: 1;
  border-radius: 999px 0 0 999px;
}

.sidebar-actions-arrow {
  flex-shrink: 0;
  border-radius: 0 999px 999px 0;
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
