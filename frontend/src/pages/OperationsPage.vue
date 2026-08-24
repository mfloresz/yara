<template>
  <AppLayout>
    <n-space vertical size="large">
      <!-- Header -->
      <n-flex justify="space-between" align="center" wrap :size="12">
        <n-space vertical :size="2">
          <n-h1 style="margin: 0; font-size: 1.35rem">Operaciones</n-h1>
          <n-text depth="3" style="font-size: 13px">Verifica, descarga y traduce solo lo que selecciones.</n-text>
        </n-space>
        <n-space :size="8" wrap>
          <n-tag round size="small" type="info">{{ filteredNovels.length }} en vista</n-tag>
          <n-tag v-if="actualizableCount > 0" round size="small" type="warning">{{ actualizableCount }} actualizables</n-tag>
          <n-tag v-if="activeCheckJobs.length > 0" round size="small" type="info">{{ activeCheckJobs.length }} verificando</n-tag>
          <n-tag v-if="activeDownloadCount > 0" round size="small" type="warning">{{ activeDownloadCount }} descargando</n-tag>
          <n-tag v-if="activeTranslateCount > 0" round size="small" type="success">{{ activeTranslateCount }} traduciendo</n-tag>
        </n-space>
      </n-flex>

      <!-- Toolbar granular -->
      <n-card size="small" content-style="padding: 10px 12px;">
        <n-flex justify="space-between" align="center" wrap :size="12">
          <n-flex :size="8" wrap align="center">
            <n-input
              v-model:value="searchQuery"
              clearable
              placeholder="Buscar título o autor"
              style="width: 210px"
              size="small"
            >
              <template #prefix><n-icon><SearchOutline /></n-icon></template>
            </n-input>
            <n-select
              v-model:value="filter"
              :options="filterOptions"
              style="width: 170px"
              size="small"
            />
            <n-button size="tiny" quaternary @click="selectActualizable">Seleccionar actualizables</n-button>
            <n-button v-if="selectedRowKeys.length > 0" size="tiny" quaternary @click="selectedRowKeys = []">Limpiar</n-button>
          </n-flex>

          <n-flex :size="8" wrap align="center">
            <n-tooltip trigger="hover">
              <template #trigger>
                <n-button
                  size="tiny"
                  secondary
                  :loading="checkingSelected"
                  :disabled="selectedActualizableIds.length === 0"
                  @click="handleCheckSelected"
                >
                  <template #icon><n-icon><RefreshOutline /></n-icon></template>
                  Verificar{{ selectedActualizableIds.length ? ` (${selectedActualizableIds.length})` : '' }}
                </n-button>
              </template>
              Verifica novedades solo en las seleccionadas
            </n-tooltip>

            <n-tooltip trigger="hover">
              <template #trigger>
                <n-button
                  size="tiny"
                  :loading="bulkDownloading"
                  :disabled="selectedWithUpdatesIds.length === 0"
                  @click="handleDownloadSelected"
                >
                  <template #icon><n-icon><DownloadOutline /></n-icon></template>
                  Descargar{{ selectedWithUpdatesIds.length ? ` (${selectedWithUpdatesIds.length})` : '' }}
                </n-button>
              </template>
              Descarga los capítulos nuevos ya detectados
            </n-tooltip>

            <n-tooltip trigger="hover">
              <template #trigger>
                <n-button
                  size="tiny"
                  type="primary"
                  :loading="translatingSelected"
                  :disabled="selectedTranslatableIds.length === 0"
                  @click="handleTranslateSelected"
                >
                  <template #icon><n-icon><PlayOutline /></n-icon></template>
                  Traducir{{ selectedTranslatableIds.length ? ` (${selectedTranslatableIds.length})` : '' }}
                </n-button>
              </template>
              Traduce los pendientes de las seleccionadas
            </n-tooltip>
          </n-flex>
        </n-flex>
      </n-card>

      <n-card v-if="loading" content-style="padding: 12px;">
        <n-space vertical :size="8">
          <n-skeleton v-for="i in 6" :key="i" height="38px" :sharp="false" />
        </n-space>
      </n-card>

      <n-alert v-else-if="error" type="error" :title="error" show-icon closable @close="error = null" />

      <n-empty v-else-if="filteredNovels.length === 0" description="No hay novelas para este filtro" style="padding: 1.5rem 0" />

      <template v-else>
        <n-data-table
          :columns="columns"
          :data="filteredNovels"
          :row-key="(row: Novel) => row.id"
          :checked-row-keys="selectedRowKeys"
          :pagination="pagination"
          :bordered="false"
          single-line
          size="small"
          :row-props="rowProps"
          @update:checked-row-keys="selectedRowKeys = $event as string[]"
        />

        <n-card v-if="selectedRowKeys.length > 0" size="small" style="position: sticky; bottom: 12px; z-index: 1" content-style="padding: 10px 12px;">
          <n-flex justify="space-between" align="center" wrap :size="10">
            <n-text depth="3" style="font-size: 12px">
              {{ selectedRowKeys.length }} sel. · {{ selectedWithUpdatesIds.length }} con novedades · {{ selectedTranslatableIds.length }} por traducir · {{ selectedActualizableIds.length }} verificables
            </n-text>
            <n-flex :size="8">
              <n-button size="tiny" secondary :loading="checkingSelected" :disabled="selectedActualizableIds.length === 0" @click="handleCheckSelected">Verificar</n-button>
              <n-button size="tiny" :loading="bulkDownloading" :disabled="selectedWithUpdatesIds.length === 0" @click="handleDownloadSelected">Descargar</n-button>
              <n-button size="tiny" type="primary" :loading="translatingSelected" :disabled="selectedTranslatableIds.length === 0" @click="handleTranslateSelected">Traducir</n-button>
            </n-flex>
          </n-flex>
        </n-card>
      </template>
    </n-space>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, h, onMounted, reactive, ref, watch } from "vue";
import {
  NAlert,
  NButton,
  NCard,
  NDataTable,
  NEmpty,
  NFlex,
  NH1,
  NIcon,
  NInput,
  NSelect,
  NSkeleton,
  NSpace,
  NTag,
  NText,
  NTooltip,
  NAvatar,
  NEllipsis,
  useMessage,
  type DataTableColumns,
} from "naive-ui";
import {
  DownloadOutline,
  GlobeOutline,
  PlayOutline,
  RefreshOutline,
  SearchOutline,
} from "@vicons/ionicons5";
import AppLayout from "@/components/AppLayout.vue";
import { useAppServices } from "@/app/services";
import { useActiveJobs } from "@/composables/useActiveJobs";
import { jobStatusLabel } from "@/composables/useJobHelpers";
import { emitJobChanged } from "@/utils/job-events";
import type { Novel, TranslationJob } from "@/domain";

const PAGE_SIZE = 200;
const PREVIEW_CACHE_TTL_MS = 15 * 60 * 1000;

type FilterValue = "all" | "actualizable" | "updates" | "completed" | "active";

const message = useMessage();
const { api } = useAppServices();
const { jobs: activeJobs } = useActiveJobs();

const loading = ref(true);
const error = ref<string | null>(null);
const novels = ref<Novel[]>([]);
const filter = ref<FilterValue>("all");
const searchQuery = ref("");
const selectedRowKeys = ref<string[]>([]);

const checkingSelected = ref(false);
const bulkDownloading = ref(false);
const translatingSelected = ref(false);

const updateResults = ref<Map<string, { added: number; error?: string }>>(new Map());

const filterOptions = [
  { label: "Todas", value: "all" },
  { label: "Actualizable", value: "actualizable" },
  { label: "Con novedades", value: "updates" },
  { label: "Activas", value: "active" },
  { label: "Completadas", value: "completed" },
];

const pagination = reactive({
  page: 1,
  pageSize: 30,
  showSizePicker: true,
  pageSizes: [25, 50, 100],
  onUpdatePage: (page: number) => {
    pagination.page = page;
  },
  onUpdatePageSize: (pageSize: number) => {
    pagination.pageSize = pageSize;
    pagination.page = 1;
  },
});

function activeJobForNovel(novelId: string, operation: string): TranslationJob | undefined {
  return activeJobs.value.find((j) => j.novelId === novelId && j.operation === operation);
}

function hasAnyActive(novelId: string): boolean {
  return !!activeJobForNovel(novelId, "check") || !!activeJobForNovel(novelId, "download") || !!activeJobForNovel(novelId, "translate") || !!activeJobForNovel(novelId, "refine");
}

const activeCheckJobs = computed(() => activeJobs.value.filter((j) => j.operation === "check"));
const activeDownloadCount = computed(() => activeJobs.value.filter((j) => j.operation === "download").length);
const activeTranslateCount = computed(() => activeJobs.value.filter((j) => j.operation === "translate" || j.operation === "refine").length);

function isActualizable(novel: Novel): boolean {
  return novel.canUpdate;
}

function isCheckStale(novel: Novel): boolean {
  if (!novel.lastCheckedAt) return true;
  const checkedAt = new Date(novel.lastCheckedAt).getTime();
  return Date.now() - checkedAt > PREVIEW_CACHE_TTL_MS;
}

function persistedCheckLabel(novel: Novel): string {
  if (!novel.lastCheckedAt || isCheckStale(novel)) return "";
  if ((novel.lastCheckNewChapters ?? 0) === 0) return "Al día";
  return `+${novel.lastCheckNewChapters}`;
}

function hasNewChapters(novel: Novel): boolean {
  if (!isCheckStale(novel)) return (novel.lastCheckNewChapters ?? 0) > 0;
  return false;
}

function hasPendingTranslation(novel: Novel): boolean {
  return novel.chapterCount > 0 && novel.translatedCount < novel.chapterCount;
}

function updateResultsLabel(novel: Novel): string {
  const r = updateResults.value.get(novel.id);
  if (!r) return "";
  if (r.error) return "Error";
  if (r.added === 0) return "Al día";
  return `+${r.added} descargados`;
}

const actualizableCount = computed(() => novels.value.filter((n) => isActualizable(n) && n.status !== "completed").length);

const filteredNovels = computed(() => {
  const q = searchQuery.value.trim().toLowerCase();
  let list = novels.value;
  if (q) {
    list = list.filter((n) => n.sourceTitle.toLowerCase().includes(q) || n.sourceAuthor.toLowerCase().includes(q));
  }
  switch (filter.value) {
    case "actualizable":
      return list.filter((n) => isActualizable(n) && n.status !== "completed");
    case "updates":
      return list.filter((n) => hasNewChapters(n));
    case "active":
      return list.filter((n) => hasAnyActive(n.id));
    case "completed":
      return list.filter((n) => n.status === "completed");
    default:
      return list;
  }
});

const selectedNovels = computed(() => selectedRowKeys.value.map((id) => novels.value.find((n) => n.id === id)).filter(Boolean) as Novel[]);

const selectedActualizableIds = computed(() => selectedNovels.value.filter((n) => isActualizable(n) && n.status !== "completed" && !hasAnyActive(n.id)).map((n) => n.id));
const selectedWithUpdatesIds = computed(() => selectedNovels.value.filter((n) => hasNewChapters(n) && !hasAnyActive(n.id)).map((n) => n.id));
const selectedTranslatableIds = computed(() => selectedNovels.value.filter((n) => hasPendingTranslation(n) && !hasAnyActive(n.id)).map((n) => n.id));

function selectActualizable() {
  selectedRowKeys.value = filteredNovels.value.filter((n) => isActualizable(n) && n.status !== "completed" && !hasAnyActive(n.id)).map((n) => n.id);
  if (selectedRowKeys.value.length === 0) message.info("No hay novelas actualizables seleccionables en esta vista.");
}

watch([filter, searchQuery], () => {
  pagination.page = 1;
});

const rowProps = () => ({ style: "height: 44px;" });

const columns: DataTableColumns<Novel> = [
  { type: "selection", width: 36 },
  {
    title: "Novela",
    key: "sourceTitle",
    sorter: (a, b) => a.sourceTitle.localeCompare(b.sourceTitle),
    render(row) {
      const needsBrowser = row.requiresBrowser === true;
      return h(
        NFlex,
        { align: "center", wrap: false, size: 8, style: "min-width:0" },
        {
          default: () => [
            row.coverPath
              ? h(NAvatar, { size: 28, src: row.coverPath, style: "border-radius: 6px; flex-shrink:0" })
              : h(NAvatar, { size: 28, style: "border-radius: 6px; flex-shrink:0; font-size: 10px" }, { default: () => row.sourceTitle.slice(0, 2).toUpperCase() }),
            h(
              NFlex,
              { align: "center", wrap: false, size: 4, style: "min-width:0; flex:1" },
              {
                default: () => [
                  h(NEllipsis, { style: "max-width: 100%; font-weight: 600; font-size: 13px" }, { default: () => row.sourceTitle }),
                  needsBrowser
                    ? h(
                        NTooltip,
                        {},
                        {
                          trigger: () =>
                            h(NIcon, { size: 13, style: "color: var(--warning); flex-shrink:0; cursor: help", component: GlobeOutline } as unknown as Record<string, unknown>),
                          default: () => "Requiere extensión de navegador (Cloudflare). Instala el worker para verificar/descargar.",
                        },
                      )
                    : null,
                ],
              },
            ),
          ],
        },
      );
    },
  },
  {
    title: () =>
      h(
        NFlex,
        { justify: "center", size: 4, align: "center", wrap: false },
        {
          default: () => [
            h("span", { style: "font-size: 12px; font-weight: 600" }, "Estado"),
            h(NText, { depth: "3", style: "font-size: 11px" }, { default: () => "·" }),
            h(NText, { depth: "3", style: "font-size: 11px" }, { default: () => String(filteredNovels.value.length) }),
          ],
        },
      ),
    key: "status",
    width: 96,
    align: "center",
    render(row) {
      const downloadJob = activeJobForNovel(row.id, "download");
      const translateJob = activeJobForNovel(row.id, "translate") || activeJobForNovel(row.id, "refine");
      const checkJob = activeJobForNovel(row.id, "check");
      if (downloadJob) return h(NTag, { type: "warning", size: "tiny", round: true }, { default: () => jobStatusLabel(downloadJob) });
      if (checkJob) return h(NTag, { type: "info", size: "tiny", round: true }, { default: () => jobStatusLabel(checkJob) });
      if (translateJob) return h(NTag, { type: "info", size: "tiny", round: true }, { default: () => jobStatusLabel(translateJob) });
      if (updateResults.value.has(row.id)) {
        const r = updateResults.value.get(row.id)!;
        return h(NTag, { type: r.error ? "error" : "success", size: "tiny", round: true }, { default: () => updateResultsLabel(row) });
      }
      const checkLabel = persistedCheckLabel(row);
      if (checkLabel) return h(NTag, { type: checkLabel === "Al día" ? "success" : "info", size: "tiny", round: true }, { default: () => checkLabel });
      if (isActualizable(row) && row.status !== "completed") return h(NTag, { type: "warning", size: "tiny", round: true }, { default: () => "Actualizable" });
      if (row.status === "completed") return h(NTag, { type: "success", size: "tiny", round: true }, { default: () => "Completada" });
      if (hasPendingTranslation(row)) return h(NTag, { type: "info", size: "tiny", round: true }, { default: () => `${row.chapterCount - row.translatedCount} pend.` });
      return h(NTag, { size: "tiny", round: true }, { default: () => "Al día" });
    },
  },
  {
    title: "Acciones",
    key: "actions",
    width: 330,
    align: "right",
    render(row) {
      const translateJob = activeJobForNovel(row.id, "translate") || activeJobForNovel(row.id, "refine");
      const downloadJob = activeJobForNovel(row.id, "download");
      const checkJob = activeJobForNovel(row.id, "check");
      const canCheck = isActualizable(row) && row.status !== "completed";
      const canDownload = hasNewChapters(row);
      const canTranslate = hasPendingTranslation(row);
      return h(
        NFlex,
        { justify: "end", size: 6, wrap: false, align: "center" },
        {
          default: () => [
            h(
              NButton,
              {
                size: "tiny",
                secondary: true,
                loading: !!checkJob,
                disabled: !canCheck || (!!downloadJob || !!translateJob),
                style: "min-width: 92px",
                onClick: () => handleUpdateNovel(row),
              },
              {
                icon: () => h(NIcon, null, { default: () => h(RefreshOutline) }),
                default: () => (checkJob ? jobStatusLabel(checkJob) : downloadJob ? jobStatusLabel(downloadJob) : "Verificar"),
              },
            ),
            h(
              NButton,
              {
                size: "tiny",
                type: canDownload ? "warning" : "default",
                secondary: !canDownload || !!downloadJob,
                loading: !!downloadJob,
                disabled: (!canDownload && !downloadJob) || !!checkJob || !!translateJob,
                style: "min-width: 112px",
                onClick: () => handleDownloadSingle(row),
              },
              {
                icon: () => h(NIcon, null, { default: () => h(DownloadOutline) }),
                default: () => (downloadJob ? jobStatusLabel(downloadJob) : canDownload ? `Descargar +${row.lastCheckNewChapters}` : "Descargar"),
              },
            ),
            h(
              NButton,
              {
                size: "tiny",
                type: canTranslate ? "primary" : "default",
                secondary: !canTranslate,
                loading: !!translateJob,
                disabled: (!canTranslate && !translateJob) || !!checkJob || !!downloadJob,
                style: "min-width: 96px",
                onClick: () => handleTranslateNovel(row),
              },
              {
                icon: () => h(NIcon, null, { default: () => h(PlayOutline) }),
                default: () => (translateJob ? jobStatusLabel(translateJob) : "Traducir"),
              },
            ),
          ],
        },
      );
    },
  },
];

function updateNovelLocal(id: string, patch: Partial<Novel>) {
  const idx = novels.value.findIndex((n) => n.id === id);
  if (idx >= 0) novels.value[idx] = { ...novels.value[idx], ...patch };
}

watch(activeJobs, (current, prev) => {
  const prevIds = new Set(prev.map((j) => `${j.novelId}:${j.operation}`));
  const currIds = new Set(current.map((j) => `${j.novelId}:${j.operation}`));
  for (const key of prevIds) {
    if (!currIds.has(key)) {
      const [novelId, operation] = key.split(":");
      const novel = novels.value.find((n) => n.id === novelId);
      if (!novel) continue;
      if (operation === "check") {
        const completedJob = prev.find((j) => j.novelId === novelId && j.operation === "check");
        const newChapters = completedJob?.newChapters ?? 0;
        api.novels.get(novelId).then((updated) => {
          if (updated) updateNovelLocal(novelId, { chapterCount: updated.chapterCount, translatedCount: updated.translatedCount, lastCheckedAt: updated.lastCheckedAt, lastCheckNewChapters: updated.lastCheckNewChapters });
        }).catch(() => {});
        if (completedJob?.status === "failed") message.error(`${novel.sourceTitle}: Error al verificar`);
        else if (newChapters > 0) message.success(`${novel.sourceTitle}: ${newChapters} capítulos nuevos`);
      } else {
        api.novels.get(novelId).then((updated) => {
          if (updated) updateNovelLocal(novelId, { chapterCount: updated.chapterCount, translatedCount: updated.translatedCount });
        }).catch(() => {});
      }
    }
  }
});

async function loadNovels() {
  loading.value = true;
  error.value = null;
  try {
    const all: Novel[] = [];
    let offset = 0;
    for (;;) {
      const resp = await api.novels.list({ limit: PAGE_SIZE, offset });
      all.push(...resp.items);
      if (!resp.hasMore || resp.items.length === 0) break;
      offset += resp.items.length;
    }
    novels.value = all;
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err);
  } finally {
    loading.value = false;
  }
}

async function handleCheckSelected() {
  const ids = selectedActualizableIds.value;
  if (ids.length === 0) {
    message.info("Selecciona al menos una novela actualizable sin trabajos activos.");
    return;
  }
  checkingSelected.value = true;
  try {
    const result = await api.novels.batchCheck(ids);
    emitJobChanged();
    message.info(`${result.jobs.length} novelas en verificación`);
  } catch (err) {
    message.error(err instanceof Error ? err.message : String(err));
  } finally {
    checkingSelected.value = false;
  }
}

async function handleUpdateNovel(novel: Novel) {
  updateResults.value.delete(novel.id);
  try {
    const result = await api.novels.batchCheck([novel.id]);
    emitJobChanged();
    if (result.jobs.length > 0) message.info(`${novel.sourceTitle}: Verificando...`);
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    updateResults.value.set(novel.id, { added: 0, error: msg });
    message.error(`${novel.sourceTitle}: ${msg}`);
  }
}

async function handleDownloadSingle(novel: Novel) {
  if (!hasNewChapters(novel)) {
    message.warning("Primero verifica novedades.");
    return;
  }
  try {
    const result = await api.novels.updateFromUrl(novel.id, {});
    if (result.downloadJobId) {
      emitJobChanged();
      message.info(`${novel.sourceTitle}: Descargando ${result.pendingChapters ?? ""} capítulos`);
    }
  } catch (err) {
    message.error(`${novel.sourceTitle}: ${err instanceof Error ? err.message : String(err)}`);
  }
}

async function handleDownloadSelected() {
  const ids = selectedWithUpdatesIds.value;
  if (ids.length === 0) {
    message.info("Selecciona novelas con novedades detectadas (verifica primero).");
    return;
  }
  bulkDownloading.value = true;
  let enqueued = 0;
  let errors = 0;
  for (const novelId of ids) {
    try {
      const result = await api.novels.updateFromUrl(novelId, {});
      if (result.downloadJobId) enqueued++;
    } catch {
      errors++;
    }
  }
  bulkDownloading.value = false;
  emitJobChanged();
  message.success(`${enqueued} novelas en descarga${errors > 0 ? ` · ${errors} errores` : ""}`);
}

async function handleTranslateNovel(novel: Novel) {
  if (novel.chapterCount === 0) {
    message.warning("Esta novela no tiene capítulos.");
    return;
  }
  if (!hasPendingTranslation(novel)) {
    message.info("No hay capítulos pendientes por traducir.");
    return;
  }
  try {
    await api.novels.batchTranslate([{ novelId: novel.id }]);
    emitJobChanged();
    message.success(`${novel.sourceTitle}: Traducción iniciada`);
  } catch (err) {
    message.error(`${novel.sourceTitle}: ${err instanceof Error ? err.message : String(err)}`);
  }
}

async function handleTranslateSelected() {
  const ids = selectedTranslatableIds.value;
  if (ids.length === 0) {
    message.info("Selecciona novelas con capítulos pendientes.");
    return;
  }
  translatingSelected.value = true;
  try {
    const selections = ids.map((novelId) => ({ novelId }));
    const result = await api.novels.batchTranslate(selections);
    emitJobChanged();
    const failed = result.jobs.filter((j: { jobId: string }) => !j.jobId).length;
    message.success(`${result.jobs.length} novelas en traducción${failed ? ` · ${failed} con cola llena` : ""}`);
  } catch (err) {
    message.error(err instanceof Error ? err.message : String(err));
  } finally {
    translatingSelected.value = false;
  }
}

onMounted(loadNovels);
</script>

<style scoped>
:deep(.n-data-table-td) {
  padding-top: 6px !important;
  padding-bottom: 6px !important;
}
:deep(.n-data-table-th) {
  padding-top: 8px !important;
  padding-bottom: 8px !important;
}
</style>
