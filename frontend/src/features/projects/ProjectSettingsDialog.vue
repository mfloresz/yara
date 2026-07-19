<template>
  <n-modal
    v-model:show="visible"
    preset="card"
    :style="modalStyle"
    :content-style="contentStyle"
    :segmented="{ content: true, action: true }"
    @after-leave="emit('update:open', false)"
  >
    <template #header>
      <div class="modal-header">
        <div class="modal-title-block">
          <span class="modal-title">{{ novelDraft.sourceTitle || 'Configurar novela' }}</span>
          <span v-if="isDirty" class="dirty-dot" title="Cambios sin guardar" />
        </div>
        <n-tabs
          v-model:value="activeTab"
          type="bar"
          animated
          size="small"
          class="header-tabs mobile-only"
        >
          <n-tab v-for="tab in tabs" :key="tab.value" :name="tab.value">
            {{ tab.label }}
          </n-tab>
        </n-tabs>
      </div>
    </template>

    <div class="modal-body">
      <nav class="side-nav desktop-only" aria-label="Secciones">
        <button
          v-for="tab in tabs"
          :key="tab.value"
          type="button"
          class="side-nav-item"
          :class="{ active: activeTab === tab.value }"
          @click="activeTab = tab.value"
        >
          <n-icon :size="16"><component :is="tab.icon" /></n-icon>
          <span>{{ tab.label }}</span>
        </button>
      </nav>

      <div class="main-pane">
        <!-- ========== NOVELA ========== -->
        <n-scrollbar v-if="activeTab === 'novel'" class="pane-scroll">
          <div class="pane-pad stack-sm">
            <div class="identity-row">
              <div v-if="coverEditable" class="cover-editor compact">
                <div class="cover-preview-tiny" @click="triggerFileInput">
                  <img
                    v-if="displayCoverUrl"
                    :src="displayCoverUrl"
                    alt="Portada"
                    loading="lazy"
                    @error="onCoverImageError"
                  />
                  <div v-else class="cover-placeholder">
                    <n-icon :size="20"><ImageOutline /></n-icon>
                  </div>
                  <div class="cover-overlay">
                    {{ displayCoverUrl ? 'Cambiar' : 'Subir' }}
                  </div>
                </div>
                <n-button
                  v-if="displayCoverUrl"
                  size="tiny"
                  type="error"
                  quaternary
                  @click="removeCover"
                >
                  Quitar
                </n-button>
                <input
                  ref="fileInputRef"
                  type="file"
                  accept="image/jpeg,image/png,image/webp"
                  hidden
                  aria-hidden="true"
                  @change="onFileSelected"
                />
              </div>

              <div class="identity-fields">
                <div class="field-grid-3">
                  <div class="form-group span-2">
                    <label for="novel-source-url" class="lbl">URL fuente</label>
                    <n-input
                      id="novel-source-url"
                      v-model:value="novelDraft.url"
                      size="small"
                      placeholder="https://..."
                      inputmode="url"
                    />
                  </div>
                  <div class="form-group">
                    <label for="novel-status" class="lbl">Estatus</label>
                    <n-select
                      id="novel-status"
                      size="small"
                      :value="novelDraft.status"
                      :options="statusOptions"
                      @update:value="(v) => (novelDraft.status = v ?? 'ongoing')"
                    />
                  </div>
                  <div class="form-group">
                    <label for="novel-source-lang" class="lbl">Idioma origen</label>
                    <n-select
                      id="novel-source-lang"
                      v-model:value="novelDraft.sourceLanguage"
                      size="small"
                      :options="languageOptions"
                    />
                  </div>
                  <div class="form-group">
                    <label for="novel-target-lang" class="lbl">Idioma destino</label>
                    <n-select
                      id="novel-target-lang"
                      v-model:value="novelDraft.targetLanguage"
                      size="small"
                      :options="languageOptionsNoAuto"
                    />
                  </div>
                  <div class="form-group">
                    <label class="lbl">Etiquetas</label>
                    <n-dynamic-tags
                      size="small"
                      :value="novelDraft.tags"
                      placeholder="Enter para añadir"
                      :max="20"
                      @update:value="(v: string[]) => (novelDraft.tags = v)"
                    />
                  </div>
                </div>
              </div>
            </div>

            <div class="meta-split">
              <section class="meta-col">
                <header class="meta-col-head">
                  <span>Origen</span>
                </header>
                <div class="stack-xs">
                  <div class="form-group">
                    <label class="lbl" for="novel-source-title">Título</label>
                    <n-input
                      id="novel-source-title"
                      size="small"
                      :value="novelDraft.sourceTitle"
                      maxlength="500"
                      placeholder="Título original"
                      @update:value="(v) => (novelDraft.sourceTitle = v)"
                    />
                  </div>
                  <div class="form-group">
                    <label class="lbl" for="novel-source-author">Autor</label>
                    <n-input
                      id="novel-source-author"
                      size="small"
                      :value="novelDraft.sourceAuthor"
                      maxlength="300"
                      placeholder="Autor"
                      @update:value="(v) => (novelDraft.sourceAuthor = v)"
                    />
                  </div>
                  <div class="field-grid-2">
                    <div class="form-group">
                      <label class="lbl" for="novel-source-series">Serie</label>
                      <n-auto-complete
                        id="novel-source-series"
                        size="small"
                        :value="novelDraft.sourceSeries"
                        :options="sourceSeriesOptions"
                        placeholder="Serie"
                        maxlength="300"
                        @update:value="(v) => (novelDraft.sourceSeries = v ?? '')"
                      />
                    </div>
                    <div class="form-group">
                      <label class="lbl" for="novel-source-number">N.º</label>
                      <n-input
                        id="novel-source-number"
                        size="small"
                        :value="novelDraft.sourceNumber"
                        placeholder="1, 2.5…"
                        maxlength="50"
                        @update:value="(v) => (novelDraft.sourceNumber = v)"
                      />
                    </div>
                  </div>
                  <div class="form-group">
                    <label class="lbl" for="novel-source-desc">Descripción</label>
                    <n-input
                      id="novel-source-desc"
                      size="small"
                      type="textarea"
                      :rows="3"
                      maxlength="5000"
                      show-count
                      :value="novelDraft.sourceDescription"
                      placeholder="Sinopsis"
                      @update:value="(v) => (novelDraft.sourceDescription = v)"
                    />
                  </div>
                </div>
              </section>

              <div class="meta-transfer">
                <n-tooltip trigger="hover">
                  <template #trigger>
                    <n-button size="tiny" quaternary circle @click="copySourceToTarget">
                      <template #icon><n-icon><ArrowForwardOutline /></n-icon></template>
                    </n-button>
                  </template>
                  Copiar origen → destino
                </n-tooltip>
              </div>

              <section class="meta-col">
                <header class="meta-col-head">
                  <span>Destino</span>
                </header>
                <div class="stack-xs">
                  <div class="form-group">
                    <label class="lbl" for="novel-target-title">Título</label>
                    <n-input
                      id="novel-target-title"
                      size="small"
                      :value="novelDraft.targetTitle"
                      maxlength="500"
                      placeholder="Título traducido"
                      @update:value="(v) => (novelDraft.targetTitle = v)"
                    />
                  </div>
                  <div class="form-group">
                    <label class="lbl" for="novel-target-author">Autor</label>
                    <n-input
                      id="novel-target-author"
                      size="small"
                      :value="novelDraft.targetAuthor"
                      maxlength="300"
                      placeholder="Autor (traducido)"
                      @update:value="(v) => (novelDraft.targetAuthor = v)"
                    />
                  </div>
                  <div class="field-grid-2">
                    <div class="form-group">
                      <label class="lbl" for="novel-target-series">Serie</label>
                      <n-auto-complete
                        id="novel-target-series"
                        size="small"
                        :value="novelDraft.targetSeries"
                        :options="targetSeriesOptions"
                        placeholder="Serie"
                        maxlength="300"
                        @update:value="(v) => (novelDraft.targetSeries = v ?? '')"
                      />
                    </div>
                    <div class="form-group">
                      <label class="lbl" for="novel-target-number">N.º</label>
                      <n-input
                        id="novel-target-number"
                        size="small"
                        :value="novelDraft.targetNumber"
                        placeholder="1, 2.5…"
                        maxlength="50"
                        @update:value="(v) => (novelDraft.targetNumber = v)"
                      />
                    </div>
                  </div>
                  <div class="form-group">
                    <label class="lbl" for="novel-target-desc">Descripción</label>
                    <n-input
                      id="novel-target-desc"
                      size="small"
                      type="textarea"
                      :rows="3"
                      maxlength="5000"
                      show-count
                      :value="novelDraft.targetDescription"
                      placeholder="Descripción traducida"
                      @update:value="(v) => (novelDraft.targetDescription = v)"
                    />
                  </div>
                </div>
              </section>
            </div>

          </div>
        </n-scrollbar>

        <!-- ========== GLOSARIO ========== -->
        <n-scrollbar v-else-if="activeTab === 'glossary'" class="pane-scroll">
          <div class="pane-pad stack-sm">
            <n-collapse :default-expanded-names="[]">
              <n-collapse-item title="Generar glosario con IA" name="gen">
                <div class="stack-sm">
                  <div class="field-grid-4">
                    <div class="form-group">
                      <label class="lbl">Cap. desde</label>
                      <n-input-number
                        v-model:value="glossaryGenOptions.chapterFrom"
                        :min="1"
                        size="small"
                        class="w-full"
                      />
                    </div>
                    <div class="form-group">
                      <label class="lbl">Cap. hasta</label>
                      <n-input-number
                        v-model:value="glossaryGenOptions.chapterTo"
                        :min="1"
                        size="small"
                        class="w-full"
                      />
                    </div>
                    <div class="form-group">
                      <label class="lbl">Modo</label>
                      <n-button-group size="small">
                        <n-button :type="glossaryGenOptions.mode === 'together' ? 'primary' : 'default'" @click="glossaryGenOptions.mode = 'together'">Junto</n-button>
                        <n-button :type="glossaryGenOptions.mode === 'batch' ? 'primary' : 'default'" @click="glossaryGenOptions.mode = 'batch'">Lotes</n-button>
                      </n-button-group>
                    </div>
                    <div v-if="glossaryGenOptions.mode === 'batch'" class="form-group">
                      <label class="lbl">Max tokens/lote</label>
                      <n-input-number
                        v-model:value="glossaryGenOptions.maxTokensPerBatch"
                        :min="10000"
                        :step="10000"
                        size="small"
                        class="w-full"
                      />
                    </div>
                  </div>

                  <div
                    v-if="estimatedTokensLoading || estimatedTokens !== null"
                    class="small muted token-est"
                  >
                    <n-spin v-if="estimatedTokensLoading" :size="12" />
                    <template v-else-if="estimatedTokens !== null">
                      ~{{ formatTokenCount(estimatedTokens) }} tokens estimados
                    </template>
                  </div>

                  <div class="field-grid-2">
                    <div class="form-group">
                      <label class="lbl">Proveedor</label>
                      <n-select
                        v-model:value="glossaryGenOptions.provider"
                        :options="providerOptions"
                        :loading="providersLoading"
                        placeholder="Proveedor activo"
                        clearable
                        size="small"
                      />
                    </div>
                    <div class="form-group">
                      <label class="lbl">Modelo</label>
                      <n-select
                        v-if="glossaryModelOptions.length > 1"
                        v-model:value="glossaryGenOptions.model"
                        :options="glossaryModelOptions"
                        placeholder="Modelo"
                        clearable
                        size="small"
                      />
                      <n-input
                        v-else
                        v-model:value="glossaryGenOptions.model"
                        placeholder="Modelo"
                        size="small"
                      />
                    </div>
                  </div>

                  <div class="row-end">
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
              </n-collapse-item>
            </n-collapse>

            <div class="row-between">
              <div class="glossary-toolbar">
                <h4 class="section-title">Glosario</h4>
                <n-input
                  v-if="novelDraft.glossary.length > 5"
                  v-model:value="glossaryFilter"
                  size="tiny"
                  clearable
                  placeholder="Filtrar…"
                  style="max-width: 180px"
                />
                <span class="small muted">{{ filteredGlossary.length }} entradas</span>
              </div>
              <n-button size="small" @click="addGlossaryEntry">
                <template #icon><n-icon><AddOutline /></n-icon></template>
                Añadir
              </n-button>
            </div>

            <div v-if="filteredGlossary.length === 0" class="muted small empty-state">
              {{ novelDraft.glossary.length === 0 ? 'Sin entradas de glosario.' : 'Sin coincidencias.' }}
            </div>
            <div v-else class="glossary-list">
              <div class="glossary-list-header">
                <span class="glossary-col-source">Origen</span>
                <span class="glossary-col-target">Destino</span>
                <span class="glossary-col-context">Contexto</span>
                <span class="glossary-col-actions" />
              </div>
              <div
                v-for="entry in filteredGlossary"
                :key="entry.id"
                class="glossary-list-row"
              >
                <n-input
                  v-model:value="entry.source"
                  placeholder="Origen"
                  size="tiny"
                  class="glossary-col-source"
                />
                <n-input
                  v-model:value="entry.target"
                  placeholder="Destino"
                  size="tiny"
                  class="glossary-col-target"
                />
                <n-input
                  v-model:value="entry.context"
                  placeholder="Contexto"
                  size="tiny"
                  class="glossary-col-context"
                />
                <n-button
                  type="error"
                  quaternary
                  circle
                  size="tiny"
                  class="glossary-col-actions glossary-delete-btn"
                  @click="removeGlossaryEntry(entry.id)"
                >
                  <template #icon><n-icon :size="14"><TrashOutline /></n-icon></template>
                </n-button>
              </div>
            </div>
          </div>
        </n-scrollbar>

        <!-- ========== PROMPTS ========== -->
        <n-scrollbar v-else-if="activeTab === 'prompts'" class="pane-scroll">
          <div class="pane-pad stack-sm">
            <n-alert type="info" :bordered="false" class="compact-alert">
              Edita un system prompt para crear un override. Vacío o restaurado = prompt global.
              El user prompt se genera automáticamente.
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

        <!-- ========== IA ========== -->
        <n-scrollbar v-else-if="activeTab === 'ai'" class="pane-scroll">
          <div class="pane-pad stack-sm">
            <div class="field-grid-3">
              <div class="form-group">
                <label class="lbl">Proveedor</label>
                <n-select
                  v-model:value="settingsDraft.ai.provider"
                  :options="providerOptions"
                  :loading="providersLoading"
                  :disabled="providersLoading"
                  placeholder="Usar global"
                  clearable
                  size="small"
                  @update:value="onProviderChange"
                />
              </div>
              <div class="form-group">
                <label class="lbl">Modelo</label>
                <n-select
                  v-if="modelOptions.length > 1"
                  v-model:value="settingsDraft.ai.model"
                  :options="modelOptions"
                  :disabled="!settingsDraft.ai.provider || providersLoading"
                  placeholder="Usar global"
                  clearable
                  size="small"
                />
                <n-input
                  v-else
                  v-model:value="settingsDraft.ai.model"
                  :disabled="!settingsDraft.ai.provider || providersLoading"
                  placeholder="Ej: local-model"
                  size="small"
                />
              </div>
              <FieldNumber
                v-model="timeoutSec"
                label="Timeout (s)"
                :min="10"
                allow-clear
                placeholder="Global"
                wrapper-style="min-width: 0"
              />
            </div>

            <n-alert type="info" :bordered="false" class="compact-alert">
              Campos vacíos → configuración global. El timeout solo aplica si hay valor.
            </n-alert>

            <div class="subpanel">
              <div class="switch-row">
                <div>
                  <div class="switch-title">Modelo distinto para títulos</div>
                  <div class="small muted">
                    Modelo más pequeño para títulos. Si falla, usa el de contenido.
                  </div>
                </div>
                <n-switch v-model:value="settingsDraft.ai.titleEnabled" />
              </div>
              <div v-if="settingsDraft.ai.titleEnabled" class="field-grid-2" style="margin-top: 0.75rem">
                <div class="form-group">
                  <label class="lbl">Proveedor títulos</label>
                  <n-select
                    v-model:value="settingsDraft.ai.titleProvider"
                    :options="providerOptions"
                    :loading="providersLoading"
                    :disabled="providersLoading"
                    placeholder="Proveedor de contenido"
                    clearable
                    size="small"
                    @update:value="onTitleProviderChange"
                  />
                </div>
                <div class="form-group">
                  <label class="lbl">Modelo títulos</label>
                  <n-select
                    v-if="titleModelOptions.length > 1"
                    v-model:value="settingsDraft.ai.titleModel"
                    :options="titleModelOptions"
                    :disabled="!settingsDraft.ai.titleProvider || providersLoading"
                    placeholder="Modelo de contenido"
                    clearable
                    size="small"
                  />
                  <n-input
                    v-else
                    v-model:value="settingsDraft.ai.titleModel"
                    :disabled="!settingsDraft.ai.titleProvider || providersLoading"
                    placeholder="Ej: local-model"
                    size="small"
                  />
                </div>
              </div>
            </div>
          </div>
        </n-scrollbar>

        <!-- ========== TRADUCCIÓN ========== -->
        <n-scrollbar v-else-if="activeTab === 'translation'" class="pane-scroll">
          <div class="pane-pad stack-sm">
            <div class="switch-row">
              <div>
                <div class="switch-title">Auto segmentación</div>
                <div class="small muted">Divide capítulos largos antes de enviarlos al proveedor.</div>
              </div>
              <n-switch v-model:value="settingsDraft.translation.autoSegment" />
            </div>

            <div class="field-grid-4">
              <FieldNumber
                v-model="settingsDraft.translation.thresholdChars"
                label="Umbral auto"
                :min="1000"
              />
              <FieldNumber
                v-model="settingsDraft.translation.maxChars"
                label="Máx. segmento"
                :min="500"
              />
              <FieldNumber
                v-model="settingsDraft.translation.minChars"
                label="Mín. segmento"
                :min="100"
              />
              <FieldNumber
                v-model="settingsDraft.translation.maxRetries"
                label="Reintentos"
                :min="0"
              />
            </div>

            <div class="switch-row">
              <div>
                <div class="switch-title">Enable check</div>
                <div class="small muted">Verificación adicional en el backend.</div>
              </div>
              <n-switch v-model:value="settingsDraft.translation.enableCheck" />
            </div>

            <div class="switch-row">
              <div>
                <div class="switch-title">Incluir títulos anteriores</div>
                <div class="small muted">Contexto de títulos previos al traducir.</div>
              </div>
              <n-switch v-model:value="settingsDraft.translation.includePreviousChapterTitles" />
            </div>
          </div>
        </n-scrollbar>

        <!-- ========== NOTAS ========== -->
        <n-scrollbar v-else-if="activeTab === 'notes'" class="pane-scroll">
          <div class="pane-pad">
            <label class="lbl">Notas del proyecto</label>
            <n-input
              v-model:value="settingsDraft.notes"
              type="textarea"
              size="small"
              :autosize="{ minRows: 12, maxRows: 24 }"
              placeholder="Notas libres sobre esta novela…"
            />
          </div>
        </n-scrollbar>

        <!-- ========== AVANZADO ========== -->
        <n-scrollbar v-else class="pane-scroll">
          <div class="pane-pad stack-sm">
            <n-collapse :default-expanded-names="['commands']">
              <n-collapse-item title="Comandos personalizados" name="commands">
                <n-input
                  id="novel-custom-commands"
                  v-model:value="novelDraft.customCommands"
                  type="textarea"
                  size="small"
                  :autosize="{ minRows: 2, maxRows: 5 }"
                  class="mono"
                  placeholder="Comandos especiales de procesamiento"
                />
              </n-collapse-item>
              <n-collapse-item name="danger">
                <template #header>
                  <span class="danger-label">Zona de peligro</span>
                </template>
                <div class="danger-zone compact">
                  <p class="small muted" style="margin: 0">
                    Elimina la novela y todos los capítulos, traducciones y datos asociados.
                  </p>
                  <n-popconfirm @positive-click="onDeleteNovel">
                    <template #trigger>
                      <n-button type="error" secondary size="small">
                        <template #icon><n-icon><TrashOutline /></n-icon></template>
                        Eliminar novela
                      </n-button>
                    </template>
                    <div style="max-width: 280px">
                      ¿Eliminar
                      <strong class="no-wrap">{{ novelDraft.sourceTitle || 'esta novela' }}</strong>?
                      Esta acción no se puede deshacer.
                    </div>
                  </n-popconfirm>
                </div>
              </n-collapse-item>
            </n-collapse>
          </div>
        </n-scrollbar>
      </div>
    </div>

    <template #action>
      <div class="footer-actions">
        <span v-if="isDirty" class="small muted footer-hint">Cambios sin guardar</span>
        <div class="footer-btns">
          <n-button secondary size="small" @click="emit('update:open', false)">Cerrar</n-button>
          <n-button type="warning" secondary size="small" :disabled="!isDirty" @click="reset">
            Reset
          </n-button>
          <n-button type="primary" size="small" :loading="saving" :disabled="!isDirty" @click="save">
            Guardar
          </n-button>
        </div>
      </div>
    </template>
  </n-modal>
</template>

<script setup lang="ts">
import { computed, ref, watch, onBeforeUnmount } from "vue";
import { useRouter } from "vue-router";
import {
  useMessage,
  NModal,
  NButton,
  NButtonGroup,
  NInput,
  NAlert,
  NSwitch,
  NSelect,
  NIcon,
  NScrollbar,
  NAutoComplete,
  NDynamicTags,
  NPopconfirm,
  NInputNumber,
  NSpin,
  NTabs,
  NTab,
  NCollapse,
  NCollapseItem,
  NTooltip,
} from "naive-ui";
import {
  AddOutline,
  TrashOutline,
  ImageOutline,
  ArrowForwardOutline,
  BookOutline,
  ListOutline,
  CodeSlashOutline,
  HardwareChipOutline,
  LanguageOutline,
  DocumentTextOutline,
  SettingsOutline,
} from "@vicons/ionicons5";
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

function onCoverImageError() {
  localCoverUrl.value = undefined;
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
  { value: "novel", label: "Novela", icon: BookOutline },
  { value: "glossary", label: "Glosario", icon: ListOutline },
  { value: "prompts", label: "Prompts", icon: CodeSlashOutline },
  { value: "ai", label: "IA", icon: HardwareChipOutline },
  { value: "translation", label: "Traducción", icon: LanguageOutline },
  { value: "notes", label: "Notas", icon: DocumentTextOutline },
  { value: "advanced", label: "Avanzado", icon: SettingsOutline },
];

const modalStyle = {
  width: "min(1080px, 96vw)",
  height: "min(680px, 88vh)",
  "--n-padding-top": "8px",
  "--n-padding-bottom": "8px",
};

const contentStyle = {
  display: "flex",
  flexDirection: "column" as const,
  flex: "1",
  minHeight: "0",
  padding: "0",
  overflow: "hidden",
};

const glossaryFilter = ref("");

const filteredGlossary = computed(() => {
  const q = glossaryFilter.value.trim().toLowerCase();
  if (!q) return novelDraft.value.glossary;
  return novelDraft.value.glossary.filter(
    (e) =>
      e.source.toLowerCase().includes(q) || e.target.toLowerCase().includes(q) || (e.context?.toLowerCase().includes(q) ?? false),
  );
});

const draftInitialized = ref(false);

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

const savedSnapshot = ref("");

function takeSnapshot() {
  savedSnapshot.value = JSON.stringify({
    novel: {
      url: novelDraft.value.url,
      status: novelDraft.value.status,
      sourceLanguage: novelDraft.value.sourceLanguage,
      targetLanguage: novelDraft.value.targetLanguage,
      tags: novelDraft.value.tags,
      sourceTitle: novelDraft.value.sourceTitle,
      sourceAuthor: novelDraft.value.sourceAuthor,
      sourceSeries: novelDraft.value.sourceSeries,
      sourceNumber: novelDraft.value.sourceNumber,
      sourceDescription: novelDraft.value.sourceDescription,
      targetTitle: novelDraft.value.targetTitle,
      targetAuthor: novelDraft.value.targetAuthor,
      targetSeries: novelDraft.value.targetSeries,
      targetNumber: novelDraft.value.targetNumber,
      targetDescription: novelDraft.value.targetDescription,
      customCommands: novelDraft.value.customCommands,
      glossary: novelDraft.value.glossary,
      prompts: novelDraft.value.prompts,
    },
    settings: settingsDraft.value,
    timeoutSec: timeoutSec.value,
    cover: !!pendingCoverFile,
  });
}

const isDirty = computed(() => {
  if (!draftInitialized.value || !savedSnapshot.value) return false;
  const current = JSON.stringify({
    novel: {
      url: novelDraft.value.url,
      status: novelDraft.value.status,
      sourceLanguage: novelDraft.value.sourceLanguage,
      targetLanguage: novelDraft.value.targetLanguage,
      tags: novelDraft.value.tags,
      sourceTitle: novelDraft.value.sourceTitle,
      sourceAuthor: novelDraft.value.sourceAuthor,
      sourceSeries: novelDraft.value.sourceSeries,
      sourceNumber: novelDraft.value.sourceNumber,
      sourceDescription: novelDraft.value.sourceDescription,
      targetTitle: novelDraft.value.targetTitle,
      targetAuthor: novelDraft.value.targetAuthor,
      targetSeries: novelDraft.value.targetSeries,
      targetNumber: novelDraft.value.targetNumber,
      targetDescription: novelDraft.value.targetDescription,
      customCommands: novelDraft.value.customCommands,
      glossary: novelDraft.value.glossary,
      prompts: novelDraft.value.prompts,
    },
    settings: settingsDraft.value,
    timeoutSec: timeoutSec.value,
    cover: !!pendingCoverFile,
  });
  return current !== savedSnapshot.value;
});

function copySourceToTarget() {
  novelDraft.value.targetTitle = novelDraft.value.sourceTitle;
  novelDraft.value.targetAuthor = novelDraft.value.sourceAuthor;
  novelDraft.value.targetSeries = novelDraft.value.sourceSeries;
  novelDraft.value.targetNumber = novelDraft.value.sourceNumber;
  novelDraft.value.targetDescription = novelDraft.value.sourceDescription;
  message.success("Metadatos copiados a destino", { duration: 1800 });
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
  takeSnapshot();
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

watch(
  () => props.novel.glossary,
  (newGlossary) => {
    if (!props.open || !draftInitialized.value) return;
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
    takeSnapshot();
    emit("update:open", false);
  } finally {
    saving.value = false;
    uploadingCover.value = false;
  }
}
</script>

<style scoped>
/* Shell */
.modal-header {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
  width: 100%;
  min-width: 0;
}

.modal-title-block {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  min-width: 0;
}

.modal-title {
  font-weight: 600;
  font-size: 0.95rem;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.dirty-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--warning, #f59e0b);
  flex-shrink: 0;
}

.modal-body {
  display: flex;
  flex: 1;
  min-height: 0;
  overflow: hidden;
}

.side-nav {
  width: 148px;
  flex-shrink: 0;
  border-right: 1px solid var(--divide);
  padding: 0.5rem 0.375rem;
  display: flex;
  flex-direction: column;
  gap: 2px;
  background: var(--surface-muted);
  overflow-y: auto;
}

.side-nav-item {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  width: 100%;
  border: none;
  background: transparent;
  color: var(--text-secondary);
  font: inherit;
  font-size: 0.8125rem;
  padding: 0.45rem 0.6rem;
  border-radius: var(--radius-md, 6px);
  cursor: pointer;
  text-align: left;
  transition: background 0.12s, color 0.12s;
}

.side-nav-item:hover {
  background: var(--mock-row, rgba(0, 0, 0, 0.04));
  color: var(--text-primary);
}

.side-nav-item.active {
  background: color-mix(in oklab, var(--primary, #3b82f6) 14%, transparent);
  color: var(--primary, #3b82f6);
  font-weight: 600;
}

.main-pane {
  flex: 1;
  min-width: 0;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.pane-scroll {
  flex: 1;
  min-height: 0;
}

.pane-pad {
  padding: 0.65rem 1rem 1rem;
}

/* Density */
.stack-sm { display: flex; flex-direction: column; gap: 0.75rem; }
.stack-xs { display: flex; flex-direction: column; gap: 0.45rem; }

.lbl {
  display: block;
  font-size: 0.7rem;
  font-weight: 500;
  color: var(--text-secondary);
  margin-bottom: 0.15rem;
  letter-spacing: 0.01em;
}

.form-group { min-width: 0; }

.field-grid-2 {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0.5rem 0.75rem;
}

.field-grid-3 {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 0.5rem 0.75rem;
}

.field-grid-4 {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 0.5rem 0.75rem;
}

.span-2 { grid-column: span 2; }

.row-between {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
}

.row-end {
  display: flex;
  justify-content: flex-end;
}

.section-title {
  margin: 0;
  font-size: 0.875rem;
  font-weight: 600;
}

.w-full { width: 100%; }

/* Identity / cover */
.identity-row {
  display: flex;
  gap: 1rem;
  align-items: flex-start;
}

.cover-editor.compact {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 0.35rem;
  flex-shrink: 0;
}

.cover-preview-tiny {
  position: relative;
  width: 72px;
  aspect-ratio: 2 / 3;
  border-radius: var(--radius-md, 6px);
  overflow: hidden;
  border: 1px solid var(--divide);
  background: var(--surface-muted);
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
}

.cover-preview-tiny img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.cover-overlay {
  position: absolute;
  inset: auto 0 0 0;
  padding: 0.15rem;
  font-size: 0.65rem;
  text-align: center;
  background: rgba(0, 0, 0, 0.55);
  color: #fff;
  opacity: 0;
  transition: opacity 0.15s;
}

.cover-preview-tiny:hover .cover-overlay { opacity: 1; }

.cover-placeholder { color: var(--text-tertiary); }

.identity-fields { flex: 1; min-width: 0; }

/* Bilingual metadata */
.meta-split {
  display: grid;
  grid-template-columns: 1fr auto 1fr;
  gap: 0.5rem;
  align-items: start;
}

.meta-col {
  border: 1px solid var(--divide);
  border-radius: var(--radius-md, 6px);
  padding: 0.6rem 0.75rem 0.75rem;
  background: var(--surface-base);
  min-width: 0;
}

.meta-col-head {
  font-size: 0.75rem;
  font-weight: 600;
  color: var(--text-secondary);
  margin-bottom: 0.5rem;
  text-transform: uppercase;
  letter-spacing: 0.04em;
}

.meta-transfer {
  display: flex;
  align-items: center;
  padding-top: 2.5rem;
}

/* Switches / subpanels */
.switch-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  padding: 0.35rem 0;
}

.switch-title { font-weight: 600; font-size: 0.875rem; }

.subpanel {
  border-top: 1px solid var(--divide);
  padding-top: 0.85rem;
  margin-top: 0.25rem;
}

.compact-alert :deep(.n-alert) {
  display: flex;
  align-items: flex-start;
}

.compact-alert :deep(.n-alert-icon) {
  flex-shrink: 0;
}

.compact-alert :deep(.n-alert-body) {
  padding-top: 0.5rem;
  padding-right: 0.75rem;
  padding-bottom: 0.5rem;
  font-size: 0.8rem;
}

/* Danger */
.danger-label { color: var(--danger); font-weight: 600; }

.danger-zone.compact {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  flex-wrap: wrap;
}

/* Glossary */
.glossary-toolbar {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  min-width: 0;
  flex-wrap: wrap;
}

.empty-state { padding: 0.5rem 0; }

.glossary-list {
  border: 1px solid var(--divide);
  border-radius: var(--radius-md, 6px);
  background: var(--surface-base);
  overflow: hidden;
}

.glossary-list-header {
  display: grid;
  grid-template-columns: 1fr 1fr 1.4fr auto;
  gap: 0.35rem;
  padding: 0.35rem 0.5rem;
  background: var(--surface-muted);
  border-bottom: 1px solid var(--divide);
  font-size: 0.7rem;
  font-weight: 500;
  color: var(--text-secondary);
  position: sticky;
  top: 0;
  z-index: 1;
}

.glossary-list-row {
  display: grid;
  grid-template-columns: 1fr 1fr 1.4fr auto;
  gap: 0.35rem;
  padding: 0.2rem 0.5rem;
  align-items: center;
  border-bottom: 1px solid var(--divide);
}

.glossary-list-row:last-child { border-bottom: none; }
.glossary-list-row:hover { background: var(--mock-row, rgba(0, 0, 0, 0.03)); }

.glossary-delete-btn { color: #dc2626 !important; }
.glossary-delete-btn:hover {
  background: color-mix(in oklab, #dc2626 10%, transparent) !important;
}

.token-est {
  display: flex;
  align-items: center;
  gap: 0.35rem;
  margin-top: -0.25rem;
}

/* Footer */
.footer-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 0.75rem;
  width: 100%;
}

.footer-btns {
  display: flex;
  gap: 0.4rem;
}

.footer-hint { margin-right: auto; }

/* Responsive */
.mobile-only { display: none; }
.desktop-only { display: flex; }

@media (max-width: 800px) {
  .desktop-only { display: none !important; }
  .mobile-only { display: block !important; }

  .meta-split {
    grid-template-columns: 1fr;
  }

  .meta-transfer {
    padding-top: 0;
    justify-content: center;
    transform: rotate(90deg);
  }

  .field-grid-3,
  .field-grid-4 {
    grid-template-columns: 1fr 1fr;
  }

  .span-2 { grid-column: span 2; }

  .identity-row {
    flex-direction: column;
    align-items: stretch;
  }

  .cover-preview-tiny {
    width: 56px;
  }
}

@media (max-width: 520px) {
  .field-grid-2,
  .field-grid-3,
  .field-grid-4 {
    grid-template-columns: 1fr;
  }

  .span-2 { grid-column: span 1; }

  .glossary-list-header { display: none; }

  .glossary-list-row {
    grid-template-columns: 1fr auto;
    gap: 0.25rem;
    padding: 0.4rem 0.5rem;
  }

  .glossary-col-context { grid-column: 1 / -1; }
}
</style>
