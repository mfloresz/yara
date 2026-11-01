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
      <n-dropdown
        trigger="click"
        placement="bottom-end"
        :options="readingMenuOptions"
        @select="handleReadingMenuSelect"
      >
        <n-button
          secondary
          block
          :aria-label="`Estado de lectura: ${readingStatusLabel}`"
          aria-haspopup="menu"
        >
          <template #icon><n-icon><BookmarkOutline /></n-icon></template>
          {{ readingStatusLabel }}
        </n-button>
      </n-dropdown>
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
import { computed } from "vue";
import { NButton, NDropdown, NIcon, NTag, useDialog, useMessage } from "naive-ui";
import {
  BookOutline,
  BookmarkOutline,
  ChevronDownOutline,
  CloudDoneOutline,
  CloudDownloadOutline,
  CopyOutline,
  GlobeOutline,
  RefreshOutline,
  SettingsOutline,
} from "@vicons/ionicons5";
import {
  getNovelDisplayTitle,
  type Novel,
  type NovelStatus,
  type ReadingStatus,
} from "@/domain";
import { coverSrc, onCoverError } from "@/utils/cover";
import {
  readingStatusMenuOptions,
  useReadingStatus,
} from "@/composables/useReadingStatus";

const props = defineProps<{
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

const { setStatus, clearProgress } = useReadingStatus();
const message = useMessage();
const dialog = useDialog();

const readingMenuOptions = computed(() => readingStatusMenuOptions("novel", props.novel));

const readingStatusLabel = computed(() => {
  switch (props.novel.readingStatus) {
    case "finished":
      return "Leída";
    case "paused":
      return "En pausa";
    case "reading":
      return "Leyendo";
    default:
      return "Estado de lectura";
  }
});

function handleReadingMenuSelect(key: string) {
  const novelId = props.novel.id;
  if (key === "clear") {
    dialog.warning({
      title: "Quitar progreso",
      content: `¿Quitar el progreso de lectura de "${getNovelDisplayTitle(props.novel)}"? Se olvidará tu posición y sus marcas.`,
      positiveText: "Quitar",
      negativeText: "Cancelar",
      onPositiveClick: async () => {
        try {
          await clearProgress("novel", novelId);
          props.novel.readingStatus = undefined;
          props.novel.lastReadAt = undefined;
        } catch {
          message.error("No se pudo quitar el progreso.");
        }
      },
    });
    return;
  }
  const status = key as ReadingStatus;
  void setStatus("novel", novelId, status)
    .then(() => {
      props.novel.readingStatus = status;
    })
    .catch(() => {
      message.error("No se pudo actualizar el estado de lectura.");
    });
}

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
