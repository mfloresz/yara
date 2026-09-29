<template>
  <AppLayout>
    <div class="chat-page">
      <header class="page-header">
        <div class="page-context">
          <h1 class="page-title">Asistente</h1>
          <p class="muted small" aria-live="polite">
            Consulta y edita tu biblioteca conversando
          </p>
        </div>
        <div class="page-actions">
          <n-button
            secondary
            size="small"
            :disabled="streaming || items.length === 0"
            aria-label="Nueva conversación"
            @click="resetChat"
          >
            <template #icon><n-icon><RefreshOutline /></n-icon></template>
            Nueva conversación
          </n-button>
        </div>
      </header>

      <div ref="scrollContainer" class="chat-scroll" aria-live="polite">
        <div v-if="items.length === 0" class="chat-empty">
          <n-icon :size="42" class="chat-empty-icon"><ChatbubbleEllipsesOutline /></n-icon>
          <h2 class="chat-empty-title">Pregúntale a tu biblioteca</h2>
          <p class="muted small">
            El asistente puede consultar y editar tus novelas: descripciones,
            títulos, capítulos y progreso de traducción.
          </p>
          <div class="chat-suggestions">
            <button
              v-for="suggestion in suggestions"
              :key="suggestion"
              type="button"
              class="chat-suggestion"
              :disabled="streaming"
              @click="fillSuggestion(suggestion)"
            >
              {{ suggestion }}
            </button>
          </div>
        </div>

        <template v-for="(item, index) in items" :key="index">
          <div v-if="item.kind === 'message'" class="chat-row" :class="`chat-row--${item.role}`">
            <div class="chat-bubble" :class="`chat-bubble--${item.role}`">
              <div
                v-if="item.role === 'assistant' && item.content"
                class="markdown-preview chat-markdown"
                v-html="renderMarkdown(item.content)"
              ></div>
              <p v-else class="chat-plain">{{ item.content }}</p>
            </div>
          </div>

          <div v-else-if="item.kind === 'tool'" class="chat-row chat-row--tool">
            <div class="chat-tool" :class="{ 'chat-tool--running': item.running }">
              <n-icon :size="14" class="chat-tool-icon"><BuildOutline /></n-icon>
              <span class="chat-tool-name">{{ item.name }}</span>
              <n-spin v-if="item.running" :size="12" />
              <n-button
                v-else-if="item.result"
                quaternary
                size="tiny"
                class="chat-tool-toggle"
                @click="item.open = !item.open"
              >
                {{ item.open ? "Ocultar resultado" : "Ver resultado" }}
              </n-button>
              <pre v-if="item.open && item.result" class="chat-tool-result">{{ item.result }}</pre>
            </div>
          </div>

          <div v-else class="chat-row chat-row--error">
            <div class="chat-error">{{ item.content }}</div>
          </div>
        </template>

        <div v-if="streaming && waitingForFirstToken" class="chat-row chat-row--assistant">
          <div class="chat-bubble chat-bubble--assistant chat-bubble--pending">
            <span class="chat-pending-dot" aria-hidden="true"></span>
            <span class="muted small">Pensando…</span>
          </div>
        </div>
      </div>

      <div class="chat-composer">
        <div v-if="selectedNovel" class="chat-context-chip">
          <n-icon :size="14"><BookOutline /></n-icon>
          <span class="chat-context-title">{{ selectedNovel.title }}</span>
          <n-button quaternary circle size="tiny" aria-label="Quitar novela seleccionada" @click="clearSelectedNovel">
            <template #icon><n-icon :size="12"><CloseOutline /></n-icon></template>
          </n-button>
        </div>
        <div class="chat-composer-row">
          <n-button
            quaternary
            circle
            class="touch-target"
            aria-label="Seleccionar novela sobre la que preguntar"
            :disabled="streaming"
            @click="pickerOpen = true"
          >
            <template #icon><n-icon><BookOutline /></n-icon></template>
          </n-button>
          <n-input
            v-model:value="draft"
            type="textarea"
            :autosize="{ minRows: 1, maxRows: 6 }"
            placeholder="Escribe una pregunta… (Enter para enviar)"
            :disabled="streaming"
            class="chat-input"
            @keydown.enter.exact.prevent="send"
          />
          <n-button
            type="primary"
            circle
            class="touch-target"
            aria-label="Enviar mensaje"
            :disabled="!canSend"
            :loading="streaming"
            @click="send"
          >
            <template #icon><n-icon><SendOutline /></n-icon></template>
          </n-button>
        </div>
        <p class="muted chat-disclaimer">
          Las herramientas del asistente se ejecutan en el servidor y solo
          afectan a tu biblioteca. Las ediciones se aplican de inmediato.
        </p>
      </div>
    </div>

    <n-modal
      v-model:show="pickerOpen"
      preset="card"
      title="Seleccionar novela"
      :style="{ width: 'min(480px, 96vw)' }"
    >
      <n-input
        v-model:value="pickerQuery"
        clearable
        placeholder="Buscar por título, autor, serie…"
        @keydown.enter.prevent="selectFirstResult"
      />
      <div class="chat-picker-results">
        <n-spin v-if="pickerLoading" size="small" class="chat-picker-loading" />
        <p v-else-if="pickerResults.length === 0" class="muted small chat-picker-empty">
          Sin resultados.
        </p>
        <button
          v-for="novel in pickerResults"
          :key="novel.id"
          type="button"
          class="chat-picker-item"
          @click="chooseNovel(novel)"
        >
          <span class="chat-picker-novel-title">{{ novelTitle(novel) }}</span>
          <span v-if="novelAuthor(novel)" class="muted small">{{ novelAuthor(novel) }}</span>
        </button>
      </div>
    </n-modal>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from "vue";
import {
  NButton,
  NIcon,
  NInput,
  NModal,
  NSpin,
} from "naive-ui";
import {
  BookOutline,
  BuildOutline,
  ChatbubbleEllipsesOutline,
  CloseOutline,
  RefreshOutline,
  SendOutline,
} from "@vicons/ionicons5";
import AppLayout from "@/components/AppLayout.vue";
import { useAppServices } from "@/app/services";
import { markdownToHtml } from "@/utils/markdown";
import type { AgentChatEvent, AgentSessionMessage } from "@/api/types";
import type { Novel } from "@/domain";

type ChatItem =
  | { kind: "message"; role: "user" | "assistant"; content: string }
  | { kind: "tool"; name: string; args?: string; result?: string; running: boolean; open: boolean }
  | { kind: "error"; content: string };

type PickerNovel = Pick<
  Novel,
  "id" | "sourceTitle" | "targetTitle" | "sourceAuthor" | "targetAuthor"
>;

const { api } = useAppServices();

const items = ref<ChatItem[]>([]);
const draft = ref("");
const streaming = ref(false);
const sessionId = ref("");
const selectedNovel = ref<{ id: string; title: string } | null>(null);
const scrollContainer = ref<HTMLElement | null>(null);

const pickerOpen = ref(false);
const pickerQuery = ref("");
const pickerLoading = ref(false);
const pickerResults = ref<PickerNovel[]>([]);

let abortController: AbortController | null = null;

const suggestions = [
  "¿Qué novelas no tienen descripción?",
  "Resume el estado de mi biblioteca",
  "¿Cómo va la traducción de la novela que estoy leyendo?",
];

const canSend = computed(() => !streaming.value && draft.value.trim().length > 0);
const waitingForFirstToken = computed(() => {
  const last = items.value[items.value.length - 1];
  return last?.kind === "message" && last.role === "user";
});

function novelTitle(novel: PickerNovel): string {
  return novel.targetTitle || novel.sourceTitle || "(sin título)";
}

function novelAuthor(novel: PickerNovel): string {
  return novel.targetAuthor || novel.sourceAuthor || "";
}

function renderMarkdown(content: string): string {
  return markdownToHtml(content);
}

function scrollToBottom(): void {
  void nextTick(() => {
    const el = scrollContainer.value;
    if (el) el.scrollTop = el.scrollHeight;
  });
}

function fillSuggestion(text: string): void {
  draft.value = text;
}

function clearSelectedNovel(): void {
  selectedNovel.value = null;
}

function mapHistory(messages: AgentSessionMessage[]): void {
  const mapped: ChatItem[] = [];
  const pendingResults = new Map<string, { index: number }>();
  for (const message of messages) {
    if (message.role === "user") {
      mapped.push({ kind: "message", role: "user", content: message.content ?? "" });
      continue;
    }
    if (message.role === "assistant") {
      for (const call of message.toolCalls ?? []) {
        pendingResults.set(call.id, { index: mapped.length });
        mapped.push({ kind: "tool", name: call.name, args: call.args, running: false, open: false });
      }
      if (message.content) {
        mapped.push({ kind: "message", role: "assistant", content: message.content });
      }
      continue;
    }
    if (message.role === "tool" && message.toolCallId) {
      const slot = pendingResults.get(message.toolCallId);
      if (slot && mapped[slot.index]?.kind === "tool") {
        const tool = mapped[slot.index] as Extract<ChatItem, { kind: "tool" }>;
        tool.result = message.content ?? "";
      }
    }
  }
  items.value = mapped;
}

onMounted(async () => {
  try {
    const session = await api.agent.getSession();
    if (session) {
      sessionId.value = session.id;
      mapHistory(session.messages ?? []);
      scrollToBottom();
    }
  } catch {
    // A missing session is fine; the page starts empty.
  }
});

onBeforeUnmount(() => {
  abortController?.abort();
});

async function resetChat(): Promise<void> {
  if (streaming.value) return;
  try {
    await api.agent.resetSession();
  } catch {
    // Even if the server reset fails, clear the local view.
  }
  sessionId.value = "";
  selectedNovel.value = null;
  items.value = [];
}

async function send(): Promise<void> {
  const message = draft.value.trim();
  if (!message || streaming.value) return;

  draft.value = "";
  items.value.push({ kind: "message", role: "user", content: message });
  scrollToBottom();

  streaming.value = true;
  abortController = new AbortController();

  let assistantText = "";
  const ensureAssistant = (): Extract<ChatItem, { kind: "message" }> => {
    const last = items.value[items.value.length - 1];
    if (last && last.kind === "message" && last.role === "assistant") {
      return last;
    }
    const created: Extract<ChatItem, { kind: "message" }> = {
      kind: "message",
      role: "assistant",
      content: "",
    };
    items.value.push(created);
    return created;
  };

  const handleEvent = (event: AgentChatEvent): void => {
    switch (event.type) {
      case "session":
        if (event.sessionId) sessionId.value = event.sessionId;
        break;
      case "text_delta": {
        assistantText += event.text ?? "";
        const assistant = ensureAssistant();
        assistant.content = assistantText;
        scrollToBottom();
        break;
      }
      case "tool_call": {
        assistantText = "";
        items.value.push({
          kind: "tool",
          name: event.tool ?? "tool",
          args: event.args,
          running: true,
          open: false,
        });
        scrollToBottom();
        break;
      }
      case "tool_result": {
        for (let i = items.value.length - 1; i >= 0; i -= 1) {
          const item = items.value[i];
          if (item.kind === "tool" && item.name === event.tool && item.running) {
            item.result = event.result ?? "";
            item.running = false;
            break;
          }
        }
        scrollToBottom();
        break;
      }
      case "done": {
        const content = event.message?.content ?? assistantText;
        if (content) {
          const assistant = ensureAssistant();
          assistant.content = content;
        }
        sessionId.value = event.sessionId ?? sessionId.value;
        streaming.value = false;
        scrollToBottom();
        break;
      }
      case "error": {
        const detail = event.error || event.code || "error del servidor";
        items.value.push({ kind: "error", content: detail });
        streaming.value = false;
        scrollToBottom();
        break;
      }
    }
  };

  try {
    await api.agent.chat(
      {
        sessionId: sessionId.value || undefined,
        novelId: selectedNovel.value?.id,
        message,
      },
      handleEvent,
      abortController.signal,
    );
  } catch (error) {
    const detail =
      error instanceof Error && error.message
        ? error.message
        : "no se pudo contactar al servidor";
    items.value.push({ kind: "error", content: detail });
  } finally {
    streaming.value = false;
    abortController = null;
    scrollToBottom();
  }
}

let pickerDebounce: ReturnType<typeof setTimeout> | null = null;

function searchPicker(): void {
  if (pickerDebounce) clearTimeout(pickerDebounce);
  const query = pickerQuery.value.trim();
  pickerDebounce = setTimeout(async () => {
    pickerLoading.value = true;
    try {
      const result = await api.novels.list({
        q: query || undefined,
        limit: 8,
        offset: 0,
        fields: "id,sourceTitle,targetTitle,sourceAuthor,targetAuthor",
      });
      pickerResults.value = result.items as PickerNovel[];
    } catch {
      pickerResults.value = [];
    } finally {
      pickerLoading.value = false;
    }
  }, 250);
}

function selectFirstResult(): void {
  const first = pickerResults.value[0];
  if (first) chooseNovel(first);
}

function chooseNovel(novel: PickerNovel): void {
  selectedNovel.value = { id: novel.id, title: novelTitle(novel) };
  pickerOpen.value = false;
  pickerQuery.value = "";
  pickerResults.value = [];
}
</script>

<style scoped>
.chat-page {
  display: flex;
  flex-direction: column;
  gap: 1rem;
  height: calc(100dvh - 56px - 2.5rem);
  min-height: 480px;
}

.page-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 1rem;
  flex-wrap: wrap;
}

.page-title {
  margin: 0;
  font-size: 1.75rem;
  font-weight: 700;
  letter-spacing: -0.02em;
}

.chat-scroll {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
  padding: 0.25rem 0.125rem;
}

.chat-empty {
  margin: auto;
  max-width: 26rem;
  text-align: center;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 0.5rem;
  padding: 2rem 1rem;
}

.chat-empty-icon {
  color: var(--muted);
}

.chat-empty-title {
  margin: 0;
  font-size: 1.125rem;
  font-weight: 600;
}

.chat-suggestions {
  display: flex;
  flex-direction: column;
  gap: 0.375rem;
  width: 100%;
  margin-top: 0.5rem;
}

.chat-suggestion {
  border: 1px solid var(--divide);
  background: var(--surface-elevated);
  border-radius: var(--radius-md);
  padding: 0.5rem 0.75rem;
  font-size: 0.875rem;
  color: var(--foreground);
  text-align: left;
  cursor: pointer;
}

.chat-suggestion:hover {
  background: var(--mock-row);
}

.chat-row {
  display: flex;
  width: 100%;
}

.chat-row--user {
  justify-content: flex-end;
}

.chat-row--assistant,
.chat-row--tool,
.chat-row--error {
  justify-content: flex-start;
}

.chat-bubble {
  max-width: min(46rem, 88%);
  border-radius: var(--radius-lg);
  padding: 0.625rem 0.875rem;
  font-size: 0.9375rem;
  line-height: 1.55;
  overflow-wrap: anywhere;
}

.chat-bubble--user {
  background: var(--btn-primary-bg);
  color: var(--btn-primary-fg);
  white-space: pre-wrap;
}

.chat-bubble--assistant {
  background: var(--surface-elevated);
  border: 1px solid var(--divide);
}

.chat-bubble--assistant .chat-plain {
  margin: 0;
}

.chat-bubble--pending {
  display: inline-flex;
  align-items: center;
  gap: 0.5rem;
}

.chat-pending-dot {
  width: 0.5rem;
  height: 0.5rem;
  border-radius: var(--radius-pill);
  background: var(--accent-link);
  animation: chat-pulse 1.2s ease-in-out infinite;
}

@keyframes chat-pulse {
  0%, 100% { opacity: 0.35; }
  50% { opacity: 1; }
}

.chat-markdown :deep(p) {
  margin: 0 0 0.5rem;
}

.chat-markdown :deep(p:last-child) {
  margin-bottom: 0;
}

.chat-markdown :deep(ul),
.chat-markdown :deep(ol) {
  margin: 0.25rem 0 0.5rem;
  padding-left: 1.25rem;
}

.chat-markdown :deep(code) {
  font-family: ui-monospace, monospace;
  font-size: 0.875em;
  background: var(--mock-row);
  border-radius: var(--radius-sm);
  padding: 0.05rem 0.3rem;
}

.chat-tool {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 0.375rem;
  max-width: min(46rem, 88%);
  border: 1px dashed var(--divide);
  border-radius: var(--radius-md);
  background: color-mix(in oklab, var(--surface-elevated) 70%, transparent);
  padding: 0.3125rem 0.625rem;
  font-size: 0.8125rem;
}

.chat-tool--running {
  opacity: 0.85;
}

.chat-tool-icon {
  color: var(--muted);
}

.chat-tool-name {
  font-family: ui-monospace, monospace;
  font-size: 0.75rem;
  color: var(--muted);
}

.chat-tool-toggle {
  margin-left: auto;
  font-size: 0.75rem;
}

.chat-tool-result {
  flex-basis: 100%;
  margin: 0.25rem 0 0;
  max-height: 12rem;
  overflow: auto;
  white-space: pre-wrap;
  font-family: ui-monospace, monospace;
  font-size: 0.75rem;
  color: var(--muted);
  background: var(--page-bg);
  border-radius: var(--radius-sm);
  padding: 0.5rem;
}

.chat-error {
  max-width: min(46rem, 88%);
  border: 1px solid var(--danger);
  color: var(--danger);
  border-radius: var(--radius-md);
  padding: 0.5rem 0.75rem;
  font-size: 0.875rem;
}

.chat-composer {
  border-top: 1px solid var(--divide);
  padding-top: 0.75rem;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.chat-context-chip {
  display: inline-flex;
  align-items: center;
  gap: 0.375rem;
  align-self: flex-start;
  border: 1px solid var(--divide);
  background: var(--surface-elevated);
  border-radius: var(--radius-pill);
  padding: 0.1875rem 0.375rem 0.1875rem 0.625rem;
  font-size: 0.8125rem;
  max-width: 100%;
}

.chat-context-title {
  max-width: 18rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.chat-composer-row {
  display: flex;
  align-items: flex-end;
  gap: 0.5rem;
}

.chat-input {
  flex: 1;
}

.chat-disclaimer {
  margin: 0;
  font-size: 0.75rem;
}

.chat-picker-results {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
  margin-top: 0.75rem;
  max-height: 22rem;
  overflow-y: auto;
}

.chat-picker-loading,
.chat-picker-empty {
  padding: 0.75rem 0;
  text-align: center;
}

.chat-picker-item {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 0.125rem;
  border: 1px solid transparent;
  background: transparent;
  border-radius: var(--radius-md);
  padding: 0.5rem 0.625rem;
  cursor: pointer;
  text-align: left;
}

.chat-picker-item:hover {
  background: var(--mock-row);
  border-color: var(--divide);
}

.chat-picker-novel-title {
  font-size: 0.9375rem;
  font-weight: 600;
  color: var(--foreground);
}

@media (max-width: 768px) {
  .chat-page {
    height: calc(100dvh - 52px - 2.5rem);
  }

  .chat-bubble {
    max-width: 94%;
  }
}
</style>
