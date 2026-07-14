<template>
  <n-modal v-model:show="visible" preset="card" title="Confirmar importación" :style="{ width: 'min(820px, 96vw)' }" @after-leave="reset">
    <div class="import-columns">
      <div class="col-metadata">
        <div v-if="preview" class="preview-card">
          <div class="preview-cover">
            <img v-if="preview.coverURL" :src="preview.coverURL" :alt="preview.title" referrerpolicy="no-referrer" />
            <div v-else class="cover-placeholder">
              <n-icon :size="24"><ImageOutline /></n-icon>
            </div>
          </div>
          <div class="preview-info">
            <h3 style="margin: 0">{{ preview.title || "Sin título" }}</h3>
            <div v-if="preview.author" class="muted small">
              <n-icon :size="14" style="margin-right: 0.25rem"><PersonOutline /></n-icon>
              {{ preview.author }}
            </div>
            <div class="muted small">
              <n-icon :size="14" style="margin-right: 0.25rem"><ListOutline /></n-icon>
              <strong>{{ preview.totalChapters }}</strong> capítulos disponibles
            </div>
            <div v-if="preview.sourceURL" class="muted small" style="word-break: break-all">
              <n-icon :size="14" style="margin-right: 0.25rem"><LinkOutline /></n-icon>
              {{ preview.sourceURL }}
            </div>
          </div>
        </div>

        <div v-if="preview?.description" style="margin-top: 0.75rem">
          <label class="small muted">Descripción</label>
          <div class="description-box small">{{ preview.description }}</div>
        </div>
      </div>

      <div class="col-config">
        <div class="config-section">
          <label class="small muted">Idioma origen</label>
          <n-select v-model:value="sourceLanguage" :options="languageOptions" :disabled="loading" />
        </div>

        <div class="config-section">
          <label class="small muted">Idioma destino</label>
          <n-select v-model:value="targetLanguage" :options="languageOptionsNoAuto" :disabled="loading" />
        </div>

        <div class="config-divider" />

        <label class="small muted">Capítulos a descargar</label>
        <div class="stack-sm" style="margin-top: 0.375rem">
          <div class="radio-option" :class="{ active: mode === 'all' }" @click="mode = 'all'">
            <n-radio :checked="mode === 'all'" />
            <label>Todos ({{ preview?.totalChapters ?? 0 }})</label>
          </div>
          <div class="radio-option" :class="{ active: mode === 'range' }" @click="mode = 'range'">
            <n-radio :checked="mode === 'range'" />
            <label>Rango específico</label>
          </div>
        </div>

        <div v-if="mode === 'range'" class="range-fields">
          <FieldNumber
            v-model="startChapter"
            label="Desde"
            :min="1"
            :max="preview?.totalChapters ?? 1"
            :disabled="loading"
            wrapper-style="min-width: 0"
          />
          <FieldNumber
            v-model="endChapter"
            label="Hasta"
            :min="startChapter"
            :max="preview?.totalChapters ?? 1"
            :disabled="loading"
            wrapper-style="min-width: 0"
          />
        </div>
      </div>
    </div>

    <n-alert v-if="error" type="error" style="margin-top: 0.75rem">{{ error }}</n-alert>

    <template #action>
      <div class="action-bar">
        <n-button secondary :disabled="loading" @click="handleBack">Atrás</n-button>
        <n-button
          type="primary"
          :loading="loading"
          :disabled="!targetLanguage || (mode === 'range' && (!startChapter || !endChapter || startChapter > endChapter))"
          @click="handleImport"
        >
          <template #icon><n-icon><DownloadOutline /></n-icon></template>
          {{ loading ? 'Importando...' : 'Importar' }}
        </n-button>
      </div>
    </template>
  </n-modal>
</template>

<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useRouter } from "vue-router";
import { useMessage, NModal, NSelect, NAlert, NButton, NRadio, NIcon } from "naive-ui";
import { ImageOutline, PersonOutline, ListOutline, LinkOutline, DownloadOutline } from "@vicons/ionicons5";
import FieldNumber from "@/components/FieldNumber.vue";
import { LANGUAGES } from "@/config/languages";
import { useNovels } from "@/composables/useNovels";
import { emitJobChanged } from "@/utils/job-events";
import type { PreviewUrlResult } from "@/api/types";

const props = defineProps<{
  open: boolean;
  preview: PreviewUrlResult | null;
}>();
const emit = defineEmits<{
  "update:open": [value: boolean];
  "imported": [];
  "back": [];
}>();

const router = useRouter();
const message = useMessage();
const { importNovelFromUrl } = useNovels();

const visible = computed({
  get: () => props.open,
  set: (value) => emit("update:open", value),
});

const languageOptions = LANGUAGES.map((l) => ({ label: l.name, value: l.code }));
const languageOptionsNoAuto = LANGUAGES.filter((l) => l.code !== "auto").map((l) => ({ label: l.name, value: l.code }));

const mode = ref<"all" | "range">("all");
const sourceLanguage = ref("en");
const targetLanguage = ref("es");
const startChapter = ref(1);
const endChapter = ref(1);
const loading = ref(false);
const error = ref<string | null>(null);

function reset() {
  mode.value = "all";
  sourceLanguage.value = "en";
  targetLanguage.value = "es";
  startChapter.value = 1;
  endChapter.value = 1;
  loading.value = false;
  error.value = null;
}

watch(visible, (open) => {
  if (open) {
    reset();
  }
});

watch(
  () => props.preview,
  (preview) => {
    if (preview) {
      endChapter.value = preview.totalChapters;
    }
  },
);

function handleBack() {
  visible.value = false;
  emit("back");
}

async function handleImport() {
  if (!props.preview || !targetLanguage.value) return;
  loading.value = true;
  error.value = null;
  try {
    const input: {
      url: string;
      sourceLanguage?: string;
      targetLanguage?: string;
      startChapter?: number;
      endChapter?: number;
    } = {
      url: props.preview.sourceURL,
      sourceLanguage: sourceLanguage.value,
      targetLanguage: targetLanguage.value,
    };
    if (mode.value === "range") {
      input.startChapter = startChapter.value;
      input.endChapter = endChapter.value;
    }
    const result = await importNovelFromUrl(input);
    message.success(
      result.downloadJob
        ? `Novela importada. Descargando ${result.downloadJob.totalChapters} capítulos restantes en segundo plano...`
        : `${result.chaptersImported} capítulos importados.`,
      { duration: 4000 },
    );
    emit("imported");
    emitJobChanged();
    visible.value = false;
    await router.push(`/novels/${result.novel.id}`);
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err);
  } finally {
    loading.value = false;
  }
}
</script>

<style scoped>
.import-columns {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 1.25rem;
}
.col-metadata {
  min-width: 0;
}
.col-config {
  display: flex;
  flex-direction: column;
  gap: 0.625rem;
  min-width: 0;
}
.config-section {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
}
.config-divider {
  height: 1px;
  background: var(--divide);
  margin: 0.25rem 0;
}
.preview-card {
  display: flex;
  gap: 0.875rem;
  padding: 0.875rem;
  background: var(--surface-muted);
  border: 1px solid var(--divide);
  border-radius: 8px;
}
.preview-cover {
  flex-shrink: 0;
  width: 80px;
}
.preview-cover img {
  width: 80px;
  height: 115px;
  object-fit: cover;
  border-radius: 6px;
  background: #f3f4f6;
}
.cover-placeholder {
  width: 80px;
  height: 115px;
  border-radius: 6px;
  background: linear-gradient(135deg, #f3f4f6, #e5e7eb);
  display: flex;
  align-items: center;
  justify-content: center;
  color: #9ca3af;
}
.preview-info {
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
  min-width: 0;
  flex: 1;
}
.description-box {
  max-height: 200px;
  overflow: auto;
  padding: 0.6rem 0.75rem;
  background: var(--surface-muted);
  border: 1px solid var(--divide);
  border-radius: 6px;
  color: var(--text-secondary);
  white-space: pre-wrap;
  line-height: 1.4;
  font-size: 0.8125rem;
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
@media (max-width: 640px) {
  .import-columns {
    grid-template-columns: 1fr;
  }
}
</style>
