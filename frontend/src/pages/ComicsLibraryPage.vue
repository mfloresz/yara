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
          <n-button size="small" secondary @click="importOpen = true">
            <template #icon><n-icon><CloudDownloadOutline /></n-icon></template>
            Importar
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
          <n-button size="small" type="primary" secondary @click="importOpen = true">
            Importar cómic
          </n-button>
        </template>
      </n-empty>
    </section>

    <!-- Import dialog: URL preview → confirm, or ZIP upload -->
    <n-modal
      v-model:show="importOpen"
      preset="card"
      title="Importar cómic"
      style="max-width: 30rem"
    >
      <n-tabs type="line" animated>
        <n-tab name="url" tab="Desde URL">
          <div class="stack-sm">
            <n-input v-model:value="importUrl" placeholder="https://sitio-de-manga.com/serie/…" />
            <div class="row-end gap-sm">
              <n-button size="small" :loading="previewing" :disabled="!importUrl.trim()" @click="previewImport">
                Previsualizar
              </n-button>
            </div>

            <template v-if="preview">
              <div class="cl-preview">
                <img v-if="preview.coverURL" :src="coverSrc(preview.coverURL)" alt="" class="cl-preview-cover" @error="onCoverError" />
                <div class="cl-preview-meta">
                  <strong>{{ preview.title }}</strong>
                  <span v-if="preview.author" class="muted small">{{ preview.author }}</span>
                  <span class="muted small">{{ preview.totalChapters }} capítulos</span>
                </div>
              </div>
              <div class="row-wrap gap-sm">
                <n-select
                  v-model:value="sourceLang"
                  size="small"
                  :options="languageOptions"
                  style="width: 9rem"
                  aria-label="Idioma de origen"
                />
                <n-select
                  v-model:value="targetLang"
                  size="small"
                  :options="languageOptions"
                  style="width: 9rem"
                  aria-label="Idioma destino"
                />
              </div>
              <div class="row-end gap-sm">
                <n-button size="small" quaternary @click="importOpen = false">Cancelar</n-button>
                <n-button size="small" type="primary" :loading="importing" @click="confirmImport">
                  Importar y descargar
                </n-button>
              </div>
            </template>
            <n-alert v-if="importError" type="error" :bordered="false">{{ importError }}</n-alert>
          </div>
        </n-tab>

        <n-tab name="zip" tab="Desde ZIP">
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
          </div>
        </n-tab>
      </n-tabs>
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
  NTabs,
  NTab,
  useMessage,
} from "naive-ui";
import { CloudDownloadOutline } from "@vicons/ionicons5";
import AppLayout from "@/components/AppLayout.vue";
import ComicCard from "@/components/ComicCard.vue";
import LibrarySkeleton from "@/pages/LibrarySkeleton.vue";
import { useAppServices } from "@/app/services";
import { LANGUAGES } from "@/config/languages";
import { getComicDisplayTitle, type Comic } from "@/domain";
import { coverSrc, onCoverError } from "@/utils/cover";
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

const languageOptions = LANGUAGES.filter((l) => l.code !== "auto").map((l) => ({
  value: l.code,
  label: l.name,
}));

const importOpen = ref(false);
const importUrl = ref("");
const previewing = ref(false);
const importing = ref(false);
const zipImporting = ref(false);
const importError = ref("");
const preview = ref<PreviewUrlResult | null>(null);
const sourceLang = ref("en");
const targetLang = ref("es");

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

async function previewImport() {
  importError.value = "";
  preview.value = null;
  previewing.value = true;
  try {
    preview.value = await api.comics.previewFromUrl(importUrl.value.trim());
  } catch (err) {
    importError.value = err instanceof Error ? err.message : "No se pudo leer el sitio.";
  } finally {
    previewing.value = false;
  }
}

async function confirmImport() {
  if (!preview.value) return;
  importing.value = true;
  importError.value = "";
  try {
    const result = await api.comics.importFromUrl({
      url: importUrl.value.trim(),
      sourceLanguage: sourceLang.value,
      targetLanguage: targetLang.value,
      startChapter: 1,
      endChapter: 0,
    });
    importOpen.value = false;
    preview.value = null;
    importUrl.value = "";
    message.success(
      `«${getComicDisplayTitle(result.comic)}» importado; descargando ${result.downloadJob?.totalChapters ?? 0} capítulo(s).`,
    );
    router.push(`/comics/${result.comic.id}`);
  } catch (err) {
    importError.value = err instanceof Error ? err.message : "No se pudo importar.";
  } finally {
    importing.value = false;
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
    importOpen.value = false;
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

.cl-preview {
  display: flex;
  gap: 0.875rem;
  align-items: flex-start;
}

.cl-preview-cover {
  width: 64px;
  border-radius: var(--radius-sm);
  border: 1px solid var(--divide);
}

.cl-preview-meta {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
  font-size: 0.875rem;
}

.cl-file {
  font-size: 0.875rem;
}
</style>
