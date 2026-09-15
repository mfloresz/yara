<template>
  <div class="rich-editor">
    <div class="rich-toolbar row-wrap" role="toolbar" aria-label="Formato de texto">
      <n-button
        v-for="action in toolbarActions"
        :key="action.name"
        size="small"
        secondary
        :type="action.isActive() ? 'primary' : undefined"
        :disabled="action.disabled?.() ?? false"
        @click="action.run"
        :title="action.title"
      >
        {{ action.label }}
      </n-button>
    </div>
    <editor-content :editor="editor" class="rich-content" />
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, watch } from "vue";
import { NButton } from "naive-ui";
import { useEditor, EditorContent } from "@tiptap/vue-3";
import StarterKit from "@tiptap/starter-kit";
import { markdownToHtml } from "@/utils/markdown";
import { htmlToMarkdown } from "@/utils/htmlToMarkdown";

const props = withDefaults(
  defineProps<{ modelValue: string; placeholder?: string; editable?: boolean }>(),
  { placeholder: "Escribe el contenido…", editable: true },
);
const emit = defineEmits<{ (e: "update:modelValue", value: string): void }>();

// Markdown (fuente de verdad) -> HTML solo al entrar/cambiar de capítulo.
// El tecleo fluye en sentido contrario (HTML -> Markdown) vía onUpdate.
const initialHtml = computed(() => markdownToHtml(props.modelValue || "") || "<p></p>");

const editor = useEditor({
  content: initialHtml.value,
  editable: props.editable,
  extensions: [StarterKit],
  editorProps: {
    attributes: {
      class: "rich-prosemirror",
      "aria-label": "Editor de contenido del capítulo",
    },
  },
  onUpdate: ({ editor: next }) => {
    emit("update:modelValue", htmlToMarkdown(next.getHTML()));
  },
});

watch(
  () => props.modelValue,
  (next) => {
    const ed = editor.value;
    if (!ed || ed.isFocused) return;
    // Evita resetear el cursor cuando el cambio viene del propio editor.
    if (htmlToMarkdown(ed.getHTML()) === (next || "").trim()) return;
    ed.commands.setContent(markdownToHtml(next || "") || "<p></p>");
  },
);

watch(
  () => props.editable,
  (next) => editor.value?.setEditable(next),
);

onBeforeUnmount(() => editor.value?.destroy());

type ToolbarAction = {
  name: string;
  title: string;
  label: string;
  run: () => void;
  isActive: () => boolean;
  disabled?: () => boolean;
};

const toolbarActions = computed<ToolbarAction[]>(() => {
  const ed = editor.value;
  const can = (fn: () => boolean) => () => !ed || !fn();
  return [
    {
      name: "bold", title: "Negrita", label: "B",
      run: () => ed?.chain().focus().toggleBold().run(),
      isActive: () => ed?.isActive("bold") ?? false,
      disabled: can(() => ed?.can().chain().focus().toggleBold().run() ?? false),
    },
    {
      name: "italic", title: "Cursiva", label: "I",
      run: () => ed?.chain().focus().toggleItalic().run(),
      isActive: () => ed?.isActive("italic") ?? false,
      disabled: can(() => ed?.can().chain().focus().toggleItalic().run() ?? false),
    },
    {
      name: "strike", title: "Tachado", label: "S",
      run: () => ed?.chain().focus().toggleStrike().run(),
      isActive: () => ed?.isActive("strike") ?? false,
    },
    {
      name: "h1", title: "Encabezado 1", label: "H1",
      run: () => ed?.chain().focus().toggleHeading({ level: 1 }).run(),
      isActive: () => ed?.isActive("heading", { level: 1 }) ?? false,
    },
    {
      name: "h2", title: "Encabezado 2", label: "H2",
      run: () => ed?.chain().focus().toggleHeading({ level: 2 }).run(),
      isActive: () => ed?.isActive("heading", { level: 2 }) ?? false,
    },
    {
      name: "h3", title: "Encabezado 3", label: "H3",
      run: () => ed?.chain().focus().toggleHeading({ level: 3 }).run(),
      isActive: () => ed?.isActive("heading", { level: 3 }) ?? false,
    },
    {
      name: "bullet", title: "Lista", label: "• Lista",
      run: () => ed?.chain().focus().toggleBulletList().run(),
      isActive: () => ed?.isActive("bulletList") ?? false,
    },
    {
      name: "ordered", title: "Lista numerada", label: "1. Lista",
      run: () => ed?.chain().focus().toggleOrderedList().run(),
      isActive: () => ed?.isActive("orderedList") ?? false,
    },
    {
      name: "quote", title: "Cita", label: "“ ”",
      run: () => ed?.chain().focus().toggleBlockquote().run(),
      isActive: () => ed?.isActive("blockquote") ?? false,
    },
    {
      name: "undo", title: "Deshacer", label: "↩",
      run: () => ed?.chain().focus().undo().run(),
      isActive: () => false,
      disabled: can(() => ed?.can().chain().focus().undo().run() ?? false),
    },
    {
      name: "redo", title: "Rehacer", label: "↪",
      run: () => ed?.chain().focus().redo().run(),
      isActive: () => false,
      disabled: can(() => ed?.can().chain().focus().redo().run() ?? false),
    },
  ];
});
</script>

<style scoped>
.rich-editor {
  border: 1px solid var(--divide);
  border-radius: var(--radius-md);
  background: var(--surface-base);
  overflow: hidden;
}
.rich-toolbar {
  gap: 0.375rem;
  padding: 0.5rem;
  border-bottom: 1px solid var(--divide);
  background: var(--surface-muted);
}
.rich-content {
  padding: 0.75rem 1rem;
  min-height: 220px;
}
/* ponytail: estilos mínimos del área editable; sin framework extra */
.rich-content :deep(.rich-prosemirror) {
  outline: none;
  min-height: 220px;
  line-height: 1.75;
  font-size: 1rem;
}
.rich-content :deep(.rich-prosemirror p) {
  margin: 0 0 1rem;
}
.rich-content :deep(.rich-prosemirror h1),
.rich-content :deep(.rich-prosemirror h2),
.rich-content :deep(.rich-prosemirror h3) {
  margin: 0 0 0.75rem;
  line-height: 1.25;
}
.rich-content :deep(.rich-prosemirror blockquote) {
  margin: 0 0 1rem;
  padding-left: 1rem;
  border-left: 3px solid var(--border-strong);
  color: var(--text-secondary);
}
.rich-content :deep(.rich-prosemirror ul),
.rich-content :deep(.rich-prosemirror ol) {
  margin: 0 0 1rem;
  padding-left: 1.25rem;
}
</style>
