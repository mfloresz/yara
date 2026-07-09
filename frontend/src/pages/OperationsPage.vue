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
          :pagination="{ pageSize: 50 }"
          :bordered="false"
          striped
        />

        <div class="operations-summary">
          <span class="small muted">
            {{ filteredNovels.length }} novelas
            <template v-if="actualizableCount > 0"> · {{ actualizableCount }} actualizables</template>
            <template v-if="checkResults.size > 0"> · {{ checkedCount }} verificadas</template>
            <template v-if="downloadingNovelIds.size > 0"> · {{ downloadingNovelIds.size }} descargando</template>
          </span>
          <n-button
            v-if="checkResults.size > 0 && selectedRowKeys.length > 0"
            size="small"
            :loading="downloading"
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
import { computed, h, onMounted, onScopeDispose, ref, watch } from "vue";
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
import type { Novel } from "@/domain";

const SUPPORTED_DOMAINS = [
  "novelfire.net",
  "novelphoenix.com",
  "novelbin.com",
  "fenrirealm.com",
];

type FilterValue = "all" | "actualizable" | "completed";

interface CheckResult {
  newChapters: number;
  error?: string;
}

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
const translatingNovelId = ref<string | null>(null);
const downloading = ref(false);

const checkResults = ref<Map<string, CheckResult>>(new Map());
const updateResults = ref<Map<string, UpdateResult>>(new Map());

const downloadingNovelIds = ref<Map<string, string>>(new Map());

function isActualizable(novel: Novel): boolean {
  if (!novel.url) return false;
  try {
    const host = new URL(novel.url).hostname.replace(/^www\./, "");
    return SUPPORTED_DOMAINS.includes(host);
  } catch {
    return false;
  }
}

const activeDownloadNovelIds = computed(() => {
  const ids = new Set<string>();
  for (const job of activeJobs.value) {
    if (job.operation === "download") {
      ids.add(job.novelId);
    }
  }
  return ids;
});

function isDownloading(novelId: string): boolean {
  return downloadingNovelIds.value.has(novelId) || activeDownloadNovelIds.value.has(novelId);
}

const actualizableCount = computed(() =>
  novels.value.filter((n) => isActualizable(n) && n.status !== "completed").length,
);
const checkedCount = computed(() => checkResults.value.size);

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

function checkResultsLabel(novel: Novel): string {
  const r = checkResults.value.get(novel.id);
  if (!r) return "";
  if (r.error) return "Error";
  if (r.newChapters === 0) return "Al día";
  return `+${r.newChapters}`;
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
      if (isDownloading(row.id)) {
        return h(NTag, { type: "warning", size: "small", round: true }, { default: () => "Descargando..." });
      }
      if (updateResults.value.has(row.id)) {
        const r = updateResults.value.get(row.id)!;
        return h(NTag, { type: r.error ? "error" : "success", size: "small", round: true }, { default: () => updateResultsLabel(row) });
      }
      if (checkResults.value.has(row.id)) {
        const r = checkResults.value.get(row.id)!;
        return h(NTag, { type: r.error ? "error" : "info", size: "small", round: true }, { default: () => checkResultsLabel(row) });
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
      return h("div", { class: "operations-row-actions" }, [
        h(NButton, {
          size: "small",
          type: "primary",
          loading: translatingNovelId.value === row.id,
          disabled: isDownloading(row.id) || translatingNovelId.value === row.id,
          onClick: () => handleTranslateNovel(row),
        }, {
          icon: () => h(NIcon, null, { default: () => h(PlayOutline) }),
          default: () => "Traducir",
        }),
        isActualizable(row) && row.status !== "completed"
          ? h(NButton, {
              size: "small",
              secondary: true,
              loading: isDownloading(row.id),
              disabled: isDownloading(row.id) || translatingNovelId.value === row.id,
              onClick: () => handleUpdateNovel(row),
            }, {
              icon: () => h(NIcon, null, { default: () => h(DownloadOutline) }),
              default: () => "Actualizar",
            })
          : null,
      ]);
    },
  },
];

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function randomDelay(): number {
  return 5000 + Math.random() * 5000;
}

function updateNovelLocal(id: string, patch: Partial<Novel>) {
  const idx = novels.value.findIndex((n) => n.id === id);
  if (idx >= 0) {
    novels.value[idx] = { ...novels.value[idx], ...patch };
  }
}

watch(activeDownloadNovelIds, (current, prev) => {
  for (const novelId of prev) {
    if (!current.has(novelId)) {
      downloadingNovelIds.value.delete(novelId);
      const novel = novels.value.find((n) => n.id === novelId);
      if (novel) {
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
  checkResults.value = new Map();
  let checked = 0;
  let withUpdates = 0;
  let errors = 0;
  for (const novel of actualizable) {
    try {
      const result = await api.novels.updatePreviewFromUrl(novel.id);
      const newChapters = result.newChapters;
      checkResults.value.set(novel.id, { newChapters });
      if (newChapters > 0) withUpdates++;
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      checkResults.value.set(novel.id, { newChapters: 0, error: msg });
      errors++;
    }
    checked++;
    if (checked < actualizable.length) {
      await sleep(randomDelay());
    }
  }
  updatingAll.value = false;
  message.info(
    `${checked} verificadas · ${withUpdates} con actualizaciones${errors > 0 ? ` · ${errors} errores` : ""}`,
  );
}

async function handleUpdateNovel(novel: Novel) {
  const next = new Map(downloadingNovelIds.value);
  next.set(novel.id, "pending");
  downloadingNovelIds.value = next;

  updateResults.value.delete(novel.id);

  try {
    const result = await api.novels.updateFromUrl(novel.id, {});
    const added = result.chaptersAdded ?? 0;
    if (result.downloadJobId) {
      const map = new Map(downloadingNovelIds.value);
      map.set(novel.id, result.downloadJobId);
      downloadingNovelIds.value = map;
    } else {
      downloadingNovelIds.value.delete(novel.id);
      updateResults.value.set(novel.id, { added });
      updateNovelLocal(novel.id, {
        chapterCount: result.totalChapters ?? novel.chapterCount + added,
      });
      if (added > 0) {
        message.success(`${novel.sourceTitle}: ${added} capítulos descargados`);
      } else {
        message.info(`${novel.sourceTitle}: ${result.message ?? "Sin nuevos capítulos"}`);
      }
    }
  } catch (err) {
    downloadingNovelIds.value.delete(novel.id);
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
  translatingNovelId.value = novel.id;
  try {
    await api.novels.batchTranslate([{ novelId: novel.id }]);
    message.success(`${novel.sourceTitle}: Trabajo de traducción iniciado`);
  } catch (err) {
    message.error(`${novel.sourceTitle}: ${err instanceof Error ? err.message : String(err)}`);
  } finally {
    translatingNovelId.value = null;
  }
}

async function handleDownloadSelected() {
  if (selectedRowKeys.value.length === 0) return;
  downloading.value = true;
  let enqueued = 0;
  let errors = 0;
  for (const novelId of selectedRowKeys.value) {
    const novel = novels.value.find((n) => n.id === novelId);
    if (!novel) continue;
    const cr = checkResults.value.get(novel.id);
    if (!cr || cr.error || cr.newChapters === 0) continue;
    try {
      const result = await api.novels.updateFromUrl(novel.id, {});
      if (result.downloadJobId) {
        const map = new Map(downloadingNovelIds.value);
        map.set(novel.id, result.downloadJobId);
        downloadingNovelIds.value = map;
        enqueued++;
      }
    } catch {
      errors++;
    }
  }
  downloading.value = false;
  selectedRowKeys.value = [];
  checkResults.value = new Map();
  message.success(
    `${enqueued} novelas en proceso${errors > 0 ? ` · ${errors} errores` : ""}`,
  );
}

onMounted(loadNovels);

onScopeDispose(() => {
  downloadingNovelIds.value = new Map();
});
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
