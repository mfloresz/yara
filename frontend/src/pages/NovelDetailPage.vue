<template>
  <AppLayout>
    <template #back-button>
      <n-button secondary circle @click="router.push('/')" aria-label="Volver a novelas">
        <template #icon><n-icon><ArrowBackOutline /></n-icon></template>
      </n-button>
    </template>

    <div v-if="!novel && novelLoading" class="novel-detail-layout">
      <aside class="novel-sidebar">
        <n-skeleton width="100%" height="320px" style="border-radius: 12px" />
        <div class="novel-sidebar-actions">
          <n-skeleton width="100%" height="2.5rem" style="border-radius: 8px" />
          <n-skeleton width="100%" height="2.5rem" style="border-radius: 8px" />
        </div>
        <div class="novel-sidebar-tags">
          <n-skeleton width="5rem" height="1.5rem" round />
          <n-skeleton width="6rem" height="1.5rem" round />
          <n-skeleton width="7rem" height="1.5rem" round />
        </div>
      </aside>

      <div class="novel-main">
        <header class="novel-main-header">
          <n-skeleton width="60%" height="2rem" style="margin-bottom: 0.5rem" />
          <n-skeleton width="30%" height="1rem" style="margin-bottom: 0.75rem" />
          <n-skeleton width="20%" height="0.875rem" />
          <n-skeleton width="90%" height="0.875rem" style="margin-top: 1rem" />
          <n-skeleton width="75%" height="0.875rem" style="margin-top: 0.5rem" />
        </header>
        <n-skeleton width="20rem" height="2rem" style="border-radius: 8px" />
        <div class="stack-sm">
          <div v-for="i in 6" :key="i" class="row-between" style="padding: 0.5rem 0; border-bottom: 1px solid var(--divide)">
            <div class="row-wrap" style="flex: 1; min-width: 0">
              <n-skeleton width="1.5rem" height="1.5rem" />
              <n-skeleton width="2.5rem" height="1rem" />
              <n-skeleton width="45%" height="1.1rem" />
              <n-skeleton width="6rem" height="1.4rem" />
            </div>
            <div class="row-wrap">
              <n-skeleton width="5rem" height="1rem" />
              <n-skeleton width="2.25rem" height="2.25rem" />
            </div>
          </div>
        </div>
      </div>
    </div>

    <div v-else-if="!novel" class="stack-md">
      <n-alert type="warning">Novela no encontrada.</n-alert>
      <n-button secondary @click="router.push('/')">Volver</n-button>
    </div>

    <div v-else class="novel-detail-layout">
      <NovelSidebar
        :novel="novel"
        :is-owner="isOwner"
        :is-novel-cached="isNovelCached"
        :downloading-offline="downloadingOffline"
        :total-chapters="chapterStats.totalChapters"
        :translated-chapters="chapterStats.translatedChapters"
        @read="onRead"
        @open-settings="settingsOpen = true"
        @copy-novel="copyCurrentNovel"
        @toggle-visibility="toggleVisibility"
        @open-update-url="updateUrlOpen = true"
        @toggle-offline="handleToggleOfflineCache"
      />

      <div class="novel-main">
        <header class="novel-main-header">
          <h1 class="novel-title">{{ getNovelDisplayTitle(novel) }}</h1>
          <div class="novel-meta">
            <span v-if="getNovelDisplayAuthor(novel)" class="muted">{{ getNovelDisplayAuthor(novel) }}</span>
            <span v-if="getNovelDisplaySeries(novel) || getNovelDisplayNumber(novel)" class="novel-series muted small">
              <n-icon :size="14"><BookmarkOutline /></n-icon>
              <span v-if="getNovelDisplaySeries(novel)">{{ getNovelDisplaySeries(novel) }}</span>
              <span v-if="getNovelDisplaySeries(novel) && getNovelDisplayNumber(novel)">·</span>
              <span v-if="getNovelDisplayNumber(novel)">#{{ getNovelDisplayNumber(novel) }}</span>
            </span>
          </div>
          <DescriptionBlock
            :description="getNovelDisplayDescription(novel)"
            :reset-key="novel.id"
          />
          <div v-if="novel.tags.length > 0" class="novel-description-tags">
            <n-tag v-for="tagItem in novel.tags" :key="tagItem" type="info" size="small">{{ tagItem }}</n-tag>
          </div>
        </header>

        <n-tabs v-model:value="activeTab" type="bar" animated>
          <n-tab v-for="tab in visibleTabs" :key="tab.value" :name="tab.value">
            {{ tab.label }}
          </n-tab>
        </n-tabs>

        <ChaptersTab
          v-show="activeTab === 'chapters'"
          :active="true"
          :chapters="chapterSummaries"
          :total="chapterSummaryTotal"
          :loading="chapterSummariesLoading"
          :page="chapterPage"
          :page-size="chapterPageSize"
          v-model:selected="selectedChapters"
          :is-owner="isOwner"
          :gaps="chapterGaps"
          @delete="onDeleteChapter"
          @bulk-delete="onBulkDeleteChapters"
          @create="openCreateChapter"
          @import="bulkImportOpen = true"
          @update:page="chapterPage = $event"
        />

        <TranslateTab
          v-show="activeTab === 'translate'"
          :novel-id="novelId"
          :operation="translateOperation"
          :all-summaries="allSummaries"
          :all-summaries-loading="allSummariesLoading"
          :translate-all-summaries="translateAllSummaries"
          :translate-all-loading="translateAllLoading"
          :create-job="createTranslateJob"
          :mark-failed-jobs-dirty="markFailedJobsDirty"
          :on-patch-status="patchTranslateStatus"
          @operation-change="onTranslateOperationChange"
          @show-all-change="onTranslateShowAllChange"
        />

        <CleanTab
          v-show="activeTab === 'clean'"
          :novel-id="novelId"
          :clean-all-summaries="cleanAllSummaries"
          :clean-all-summaries-loading="cleanAllSummariesLoading"
        />

        <ExportTab
          v-show="activeTab === 'export'"
          :novel="novel"
        />

        <JobsTab
          v-show="activeTab === 'jobs'"
          :jobs="failedJobs"
          :all-summaries="allSummaries"
          @cancel-job="cancelFailedHistoryJob"
        />
      </div>
    </div>

    <NovelChapterDialog
      :open="chapterDialogOpen"
      :editing-chapter="editingChapter"
      :next-chapter-order="nextChapterOrder"
      :saving="chapterSaving"
      @update:open="chapterDialogOpen = $event"
      @save="saveChapter"
    />

    <BulkImportDialog
      :open="bulkImportOpen"
      :next-order="nextChapterOrder"
      :on-import="handleBulkImport"
      :on-epub-files-imported="handleImportedEpubFiles"
      :preview-epub="previewEpub"
      @update:open="bulkImportOpen = $event"
    />

    <ProjectSettingsDialog
      v-if="novel"
      :open="settingsOpen"
      :novel="novel"
      :on-save-novel="saveProjectSettings"
      @update:open="settingsOpen = $event"
      @cover-updated="onCoverUpdated"
    />

    <UpdateUrlDialog
      v-if="novel && novel.url"
      :open="updateUrlOpen"
      :novel-id="novel.id"
      @update:open="updateUrlOpen = $event"
      @updated="onUrlUpdated"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useMessage, useDialog } from "naive-ui";
import AppLayout from "@/components/AppLayout.vue";
import BulkImportDialog from "@/features/novels/BulkImportDialog.vue";
import UpdateUrlDialog from "@/features/novels/UpdateUrlDialog.vue";
import ProjectSettingsDialog from "@/features/projects/ProjectSettingsDialog.vue";
import NovelSidebar from "@/features/novels/NovelSidebar.vue";
import NovelChapterDialog from "@/features/novels/NovelChapterDialog.vue";
import DescriptionBlock from "@/features/novels/DescriptionBlock.vue";
import ChaptersTab from "@/features/novels/tabs/ChaptersTab.vue";
import TranslateTab from "@/features/novels/tabs/TranslateTab.vue";
import CleanTab from "@/features/novels/tabs/CleanTab.vue";
import ExportTab from "@/features/novels/tabs/ExportTab.vue";
import JobsTab from "@/features/novels/tabs/JobsTab.vue";
import { NAlert, NButton, NIcon, NSkeleton, NTab, NTag, NTabs } from "naive-ui";
import { ArrowBackOutline, BookmarkOutline } from "@vicons/ionicons5";
import type { ChapterSummary } from "@/api/types";
import { useAppServices } from "@/app/services";
import { useChapters } from "@/composables/useChapters";
import { useNovels } from "@/composables/useNovels";
import { useActiveJobStatus } from "@/composables/useActiveJobStatus";
import { useTranslationJobs } from "@/composables/useTranslationJobs";
import { useOfflineCache } from "@/composables/useOfflineCache";
import { useChapterSummaries } from "@/composables/useChapterSummaries";
import {
  getNovelDisplayAuthor,
  getNovelDisplayDescription,
  getNovelDisplayNumber,
  getNovelDisplaySeries,
  getNovelDisplayTitle,
  type Chapter,
  type ChapterUpsertInput,
  type CreateNovelInput,
  type Novel,
} from "@/domain";

const router = useRouter();
const route = useRoute();
const message = useMessage();
const dialog = useDialog();
const { api, auth } = useAppServices();
const { getNovel, updateNovel, replaceNovelInList } = useNovels();
const novelId = computed(() => String(route.params.novelId || ""));
const { listChapters, createChapter, updateChapter, bulkCreateChapters, deleteChapter, bulkDeleteChapters } = useChapters(novelId, { autoLoad: false });
const { hasActive } = useActiveJobStatus();
const { jobs: failedJobs, listJobs: listFailedJobs, createJob, updateJob } = useTranslationJobs(novelId, { failedOnly: true, autoLoad: false });
const { isOnline, isNovelCached, getCachedNovel, downloadNovelForOffline, removeCachedNovel } = useOfflineCache(novelId);

const chapterPage = ref(0);
const chapterPageSize = 50;
const failedJobsLoaded = ref(false);
const failedJobsDirty = ref(false);
const selectedChapters = ref<ChapterSummary[]>([]);

const {
  chapterSummaries,
  chapterSummaryTotal,
  chapterSummariesLoading,
  chapterGaps,
  allSummaries,
  allSummariesLoading,
  cleanAllSummaries,
  cleanAllSummariesLoading,
  translateAllSummaries,
  translateAllLoading,
  fullChaptersLoaded,
  loadChapterSummaries,
  loadAllSummaries,
  loadCleanAllSummaries,
  loadTranslateAll,
  ensureFullChaptersLoaded,
  patchSummaryStatus,
  markAllSummariesDirty,
} = useChapterSummaries(novelId, chapterPage, chapterPageSize, { isOnline, getCachedNovel });

const tabs = [
  { value: "chapters", label: "Capítulos" },
  { value: "translate", label: "Traducir" },
  { value: "clean", label: "Limpieza" },
  { value: "export", label: "Exportar" },
  { value: "jobs", label: "Trabajos" },
];

const activeTab = ref("chapters");
const settingsOpen = ref(false);
const bulkImportOpen = ref(false);
const updateUrlOpen = ref(false);
const chapterDialogOpen = ref(false);
const chapterSaving = ref(false);
const editingChapter = ref<Chapter | null>(null);
const pendingDeleteChapterId = ref<string | null>(null);
const bulkDeleting = ref(false);

const downloadingOffline = ref(false);

const novelLoading = ref(true);
const novel = ref<Novel | null>(null);
const isOwner = computed(() => novel.value?.ownerId === auth.user.value?.id);
const visibleTabs = computed(() => (isOwner.value ? tabs : tabs.filter((tab) => tab.value === "chapters")));
const chapterStats = computed(() => ({
  totalChapters: novel.value?.chapterCount ?? 0,
  translatedChapters: novel.value?.translatedCount ?? 0,
  completedChapters: novel.value?.completedCount ?? 0,
  maxChapterOrder: novel.value?.maxChapterOrder ?? 0,
}));
const nextChapterOrder = computed(() => chapterStats.value.maxChapterOrder + 1);

// Owned by the page (as before the refactor) so the operation persists across
// tab switches and novel navigation; the tab mutates it via `operation-change`.
const translateOperation = ref<"translate" | "refine">("translate");

function tabNeedsAllSummaries(tab: string) {
  return tab === "translate" || tab === "jobs";
}

function tabNeedsCleanSummaries(tab: string) {
  return tab === "clean";
}

function tabNeedsFullChapters(_tab: string) {
  return false;
}

function markFailedJobsDirty() {
  failedJobsDirty.value = true;
}

async function ensureFailedJobsLoaded(force = false) {
  if (!novelId.value) {
    failedJobsLoaded.value = false;
    failedJobsDirty.value = false;
    return [] as typeof failedJobs.value;
  }
  if (!force && failedJobsLoaded.value && !failedJobsDirty.value) {
    return failedJobs.value;
  }
  const items = await listFailedJobs();
  failedJobsLoaded.value = true;
  failedJobsDirty.value = false;
  return items;
}

async function loadCurrentNovel() {
  if (!novelId.value) return;
  novelLoading.value = true;
  try {
    let current: Novel | null = null;
    if (!isOnline.value) {
      const cached = await getCachedNovel(novelId.value);
      if (cached) current = cached.novel;
    }
    if (!current) {
      current = await getNovel(novelId.value);
    }
    if (!current) {
      novel.value = null;
      return;
    }
    novel.value = current;
    replaceNovelInList(current);
  } finally {
    novelLoading.value = false;
  }
}

async function refreshNovelAndChapterMeta() {
  await Promise.all([loadCurrentNovel(), loadChapterSummaries()]);
}

async function refreshChapterViews() {
  await refreshNovelAndChapterMeta();
  if (tabNeedsAllSummaries(activeTab.value)) {
    await loadAllSummaries(true, translateOperation.value);
  }
  if (tabNeedsCleanSummaries(activeTab.value)) {
    await loadCleanAllSummaries(true);
  }
  if (fullChaptersLoaded.value || tabNeedsFullChapters(activeTab.value)) {
    await ensureFullChaptersLoaded(true, listChapters);
  }
}

watch(activeTab, (tab) => {
  if (tabNeedsFullChapters(tab)) {
    void ensureFullChaptersLoaded(false, listChapters);
  }
  if (tabNeedsAllSummaries(tab)) {
    void loadAllSummaries(false, translateOperation.value);
  }
  if (tabNeedsCleanSummaries(tab)) {
    void loadCleanAllSummaries();
  }
  if (tab === "jobs") {
    void ensureFailedJobsLoaded();
  }
});

watch(novelId, () => {
  selectedChapters.value = [];
  failedJobsLoaded.value = false;
  failedJobsDirty.value = false;
  chapterPage.value = 0;
  void refreshNovelAndChapterMeta();
});

watch(chapterPage, () => {
  void loadChapterSummaries();
});

watch(chapterSummaries, (items) => {
  const validIds = new Set(items.map((item) => item.id));
  const pruned = selectedChapters.value.filter((selected) => validIds.has(selected.id));
  if (pruned.length !== selectedChapters.value.length) {
    selectedChapters.value = pruned;
  }
});

watch(hasActive, (active, previous) => {
  if (!previous || active) return;
  markFailedJobsDirty();
  if (activeTab.value === "jobs") {
    void Promise.all([refreshChapterViews(), ensureFailedJobsLoaded(true)]);
    return;
  }
  void refreshChapterViews();
});

async function onRead() {
  if (!novel.value) return;
  await router.push(`/novels/${novel.value.id}/read`);
}

async function copyCurrentNovel() {
  if (!novel.value) return;
  const copy = await api.novels.copy(novel.value.id);
  replaceNovelInList(copy);
  message.success("Novela copiada", { duration: 2500 });
  await router.push(`/novels/${copy.id}`);
}

async function toggleVisibility() {
  if (!novel.value || !isOwner.value) return;
  const wasPublic = novel.value.isPublic;
  await api.novels.updateVisibility(novel.value.id, !novel.value.isPublic);
  novel.value = { ...novel.value, isPublic: !novel.value.isPublic };
  replaceNovelInList(novel.value);
  message.success(wasPublic ? "Novela despublicada" : "Novela publicada", { duration: 2500 });
}

async function onUrlUpdated(pending?: number) {
  fullChaptersLoaded.value = false;
  markAllSummariesDirty();
  markFailedJobsDirty();
  if (activeTab.value === "jobs") {
    await Promise.all([refreshChapterViews(), ensureFailedJobsLoaded(true)]);
  } else {
    await refreshChapterViews();
  }
  if (!pending || pending <= 0) {
    message.success("Novela actualizada desde internet", { duration: 2500 });
  }
}

function openCreateChapter() {
  editingChapter.value = null;
  chapterDialogOpen.value = true;
}

async function saveChapter(payload: ChapterUpsertInput & { id?: string }) {
  chapterSaving.value = true;
  try {
    if (payload.id) {
      await updateChapter(payload);
    } else {
      const { id: _id, ...rest } = payload;
      void _id;
      await createChapter({ ...rest, status: "pending" });
    }
    markAllSummariesDirty();
    chapterDialogOpen.value = false;
    await refreshChapterViews();
  } catch (err) {
    message.error(`Error al guardar capítulo: ${err instanceof Error ? err.message : String(err)}`, { duration: 4000 });
  } finally {
    chapterSaving.value = false;
  }
}

async function confirmDeleteChapter() {
  const id = pendingDeleteChapterId.value;
  if (!id) return;
  try {
    await deleteChapter(id);
    markAllSummariesDirty();
    await refreshChapterViews();
  } catch (err) {
    message.error(`Error al eliminar capítulo: ${err instanceof Error ? err.message : String(err)}`, { duration: 4000 });
  } finally {
    pendingDeleteChapterId.value = null;
  }
}

function onDeleteChapter({ chapter }: { event: Event; chapter: ChapterSummary }) {
  pendingDeleteChapterId.value = chapter.id;
  void confirmDeleteChapter();
}

function onBulkDeleteChapters(_event: Event) {
  if (selectedChapters.value.length <= 1) return;
  const count = selectedChapters.value.length;
  dialog.warning({
    title: `¿Eliminar ${count} capítulos?`,
    content: "Esta acción no se puede deshacer y eliminará los capítulos seleccionados junto con su contenido traducido y refinado.",
    positiveText: `Eliminar ${count}`,
    negativeText: "Cancelar",
    onPositiveClick: () => void confirmBulkDeleteChapters(),
  });
}

async function confirmBulkDeleteChapters() {
  const ids = selectedChapters.value.map((chapter) => chapter.id);
  if (ids.length === 0) {
    return;
  }
  bulkDeleting.value = true;
  try {
    const { deleted, requested } = await bulkDeleteChapters(ids);
    markAllSummariesDirty();
    await refreshChapterViews();
    selectedChapters.value = [];
    if (deleted === requested) {
      message.success(
        `Capítulos eliminados: ${deleted} ${deleted === 1 ? "capítulo eliminado" : "capítulos eliminados"}.`,
        { duration: 3000 },
      );
    } else {
      message.warning(
        `Eliminación parcial: ${deleted} de ${requested} capítulos eliminados.`,
        { duration: 4500 },
      );
    }
  } catch (err) {
    message.error(`Error al eliminar capítulos: ${err instanceof Error ? err.message : String(err)}`, { duration: 4000 });
  } finally {
    bulkDeleting.value = false;
  }
}

async function handleBulkImport(inputs: ChapterUpsertInput[]) {
  try {
    await bulkCreateChapters(inputs);
    markAllSummariesDirty();
    await refreshChapterViews();
  } catch (err) {
    message.error(`Error en importación masiva: ${err instanceof Error ? err.message : String(err)}`, { duration: 4000 });
    throw err;
  }
}

async function previewEpub(input: { file: Blob; fileName: string }) {
  return api.epubs.preview(input.file, input.fileName);
}

async function handleImportedEpubFiles(files: File[]) {
  if (!novel.value) return;
  for (const file of files) {
    await api.epubs.save({
      novelId: novel.value.id,
      fileKind: "original",
      sourceVariant: "original",
      fileName: file.name,
      blob: file,
    });
  }
}

async function saveProjectSettings(patch: Partial<CreateNovelInput>) {
  if (!novel.value) return;
  try {
    const updated = await updateNovel(novel.value.id, patch);
    novel.value = updated;
    message.success("Proyecto actualizado", { duration: 2500 });
  } catch (err) {
    message.error(`Error al guardar configuración: ${err instanceof Error ? err.message : String(err)}`, { duration: 4000 });
  }
}

function onCoverUpdated(updated: Novel) {
  novel.value = updated;
  replaceNovelInList(updated);
}

async function handleToggleOfflineCache() {
  if (!novel.value) return;
  if (isNovelCached.value) {
    try {
      await removeCachedNovel(novel.value.id);
      message.success("Novela eliminada de caché offline", { duration: 2500 });
    } catch (err) {
      message.error(`Error al eliminar de caché: ${err instanceof Error ? err.message : String(err)}`, { duration: 4000 });
    }
    return;
  }
  downloadingOffline.value = true;
  try {
    const result = await downloadNovelForOffline(novel.value.id);
    if (result) {
      message.success("Novela guardada para lectura offline", { duration: 2500 });
    } else {
      throw new Error("No se pudo descargar la novela");
    }
  } catch (err) {
    message.error(`Error al guardar offline: ${err instanceof Error ? err.message : String(err)}`, { duration: 4000 });
  } finally {
    downloadingOffline.value = false;
  }
}

async function createTranslateJob(targetIds: string[]) {
  if (!novel.value) return;
  await createJob(targetIds, {
    operation: translateOperation.value,
    provider: novel.value.aiOptions.provider || undefined,
    model: novel.value.aiOptions.model || undefined,
  });
}

function onTranslateOperationChange(operation: "translate" | "refine") {
  translateOperation.value = operation;
  void loadAllSummaries(true, operation);
}

function onTranslateShowAllChange(showAll: boolean) {
  if (showAll) {
    void loadTranslateAll(true);
  } else {
    void loadAllSummaries(true, translateOperation.value);
  }
}

function patchTranslateStatus(targetIds: string[]) {
  allSummaries.value = patchSummaryStatus(allSummaries.value, targetIds, "processing");
  translateAllSummaries.value = patchSummaryStatus(translateAllSummaries.value, targetIds, "processing");
  chapterSummaries.value = patchSummaryStatus(chapterSummaries.value, targetIds, "processing");
}

async function cancelFailedHistoryJob(jobId: string) {
  try {
    await updateJob(jobId, { status: "cancelled" });
    markFailedJobsDirty();
    await ensureFailedJobsLoaded(true);
  } catch (err) {
    message.error(`Error al cancelar trabajo: ${err instanceof Error ? err.message : String(err)}`, { duration: 4000 });
  }
}

void refreshNovelAndChapterMeta();
</script>

<style scoped>
.novel-detail-layout {
  display: grid;
  grid-template-columns: minmax(150px, 200px) minmax(0, 1fr);
  gap: 1.5rem;
  align-items: start;
}

.novel-main {
  display: flex;
  flex-direction: column;
  gap: 1rem;
  min-width: 0;
}

.novel-main-header {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.novel-title {
  margin: 0;
  font-size: 1.625rem;
  font-weight: 700;
  line-height: 1.15;
  letter-spacing: -0.02em;
}

.novel-meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.25rem 0.875rem;
  font-size: 0.875rem;
}

.novel-description-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 0.375rem;
  margin-top: 0.375rem;
}

.tab-panel {
  content-visibility: auto;
  contain-intrinsic-size: auto 400px;
}

.novel-series {
  display: inline-flex;
  align-items: center;
  gap: 0.35rem;
  color: var(--text-secondary);
}

.novel-series i {
  font-size: 0.8rem;
  color: var(--accent-link);
}

/* Skeleton sidebar styles — kept here so the loading state mirrors the
   NovelSidebar layout without instantiating the component. */
.novel-sidebar {
  display: flex;
  flex-direction: column;
  gap: 0.875rem;
}

.novel-sidebar-actions {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.novel-sidebar-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}

@media (max-width: 768px) {
  .novel-detail-layout {
    grid-template-columns: 1fr;
    gap: 1rem;
  }

  .novel-sidebar {
    display: grid;
    grid-template-columns: 100px 1fr;
    gap: 0.75rem;
    align-items: start;
  }

  .novel-sidebar-actions {
    gap: 0.375rem;
  }

  .novel-sidebar-tags {
    grid-column: 1 / -1;
  }

  .novel-title {
    font-size: 1.375rem;
  }
}
</style>
