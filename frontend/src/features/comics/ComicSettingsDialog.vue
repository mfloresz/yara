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
          <span class="modal-title">{{ draft.sourceTitle || 'Configurar cómic' }}</span>
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
        <!-- ========== FICHA ========== -->
        <n-scrollbar v-if="activeTab === 'comic'" class="pane-scroll">
          <div class="pane-pad stack-sm">
            <div class="identity-row">
              <div class="cover-editor compact">
                <button
                  type="button"
                  class="cover-preview-tiny"
                  :aria-label="displayCoverUrl ? 'Cambiar portada' : 'Subir portada'"
                  @click="triggerFileInput"
                >
                  <img
                    :src="displayCoverUrl || DEFAULT_COVER_URL"
                    alt=""
                    loading="lazy"
                    @error="onCoverImgError"
                  />
                  <span class="cover-overlay">
                    {{ displayCoverUrl ? 'Cambiar' : 'Subir' }}
                  </span>
                </button>
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
                    <label for="comic-source-url" class="lbl">URL fuente</label>
                    <n-input
                      id="comic-source-url"
                      v-model:value="draft.url"
                      size="small"
                      placeholder="https://..."
                      inputmode="url"
                    />
                  </div>
                  <div class="form-group">
                    <label for="comic-status" class="lbl">Estatus</label>
                    <n-select
                      id="comic-status"
                      size="small"
                      :value="draft.status"
                      :options="statusOptions"
                      @update:value="(v) => (draft.status = v ?? 'ongoing')"
                    />
                  </div>
                  <div class="form-group">
                    <label for="comic-source-lang" class="lbl">Idioma origen</label>
                    <n-select
                      id="comic-source-lang"
                      v-model:value="draft.sourceLanguage"
                      size="small"
                      :options="languageOptions"
                    />
                  </div>
                  <div class="form-group">
                    <label for="comic-target-lang" class="lbl">Idioma destino</label>
                    <n-select
                      id="comic-target-lang"
                      v-model:value="draft.targetLanguage"
                      size="small"
                      :options="languageOptionsNoAuto"
                    />
                  </div>
                  <div class="form-group">
                    <label class="lbl">Etiquetas</label>
                    <n-dynamic-tags
                      size="small"
                      :value="draft.tags"
                      placeholder="Enter para añadir"
                      :max="20"
                      @update:value="(v: string[]) => (draft.tags = v)"
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
                    <label class="lbl" for="comic-source-title">Título</label>
                    <n-input
                      id="comic-source-title"
                      v-model:value="draft.sourceTitle"
                      size="small"
                      maxlength="500"
                      placeholder="Título original"
                    />
                  </div>
                  <div class="form-group">
                    <label class="lbl" for="comic-source-author">Autor</label>
                    <n-input
                      id="comic-source-author"
                      v-model:value="draft.sourceAuthor"
                      size="small"
                      maxlength="300"
                      placeholder="Autor"
                    />
                  </div>
                  <div class="field-grid-2">
                    <div class="form-group">
                      <label class="lbl" for="comic-source-series">Serie</label>
                      <n-input
                        id="comic-source-series"
                        v-model:value="draft.sourceSeries"
                        size="small"
                        maxlength="300"
                        placeholder="Serie"
                      />
                    </div>
                    <div class="form-group">
                      <label class="lbl" for="comic-source-number">N.º</label>
                      <n-input
                        id="comic-source-number"
                        v-model:value="draft.sourceNumber"
                        size="small"
                        maxlength="50"
                        placeholder="1, 2.5…"
                      />
                    </div>
                  </div>
                  <div class="form-group">
                    <label class="lbl" for="comic-source-desc">Descripción</label>
                    <n-input
                      id="comic-source-desc"
                      v-model:value="draft.sourceDescription"
                      size="small"
                      type="textarea"
                      :rows="3"
                      maxlength="5000"
                      show-count
                      placeholder="Sinopsis"
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
                    <label class="lbl" for="comic-target-title">Título</label>
                    <n-input
                      id="comic-target-title"
                      v-model:value="draft.targetTitle"
                      size="small"
                      maxlength="500"
                      placeholder="Título traducido"
                    />
                  </div>
                  <div class="form-group">
                    <label class="lbl" for="comic-target-author">Autor</label>
                    <n-input
                      id="comic-target-author"
                      v-model:value="draft.targetAuthor"
                      size="small"
                      maxlength="300"
                      placeholder="Autor (traducido)"
                    />
                  </div>
                  <div class="field-grid-2">
                    <div class="form-group">
                      <label class="lbl" for="comic-target-series">Serie</label>
                      <n-input
                        id="comic-target-series"
                        v-model:value="draft.targetSeries"
                        size="small"
                        maxlength="300"
                        placeholder="Serie"
                      />
                    </div>
                    <div class="form-group">
                      <label class="lbl" for="comic-target-number">N.º</label>
                      <n-input
                        id="comic-target-number"
                        v-model:value="draft.targetNumber"
                        size="small"
                        maxlength="50"
                        placeholder="1, 2.5…"
                      />
                    </div>
                  </div>
                  <div class="form-group">
                    <label class="lbl" for="comic-target-desc">Descripción</label>
                    <n-input
                      id="comic-target-desc"
                      v-model:value="draft.targetDescription"
                      size="small"
                      type="textarea"
                      :rows="3"
                      maxlength="5000"
                      show-count
                      placeholder="Descripción traducida"
                    />
                  </div>
                </div>
              </section>
            </div>
          </div>
        </n-scrollbar>

        <!-- ========== NOTAS ========== -->
        <n-scrollbar v-else-if="activeTab === 'notes'" class="pane-scroll">
          <div class="pane-pad">
            <label class="lbl">Notas del cómic</label>
            <n-input
              v-model:value="draft.notes"
              type="textarea"
              size="small"
              :autosize="{ minRows: 12, maxRows: 24 }"
              placeholder="Notas libres sobre este cómic…"
            />
          </div>
        </n-scrollbar>

        <!-- ========== AVANZADO ========== -->
        <n-scrollbar v-else class="pane-scroll">
          <div class="pane-pad stack-sm">
            <n-collapse :default-expanded-names="['redownload']">
              <n-collapse-item name="redownload">
                <template #header>
                  <span>Redescargar capítulos</span>
                </template>
                <div class="stack-sm">
                  <p class="small muted" style="margin: 0">
                    Vuelve a descargar las páginas de los capítulos fallidos o
                    pendientes desde la fuente ({{ props.failedChapters ?? 0 }}
                    pendiente(s)). Las páginas ya descargadas se conservan.
                  </p>
                  <div class="redownload-actions">
                    <n-popconfirm
                      :disabled="!props.comic.url || !props.failedChapters"
                      @positive-click="onReDownloadChapters"
                    >
                      <template #trigger>
                        <n-button
                          size="small"
                          :loading="reDownloading"
                          :disabled="!props.comic.url || !props.failedChapters"
                        >
                          <template #icon><n-icon><RefreshOutline /></n-icon></template>
                          Re-descargar pendientes
                        </n-button>
                      </template>
                      <div style="max-width: 280px">
                        Se reemplazarán las páginas de los capítulos fallidos o
                        pendientes. Puede tardar varios minutos.
                      </div>
                    </n-popconfirm>
                    <span v-if="!props.comic.url" class="small muted">
                      El cómic no tiene URL de origen.
                    </span>
                  </div>
                </div>
              </n-collapse-item>
              <n-collapse-item name="danger">
                <template #header>
                  <span class="danger-label">Zona de peligro</span>
                </template>
                <div class="danger-zone compact">
                  <p class="small muted" style="margin: 0">
                    Elimina el cómic y todos sus capítulos y páginas.
                  </p>
                  <n-popconfirm @positive-click="onDeleteComic">
                    <template #trigger>
                      <n-button type="error" secondary size="small">
                        <template #icon><n-icon><TrashOutline /></n-icon></template>
                        Eliminar cómic
                      </n-button>
                    </template>
                    <div style="max-width: 280px">
                      ¿Eliminar el cómic? Esta acción no se puede deshacer.
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
          <n-button type="warning" secondary size="small" :disabled="!isDirty" @click="resetDraft">
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
import { computed, ref, watch } from "vue";
import {
  NButton,
  NCollapse,
  NCollapseItem,
  NDynamicTags,
  NIcon,
  NInput,
  NModal,
  NPopconfirm,
  NScrollbar,
  NSelect,
  NTab,
  NTabs,
  NTooltip,
  useMessage,
} from "naive-ui";
import {
  ArrowForwardOutline,
  BookOutline,
  DocumentTextOutline,
  RefreshOutline,
  SettingsOutline,
  TrashOutline,
} from "@vicons/ionicons5";
import { useAppServices } from "@/app/services";
import { LANGUAGES } from "@/config/languages";
import { type Comic, type ComicStatus } from "@/domain";
import { DEFAULT_COVER_URL, onCoverError } from "@/utils/cover";
import { emitJobChanged } from "@/utils/job-events";

const props = defineProps<{
  open: boolean;
  comic: Comic;
  /** Capítulos fallidos o pendientes; habilita la re-descarga. */
  failedChapters?: number;
}>();

const emit = defineEmits<{
  (e: "update:open", value: boolean): void;
  (e: "saved", comic: Comic): void;
  (e: "deleted"): void;
}>();

const { api } = useAppServices();
const message = useMessage();

const activeTab = ref("comic");
const saving = ref(false);
const reDownloading = ref(false);
const fileInputRef = ref<HTMLInputElement | null>(null);
const localCoverUrl = ref<string | undefined>();
let pendingCoverFile: File | undefined;

const draft = ref<Comic>(cloneComic(props.comic));
const savedSnapshot = ref("");

function cloneComic(value: Comic): Comic {
  return JSON.parse(JSON.stringify(value)) as Comic;
}

// Only the editable identity/metadata fields participate in the dirty check;
// server-owned counters (chapterCount, lastReadAt…) never mark the dialog dirty.
function snapshotOf(value: Comic) {
  return JSON.stringify({
    url: value.url,
    status: value.status,
    sourceLanguage: value.sourceLanguage,
    targetLanguage: value.targetLanguage,
    tags: value.tags,
    sourceTitle: value.sourceTitle,
    sourceAuthor: value.sourceAuthor,
    sourceSeries: value.sourceSeries,
    sourceNumber: value.sourceNumber,
    sourceDescription: value.sourceDescription,
    targetTitle: value.targetTitle,
    targetAuthor: value.targetAuthor,
    targetSeries: value.targetSeries,
    targetNumber: value.targetNumber,
    targetDescription: value.targetDescription,
    notes: value.notes,
  });
}

const isDirty = computed(
  () => Boolean(savedSnapshot.value) && (snapshotOf(draft.value) !== savedSnapshot.value || Boolean(pendingCoverFile)),
);

const visible = computed({
  get: () => props.open,
  set: (value) => emit("update:open", value),
});

const displayCoverUrl = computed(() => localCoverUrl.value || props.comic.coverPath);

const languageOptions = LANGUAGES.map((l) => ({ label: l.name, value: l.code }));
const languageOptionsNoAuto = LANGUAGES.filter((l) => l.code !== "auto").map((l) => ({
  label: l.name,
  value: l.code,
}));

const statusOptions: Array<{ label: string; value: ComicStatus }> = [
  { label: "En curso", value: "ongoing" },
  { label: "Completo", value: "completed" },
  { label: "Pausa", value: "hiatus" },
  { label: "Abandonado", value: "cancelled" },
];

const tabs = [
  { value: "comic", label: "Ficha", icon: BookOutline },
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

function resetDraft() {
  draft.value = cloneComic(props.comic);
  activeTab.value = "comic";
  pendingCoverFile = undefined;
  if (localCoverUrl.value) {
    URL.revokeObjectURL(localCoverUrl.value);
    localCoverUrl.value = undefined;
  }
  savedSnapshot.value = snapshotOf(draft.value);
}

watch(
  () => props.open,
  (open) => {
    if (open) resetDraft();
  },
  { immediate: true },
);

function copySourceToTarget() {
  draft.value.targetTitle = draft.value.sourceTitle;
  draft.value.targetAuthor = draft.value.sourceAuthor;
  draft.value.targetSeries = draft.value.sourceSeries;
  draft.value.targetNumber = draft.value.sourceNumber;
  draft.value.targetDescription = draft.value.sourceDescription;
  message.success("Metadatos copiados a destino", { duration: 1800 });
}

function triggerFileInput() {
  fileInputRef.value?.click();
}

function onFileSelected(event: Event) {
  const file = (event.target as HTMLInputElement).files?.[0];
  if (!file) return;
  if (localCoverUrl.value) URL.revokeObjectURL(localCoverUrl.value);
  localCoverUrl.value = URL.createObjectURL(file);
  pendingCoverFile = file;
}

function onCoverImgError(event: Event) {
  if (localCoverUrl.value) {
    URL.revokeObjectURL(localCoverUrl.value);
    localCoverUrl.value = undefined;
  }
  onCoverError(event);
}

function toPatch(): Partial<Comic> {
  return {
    url: draft.value.url,
    status: draft.value.status,
    sourceLanguage: draft.value.sourceLanguage,
    targetLanguage: draft.value.targetLanguage,
    tags: draft.value.tags,
    sourceTitle: draft.value.sourceTitle,
    sourceAuthor: draft.value.sourceAuthor,
    sourceSeries: draft.value.sourceSeries,
    sourceNumber: draft.value.sourceNumber,
    sourceDescription: draft.value.sourceDescription,
    targetTitle: draft.value.targetTitle,
    targetAuthor: draft.value.targetAuthor,
    targetSeries: draft.value.targetSeries,
    targetNumber: draft.value.targetNumber,
    targetDescription: draft.value.targetDescription,
    notes: draft.value.notes,
  };
}

async function save() {
  if (saving.value) return;
  if (!draft.value.sourceTitle.trim()) {
    message.warning("El título de origen es obligatorio.", { duration: 3000 });
    activeTab.value = "comic";
    return;
  }
  saving.value = true;
  try {
    let updated = await api.comics.patch(props.comic.id, toPatch());
    if (pendingCoverFile) {
      updated = await api.comics.uploadCover(props.comic.id, pendingCoverFile);
      pendingCoverFile = undefined;
      if (localCoverUrl.value) {
        URL.revokeObjectURL(localCoverUrl.value);
        localCoverUrl.value = undefined;
      }
    }
    draft.value = cloneComic(updated);
    savedSnapshot.value = snapshotOf(draft.value);
    emit("saved", updated);
    message.success("Cómic actualizado", { duration: 2500 });
    emit("update:open", false);
  } catch (err) {
    message.error(
      `No se pudieron guardar los cambios: ${err instanceof Error ? err.message : String(err)}`,
      { duration: 4000 },
    );
  } finally {
    saving.value = false;
  }
}

async function onReDownloadChapters() {
  if (reDownloading.value) return;
  reDownloading.value = true;
  try {
    const result = await api.comics.redownloadFromUrl(props.comic.id);
    emitJobChanged();
    message.success(`Re-descarga encolada: ${result.chaptersQueued} capítulo(s).`, {
      duration: 3500,
    });
  } catch (err) {
    message.error(
      `No se pudo encolar la re-descarga: ${err instanceof Error ? err.message : String(err)}`,
      { duration: 4000 },
    );
  } finally {
    reDownloading.value = false;
  }
}

async function onDeleteComic() {
  try {
    await api.comics.delete(props.comic.id);
    emit("update:open", false);
    emit("deleted");
  } catch (err) {
    message.error(
      `No se pudo eliminar el cómic: ${err instanceof Error ? err.message : String(err)}`,
      { duration: 4000 },
    );
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

.span-2 { grid-column: span 2; }

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
  padding: 0;
  font: inherit;
  color: inherit;
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

/* Danger */
.danger-label { color: var(--danger); font-weight: 600; }

.danger-zone.compact {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  flex-wrap: wrap;
}

.redownload-actions {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  flex-wrap: wrap;
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

  .field-grid-3 {
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
  .field-grid-3 {
    grid-template-columns: 1fr;
  }

  .span-2 { grid-column: span 1; }
}
</style>
