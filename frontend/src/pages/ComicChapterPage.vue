<template>
  <AppLayout>
    <div v-if="!chapter && loading" class="stack-md" style="padding-top: 1.5rem">
      <n-skeleton width="40%" height="1.75rem" />
      <n-skeleton v-for="i in 4" :key="i" width="90%" height="120px" style="border-radius: 12px" />
    </div>

    <div v-else-if="!chapter" class="stack-md">
      <n-alert type="warning">Capítulo no encontrado.</n-alert>
      <n-button secondary @click="backToComic">Volver</n-button>
    </div>

    <div v-else class="comic-chapter-editor">
      <div class="wb-bar">
        <div class="wb-bar-start">
          <n-button quaternary circle size="small" aria-label="Volver al cómic" @click="backToComic">
            <template #icon><n-icon><ArrowBackOutline /></n-icon></template>
          </n-button>
          <div class="wb-bar-titles">
            <span class="wb-bar-title">{{ chapter.title || `Capítulo ${chapter.chapterOrder}` }}</span>
            <span class="wb-bar-sub muted">
              {{ chapter.pageCount }} página(s)
              <template v-if="comic"> · {{ getComicDisplayTitle(comic) }}</template>
            </span>
          </div>
          <n-tag :type="statusTagType(chapter.status)" size="small" :bordered="false">
            {{ statusLabels[chapter.status] || chapter.status }}
          </n-tag>
        </div>

        <div class="wb-bar-end">
          <n-button
            v-if="chapter.status === 'failed' || chapter.status === 'partial'"
            size="small"
            secondary
            :loading="redownloading"
            @click="redownloadThis"
          >
            Re-descargar
          </n-button>
          <n-button size="small" type="primary" secondary @click="readHere">
            Leer aquí
          </n-button>
        </div>
      </div>

      <n-alert type="info" :bordered="false" class="wip-banner">
        La traducción de cómics está en construcción: aquí se administran las
        páginas del capítulo; el flujo de traducción llegará en una fase
        posterior.
      </n-alert>

      <div v-if="chapter.errorMessage" class="comic-chapter-error">
        <n-alert type="error">{{ chapter.errorMessage }}</n-alert>
      </div>

      <div v-if="pageList.length" class="page-grid">
        <a
          v-for="page in pageList"
          :key="page.id"
          :href="page.url"
          target="_blank"
          rel="noopener"
          class="page-thumb"
          :aria-label="`Página ${page.pageOrder}`"
        >
          <img :src="page.url" :alt="`Página ${page.pageOrder}`" loading="lazy" />
          <span class="page-thumb-num">{{ page.pageOrder }}</span>
        </a>
      </div>
      <n-empty v-else description="Este capítulo todavía no tiene páginas descargadas." />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import {
  NAlert,
  NButton,
  NEmpty,
  NIcon,
  NSkeleton,
  NTag,
  useMessage,
} from "naive-ui";
import { ArrowBackOutline } from "@vicons/ionicons5";
import AppLayout from "@/components/AppLayout.vue";
import { useAppServices } from "@/app/services";
import { getComicDisplayTitle, type Comic, type ComicChapter } from "@/domain";

const route = useRoute();
const router = useRouter();
const { api } = useAppServices();
const message = useMessage();

const comic = ref<Comic | null>(null);
const chapter = ref<ComicChapter | null>(null);
const loading = ref(true);
const redownloading = ref(false);

const statusLabels: Record<string, string> = {
  pending: "Pendiente",
  processing: "Descargando",
  done: "Descargado",
  failed: "Fallido",
  partial: "Parcial",
};

function statusTagType(status: string): "default" | "success" | "error" | "warning" {
  if (status === "done") return "success";
  if (status === "failed") return "error";
  if (status === "processing" || status === "partial") return "warning";
  return "default";
}

const pageList = computed(() => chapter.value?.pages ?? []);

async function load() {
  const comicId = route.params.comicId as string;
  const chapterId = route.params.chapterId as string;
  loading.value = true;
  try {
    const [c, ch] = await Promise.all([
      api.comics.get(comicId),
      api.comics.chapter(comicId, chapterId).catch(() => null),
    ]);
    comic.value = c;
    chapter.value = ch;
  } finally {
    loading.value = false;
  }
}

function backToComic() {
  if (!comic.value) {
    router.push("/comics");
    return;
  }
  router.push(`/comics/${comic.value.id}`);
}

function readHere() {
  if (!comic.value) return;
  router.push({
    path: `/comics/${comic.value.id}/read`,
    query: { chapter: chapter.value?.id },
  });
}

async function redownloadThis() {
  if (!comic.value || !chapter.value) return;
  redownloading.value = true;
  try {
    await api.comics.redownloadFromUrl(comic.value.id, [chapter.value.id]);
    message.success("Re-descarga encolada.");
  } catch (err) {
    message.error(err instanceof Error ? err.message : "No se pudo encolar.");
  } finally {
    redownloading.value = false;
  }
}

onMounted(load);
</script>

<style scoped>
.comic-chapter-editor {
  display: flex;
  flex-direction: column;
  gap: 1rem;
  padding: 0 0 3rem;
}

.wb-bar {
  position: sticky;
  top: 0;
  z-index: 20;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  min-height: 56px;
  padding: 0.5rem 0;
  background: color-mix(in oklab, var(--page-bg) 88%, transparent);
  backdrop-filter: blur(12px);
  border-bottom: 1px solid var(--divide);
}

.wb-bar-start {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  min-width: 0;
}

.wb-bar-titles {
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.wb-bar-title {
  font-weight: 600;
  font-size: 0.9375rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.wb-bar-sub {
  font-size: 0.75rem;
}

.wb-bar-end {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  flex-shrink: 0;
}

.wip-banner {
  margin-top: 0.5rem;
}

.page-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
  gap: 0.75rem;
}

.page-thumb {
  position: relative;
  display: block;
  border: 1px solid var(--divide);
  border-radius: var(--radius-sm);
  overflow: hidden;
  background: var(--surface-muted);
  aspect-ratio: 2 / 3;
}

.page-thumb img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.page-thumb-num {
  position: absolute;
  bottom: 0.25rem;
  right: 0.375rem;
  padding: 0.05rem 0.4rem;
  border-radius: var(--radius-pill);
  background: color-mix(in oklab, var(--surface-elevated) 85%, transparent);
  font-size: 0.6875rem;
  color: var(--text-secondary);
}
</style>
