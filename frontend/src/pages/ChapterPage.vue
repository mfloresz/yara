<template>
  <AppLayout>
    <div v-if="!chapter" class="stack-md chapter-empty">
      <n-button secondary class="touch-target" @click="router.push(`/novels/${novelId}`)">
        <template #icon><n-icon><ArrowBackOutline /></n-icon></template>
        Volver
      </n-button>
      <div v-if="chaptersLoading || novelLoading" class="stack-md" aria-label="Cargando capítulo">
        <n-skeleton width="40%" height="1.75rem" />
        <n-skeleton width="100%" height="12rem" style="border-radius: 12px" />
        <n-skeleton width="100%" height="12rem" style="border-radius: 12px" />
      </div>
      <n-alert v-else type="warning" title="Capítulo no encontrado." />
    </div>

    <div v-else class="chapter-page">
      <!-- Sticky workbench bar: navegación + estado + acciones. No hace scroll. -->
      <div class="wb-bar">
        <div class="wb-nav">
          <n-button quaternary circle size="small" class="touch-target" aria-label="Volver a capítulos" @click="router.push(`/novels/${novelId}`)">
            <template #icon><n-icon><ArrowBackOutline /></n-icon></template>
          </n-button>
          <n-button quaternary circle size="small" class="touch-target" aria-label="Capítulo anterior" :disabled="!prevChapter" @click="goToChapter(prevChapter)">
            <template #icon><n-icon><ChevronBackOutline /></n-icon></template>
          </n-button>
          <n-button quaternary circle size="small" class="touch-target" aria-label="Capítulo siguiente" :disabled="!nextChapter" @click="goToChapter(nextChapter)">
            <template #icon><n-icon><ChevronForwardOutline /></n-icon></template>
          </n-button>
          <div class="wb-title-group">
            <span class="wb-position mono">#{{ chapterPosition(chapter) }}</span>
            <h1 class="wb-title">{{ translatedTitle || title }}</h1>
            <n-tag :type="chapterTagType(displayStatus)" size="small" round>
              {{ chapterStatusLabel(displayStatus) }}
            </n-tag>
          </div>
        </div>
        <div class="wb-actions">
          <n-button
            secondary
            size="small"
            class="touch-target wb-action-desktop"
            :loading="translateLoading"
            :disabled="!originalContent || chapterIsProcessing || translateLoading || refineLoading"
            @click="handleTranslate"
          >
            <template #icon><n-icon><SparklesOutline /></n-icon></template>
            Traducir
          </n-button>
          <n-button
            secondary
            size="small"
            class="touch-target wb-action-desktop"
            :loading="refineLoading"
            :disabled="!translatedContent || chapterIsProcessing || translateLoading || refineLoading"
            @click="handleRefine"
          >
            <template #icon><n-icon><ColorWandOutline /></n-icon></template>
            Refinar
          </n-button>
          <n-button
            type="primary"
            size="small"
            class="touch-target wb-save"
            :loading="saving"
            :disabled="saving"
            @click="handleSave"
          >
            <template #icon><n-icon><SaveOutline /></n-icon></template>
            Guardar
            <span v-if="isDirty && !saving" class="wb-dirty-dot" aria-label=" (con cambios sin guardar)"></span>
          </n-button>
        </div>
      </div>

      <div v-if="chapterIsProcessing" class="wb-processing" role="status" aria-live="polite">
        <n-icon><ColorWandOutline /></n-icon>
        <span>Procesando con IA… el contenido se actualizará solo al terminar.</span>
      </div>

      <n-alert v-if="error" type="error" :title="error" closable @close="error = null" />

      <!-- Identidad del capítulo: títulos + meta. Una sola card plana. -->
      <section class="wb-block" aria-label="Identidad del capítulo">
        <div class="wb-title-grid">
          <div class="wb-field">
            <label class="small muted" for="chapter-title-original">Título original</label>
            <n-input id="chapter-title-original" v-model:value="title" placeholder="Título original" />
          </div>
          <div class="wb-field">
            <label class="small muted" for="chapter-title-translated">Título traducido</label>
            <n-input id="chapter-title-translated" v-model:value="translatedTitle" placeholder="Título traducido" />
          </div>
        </div>
        <div v-if="chapter.status === 'refined' || chapter.status === 'done'" class="wb-meta">
          <n-button
            text
            size="small"
            class="wb-done-link"
            @click="handleMarkDone"
         >
            <template #icon><n-icon><CheckmarkOutline /></n-icon></template>
            {{ chapter.status === "done" ? "Completado ✓" : "Marcar completado" }}
          </n-button>
        </div>
      </section>

      <!-- Workspace: una sola superficie, tabs por fase + vista global. -->
      <section class="wb-block wb-workspace" aria-label="Contenido del capítulo">
        <div class="ws-toolbar">
          <div class="ws-tabs" role="tablist" aria-label="Fase del contenido">
            <button
              v-for="tab in tabs"
              :key="tab.id"
              type="button"
              role="tab"
              class="ws-tab touch-target"
              :class="{ active: activeTab === tab.id }"
              :aria-selected="activeTab === tab.id"
              @click="activeTab = tab.id"
            >
              {{ tab.label }}
            </button>
          </div>
          <div class="ws-tools">
            <label v-if="activeTab !== 'original'" class="ws-compare small muted">
              <n-switch v-model:value="compareWithSource" size="small" aria-label="Comparar con original" />
              Comparar
            </label>
            <n-radio-group v-model:value="viewMode" size="small" aria-label="Modo de edición">
              <n-radio-button value="plain">Texto</n-radio-button>
              <n-radio-button value="rich">Editor</n-radio-button>
              <n-radio-button value="markdown">Vista</n-radio-button>
            </n-radio-group>
          </div>
        </div>

        <div
          class="ws-grid"
          :class="{ 'ws-grid--single': !showCompare }"
          role="tabpanel"
          :aria-label="activePanelLabel"
        >
          <!-- Fuente: solo lectura cuando se compara. -->
          <div v-if="showCompare" class="ws-pane ws-pane--source">
            <div class="ws-pane-head">
              <span class="small muted">{{ novel?.sourceLanguage || "origen" }} · {{ originalContent.length }} chars · solo lectura</span>
            </div>
            <template v-if="viewMode === 'rich'">
              <RichTextEditor :key="`source-${chapter?.id}`" :model-value="originalContent" :editable="false" @update:model-value="() => {}" />
            </template>
            <div v-else-if="viewMode === 'markdown'" class="markdown-preview ws-preview" v-html="markdownToHtml(originalContent || 'Sin contenido original')" />
            <n-input v-else :value="originalContent" type="textarea" :rows="16" readonly :style="{ fontFamily: 'monospace' }" placeholder="Sin contenido original" tabindex="-1" />
          </div>

          <!-- Target activo: el único editable. -->
          <div class="ws-pane ws-pane--target">
            <div class="ws-pane-head">
              <span class="small muted">{{ targetLanguageLabel }} · {{ activeCharCount }} chars{{ isDirty ? " · sin guardar" : "" }}</span>
            </div>
            <template v-if="viewMode === 'plain'">
              <n-input
                :value="activeValue"
                type="textarea"
                :rows="18"
                :style="{ fontFamily: 'monospace' }"
                :placeholder="activePlaceholder"
                :aria-label="activePanelLabel"
                @update:value="onActiveChange($event)"
              />
            </template>
            <template v-else-if="viewMode === 'rich'">
              <RichTextEditor :key="`${activeTab}-${chapter?.id}`" :model-value="activeValue" @update:model-value="onActiveChange($event)" />
            </template>
            <div v-else class="markdown-preview ws-preview" v-html="markdownToHtml(activeValue || activePlaceholder)" />
          </div>
        </div>
      </section>

      <!-- Bottombar móvil: alcance del pulgar. -->
      <nav class="wb-bottombar" aria-label="Acciones del capítulo">
        <n-button quaternary circle class="touch-target" aria-label="Capítulo anterior" :disabled="!prevChapter" @click="goToChapter(prevChapter)">
          <template #icon><n-icon><ChevronBackOutline /></n-icon></template>
        </n-button>
        <n-button
          secondary
          size="small"
          class="touch-target wb-bottombar-ai"
          :loading="translateLoading"
          :disabled="!originalContent || chapterIsProcessing || translateLoading || refineLoading"
          @click="handleTranslate"
        >
          <template #icon><n-icon><SparklesOutline /></n-icon></template>
          Traducir
        </n-button>
        <n-button
          type="primary"
          class="touch-target wb-bottombar-save"
          :loading="saving"
          :disabled="saving"
          @click="handleSave"
        >
          <template #icon><n-icon><SaveOutline /></n-icon></template>
          Guardar
          <span v-if="isDirty && !saving" class="wb-dirty-dot" aria-hidden="true"></span>
        </n-button>
        <n-button quaternary circle class="touch-target" aria-label="Capítulo siguiente" :disabled="!nextChapter" @click="goToChapter(nextChapter)">
          <template #icon><n-icon><ChevronForwardOutline /></n-icon></template>
        </n-button>
      </nav>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import {
  NButton,
  NInput,
  NAlert,
  NSkeleton,
  NRadioGroup,
  NRadioButton,
  NSwitch,
  NTag,
  NIcon,
} from "naive-ui";
import {
  ArrowBackOutline,
  ChevronBackOutline,
  ChevronForwardOutline,
  SparklesOutline,
  ColorWandOutline,
  CheckmarkOutline,
  SaveOutline,
} from "@vicons/ionicons5";
import AppLayout from "@/components/AppLayout.vue";
import RichTextEditor from "@/components/RichTextEditor.vue";
import { useNovels } from "@/composables/useNovels";
import { useActiveJobStatus } from "@/composables/useActiveJobStatus";
import { useAppServices } from "@/app/services";
import { chapterStatusLabel, chapterTagType } from "@/composables/useChapterStatus";
import { chapterPosition, getNovelDisplayTitle, type Chapter, type Novel } from "@/domain";
import { useDocumentTitle } from "@/composables/useDocumentTitle";
import type { ChapterSummary } from "@/api/types";
import { emitJobChanged } from "@/utils/job-events";
import { markdownToHtml } from "@/utils/markdown";

const route = useRoute();
const router = useRouter();
const { api } = useAppServices();
const novelId = computed(() => String(route.params.novelId || ""));
const chapterId = computed(() => String(route.params.chapterId || ""));
const { getNovel } = useNovels();
const { hasActive } = useActiveJobStatus();
const novel = ref<Novel | null>(null);
const novelLoading = ref(false);
useDocumentTitle(() => (novel.value ? getNovelDisplayTitle(novel.value) : null));
const chapter = ref<Chapter | null>(null);
const chaptersLoading = ref(false);
// Prev/next llegan con el propio capítulo (?neighbors=true, orden position).
// Ya no se pide la lista completa para navegar.
const prevNeighbor = ref<ChapterSummary | null>(null);
const nextNeighbor = ref<ChapterSummary | null>(null);

const title = ref("");
const translatedTitle = ref("");
const originalContent = ref("");
const translatedContent = ref("");
const refinedContent = ref("");
const saving = ref(false);
const translateLoading = ref(false);
const refineLoading = ref(false);
const error = ref<string | null>(null);

// Snapshot para el indicador dirty ("sin guardar").
const snapshot = ref({ title: "", translatedTitle: "", original: "", translated: "", refined: "" });
const isDirty = computed(() =>
  title.value !== snapshot.value.title ||
  translatedTitle.value !== snapshot.value.translatedTitle ||
  originalContent.value !== snapshot.value.original ||
  translatedContent.value !== snapshot.value.translated ||
  refinedContent.value !== snapshot.value.refined,
);

type WorkspaceTab = "original" | "translated" | "refined";
type PanelMode = "plain" | "rich" | "markdown";

const CHAPTER_EDITOR_MODE_KEY = "yara.chapter.editorMode";
function loadViewMode(): PanelMode {
  try {
    const raw = localStorage.getItem(CHAPTER_EDITOR_MODE_KEY);
    if (raw === "plain" || raw === "rich" || raw === "markdown") return raw;
  } catch { /* ignore */ }
  return "rich";
}
// Un solo modo de vista global (antes: uno por panel). Persiste como readerSettings.
const viewMode = ref<PanelMode>(loadViewMode());
watch(viewMode, (mode) => {
  try { localStorage.setItem(CHAPTER_EDITOR_MODE_KEY, mode); } catch { /* ignore */ }
});

const activeTab = ref<WorkspaceTab>("translated");
const compareWithSource = ref(true);
const showCompare = computed(() => activeTab.value !== "original" && compareWithSource.value);

const tabs = [
  { id: "original" as const, label: "Original" },
  { id: "translated" as const, label: "Traducido" },
  { id: "refined" as const, label: "Refinado" },
];
const activeValue = computed(() =>
  activeTab.value === "original" ? originalContent.value
  : activeTab.value === "translated" ? translatedContent.value
  : refinedContent.value,
);
const activeCharCount = computed(() => activeValue.value.length);
const activePanelLabel = computed(() =>
  activeTab.value === "original" ? "Contenido original"
  : activeTab.value === "translated" ? "Contenido traducido"
  : "Contenido refinado",
);
const activePlaceholder = computed(() =>
  activeTab.value === "original" ? "Sin contenido original"
  : activeTab.value === "translated" ? "Sin contenido traducido"
  : "Sin contenido refinado",
);
const targetLanguageLabel = computed(() => novel.value?.targetLanguage || "destino");

function onActiveChange(value: string) {
  if (activeTab.value === "original") originalContent.value = value;
  else if (activeTab.value === "translated") translatedContent.value = value;
  else refinedContent.value = value;
}

const chapterIsProcessing = computed(() => chapter.value?.status === "processing");
const displayStatus = computed<Chapter["status"]>(() => {
  if (chapterIsProcessing.value) return "processing";
  return chapter.value?.status ?? "pending";
});

async function loadNovel() {
  if (!novelId.value) {
    novel.value = null;
    return null;
  }
  novelLoading.value = true;
  try {
    novel.value = await getNovel(novelId.value, false);
    return novel.value;
  } finally {
    novelLoading.value = false;
  }
}

function takeSnapshot() {
  snapshot.value = {
    title: title.value,
    translatedTitle: translatedTitle.value,
    original: originalContent.value,
    translated: translatedContent.value,
    refined: refinedContent.value,
  };
}

function syncChapterFields(next: Chapter, options: { replaceOriginalFields?: boolean; autoTab?: boolean } = {}) {
  const { replaceOriginalFields = true, autoTab = false } = options;
  chapter.value = next;
  if (replaceOriginalFields) {
    title.value = next.title;
    originalContent.value = next.originalContent || "";
  }
  translatedTitle.value = next.translatedTitle || "";
  translatedContent.value = next.translatedContent || "";
  refinedContent.value = next.refinedContent || "";
  takeSnapshot();
  if (autoTab) {
    // El target con contenido manda; si no hay nada, mostrar el original.
    if (refinedContent.value) activeTab.value = "refined";
    else if (translatedContent.value) activeTab.value = "translated";
    else activeTab.value = "original";
  }
}

function markChapterProcessing() {
  if (!chapter.value) return;
  syncChapterFields({
    ...chapter.value,
    status: "processing",
    errorMessage: "",
  }, { replaceOriginalFields: false });
}

async function loadChapter(options: { replaceOriginalFields?: boolean; autoTab?: boolean } = {}) {
  if (!novelId.value || !chapterId.value) {
    chapter.value = null;
    prevNeighbor.value = null;
    nextNeighbor.value = null;
    return null;
  }
  chaptersLoading.value = true;
  try {
    const { chapter: next, prev, next: nextSummary } = await api.chapters.getWithNeighbors(novelId.value, chapterId.value);
    if (!next) {
      chapter.value = null;
      prevNeighbor.value = null;
      nextNeighbor.value = null;
      return null;
    }
    prevNeighbor.value = prev;
    nextNeighbor.value = nextSummary;
    syncChapterFields(next, options);
    return chapter.value;
  } finally {
    chaptersLoading.value = false;
  }
}

watch([novelId, chapterId], () => {
  translateLoading.value = false;
  refineLoading.value = false;
  error.value = null;
  void loadNovel();
  void loadChapter({ autoTab: true });
}, { immediate: true });

const prevChapter = computed<ChapterSummary | null>(() => prevNeighbor.value);

const nextChapter = computed<ChapterSummary | null>(() => nextNeighbor.value);

function goToChapter(target: ChapterSummary | null) {
  if (!target) return;
  router.push(`/novels/${novelId.value}/chapters/${target.id}`);
  window.scrollTo({ top: 0 });
}

watch(hasActive, (active, previous) => {
  if (!previous || active || chapter.value?.status !== "processing") return;
  // Refresh tras el job: no tocar originales ni mover el tab activo.
  void loadChapter({ replaceOriginalFields: false });
});

async function handleSave() {
  if (!chapter.value) return;
  saving.value = true;
  try {
    const updated = await api.chapters.upsert(novelId.value, {
      id: chapter.value.id,
      chapterOrder: chapter.value.chapterOrder,
      title: title.value,
      translatedTitle: translatedTitle.value || undefined,
      originalContent: originalContent.value || undefined,
      translatedContent: translatedContent.value || undefined,
      refinedContent: refinedContent.value || undefined,
    });
    syncChapterFields(updated);
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err);
  } finally {
    saving.value = false;
  }
}

async function handleTranslate() {
  if (!novel.value || !chapter.value || !originalContent.value) return;
  translateLoading.value = true;
  error.value = null;
  try {
    await api.jobs.create(novelId.value, [chapter.value.id], {
      operation: "translate",
      provider: novel.value.aiOptions.provider || undefined,
      model: novel.value.aiOptions.model || undefined,
    });
    markChapterProcessing();
    emitJobChanged();
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err);
  } finally {
    translateLoading.value = false;
  }
}

async function handleRefine() {
  if (!novel.value || !chapter.value || !translatedContent.value) return;
  refineLoading.value = true;
  error.value = null;
  try {
    await api.jobs.create(novelId.value, [chapter.value.id], {
      operation: "refine",
      provider: novel.value.aiOptions.provider || undefined,
      model: novel.value.aiOptions.model || undefined,
    });
    markChapterProcessing();
    emitJobChanged();
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err);
  } finally {
    refineLoading.value = false;
  }
}

async function handleMarkDone() {
  if (!chapter.value) return;
  try {
    await api.chapters.updateStatus(novelId.value, chapter.value.id, "done");
    chapter.value.status = "done";
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err);
  }
}
</script>

<style scoped>
.chapter-page {
  display: flex;
  flex-direction: column;
  gap: 1rem;
  padding-bottom: 1rem;
}

/* ── Sticky workbench bar (bajo el topbar de AppLayout) ── */
.wb-bar {
  position: sticky;
  top: 57px;
  z-index: 40;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  flex-wrap: wrap;
  padding: 0.5rem 0.75rem;
  background: color-mix(in oklab, var(--surface-elevated) 92%, var(--background));
  border: 1px solid var(--divide);
  border-radius: var(--radius-lg);
  backdrop-filter: blur(12px);
}

.wb-nav {
  display: flex;
  align-items: center;
  gap: 0.25rem;
  min-width: 0;
  flex: 1;
}

.wb-title-group {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  min-width: 0;
  margin-left: 0.375rem;
}

.wb-position {
  font-size: 0.75rem;
  color: var(--text-secondary);
  flex-shrink: 0;
}

.wb-title {
  margin: 0;
  font-size: 1rem;
  font-weight: 700;
  letter-spacing: -0.01em;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 380px;
}

.wb-actions {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  flex-wrap: wrap;
}

.wb-save {
  position: relative;
}

.wb-dirty-dot {
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: 999px;
  background: var(--warning);
  margin-left: 0.375rem;
  flex-shrink: 0;
}

.wb-processing {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  padding: 0.625rem 0.875rem;
  font-size: 0.875rem;
  color: var(--text-secondary);
  background: var(--surface-muted);
  border: 1px solid var(--divide);
  border-radius: var(--radius-md);
}

/* ── Bloques planos (Flat-Except-The-Book: sin sombra) ── */
.wb-block {
  background: var(--surface-elevated);
  border: 1px solid var(--divide);
  border-radius: var(--radius-lg);
  padding: 1rem;
}

.wb-title-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0.75rem;
}

.wb-field {
  display: flex;
  flex-direction: column;
  gap: 0.375rem;
  min-width: 0;
}

.wb-meta {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  flex-wrap: wrap;
  margin-top: 0.625rem;
}

.wb-done-link {
  margin-left: auto;
}

/* ── Workspace ── */
.ws-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  flex-wrap: wrap;
  padding-bottom: 0.75rem;
  margin-bottom: 0.75rem;
  border-bottom: 1px solid var(--divide);
}

.ws-tabs {
  display: flex;
  gap: 0.25rem;
  padding: 0.25rem;
  background: var(--surface-muted);
  border: 1px solid var(--divide);
  border-radius: var(--radius-pill);
}

.ws-tab {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 0.375rem 0.875rem;
  background: transparent;
  border: none;
  border-radius: var(--radius-pill);
  color: var(--text-secondary);
  font-size: 0.875rem;
  font-weight: 600;
  cursor: pointer;
  transition: background 0.15s ease, color 0.15s ease;
  min-height: 44px;
}

.ws-tab:hover {
  color: var(--foreground);
}

.ws-tab.active {
  background: var(--surface-base);
  color: var(--foreground);
  box-shadow: inset 0 0 0 1px var(--divide);
}

.ws-tools {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  flex-wrap: wrap;
}

.ws-compare {
  display: inline-flex;
  align-items: center;
  gap: 0.375rem;
  cursor: pointer;
}

.ws-grid {
  display: grid;
  grid-template-columns: minmax(0, 5fr) minmax(0, 7fr);
  gap: 0.75rem;
  align-items: start;
}

.ws-grid--single {
  grid-template-columns: minmax(0, 1fr);
}

.ws-grid--single .ws-pane--target {
  max-width: 75ch;
}

.ws-pane {
  min-width: 0;
  max-width: 75ch;
}

.ws-pane--source {
  opacity: 0.92;
}

.ws-pane-head {
  margin-bottom: 0.5rem;
  font-variant-numeric: tabular-nums;
}

.ws-preview {
  border: 1px solid var(--divide);
  border-radius: var(--radius-md);
  background: var(--surface-base);
  padding: 1rem 1.125rem;
  min-height: 220px;
  font-size: 1.05rem;
  line-height: 1.75;
  overflow-wrap: anywhere;
}

.ws-preview :deep(p) {
  margin: 0 0 1rem;
}

/* ── Bottombar móvil (alcance del pulgar) ── */
.wb-bottombar {
  display: none;
}

.chapter-empty {
  max-width: 720px;
}

@media (max-width: 1023px) {
  .ws-grid {
    grid-template-columns: minmax(0, 1fr);
  }

  .ws-grid--single .ws-pane--target {
    max-width: none;
  }
}

@media (max-width: 768px) {
  .wb-bar {
    top: 53px;
  }

  .wb-title {
    max-width: 180px;
  }

  .wb-action-desktop {
    display: none;
  }

  .wb-title-grid {
    grid-template-columns: 1fr;
  }

  .chapter-page {
    padding-bottom: 84px;
  }

  .wb-bottombar {
    position: fixed;
    bottom: 0;
    left: 0;
    right: 0;
    z-index: 60;
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.625rem 0.75rem calc(0.625rem + env(safe-area-inset-bottom));
    background: color-mix(in oklab, var(--surface-elevated) 92%, var(--background));
    border-top: 1px solid var(--divide);
    backdrop-filter: blur(12px);
  }

  .wb-bottombar-save {
    flex: 1;
  }

  .wb-bottombar-ai {
    flex-shrink: 0;
  }
}

@media (prefers-reduced-motion: reduce) {
  .wb-bar,
  .wb-bottombar,
  .ws-tab,
  .ws-preview {
    transition: none;
  }

  html {
    scroll-behavior: auto;
  }
}
</style>
