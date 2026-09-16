<template>
  <AppLayout>
    <div class="stack-lg ops-page">
      <!-- Header: mismo patrón page-header que Biblioteca -->
      <header class="page-header">
        <div class="page-context">
          <h1 class="page-title">Operaciones</h1>
          <p class="muted small ops-subtitle" aria-live="polite">{{ subtitle }}</p>
        </div>
        <div class="page-actions">
          <n-input
            v-model:value="searchQuery"
            clearable
            placeholder="Buscar título o autor"
            class="search-input"
            :input-props="{ type: 'search', inputmode: 'search', enterkeyhint: 'search' }"
            aria-label="Buscar novelas"
          >
            <template #prefix><n-icon><SearchOutline /></n-icon></template>
          </n-input>
        </div>
      </header>

      <!-- Filtros visibles con conteos + ayudas de selección -->
      <div class="ops-filters" role="group" aria-label="Filtrar novelas">
        <div class="ops-chips">
          <n-button
            v-for="opt in filterOptionsWithCounts"
            :key="opt.value"
            size="small"
            round
            :type="filter === opt.value ? 'primary' : 'default'"
            :secondary="filter !== opt.value"
            :aria-pressed="filter === opt.value"
            class="ops-chip"
            @click="filter = opt.value"
          >
            {{ opt.label }} ({{ opt.count }})
          </n-button>
        </div>
        <div class="ops-filter-helpers">
          <n-button size="tiny" quaternary @click="selectActualizable">Seleccionar actualizables</n-button>
          <n-button v-if="selectedRowKeys.length > 0" size="tiny" quaternary @click="selectedRowKeys = []">
            Limpiar ({{ selectedRowKeys.length }})
          </n-button>
        </div>
      </div>

      <n-card v-if="loading" content-style="padding: 12px;">
        <n-space vertical :size="8">
          <n-skeleton v-for="i in 6" :key="i" height="38px" :sharp="false" />
        </n-space>
      </n-card>

      <n-alert v-else-if="error" type="error" :title="error" show-icon closable @close="error = null" />

      <n-card v-else-if="filteredNovels.length === 0" role="status">
        <div class="empty-state">
          <div>
            <h2 class="empty-state-title">{{ emptyState.title }}</h2>
            <p class="muted empty-state-body">{{ emptyState.body }}</p>
          </div>
          <div class="empty-state-actions">
            <n-button secondary size="small" @click="clearFilters">Limpiar filtros</n-button>
          </div>
        </div>
      </n-card>

      <template v-else>
        <!-- Desktop: tabla compacta (selección + novela + estado + iconos) -->
        <div class="ops-table-wrap">
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
            aria-label="Novelas para operar"
            @update:checked-row-keys="selectedRowKeys = $event as string[]"
          />
        </div>

        <!-- Móvil: cards apiladas a 44px, misma data y acciones -->
        <div class="ops-cards" role="list" aria-label="Novelas para operar">
          <OperationsCard
            v-for="novel in pagedNovels"
            :key="novel.id"
            :novel="novel"
            :checked="selectedRowKeys.includes(novel.id)"
            :status="statusFor(novel)"
            @update:checked="setChecked(novel.id, $event)"
            @check="handleUpdateNovel(novel)"
            @download="handleDownloadSingle(novel)"
            @translate="handleTranslateNovel(novel)"
          />
        </div>
        <div class="ops-cards-pagination">
          <n-pagination
            :page="pagination.page"
            :page-size="pagination.pageSize"
            :item-count="filteredNovels.length"
            :page-sizes="[25, 50, 100]"
            show-size-picker
            size="small"
            @update:page="pagination.page = $event"
            @update:page-size="pagination.pageSize = $event; pagination.page = 1;"
          />
        </div>

        <!-- Única barra de acción en lote: solo con selección -->
        <OperationsActionBar
          v-if="selectedRowKeys.length > 0"
          :selected-count="selectedRowKeys.length"
          :with-updates="selectedWithUpdatesIds.length"
          :translatable="selectedTranslatableIds.length"
          :actualizable="selectedActualizableIds.length"
          :checking="checkingSelected"
          :downloading="bulkDownloading"
          :translating="translatingSelected"
          :deleting="deletingSelected"
          @verify="handleCheckSelected"
          @download="handleDownloadSelected"
          @translate="handleTranslateSelected"
          @remove="handleDeleteSelected"
          @clear="selectedRowKeys = []"
        />
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, onMounted, reactive, ref, watch } from "vue";
import { RouterLink } from "vue-router";
import {
  NAlert,
  NButton,
  NCard,
  NDataTable,
  NFlex,
  NIcon,
  NInput,
  NPagination,
  NSkeleton,
  NSpace,
  NTag,
  NTooltip,
  NAvatar,
  NEllipsis,
  useDialog,
  useMessage,
  type DataTableColumns,
} from "naive-ui";
import { GlobeOutline, SearchOutline } from "@vicons/ionicons5";
import AppLayout from "@/components/AppLayout.vue";
import OperationsActionBar from "@/components/operations/OperationsActionBar.vue";
import OperationsCard from "@/components/operations/OperationsCard.vue";
import OperationsOriginTag from "@/components/operations/OperationsOriginTag.vue";
import OperationsRowActions from "@/components/operations/OperationsRowActions.vue";
import OperationsTranslation from "@/components/operations/OperationsTranslation.vue";
import { useAppServices } from "@/app/services";
import { authState } from "@/app/auth";
import { useActiveJobs } from "@/composables/useActiveJobs";
import {
  buildNovelOperationStatus,
  hasAnyActive as hasAnyActiveNovel,
  hasNewChapters,
  hasPendingTranslation,
  isActualizable,
  isSameLanguage,
  translationRatio,
  type NovelJobContext,
} from "@/composables/useOperationDisplay";
import { emitJobChanged } from "@/utils/job-events";
import type { Novel, TranslationJob } from "@/domain";

const PAGE_SIZE = 200;

type FilterValue = "all" | "actualizable" | "updates" | "completed" | "active";

const message = useMessage();
const dialog = useDialog();
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
const deletingSelected = ref(false);

const pendingDeleteIds = ref<string[]>([]);
const pendingDeleteTitles = ref<Map<string, string>>(new Map());

const updateResults = ref<Map<string, { added: number; error?: string }>>(new Map());

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
  return hasAnyActiveNovel(novelId, activeJobs.value);
}

const filterCounts = computed(() => ({
  all: novels.value.length,
  actualizable: novels.value.filter((n) => isActualizable(n) && n.status !== "completed").length,
  updates: novels.value.filter((n) => hasNewChapters(n)).length,
  active: novels.value.filter((n) => hasAnyActive(n.id)).length,
  completed: novels.value.filter((n) => n.status === "completed").length,
}));

const filterOptionsWithCounts = computed(() => [
  { label: "Todas", value: "all" as FilterValue, count: filterCounts.value.all },
  { label: "Actualizables", value: "actualizable" as FilterValue, count: filterCounts.value.actualizable },
  { label: "Con novedades", value: "updates" as FilterValue, count: filterCounts.value.updates },
  { label: "Activas", value: "active" as FilterValue, count: filterCounts.value.active },
  { label: "Completadas", value: "completed" as FilterValue, count: filterCounts.value.completed },
]);

const subtitle = computed(() => {
  const parts = [`${filteredNovels.value.length} en vista`];
  if (filterCounts.value.updates > 0) parts.push(`${filterCounts.value.updates} con novedades`);
  if (filterCounts.value.active > 0) parts.push(`${filterCounts.value.active} activas`);
  else if (filterCounts.value.actualizable > 0) parts.push(`${filterCounts.value.actualizable} actualizables`);
  return parts.join(" · ");
});

const emptyState = computed(() => {
  if (searchQuery.value.trim()) {
    return {
      title: "Sin coincidencias",
      body: `No se encontraron novelas para «${searchQuery.value.trim()}». Prueba con otro término o limpia los filtros.`,
    };
  }
  switch (filter.value) {
    case "updates":
      return {
        title: "Sin novedades",
        body: "Ninguna novela tiene capítulos nuevos detectados. Verifica las actualizables para buscar novedades.",
      };
    case "actualizable":
      return {
        title: "Nada que verificar",
        body: "No hay novelas con URL actualizable pendientes. Todas están al día o completadas.",
      };
    case "active":
      return {
        title: "Sin trabajos activos",
        body: "No hay verificaciones, descargas ni traducciones en curso ahora mismo.",
      };
    case "completed":
      return {
        title: "Sin completadas",
        body: "Todavía no hay novelas marcadas como completadas.",
      };
    default:
      return {
        title: "Sin novelas",
        body: "Importa una novela para empezar a operar con ella.",
      };
  }
});

function clearFilters() {
  searchQuery.value = "";
  filter.value = "all";
}

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

const pagedNovels = computed(() => {
  const start = (pagination.page - 1) * pagination.pageSize;
  return filteredNovels.value.slice(start, start + pagination.pageSize);
});

const selectedNovels = computed(() => selectedRowKeys.value.map((id) => novels.value.find((n) => n.id === id)).filter(Boolean) as Novel[]);

const selectedActualizableIds = computed(() => selectedNovels.value.filter((n) => isActualizable(n) && n.status !== "completed" && !hasAnyActive(n.id)).map((n) => n.id));
const selectedWithUpdatesIds = computed(() => selectedNovels.value.filter((n) => hasNewChapters(n) && !hasAnyActive(n.id)).map((n) => n.id));
const selectedTranslatableIds = computed(() => selectedNovels.value.filter((n) => hasPendingTranslation(n) && !hasAnyActive(n.id)).map((n) => n.id));

function selectActualizable() {
  selectedRowKeys.value = filteredNovels.value.filter((n) => isActualizable(n) && n.status !== "completed" && !hasAnyActive(n.id)).map((n) => n.id);
  if (selectedRowKeys.value.length === 0) message.info("No hay novelas actualizables seleccionables en esta vista.");
}

function setChecked(id: string, checked: boolean) {
  if (checked && !selectedRowKeys.value.includes(id)) selectedRowKeys.value.push(id);
  else if (!checked) selectedRowKeys.value = selectedRowKeys.value.filter((k) => k !== id);
}

watch([filter, searchQuery], () => {
  pagination.page = 1;
});

const rowProps = () => ({ style: "height: 52px;" });

function jobContextFor(novelId: string): NovelJobContext {
  return {
    checkJob: activeJobForNovel(novelId, "check"),
    downloadJob: activeJobForNovel(novelId, "download"),
    translateJob: activeJobForNovel(novelId, "translate") || activeJobForNovel(novelId, "refine"),
    updateResult: updateResults.value.get(novelId),
  };
}

function statusFor(novel: Novel) {
  return buildNovelOperationStatus(novel, jobContextFor(novel.id));
}

const columns: DataTableColumns<Novel> = [
  { type: "selection", width: 36 },
  {
    title: "Novela",
    key: "sourceTitle",
    ellipsis: { tooltip: true },
    sorter: (a, b) => a.sourceTitle.localeCompare(b.sourceTitle),
    render(row) {
      const needsBrowser = row.requiresBrowser === true;
      return h(
        NFlex,
        { align: "center", wrap: false, size: 8, style: "min-width:0" },
        {
          default: () => [
            row.coverPath
              ? h(NAvatar, { size: 32, src: row.coverPath, style: "border-radius: 8px; flex-shrink:0" })
              : h(NAvatar, { size: 32, style: "border-radius: 8px; flex-shrink:0; font-size: 11px" }, { default: () => row.sourceTitle.slice(0, 2).toUpperCase() }),
            h(
              NFlex,
              { vertical: true, size: 0, style: "min-width:0; flex:1" },
              {
                default: () => [
                  h(
                    NFlex,
                    { align: "center", wrap: false, size: 4, style: "min-width:0" },
                    {
                      default: () => [
                        h(RouterLink, { to: `/novels/${row.id}`, class: "ops-novel-link" }, { default: () => h(NEllipsis, { tooltip: true, style: "max-width: 100%" }, { default: () => row.sourceTitle }) }),
                        needsBrowser
                          ? h(
                              NTooltip,
                              {},
                              {
                                trigger: () =>
                                  h(NIcon, { size: 13, style: "color: var(--warning); flex-shrink:0; cursor: help", component: GlobeOutline } as unknown as Record<string, unknown>, { "aria-label": "Requiere extensión de navegador" }),
                                default: () => "Requiere extensión de navegador (Cloudflare). Instala el worker para verificar/descargar.",
                              },
                            )
                          : null,
                      ],
                    },
                  ),
                  h(NEllipsis, { tooltip: true, style: "max-width: 100%; font-size: 12px; color: var(--text-tertiary)" }, { default: () => row.sourceAuthor || "Autor desconocido" }),
                ],
              },
            ),
          ],
        },
      );
    },
  },
  {
    title: "Origen",
    key: "originStatus",
    width: 135,
    align: "center",
    render(row) {
      const s = statusFor(row);
      return h(OperationsOriginTag, {
        type: s.origin.type,
        text: s.origin.text,
        tip: s.origin.tip,
      });
    },
  },
  {
    title: "Traducción",
    key: "translationStatus",
    width: 165,
    align: "center",
    sorter: (a, b) => translationRatio(a) - translationRatio(b),
    render(row) {
      const s = statusFor(row);
      return h(OperationsTranslation, { status: s.translation });
    },
  },
  {
    title: "Acciones",
    key: "actions",
    width: 150,
    align: "right",
    render(row) {
      const s = statusFor(row);
      return h(OperationsRowActions, {
        canDownload: s.canDownload,
        canTranslate: s.canTranslate,
        checkLabel: s.checkLabel,
        downloadLabel: s.downloadLabel,
        translateLabel: s.translateLabel,
        checkBusy: s.checkBusy,
        downloadBusy: s.downloadBusy,
        translateBusy: s.translateBusy,
        checkDisabled: s.checkDisabled,
        downloadDisabled: s.downloadDisabled,
        translateDisabled: s.translateDisabled,
        onCheck: () => handleUpdateNovel(row),
        onDownload: () => handleDownloadSingle(row),
        onTranslate: () => handleTranslateNovel(row),
      });
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
          if (updated) updateNovelLocal(novelId, { chapterCount: updated.chapterCount, translatedCount: updated.translatedCount, lastCheckedAt: updated.lastCheckedAt, lastCheckNewChapters: updated.lastCheckNewChapters, canUpdate: updated.canUpdate });
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
    const ownerId = authState.user.value?.id ?? "";
    const all: Novel[] = [];
    let offset = 0;
    for (;;) {
      const resp = await api.novels.list({ limit: PAGE_SIZE, offset });
      for (const item of resp.items) {
        if (item.ownerId === ownerId) all.push(item);
      }
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
  if (isSameLanguage(novel)) {
    message.info("Origen y destino son el mismo idioma, no requiere traducción.");
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

async function handleDeleteSelected() {
  const ids = selectedRowKeys.value.slice();
  if (ids.length === 0) return;

  const titlesById = new Map(novels.value.map((n) => [n.id, n.sourceTitle] as const));
  pendingDeleteIds.value = ids;
  pendingDeleteTitles.value = titlesById;

  const DeleteDialogBody = defineComponent({
    setup() {
      return () =>
        h("div", { style: "display: flex; flex-direction: column; gap: 12px;" }, [
          h(
            "p",
            { style: "margin: 0; line-height: 1.4;" },
            "Esta operación no se puede deshacer. Se eliminarán también todos sus capítulos, trabajos, EPUBs, glosario y progreso de lectura asociados.",
          ),
          h(
            "p",
            { style: "margin: 0; font-size: 12px; opacity: 0.85;" },
            "Quita del listado cualquier novela que no quieras incluir:",
          ),
          h(
            NSpace,
            { size: [6, 6], wrap: true, style: "max-height: 220px; overflow-y: auto; padding: 2px;" },
            pendingDeleteIds.value.length === 0
              ? [h("span", { style: "font-size: 12px; opacity: 0.7;" }, "Sin novelas seleccionadas")]
              : pendingDeleteIds.value.map((id) =>
                  h(
                    NTag,
                    {
                      key: id,
                      size: "small",
                      type: "error",
                      ghost: true,
                      closable: true,
                      onClose: () => {
                        pendingDeleteIds.value = pendingDeleteIds.value.filter((x) => x !== id);
                      },
                    },
                    { default: () => pendingDeleteTitles.value.get(id) ?? id },
                  ),
                ),
          ),
        ]);
    },
  });

  dialog.warning({
    title: () =>
      `Eliminar ${pendingDeleteIds.value.length === 1 ? "1 novela" : `${pendingDeleteIds.value.length} novelas`}`,
    content: () => h(DeleteDialogBody),
    positiveText: "Eliminar",
    negativeText: "Cancelar",
    onPositiveClick: async () => {
      const idsToDelete = pendingDeleteIds.value.slice();
      if (idsToDelete.length === 0) return;
      deletingSelected.value = true;
      const deleted: string[] = [];
      const failed: { id: string; error: string }[] = [];
      try {
        for (const id of idsToDelete) {
          try {
            await api.novels.remove(id);
            deleted.push(id);
          } catch (err) {
            failed.push({ id, error: err instanceof Error ? err.message : String(err) });
            break;
          }
        }
        if (deleted.length > 0) {
          selectedRowKeys.value = selectedRowKeys.value.filter((k) => !deleted.includes(k));
          await loadNovels();
        }
        if (failed.length === 0) {
          message.success(`${deleted.length === 1 ? "Novela eliminada" : `${deleted.length} novelas eliminadas`}.`);
        } else {
          const first = failed[0];
          const title = pendingDeleteTitles.value.get(first.id) ?? first.id;
          message.error(`No se pudo eliminar "${title}": ${first.error}. ${deleted.length} eliminadas antes del fallo.`);
        }
      } finally {
        pendingDeleteIds.value = [];
        deletingSelected.value = false;
      }
    },
    onClose: () => {
      pendingDeleteIds.value = [];
    },
  });
}

onMounted(loadNovels);
</script>

<style scoped>
.page-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 1rem clamp(1rem, 3vw, 2rem);
  flex-wrap: wrap;
}

.page-context {
  flex: 0 1 auto;
  min-width: 7rem;
  padding-top: 0.6rem;
}

.page-title {
  margin: 0;
  font-size: 1.75rem;
  font-weight: 700;
  letter-spacing: -0.02em;
}

.ops-subtitle {
  margin: 0.125rem 0 0;
}

.page-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 0.5rem;
  flex: 1 1 auto;
  min-width: 0;
  flex-wrap: wrap;
}

.search-input {
  width: clamp(12rem, 22vw, 17rem);
}

.ops-filters {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  flex-wrap: wrap;
}

.ops-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}

.ops-chip {
  min-height: 36px;
}

.ops-filter-helpers {
  display: flex;
  gap: 0.25rem;
  flex-wrap: wrap;
}

.ops-cards {
  display: none;
}

.ops-cards-pagination {
  display: none;
}

.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
  gap: 1rem;
  padding: 2rem 1rem;
}

.empty-state-title {
  margin: 0 0 0.25rem;
  font-size: 1.125rem;
}

.empty-state-body {
  margin: 0;
  max-width: 52ch;
}

.empty-state-actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: center;
  gap: 0.75rem;
}

:deep(.ops-novel-link) {
  font-weight: 600;
  font-size: 13px;
  color: inherit;
  min-width: 0;
}

:deep(.ops-novel-link:hover) {
  color: var(--accent-link-hover);
  text-decoration: underline;
  text-underline-offset: 3px;
}

:deep(.n-data-table-td) {
  padding-top: 6px !important;
  padding-bottom: 6px !important;
}

:deep(.n-data-table-th) {
  padding-top: 8px !important;
  padding-bottom: 8px !important;
}

@media (max-width: 820px) {
  .page-header {
    display: block;
  }

  .page-context {
    padding-top: 0;
    margin-bottom: 0.75rem;
  }

  .page-actions {
    justify-content: flex-start;
  }

  .search-input {
    flex: 1 1 100%;
    width: 100%;
  }

  .page-title {
    font-size: 1.5rem;
  }
}

/* Móvil: cards en lugar de tabla */
@media (max-width: 768px) {
  .ops-table-wrap {
    display: none;
  }

  .ops-cards {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }

  .ops-cards-pagination {
    display: flex;
    justify-content: center;
    padding-top: 0.25rem;
  }
}

@media (prefers-reduced-motion: reduce) {
  .ops-chip,
  .search-input {
    transition: none;
  }
}
</style>
