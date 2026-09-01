<template>
  <section class="stack-md tab-panel" aria-labelledby="tab-translate">
    <h2 id="tab-translate" class="sr-only">{{ operation === 'translate' ? 'Traducción' : 'Refinamiento' }}</h2>
    <n-card :title="operation === 'translate' ? 'Traducción automática' : 'Refinamiento'">
      <div v-if="allSummariesLoading || (showAll && translateAllLoading)" class="stack-md">
        <n-skeleton width="100%" height="8rem" style="border-radius: 12px" />
        <n-skeleton width="100%" height="14rem" style="border-radius: 12px" />
      </div>
      <div v-else class="stack-md">
        <div class="row-between">
          <div class="row-wrap" style="gap: 0.5rem">
            <n-button-group size="small">
              <n-button :type="operation === 'translate' ? 'primary' : 'default'" @click="setOperation('translate')">Traducir</n-button>
              <n-button :type="operation === 'refine' ? 'primary' : 'default'" @click="setOperation('refine')">Refinar</n-button>
            </n-button-group>
            <n-button-group size="small">
              <n-button :type="!showAll ? 'primary' : 'default'" @click="setShowAll(false)">Solo elegibles</n-button>
              <n-button :type="showAll ? 'primary' : 'default'" @click="setShowAll(true)">Listar Todos</n-button>
            </n-button-group>
          </div>
          <div class="row-wrap">
            <n-button type="primary" :loading="submitting" :disabled="selectedIds.size === 0 || submitting" @click="startJob">
              <template #icon><n-icon><PlayOutline /></n-icon></template>
              Iniciar ({{ selectedIds.size }})
            </n-button>
          </div>
        </div>

        <div class="row-wrap small muted">
          <n-button-group size="small">
            <n-button :type="selectionMode === 'todos' ? 'primary' : 'default'" @click="selectAll">Todos</n-button>
            <n-button :type="selectionMode === 'rango' ? 'primary' : 'default'" @click="selectionMode = 'rango'">Rango</n-button>
          </n-button-group>
          <template v-if="selectionMode === 'rango'">
            <n-input-number v-model:value="rangeFrom" size="small" :min="1" :show-button="false" placeholder="Desde #" style="width: 110px" />
            <n-input-number v-model:value="rangeTo" size="small" :min="1" :show-button="false" placeholder="Hasta #" style="width: 110px" />
            <n-button size="small" secondary @click="applyRange(message.warning)">Aplicar</n-button>
          </template>
          <n-button size="small" text @click="clear">Ninguno</n-button>
          <span>{{ selectedIds.size }} seleccionados</span>
          <span v-if="showAll"> · {{ translateAllSummaries.length }} totales</span>
        </div>

        <div v-if="!showAll && eligibleChapters.length === 0" class="muted small">Todos los capítulos ya fueron {{ operation === 'translate' ? 'traducidos' : 'refinados' }}.</div>
        <div v-else style="border: 1px solid var(--divide); border-radius: 12px; overflow: auto; max-height: 420px">
          <div v-for="chapter in sourceSummaries" :key="chapter.id" style="display: flex; gap: 0.75rem; align-items: center; padding: 0.875rem 1rem; border-bottom: 1px solid var(--divide)">
            <n-checkbox :checked="selectedIds.has(chapter.id)" :disabled="submitting || !selectableChapterIds.has(chapter.id)" @update:checked="toggle(chapter.id, $event)" />
            <span class="mono small muted" style="width: 48px">#{{ chapterPosition(chapter) }}</span>
            <span style="flex: 1; min-width: 0">{{ chapter.title }}</span>
            <n-tag :type="chapterTagType(resolvedChapterStatus(chapter))" size="small" round>{{ chapterStatusLabel(resolvedChapterStatus(chapter)) }}</n-tag>
          </div>
        </div>
      </div>
    </n-card>
  </section>
</template>

<script setup lang="ts">
import { toRef } from "vue";
import { useMessage, NButton, NButtonGroup, NCard, NCheckbox, NSkeleton, NTag, NIcon, NInputNumber } from "naive-ui";
import { PlayOutline } from "@vicons/ionicons5";
import type { ChapterSummary } from "@/api/types";
import { chapterPosition } from "@/domain";
import { chapterStatusLabel, chapterTagType, resolvedChapterStatus } from "@/composables/useChapterStatus";
import {
  useTranslateSelection,
  type TranslateOperation,
} from "@/composables/useTranslateSelection";

const props = defineProps<{
  novelId: string;
  operation: TranslateOperation;
  allSummaries: ChapterSummary[];
  allSummariesLoading: boolean;
  translateAllSummaries: ChapterSummary[];
  translateAllLoading: boolean;
  createJob: (targetIds: string[]) => Promise<void>;
  markFailedJobsDirty: () => void;
  onPatchStatus: (targetIds: string[]) => void;
}>();

const emit = defineEmits<{
  (e: "operation-change", operation: TranslateOperation): void;
  (e: "show-all-change", showAll: boolean): void;
}>();

const message = useMessage();

const stableNovelId = toRef(props, "novelId");

const {
  showAll,
  selectedIds,
  submitting,
  selectionMode,
  rangeFrom,
  rangeTo,
  eligibleChapters,
  sourceSummaries,
  selectableChapterIds,
  toggle,
  selectAll,
  clear,
  applyRange,
  onOperationChanged,
  removeFromSelection,
} = useTranslateSelection(
  stableNovelId,
  toRef(props, "operation"),
  toRef(props, "allSummaries"),
  toRef(props, "translateAllSummaries"),
);

function setOperation(next: TranslateOperation) {
  if (props.operation === next) return;
  showAll.value = false;
  onOperationChanged();
  emit("operation-change", next);
}

function setShowAll(next: boolean) {
  if (showAll.value === next) return;
  showAll.value = next;
  selectedIds.value = new Set();
  emit("show-all-change", next);
}

async function startJob() {
  const target = sourceSummaries.value.filter((chapter) => selectedIds.value.has(chapter.id));
  if (target.length === 0) return;
  submitting.value = true;
  try {
    const targetIds = target.map((chapter) => chapter.id);
    await props.createJob(targetIds);
    props.onPatchStatus(targetIds);
    removeFromSelection(targetIds);
    props.markFailedJobsDirty();
  } catch (err) {
    message.error(`Error al iniciar trabajo: ${err instanceof Error ? err.message : String(err)}`, { duration: 4000 });
  } finally {
    submitting.value = false;
  }
}
</script>
