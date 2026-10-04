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
            <n-button type="primary" :loading="previewLoading" :disabled="selectedIds.size === 0" @click="previewSelected">
              <template #icon><n-icon><EyeOutline /></n-icon></template>
              Previsualizar ({{ selectedIds.size }})
            </n-button>
          </div>
        </div>

        <div v-if="eligibleChapters.length === 0" class="muted small">No hay capítulos con contenido para el tipo seleccionado.</div>
        <div v-else style="border: 1px solid var(--divide); border-radius: 12px; overflow: auto; max-height: 320px">
          <div v-for="chapter in eligibleChapters" :key="chapter.id" style="display: flex; gap: 0.75rem; align-items: center; padding: 0.875rem 1rem; border-bottom: 1px solid var(--divide)">
            <n-checkbox :checked="selectedIds.has(chapter.id)" @update:checked="toggle(chapter.id, $event)" />
            <span class="mono small muted" style="width: 48px">#{{ chapterPosition(chapter) }}</span>
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

        <CleanDiffList :items="previewItems" />
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
import { useMessage, NAlert, NButton, NCard, NCheckbox, NInput, NModal, NSelect, NSkeleton, NSwitch, NIcon } from "naive-ui";
import { EyeOutline } from "@vicons/ionicons5";
import type { ChapterSummary } from "@/api/types";
import { chapterPosition } from "@/domain";
import { CLEAN_MODE_DESCRIPTIONS, CLEAN_MODE_LABELS, type CleanMode } from "@/utils/cleaner";
import { useCleanSelection, type CleanApplyTo } from "@/composables/useCleanSelection";
import { useAppServices } from "@/app/services";
import CleanDiffList from "@/components/CleanDiffList.vue";

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

function applyFromPreview() {
  const chapterIds = previewItems.value.map((item) => item.chapterId);
  previewOpen.value = false;
  void apply(chapterIds);
}
</script>
