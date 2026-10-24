<template>
  <AppLayout>
    <section class="cl-page" aria-label="Cómics">
      <header class="cl-header">
        <h1 class="cl-title">Cómics</h1>

        <div class="cl-toolbar">
          <n-input
            v-model:value="query"
            size="small"
            clearable
            placeholder="Buscar por título, autor o serie…"
            style="max-width: 22rem"
            @keydown.enter="reload"
            @clear="reload"
          />
          <n-select
            v-model:value="sortField"
            size="small"
            :options="sortOptions"
            style="width: 10rem"
            @update:value="reload"
          />
          <n-button size="small" secondary @click="urlOpen = true">
            <template #icon><n-icon><CloudDownloadOutline /></n-icon></template>
            Desde URL
          </n-button>
          <n-button size="small" secondary @click="zipOpen = true">
            <template #icon><n-icon><ArchiveOutline /></n-icon></template>
            Desde ZIP
          </n-button>
        </div>
      </header>

      <LibrarySkeleton v-if="loading" />

      <div v-else-if="comics.length" class="cl-grid">
        <ComicCard v-for="comic in comics" :key="comic.id" :comic="comic" />
      </div>

      <n-empty
        v-else
        class="cl-empty"
        description="Sin cómics todavía. Impórtalos desde una URL o un ZIP de imágenes."
      >
        <template #extra>
          <n-button size="small" type="primary" secondary @click="urlOpen = true">
            Importar cómic
          </n-button>
        </template>
      </n-empty>
    </section>

    <!-- Import flow shared with the novels library: URL → preview → confirm. -->
    <ImportUrlDialog
      :open="urlOpen"
      media="comics"
      :preview-fn="api.comics.previewFromUrl"
      @update:open="urlOpen = $event"
      @preview="onUrlPreviewed"
    />

    <ImportUrlConfirmDialog
      :open="confirmOpen"
      :preview="preview"
      media="comics"
      :import-fn="importComic"
      @update:open="confirmOpen = $event"
      @imported="reload"
      @back="onBackToUrlDialog"
    />

    <!-- ZIP import: one folder per chapter, images inside. -->
    <n-modal v-model:show="zipOpen" preset="card" title="Importar cómic desde ZIP" style="max-width: 26rem">
      <div class="stack-sm">
        <p class="muted small">
          Un ZIP con una carpeta por capítulo («Capitulo 001/001.jpg»), y opcionalmente
          metadata.json y cover.png en la raíz.
        </p>
        <input type="file" accept=".zip" class="cl-file" @change="onZipSelected" />
        <n-progress
          v-if="zipImporting"
          type="line"
          :percentage="100"
          :show-indicator="false"
          processing
        />
        <n-alert v-if="importError" type="error" :bordered="false">{{ importError }}</n-alert>
      </div>
    </n-modal>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import {
  NAlert,
  NButton,
  NEmpty,
  NIcon,
  NInput,
  NModal,
  NProgress,
  NSelect,
  useMessage,
} from "naive-ui";
import { ArchiveOutline, CloudDownloadOutline } from "@vicons/ionicons5";
import AppLayout from "@/components/AppLayout.vue";
import ComicCard from "@/components/ComicCard.vue";
import LibrarySkeleton from "@/pages/LibrarySkeleton.vue";
import ImportUrlDialog from "@/features/import/ImportUrlDialog.vue";
import ImportUrlConfirmDialog from "@/features/import/ImportUrlConfirmDialog.vue";
import type { ImportOutcome, ImportUrlInput } from "@/features/import/media";
import { useAppServices } from "@/app/services";
import { getComicDisplayTitle, type Comic } from "@/domain";
import type { PreviewUrlResult } from "@/api/types";

const router = useRouter();
const { api } = useAppServices();
const message = useMessage();

const comics = ref<Comic[]>([]);
const loading = ref(true);
const query = ref("");
const sortField = ref<"title" | "created" | "lastRead">("title");

const sortOptions = [
  { label: "Título", value: "title" },
  { label: "Fecha Adición", value: "created" },
  { label: "Fecha Lectura", value: "lastRead" },
];

const urlOpen = ref(false);
const confirmOpen = ref(false);
const preview = ref<PreviewUrlResult | null>(null);
const zipOpen = ref(false);
const zipImporting = ref(false);
const importError = ref("");

function onUrlPreviewed(result: PreviewUrlResult) {
  preview.value = result;
  urlOpen.value = false;
  confirmOpen.value = true;
}

function onBackToUrlDialog() {
  confirmOpen.value = false;
  urlOpen.value = true;
}

// The shared confirm dialog consumes a media-neutral outcome.
async function importComic(input: ImportUrlInput): Promise<ImportOutcome> {
  const result = await api.comics.importFromUrl({
    url: input.url,
    sourceLanguage: input.sourceLanguage ?? "en",
    targetLanguage: input.targetLanguage ?? "es",
    startChapter: input.startChapter ?? 1,
    endChapter: input.endChapter ?? 0,
  });
  return {
    id: result.comic.id,
    chaptersImported: result.chaptersImported,
    downloadJob: result.downloadJob,
  };
}

async function reload() {
  loading.value = true;
  try {
    const page = await api.comics.list({
      q: query.value.trim() || undefined,
      sort: sortField.value,
      // Convención de la API: para created/lastRead, "asc" ordena lo más
      // reciente primero.
      order: "asc",
      limit: 500,
    });
    comics.value = page.items;
  } finally {
    loading.value = false;
  }
}

async function onZipSelected(event: Event) {
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0];
  if (!file) return;
  zipImporting.value = true;
  importError.value = "";
  try {
    const result = await api.comics.importZip(file);
    zipOpen.value = false;
    message.success(`«${getComicDisplayTitle(result.comic)}» importado (${result.chaptersImported} capítulos).`);
    router.push(`/comics/${result.comic.id}`);
  } catch (err) {
    importError.value = err instanceof Error ? err.message : "No se pudo importar el ZIP.";
  } finally {
    zipImporting.value = false;
    input.value = "";
  }
}

onMounted(reload);
</script>

<style scoped>
.cl-page {
  display: flex;
  flex-direction: column;
  gap: 1rem;
  padding: 1.25rem 0 3rem;
}

.cl-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 0.75rem;
}

.cl-title {
  font-size: 1.5rem;
  font-weight: 700;
  letter-spacing: -0.02em;
  margin: 0;
}

.cl-toolbar {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  flex-wrap: wrap;
}

.cl-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(132px, 1fr));
  gap: 1rem;
}

.cl-empty {
  padding: 3rem 0;
}

.cl-file {
  font-size: 0.875rem;
}
</style>
