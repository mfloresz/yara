<template>
  <AppLayout>
    <div class="chat-page">
      <header class="page-header">
        <div class="page-context">
          <h1 class="page-title">Asistente</h1>
          <p class="muted small">
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

      <!-- role="log" announces appended messages instead of re-reading the
                 whole transcript; the pending/streaming indicator below carries its
                 own status so token-by-token deltas stay silent. -->
            <div
              ref="scrollContainer"
              class="chat-scroll"
              role="log"
              aria-live="polite"
              aria-relevant="additions"
              :aria-busy="streaming"
              aria-label="Conversación con el asistente"
            >
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
              <template v-if="item.role === 'assistant' && item.content">
                <!-- Streaming assistant text is excluded from the live region:
                     aria-live="off" keeps per-token mutations from re-announcing. -->
                <div
                  class="markdown-preview chat-markdown"
                  :aria-live="isStreamingTail(index) ? 'off' : undefined"
                >
                  <!-- markstream-vue parses incrementally and keeps partial
                       markdown stable, so an unterminated heading or code fence
                       does not flicker while the answer is still arriving.
                       htmlPolicy="escape" matters here: this is LLM output, a
                       lower-trust source than the novel text markdownToHtml
                       was written for. -->
                  <MarkdownRender
                    :content="item.content"
                    :final="!isStreamingTail(index)"
                    html-policy="escape"
                  />
                </div>
              </template>
              <p v-else class="chat-plain">{{ item.content }}</p>
            </div>
          </div>

          <div v-else-if="item.kind === 'tool'" class="chat-row chat-row--tool">
            <div class="chat-tool" :class="{ 'chat-tool--running': item.running }">
              <n-icon :size="14" class="chat-tool-icon"><BuildOutline /></n-icon>
              <span class="chat-tool-name">{{ toolLabel(item.name) }}</span>
              <n-spin v-if="item.running" :size="12" />
              <button
                v-else-if="item.result"
                type="button"
                class="chat-tool-toggle"
                :aria-expanded="item.open"
                @click="item.open = !item.open"
              >
                {{ item.open ? "Ocultar resultado" : "Ver resultado" }}
              </button>
              <pre v-if="item.open && item.result" class="chat-tool-result">{{ item.result }}</pre>
            </div>
          </div>

          <div v-else-if="item.kind === 'question'" class="chat-row chat-row--assistant">
            <div class="chat-bubble chat-bubble--assistant">
              <div class="markdown-preview chat-markdown">
                <MarkdownRender :content="item.question" final html-policy="escape" />
              </div>
              <div class="chat-options">
                <button
                  v-for="opt in item.options"
                  :key="opt.value"
                  type="button"
                  class="chat-suggestion"
                  :disabled="item.answered || streaming"
                  @click="chooseOption(item, opt)"
                >
                  {{ opt.label }}
                </button>
              </div>
            </div>
          </div>

          <div v-else class="chat-row chat-row--error">
            <div class="chat-error">{{ item.content }}</div>
          </div>
        </template>

        <div
          v-if="streaming && waitingForFirstToken"
          class="chat-row chat-row--assistant"
          role="status"
        >
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
            @click="pickerOpen = true"
          >
            <template #icon><n-icon><BookOutline /></n-icon></template>
          </n-button>
          <n-input
            v-model:value="draft"
            type="textarea"
            :autosize="{ minRows: 1, maxRows: 6 }"
            placeholder="Escribe una pregunta… (Enter para enviar)"
            class="chat-input"
            @keydown.enter.exact.prevent="send"
          />
          <n-button
            v-if="streaming"
            quaternary
            circle
            class="touch-target"
            aria-label="Detener la respuesta"
            @click="stopStreaming"
          >
            <template #icon><n-icon><StopOutline /></n-icon></template>
          </n-button>
          <n-button
            v-else
            type="primary"
            circle
            class="touch-target"
            aria-label="Enviar mensaje"
            :disabled="!canSend"
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
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
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
  StopOutline,
} from "@vicons/ionicons5";
import AppLayout from "@/components/AppLayout.vue";
import { useAppServices } from "@/app/services";
import { MarkdownRender } from "markstream-vue";
import "markstream-vue/index.css";
import type { AgentChatEvent, AgentChatOption, AgentSessionMessage } from "@/api/types";
import type { Novel } from "@/domain";

type ChatItem =
  | { kind: "message"; role: "user" | "assistant"; content: string }
  | { kind: "tool"; name: string; args?: string; result?: string; running: boolean; open: boolean }
  | { kind: "question"; question: string; options: AgentChatOption[]; answered: boolean }
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

// Tool identifiers are an implementation detail of the agent loop; the UI
// speaks the user's language, not the backend's.
const TOOL_LABELS: Record<string, string> = {
  list_novels: "Consultando novelas",
  get_novel: "Leyendo la novela",
  get_novel_stats: "Consultando el progreso",
  get_novel_chapters: "Consultando capítulos",
  get_chapter: "Leyendo el capítulo",
  query_library: "Consultando la biblioteca",
  search_chapters: "Buscando en capítulos",
  update_novel: "Actualizando la novela",
  update_chapter: "Actualizando el capítulo",
  set_chapter_status: "Cambiando el estado",
  set_chapter_excluded: "Cambiando la visibilidad",
};

function toolLabel(name: string): string {
  return TOOL_LABELS[name] ?? "Trabajando";
}

const canSend = computed(() => !streaming.value && draft.value.trim().length > 0);
const waitingForFirstToken = computed(() => {
  const last = items.value[items.value.length - 1];
  return last?.kind === "message" && last.role === "user";
});

// The last assistant message is the one still receiving token deltas.
function isStreamingTail(index: number): boolean {
  if (!streaming.value) return false;
  const item = items.value[index];
  if (item?.kind !== "message" || item.role !== "assistant") return false;
  return index === items.value.length - 1;
}

function novelTitle(novel: PickerNovel): string {
  return novel.targetTitle || novel.sourceTitle || "(sin título)";
}

function novelAuthor(novel: PickerNovel): string {
  return novel.targetAuthor || novel.sourceAuthor || "";
}

// Streaming coalescing. Deltas arrive far faster than a frame, so mutating
// the reactive message on every one of them re-renders the whole bubble (and
// forces a layout for the scroll pin) dozens of times per second. Deltas are
// accumulated into a local string and the reactive update happens at most
// once per frame. Parsing itself is incremental now: markstream-vue handles
// partial markdown instead of re-running marked over the whole message.
const STREAM_FLUSH_MS = 60;

let streamFlushTimer: ReturnType<typeof setTimeout> | null = null;
let streamDirty = false;

function scheduleStreamFlush(apply: () => void): void {
  streamDirty = true;
  if (streamFlushTimer) return;
  streamFlushTimer = setTimeout(() => {
    streamFlushTimer = null;
    if (!streamDirty) return;
    streamDirty = false;
    apply();
  }, STREAM_FLUSH_MS);
}

function flushStreamNow(apply: () => void): void {
  if (streamFlushTimer) {
    clearTimeout(streamFlushTimer);
    streamFlushTimer = null;
  }
  streamDirty = false;
  apply();
}

// Distance from the bottom, in px, within which the transcript still counts
// as "pinned". Beyond this the user has deliberately scrolled up to re-read,
// so streaming must not drag them back down.
const STICK_THRESHOLD_PX = 64;

function isPinnedToBottom(): boolean {
  const el = scrollContainer.value;
  if (!el) return true;
  return el.scrollHeight - el.scrollTop - el.clientHeight <= STICK_THRESHOLD_PX;
}

// Call before mutating `items` so the pinned check reflects the pre-update
// geometry, then restore the pin once Vue has flushed the DOM.
function scrollToBottom(force = false): void {
  if (!force && !isPinnedToBottom()) return;
  void nextTick(() => {
    const el = scrollContainer.value;
    if (el) el.scrollTop = el.scrollHeight;
  });
}

function stopStreaming(): void {
  abortController?.abort();
  // Stop the UI immediately. Without this the button stays inert until the
  // pending read() rejects, which is bounded only by the server's per-turn
  // timeout — up to eight minutes of a frozen page after the user asked to
  // stop.
  if (streamFlushTimer) {
    clearTimeout(streamFlushTimer);
    streamFlushTimer = null;
  }
  streamDirty = false;
  streaming.value = false;
  settleRunningTools();
}

// settleRunningTools clears the spinner on any tool chip left mid-flight. A
// turn can end without a tool_result (a mid-stream error, an abort, a
// malformed ask_user) and an eternally spinning chip reads as a hang.
function settleRunningTools(): void {
  for (const item of items.value) {
    if (item.kind === "tool" && item.running) item.running = false;
  }
}

function fillSuggestion(text: string): void {
  draft.value = text;
}

function clearSelectedNovel(): void {
  selectedNovel.value = null;
}

// parseAskUserQuestion rebuilds a persisted ask_user call into a question card
// with its options intact, so an unanswered question survives a page reload.
// It used to flatten to plain text, which meant reloading the page silently
// removed the user's only way to answer.
function parseAskUserQuestion(args?: string): { question: string; options: AgentChatOption[] } | null {
  if (!args) return null;
  try {
    const parsed = JSON.parse(args) as {
      question?: string;
      options?: AgentChatOption[];
    };
    const question = parsed.question?.trim();
    const options = (parsed.options ?? []).filter(
      (o) => o.label?.trim() && o.value?.trim(),
    );
    if (!question || options.length === 0) return null;
    return { question, options };
  } catch {
    return null;
  }
}

function mapHistory(messages: AgentSessionMessage[]): void {
  const mapped: ChatItem[] = [];
  const pendingResults = new Map<string, { index: number }>();
  // Index of the last ask_user card, so the user's next message can mark it
  // answered instead of leaving a stale clickable card in the transcript.
  let lastQuestionIndex = -1;
  for (const message of messages) {
    if (message.role === "user") {
      if (lastQuestionIndex >= 0 && mapped[lastQuestionIndex]?.kind === "question") {
        (mapped[lastQuestionIndex] as Extract<ChatItem, { kind: "question" }>).answered = true;
        lastQuestionIndex = -1;
      }
      mapped.push({ kind: "message", role: "user", content: message.content ?? "" });
      continue;
    }
    if (message.role === "assistant") {
      const toolCallsThisMessage = (message.toolCalls ?? []).length;
      for (const call of message.toolCalls ?? []) {
        if (call.name === "ask_user") {
          const parsed = parseAskUserQuestion(call.args);
          if (parsed) {
            lastQuestionIndex = mapped.length;
            mapped.push({
              kind: "question",
              question: parsed.question,
              options: parsed.options,
              answered: false,
            });
          }
          continue;
        }
        pendingResults.set(call.id, { index: mapped.length });
        mapped.push({ kind: "tool", name: call.name, args: call.args, running: false, open: false });
      }
      if (message.content) {
        // The assistant narrates BEFORE calling a tool (live streaming shows
        // text, then the chip). Replaying chips first inverted that order, so
        // the transcript read differently after a reload. The narration was
        // pushed first here for the same reason.
        mapped.splice(mapped.length - toolCallsThisMessage, 0, {
          kind: "message",
          role: "assistant",
          content: message.content,
        });
        lastQuestionIndex = -1;
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
  if (streamFlushTimer) {
    clearTimeout(streamFlushTimer);
    streamFlushTimer = null;
  }
  if (pickerDebounce) clearTimeout(pickerDebounce);
});

async function resetChat(): Promise<void> {
  if (streaming.value) return;
  try {
    await api.agent.resetSession();
  } catch (error) {
    // Clearing sessionId locally while the server still holds the session used
    // to resurrect it: the next send omits the id, the backend falls back to
    // the latest session, and the transcript the user just discarded comes
    // back. Report the failure instead of pretending the reset worked.
    const detail =
      error instanceof Error && error.message
        ? error.message
        : "no se pudo reiniciar el chat";
    items.value = [{ kind: "error", content: detail }];
    return;
  }
  sessionId.value = "";
  selectedNovel.value = null;
  items.value = [];
}

function send(): void {
  void sendMessage(draft.value.trim());
}

async function chooseOption(
  item: Extract<ChatItem, { kind: "question" }>,
  option: AgentChatOption,
): Promise<void> {
  if (item.answered || streaming.value) return;
  item.answered = true;
  await sendMessage(option.value);
}

async function sendMessage(message: string): Promise<void> {
  if (!message || streaming.value) return;

  draft.value = "";
  items.value.push({ kind: "message", role: "user", content: message });
  scrollToBottom();

  streaming.value = true;
  abortController = new AbortController();

  let assistantText = "";
  // Text already committed to the transcript for the step in progress. A
  // terminal step (ask_user) is not the final answer, so the next user reply
  // starts a new assistant bubble instead of overwriting this one.
  let committedText = "";
  let sawDone = false;
  let sawError = false;

  const commitAssistant = (): void => {
    committedText = assistantText;
    if (!assistantText) return;
    const last = items.value[items.value.length - 1];
    // Reuse an empty assistant bubble we just created, never overwrite one
    // that already holds text.
    if (last && last.kind === "message" && last.role === "assistant" && !last.content) {
      last.content = assistantText;
      return;
    }
    items.value.push({ kind: "message", role: "assistant", content: assistantText });
  };

  const handleEvent = (event: AgentChatEvent): void => {
    switch (event.type) {
      case "session":
        if (event.sessionId) sessionId.value = event.sessionId;
        break;
      case "text_delta": {
        assistantText += event.text ?? "";
        scheduleStreamFlush(() => {
          if (assistantText === committedText) return;
          const last = items.value[items.value.length - 1];
          if (last && last.kind === "message" && last.role === "assistant") {
            last.content = assistantText;
          } else {
            items.value.push({ kind: "message", role: "assistant", content: assistantText });
          }
          committedText = assistantText;
          scrollToBottom();
        });
        break;
      }
      case "tool_call": {
        flushStreamNow(commitAssistant);
        assistantText = "";
        committedText = "";
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
      case "question": {
        flushStreamNow(commitAssistant);
        assistantText = "";
        committedText = "";
        items.value.push({
          kind: "question",
          question: event.question ?? "",
          options: event.options ?? [],
          answered: false,
        });
        scrollToBottom();
        break;
      }
      case "done": {
        sawDone = true;
        // The final answer replaces the in-progress bubble; a terminal step
        // sends an empty content, in which case what streamed stays.
        flushStreamNow(() => {
          const finalText = event.message?.content || assistantText;
          const last = items.value[items.value.length - 1];
          if (finalText && last && last.kind === "message" && last.role === "assistant") {
            last.content = finalText;
          } else if (finalText) {
            items.value.push({ kind: "message", role: "assistant", content: finalText });
          }
          committedText = finalText;
          assistantText = finalText;
        });
        settleRunningTools();
        sessionId.value = event.sessionId ?? sessionId.value;
        streaming.value = false;
        scrollToBottom();
        break;
      }
      case "error": {
        sawError = true;
        flushStreamNow(commitAssistant);
        const detail = event.error || event.code || "error del servidor";
        items.value.push({ kind: "error", content: detail });
        settleRunningTools();
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
    // An intentional stop is not a failure: keep whatever partial answer
    // already streamed in and stay silent.
    if (!abortController?.signal.aborted) {
      flushStreamNow(commitAssistant);
      const detail =
        error instanceof Error && error.message
          ? error.message
          : "no se pudo contactar al servidor";
      items.value.push({ kind: "error", content: detail });
    }
  } finally {
    flushStreamNow(commitAssistant);
    // A stream that ends without a done event was cut off (proxy timeout,
    // dropped connection). Without this the partial answer is left on screen
    // looking exactly like a finished one.
    if (!sawDone && !sawError && !abortController?.signal.aborted) {
      items.value.push({
        kind: "error",
        content: "La respuesta se cortó a mitad. Vuelve a intentarlo.",
      });
    }
    settleRunningTools();
    streaming.value = false;
    abortController = null;
    scrollToBottom();
  }
}

let pickerDebounce: ReturnType<typeof setTimeout> | null = null;

// The debounced search is driven by watching the query. It used to exist with
// no caller at all — the input had no @input/@watch — so the modal always
// rendered "Sin resultados." and picking a novel to ask about was impossible.
watch(pickerQuery, () => searchPicker());

// Opening the picker with an empty query should already show something
// pickable, so a click on the button is never a dead end.
watch(pickerOpen, (open) => {
  if (open && pickerResults.value.length === 0) searchPicker();
});

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
  // The selected novel rides along as the request's novelId on every turn, so
  // the assistant treats it as the default subject. It is a context marker, not
  // something written into the transcript.
  selectedNovel.value = { id: novel.id, title: novelTitle(novel) };
  pickerOpen.value = false;
  pickerQuery.value = "";
  pickerResults.value = [];
}
</script>

<style scoped>
/* The transcript owns the leftover viewport height instead of pinning a
   480px floor: on a short viewport (landscape phone) a min-height would push
   the composer below the fold while the transcript still refuses to scroll.
   The header offset is the app-topbar's min-height; it lives in one place so
   the desktop and mobile values can be corrected side by side. */
.chat-page {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
  height: calc(100dvh - 56px - 2.5rem);
  min-height: 0;
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

/* Reset the UA's 1em block margin: as written it opened ~32px of dead air
   above and below the subtitle. */
.page-context p {
  margin: 0.25rem 0 0;
  line-height: 1.4;
}

.chat-scroll {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
  padding: 0.25rem 0.125rem 0.5rem;
  overscroll-behavior: contain;
}

.chat-empty {
  margin: auto;
  width: 100%;
  text-align: center;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 0.5rem;
  padding: 2rem 1rem;
}

.chat-empty-icon {
  color: var(--text-tertiary);
}

.chat-empty-title {
  margin: 0;
  font-size: 1.125rem;
  font-weight: 600;
}

/* Keep the intro copy inside a readable measure even though the column
   widened, so it does not run to the full edge on wide screens. */
.chat-empty p {
  max-width: 34rem;
  margin: 0;
  line-height: 1.55;
}

.chat-suggestions {
  display: flex;
  flex-direction: column;
  gap: 0.375rem;
  width: 100%;
  max-width: 34rem;
  margin-top: 0.75rem;
}

.chat-suggestion {
  border: 1px solid var(--divide);
  background: var(--surface-elevated);
  border-radius: var(--radius-md);
  padding: 0.625rem 0.75rem;
  font-size: 0.875rem;
  color: var(--foreground);
  text-align: left;
  cursor: pointer;
  transition: background-color 0.15s ease-out, border-color 0.15s ease-out;
}

.chat-suggestion:hover:not(:disabled) {
  background: var(--mock-row);
  border-color: var(--border-strong);
}

.chat-suggestion:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.chat-options {
  display: flex;
  flex-direction: column;
  gap: 0.375rem;
  margin-top: 0.5rem;
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
  max-width: 100%;
  border-radius: var(--radius-lg);
  padding: 0.625rem 0.875rem;
  font-size: 0.9375rem;
  line-height: 1.55;
  overflow-wrap: anywhere;
}

/* User turns stay narrower than assistant turns: they are typically a question
   or a short instruction, and the asymmetry is what makes the thread readable
   without alternating heavy blocks. */
.chat-bubble--user {
  max-width: min(38rem, 82%);
  background: var(--btn-primary-bg);
  color: var(--btn-primary-fg);
  white-space: pre-wrap;
}

.chat-bubble--assistant {
  background: var(--surface-elevated);
  border: 1px solid var(--divide);
}

/* Reset the UA's 1em block margin on the plain-text path. This was scoped to
   assistant bubbles only, so user turns carried a stray gap inside the bubble. */
.chat-plain {
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
  font-family: "SFMono-Regular", ui-monospace, Menlo, Monaco, Consolas, monospace;
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
  max-width: 100%;
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
  color: var(--text-tertiary);
}

.chat-tool-name {
  font-size: 0.75rem;
  color: var(--text-secondary);
}

/* A real <button> so the 44px touch target and keyboard focus come for free;
   n-button's tiny variant collapsed to well under both. */
.chat-tool-toggle {
  margin-left: auto;
  min-height: 44px;
  padding: 0 0.5rem;
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  font: inherit;
  font-size: 0.75rem;
  color: var(--accent-link);
  cursor: pointer;
  transition: background-color 0.15s ease-out;
}

.chat-tool-toggle:hover {
  background: var(--mock-row);
  color: var(--accent-link-hover);
}

.chat-tool-result {
  flex-basis: 100%;
  margin: 0.25rem 0 0;
  max-height: 12rem;
  overflow: auto;
  white-space: pre-wrap;
  font-family: "SFMono-Regular", ui-monospace, Menlo, Monaco, Consolas, monospace;
  font-size: 0.75rem;
  color: var(--text-secondary);
  background: var(--page-bg);
  border-radius: var(--radius-sm);
  padding: 0.5rem;
}

.chat-error {
  max-width: 100%;
  border: 1px solid var(--danger);
  color: var(--danger);
  border-radius: var(--radius-md);
  padding: 0.625rem 0.75rem;
  font-size: 0.875rem;
  line-height: 1.5;
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
  align-items: flex-end;
  gap: 0.5rem;
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
  min-height: 44px;
  justify-content: center;
  border: 1px solid transparent;
  background: transparent;
  border-radius: var(--radius-md);
  padding: 0.5rem 0.625rem;
  cursor: pointer;
  text-align: left;
  transition: background-color 0.15s ease-out, border-color 0.15s ease-out;
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
    height: calc(100dvh - 52px - 2rem);
  }

  .page-title {
    font-size: 1.5rem;
  }

  /* Narrow viewports: the measure cap has to yield so bubbles use the width
     they actually have. User turns keep the tighter share. */
  .chat-bubble--user {
    max-width: 88%;
  }

  /* 44px minimum touch target for every tappable chip. */
  .chat-suggestion {
    min-height: 44px;
    display: flex;
    align-items: center;
  }
}

@media (prefers-reduced-motion: reduce) {
  .chat-suggestion,
  .chat-tool-toggle,
  .chat-picker-item {
    transition: none;
  }

  .chat-pending-dot {
    animation: none;
    opacity: 0.7;
  }
}
</style>
