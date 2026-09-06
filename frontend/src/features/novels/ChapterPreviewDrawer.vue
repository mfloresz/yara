<template>
  <n-drawer
    :show="open"
    placement="right"
    :width="drawerWidth"
    @update:show="(show: boolean) => emit('update:open', show)"
  >
    <n-drawer-content closable>
      <template #header>
        <div class="chapter-preview-header">
          <span v-if="chapter" class="mono small muted">#{{ String(chapterPosition(chapter)).padStart(2, "0") }}</span>
          <span class="chapter-preview-title">{{ chapter?.title }}</span>
        </div>
      </template>

      <div v-if="chapter" class="stack-md">
        <div class="row-wrap chapter-preview-meta">
          <n-tag :type="chapterTagType(status)" size="small" round>
            {{ chapterStatusLabel(status) }}
          </n-tag>
          <span v-if="metaLine" class="small muted">{{ metaLine }}</span>
        </div>

        <div v-if="chapterLoading" class="stack-sm">
          <n-skeleton width="40%" height="1.5rem" />
          <n-skeleton width="100%" height="6rem" />
          <n-skeleton width="100%" height="6rem" />
          <n-skeleton width="100%" height="6rem" />
        </div>

        <n-alert v-else-if="!hasAnyContent" type="info">
          Este capítulo todavía no tiene contenido.
        </n-alert>

        <template v-else>
          <n-tabs v-if="availableVariants.length > 1" v-model:value="activeVariant" type="segment" size="small">
            <n-tab v-for="variant in availableVariants" :key="variant.key" :name="variant.key" :disabled="variant.disabled">
              {{ variant.label }}
            </n-tab>
          </n-tabs>
          <div class="chapter-preview-body markdown-preview" v-html="previewHtml" />
        </template>
      </div>

      <template v-if="isOwner && chapter" #footer>
        <div class="row-wrap chapter-preview-actions">
          <n-button
            secondary
            :disabled="!chapter.hasOriginalContent"
            :loading="jobLoading === 'translate'"
            @click="runJob('translate')"
          >
            <template #icon><n-icon><LanguageOutline /></n-icon></template>
            Traducir
          </n-button>
          <n-button
            secondary
            :disabled="!chapter.hasTranslatedContent"
            :loading="jobLoading === 'refine'"
            @click="runJob('refine')"
          >
            <template #icon><n-icon><SparklesOutline /></n-icon></template>
            Refinar
          </n-button>
          <n-button type="primary" @click="editChapter">
            <template #icon><n-icon><CreateOutline /></n-icon></template>
            Editar capítulo
          </n-button>
        </div>
      </template>
    </n-drawer-content>
  </n-drawer>
</template>

<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useRouter } from "vue-router";
import { useMessage } from "naive-ui";
import { NAlert, NButton, NDrawer, NDrawerContent, NIcon, NSkeleton, NTab, NTabs, NTag } from "naive-ui";
import { CreateOutline, LanguageOutline, SparklesOutline } from "@vicons/ionicons5";
import type { ChapterSummary } from "@/api/types";
import { useAppServices } from "@/app/services";
import { chapterPosition, type Chapter } from "@/domain";
import { chapterStatusLabel, chapterTagType, resolvedChapterStatus } from "@/composables/useChapterStatus";
import { markdownToHtml } from "@/utils/markdown";

type ContentVariant = "original" | "translated" | "refined";

const props = defineProps<{
  open: boolean;
  chapter: ChapterSummary | null;
  isOwner: boolean;
  createJob: (chapterIds: string[], operation: "translate" | "refine") => Promise<unknown>;
}>();

const emit = defineEmits<{
  (e: "update:open", open: boolean): void;
  (e: "job-created", chapterId: string): void;
}>();

const router = useRouter();
const message = useMessage();
const { api } = useAppServices();

const drawerWidth = "min(720px, 100vw)";

const fullChapter = ref<Chapter | null>(null);
const chapterLoading = ref(false);
const activeVariant = ref<ContentVariant>("original");
const jobLoading = ref<"translate" | "refine" | null>(null);

const status = computed(() => (props.chapter ? resolvedChapterStatus(props.chapter) : "pending"));

const hasAnyContent = computed(() =>
  !!(fullChapter.value?.originalContent || fullChapter.value?.translatedContent || fullChapter.value?.refinedContent),
);

const availableVariants = computed(() => {
  const variants: Array<{ key: ContentVariant; label: string; disabled: boolean }> = [
    { key: "refined", label: "Refinado", disabled: !fullChapter.value?.refinedContent },
    { key: "translated", label: "Traducido", disabled: !fullChapter.value?.translatedContent },
    { key: "original", label: "Original", disabled: !fullChapter.value?.originalContent },
  ];
  return variants.filter((variant) => !variant.disabled || variant.key === activeVariant.value);
});

const variantContent: Record<ContentVariant, (chapter: Chapter | null) => string> = {
  refined: (chapter) => chapter?.refinedContent || "",
  translated: (chapter) => chapter?.translatedContent || "",
  original: (chapter) => chapter?.originalContent || "",
};

const previewHtml = computed(() => markdownToHtml(variantContent[activeVariant.value](fullChapter.value)));

const metaLine = computed(() => {
  const parts: string[] = [];
  if (props.chapter?.originalChars) parts.push(`${props.chapter.originalChars.toLocaleString("es")} car. originales`);
  if (props.chapter?.translatedChars) parts.push(`${props.chapter.translatedChars.toLocaleString("es")} car. traducidos`);
  return parts.join(" · ");
});

watch(
  () => [props.open, props.chapter?.id] as const,
  async ([open]) => {
    if (!open || !props.chapter) return;
    fullChapter.value = null;
    chapterLoading.value = true;
    try {
      fullChapter.value = await api.chapters.get(props.chapter.novelId, props.chapter.id);
    } catch (err) {
      message.error(`Error al cargar el capítulo: ${err instanceof Error ? err.message : String(err)}`, { duration: 4000 });
    } finally {
      chapterLoading.value = false;
    }
    activeVariant.value = fullChapter.value?.refinedContent
      ? "refined"
      : fullChapter.value?.translatedContent
        ? "translated"
        : "original";
  },
  { immediate: true },
);

async function runJob(operation: "translate" | "refine") {
  if (!props.chapter || jobLoading.value) return;
  jobLoading.value = operation;
  try {
    await props.createJob([props.chapter.id], operation);
    emit("job-created", props.chapter.id);
    message.success(`Capítulo en cola de ${operation === "translate" ? "traducción" : "refinado"}`, { duration: 2500 });
  } catch (err) {
    message.error(`Error al crear el trabajo: ${err instanceof Error ? err.message : String(err)}`, { duration: 4000 });
  } finally {
    jobLoading.value = null;
  }
}

function editChapter() {
  if (!props.chapter) return;
  emit("update:open", false);
  void router.push(`/novels/${props.chapter.novelId}/chapters/${props.chapter.id}`);
}
</script>

<style scoped>
.chapter-preview-header {
  display: flex;
  align-items: baseline;
  gap: 0.5rem;
  min-width: 0;
}

.chapter-preview-title {
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.chapter-preview-meta {
  align-items: center;
}

.chapter-preview-body {
  overflow-wrap: anywhere;
}

.chapter-preview-actions {
  justify-content: flex-end;
}
</style>
