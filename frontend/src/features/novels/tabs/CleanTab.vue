<template>
  <section class="stack-md tab-panel" aria-labelledby="tab-clean">
    <h2 id="tab-clean" class="sr-only">Limpieza de texto</h2>
    <n-card title="Limpieza de texto">
      <div v-if="cleanAllSummariesLoading" class="stack-md">
        <n-skeleton width="100%" height="8rem" style="border-radius: 12px" />
        <n-skeleton width="100%" height="12rem" style="border-radius: 12px" />
      </div>
      <div v-else class="stack-md">
        <div class="row-wrap">
          <div style="min-width: 240px; flex: 1">
            <label class="small muted">Modo de limpieza</label>
            <n-select v-model:value="mode" :options="cleanModeOptions" />
            <div class="small muted" style="margin-top: 0.4rem">{{ cleanModeDescription }}</div>
          </div>
          <div style="min-width: 220px; flex: 1">
            <label class="small muted">Aplicar a</label>
            <n-select v-model:value="applyTo" :options="cleanApplyOptions" />
          </div>
        </div>

        <div class="row-wrap">
          <div style="min-width: 240px; flex: 1">
            <label class="small muted">Buscar</label>
            <n-input v-model:value="searchText" :disabled="mode === 'remove_multiple_blanks'" />
          </div>
          <div v-if="mode === 'search_replace'" style="min-width: 240px; flex: 1">
            <label class="small muted">Reemplazar con</label>
            <n-input v-model:value="replaceText" />
          </div>
        </div>

        <div class="row-wrap">
          <div style="display: flex; align-items: center; gap: 0.5rem">
            <n-switch v-model:value="caseSensitive" />
            <span class="small muted">Distinguir mayúsculas</span>
          </div>
          <div style="display: flex; align-items: center; gap: 0.5rem">
            <n-switch v-model:value="useRegex" />
            <span class="small muted">Usar regex</span>
          </div>
        </div>
      </div>
    </n-card>

    <n-card title="Capítulos a limpiar">
      <div class="stack-md">
        <div class="row-between">
          <div class="row-wrap small muted">
            <n-button size="small" text @click="selectAll">Todos</n-button>
            <n-button size="small" text @click="clear">Ninguno</n-button>
            <span>{{ eligibleChapters.length }} capítulos con contenido</span>
          </div>
          <div class="row-wrap">
            <n-button :loading="previewLoading" :disabled="selectedIds.size === 0" @click="previewSelected">
              <template #icon><n-icon><EyeOutline /></n-icon></template>
              Previsualizar ({{ selectedIds.size }})
            </n-button>
            <n-button type="primary" :loading="applying" :disabled="selectedIds.size === 0" @click="applyToSelected">
              <template #icon><n-icon><SaveOutline /></n-icon></template>
              Aplicar a {{ selectedIds.size }} capítulos
            </n-button>
          </div>
        </div>

        <div v-if="eligibleChapters.length === 0" class="muted small">No hay capítulos con contenido para el tipo seleccionado.</div>
        <div v-else style="border: 1px solid var(--divide); border-radius: 12px; overflow: auto; max-height: 320px">
          <div v-for="chapter in eligibleChapters" :key="chapter.id" style="display: flex; gap: 0.75rem; align-items: center; padding: 0.875rem 1rem; border-bottom: 1px solid var(--divide)">
            <n-checkbox :checked="selectedIds.has(chapter.id)" @update:checked="toggle(chapter.id, $event)" />
            <span class="mono small muted" style="width: 48px">#{{ chapter.chapterOrder }}</span>
            <span style="flex: 1">{{ chapter.title }}</span>
            <n-button size="small" secondary @click="previewChapter(chapter)">Previsualizar</n-button>
          </div>
        </div>
      </div>
    </n-card>

    <n-modal
      v-model:show="previewOpen"
      preset="card"
      title="Vista previa de limpieza"
      :style="{ width: 'min(880px, 96vw)' }"
    >
      <div class="stack-md">
        <n-alert v-if="previewItems.length === 0" type="warning">
          Ninguno de los {{ previewTotal }} capítulos se verá afectado por la limpieza actual.
        </n-alert>
        <n-alert v-else type="info">
          Se modificarán {{ previewItems.length }} de {{ previewTotal }} capítulos seleccionados.
        </n-alert>

        <div class="clean-preview-list">
          <div
            v-for="item in previewDisplay"
            :key="item.chapterId"
            class="clean-preview-item"
          >
            <div class="row-between" style="margin-bottom: 0.75rem">
              <span class="small muted">#{{ item.chapterOrder }} · {{ item.chapterTitle }}</span>
              <n-tag size="small" round type="warning">−{{ item.removedLines }} líneas</n-tag>
            </div>
            <div v-if="item.hunks.length === 0" class="small muted">Sin cambios de líneas.</div>
            <div v-else class="clean-preview-hunks">
              <div
                v-for="(hunk, hunkIndex) in item.hunks"
                :key="hunkIndex"
                class="clean-preview-hunk"
              >
                <div
                  v-for="(line, lineIndex) in hunk.before"
                  :key="`b-${hunkIndex}-${lineIndex}`"
                  class="clean-diff-line clean-diff-before"
                >− {{ line }}</div>
                <div v-if="hunk.beforeHidden > 0" class="small muted" style="padding: 0.25rem 0.75rem">… y {{ hunk.beforeHidden }} líneas eliminadas más</div>
                <div
                  v-for="(line, lineIndex) in hunk.after"
                  :key="`a-${hunkIndex}-${lineIndex}`"
                  class="clean-diff-line clean-diff-after"
                >+ {{ line }}</div>
                <div v-if="hunk.afterHidden > 0" class="small muted" style="padding: 0.25rem 0.75rem">… y {{ hunk.afterHidden }} líneas añadidas más</div>
              </div>
            </div>
          </div>
        </div>
      </div>
      <template #action>
        <n-button secondary @click="previewOpen = false">Cerrar</n-button>
        <n-button type="primary" :loading="applying" :disabled="previewItems.length === 0" @click="applyFromPreview">
          Aplicar a {{ previewItems.length }} capítulos
        </n-button>
      </template>
    </n-modal>
  </section>
</template>

<script setup lang="ts">
import { computed, toRef } from "vue";
import { useMessage, NAlert, NButton, NCard, NCheckbox, NInput, NModal, NSelect, NSkeleton, NSwitch, NTag, NIcon } from "naive-ui";
import { EyeOutline, SaveOutline } from "@vicons/ionicons5";
import type { ChapterSummary } from "@/api/types";
import { CLEAN_MODE_DESCRIPTIONS, CLEAN_MODE_LABELS, type CleanMode } from "@/utils/cleaner";
import { useCleanSelection, type CleanApplyTo } from "@/composables/useCleanSelection";
import { useAppServices } from "@/app/services";

const props = defineProps<{
  novelId: string;
  cleanAllSummaries: ChapterSummary[];
  cleanAllSummariesLoading: boolean;
}>();

const { api } = useAppServices();
const message = useMessage();

const cleanModeOptions = Object.entries(CLEAN_MODE_LABELS).map(([value, label]) => ({ value: value as CleanMode, label }));
const cleanApplyOptions: { value: CleanApplyTo; label: string }[] = [
  { value: "translated", label: "Traducción" },
  { value: "original", label: "Original" },
  { value: "refined", label: "Refinado" },
  { value: "all", label: "Todos (prioriza refinado)" },
];

const stableNovelId = toRef(props, "novelId");

const {
  mode,
  applyTo,
  searchText,
  replaceText,
  caseSensitive,
  useRegex,
  selectedIds,
  applying,
  previewOpen,
  previewLoading,
  previewItems,
  previewTotal,
  eligibleChapters,
  toggle,
  selectAll,
  clear,
  buildInput,
} = useCleanSelection(
  stableNovelId,
  toRef(props, "cleanAllSummaries"),
);

const cleanModeDescription = computed(() => CLEAN_MODE_DESCRIPTIONS[mode.value]);

const previewDisplay = computed(() => previewItems.value.map((item) => {
  const maxLines = 40;
  return {
    chapterId: item.chapterId,
    chapterOrder: item.chapterOrder,
    chapterTitle: item.chapterTitle,
    removedLines: item.removedLines,
    hunks: item.changes.map((hunk) => {
      const before = hunk.before ?? [];
      const after = hunk.after ?? [];
      return {
        before: before.length > maxLines ? before.slice(0, maxLines) : before,
        after: after.length > maxLines ? after.slice(0, maxLines) : after,
        beforeHidden: Math.max(0, before.length - maxLines),
        afterHidden: Math.max(0, after.length - maxLines),
      };
    }),
  };
}));

async function runPreview(chapterIds: string[]) {
  if (chapterIds.length === 0) return;
  previewLoading.value = true;
  try {
    const res = await api.chapters.cleanPreviewBulk(props.novelId, buildInput(chapterIds));
    previewItems.value = res.items;
    previewTotal.value = res.total;
    previewOpen.value = true;
  } catch (err) {
    message.error(`Error al previsualizar: ${err instanceof Error ? err.message : String(err)}`, { duration: 4000 });
  } finally {
    previewLoading.value = false;
  }
}

function previewSelected() {
  void runPreview(Array.from(selectedIds.value));
}

function previewChapter(chapter: ChapterSummary) {
  void runPreview([chapter.id]);
}

async function apply(chapterIds: string[]) {
  applying.value = true;
  try {
    const result = await api.chapters.clean(props.novelId, buildInput(chapterIds));
    message.success(`Limpieza aplicada a ${result.modified} capítulos.`, { duration: 4000 });
    const issues: string[] = [];
    if (result.skipped) issues.push(`${result.skipped} sin contenido aplicable`);
    if (result.notFound) issues.push(`${result.notFound} no encontrados`);
    if (result.failed) issues.push(`${result.failed} fallaron al guardar`);
    if (issues.length > 0) {
      message.warning(issues.join(", ") + ".", { duration: 5000 });
    }
  } catch (err) {
    message.error(`Error al aplicar limpieza: ${err instanceof Error ? err.message : String(err)}`, { duration: 4000 });
  } finally {
    applying.value = false;
  }
}

function applyToSelected() {
  void apply(Array.from(selectedIds.value));
}

function applyFromPreview() {
  const chapterIds = previewItems.value.map((item) => item.chapterId);
  previewOpen.value = false;
  void apply(chapterIds);
}
</script>

<style scoped>
.clean-preview-list {
  display: flex;
  flex-direction: column;
  gap: 1rem;
  max-height: 62vh;
  overflow: auto;
}

.clean-preview-item {
  border: 1px solid var(--divide);
  border-radius: 12px;
  padding: 0.875rem 1rem;
}

.clean-preview-hunks {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.clean-preview-hunk {
  border: 1px solid var(--divide);
  border-radius: 8px;
  overflow: hidden;
}

.clean-diff-line {
  font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, monospace);
  font-size: 0.8125rem;
  line-height: 1.45;
  padding: 0.125rem 0.75rem;
  white-space: pre-wrap;
  word-break: break-word;
}

.clean-diff-before {
  color: color-mix(in oklab, var(--danger) 82%, var(--text-primary));
  background: color-mix(in oklab, var(--danger) 9%, transparent);
}

.clean-diff-after {
  color: color-mix(in oklab, var(--success) 82%, var(--text-primary));
  background: color-mix(in oklab, var(--success) 9%, transparent);
}
</style>
