<template>
  <div v-if="!ready" class="reader-loading">
    <n-spin size="large" />
  </div>

  <div v-else-if="!comic" class="reader-error">
    <p>Cómic no encontrado.</p>
    <n-button @click="router.push('/comics')">Volver</n-button>
  </div>

  <div v-else class="comic-reader" :style="{ '--reader-width': `${contentWidth}px` }">
    <div class="reader-progress-track" aria-hidden="true">
      <div class="reader-progress-bar" :style="{ width: `${scrollPercent}%` }" />
    </div>

    <header class="reader-topbar">
      <n-button quaternary circle size="small" aria-label="Volver al cómic" @click="goToComic">
        <template #icon><n-icon><ArrowBackOutline /></n-icon></template>
      </n-button>
      <div class="reader-topbar-titles">
        <span class="reader-topbar-title">{{ getComicDisplayTitle(comic) }}</span>
        <span v-if="currentChapter" class="reader-topbar-sub muted">
          {{ currentChapter.title || `Capítulo ${currentChapter.chapterOrder}` }}
        </span>
      </div>
      <div class="reader-topbar-actions">
        <n-button
          quaternary
          circle
          size="small"
          aria-label="Reducir ancho"
          :disabled="contentWidth <= minWidth"
          @click="changeWidth(-40)"
        >
          <template #icon><n-icon><RemoveOutline /></n-icon></template>
        </n-button>
        <n-button
          quaternary
          circle
          size="small"
          aria-label="Aumentar ancho"
          :disabled="contentWidth >= maxWidth"
          @click="changeWidth(40)"
        >
          <template #icon><n-icon><AddOutline /></n-icon></template>
        </n-button>
        <n-button quaternary circle size="small" aria-label="Lista de capítulos" @click="chapterListOpen = true">
          <template #icon><n-icon><ListOutline /></n-icon></template>
        </n-button>
      </div>
    </header>

    <main ref="scrollRoot" class="reader-body" @scroll.passive="onScroll">
      <div v-if="currentChapter && currentChapter.pages.length" class="reader-pages">
        <img
          v-for="page in currentChapter.pages"
          :key="page.id"
          :src="page.url"
          :alt="`Página ${page.pageOrder} de ${currentChapter.title || currentChapter.chapterOrder}`"
          loading="lazy"
          class="reader-page"
        />
      </div>
      <div v-else class="reader-empty">
        <p class="muted">Este capítulo no tiene páginas descargadas.</p>
        <n-button secondary size="small" @click="goToComic">Ver capítulos</n-button>
      </div>
    </main>

    <footer class="reader-bottombar">
      <n-button
        quaternary
        size="small"
        :disabled="!neighbors.prev"
        aria-label="Capítulo anterior"
        @click="goChapter(neighbors.prev?.id)"
      >
        <template #icon><n-icon><ChevronBackOutline /></n-icon></template>
        Anterior
      </n-button>
      <span class="muted small reader-bottombar-info">
        {{ currentChapter?.chapterOrder }} / {{ chapters.length || "—" }}
      </span>
      <n-button
        quaternary
        size="small"
        :disabled="!neighbors.next"
        aria-label="Capítulo siguiente"
        @click="goChapter(neighbors.next?.id)"
      >
        Siguiente
        <template #icon><n-icon><ChevronForwardOutline /></n-icon></template>
      </n-button>
    </footer>

    <n-modal
      v-model:show="chapterListOpen"
      preset="card"
      title="Capítulos"
      style="max-width: 26rem"
    >
      <n-input v-model:value="chapterQuery" size="small" clearable placeholder="Buscar…" />
      <ul class="reader-chapter-list">
        <li v-for="ch in filteredChapters" :key="ch.id">
          <button
            class="reader-chapter-item"
            :class="{ 'reader-chapter-item--current': ch.id === currentChapterId }"
            :disabled="!ch.hasPages"
            @click="goChapter(ch.id)"
          >
            <span class="reader-chapter-order muted">{{ ch.chapterOrder }}</span>
            <span class="reader-chapter-title">{{ ch.title || `Capítulo ${ch.chapterOrder}` }}</span>
          </button>
        </li>
      </ul>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import {
  NButton,
  NIcon,
  NInput,
  NModal,
  NSpin,
} from "naive-ui";
import {
  AddOutline,
  ArrowBackOutline,
  ChevronBackOutline,
  ChevronForwardOutline,
  ListOutline,
  RemoveOutline,
} from "@vicons/ionicons5";
import { useAppServices } from "@/app/services";
import {
  getComicDisplayTitle,
  type Comic,
  type ComicChapter,
  type ComicChapterSummary,
} from "@/domain";

const STORAGE_KEY = "yara-comic-reader-settings";
const minWidth = 520;
const maxWidth = 1100;

const route = useRoute();
const router = useRouter();
const { api } = useAppServices();

const comic = ref<Comic | null>(null);
const chapters = ref<ComicChapterSummary[]>([]);
const currentChapterId = ref<string | null>(null);
const currentChapter = ref<ComicChapter | null>(null);
const neighbors = ref<{ prev?: ComicChapterSummary; next?: ComicChapterSummary }>({});
const ready = ref(false);
const chapterListOpen = ref(false);
const chapterQuery = ref("");
const scrollRoot = ref<HTMLElement | null>(null);
const scrollPercent = ref(0);

const contentWidth = ref(760);
try {
  const saved = JSON.parse(localStorage.getItem(STORAGE_KEY) || "{}");
  if (typeof saved.contentWidth === "number") {
    contentWidth.value = Math.min(maxWidth, Math.max(minWidth, saved.contentWidth));
  }
} catch {
  /* defaults */
}

let progressTimer: ReturnType<typeof setInterval> | null = null;
let lastReportedChapter: string | null = null;

const filteredChapters = computed(() => {
  const query = chapterQuery.value.trim().toLowerCase();
  if (!query) return chapters.value;
  return chapters.value.filter(
    (ch) => ch.title.toLowerCase().includes(query) || String(ch.chapterOrder).includes(query),
  );
});

function persistSettings() {
  localStorage.setItem(STORAGE_KEY, JSON.stringify({ contentWidth: contentWidth.value }));
}

function changeWidth(delta: number) {
  contentWidth.value = Math.min(maxWidth, Math.max(minWidth, contentWidth.value + delta));
  persistSettings();
}

async function loadComic() {
  const comicId = route.params.comicId as string;
  const [c, list] = await Promise.all([
    api.comics.get(comicId),
    api.comics.chapters(comicId).catch(() => []),
  ]);
  comic.value = c;
  chapters.value = ((Array.isArray(list) ? list : []) as ComicChapterSummary[]).filter(
    (ch) => !ch.excluded,
  );
}

async function loadChapter(chapterId: string) {
  if (!comic.value) return;
  currentChapterId.value = chapterId;
  const [ch, nb] = await Promise.all([
    api.comics.chapter(comic.value.id, chapterId),
    api.comics.chapterNeighbors(comic.value.id, chapterId).catch(() => ({})),
  ]);
  currentChapter.value = ch;
  neighbors.value = nb;
  lastReportedChapter = null;
  requestAnimationFrame(() => {
    scrollRoot.value?.scrollTo({ top: 0 });
  });
}

async function pickInitialChapter() {
  if (!comic.value) return;
  const deepLink = route.query.chapter?.toString();
  if (deepLink && chapters.value.some((ch) => ch.id === deepLink && ch.hasPages)) {
    await loadChapter(deepLink);
    return;
  }
  const progress = await api.comics.readingProgress.get(comic.value.id);
  const target =
    (progress?.chapterId && chapters.value.find((ch) => ch.id === progress.chapterId)) ||
    chapters.value.find((ch) => ch.hasPages) ||
    chapters.value[0];
  if (target) {
    await loadChapter(target.id);
    if (progress && progress.chapterId === target.id && progress.scrollPercent > 1) {
      requestAnimationFrame(() => {
        const el = scrollRoot.value;
        if (!el) return;
        el.scrollTo({
          top: (el.scrollHeight - el.clientHeight) * (progress.scrollPercent / 100),
        });
      });
    }
  }
}

function goChapter(chapterId?: string) {
  if (!chapterId) return;
  chapterListOpen.value = false;
  router.replace({ query: { ...route.query, chapter: chapterId } });
}

function goToComic() {
  if (!comic.value) {
    router.push("/comics");
    return;
  }
  router.push(`/comics/${comic.value.id}`);
}

function onScroll() {
  const el = scrollRoot.value;
  if (!el) return;
  const scrollable = el.scrollHeight - el.clientHeight;
  scrollPercent.value = scrollable > 0 ? Math.min(100, (el.scrollTop / scrollable) * 100) : 0;
}

async function flushProgress() {
  if (!comic.value || !currentChapterId.value) return;
  if (lastReportedChapter === currentChapterId.value && scrollPercent.value === 0) return;
  lastReportedChapter = currentChapterId.value;
  try {
    await api.comics.readingProgress.update(comic.value.id, {
      chapterId: currentChapterId.value,
      scrollPercent: Math.round(scrollPercent.value * 10) / 10,
    });
  } catch {
    /* offline: the next tick retries */
  }
}

watch(
  () => route.query.chapter,
  async (chapterId) => {
    if (chapterId && chapterId !== currentChapterId.value) {
      await loadChapter(chapterId.toString());
    }
  },
);

onMounted(async () => {
  try {
    await loadComic();
    if (comic.value) {
      await pickInitialChapter();
      if (currentChapterId.value && !route.query.chapter) {
        router.replace({ query: { ...route.query, chapter: currentChapterId.value } });
      }
    }
  } finally {
    ready.value = true;
  }
  progressTimer = setInterval(flushProgress, 30_000);
  window.addEventListener("keydown", onKeydown);
});

onUnmounted(() => {
  if (progressTimer) clearInterval(progressTimer);
  void flushProgress();
  window.removeEventListener("keydown", onKeydown);
});

function onKeydown(event: KeyboardEvent) {
  if (event.key === "ArrowLeft" && neighbors.value.prev) goChapter(neighbors.value.prev.id);
  if (event.key === "ArrowRight" && neighbors.value.next) goChapter(neighbors.value.next.id);
  if (event.key === "Escape") chapterListOpen.value = false;
}
</script>

<style scoped>
.reader-loading,
.reader-error {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 1rem;
  height: 100dvh;
  background: var(--page-bg);
}

.comic-reader {
  display: flex;
  flex-direction: column;
  height: 100dvh;
  background: #0c0c0b;
}

.reader-progress-track {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  height: 3px;
  z-index: 30;
  background: rgba(255, 255, 255, 0.12);
}

.reader-progress-bar {
  height: 100%;
  background: var(--accent-link, #b9b4a6);
  transition: width 0.1s linear;
}

.reader-topbar {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  padding: 0.5rem 0.875rem;
  color: #f5f4f2;
  background: rgba(12, 12, 11, 0.86);
  backdrop-filter: blur(10px);
  border-bottom: 1px solid rgba(255, 255, 255, 0.08);
  flex-shrink: 0;
}

.reader-topbar-titles {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
}

.reader-topbar-title {
  font-weight: 600;
  font-size: 0.9375rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.reader-topbar-sub {
  font-size: 0.75rem;
  color: rgba(245, 244, 242, 0.6);
}

.reader-topbar-actions {
  display: flex;
  align-items: center;
  gap: 0.25rem;
  flex-shrink: 0;
}

.reader-body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
}

.reader-pages {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 4px;
  max-width: var(--reader-width);
  margin: 0 auto;
  padding: 8px 0 24px;
}

.reader-page {
  display: block;
  width: 100%;
  height: auto;
}

.reader-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 0.75rem;
  padding: 4rem 1rem;
  color: #f5f4f2;
}

.reader-bottombar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  padding: 0.5rem 0.875rem calc(0.5rem + env(safe-area-inset-bottom));
  color: #f5f4f2;
  background: rgba(12, 12, 11, 0.92);
  border-top: 1px solid rgba(255, 255, 255, 0.08);
  flex-shrink: 0;
}

.reader-bottombar-info {
  color: rgba(245, 244, 242, 0.6);
  font-variant-numeric: tabular-nums;
}

.reader-chapter-list {
  list-style: none;
  margin: 0.75rem 0 0;
  padding: 0;
  max-height: 50vh;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
}

.reader-chapter-item {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  width: 100%;
  padding: 0.5rem 0.5rem;
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--foreground);
  font-size: 0.875rem;
  text-align: left;
  cursor: pointer;
}

.reader-chapter-item:hover:not(:disabled) {
  background: var(--mock-row);
}

.reader-chapter-item:disabled {
  opacity: 0.4;
  cursor: default;
}

.reader-chapter-item--current {
  background: var(--surface-strong);
  font-weight: 600;
}

.reader-chapter-order {
  width: 2.5rem;
  text-align: right;
  font-variant-numeric: tabular-nums;
  font-size: 0.8125rem;
  flex-shrink: 0;
}

.reader-chapter-title {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
