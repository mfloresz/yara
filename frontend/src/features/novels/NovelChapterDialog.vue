<template>
  <n-modal
    :show="open"
    preset="card"
    :title="editingChapter ? 'Editar capítulo' : 'Nuevo capítulo'"
    :style="modalStyle"
    :content-style="contentStyle"
    :segmented="{ content: true, action: true }"
    @update:show="(v) => emit('update:open', v)"
  >
    <div class="chapter-dialog-body">
      <div class="row-wrap">
        <FieldNumber
          v-model="position"
          label="Posición"
          :min="1"
          :max="maxPosition"
          wrapper-style="flex: 0 0 160px"
        />
        <div style="flex: 1; min-width: 240px">
          <label class="small muted">Título</label>
          <n-input v-model:value="title" />
        </div>
      </div>
      <p v-if="positionHint" class="small muted" aria-live="polite" style="margin: 0">
        {{ positionHint }}
      </p>
      <div class="chapter-content-field">
        <label class="small muted">Contenido original (markdown)</label>
        <n-input
          v-model:value="originalContent"
          type="textarea"
          :autosize="{ minRows: 8, maxRows: 18 }"
          class="mono chapter-textarea"
          placeholder="Escribe el contenido original…"
        />
      </div>
    </div>
    <template #action>
      <n-button secondary @click="emit('update:open', false)">Cancelar</n-button>
      <n-button type="primary" :loading="saving" :disabled="!title.trim()" @click="onSave">
        {{ editingChapter ? 'Guardar cambios' : 'Crear capítulo' }}
      </n-button>
    </template>
  </n-modal>
</template>

<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { NButton, NInput, NModal } from "naive-ui";
import FieldNumber from "@/components/FieldNumber.vue";
import { chapterPosition } from "@/domain";
import type { Chapter, ChapterUpsertInput } from "@/domain";

const props = defineProps<{
  open: boolean;
  editingChapter: Chapter | null;
  chapterCount: number;
  saving: boolean;
}>();

const emit = defineEmits<{
  (e: "update:open", value: boolean): void;
  (e: "save", payload: ChapterUpsertInput & { id?: string }): void;
}>();

const modalStyle = {
  width: "min(720px, 96vw)",
  height: "min(640px, 88vh)",
  display: "flex",
  flexDirection: "column" as const,
};

const contentStyle = {
  display: "flex",
  flexDirection: "column" as const,
  flex: "1",
  minHeight: "0",
  overflow: "hidden",
  padding: "0",
};

const position = ref(1);
const title = ref("");
const originalContent = ref("");

const maxPosition = computed(() => props.chapterCount + 1);

const positionHint = computed(() => {
  const pos = position.value;
  const total = props.chapterCount;
  if (!Number.isFinite(pos) || pos < 1) return "";
  if (pos > total) return "Se añadirá al final · sin desplazamientos";
  const shifted = total - pos + 1;
  if (shifted === 1) return "1 capítulo se desplazará";
  return `${shifted} capítulos se desplazarán`;
});

watch(
  () => [props.open, props.editingChapter] as const,
  ([open, editing]) => {
    if (!open) return;
    if (editing) {
      position.value = chapterPosition(editing);
      title.value = editing.title;
      originalContent.value = editing.originalContent || "";
    } else {
      position.value = props.chapterCount + 1;
      title.value = "";
      originalContent.value = "";
    }
  },
  { immediate: true },
);

function onSave() {
  emit("save", {
    id: props.editingChapter?.id,
    position: position.value,
    title: title.value,
    originalContent: originalContent.value || undefined,
  });
}
</script>

<style scoped>
.chapter-dialog-body {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
  gap: 1rem;
  padding: 16px;
  overflow: hidden;
}

.chapter-content-field {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
  gap: 0.35rem;
}

/* El textarea de naive-ui debe ocupar el espacio disponible y scrollear dentro */
.chapter-content-field :deep(.n-input) {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.chapter-content-field :deep(.n-input-wrapper) {
  flex: 1;
  min-height: 0;
}

.chapter-content-field :deep(.n-input__textarea-el) {
  resize: none;
}
</style>
