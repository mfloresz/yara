<template>
  <n-modal v-model:show="visible" preset="card" title="Actualizar desde internet" :style="{ width: 'min(480px, 96vw)' }" @after-leave="reset">
    <div v-if="loading" class="preview-loading">
      <n-spin :size="40" />
      <span class="muted small">Buscando capítulos nuevos en la fuente…</span>
    </div>

    <template v-else-if="preview">
      <n-alert v-if="preview.newChapters === 0" type="success" :closable="false">
        La novela ya está al día. No hay capítulos nuevos para descargar.
      </n-alert>

      <template v-else>
        <div class="stack-sm">
          <n-alert type="info" :closable="false">
            Hay <strong>{{ preview.newChapters }}</strong> capítulos nuevos disponibles (del {{ preview.firstNewChapter }} al {{ preview.lastNewChapter }}).
          </n-alert>

          <div class="radio-option" :class="{ active: mode === 'all' }" @click="mode = 'all'">
            <n-radio :checked="mode === 'all'" />
            <label>Descargar los {{ preview.newChapters }} capítulos nuevos</label>
          </div>
          <div class="radio-option" :class="{ active: mode === 'range' }" @click="mode = 'range'">
            <n-radio :checked="mode === 'range'" />
            <label>Descargar un rango específico</label>
          </div>
        </div>

        <div v-if="mode === 'range'" class="range-fields">
          <FieldNumber
            v-model="startChapter"
            label="Capítulo inicial"
            :min="preview.firstNewChapter"
            :max="preview.lastNewChapter"
            :disabled="loading"
            wrapper-style="min-width: 0"
          />
          <FieldNumber
            v-model="endChapter"
            label="Capítulo final"
            :min="startChapter"
            :max="preview.lastNewChapter"
            :disabled="loading"
            wrapper-style="min-width: 0"
          />
        </div>
      </template>
    </template>

    <n-alert v-if="error" type="error" style="margin-top: 0.75rem">{{ error }}</n-alert>
    <n-alert v-if="success" type="success" style="margin-top: 0.75rem">{{ success }}</n-alert>

    <template #action>
      <div class="action-bar">
        <n-button secondary :disabled="loading" @click="visible = false">Cancelar</n-button>
        <n-button
          type="primary"
          :loading="updating"
          :disabled="!canUpdate"
          @click="handleUpdate"
        >
          <template #icon><n-icon><RefreshOutline /></n-icon></template>
          {{ updating ? 'Descargando...' : 'Actualizar' }}
        </n-button>
      </div>
    </template>
  </n-modal>
</template>

<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useMessage, NModal, NAlert, NButton, NRadio, NSpin, NIcon } from "naive-ui";
import { RefreshOutline } from "@vicons/ionicons5";
import FieldNumber from "@/components/FieldNumber.vue";
import { useAppServices } from "@/app/services";
import { emitJobChanged } from "@/utils/job-events";
import type { UpdateUrlPreviewResult } from "@/api/types";

const props = defineProps<{ open: boolean; novelId: string }>();
const emit = defineEmits<{ "update:open": [value: boolean]; updated: [pending?: number] }>();

const { api } = useAppServices();
const message = useMessage();

const visible = computed({
  get: () => props.open,
  set: (value) => emit("update:open", value),
});

const mode = ref<"all" | "range">("all");
const startChapter = ref(1);
const endChapter = ref(1);
const loading = ref(false);
const updating = ref(false);
const error = ref<string | null>(null);
const success = ref<string | null>(null);
const preview = ref<UpdateUrlPreviewResult | null>(null);

const canUpdate = computed(() => {
  if (loading.value || updating.value) return false;
  if (!preview.value) return false;
  if (preview.value.newChapters === 0) return false;
  if (mode.value === "range") {
    return Boolean(startChapter.value && endChapter.value && startChapter.value <= endChapter.value);
  }
  return true;
});

function reset() {
  mode.value = "all";
  startChapter.value = 1;
  endChapter.value = 1;
  loading.value = false;
  updating.value = false;
  error.value = null;
  success.value = null;
  preview.value = null;
}

watch(visible, (open) => {
  if (open) {
    reset();
    void fetchPreview();
  }
});

async function fetchPreview() {
  loading.value = true;
  error.value = null;
  try {
    const result = await api.novels.updatePreviewFromUrl(props.novelId);
    preview.value = result;
    if (result.newChapters > 0) {
      startChapter.value = result.firstNewChapter;
      endChapter.value = result.lastNewChapter;
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err);
    preview.value = null;
  } finally {
    loading.value = false;
  }
}

async function handleUpdate() {
  if (!preview.value || preview.value.newChapters === 0) return;
  updating.value = true;
  error.value = null;
  success.value = null;
  try {
    const input: { startChapter?: number; endChapter?: number } = {};
    if (mode.value === "range") {
      input.startChapter = startChapter.value;
      input.endChapter = endChapter.value;
    }
    const result = await api.novels.updateFromUrl(props.novelId, input);
    const pending = (result as any).pendingChapters ?? result.chaptersAdded;
    if (pending > 0) {
      message.success(
        `${pending} capítulos nuevos se están descargando en segundo plano.`,
        { duration: 4000 },
      );
    } else {
      success.value = `${result.chaptersAdded} capítulos nuevos descargados.`;
    }
    emit("updated", pending);
    emitJobChanged();
    visible.value = false;
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err);
  } finally {
    updating.value = false;
  }
}
</script>

<style scoped>
.preview-loading {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 0.75rem;
  padding: 1.5rem 0;
}
.radio-option {
  display: flex;
  align-items: center;
  gap: 0.625rem;
  padding: 0.625rem 0.75rem;
  border: 1px solid var(--divide);
  border-radius: 8px;
  cursor: pointer;
  transition: border-color 0.15s;
}
.radio-option.active {
  border-color: var(--accent-link);
}
.radio-option label {
  cursor: pointer;
  font-size: 0.875rem;
}
.range-fields {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0.75rem;
  margin-top: 0.375rem;
}
.action-bar {
  display: flex;
  justify-content: flex-end;
  gap: 0.5rem;
}
</style>
