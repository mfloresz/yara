<template>
  <AppLayout>
    <div v-if="!chapter" class="stack-md">
      <n-button secondary @click="router.push(`/novels/${novelId}`)">
        <template #icon><n-icon><ArrowBackOutline /></n-icon></template>
        Volver
      </n-button>
      <n-spin v-if="chaptersLoading || novelLoading" :size="48" />
      <n-alert v-else type="warning" title="Capítulo no encontrado." />
    </div>

    <div v-else class="stack-lg">
      <n-button secondary @click="router.push(`/novels/${novelId}`)">
        <template #icon><n-icon><ArrowBackOutline /></n-icon></template>
        Volver a capítulos
      </n-button>

      <div class="row-between">
        <div style="min-width: 0">
          <h1 style="margin: 0 0 0.25rem">{{ translatedTitle || title }}</h1>
          <div v-if="translatedTitle" class="small muted">{{ title }}</div>
          <div class="row-wrap" style="margin-top: 0.75rem">
            <n-tag size="small" round> #{{ chapter.chapterOrder }} </n-tag>
            <n-tag :type="chapterTagType(displayStatus)" size="small" round>
              {{ chapterStatusLabel(displayStatus) }}
            </n-tag>
          </div>
        </div>
        <div class="row-wrap">
          <n-button
            type="primary"
            :loading="translateLoading"
            :disabled="!originalContent || chapterIsProcessing || translateLoading || refineLoading"
            @click="handleTranslate"
          >
            <template #icon><n-icon><SparklesOutline /></n-icon></template>
            Traducir
          </n-button>
          <n-button
            secondary
            :loading="refineLoading"
            :disabled="!translatedContent || chapterIsProcessing || translateLoading || refineLoading"
            @click="handleRefine"
          >
            <template #icon><n-icon><ColorWandOutline /></n-icon></template>
            Refinar
          </n-button>
          <n-button
            v-if="chapter.status === 'refined' || chapter.status === 'done'"
            type="success"
            secondary
            @click="handleMarkDone"
          >
            <template #icon><n-icon><CheckmarkOutline /></n-icon></template>
            Marcar completado
          </n-button>
          <n-button type="primary" :loading="saving" @click="handleSave">
            <template #icon><n-icon><SaveOutline /></n-icon></template>
            Guardar
          </n-button>
        </div>
      </div>

      <n-alert v-if="error" type="error" :title="error" />

      <n-card size="small">
        <div class="stack-md">
          <div class="row-wrap">
            <div style="flex: 1; min-width: 240px">
              <label class="small muted">Título original</label>
              <n-input v-model:value="title" />
            </div>
            <div style="flex: 1; min-width: 240px">
              <label class="small muted">Título traducido</label>
              <n-input v-model:value="translatedTitle" />
            </div>
          </div>
        </div>
      </n-card>

      <div class="stack-lg">
        <n-card v-for="panel in panels" :key="panel.id" :title="panel.label" size="small">
          <div class="stack-md">
            <div class="row-between">
              <div class="small muted">{{ panel.languageLabel }} · {{ panel.value.length }} chars</div>
              <div class="row-wrap">
                <n-radio-group :value="contentViewMode[panel.id]" @update:value="setPanelMode(panel.id, $event)">
                  <n-radio-button value="plain">Texto plano</n-radio-button>
                  <n-radio-button value="markdown">Markdown</n-radio-button>
                </n-radio-group>
              </div>
            </div>

            <template v-if="contentViewMode[panel.id] === 'plain'">
              <n-input
                :value="panel.value"
                type="textarea"
                :rows="14"
                :style="{ fontFamily: 'monospace' }"
                @update:value="panel.onChange($event)"
              />
            </template>
            <template v-else>
              <div class="markdown-preview" style="border: 1px solid var(--divide); border-radius: 12px; padding: 1rem; min-height: 220px" v-html="markdownToHtml(panel.value || panel.placeholder)" />
            </template>
          </div>
        </n-card>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import {
  NButton,
  NCard,
  NInput,
  NAlert,
  NSpin,
  NRadioGroup,
  NRadioButton,
  NTag,
  NIcon,
} from "naive-ui";
import {
  ArrowBackOutline,
  SparklesOutline,
  ColorWandOutline,
  CheckmarkOutline,
  SaveOutline,
} from "@vicons/ionicons5";
import AppLayout from "@/components/AppLayout.vue";
import { useNovels } from "@/composables/useNovels";
import { useActiveJobStatus } from "@/composables/useActiveJobStatus";
import { useAppServices } from "@/app/services";
import { type Chapter, type Novel } from "@/domain";
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
const chapter = ref<Chapter | null>(null);
const chaptersLoading = ref(false);

const title = ref("");
const translatedTitle = ref("");
const originalContent = ref("");
const translatedContent = ref("");
const refinedContent = ref("");
const saving = ref(false);
const translateLoading = ref(false);
const refineLoading = ref(false);
const error = ref<string | null>(null);
const contentViewMode = reactive<Record<"original" | "translated" | "refined", "plain" | "markdown">>({
  original: "plain",
  translated: "plain",
  refined: "plain",
});

const chapterIsProcessing = computed(() => chapter.value?.status === "processing");
const displayStatus = computed<Chapter["status"]>(() => {
  if (chapterIsProcessing.value) return "processing";
  return chapter.value?.status ?? "pending";
});
const panels = computed(() => [
  {
    id: "original" as const,
    label: "Contenido original",
    languageLabel: novel.value?.sourceLanguage || "origen",
    value: originalContent.value,
    placeholder: "Sin contenido original",
    onChange: (value: string) => { originalContent.value = value; },
  },
  {
    id: "translated" as const,
    label: "Contenido traducido",
    languageLabel: novel.value?.targetLanguage || "destino",
    value: translatedContent.value,
    placeholder: "Sin contenido traducido",
    onChange: (value: string) => { translatedContent.value = value; },
  },
  {
    id: "refined" as const,
    label: "Contenido refinado",
    languageLabel: novel.value?.targetLanguage || "destino",
    value: refinedContent.value,
    placeholder: "Sin contenido refinado",
    onChange: (value: string) => { refinedContent.value = value; },
  },
]);

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

function syncChapterFields(next: Chapter, options: { replaceOriginalFields?: boolean } = {}) {
  const { replaceOriginalFields = true } = options;
  chapter.value = next;
  if (replaceOriginalFields) {
    title.value = next.title;
    originalContent.value = next.originalContent || "";
  }
  translatedTitle.value = next.translatedTitle || "";
  translatedContent.value = next.translatedContent || "";
  refinedContent.value = next.refinedContent || "";
}

function markChapterProcessing() {
  if (!chapter.value) return;
  syncChapterFields({
    ...chapter.value,
    status: "processing",
    errorMessage: "",
  }, { replaceOriginalFields: false });
}

async function loadChapter(options: { replaceOriginalFields?: boolean } = {}) {
  if (!novelId.value || !chapterId.value) {
    chapter.value = null;
    return null;
  }
  chaptersLoading.value = true;
  try {
    const next = await api.chapters.get(novelId.value, chapterId.value);
    if (!next) {
      chapter.value = null;
      return null;
    }
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
  void loadChapter();
}, { immediate: true });

watch(hasActive, (active, previous) => {
  if (!previous || active || chapter.value?.status !== "processing") return;
  void loadChapter({ replaceOriginalFields: false });
});

function chapterStatusLabel(status: Chapter["status"]) {
  return {
    pending: "Pendiente",
    processing: "Procesando",
    translated: "Traducido",
    refined: "Refinado",
    done: "Completado",
    failed: "Error",
  }[status] || status;
}

function chapterTagType(status: Chapter["status"]) {
  return ({
    pending: "default",
    processing: "warning",
    translated: "success",
    refined: "info",
    done: "success",
    failed: "error",
  }[status] || "default") as "default" | "info" | "warning" | "success" | "error";
}

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

function setPanelMode(id: "original" | "translated" | "refined", mode: "plain" | "markdown") {
  contentViewMode[id] = mode;
}
</script>
