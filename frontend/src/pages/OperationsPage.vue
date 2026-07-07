<template>
  <AppLayout>
    <div class="stack-lg">
      <div>
        <h1 style="margin: 0 0 0.25rem">Operaciones</h1>
        <p class="muted" style="margin: 0">Gestiona y actualiza todas tus novelas.</p>
      </div>

      <div class="operations-toolbar">
        <SelectButton
          v-model="filter"
          :options="filterOptions"
          optionLabel="label"
          optionValue="value"
        />
        <Button
          :label="updatingAll ? 'Verificando...' : 'Actualizar'"
          :icon="updatingAll ? 'pi pi-spin pi-spinner' : 'pi pi-refresh'"
          :loading="updatingAll"
          :disabled="loading"
          @click="handleUpdateAll"
        />
      </div>

      <Card v-if="loading">
        <template #content>
          <div class="stack-sm">
            <Skeleton v-for="i in 6" :key="i" height="3rem" borderRadius="8px" />
          </div>
        </template>
      </Card>

      <Message v-else-if="error" severity="error" :closable="false">{{ error }}</Message>

      <template v-else>
        <DataTable
          v-model:selection="selectedNovels"
          :value="filteredNovels"
          dataKey="id"
          stripedRows
          :rows="50"
          :rowsPerPageOptions="[20, 50, 100]"
          paginator
          :globalFilterFields="['sourceTitle']"
          sortMode="multiple"
          removableSort
          responsiveLayout="scroll"
        >
          <template #empty>No hay novelas que coincidan con el filtro.</template>

          <Column selectionMode="multiple" headerStyle="width: 3rem" />

          <Column field="sourceTitle" header="Nombre" sortable style="min-width: 180px">
            <template #body="{ data }">
              <div class="operations-novel-name">
                <img
                  v-if="data.coverPath"
                  :src="data.coverPath"
                  alt=""
                  class="operations-novel-cover"
                  referrerpolicy="no-referrer"
                />
                <span class="operations-novel-title">{{ data.sourceTitle }}</span>
              </div>
            </template>
          </Column>

          <Column field="chapterCount" header="Capítulos" sortable style="width: 100px" bodyStyle="text-align: center" />

          <Column field="translatedCount" header="Traducidos" sortable style="width: 110px" bodyStyle="text-align: center" />

          <Column header="Estado" style="width: 140px" bodyStyle="text-align: center">
            <template #body="{ data }">
              <Tag
                v-if="isDownloading(data.id)"
                severity="warn"
                value="Descargando..."
              />
              <Tag
                v-else-if="updateResults.has(data.id)"
                :severity="updateResults.get(data.id)!.error ? 'danger' : 'success'"
                :value="updateResultsLabel(data)"
              />
              <Tag
                v-else-if="checkResults.has(data.id)"
                :severity="checkResults.get(data.id)!.error ? 'danger' : 'info'"
                :value="checkResultsLabel(data)"
              />
              <Tag v-else-if="isActualizable(data)" value="Actualizable" severity="warn" />
              <Tag v-else-if="data.status === 'completed'" value="Completada" severity="success" />
            </template>
          </Column>

          <Column header="Acciones" style="width: 180px" bodyStyle="text-align: center">
            <template #body="{ data }">
              <div class="operations-row-actions">
                <Button
                  label="Traducir"
                  icon="pi pi-play"
                  size="small"
                  :disabled="isDownloading(data.id) || translatingNovelId === data.id"
                  :loading="translatingNovelId === data.id"
                  @click="handleTranslateNovel(data)"
                />
                <Button
                  v-if="isActualizable(data)"
                  label="Actualizar"
                  icon="pi pi-download"
                  severity="secondary"
                  size="small"
                  :disabled="isDownloading(data.id) || translatingNovelId === data.id"
                  :loading="isDownloading(data.id)"
                  @click="handleUpdateNovel(data)"
                />
              </div>
            </template>
          </Column>
        </DataTable>

        <div class="operations-summary">
          <span class="small muted">
            {{ filteredNovels.length }} novelas
            <template v-if="actualizableCount > 0"> · {{ actualizableCount }} actualizables</template>
            <template v-if="checkResults.size > 0"> · {{ checkedCount }} verificadas</template>
            <template v-if="downloadingNovelIds.size > 0"> · {{ downloadingNovelIds.size }} descargando</template>
          </span>
          <Button
            v-if="checkResults.size > 0 && selectedNovels.length > 0"
            :label="`Descargar seleccionadas (${selectedNovels.length})`"
            icon="pi pi-download"
            size="small"
            :loading="downloading"
            @click="handleDownloadSelected"
          />
        </div>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, onScopeDispose, ref, watch } from "vue";
import { useToast } from "primevue/usetoast";
import Button from "primevue/button";
import Card from "primevue/card";
import Column from "primevue/column";
import DataTable from "primevue/datatable";
import Message from "primevue/message";
import SelectButton from "primevue/selectbutton";
import Skeleton from "primevue/skeleton";
import Tag from "primevue/tag";
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

const toast = useToast();
const { api } = useAppServices();
const { jobs: activeJobs } = useActiveJobs();

const loading = ref(true);
const error = ref<string | null>(null);
const novels = ref<Novel[]>([]);
const filter = ref<FilterValue>("all");
const selectedNovels = ref<Novel[]>([]);

const updatingAll = ref(false);
const translatingNovelId = ref<string | null>(null);
const downloading = ref(false);

const checkResults = ref<Map<string, CheckResult>>(new Map());
const updateResults = ref<Map<string, UpdateResult>>(new Map());

const downloadingNovelIds = ref<Map<string, string>>(new Map());

const filterOptions = [
  { label: "Todas", value: "all" },
  { label: "Actualizable", value: "actualizable" },
  { label: "Completadas", value: "completed" },
];

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
    toast.add({ severity: "info", summary: "Sin novelas", detail: "No hay novelas actualizables.", life: 3000 });
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
  toast.add({
    severity: "info",
    summary: "Verificación completa",
    detail: `${checked} verificadas · ${withUpdates} con actualizaciones${errors > 0 ? ` · ${errors} errores` : ""}`,
    life: 4000,
  });
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
      toast.add({
        severity: added > 0 ? "success" : "info",
        summary: novel.sourceTitle,
        detail: added > 0 ? `${added} capítulos descargados` : result.message ?? "Sin nuevos capítulos",
        life: 3000,
      });
    }
  } catch (err) {
    downloadingNovelIds.value.delete(novel.id);
    const msg = err instanceof Error ? err.message : String(err);
    updateResults.value.set(novel.id, { added: 0, error: msg });
    toast.add({
      severity: "error",
      summary: novel.sourceTitle,
      detail: msg,
      life: 4000,
    });
  }
}

async function handleTranslateNovel(novel: Novel) {
  if (novel.chapterCount === 0) {
    toast.add({ severity: "warn", summary: "Sin capítulos", detail: "Esta novela no tiene capítulos.", life: 3000 });
    return;
  }
  translatingNovelId.value = novel.id;
  try {
    await api.novels.batchTranslate([{ novelId: novel.id }]);
    toast.add({
      severity: "success",
      summary: novel.sourceTitle,
      detail: "Trabajo de traducción iniciado",
      life: 3000,
    });
  } catch (err) {
    toast.add({
      severity: "error",
      summary: novel.sourceTitle,
      detail: err instanceof Error ? err.message : String(err),
      life: 4000,
    });
  } finally {
    translatingNovelId.value = null;
  }
}

async function handleDownloadSelected() {
  if (selectedNovels.value.length === 0) return;
  downloading.value = true;
  let enqueued = 0;
  let errors = 0;
  for (const novel of selectedNovels.value) {
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
  selectedNovels.value = [];
  checkResults.value = new Map();
  toast.add({
    severity: enqueued > 0 ? "success" : "info",
    summary: "Descargas encoladas",
    detail: `${enqueued} novelas en proceso${errors > 0 ? ` · ${errors} errores` : ""}`,
    life: 4000,
  });
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

.operations-novel-name {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  min-width: 0;
}

.operations-novel-cover {
  width: 1.75rem;
  height: 2.5rem;
  object-fit: cover;
  border-radius: 3px;
  flex-shrink: 0;
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
