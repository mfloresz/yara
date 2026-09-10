<template>
  <div class="reader-shell">
    <div id="progress-bar" class="reader-progress-track" role="progressbar" :aria-valuenow="Math.round(progress)" aria-valuemin="0" aria-valuemax="100">
      <div class="reader-progress-bar" :style="{ width: `${progress}%` }" />
    </div>

    <header class="reader-header">
      <div class="reader-header-left">
        <n-button
          quaternary
          circle
          class="reader-back-btn"
          aria-label="Volver a la novela"
          @click="router.push(`/novels/${novelId}`)"
        >
          <template #icon><n-icon><ArrowBackOutline /></n-icon></template>
        </n-button>

      </div>
      <div class="reader-header-title-group">
        <h1 class="reader-header-title">{{ novel ? getNovelDisplayTitle(novel) : 'Lector' }}</h1>
      </div>
      <n-button
        quaternary
        circle
        class="reader-settings-btn"
        aria-label="Configuración"
        @click="settingsOpen = !settingsOpen"
      >
        <template #icon><n-icon><SettingsOutline /></n-icon></template>
      </n-button>
    </header>

    <div class="reader-layout">
      <main ref="scrollContainer" class="reader-main">
        <n-card v-if="showEmpty">
          <div class="reader-empty-state">
            <h2>Sin contenido</h2>
            <p class="muted">
              <template v-if="variant === 'translated' && stats.totalChapters > 0">
                No hay capítulos traducidos todavía. Puedes cambiar a originales.
              </template>
              <template v-else>
                No hay capítulos disponibles para esta variante.
              </template>
            </p>
            <div class="reader-empty-actions">
              <n-button v-if="variant === 'translated' && stats.totalChapters > 0" secondary @click="variant = 'original'">
                <template #icon><n-icon><EyeOutline /></n-icon></template>
                Ver originales
              </n-button>
              <n-button type="primary" @click="router.push(`/novels/${novelId}`)">
                <template #icon><n-icon><HomeOutline /></n-icon></template>
                Volver al proyecto
              </n-button>
            </div>
          </div>
        </n-card>

        <n-card v-else-if="chapterLoading">
          <div class="reader-loading">
            <n-skeleton width="10rem" height="1rem" />
            <n-skeleton width="50%" height="2rem" />
            <n-skeleton width="100%" height="8rem" style="border-radius: 12px" />
            <n-skeleton width="100%" height="8rem" style="border-radius: 12px" />
          </div>
        </n-card>

        <article v-else-if="activeChapter" class="reader-article">
          <header class="reader-chapter-header">
            <h1 class="reader-chapter-heading">{{ chapterDisplayTitle(activeChapter) }}</h1>
            <div class="reader-chapter-ornament">❧ ✦ ❧</div>
          </header>
          <div class="reader-body markdown-preview" v-html="markdownToHtml(activeChapterContent)" />
        </article>
      </main>
    </div>

    <nav class="reader-bottombar" aria-label="Navegación entre capítulos">
      <div class="reader-bottombar-inner">
      <n-button
        secondary
        circle
        class="reader-bottombar-arrow"
        aria-label="Capítulo anterior"
        :disabled="!previousChapterId"
        @click="previousChapterId && selectChapter(previousChapterId)"
      >
        <template #icon><n-icon><ArrowBackOutline /></n-icon></template>
      </n-button>
      <n-button
        secondary
        class="reader-bottombar-list"
        aria-label="Abrir lista de capítulos"
        @click="openChapterList"
      >
        <span class="reader-bottombar-list-label">{{ bottomBarLabel }}</span>
      </n-button>
      <n-button
        secondary
        circle
        class="reader-bottombar-arrow"
        aria-label="Capítulo siguiente"
        :disabled="!nextChapterId"
        @click="nextChapterId && selectChapter(nextChapterId)"
      >
        <template #icon><n-icon><ArrowForwardOutline /></n-icon></template>
      </n-button>
      </div>
    </nav>

    <n-modal
      v-model:show="listModalOpen"
      preset="card"
      title="Capítulos"
      class="reader-list-modal"
      :bordered="false"
      role="dialog"
      aria-modal="true"
    >
      <n-input
        v-model:value="listSearch"
        clearable
        placeholder="Buscar capítulos…"
        class="reader-list-search"
      />
      <div v-if="summariesLoading" class="reader-list-loading">
        <n-skeleton text :repeat="6" />
      </div>
      <p v-else-if="filteredSummaries.length === 0" class="muted">Sin resultados.</p>
      <ul v-else class="reader-list">
        <li v-for="item in filteredSummaries" :key="item.id">
          <button
            type="button"
            class="reader-list-link"
            :class="{ active: item.id === activeChapterId }"
            :disabled="!summaryHasVariantContent(item)"
            @click="selectChapterFromList(item.id)"
          >
            <span class="reader-ch-title">{{ summaryDisplayTitle(item) }}</span>
          </button>
        </li>
      </ul>
      <template #footer>
        <div class="reader-list-footer muted">{{ visibleSummaries.length }} capítulos</div>
      </template>
    </n-modal>

    <div class="reader-settings-popover" :class="{ open: settingsOpen }">
      <div class="reader-settings-row">
        <span class="reader-settings-label">Tamaño de texto</span>
        <div class="reader-typo-group">
          <button class="reader-typo-btn" @click="adjustFontSize(-1)" title="Reducir fuente">−</button>
          <span class="reader-typo-val">{{ fontSize }}px</span>
          <button class="reader-typo-btn" @click="adjustFontSize(1)" title="Aumentar fuente">+</button>
        </div>
      </div>
      <div class="reader-settings-row">
        <span class="reader-settings-label">Interlineado</span>
        <div class="reader-typo-group">
          <button class="reader-typo-btn" @click="adjustLineHeight(-0.05)" title="Reducir interlineado">−</button>
          <span class="reader-typo-val">{{ lineHeight.toFixed(2) }}</span>
          <button class="reader-typo-btn" @click="adjustLineHeight(0.05)" title="Aumentar interlineado">+</button>
        </div>
      </div>
      <div class="reader-settings-row">
        <span class="reader-settings-label">Ancho del texto</span>
        <div class="reader-typo-group">
          <button class="reader-typo-btn" @click="adjustContentWidth(-40)" title="Reducir ancho">−</button>
          <span class="reader-typo-val">{{ contentWidth }}px</span>
          <button class="reader-typo-btn" @click="adjustContentWidth(40)" title="Aumentar ancho">+</button>
        </div>
      </div>
      <div class="reader-settings-divider"></div>
      <div class="reader-settings-row">
        <span class="reader-settings-label">Idioma</span>
        <div class="reader-variant-group">
          <button
            class="reader-variant-btn"
            :class="{ active: variant === 'translated' }"
            @click="variant = 'translated'"
          >Traducido</button>
          <button
            class="reader-variant-btn"
            :class="{ active: variant === 'original' }"
            @click="variant = 'original'"
          >Original</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch, type CSSProperties } from "vue";
import "@fontsource-variable/newsreader";
import "@fontsource-variable/playfair-display";
import { useRoute, useRouter } from "vue-router";
import {
  NButton,
  NCard,
  NIcon,
  NInput,
  NModal,
  NSkeleton,
} from "naive-ui";
import {
  ArrowBackOutline,
  SettingsOutline,
  ArrowForwardOutline,
  EyeOutline,
  HomeOutline,
} from "@vicons/ionicons5";
import type { ChapterSummary } from "@/api/types";
import { useAppServices } from "@/app/services";
import { STORAGE_KEYS } from "@/app/storage-keys";
import { useNovels } from "@/composables/useNovels";
import { getNovelDisplayTitle, type Chapter, type Novel } from "@/domain";
import { useDocumentTitle } from "@/composables/useDocumentTitle";
import { markdownToHtml } from "@/utils/markdown";
import { useReadingProgress } from "@/composables/useReadingProgress";
import { useOfflineCache } from "@/composables/useOfflineCache";

const route = useRoute();
const router = useRouter();
const { api } = useAppServices();
const novelId = computed(() => String(route.params.novelId || ""));
const { getNovel } = useNovels();
const { getCachedNovel } = useOfflineCache(novelId);

function loadReaderSettings() {
  try {
    const raw = localStorage.getItem(STORAGE_KEYS.readerSettings);
    if (raw) return JSON.parse(raw) as { fontSize?: number; lineHeight?: number; contentWidth?: number; variant?: "translated" | "original" };
  } catch { /* ignore */ }
  return null;
}

function saveReaderSettings() {
  try {
    localStorage.setItem(STORAGE_KEYS.readerSettings, JSON.stringify({
      fontSize: fontSize.value,
      lineHeight: lineHeight.value,
      contentWidth: contentWidth.value,
      variant: variant.value,
    }));
  } catch { /* ignore */ }
}

const isMobile = window.innerWidth < 720;
const saved = loadReaderSettings();
const variant = ref<"translated" | "original">(saved?.variant ?? "translated");

nextTick(() => applyTypography());
const activeChapterId = ref<string | null>(null);
const activeChapter = ref<Chapter | null>(null);
const settingsOpen = ref(false);
const progress = ref(0);
const scrollContainer = ref<HTMLElement | null>(null);
const chapterLoading = ref(false);
// Lista completa de summaries: carga diferida (lazy) y cacheada por novela.
// El capítulo activo se pide directo por id, sin esperar a la lista.
const allSummaries = ref<ChapterSummary[]>([]);
const summariesLoading = ref(false);
const summariesLoaded = ref(false);
const listModalOpen = ref(false);
const listSearch = ref("");
let summariesToken = 0;
// Vecinos del capítulo activo (?neighbors=true, orden position): activan ←/→
// al instante sin esperar a la lista. Cuando la lista ya está cacheada,
// prev/next se resuelven sobre ella para respetar la variante activa.

const fontSize = ref(saved?.fontSize ?? (isMobile ? 13 : 16));
const lineHeight = ref(saved?.lineHeight ?? 1.5);
const contentWidth = ref(saved?.contentWidth ?? 900);
const novel = ref<Novel | null>(null);
useDocumentTitle(() => (novel.value ? getNovelDisplayTitle(novel.value) : null));

applyTypography();
const {
  savedChapterId,
  savedScrollPercent,
  isLoaded: progressLoaded,
  load: loadReadingProgress,
  flush: flushReadingProgress,
  startAutoSave,
} = useReadingProgress(novelId, activeChapterId, progress);
const stats = computed(() => ({
  totalChapters: novel.value?.chapterCount ?? 0,
}));

const loadedSummaries = computed(() => allSummaries.value);
const visibleSummaries = computed(() =>
  loadedSummaries.value.filter((item) => summaryHasVariantContent(item)),
);
const activeChapterContent = computed(() => {
  if (!activeChapter.value) return "";
  if (variant.value === "translated") {
    return activeChapter.value.refinedContent || activeChapter.value.translatedContent || "";
  }
  return activeChapter.value.originalContent || "";
});
const activeVisibleIndex = computed(() =>
  visibleSummaries.value.findIndex((item) => item.id === activeChapterId.value),
);
const prevNeighbor = ref<ChapterSummary | null>(null);
const nextNeighbor = ref<ChapterSummary | null>(null);
const previousChapterId = computed(() => {
  if (summariesLoaded.value) {
    return activeVisibleIndex.value > 0 ? visibleSummaries.value[activeVisibleIndex.value - 1]?.id ?? null : null;
  }
  return prevNeighbor.value?.id ?? null;
});
const nextChapterId = computed(() => {
  if (summariesLoaded.value) {
    if (activeVisibleIndex.value < 0) return null;
    return visibleSummaries.value[activeVisibleIndex.value + 1]?.id ?? null;
  }
  return nextNeighbor.value?.id ?? null;
});
const filteredSummaries = computed(() => {
  const q = listSearch.value.trim().toLowerCase();
  if (!q) return visibleSummaries.value;
  return visibleSummaries.value.filter((item) =>
    summaryDisplayTitle(item).toLowerCase().includes(q),
  );
});
const bottomBarLabel = computed(() =>
  activeChapter.value ? chapterDisplayTitle(activeChapter.value) : "Capítulos",
);
const showEmpty = computed(() =>
  !chapterLoading.value && !summariesLoading.value && summariesLoaded.value &&
  stats.value.totalChapters > 0 && visibleSummaries.value.length === 0,
);

onMounted(() => {
  void initializeReader();
  document.addEventListener("keydown", onKeydown);
  document.addEventListener("click", handleClickOutside);
  window.addEventListener("scroll", updateProgress, { passive: true });
});

onBeforeUnmount(() => {
  summariesToken++;
  document.removeEventListener("keydown", onKeydown);
  document.removeEventListener("click", handleClickOutside);
  window.removeEventListener("scroll", updateProgress);
});

// selectChapter es la única vía para cambiar de capítulo (fija id + carga).
// No hay watcher sobre activeChapterId: evita doble fetch.

watch(variant, () => {
  // El capítulo ya trae todas las variantes; cambiar de idioma no refetchea.
  if (!activeChapterId.value) {
    void selectFirstAvailableChapter();
    return;
  }
  const currentSummary = loadedSummaries.value.find((item) => item.id === activeChapterId.value);
  if (currentSummary && !summaryHasVariantContent(currentSummary)) {
    void selectFirstAvailableChapter();
    return;
  }
  if (activeChapter.value && !chapterHasVariantContent(activeChapter.value)) {
    void selectFirstAvailableChapter();
  }
});

let initialScrollRestored = false;

watch(activeChapter, async () => {
  await nextTick();
  if (!initialScrollRestored && savedScrollPercent.value > 0) {
    const scrollRange = document.documentElement.scrollHeight - document.documentElement.clientHeight;
    if (scrollRange > 0) {
      const targetScroll = (savedScrollPercent.value / 100) * scrollRange;
      window.scrollTo({ top: targetScroll, behavior: "auto" });
    }
    initialScrollRestored = true;
  } else {
    window.scrollTo({ top: 0, behavior: "auto" });
  }
  updateProgress();
});

watch(novelId, () => {
  summariesToken++;
  initialScrollRestored = false;
  allSummaries.value = [];
  summariesLoaded.value = false;
  summariesLoading.value = false;
  listModalOpen.value = false;
  listSearch.value = "";
  activeChapterId.value = null;
  activeChapter.value = null;
  prevNeighbor.value = null;
  nextNeighbor.value = null;
  void initializeReader();
});

watch([fontSize, lineHeight, contentWidth, variant], saveReaderSettings);

function chapterToSummary(chapter: Chapter): ChapterSummary {
  return {
    id: chapter.id,
    novelId: chapter.novelId,
    chapterOrder: chapter.chapterOrder,
    position: chapter.position,
    excluded: chapter.excluded,
    title: chapter.title,
    translatedTitle: chapter.translatedTitle,
    status: chapter.status,
    errorMessage: chapter.errorMessage,
    hasOriginalContent: Boolean(chapter.originalContent),
    hasTranslatedContent: Boolean(chapter.translatedContent),
    hasRefinedContent: Boolean(chapter.refinedContent),
    originalChars: chapter.originalContent?.length || 0,
    translatedChars: chapter.translatedContent?.length || 0,
    refinedChars: chapter.refinedContent?.length || 0,
    createdAt: chapter.createdAt,
    updatedAt: chapter.updatedAt,
  };
}

function summaryHasVariantContent(summary: ChapterSummary) {
  return variant.value === "translated"
    ? summary.hasRefinedContent || summary.hasTranslatedContent
    : summary.hasOriginalContent;
}

function summaryDisplayTitle(summary: ChapterSummary) {
  if (variant.value === "translated" && summary.translatedTitle?.trim()) {
    return summary.translatedTitle;
  }
  return summary.title;
}

function chapterDisplayTitle(chapter: Chapter) {
  if (variant.value === "translated" && chapter.translatedTitle?.trim()) {
    return chapter.translatedTitle;
  }
  return chapter.title;
}

async function loadCurrentNovel() {
  if (!novelId.value) {
    novel.value = null;
    return;
  }

  try {
    novel.value = await getNovel(novelId.value, false);
  } catch {
    const cached = await getCachedNovel(novelId.value);
    novel.value = cached?.novel ?? null;
  }

  if (!novel.value) {
    const cached = await getCachedNovel(novelId.value);
    novel.value = cached?.novel ?? null;
  }
}

function chapterHasVariantContent(chapter: Chapter) {
  if (variant.value === "translated") {
    return Boolean(chapter.refinedContent || chapter.translatedContent);
  }
  return Boolean(chapter.originalContent);
}

async function initializeReader() {
  if (!novelId.value) return;
  const token = ++summariesToken;
  chapterLoading.value = true;
  await loadCurrentNovel();
  if (token !== summariesToken) return;
  // Progreso y contenido se piden directo por id: la lista no bloquea.
  await loadReadingProgress();
  if (token !== summariesToken) return;
  const savedCh = savedChapterId.value;
  if (savedCh) {
    activeChapterId.value = savedCh;
    await loadActiveChapter(savedCh);
    if (token !== summariesToken) return;
    if (!activeChapter.value || !chapterHasVariantContent(activeChapter.value)) {
      await selectFirstAvailableChapter();
    }
  } else {
    await selectFirstAvailableChapter();
  }
  if (token !== summariesToken) return;
  startAutoSave();
  // La lista completa llega en fondo para activar ←/→ y el modal.
  void ensureSummaries();
}

// Trae la lista completa en una sola petición (mismo patrón que
// ChapterPage para resolver vecinos) y la cachea por novela.
async function ensureSummaries(): Promise<void> {
  if (!novelId.value || summariesLoaded.value || summariesLoading.value) return;
  const token = summariesToken;
  summariesLoading.value = true;
  try {
    const items = await api.chapters.list(novelId.value);
    if (token !== summariesToken) return;
    allSummaries.value = items;
    summariesLoaded.value = true;
  } catch {
    if (token !== summariesToken) return;
    const cached = await getCachedNovel(novelId.value);
    if (cached) {
      allSummaries.value = cached.chapters
        .slice()
        .filter((c) => !c.excluded)
        .sort((a, b) => {
          const pa = a.position && a.position > 0 ? a.position : a.chapterOrder;
          const pb = b.position && b.position > 0 ? b.position : b.chapterOrder;
          return pa - pb;
        })
        .map(chapterToSummary);
      summariesLoaded.value = true;
    }
  } finally {
    if (token === summariesToken) summariesLoading.value = false;
  }
}

async function openChapterList() {
  listModalOpen.value = true;
  await ensureSummaries();
}

async function selectChapterFromList(chapterId: string) {
  listModalOpen.value = false;
  await selectChapter(chapterId);
}

async function selectFirstAvailableChapter() {
  if (!summariesLoaded.value) await ensureSummaries();
  const firstAvailable = loadedSummaries.value.find((item) => summaryHasVariantContent(item));
  if (firstAvailable) {
    await selectChapter(firstAvailable.id);
    return;
  }
  activeChapterId.value = null;
  activeChapter.value = null;
  prevNeighbor.value = null;
  nextNeighbor.value = null;
}

async function selectChapter(chapterId: string) {
  activeChapterId.value = chapterId;
  await loadActiveChapter(chapterId);
}

async function loadActiveChapter(chapterId: string) {
  if (!novelId.value) return;
  chapterLoading.value = true;
  try {
    const { chapter, prev, next } = await api.chapters.getWithNeighbors(novelId.value, chapterId);
    activeChapter.value = chapter;
    prevNeighbor.value = prev;
    nextNeighbor.value = next;
    if (chapter) return;
    const cached = await getCachedNovel(novelId.value);
    activeChapter.value = cached?.chapters.find((c) => c.id === chapterId) ?? null;
    prevNeighbor.value = null;
    nextNeighbor.value = null;
  } catch {
    const cached = await getCachedNovel(novelId.value);
    activeChapter.value = cached?.chapters.find((chapter) => chapter.id === chapterId) ?? null;
    prevNeighbor.value = null;
    nextNeighbor.value = null;
  } finally {
    chapterLoading.value = false;
  }
}

function onKeydown(event: KeyboardEvent) {
  if (event.key === "Escape") {
    listModalOpen.value = false;
    settingsOpen.value = false;
    return;
  }
  if (listModalOpen.value) return;
  if (event.key === "ArrowRight" || event.key === "ArrowDown") {
    event.preventDefault();
    if (nextChapterId.value) selectChapter(nextChapterId.value);
  } else if (event.key === "ArrowLeft" || event.key === "ArrowUp") {
    event.preventDefault();
    if (previousChapterId.value) selectChapter(previousChapterId.value);
  }
}

function updateProgress() {
  const scrollTop = window.scrollY || document.documentElement.scrollTop || 0;
  const scrollRange = document.documentElement.scrollHeight - document.documentElement.clientHeight;
  progress.value = scrollRange > 0 ? Math.min(100, (scrollTop / scrollRange) * 100) : 0;
}

function handleClickOutside(event: MouseEvent) {
  const target = event.target as HTMLElement;
  if (settingsOpen.value && !target.closest(".reader-settings-popover") && !target.closest(".reader-settings-btn")) {
    settingsOpen.value = false;
  }
}

function adjustFontSize(delta: number) {
  fontSize.value = Math.min(26, Math.max(13, fontSize.value + delta));
  applyTypography();
}

function adjustLineHeight(delta: number) {
  lineHeight.value = parseFloat(Math.min(2.6, Math.max(1.3, lineHeight.value + delta)).toFixed(2));
  applyTypography();
}

function adjustContentWidth(delta: number) {
  contentWidth.value = Math.min(1100, Math.max(520, contentWidth.value + delta));
  applyTypography();
}

function applyTypography() {
  document.documentElement.style.setProperty("--reader-fs-body", `${fontSize.value}px`);
  document.documentElement.style.setProperty("--reader-lh-body", `${lineHeight.value}`);
  document.documentElement.style.setProperty("--reader-content-w", `${contentWidth.value}px`);
}
</script>

<style scoped>
:root {
  --reader-fs-body: 16px;
  --reader-lh-body: 1.5;
  --reader-content-w: 900px;
}

@media (max-width: 720px) {
  :root {
    --reader-fs-body: 13px;
  }
}

.reader-shell {
  min-height: 100vh;
  background: var(--background);
  --reader-ink: var(--foreground);
  --reader-paper: var(--background);
  --reader-gold: var(--accent-link);
  --reader-text-muted: var(--text-secondary);
  color: var(--foreground);
  font-family: inherit;
  line-height: inherit;
}

/* ── Progress bar ── */
.reader-progress-track {
  position: fixed;
  top: 0;
  left: 0;
  height: 3px;
  width: 100%;
  background: var(--surface-strong);
  z-index: 9999;
}

.reader-progress-bar {
  height: 100%;
  background: linear-gradient(90deg, var(--accent-link), var(--btn-primary-bg));
  transition: width 0.1s linear;
}

/* ── Header ── */
.reader-header {
  background: color-mix(in oklab, var(--surface-elevated) 92%, var(--background));
  color: var(--foreground);
  padding: 0 20px;
  height: 62px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  border-bottom: 1px solid var(--divide);
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  z-index: 100;
  box-shadow: 0 1px 3px color-mix(in oklab, var(--foreground) 8%, transparent);
  backdrop-filter: blur(14px);
}

.reader-header-left {
  display: flex;
  align-items: center;
  gap: 4px;
}

.reader-back-btn {
  color: var(--foreground) !important;
}

.reader-back-btn:hover {
  background: var(--surface-muted) !important;
}

.reader-header-title-group {
  display: flex;
  align-items: center;
  flex: 1;
  justify-content: center;
}

.reader-header-title {
  font-size: 1rem;
  font-weight: 600;
  letter-spacing: 0.01em;
  color: var(--foreground);
  margin: 0;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 400px;
}

/* ── Settings button ── */
.reader-settings-btn {
  color: var(--foreground) !important;
}

.reader-settings-btn:hover {
  background: var(--surface-muted) !important;
}

/* ── Layout ── */
.reader-layout {
  display: flex;
  margin-top: 62px;
  min-height: calc(100vh - 62px);
  position: relative;
  z-index: 1;
}

/* ── Bottom bar ── */
.reader-bottombar {
  position: fixed;
  bottom: 0;
  left: 0;
  right: 0;
  z-index: 100;
  display: flex;
  justify-content: center;
  padding: 12px 16px calc(12px + env(safe-area-inset-bottom));
  background: color-mix(in oklab, var(--surface-elevated) 92%, var(--background));
  border-top: 1px solid var(--divide);
  backdrop-filter: blur(14px);
}

.reader-bottombar-inner {
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
  max-width: calc(var(--reader-content-w) - 128px);
  margin: 0 auto;
}

.reader-bottombar-arrow {
  flex-shrink: 0;
}

.reader-bottombar-list {
  flex: 1;
  min-width: 0;
}

.reader-bottombar-list-label {
  display: block;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* ── Chapter list modal ── */
.reader-list-modal {
  width: 560px;
  max-width: calc(100vw - 32px);
}

.reader-list-search {
  margin-bottom: 12px;
}

.reader-list {
  list-style: none;
  padding: 0;
  margin: 0;
  max-height: 50vh;
  overflow-y: auto;
}

.reader-list li {
  border-bottom: 1px solid var(--divide);
}

.reader-list li:last-child {
  border-bottom: none;
}

.reader-list-link {
  display: flex;
  align-items: baseline;
  gap: 10px;
  padding: 12px 20px 12px 22px;
  color: var(--text-secondary);
  text-decoration: none;
  font-size: 0.95rem;
  transition: background 0.2s, color 0.2s, border-color 0.15s;
  border-left: 2px solid transparent;
  cursor: pointer;
  line-height: 1.45;
  background: none;
  border-top: none;
  border-right: none;
  border-bottom: none;
  width: 100%;
  text-align: left;
}

.reader-list-link:hover {
  background: var(--surface-muted);
  color: var(--foreground);
}

.reader-list-link.active {
  background: var(--surface-muted);
  color: var(--foreground);
  border-left-color: var(--accent-link);
}

.reader-list-link.active .reader-ch-title {
  color: var(--foreground);
}

.reader-list-link:disabled {
  opacity: 0.35;
  cursor: not-allowed;
}

.reader-list-loading {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.reader-list-footer {
  text-align: center;
}

.reader-ch-title {
  flex: 1;
  min-width: 0;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  color: inherit;
}

/* ── Main content ── */
.reader-main {
  flex: 1;
  max-width: var(--reader-content-w);
  margin: 0 auto;
  padding: 56px 64px 150px;
  min-width: 0;
  background: var(--background);
}

/* ── Article ── */
.reader-article {
  max-width: 100%;
  font-family: 'Newsreader Variable', 'Newsreader', 'Georgia', serif;
  font-size: var(--reader-fs-body);
  line-height: var(--reader-lh-body);
  color: var(--reader-ink);
}

.reader-chapter-header {
  text-align: center;
  margin-bottom: 44px;
}

.reader-chapter-heading {
  font-family: 'Playfair Display Variable', 'Playfair Display', serif;
  font-size: 2.6rem;
  font-weight: 700;
  text-align: center;
  margin: 0 0 12px;
  color: var(--reader-ink);
  line-height: 1.2;
  letter-spacing: 0.01em;
}

.reader-chapter-ornament {
  text-align: center;
  color: var(--accent-link);
  font-size: 1.2em;
  letter-spacing: 0.3em;
  margin: 0;
  opacity: 0.65;
}

/* ── Body typography ── */
.reader-body {
  font-size: 1.09em;
  line-height: var(--reader-lh-body);
}

.reader-body :deep(p) {
  margin-bottom: 1.4em;
  text-align: justify;
  hyphens: auto;
  -webkit-hyphens: auto;
  orphans: 3;
  widows: 3;
}


.reader-body :deep(h1) {
  font-family: 'Playfair Display Variable', 'Playfair Display', serif;
  font-size: 2.6rem;
  font-weight: 700;
  text-align: center;
  margin: 0 0 12px;
  color: var(--reader-ink);
  line-height: 1.2;
  letter-spacing: 0.01em;
}

.reader-body :deep(h2) {
  font-family: 'Playfair Display Variable', 'Playfair Display', serif;
  font-size: 1.75rem;
  font-weight: 600;
  margin: 52px 0 20px;
  color: var(--reader-ink);
  text-align: center;
}

.reader-body :deep(h2)::before,
.reader-body :deep(h2)::after {
  content: ' — ';
  color: var(--accent-link);
  font-style: normal;
  font-size: 0.7em;
}

.reader-body :deep(h3) {
  font-family: 'Playfair Display Variable', 'Playfair Display', serif;
  font-size: 1.3rem;
  font-weight: 600;
  margin: 36px 0 14px;
  color: var(--reader-ink);
  letter-spacing: 0.04em;
}

.reader-body :deep(h4),
.reader-body :deep(h5),
.reader-body :deep(h6) {
  font-family: 'Playfair Display Variable', 'Playfair Display', serif;
  font-size: 1.05em;
  font-weight: 600;
  font-variant: small-caps;
  letter-spacing: 0.12em;
  margin: 28px 0 12px;
  color: var(--reader-text-muted);
}

.reader-body :deep(hr) {
  border: none;
  text-align: center;
  margin: 44px 0;
  color: var(--accent-link);
  font-size: 1.1em;
  letter-spacing: 0.4em;
  opacity: 0.6;
}

.reader-body :deep(hr)::after {
  content: '❧  ✦  ❧';
}

.reader-body :deep(strong) {
  font-weight: 700;
  color: var(--reader-ink);
}

/* ── Scrollbar ── */
.reader-shell :deep(::-webkit-scrollbar) {
  width: 7px;
}

.reader-shell :deep(::-webkit-scrollbar-track) {
  background: var(--surface-muted);
}

.reader-shell :deep(::-webkit-scrollbar-thumb) {
  background: color-mix(in oklab, var(--foreground) 24%, transparent);
  border-radius: 3px;
}

.reader-shell :deep(::-webkit-scrollbar-thumb:hover) {
  background: color-mix(in oklab, var(--foreground) 38%, transparent);
}

/* ── Empty / Loading states ── */
.reader-empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
  padding: 2rem 1rem;
  gap: 1rem;
}

.reader-empty-state h2 {
  margin: 0;
}

.reader-empty-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
  justify-content: center;
}

.reader-loading {
  display: flex;
  flex-direction: column;
  gap: 1.5rem;
  padding: 1rem 0;
}

/* ── Settings popover ── */
.reader-settings-popover {
  display: none;
  position: fixed;
  top: 70px;
  right: 12px;
  background: var(--surface-elevated);
  border: 1px solid var(--divide);
  border-radius: var(--radius-md);
  padding: 14px;
  z-index: 202;
  min-width: 240px;
  box-shadow: 0 8px 32px color-mix(in oklab, var(--foreground) 18%, transparent);
  flex-direction: column;
  gap: 12px;
}

.reader-settings-popover.open {
  display: flex;
}

.reader-settings-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.reader-settings-label {
  font-size: 0.82rem;
  color: var(--foreground);
  white-space: nowrap;
  user-select: none;
}

.reader-settings-divider {
  height: 1px;
  background: var(--divide);
  margin: 2px 0;
}

.reader-typo-group {
  display: flex;
  align-items: center;
  gap: 0;
  border: 1px solid var(--divide);
  border-radius: var(--radius-sm);
  overflow: hidden;
}

.reader-typo-btn {
  background: none;
  border: none;
  color: var(--foreground);
  cursor: pointer;
  width: 28px;
  height: 28px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 0.95em;
  transition: background 0.15s, color 0.15s;
  padding: 0;
  line-height: 1;
}

.reader-typo-btn:hover {
  background: var(--surface-muted);
  color: var(--foreground);
}

.reader-typo-btn:active {
  background: var(--surface-strong);
}

.reader-typo-btn + .reader-typo-btn {
  border-left: 1px solid var(--divide);
}

.reader-typo-val {
  font-size: 0.75rem;
  color: var(--text-secondary);
  min-width: 36px;
  text-align: center;
  padding: 0 4px;
  border-left: 1px solid var(--divide);
  border-right: 1px solid var(--divide);
  line-height: 28px;
  height: 28px;
  user-select: none;
  font-variant-numeric: tabular-nums;
}

.reader-variant-group {
  display: flex;
  gap: 0;
  border: 1px solid var(--divide);
  border-radius: var(--radius-sm);
  overflow: hidden;
}

.reader-variant-btn {
  flex: 1;
  background: none;
  border: none;
  color: var(--text-secondary);
  cursor: pointer;
  font-size: 0.82rem;
  font-weight: 500;
  padding: 6px 12px;
  transition: background 0.15s, color 0.15s;
}

.reader-variant-btn + .reader-variant-btn {
  border-left: 1px solid var(--divide);
}

.reader-variant-btn:hover {
  background: var(--surface-muted);
  color: var(--foreground);
}

.reader-variant-btn.active {
  background: var(--surface-muted);
  color: var(--foreground);
  font-weight: 600;
}

/* ── Responsive: mobile ── */
@media (max-width: 720px) {
  .reader-main {
    padding: 30px 22px 130px;
  }

  .reader-header {
    padding: 0 12px;
  }

  .reader-header-title {
    font-size: 1em;
    max-width: 200px;
  }

  .reader-chapter-heading {
    font-size: 1.75em;
  }
}

@media (max-width: 480px) {
  .reader-main {
    padding: 24px 16px 120px;
  }

  .reader-header-title {
    font-size: 0.92em;
    max-width: 160px;
  }
}

/* ── Footnotes ── */
.reader-body :deep(sup a[href^="#fnref:"]) {
  display: inline-block;
  font-size: 0.72em;
  line-height: 1;
  vertical-align: super;
  color: var(--accent-link);
  text-decoration: none;
  font-weight: 600;
  padding: 0 0.2em;
  border-radius: 3px;
  transition: background 0.15s, color 0.15s;
  cursor: pointer;
  letter-spacing: 0.02em;
}

.reader-body :deep(sup a[href^="#fnref:"]:hover),
.reader-body :deep(sup a[href^="#fnref:"]:focus-visible) {
  background: color-mix(in oklab, var(--accent-link) 12%, transparent);
  color: var(--foreground);
  outline: none;
}

.reader-body :deep(sup a[href^="#fnref:"]:focus-visible) {
  box-shadow: 0 0 0 2px var(--accent-link);
}

.reader-body :deep(.footnotes) {
  counter-reset: footnote;
  margin-top: 48px;
  padding-top: 24px;
  border-top: 1px solid var(--divide);
  font-size: 0.85em;
  line-height: 1.6;
  color: var(--text-secondary);
  list-style: none;
  padding-left: 0;
}

.reader-body :deep(.footnotes li) {
  counter-increment: footnote;
  padding: 0.25em 0;
  padding-left: 1.5em;
  text-indent: -1.5em;
  position: relative;
}

.reader-body :deep(.footnotes li::before) {
  content: counter(footnote);
  position: absolute;
  left: 0;
  top: 0.25em;
  font-size: 0.75em;
  color: var(--accent-link);
  font-weight: 600;
}

.reader-body :deep(.footnotes a[href^="#fnref:"]) {
  font-size: 0.85em;
  color: var(--text-secondary);
  text-decoration: none;
  padding: 0 0.2em;
  border-radius: 2px;
  transition: background 0.15s, color 0.15s;
}

.reader-body :deep(.footnotes a[href^="#fnref:"]:hover),
.reader-body :deep(.footnotes a[href^="#fnref:"]:focus-visible) {
  background: color-mix(in oklab, var(--accent-link) 10%, transparent);
  color: var(--accent-link);
  outline: none;
}

.reader-body :deep(.footnotes a[href^="#fnref:"]:focus-visible) {
  box-shadow: 0 0 0 2px var(--accent-link);
}

</style>
