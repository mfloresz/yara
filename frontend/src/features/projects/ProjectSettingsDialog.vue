<template>
  <n-modal v-model:show="visible" preset="card" :style="{ width: 'min(1100px, 96vw)', height: 'min(720px, 85vh)', '--n-padding-top': '5px', '--n-padding-bottom': '5px' }" :content-style="{ display: 'flex', flexDirection: 'column', flex: 1, minHeight: 0, padding: 0 }" :segmented="{ content: true, action: true }" @after-leave="emit('update:open', false)">
    <template #header>
      <n-tabs v-model:value="activeTab" type="bar" animated style="flex: 1">
        <n-tab v-for="tab in tabs" :key="tab.value" :name="tab.value">
          {{ tab.label }}
        </n-tab>
      </n-tabs>
    </template>

    <div :style="{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }">

      <n-scrollbar v-if="activeTab === 'novel'" :style="{ flex: 1, minHeight: 0 }">
        <div :style="{ padding: '0.75rem 1.25rem 1.25rem' }" class="stack-md">
          <div class="row-wrap">
            <div style="min-width: 220px; flex: 1">
              <label class="small muted">Idioma origen</label>
              <n-select v-model:value="novelDraft.sourceLanguage" :options="languageOptions" />
            </div>
            <div style="min-width: 220px; flex: 1">
              <label class="small muted">Idioma destino</label>
              <n-select v-model:value="novelDraft.targetLanguage" :options="languageOptionsNoAuto" />
            </div>
          </div>

          <div class="row-wrap">
            <n-card style="flex: 1; min-width: 280px" title="Metadatos idioma origen">
                <div class="stack-md">
                  <div v-if="coverEditable" class="cover-editor">
                    <label class="small muted">Portada</label>
                    <div class="cover-preview">
                      <img v-if="displayCoverUrl" :src="displayCoverUrl" alt="Portada actual" />
                      <div v-else class="cover-placeholder">
                        <n-icon :size="32"><ImageOutline /></n-icon>
                      </div>
                    </div>
                    <div class="row-wrap" style="margin-top: 0.5rem">
                      <n-button size="small" secondary @click="triggerFileInput">
                        {{ displayCoverUrl ? 'Cambiar portada' : 'Subir portada' }}
                      </n-button>
                      <n-button v-if="displayCoverUrl" size="small" type="error" secondary @click="removeCover">
                        Eliminar
                      </n-button>
                    </div>
                    <input ref="fileInputRef" type="file" accept="image/*" hidden @change="onFileSelected" />
                  </div>

                  <div class="row-wrap">
                    <div class="form-group" style="flex: 1; min-width: 180px">
                      <label class="small muted">Título</label>
                      <n-input :value="novelDraft.sourceTitle" @update:value="(v) => (novelDraft.sourceTitle = v)" />
                    </div>
                    <div class="form-group" style="flex: 1; min-width: 180px">
                      <label class="small muted">Autor</label>
                      <n-input :value="novelDraft.sourceAuthor" @update:value="(v) => (novelDraft.sourceAuthor = v)" />
                    </div>
                  </div>

                  <div class="row-wrap">
                    <div class="form-group" style="flex: 1; min-width: 180px">
                      <label class="small muted">Serie</label>
                      <n-auto-complete
                        :value="novelDraft.sourceSeries"
                        :options="sourceSeriesOptions"
                        placeholder="Nombre de la serie"
                        @update:value="(v) => (novelDraft.sourceSeries = v ?? '')"
                      />
                    </div>
                    <div class="form-group" style="flex: 1; min-width: 180px">
                      <label class="small muted">Número</label>
                      <n-input :value="novelDraft.sourceNumber" @update:value="(v) => (novelDraft.sourceNumber = v)" />
                    </div>
                  </div>

                  <div class="form-group">
                    <label class="small muted">Descripción</label>
                    <n-input
                      :value="novelDraft.sourceDescription"
                      type="textarea"
                      :rows="4"
                      @update:value="(v) => (novelDraft.sourceDescription = v)"
                    />
                  </div>

                  <div class="form-group" style="max-width: 220px">
                    <label class="small muted">Estatus</label>
                    <n-select
                      :value="novelDraft.status"
                      :options="statusOptions"
                      @update:value="(v) => (novelDraft.status = v ?? 'ongoing')"
                    />
                  </div>

                  <div class="form-group">
                    <label class="small muted">Etiquetas</label>
                    <n-dynamic-tags
                      :value="novelDraft.tags"
                      placeholder="Escribe una etiqueta y presiona Enter"
                      @update:value="(v: string[]) => (novelDraft.tags = v)"
                    />
                  </div>

                  <div class="row-wrap" style="justify-content: flex-end; padding-top: 0.5rem; border-top: 1px solid var(--divide)">
                    <n-popconfirm @positive-click="onDeleteNovel">
                      <template #trigger>
                        <n-button size="small" type="error" secondary>
                          <template #icon><n-icon><TrashOutline /></n-icon></template>
                          Eliminar novela
                        </n-button>
                      </template>
                      ¿Eliminar esta novela? Esta acción no se puede deshacer.
                    </n-popconfirm>
                  </div>
                </div>
            </n-card>

            <n-card style="flex: 1; min-width: 280px" title="Metadatos idioma destino">
                <div class="stack-md">
                  <div class="row-wrap">
                    <div class="form-group" style="flex: 1; min-width: 180px">
                      <label class="small muted">Título</label>
                      <n-input :value="novelDraft.targetTitle" @update:value="(v) => (novelDraft.targetTitle = v)" />
                    </div>
                    <div class="form-group" style="flex: 1; min-width: 180px">
                      <label class="small muted">Autor</label>
                      <n-input :value="novelDraft.targetAuthor" @update:value="(v) => (novelDraft.targetAuthor = v)" />
                    </div>
                  </div>

                  <div class="row-wrap">
                    <div class="form-group" style="flex: 1; min-width: 180px">
                      <label class="small muted">Serie</label>
                      <n-auto-complete
                        :value="novelDraft.targetSeries"
                        :options="targetSeriesOptions"
                        placeholder="Nombre de la serie"
                        @update:value="(v) => (novelDraft.targetSeries = v ?? '')"
                      />
                    </div>
                    <div class="form-group" style="flex: 1; min-width: 180px">
                      <label class="small muted">Número</label>
                      <n-input :value="novelDraft.targetNumber" @update:value="(v) => (novelDraft.targetNumber = v)" />
                    </div>
                  </div>

                  <div class="form-group">
                    <label class="small muted">Descripción</label>
                    <n-input
                      :value="novelDraft.targetDescription"
                      type="textarea"
                      :rows="4"
                      @update:value="(v) => (novelDraft.targetDescription = v)"
                    />
                  </div>
                </div>
            </n-card>
          </div>

          <div class="row-wrap">
            <div style="min-width: 280px; flex: 1">
              <label class="small muted">URL fuente</label>
              <n-input v-model:value="novelDraft.url" />
            </div>
            <div style="min-width: 280px; flex: 1">
              <label class="small muted">Custom commands</label>
              <n-input v-model:value="novelDraft.customCommands" type="textarea" :autosize="{ minRows: 5 }" class="mono" />
            </div>
          </div>
        </div>
      </n-scrollbar>

      <n-scrollbar v-else-if="activeTab === 'glossary'" :style="{ flex: 1, minHeight: 0 }">
        <div :style="{ padding: '0.75rem 1.25rem 1.25rem' }" class="stack-md">
          <n-card title="Generar glosario con IA" size="small">
            <div class="stack-md">
              <div class="row-wrap">
                <div style="min-width: 120px; flex: 1">
                  <label class="small muted">Capítulo desde</label>
                  <n-input-number v-model:value="glossaryGenOptions.chapterFrom" :min="1" size="small" />
                </div>
                <div style="min-width: 120px; flex: 1">
                  <label class="small muted">Capítulo hasta</label>
                  <n-input-number v-model:value="glossaryGenOptions.chapterTo" :min="1" size="small" />
                </div>
              </div>
              <div v-if="estimatedTokensLoading || estimatedTokens !== null" class="small muted" style="margin-top: -0.25rem">
                <n-spin v-if="estimatedTokensLoading" :size="12" style="margin-right: 0.35rem" />
                <template v-else-if="estimatedTokens !== null">
                  ~{{ formatTokenCount(estimatedTokens) }} tokens estimados
                </template>
              </div>
              <div class="row-wrap">
                <div style="flex: 1">
                  <label class="small muted">Modo de envío</label>
                  <n-radio-group v-model:value="glossaryGenOptions.mode" size="small">
                    <n-radio value="together">Todo junto</n-radio>
                    <n-radio value="batch">Por lotes</n-radio>
                  </n-radio-group>
                </div>
                <div v-if="glossaryGenOptions.mode === 'batch'" style="min-width: 140px">
                  <label class="small muted">Max tokens/lote</label>
                  <n-input-number v-model:value="glossaryGenOptions.maxTokensPerBatch" :min="10000" :step="10000" size="small" />
                </div>
              </div>
              <div class="row-wrap">
                <div style="flex: 1; min-width: 180px">
                  <label class="small muted">Proveedor</label>
                  <n-select
                    v-model:value="glossaryGenOptions.provider"
                    :options="providerOptions"
                    :loading="providersLoading"
                    placeholder="Proveedor activo"
                    clearable
                    size="small"
                  />
                </div>
                <div style="flex: 1; min-width: 180px">
                  <label class="small muted">Modelo</label>
                  <n-select
                    v-if="glossaryModelOptions.length > 1"
                    v-model:value="glossaryGenOptions.model"
                    :options="glossaryModelOptions"
                    placeholder="Modelo del proveedor"
                    clearable
                    size="small"
                  />
                  <n-input
                    v-else
                    v-model:value="glossaryGenOptions.model"
                    placeholder="Modelo del proveedor"
                    size="small"
                  />
                </div>
              </div>
              <div style="display: flex; justify-content: flex-end">
                <n-button
                  type="primary"
                  size="small"
                  :loading="glossaryGenerating"
                  :disabled="glossaryGenerating"
                  @click="generateGlossary"
                >
                  Generar glosario
                </n-button>
              </div>
            </div>
          </n-card>

          <div class="row-between">
            <h4 style="margin: 0">Glosario</h4>
            <n-button size="small" @click="addGlossaryEntry">
              <template #icon><n-icon><AddOutline /></n-icon></template>
              Añadir entrada
            </n-button>
          </div>
          <div v-if="novelDraft.glossary.length === 0" class="muted small">Sin entradas de glosario.</div>
          <div v-else class="glossary-list">
            <div class="glossary-list-header">
              <span class="glossary-col-source">Origen</span>
              <span class="glossary-col-target">Destino</span>
              <span class="glossary-col-context">Contexto</span>
              <span class="glossary-col-actions" />
            </div>
            <div v-for="entry in novelDraft.glossary" :key="entry.id" class="glossary-list-row">
              <n-input v-model:value="entry.source" placeholder="Origen" size="small" class="glossary-col-source" />
              <n-input v-model:value="entry.target" placeholder="Destino" size="small" class="glossary-col-target" />
              <n-input v-model:value="entry.context" placeholder="Contexto" size="small" class="glossary-col-context" />
              <n-button type="error" quaternary circle size="tiny" class="glossary-col-actions glossary-delete-btn" @click="removeGlossaryEntry(entry.id)">
                <template #icon><n-icon :size="14"><TrashOutline /></n-icon></template>
              </n-button>
            </div>
          </div>
        </div>
      </n-scrollbar>

      <n-scrollbar v-else-if="activeTab === 'prompts'" :style="{ flex: 1, minHeight: 0 }">
        <div :style="{ padding: '0.75rem 1.25rem 1.25rem' }" class="stack-md">
          <n-alert type="info">
            Edita un system prompt para crear un override en la novela. Déjalo vacío (o restáuralo al global) para usar el prompt configurado en Configuración general. El user prompt se genera automáticamente con los datos del capítulo y no se expone aquí.
          </n-alert>
          <PromptRoleEditor
            title="Traducción"
            :model-value="effectiveSystemPrompt('translation')"
            :global-value="globalSystemPrompt('translation')"
            :overridden="isOverridden('translation')"
            @update:model-value="setSystemPrompt('translation', $event)"
          />
          <PromptRoleEditor
            title="Refinamiento"
            :model-value="effectiveSystemPrompt('refine')"
            :global-value="globalSystemPrompt('refine')"
            :overridden="isOverridden('refine')"
            @update:model-value="setSystemPrompt('refine', $event)"
          />
          <PromptRoleEditor
            title="Verificación"
            :model-value="effectiveSystemPrompt('check')"
            :global-value="globalSystemPrompt('check')"
            :overridden="isOverridden('check')"
            @update:model-value="setSystemPrompt('check', $event)"
          />
        </div>
      </n-scrollbar>

      <n-scrollbar v-else-if="activeTab === 'ai'" :style="{ flex: 1, minHeight: 0 }">
        <div :style="{ padding: '0.75rem 1.25rem 1.25rem' }" class="stack-md">
          <div class="row-wrap">
            <div style="min-width: 220px; flex: 1">
              <label class="small muted">Proveedor</label>
              <n-select
                v-model:value="settingsDraft.ai.provider"
                :options="providerOptions"
                :loading="providersLoading"
                :disabled="providersLoading"
                placeholder="Usar proveedor global"
                clearable
                @update:value="onProviderChange"
              />
            </div>
            <div style="min-width: 220px; flex: 1">
              <label class="small muted">Modelo</label>
              <n-select
                v-if="modelOptions.length > 1"
                v-model:value="settingsDraft.ai.model"
                :options="modelOptions"
                :disabled="!settingsDraft.ai.provider || providersLoading"
                placeholder="Usar modelo global"
                clearable
              />
              <n-input
                v-else
                v-model:value="settingsDraft.ai.model"
                :disabled="!settingsDraft.ai.provider || providersLoading"
                placeholder="Ej: local-model"
              />
            </div>
            <FieldNumber v-model="timeoutSec" label="Timeout (segundos)" :min="10" allow-clear placeholder="Usar global" wrapper-style="min-width: 180px; flex: 1" />
          </div>
          <n-alert type="info">Si dejas estos campos vacíos, el backend usará la configuración global. El timeout aplica solo si se configura un valor.</n-alert>

          <div style="border-top: 1px solid var(--divide); padding-top: 1rem; margin-top: 0.5rem">
            <div style="display: flex; justify-content: space-between; align-items: center; gap: 1rem">
              <div>
                <div style="font-weight: 600">Usar modelo diferente para traducir títulos</div>
                <div class="small muted">Usa un modelo más pequeño para títulos de capítulos. Si falla, se usa el modelo de contenido.</div>
              </div>
              <n-switch v-model:value="settingsDraft.ai.titleEnabled" style="flex-shrink: 0" />
            </div>
            <div v-if="settingsDraft.ai.titleEnabled" class="row-wrap" style="margin-top: 0.75rem">
              <div style="min-width: 220px; flex: 1">
                <label class="small muted">Proveedor para títulos</label>
                <n-select
                  v-model:value="settingsDraft.ai.titleProvider"
                  :options="providerOptions"
                  :loading="providersLoading"
                  :disabled="providersLoading"
                  placeholder="Usar proveedor de contenido"
                  clearable
                  @update:value="onTitleProviderChange"
                />
              </div>
              <div style="min-width: 220px; flex: 1">
                <label class="small muted">Modelo para títulos</label>
                <n-select
                  v-if="titleModelOptions.length > 1"
                  v-model:value="settingsDraft.ai.titleModel"
                  :options="titleModelOptions"
                  :disabled="!settingsDraft.ai.titleProvider || providersLoading"
                  placeholder="Usar modelo de contenido"
                  clearable
                />
                <n-input
                  v-else
                  v-model:value="settingsDraft.ai.titleModel"
                  :disabled="!settingsDraft.ai.titleProvider || providersLoading"
                  placeholder="Ej: local-model"
                />
              </div>
            </div>
          </div>
        </div>
      </n-scrollbar>

      <n-scrollbar v-else-if="activeTab === 'translation'" :style="{ flex: 1, minHeight: 0 }">
        <div :style="{ padding: '0.75rem 1.25rem 1.25rem' }" class="stack-md">
          <div style="display: flex; align-items: center; justify-content: space-between; gap: 1rem">
            <div>
              <div style="font-weight: 600">Auto segmentación</div>
              <div class="small muted">Divide capítulos largos antes de enviarlos al proveedor AI.</div>
            </div>
            <n-switch v-model:value="settingsDraft.translation.autoSegment" />
          </div>

          <div class="row-wrap">
            <FieldNumber v-model="settingsDraft.translation.thresholdChars" label="Umbral auto" :min="1000" wrapper-style="min-width: 180px; flex: 1" />
            <FieldNumber v-model="settingsDraft.translation.maxChars" label="Máx. por segmento" :min="500" wrapper-style="min-width: 180px; flex: 1" />
            <FieldNumber v-model="settingsDraft.translation.minChars" label="Mín. por segmento" :min="100" wrapper-style="min-width: 180px; flex: 1" />
            <FieldNumber v-model="settingsDraft.translation.maxRetries" label="Reintentos" :min="0" wrapper-style="min-width: 180px; flex: 1" />
          </div>

          <div style="display: flex; align-items: center; justify-content: space-between; gap: 1rem">
            <div>
              <div style="font-weight: 600">Enable check</div>
              <div class="small muted">Permite verificación adicional en el backend.</div>
            </div>
            <n-switch v-model:value="settingsDraft.translation.enableCheck" />
          </div>

          <div style="display: flex; align-items: center; justify-content: space-between; gap: 1rem">
            <div>
              <div style="font-weight: 600">Incluir títulos anteriores</div>
              <div class="small-muted">Añade contexto de títulos previos al traducir.</div>
            </div>
            <n-switch v-model:value="settingsDraft.translation.includePreviousChapterTitles" />
          </div>
        </div>
      </n-scrollbar>

      <n-scrollbar v-else :style="{ flex: 1, minHeight: 0 }">
        <div :style="{ padding: '0.75rem 1.25rem 1.25rem' }" class="stack-md">
          <label class="small muted">Notas del proyecto</label>
          <n-input v-model:value="settingsDraft.notes" type="textarea" :autosize="{ minRows: 10 }" />
        </div>
      </n-scrollbar>

    </div>

    <template #action>
      <div :style="{ display: 'flex', gap: '0.5rem', justifyContent: 'flex-end', width: '100%' }">
        <n-button secondary @click="emit('update:open', false)">Cerrar</n-button>
        <n-button type="warning" secondary @click="reset">Reset</n-button>
        <n-button type="primary" :loading="saving" @click="save">Guardar</n-button>
      </div>
    </template>
  </n-modal>
</template>

<script setup lang="ts">
import { computed, ref, watch, onBeforeUnmount } from "vue";
import { useRouter } from "vue-router";
import { useMessage, NModal, NButton, NCard, NInput, NAlert, NSwitch, NSelect, NIcon, NScrollbar, NAutoComplete, NDynamicTags, NPopconfirm, NInputNumber, NRadioGroup, NRadio, NSpin, NTabs, NTab } from "naive-ui";
import { AddOutline, TrashOutline, ImageOutline } from "@vicons/ionicons5";
import PromptRoleEditor from "@/components/PromptRoleEditor.vue";
import FieldNumber from "@/components/FieldNumber.vue";
import { useProjectSettings } from "@/composables/useProjectSettings";
import { useProviders } from "@/composables/useProviders";
import { useNovels } from "@/composables/useNovels";
import { type Novel, type NovelStatus, type UpdateNovelInput } from "@/domain";
import { normalizeTranslationOptions, type GlossaryGenerationOptions } from "@/domain/project-settings";
import { useAppServices } from "@/app/services";
import { emitJobChanged } from "@/utils/job-events";
import { safeUuid } from "@/utils/safe-uuid";
import { LANGUAGES } from "@/config/languages";

const props = defineProps<{
  open: boolean;
  novel: Novel;
  onSaveNovel: (patch: UpdateNovelInput) => Promise<void>;
}>();

const emit = defineEmits<{
  (e: "update:open", value: boolean): void;
  (e: "cover-updated", novel: Novel): void;
}>();

const { api, defaults } = useAppServices();
const router = useRouter();
const message = useMessage();
const { deleteNovel } = useNovels();
const novelRef = computed(() => props.novel);
const { settings, globalPrompts, loading: settingsLoading } = useProjectSettings(novelRef);
const { providers, byId, loading: providersLoading } = useProviders();
const activeTab = ref("novel");
const saving = ref(false);
const uploadingCover = ref(false);
const tagSuggestions = ref<string[]>([]);
const seriesSuggestions = ref<string[]>([]);
const novelDraft = ref<Novel>(JSON.parse(JSON.stringify(props.novel)) as Novel);
const settingsDraft = ref(JSON.parse(JSON.stringify(settings.value)));
const timeoutSec = ref<number | null>(null);
let pendingCoverFile: File | undefined;
const fileInputRef = ref<HTMLInputElement | null>(null);
const localCoverUrl = ref<string | undefined>();

const glossaryGenOptions = ref<GlossaryGenerationOptions>({
  chapterFrom: 1,
  chapterTo: 0,
  mode: "together",
  maxTokensPerBatch: 90000,
  provider: "",
  model: "",
});
const glossaryGenerating = ref(false);
const estimatedTokens = ref<number | null>(null);
const estimatedTokensLoading = ref(false);
let estimateTimer: ReturnType<typeof setTimeout> | null = null;

const glossaryModelOptions = computed(() => {
  if (!glossaryGenOptions.value.provider) return [];
  const p = byId.value.get(glossaryGenOptions.value.provider);
  if (!p || !p.models || p.models.length === 0) return [];
  return p.models.map((m: string) => ({ label: m, value: m }));
});

function formatTokenCount(n: number): string {
  return n.toLocaleString("es-ES");
}

function fetchEstimatedTokens() {
  if (estimateTimer) clearTimeout(estimateTimer);
  const from = glossaryGenOptions.value.chapterFrom;
  if (!from || from <= 0) {
    estimatedTokens.value = null;
    return;
  }
  estimateTimer = setTimeout(async () => {
    estimatedTokensLoading.value = true;
    try {
      const result = await api.novels.estimateGlossaryTokens(
        props.novel.id,
        from,
        glossaryGenOptions.value.chapterTo || 0,
      );
      estimatedTokens.value = result.totalTokens;
    } catch {
      estimatedTokens.value = null;
    } finally {
      estimatedTokensLoading.value = false;
    }
  }, 2000);
}

watch(
  [() => glossaryGenOptions.value.chapterFrom, () => glossaryGenOptions.value.chapterTo],
  () => { fetchEstimatedTokens(); },
);

onBeforeUnmount(() => {
  if (estimateTimer) clearTimeout(estimateTimer);
});

const displayCoverUrl = computed(() => localCoverUrl.value || props.novel.coverPath);

const visible = computed({
  get: () => props.open,
  set: (value) => emit("update:open", value),
});

const languageOptions = LANGUAGES.map((l) => ({ label: l.name, value: l.code }));
const languageOptionsNoAuto = LANGUAGES.filter((l) => l.code !== "auto").map((l) => ({ label: l.name, value: l.code }));

const coverEditable = computed(() => true);

const statusOptions: Array<{ label: string; value: NovelStatus }> = [
  { label: "En curso", value: "ongoing" },
  { label: "Completada", value: "completed" },
  { label: "Hiatus", value: "hiatus" },
  { label: "Cancelada", value: "cancelled" },
];

function onSelectCover(file: File) {
  pendingCoverFile = file;
  localCoverUrl.value = URL.createObjectURL(file);
}

async function onRemoveCover() {
  pendingCoverFile = undefined;
  if (localCoverUrl.value) {
    URL.revokeObjectURL(localCoverUrl.value);
    localCoverUrl.value = undefined;
  }
}

function triggerFileInput() {
  fileInputRef.value?.click();
}

function onFileSelected(event: Event) {
  const file = (event.target as HTMLInputElement).files?.[0];
  if (!file) return;
  localCoverUrl.value = URL.createObjectURL(file);
  onSelectCover(file);
}

function removeCover() {
  if (localCoverUrl.value) {
    URL.revokeObjectURL(localCoverUrl.value);
    localCoverUrl.value = undefined;
  }
  onRemoveCover();
}

function buildSeriesOptions(query: string, available: string[]) {
  let filtered: string[];
  if (!query) {
    filtered = available.slice(0, 8);
  } else {
    const startsWith = available.filter((s) => s.toLowerCase().startsWith(query));
    const contains = available.filter(
      (s) => !s.toLowerCase().startsWith(query) && s.toLowerCase().includes(query),
    );
    filtered = [...startsWith, ...contains].slice(0, 8);
  }
  return filtered.map((s) => ({ label: s, value: s }));
}

const sourceSeriesOptions = computed(() => {
  const query = novelDraft.value.sourceSeries?.trim().toLowerCase() ?? "";
  return buildSeriesOptions(query, seriesSuggestions.value);
});

const targetSeriesOptions = computed(() => {
  const query = novelDraft.value.targetSeries?.trim().toLowerCase() ?? "";
  return buildSeriesOptions(query, seriesSuggestions.value);
});

watch(
  () => props.novel.coverPath,
  () => {
    localCoverUrl.value = undefined;
  },
);

async function onDeleteNovel() {
  try {
    await deleteNovel(props.novel.id);
    emit("update:open", false);
    message.success("Novela eliminada", { duration: 2500 });
    await router.push("/");
  } catch (err) {
    message.error(
      `No se pudo eliminar la novela: ${err instanceof Error ? err.message : String(err)}`,
      { duration: 4000 },
    );
  }
}

const providerOptions = computed(() => providers.value.map((p) => ({ label: p.name, value: p.id })));

const modelOptions = computed(() => {
  const info = byId.value.get(settingsDraft.value.ai.provider);
  if (info) return info.models.map((m) => ({ label: m, value: m }));
  return [];
});

const titleModelOptions = computed(() => {
  const info = byId.value.get(settingsDraft.value.ai.titleProvider ?? "");
  if (info) return info.models.map((m) => ({ label: m, value: m }));
  return [];
});

function onProviderChange(value: string) {
  const info = byId.value.get(value);
  if (!info) return;
  if (!info.models.includes(settingsDraft.value.ai.model)) {
    settingsDraft.value.ai.model = info.defaultModel;
  }
}

function onTitleProviderChange(value: string) {
  const info = byId.value.get(value);
  if (!info) return;
  if (!info.models.includes(settingsDraft.value.ai.titleModel)) {
    settingsDraft.value.ai.titleModel = info.defaultModel;
  }
}

type PromptRole = "translation" | "refine" | "check";

const globalSystemPrompts = computed<Record<PromptRole, string | undefined>>(() => {
  const map: Record<PromptRole, string | undefined> = { translation: undefined, refine: undefined, check: undefined };
  for (const p of globalPrompts.value) {
    if (!p.active) continue;
    if (p.key === "translation" || p.key === "refine" || p.key === "check") {
      map[p.key] = p.prompt.systemPrompt;
    }
  }
  return map;
});

function globalSystemPrompt(role: PromptRole): string {
  return globalSystemPrompts.value[role] ?? "";
}

function effectiveSystemPrompt(role: PromptRole): string {
  return novelDraft.value.prompts[role]?.systemPrompt ?? globalSystemPrompts.value[role] ?? "";
}

function isOverridden(role: PromptRole): boolean {
  return novelDraft.value.prompts[role]?.systemPrompt != null;
}

function setSystemPrompt(role: PromptRole, value: string): void {
  const trimmed = value ?? "";
  const globalValue = globalSystemPrompts.value[role];
  const isCustom = trimmed.length > 0 && trimmed !== globalValue;
  const nextPrompts = { ...novelDraft.value.prompts };
  if (isCustom) {
    nextPrompts[role] = { ...(nextPrompts[role] ?? {}), systemPrompt: trimmed };
  } else {
    delete nextPrompts[role];
  }
  novelDraft.value = { ...novelDraft.value, prompts: nextPrompts };
}

const tabs = [
  { value: "novel", label: "Novela" },
  { value: "glossary", label: "Glosario" },
  { value: "prompts", label: "Prompts" },
  { value: "ai", label: "IA" },
  { value: "translation", label: "Traducción" },
  { value: "notes", label: "Notas" },
];

const draftInitialized = ref(false);

/** Defense-in-depth: ensure glossary entries always have an id. */
function ensureGlossaryIds(
  glossary: unknown,
): Array<{ id: string; source: string; target: string; context?: string }> {
  if (!Array.isArray(glossary)) return [];
  return glossary.map((entry) => {
    const e = entry as Record<string, unknown>;
    return {
      id: (typeof e.id === "string" && e.id) || safeUuid(),
      source: typeof e.source === "string" ? e.source : "",
      target: typeof e.target === "string" ? e.target : "",
      context: typeof e.context === "string" ? e.context : undefined,
    };
  });
}

async function resetDraft() {
  const base = JSON.parse(JSON.stringify(props.novel)) as Novel;
  base.glossary = ensureGlossaryIds(base.glossary);
  novelDraft.value = {
    ...base,
  };
  settingsDraft.value = JSON.parse(JSON.stringify(settings.value));
  activeTab.value = "novel";
  const ms = settingsDraft.value.ai.timeoutMs;
  timeoutSec.value = ms ? Math.round(ms / 1000) : null;
  draftInitialized.value = true;
  try {
    tagSuggestions.value = await api.novels.listTagSuggestions();
  } catch {
    tagSuggestions.value = [];
  }
  try {
    seriesSuggestions.value = await api.novels.listSeriesSuggestions();
  } catch {
    seriesSuggestions.value = [];
  }
}

watch(
  () => [props.open, settingsLoading.value, settings.value] as const,
  async () => {
    if (!props.open) {
      draftInitialized.value = false;
      return;
    }
    if (draftInitialized.value) return;
    if (settingsLoading.value) return;
    await resetDraft();
    draftInitialized.value = true;
  },
  { immediate: true },
);

// When the parent novel glossary changes (e.g. after a generate-glossary job
// completes), sync it into the draft so the user sees the updated glossary
// without having to close and reopen the dialog.
watch(
  () => props.novel.glossary,
  (newGlossary) => {
    if (!props.open || !draftInitialized.value) return;
    // Only update if the reference actually changed (parent re-fetched novel).
    if (novelDraft.value.glossary === newGlossary) return;
    novelDraft.value.glossary = ensureGlossaryIds(JSON.parse(JSON.stringify(newGlossary)));
  },
);

function addGlossaryEntry() {
  novelDraft.value.glossary = [
    ...novelDraft.value.glossary,
    { id: safeUuid(), source: "", target: "", context: "" },
  ];
}

function removeGlossaryEntry(id: string) {
  novelDraft.value.glossary = novelDraft.value.glossary.filter((entry) => entry.id !== id);
}

async function generateGlossary() {
  if (glossaryGenerating.value) return;
  glossaryGenerating.value = true;
  try {
    const opts: GlossaryGenerationOptions = {
      chapterFrom: glossaryGenOptions.value.chapterFrom || 1,
      chapterTo: glossaryGenOptions.value.chapterTo || 0,
      mode: glossaryGenOptions.value.mode,
      maxTokensPerBatch: glossaryGenOptions.value.maxTokensPerBatch || 90000,
      provider: glossaryGenOptions.value.provider || "",
      model: glossaryGenOptions.value.model || "",
    };
    await api.novels.generateGlossary(props.novel.id, opts);
    emitJobChanged();
    message.success("Glosario en generación. Se actualizará al completar.");
  } catch (err: unknown) {
    const msg = err instanceof Error ? err.message : "Error desconocido";
    message.error(`Error al generar glosario: ${msg}`);
  } finally {
    glossaryGenerating.value = false;
  }
}

function reset() {
  settingsDraft.value = {
    notes: "",
    glossary: [],
    prompts: {},
    ai: { provider: "", model: "", timeoutMs: undefined, titleEnabled: false, titleProvider: "", titleModel: "" },
    translation: normalizeTranslationOptions(defaults.value?.translation),
    cleanupRules: [],
  };

  novelDraft.value = {
    ...props.novel,
    glossary: [],
    prompts: {},
    notes: "",
    aiOptions: { provider: "", model: "", timeoutMs: undefined, titleEnabled: false, titleProvider: "", titleModel: "" },
    translationOptions: normalizeTranslationOptions(defaults.value?.translation),
    cleanupRules: [],
    url: "",
    customCommands: "",
  };
}

async function save() {
  saving.value = true;
  try {
    settingsDraft.value.ai.timeoutMs = timeoutSec.value != null ? timeoutSec.value * 1000 : undefined;
    if (!settingsDraft.value.ai.titleEnabled) {
      settingsDraft.value.ai.titleProvider = "";
      settingsDraft.value.ai.titleModel = "";
    }
    await props.onSaveNovel({
      sourceLanguage: novelDraft.value.sourceLanguage,
      targetLanguage: novelDraft.value.targetLanguage,
      sourceTitle: novelDraft.value.sourceTitle,
      sourceAuthor: novelDraft.value.sourceAuthor,
      sourceDescription: novelDraft.value.sourceDescription,
      sourceSeries: novelDraft.value.sourceSeries,
      sourceNumber: novelDraft.value.sourceNumber,
      targetTitle: novelDraft.value.targetTitle,
      targetAuthor: novelDraft.value.targetAuthor,
      targetDescription: novelDraft.value.targetDescription,
      targetSeries: novelDraft.value.targetSeries,
      targetNumber: novelDraft.value.targetNumber,
      glossary: novelDraft.value.glossary,
      prompts: novelDraft.value.prompts,
      notes: settingsDraft.value.notes,
      aiOptions: settingsDraft.value.ai,
      translationOptions: settingsDraft.value.translation,
      cleanupRules: novelDraft.value.cleanupRules,
      url: novelDraft.value.url,
      customCommands: novelDraft.value.customCommands,
      status: novelDraft.value.status,
      tags: novelDraft.value.tags,
    });
    if (pendingCoverFile) {
      uploadingCover.value = true;
      const updated = await api.novels.uploadCover(props.novel.id, pendingCoverFile);
      pendingCoverFile = undefined;
      emit("cover-updated", updated);
    }
    emit("update:open", false);
  } finally {
    saving.value = false;
    uploadingCover.value = false;
  }
}
</script>

<style scoped>
.cover-editor {
  margin-bottom: 0.5rem;
}

.cover-preview {
  width: 100%;
  max-width: 280px;
  aspect-ratio: 2 / 3;
  border-radius: 4px;
  overflow: hidden;
  border: 1px solid var(--border);
  background: var(--n-color-embedded);
  display: flex;
  align-items: center;
  justify-content: center;
}

.cover-preview img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.cover-placeholder {
  color: var(--text-color-3);
}

/* Glossary compact list */
.glossary-list {
  border: 1px solid var(--divide);
  border-radius: var(--radius-md);
  background: var(--surface-base);
  overflow: hidden;
}

.glossary-list-header {
  display: grid;
  grid-template-columns: 1fr 1fr 1.5fr auto;
  gap: 0.375rem;
  padding: 0.375rem 0.625rem;
  background: var(--surface-muted);
  border-bottom: 1px solid var(--divide);
  font-size: 0.75rem;
  font-weight: 500;
  color: var(--text-secondary);
}

.glossary-list-row {
  display: grid;
  grid-template-columns: 1fr 1fr 1.5fr auto;
  gap: 0.375rem;
  padding: 0.25rem 0.625rem;
  align-items: center;
  border-bottom: 1px solid var(--divide);
  transition: background 0.12s ease;
}

.glossary-list-row:last-child {
  border-bottom: none;
}

.glossary-list-row:hover {
  background: var(--mock-row);
}

.glossary-delete-btn {
  color: #dc2626 !important;
}

.glossary-delete-btn:hover {
  background: color-mix(in oklab, #dc2626 10%, transparent) !important;
}

@media (max-width: 640px) {
  .glossary-list-header {
    display: none;
  }

  .glossary-list-row {
    grid-template-columns: 1fr auto;
    gap: 0.25rem;
    padding: 0.375rem 0.625rem;
  }

  .glossary-col-context {
    grid-column: 1 / -1;
  }
}
</style>
