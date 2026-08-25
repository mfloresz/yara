<template>
  <n-modal
    :show="open"
    preset="card"
    :title="editingChapter ? 'Editar capítulo' : 'Nuevo capítulo'"
    :style="{ width: 'min(720px, 96vw)' }"
    @update:show="(v) => emit('update:open', v)"
  >
    <div class="stack-md">
      <div class="row-wrap">
        <FieldNumber v-model="chapterOrder" label="N° capítulo" :min="1" wrapper-style="flex: 1; min-width: 160px" />
        <div style="flex: 2; min-width: 240px">
          <label class="small muted">Título</label>
          <n-input v-model:value="title" />
        </div>
      </div>
      <div>
        <label class="small muted">Contenido original (markdown)</label>
        <n-input v-model:value="originalContent" type="textarea" :autosize="{ minRows: 12 }" class="mono" />
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
import { ref, watch } from "vue";
import { NButton, NInput, NModal } from "naive-ui";
import FieldNumber from "@/components/FieldNumber.vue";
import type { Chapter, ChapterUpsertInput } from "@/domain";

const props = defineProps<{
  open: boolean;
  editingChapter: Chapter | null;
  nextChapterOrder: number;
  saving: boolean;
}>();

const emit = defineEmits<{
  (e: "update:open", value: boolean): void;
  (e: "save", payload: ChapterUpsertInput & { id?: string }): void;
}>();

const chapterOrder = ref(1);
const title = ref("");
const originalContent = ref("");

// Only reinitialize on open/edit changes: watching nextChapterOrder would wipe
// user input if novel stats refresh while the dialog is open.
watch(
  () => [props.open, props.editingChapter] as const,
  ([open, editing]) => {
    if (!open) return;
    if (editing) {
      chapterOrder.value = editing.chapterOrder;
      title.value = editing.title;
      originalContent.value = editing.originalContent || "";
    } else {
      chapterOrder.value = props.nextChapterOrder;
      title.value = "";
      originalContent.value = "";
    }
  },
  { immediate: true },
);

function onSave() {
  emit("save", {
    id: props.editingChapter?.id,
    chapterOrder: chapterOrder.value,
    title: title.value,
    originalContent: originalContent.value || undefined,
  });
}
</script>
