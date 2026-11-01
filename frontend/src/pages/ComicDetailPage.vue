<template>
  <AppLayout>
    <div v-if="!comic && loading" class="comic-detail-layout">
      <aside class="comic-sidebar">
        <n-skeleton width="100%" height="320px" style="border-radius: 12px" />
        <div class="comic-sidebar-actions">
          <n-skeleton width="100%" height="2.5rem" style="border-radius: 8px" />
          <n-skeleton width="100%" height="2.5rem" style="border-radius: 8px" />
        </div>
        <div class="comic-sidebar-tags">
          <n-skeleton width="5rem" height="1.5rem" round />
          <n-skeleton width="6rem" height="1.5rem" round />
          <n-skeleton width="7rem" height="1.5rem" round />
        </div>
      </aside>

      <div class="comic-main">
        <header class="comic-main-header">
          <n-skeleton width="60%" height="2rem" style="margin-bottom: 0.5rem" />
          <n-skeleton width="30%" height="1rem" style="margin-bottom: 0.75rem" />
          <n-skeleton width="20%" height="0.875rem" />
          <n-skeleton width="90%" height="0.875rem" style="margin-top: 1rem" />
          <n-skeleton width="75%" height="0.875rem" style="margin-top: 0.5rem" />
        </header>
        <n-skeleton width="20rem" height="2rem" style="border-radius: 8px" />
        <div class="stack-sm">
          <div
            v-for="i in 6"
            :key="i"
            style="display: flex; gap: 0.75rem; align-items: center; padding: 0.5rem 0; border-bottom: 1px solid var(--divide)"
          >
            <n-skeleton width="2rem" height="1rem" />
            <n-skeleton width="45%" height="1.1rem" />
            <n-skeleton width="6rem" height="1.4rem" />
          </div>
        </div>
      </div>
    </div>

    <div v-else-if="!comic" class="stack-md">
      <n-alert type="warning">Cómic no encontrado.</n-alert>
      <n-button secondary @click="router.push('/comics')">Volver</n-button>
    </div>

    <div v-else class="comic-detail-layout">
      <aside class="comic-sidebar">
        <div class="comic-cover-large">
          <img
            :src="coverSrc(comic.coverPath)"
            :alt="`Portada de ${getComicDisplayTitle(comic)}`"
            loading="lazy"
            @error="onCoverError"
          />
        </div>

        <div class="comic-sidebar-actions">
          <n-button type="primary" block :disabled="!hasChapters" @click="onRead">
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
          <n-button
            v-if="comic.canUpdate"
            secondary
            block
            :loading="checking"
            @click="runUpdateCheck"
          >
            <template #icon><n-icon><CloudDownloadOutline /></n-icon></template>
            Buscar nuevos capítulos
          </n-button>
          <span class="sidebar-actions-group" role="group" aria-label="Acciones del cómic">
            <n-button class="sidebar-actions-main" secondary @click="toggleVisibility">
              <template #icon><n-icon><ShareSocialOutline /></n-icon></template>
              {{ comic.isPublic ? "Dejar de compartir" : "Compartir" }}
            </n-button>
            <n-dropdown
              trigger="click"
              placement="bottom-end"
              :options="sidebarMenuOptions"
              @select="handleSidebarMenuSelect"
            >
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
        </div>

        <div class="comic-sidebar-tags">
          <n-tag :type="comicStatusType(comic.status)" size="small" round>
            {{ statusLabels[comic.status] || comic.status }}
          </n-tag>
          <n-tag size="small" round>{{ stats.totalChapters }} capítulos</n-tag>
          <n-tag size="small" round>{{ stats.downloadedChapters }} descargados</n-tag>
          <n-tag v-if="stats.failedChapters" type="error" size="small" round>
            {{ stats.failedChapters }} fallidos
          </n-tag>
          <n-tag type="success" size="small" round>
            {{ comic.sourceLanguage }} → {{ comic.targetLanguage }}
          </n-tag>
        </div>
      </aside>

      <div class="comic-main">
        <header class="comic-main-header">
          <h1 class="comic-title">{{ getComicDisplayTitle(comic) }}</h1>
          <div class="comic-meta">
            <span v-if="getComicDisplayAuthor(comic)" class="muted">{{ getComicDisplayAuthor(comic) }}</span>
            <span
              v-if="getComicDisplaySeries(comic) || getComicDisplayNumber(comic)"
              class="comic-series muted small"
            >
              <n-icon :size="14"><BookmarkOutline /></n-icon>
              <span v-if="getComicDisplaySeries(comic)">{{ getComicDisplaySeries(comic) }}</span>
              <span v-if="getComicDisplaySeries(comic) && getComicDisplayNumber(comic)">·</span>
              <span v-if="getComicDisplayNumber(comic)">#{{ getComicDisplayNumber(comic) }}</span>
            </span>
            <span v-if="comic.url" class="muted small">{{ hostOf(comic.url) }}</span>
            <n-tag v-if="comic.isPublic" size="small" :bordered="false">Compartido</n-tag>
          </div>
          <DescriptionBlock
            :description="descriptionText"
            :reset-key="comic.id"
          />
          <div v-if="comic.tags?.length" class="comic-description-tags">
            <n-tag v-for="tag in comic.tags" :key="tag" size="small" :bordered="false">
              {{ tag }}
            </n-tag>
          </div>
        </header>

        <n-tabs v-model:value="activeTab" type="bar" animated>
          <n-tab name="chapters">Capítulos</n-tab>
          <n-tab name="jobs">Trabajos</n-tab>
        </n-tabs>

        <div v-show="activeTab === 'chapters'" class="stack-md tab-panel" aria-labelledby="tab-chapters">
          <div class="comic-chapters-toolbar">
            <n-input
              v-model:value="chapterQuery"
              size="small"
              clearable
              placeholder="Buscar capítulo…"
              style="max-width: 18rem"
            />
            <n-button
              v-if="excludedChapters.length"
              size="small"
              quaternary
              @click="showExcluded = !showExcluded"
            >
              {{ showExcluded ? "Ocultar" : "Ver" }} excluidos ({{ excludedChapters.length }})
            </n-button>
          </div>

          <ul class="comic-chapter-list">
            <li v-for="ch in visibleChapters" :key="ch.id" class="comic-chapter-row">
              <span class="comic-chapter-order muted">{{ chapterPosition(ch) }}</span>
              <RouterLink
                :to="`/comics/${comic.id}/chapters/${ch.id}`"
                class="comic-chapter-title"
                :class="{ 'comic-chapter-title--excluded': ch.excluded }"
              >
                {{ ch.title || `Capítulo ${ch.chapterOrder}` }}
              </RouterLink>
              <span class="comic-chapter-pages muted small">{{ ch.pageCount }} pág.</span>
              <n-tag :type="chapterStatusType(ch.status)" size="small" :bordered="false">
                {{ statusLabels[ch.status] || ch.status }}
              </n-tag>
              <n-button
                v-if="ch.excluded"
                size="tiny"
                quaternary
                aria-label="Restaurar capítulo"
                @click="restoreChapter(ch)"
              >
                Restaurar
              </n-button>
              <n-button
                v-else
                size="tiny"
                quaternary
                type="error"
                aria-label="Excluir capítulo"
                @click="excludeChapter(ch)"
              >
                Excluir
              </n-button>
            </li>
          </ul>
        </div>

        <div v-show="activeTab === 'jobs'" class="tab-panel" aria-labelledby="tab-jobs">
          <ul class="comic-jobs-list">
            <li v-for="job in jobs" :key="job.id" class="comic-job-row">
              <n-tag size="small" :bordered="false">{{ jobLabel(job) }}</n-tag>
              <n-tag :type="jobStatusType(job.status)" size="small" :bordered="false">
                {{ job.status }}
              </n-tag>
              <span class="muted small">
                {{ job.completedChapters ?? 0 }}/{{ job.totalChapters ?? 0 }}
                <template v-if="job.errorMessage"> · {{ job.errorMessage }}</template>
              </span>
              <span class="muted small">{{ formatTime(job.createdAt) }}</span>
            </li>
            <li v-if="!jobs.length" class="muted small">Sin trabajos todavía.</li>
          </ul>
        </div>
      </div>
    </div>

    <!-- Update preview modal -->
    <n-modal
      v-model:show="updatePreviewOpen"
      preset="card"
      title="Nuevos capítulos"
      style="max-width: 28rem"
    >
      <div v-if="updatePreview" class="stack-sm">
        <p class="muted">
          {{ updatePreview.title }} — {{ updatePreview.newChapters }} capítulo(s) nuevo(s)
          ({{ updatePreview.currentChapters }} → {{ updatePreview.totalChapters }}).
        </p>
        <div style="display: flex; justify-content: flex-end; gap: 0.5rem">
          <n-button quaternary @click="updatePreviewOpen = false">Cancelar</n-button>
          <n-button type="primary" :loading="updating" @click="confirmUpdate">
            Descargar novedades
          </n-button>
        </div>
      </div>
    </n-modal>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { useRoute, useRouter, RouterLink } from "vue-router";
import {
  NAlert,
  NButton,
  NDropdown,
  NIcon,
  NInput,
  NModal,
  NSkeleton,
  NTabs,
  NTab,
  NTag,
  useDialog,
  useMessage,
} from "naive-ui";
import {
  BookOutline,
  BookmarkOutline,
  ChevronDownOutline,
  CloudDownloadOutline,
  ShareSocialOutline,
} from "@vicons/ionicons5";
import AppLayout from "@/components/AppLayout.vue";
import DescriptionBlock from "@/features/novels/DescriptionBlock.vue";
import { useAppServices } from "@/app/services";
import { setDocumentTitle } from "@/composables/useDocumentTitle";
import { readingStatusMenuOptions, useReadingStatus } from "@/composables/useReadingStatus";
import {
  chapterPosition,
  getComicDisplayAuthor,
  getComicDisplayNumber,
  getComicDisplaySeries,
  getComicDisplayTitle,
  type Comic,
  type ComicChapterStats,
  type ComicChapterSummary,
  type ReadingStatus,
} from "@/domain";
import { coverSrc, onCoverError } from "@/utils/cover";

const route = useRoute();
const router = useRouter();
const { api } = useAppServices();
const dialog = useDialog();
const message = useMessage();

const comic = ref<Comic | null>(null);
const chapters = ref<ComicChapterSummary[]>([]);
const stats = ref<ComicChapterStats>({ totalChapters: 0, downloadedChapters: 0, failedChapters: 0, maxChapterOrder: 0 });
type ComicJobRow = { id: string; operation: string; status: string; completedChapters?: number; totalChapters?: number; errorMessage?: string; createdAt?: string };
const jobs = ref<ComicJobRow[]>([]);
const loading = ref(true);
const activeTab = ref("chapters");
const chapterQuery = ref("");
const showExcluded = ref(false);
const checking = ref(false);
const updating = ref(false);
const updatePreviewOpen = ref(false);
const updatePreview = ref<{
  title: string;
  newChapters: number;
  currentChapters: number;
  totalChapters: number;
} | null>(null);

const statusLabels: Record<string, string> = {
  pending: "Pendiente",
  processing: "Descargando",
  done: "Descargado",
  failed: "Fallido",
  partial: "Parcial",
  ongoing: "En curso",
  completed: "Completo",
  hiatus: "Pausa",
  cancelled: "Abandonado",
};

function comicStatusType(status: string): "success" | "info" | "warning" | "error" {
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

function chapterStatusType(status: string): "default" | "success" | "error" | "warning" {
  switch (status) {
    case "done":
      return "success";
    case "failed":
      return "error";
    case "processing":
    case "partial":
      return "warning";
    default:
      return "default";
  }
}

function jobStatusType(status: string) {
  if (status === "done") return "success" as const;
  if (status === "failed") return "error" as const;
  if (status === "running") return "info" as const;
  return "default" as const;
}

function jobLabel(job: { operation: string }): string {
  switch (job.operation) {
    case "download":
      return "Descarga";
    case "check":
      return "Verificación";
    default:
      return job.operation;
  }
}

function formatTime(value?: string): string {
  if (!value) return "";
  try {
    return new Date(value).toLocaleString();
  } catch {
    return value;
  }
}

const hasChapters = computed(() => chapters.value.some((ch) => !ch.excluded && ch.hasPages));
const hasFailedChapters = computed(() => chapters.value.some((ch) => ch.status === "failed" || ch.status === "partial"));
const excludedChapters = computed(() => chapters.value.filter((ch) => ch.excluded));

const visibleChapters = computed(() => {
  const query = chapterQuery.value.trim().toLowerCase();
  let list = chapters.value;
  if (showExcluded.value) {
    // both visible and excluded, excluded at the end
    list = [...list.filter((c) => !c.excluded), ...list.filter((c) => c.excluded)];
  }
  if (!query) return list;
  return list.filter(
    (ch) =>
      ch.title.toLowerCase().includes(query) ||
      String(ch.chapterOrder).includes(query),
  );
});

const descriptionText = computed(() => {
  const text = comic.value?.targetDescription || comic.value?.sourceDescription || "";
  return text.trim();
});

const sidebarMenuOptions = computed(() => [
  { label: "Re-descargar fallidos", key: "redownload", disabled: !hasFailedChapters.value },
  { label: "Eliminar", key: "delete" },
]);

const { setStatus: setReadingStatus, clearProgress: clearReadingProgress } = useReadingStatus();

const readingMenuOptions = computed(() =>
  comic.value ? readingStatusMenuOptions("comic", comic.value) : [],
);

const readingStatusLabel = computed(() => {
  switch (comic.value?.readingStatus) {
    case "finished":
      return "Leído";
    case "paused":
      return "En pausa";
    case "reading":
      return "Leyendo";
    default:
      return "Estado de lectura";
  }
});

function handleReadingMenuSelect(key: string) {
  if (!comic.value) return;
  const comicId = comic.value.id;
  if (key === "clear") {
    dialog.warning({
      title: "Quitar progreso",
      content: `¿Quitar el progreso de lectura de "${getComicDisplayTitle(comic.value)}"? Se olvidará tu posición y sus marcas.`,
      positiveText: "Quitar",
      negativeText: "Cancelar",
      onPositiveClick: async () => {
        try {
          await clearReadingProgress("comic", comicId);
          if (comic.value) {
            comic.value.readingStatus = undefined;
            comic.value.lastReadAt = undefined;
          }
        } catch {
          message.error("No se pudo quitar el progreso.");
        }
      },
    });
    return;
  }
  const status = key as ReadingStatus;
  void setReadingStatus("comic", comicId, status)
    .then(() => {
      if (comic.value) comic.value.readingStatus = status;
    })
    .catch(() => {
      message.error("No se pudo actualizar el estado de lectura.");
    });
}

function handleSidebarMenuSelect(key: string) {
  switch (key) {
    case "redownload":
      void redownloadFailed();
      break;
    case "delete":
      confirmDelete();
      break;
  }
}

function hostOf(url: string): string {
  try {
    return new URL(url).hostname.replace(/^www\./, "");
  } catch {
    return url;
  }
}

async function loadAll() {
  const comicId = route.params.comicId as string;
  loading.value = true;
  try {
    const [c, s, j] = await Promise.all([
      api.comics.get(comicId),
      api.comics.chapterStats(comicId).catch(() => null),
      api.comics.jobs(comicId).catch(() => []),
    ]);
    comic.value = c;
    stats.value = s ?? { totalChapters: 0, downloadedChapters: 0, failedChapters: 0, maxChapterOrder: 0 };
    jobs.value = j as ComicJobRow[];
    if (c) {
      setDocumentTitle(getComicDisplayTitle(c));
      const list = await api.comics.chapters(comicId);
      chapters.value = (Array.isArray(list) ? list : []) as ComicChapterSummary[];
    }
  } finally {
    loading.value = false;
  }
}

function onRead() {
  if (!comic.value) return;
  router.push(`/comics/${comic.value.id}/read`);
}

async function runUpdateCheck() {
  if (!comic.value) return;
  checking.value = true;
  try {
    const preview = await api.comics.checkPreview(comic.value.id);
    updatePreview.value = preview;
    updatePreviewOpen.value = true;
    await loadAll();
  } catch (err) {
    message.error(err instanceof Error ? err.message : "No se pudo verificar el sitio.");
  } finally {
    checking.value = false;
  }
}

async function confirmUpdate() {
  if (!comic.value || !updatePreview.value) return;
  if (updatePreview.value.newChapters === 0) {
    updatePreviewOpen.value = false;
    message.info("Ya estás al día.");
    return;
  }
  updating.value = true;
  try {
    const result = await api.comics.updateFromUrl(comic.value.id, {});
    updatePreviewOpen.value = false;
    message.success(`Descarga encolada: ${result.chaptersQueued} capítulo(s).`);
    await loadAll();
  } catch (err) {
    message.error(err instanceof Error ? err.message : "No se pudo encolar la descarga.");
  } finally {
    updating.value = false;
  }
}

async function redownloadFailed() {
  if (!comic.value) return;
  try {
    const result = await api.comics.redownloadFromUrl(comic.value.id);
    message.success(`Re-descarga encolada: ${result.chaptersQueued} capítulo(s).`);
  } catch (err) {
    message.error(err instanceof Error ? err.message : "No se pudo encolar la re-descarga.");
  }
}

async function toggleVisibility() {
  if (!comic.value) return;
  try {
    comic.value = await api.comics.setVisibility(comic.value.id, !comic.value.isPublic);
  } catch {
    message.error("No se pudo cambiar la visibilidad.");
  }
}

function confirmDelete() {
  if (!comic.value) return;
  dialog.warning({
    title: "Eliminar cómic",
    content: `Se eliminará "${getComicDisplayTitle(comic.value)}" y todas sus páginas. Esta acción no se puede deshacer.`,
    positiveText: "Eliminar",
    negativeText: "Cancelar",
    onPositiveClick: async () => {
      if (!comic.value) return;
      await api.comics.delete(comic.value.id);
      message.success("Cómic eliminado.");
      router.push("/comics");
    },
  });
}

async function excludeChapter(ch: ComicChapterSummary) {
  if (!comic.value) return;
  await api.comics.setChapterExcluded(comic.value.id, ch.id, true);
  ch.excluded = true;
}

async function restoreChapter(ch: ComicChapterSummary) {
  if (!comic.value) return;
  await api.comics.setChapterExcluded(comic.value.id, ch.id, false);
  ch.excluded = false;
}

onMounted(loadAll);
watch(
  () => route.params.comicId,
  () => {
    if (route.name === "comic-detail") loadAll();
  },
);
</script>

<style scoped>
.comic-detail-layout {
  display: grid;
  grid-template-columns: minmax(150px, 200px) minmax(0, 1fr);
  gap: 1.5rem;
  align-items: start;
}

.comic-main {
  display: flex;
  flex-direction: column;
  gap: 1rem;
  min-width: 0;
}

.comic-main-header {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.comic-title {
  margin: 0;
  font-size: 1.625rem;
  font-weight: 700;
  line-height: 1.15;
  letter-spacing: -0.02em;
}

.comic-meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.25rem 0.875rem;
  font-size: 0.875rem;
}

.comic-series {
  display: inline-flex;
  align-items: center;
  gap: 0.35rem;
  color: var(--text-secondary);
}

.comic-series i {
  font-size: 0.8rem;
  color: var(--accent-link);
}

.comic-description-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 0.375rem;
  margin-top: 0.375rem;
}

.tab-panel {
  content-visibility: auto;
  contain-intrinsic-size: auto 400px;
}

.comic-sidebar {
  display: flex;
  flex-direction: column;
  gap: 0.875rem;
}

.comic-cover-large {
  border-radius: var(--radius-md);
  overflow: hidden;
  border: 1px solid var(--divide);
  background: var(--surface-muted);
}

.comic-cover-large img {
  width: 100%;
  height: auto;
  aspect-ratio: 2 / 3;
  object-fit: cover;
  display: block;
}

.comic-sidebar-actions {
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

.comic-sidebar-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}

.comic-chapters-toolbar {
  display: flex;
  align-items: center;
  gap: 0.75rem;
}

.comic-chapter-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
}

.comic-chapter-row {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  padding: 0.5rem 0.25rem;
  border-bottom: 1px solid var(--divide);
}

.comic-chapter-order {
  width: 2.5rem;
  text-align: right;
  flex-shrink: 0;
  font-variant-numeric: tabular-nums;
  font-size: 0.8125rem;
}

.comic-chapter-title {
  flex: 1;
  min-width: 0;
  font-size: 0.9375rem;
  color: var(--foreground);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.comic-chapter-title:hover {
  color: var(--accent-link);
}

.comic-chapter-title--excluded {
  text-decoration: line-through;
  opacity: 0.55;
}

.comic-chapter-pages {
  flex-shrink: 0;
}

.comic-jobs-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
}

.comic-job-row {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  padding: 0.5rem 0.25rem;
  border-bottom: 1px solid var(--divide);
}

@media (max-width: 768px) {
  .comic-detail-layout {
    grid-template-columns: 1fr;
    gap: 1rem;
  }

  .comic-sidebar {
    display: grid;
    grid-template-columns: 100px 1fr;
    gap: 0.75rem;
    align-items: start;
  }

  .comic-cover-large {
    max-width: 100px;
  }

  .comic-sidebar-actions {
    gap: 0.375rem;
  }

  .comic-sidebar-tags {
    grid-column: 1 / -1;
  }

  .comic-title {
    font-size: 1.375rem;
  }
}
</style>
