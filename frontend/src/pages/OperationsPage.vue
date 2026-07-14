<template>
  <AppLayout>
    <div class="stack-lg">
      <div>
        <h1 style="margin: 0 0 0.25rem">Operaciones</h1>
        <p class="muted" style="margin: 0">Gestiona y actualiza todas tus novelas.</p>
      </div>

      <div class="operations-toolbar">
        <n-radio-group v-model:value="filter" size="small">
          <n-radio-button value="all">Todas</n-radio-button>
          <n-radio-button value="actualizable">Actualizable</n-radio-button>
          <n-radio-button value="completed">Completadas</n-radio-button>
        </n-radio-group>
        <n-button
          :loading="updatingAll"
          :disabled="loading"
          @click="handleUpdateAll"
        >
          <template #icon><n-icon><RefreshOutline /></n-icon></template>
          {{ updatingAll ? 'Verificando...' : 'Actualizar' }}
        </n-button>
      </div>

      <n-card v-if="loading">
        <div class="stack-sm">
          <n-skeleton v-for="i in 6" :key="i" style="height: 3rem" :border-radius="8" />
        </div>
      </n-card>

      <n-alert v-else-if="error" type="error" :title="error" />

      <template v-else>
        <n-data-table
          :columns="columns"
          :data="filteredNovels"
          :row-key="(row: Novel) => row.id"
          :checked-row-keys="selectedRowKeys"
          :pagination="{ pageSize: 50 }"
          :bordered="false"
          striped
          @update:checked-row-keys="selectedRowKeys = $event as string[]"
        />

        <div class="operations-summary">
          <span class="small muted">
            {{ filteredNovels.length }} novelas
            <template v-if="actualizableCount > 0"> · {{ actualizableCount }} actualizables</template>
            <template v-if="activeCheckJobs.length > 0"> · {{ activeCheckJobs.length }} verificando</template>
            <template v-if="activeDownloadCount > 0"> · {{ activeDownloadCount }} descargando</template>
          </span>
          <n-button
            v-if="selectedRowKeys.length > 0 && selectedHasUpdates"
            size="small"
            :loading="bulkDownloading"
            @click="handleDownloadSelected"
          >
            <template #icon><n-icon><DownloadOutline /></n-icon></template>
            Descargar seleccionadas ({{ selectedRowKeys.length }})
          </n-button>
        </div>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, h, onMounted, ref, watch } from "vue";
import {
  NButton,
  NCard,
  NRadioGroup,
  NRadioButton,
  NAlert,
  NSkeleton,
  NTag,
  NIcon,
  NDataTable,
  useMessage,
  type DataTableColumns,
} from "naive-ui";
import {
  RefreshOutline,
  DownloadOutline,
  PlayOutline,
} from "@vicons/ionicons5";
import AppLayout from "@/components/AppLayout.vue";
import { useAppServices } from "@/app/services";
import { useActiveJobs } from "@/composables/useActiveJobs";
import { jobStatusLabel } from "@/composables/useJobHelpers";
import { emitJobChanged } from "@/utils/job-events";
import type { Novel, TranslationJob } from "@/domain";

const SUPPORTED_DOMAINS = [
  "novelfire.net",
  "novelphoenix.com",
  "novelbin.com",
  "fenrirealm.com",
];

const PREVIEW_CACHE_TTL_MS = 15 * 60 * 1000;

type FilterValue = "all" | "actualizable" | "completed";

interface UpdateResult {
  added: number;
  error?: string;
}

const message = useMessage();
const { api } = useAppServices();
const { jobs: activeJobs } = useActiveJobs();

const loading = ref(true);
const error = ref<string | null>(null);
const novels = ref<Novel[]>([]);
const filter = ref<FilterValue>("all");
const selectedRowKeys = ref<string[]>([]);

const updatingAll = ref(false);
const bulkDownloading = ref(false);

const updateResults = ref<Map<string, UpdateResult>>(new Map());

function activeJobForNovel(novelId: string, operation: string): TranslationJob | undefined {
  return activeJobs.value.find((j) => j.novelId === novelId && j.operation === operation);
}

function isChecking(novelId: string): boolean {
  return !!activeJobForNovel(novelId, "check");
}

const activeCheckJobs = computed(() =>
  activeJobs.value.filter((j) => j.operation === "check"),
);

const activeDownloadCount = computed(() =>
  activeJobs.value.filter((j) => j.operation === "download").length,
);

function isActualizable(novel: Novel): boolean {
  if (!novel.url) return false;
  try {
    const host = new URL(novel.url).hostname.replace(/^www\./, "");
    return SUPPORTED_DOMAINS.includes(host);
  } catch {
    return false;
  }
}

const actualizableCount = computed(() =>
  novels.value.filter((n) => isActualizable(n) && n.status !== "completed").length,
);

const selectedHasUpdates = computed(() =>
  selectedRowKeys.value.some((id) => {
    const novel = novels.value.find((n) => n.id === id);
    return novel && hasNewChapters(novel);
  }),
);

const filteredNovels = computed(() => {
  switch (filter.value) {
    case "actualizable":
      return novels.value.filter((n) => isActualizable(n) && n.status !== "completed");
    case "completed":
      return novels.value.filter((n) => n.status === "completed");
    default:
      return novels.value;
  }
});

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

function updateResultsLabel(novel: Novel): string {
  const r = updateResults.value.get(novel.id);
  if (!r) return "";
  if (r.error) return "Error";
  if (r.added === 0) return "Al día";
  return `+${r.added} descargados`;
}

const columns: DataTableColumns<Novel> = [
  {
    type: "selection",
    disabled(row) {
      return !hasNewChapters(row);
    },
  },
  {
    title: "Nombre",
    key: "sourceTitle",
    sorter: true,
    render(row) {
      return h("span", { class: "operations-novel-title" }, row.sourceTitle);
    },
  },
  {
    title: "Capítulos",
    key: "chapterCount",
    width: 100,
    align: "center",
    sorter: true,
  },
  {
    title: "Traducidos",
    key: "translatedCount",
    width: 110,
    align: "center",
    sorter: true,
  },
  {
    title: "Estado",
    key: "status",
    width: 140,
    align: "center",
    render(row) {
      const downloadJob = activeJobForNovel(row.id, "download");
      const translateJob = activeJobForNovel(row.id, "translate");
      const checkJob = activeJobForNovel(row.id, "check");
      if (downloadJob) {
        return h(NTag, { type: "warning", size: "small", round: true }, { default: () => jobStatusLabel(downloadJob) });
      }
      if (translateJob) {
        return h(NTag, { type: "info", size: "small", round: true }, { default: () => jobStatusLabel(translateJob) });
      }
      if (checkJob) {
        return h(NTag, { type: "info", size: "small", round: true }, { default: () => jobStatusLabel(checkJob) });
      }
      if (updateResults.value.has(row.id)) {
        const r = updateResults.value.get(row.id)!;
        return h(NTag, { type: r.error ? "error" : "success", size: "small", round: true }, { default: () => updateResultsLabel(row) });
      }
      const checkLabel = persistedCheckLabel(row);
      if (checkLabel) {
        return h(NTag, { type: "info", size: "small", round: true }, { default: () => checkLabel });
      }
      if (isActualizable(row) && row.status !== "completed") {
        return h(NTag, { type: "warning", size: "small", round: true }, { default: () => "Actualizable" });
      }
      if (row.status === "completed") {
        return h(NTag, { type: "success", size: "small", round: true }, { default: () => "Completada" });
      }
      return null;
    },
  },
  {
    title: "Acciones",
    key: "actions",
    width: 180,
    align: "center",
    render(row) {
      const translateJob = activeJobForNovel(row.id, "translate");
      const downloadJob = activeJobForNovel(row.id, "download");
      const checkJob = activeJobForNovel(row.id, "check");
      const hasAnyActive = !!translateJob || !!downloadJob || !!checkJob;
      return h("div", { class: "operations-row-actions" }, [
        h(NButton, {
          size: "small",
          type: "primary",
          loading: !!translateJob,
          disabled: hasAnyActive,
          onClick: () => handleTranslateNovel(row),
        }, {
          icon: () => h(NIcon, null, { default: () => h(PlayOutline) }),
          default: () => translateJob ? jobStatusLabel(translateJob) : "Traducir",
        }),
        isActualizable(row) && row.status !== "completed"
          ? h(NButton, {
              size: "small",
              secondary: true,
              loading: !!downloadJob || !!checkJob,
              disabled: hasAnyActive,
              onClick: () => handleUpdateNovel(row),
            }, {
              icon: () => h(NIcon, null, { default: () => h(DownloadOutline) }),
              default: () => {
                if (downloadJob) return jobStatusLabel(downloadJob);
                if (checkJob) return jobStatusLabel(checkJob);
                return "Actualizar";
              },
            })
          : null,
      ]);
    },
  },
];

function updateNovelLocal(id: string, patch: Partial<Novel>) {
  const idx = novels.value.findIndex((n) => n.id === id);
  if (idx >= 0) {
    novels.value[idx] = { ...novels.value[idx], ...patch };
  }
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
          if (updated) {
            updateNovelLocal(novelId, {
              chapterCount: updated.chapterCount,
              translatedCount: updated.translatedCount,
              lastCheckedAt: updated.lastCheckedAt,
              lastCheckNewChapters: updated.lastCheckNewChapters,
            });
          }
        }).catch(() => {});
        if (completedJob?.status === "failed") {
          message.error(`${novel.sourceTitle}: Error al verificar`);
        } else if (newChapters > 0) {
          message.success(`${novel.sourceTitle}: ${newChapters} capítulos nuevos`);
        }
      } else {
        api.novels.get(novelId).then((updated) => {
          if (updated) {
            updateNovelLocal(novelId, {
              chapterCount: updated.chapterCount,
              translatedCount: updated.translatedCount,
            });
          }
        }).catch(() => {});
      }
    }
  }
});

async function loadNovels() {
  loading.value = true;
  error.value = null;
  try {
    const resp = await api.novels.list({ limit: 200 });
    novels.value = resp.items;
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err);
  } finally {
    loading.value = false;
  }
}

async function handleUpdateAll() {
  if (updatingAll.value) return;
  const actualizable = novels.value.filter((n) => isActualizable(n) && n.status !== "completed");
  if (actualizable.length === 0) {
    message.info("No hay novelas actualizables.");
    return;
  }
  updatingAll.value = true;
  try {
    const novelIds = actualizable.map((n) => n.id);
    const result = await api.novels.batchCheck(novelIds);
    emitJobChanged();
    message.info(`${result.jobs.length} novelas en verificación`);
  } catch (err) {
    message.error(err instanceof Error ? err.message : String(err));
  } finally {
    updatingAll.value = false;
  }
}

async function handleUpdateNovel(novel: Novel) {
  updateResults.value.delete(novel.id);
  try {
    const result = await api.novels.batchCheck([novel.id]);
    emitJobChanged();
    if (result.jobs.length > 0) {
      message.info(`${novel.sourceTitle}: Verificando...`);
    }
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    updateResults.value.set(novel.id, { added: 0, error: msg });
    message.error(`${novel.sourceTitle}: ${msg}`);
  }
}

async function handleTranslateNovel(novel: Novel) {
  if (novel.chapterCount === 0) {
    message.warning("Esta novela no tiene capítulos.");
    return;
  }
  try {
    await api.novels.batchTranslate([{ novelId: novel.id }]);
    emitJobChanged();
    message.success(`${novel.sourceTitle}: Trabajo de traducción iniciado`);
  } catch (err) {
    message.error(`${novel.sourceTitle}: ${err instanceof Error ? err.message : String(err)}`);
  }
}

async function handleDownloadSelected() {
  if (selectedRowKeys.value.length === 0) return;
  bulkDownloading.value = true;
  let enqueued = 0;
  let errors = 0;
  for (const novelId of selectedRowKeys.value) {
    const novel = novels.value.find((n) => n.id === novelId);
    if (!novel || !hasNewChapters(novel)) continue;
    try {
      const result = await api.novels.updateFromUrl(novelId, {});
      if (result.downloadJobId) {
        enqueued++;
      }
    } catch {
      errors++;
    }
  }
  bulkDownloading.value = false;
  selectedRowKeys.value = [];
  emitJobChanged();
  message.success(
    `${enqueued} novelas en proceso${errors > 0 ? ` · ${errors} errores` : ""}`,
  );
}

onMounted(loadNovels);
</script>

<style scoped>
.operations-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  flex-wrap: wrap;
}

.operations-novel-title {
  font-weight: 500;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.operations-row-actions {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 0.375rem;
}

.operations-summary {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  flex-wrap: wrap;
}
</style>
