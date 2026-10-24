<template>
  <AppLayout>
    <div v-if="!comic && loading" class="comic-detail-loading">
      <n-skeleton width="220px" height="330px" style="border-radius: 12px" />
      <div style="flex: 1; min-width: 0">
        <n-skeleton width="60%" height="2rem" style="margin-bottom: 0.75rem" />
        <n-skeleton width="30%" height="1rem" style="margin-bottom: 1rem" />
        <n-skeleton v-for="i in 5" :key="i" width="85%" height="0.875rem" style="margin-bottom: 0.5rem" />
      </div>
    </div>

    <div v-else-if="!comic" class="stack-md">
      <n-alert type="warning">Cómic no encontrado.</n-alert>
      <n-button secondary @click="router.push('/library/comics')">Volver</n-button>
    </div>

    <div v-else class="comic-detail">
      <aside class="comic-sidebar">
        <div class="comic-cover">
          <img
            v-if="comic.coverPath"
            :src="coverSrc(comic.coverPath)"
            :alt="`Portada de ${getComicDisplayTitle(comic)}`"
            @error="onCoverError"
          />
          <div v-else class="comic-cover-empty" aria-hidden="true">
            <n-icon :size="40"><ImageOutline /></n-icon>
          </div>
        </div>

        <div class="comic-sidebar-actions">
          <n-button type="primary" block :disabled="!hasChapters" @click="onRead">
            <template #icon><n-icon><BookOutline /></n-icon></template>
            Leer
          </n-button>
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
          <n-button
            v-if="hasFailedChapters"
            secondary
            block
            :loading="redownloading"
            @click="redownloadFailed"
          >
            <template #icon><n-icon><RefreshOutline /></n-icon></template>
            Re-descargar fallidos
          </n-button>
          <n-button quaternary block @click="toggleVisibility">
            <template #icon><n-icon><ShareSocialOutline /></n-icon></template>
            {{ comic.isPublic ? "Dejar de compartir" : "Compartir" }}
          </n-button>
          <n-button quaternary block type="error" @click="confirmDelete">
            <template #icon><n-icon><TrashOutline /></n-icon></template>
            Eliminar
          </n-button>
        </div>

        <dl class="comic-sidebar-meta">
          <div class="meta-row">
            <dt>Capítulos</dt>
            <dd>{{ stats.totalChapters }}</dd>
          </div>
          <div class="meta-row">
            <dt>Descargados</dt>
            <dd>{{ stats.downloadedChapters }}</dd>
          </div>
          <div v-if="stats.failedChapters" class="meta-row">
            <dt>Fallidos</dt>
            <dd>{{ stats.failedChapters }}</dd>
          </div>
          <div v-if="comic.status" class="meta-row">
            <dt>Estado</dt>
            <dd>{{ statusLabels[comic.status] || comic.status }}</dd>
          </div>
        </dl>

        <div v-if="comic.tags?.length" class="comic-sidebar-tags">
          <n-tag v-for="tag in comic.tags" :key="tag" size="small" :bordered="false">
            {{ tag }}
          </n-tag>
        </div>
      </aside>

      <div class="comic-main">
        <header class="comic-main-header">
          <RouterLink to="/library/comics" class="comic-back">
            <n-icon :size="14"><ArrowBackOutline /></n-icon>
            Cómics
          </RouterLink>
          <h1 class="comic-title">{{ getComicDisplayTitle(comic) }}</h1>
          <div class="comic-meta">
            <span v-if="getComicDisplayAuthor(comic)" class="muted">{{ getComicDisplayAuthor(comic) }}</span>
            <span v-if="comic.url" class="muted small">{{ hostOf(comic.url) }}</span>
            <n-tag v-if="comic.isPublic" size="small" :bordered="false">Compartido</n-tag>
          </div>
          <p v-if="descriptionText" class="comic-description">
            {{ descriptionText }}
          </p>
        </header>

        <n-tabs v-model:value="activeTab" type="line" animated>
          <n-tab name="chapters" tab="Capítulos">
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
                <span class="comic-chapter-order muted">{{ ch.chapterOrder }}</span>
                <RouterLink
                  :to="`/comics/${comic.id}/chapters/${ch.id}`"
                  class="comic-chapter-title"
                  :class="{ 'comic-chapter-title--excluded': ch.excluded }"
                >
                  {{ ch.title || `Capítulo ${ch.chapterOrder}` }}
                </RouterLink>
                <span class="comic-chapter-pages muted small">{{ ch.pageCount }} pág.</span>
                <n-tag :type="statusTagType(ch.status)" size="small" :bordered="false">
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
          </n-tab>

          <n-tab name="jobs" tab="Trabajos">
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
          </n-tab>
        </n-tabs>
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
        <div class="row-end gap-sm">
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
  ArrowBackOutline,
  BookOutline,
  CloudDownloadOutline,
  ImageOutline,
  RefreshOutline,
  ShareSocialOutline,
  TrashOutline,
} from "@vicons/ionicons5";
import AppLayout from "@/components/AppLayout.vue";
import { useAppServices } from "@/app/services";
import { setDocumentTitle } from "@/composables/useDocumentTitle";
import {
  getComicDisplayAuthor,
  getComicDisplayTitle,
  type Comic,
  type ComicChapterStats,
  type ComicChapterSummary,
  type TranslationJob,
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
const redownloading = ref(false);
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

function statusTagType(status: string): "default" | "success" | "error" | "warning" | "info" {
  switch (status) {
    case "done":
    case "completed":
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
  redownloading.value = true;
  try {
    const result = await api.comics.redownloadFromUrl(comic.value.id);
    message.success(`Re-descarga encolada: ${result.chaptersQueued} capítulo(s).`);
  } catch (err) {
    message.error(err instanceof Error ? err.message : "No se pudo encolar la re-descarga.");
  } finally {
    redownloading.value = false;
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
      router.push("/library/comics");
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
.comic-detail-loading {
  display: flex;
  gap: 2rem;
  padding-top: 1.5rem;
}

.comic-detail {
  display: grid;
  grid-template-columns: 220px 1fr;
  gap: 2rem;
  padding: 1.5rem 0 3rem;
  align-items: start;
}

.comic-sidebar {
  display: flex;
  flex-direction: column;
  gap: 1rem;
  position: sticky;
  top: 1rem;
}

.comic-cover {
  aspect-ratio: 2 / 3;
  border-radius: var(--radius-md);
  overflow: hidden;
  border: 1px solid var(--divide);
  background: var(--surface-muted);
  box-shadow: 0 8px 20px rgba(0, 0, 0, 0.1);
}

.comic-cover img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.comic-cover-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100%;
  color: var(--text-secondary);
}

.comic-sidebar-actions {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.comic-sidebar-meta {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
  margin: 0;
  font-size: 0.8125rem;
}

.meta-row {
  display: flex;
  justify-content: space-between;
  gap: 1rem;
}

.meta-row dt {
  color: var(--text-secondary);
}

.meta-row dd {
  margin: 0;
  font-weight: 600;
}

.comic-sidebar-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 0.375rem;
}

.comic-main {
  min-width: 0;
}

.comic-back {
  display: inline-flex;
  align-items: center;
  gap: 0.375rem;
  font-size: 0.8125rem;
  color: var(--accent-link);
  margin-bottom: 0.75rem;
}

.comic-main-header {
  margin-bottom: 1.25rem;
}

.comic-title {
  font-size: 1.75rem;
  font-weight: 700;
  letter-spacing: -0.02em;
  margin: 0 0 0.375rem;
}

.comic-meta {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 0.625rem;
  font-size: 0.875rem;
  margin-bottom: 0.75rem;
}

.comic-description {
  font-size: 0.9375rem;
  line-height: 1.6;
  color: var(--text-secondary);
  white-space: pre-line;
  margin: 0;
  max-width: 60ch;
}

.comic-chapters-toolbar {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  margin: 0.75rem 0;
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
  margin: 0.75rem 0 0;
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

@media (max-width: 760px) {
  .comic-detail {
    grid-template-columns: 1fr;
  }

  .comic-sidebar {
    position: static;
    flex-direction: row;
    flex-wrap: wrap;
    align-items: flex-start;
  }

  .comic-cover {
    width: 132px;
    flex-shrink: 0;
  }

  .comic-sidebar-actions {
    flex: 1;
    min-width: 200px;
  }
}
</style>
