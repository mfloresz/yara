<template>
  <AppLayout>
    <div class="chat-page">
      <header class="page-header">
        <!-- Everything in the header sits at the right edge: status ring first,
             then the action button, so the corner keeps one reading order. -->
        <div class="page-actions">
          <!-- Context usage ring, always present (even on a fresh chat, where
               it rests at 0): percentage of the serving model's window held at
               answer time (prompt tokens of the last step). Hover shows Uso +
               Tokens only — never cost. Persisted per session in localStorage,
               so reloading the page does not lose the reading until the next
               message. -->
          <n-popover trigger="hover" placement="bottom-end">
            <template #trigger>
              <span class="chat-usage-ring" role="img" :aria-label="usageTitle">
                <n-progress
                  type="circle"
                  :percentage="usagePercent"
                  :show-indicator="false"
                  :stroke-width="12"
                  :offset-degree="120"
                  :color="usageRingColor"
                  :rail-color="usageRingRail"
                />
              </span>
            </template>
            <div class="chat-usage-pop">
              <div class="chat-usage-row"><span>Uso</span><strong>{{ usagePercentLabel }}</strong></div>
              <div class="chat-usage-row"><span>Tokens</span><strong>{{ usageTokensLabel }}</strong></div>
              <div v-if="lastModel" class="muted small chat-usage-model">{{ lastModel }}</div>
            </div>
          </n-popover>
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

      <!-- Split layout: transcript on the left, the active artifact (cleanup
           proposal preview) as a side panel on the right, like classic AI
           artifacts. On narrow screens the panel becomes a full overlay. -->
      <div class="chat-body">
        <!-- role="log" announces appended messages instead of re-reading the
                 whole transcript. Only additions are announced: streaming text
                 mutates existing nodes (stays silent) and the pending bubble
                 below carries its own status. Tool rows are deliberately NOT
                 live regions — their duration ticks would re-announce. -->
            <n-scrollbar
              ref="scrollContainer"
              class="chat-scroll"
              content-class="chat-scroll-content"
            >
              <div
                class="chat-scroll-inner"
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

        <!-- A turn is the visual unit: the user's question (right-aligned
             bubble) plus everything the agent did about it, wrapped in ONE
             left-aligned assistant bubble — narration, step rows and the
             final answer share the container, which closes when the turn
             ends and the user writes again. -->
        <div v-for="turn in turns" :key="turn.id" class="turn">
          <div v-if="turn.user" class="turn-user">
            <div class="chat-bubble chat-bubble--user">{{ turn.user.content }}</div>
          </div>
          <div
            v-if="turn.agent.length > 0 || (turn.active && waitingForFirstToken)"
            class="turn-bubble"
          >
            <!-- Finished turns with several steps collapse to one summary
                 line so long transcripts stay scannable; the final answer,
                 questions and proposals are always kept visible. -->
            <button
              v-if="!turn.active && turn.toolCount >= 3"
              type="button"
              class="turn-steps"
              :aria-expanded="!isTurnCollapsed(turn)"
              @click="toggleTurn(turn.id)"
            >
              <n-icon :size="12" aria-hidden="true">
                <ChevronForwardOutline v-if="isTurnCollapsed(turn)" />
                <ChevronDownOutline v-else />
              </n-icon>
              <template v-if="isTurnCollapsed(turn)">
                {{ turn.toolCount }} pasos · {{ formatDuration(turn.toolMs) }}
              </template>
              <template v-else>
                Ocultar los {{ turn.toolCount }} pasos
              </template>
            </button>

            <!-- v-memo + stable keys: only the streaming tail (and whatever
                 the events actually touched) re-renders on each frame; the
                 memo is keyed per item kind so a ticking tool duration costs
                 one row, not the transcript. -->
            <div
              v-for="item in turnVisibleItems(turn)"
              :key="item.id"
              class="turn-item"
              v-memo="itemDeps(item)"
            >
              <div
                v-if="item.kind === 'message'"
                class="turn-text chat-markdown"
                :class="{ 'chat-markdown--streaming': item.id === tailId }"
                :aria-live="item.id === tailId ? 'off' : undefined"
              >
                <!-- markstream-vue parses incrementally and keeps partial
                     markdown stable, so an unterminated heading or code fence
                     does not flicker while the answer is still arriving.
                     htmlPolicy="escape" matters here: this is LLM output, a
                     lower-trust source than the novel text markdownToHtml
                     was written for. The caret is a CSS ::after on the last
                     rendered block — no DOM measurement per token. -->
                <MarkdownRender
                  :content="item.content"
                  :final="item.id !== tailId"
                  html-policy="escape"
                />
                <!-- Action bar under the turn's final answer, chatbot-style:
                     copy the answer, or regenerate the turn (replaces the
                     answer in place, no new user bubble). Shown once the
                     turn is over (never on the streaming tail) and only on the
                     last message of the turn, so narration between steps does
                     not grow buttons. It must live INSIDE this branch: an
                     element between the v-if and the v-else-if below would
                     break Vue's conditional chain. -->
                <div
                  v-if="item.role === 'assistant' && isTurnFinalMessage(turn, item)"
                  class="turn-actions"
                >
                  <n-button
                    quaternary
                    size="tiny"
                    class="turn-action"
                    :aria-label="copiedId === item.id ? 'Copiado' : 'Copiar respuesta'"
                    @click="copyMessage(item)"
                  >
                    <template #icon>
                      <n-icon :size="15">
                        <CheckmarkOutline v-if="copiedId === item.id" />
                        <CopyOutline v-else />
                      </n-icon>
                    </template>
                  </n-button>
                  <!-- Regenerate only on the newest turn: the backend truncates
                       the session at the last user message, so re-running an
                       older answer would erase every turn after it. -->
                  <n-button
                    v-if="isLastTurn(turn)"
                    quaternary
                    size="tiny"
                    class="turn-action"
                    aria-label="Regenerar respuesta"
                    :disabled="streaming || !turn.user"
                    @click="regenerateTurn(turn)"
                  >
                    <template #icon><n-icon :size="15"><RefreshOutline /></n-icon></template>
                  </n-button>
                </div>
              </div>

              <div v-else-if="item.kind === 'tool'" class="tool-row" :class="`tool-row--${item.status}`">
                <!-- Steps stay in the transcript (collapsed to one line, like
                     coding agents do) instead of being spliced out on
                     tool_result: the user can audit what the agent ran.
                     Disabled (not hidden) when there is no result to show. -->
                <button
                  type="button"
                  class="tool-row-main"
                  :disabled="!item.detail"
                  :aria-expanded="!!item.open"
                  @click="toggleTool(item)"
                >
                  <span class="tool-row-state" aria-hidden="true">
                    <span v-if="item.status === 'running'" class="tool-row-spinner"></span>
                    <n-icon v-else-if="item.status === 'done'" :size="13"><CheckmarkOutline /></n-icon>
                    <n-icon v-else-if="item.status === 'error'" :size="13"><CloseOutline /></n-icon>
                    <n-icon v-else :size="13"><StopOutline /></n-icon>
                  </span>
                  <span class="tool-row-name">{{ toolLabel(item.name) }}</span>
                  <span v-if="toolArgsHint(item.args)" class="tool-row-args">{{ toolArgsHint(item.args) }}</span>
                  <span v-if="item.summary" class="tool-row-summary">· {{ item.summary }}</span>
                  <span v-if="toolTimeLabel(item)" class="tool-row-time">{{ toolTimeLabel(item) }}</span>
                  <span v-if="item.detail" class="tool-row-chevron" aria-hidden="true">{{ item.open ? "▾" : "▸" }}</span>
                </button>
                <pre v-if="item.detail && item.open" class="tool-row-detail">{{ item.detail }}</pre>
              </div>

              <div v-else-if="item.kind === 'question'" class="turn-question">
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

              <div v-else-if="item.kind === 'proposal'">
                <CleanupProposalCard
                  :proposal="item.proposal"
                  :resolved="item.resolved"
                  :outcome="item.outcome"
                  @open="activeProposal = item"
                />
              </div>

              <div v-else class="chat-error">
                <p class="chat-error-text">{{ item.content }}</p>
                <n-button
                  v-if="item.retry"
                  size="tiny"
                  secondary
                  class="chat-error-retry"
                  :disabled="streaming"
                  @click="retryTurn(item)"
                >
                  Reintentar
                </n-button>
              </div>
            </div>

            <div v-if="turn.active && waitingForFirstToken" class="turn-pending" role="status">
              <span class="chat-pending-dot" aria-hidden="true"></span>
              <span class="muted small">Pensando…<template v-if="thinkingSeconds > 0"> {{ thinkingSeconds }} s</template></span>
            </div>
          </div>
        </div>
        </div>
            </n-scrollbar>

        <aside v-if="activeProposal" class="chat-artifact" aria-label="Vista previa de limpieza">
          <CleanupProposalPanel
            :proposal="activeProposal.proposal"
            :resolved="activeProposal.resolved"
            :outcome="activeProposal.outcome"
            :busy="streaming"
            @applied="(result) => onProposalApplied(activeProposal, result)"
            @discarded="onProposalDiscarded(activeProposal)"
            @close="activeProposal = null"
          />
        </aside>
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
            class="touch-target chat-attach-trigger"
            aria-label="Seleccionar novela sobre la que preguntar"
            @click="pickerOpen = true"
          >
            <template #icon><n-icon><BookOutline /></n-icon></template>
          </n-button>
          <div class="chat-input-shell">
            <n-input
              v-model:value="draft"
              type="textarea"
              :autosize="{ minRows: 1, maxRows: 6 }"
              :bordered="false"
              placeholder="Escribe una pregunta o continúa el análisis…"
              class="chat-input"
              @keydown.enter.exact.prevent="send"
            />
            <div class="chat-input-actions">
              <n-select
                v-model:value="mediaScope"
                size="small"
                :options="mediaOptions"
                class="chat-media-select"
                aria-label="Tipo de contenido del asistente"
                @update:value="onMediaChange"
              />
              <!-- Keep provider/model selection attached to the text field. -->
              <n-dropdown
                trigger="click"
                placement="top-end"
                :options="modelMenuOptions"
                :loading="providersLoading"
                @select="onModelMenuSelect"
              >
                <n-button
                  secondary
                  size="small"
                  class="chat-model-trigger"
                  aria-label="Modelo del asistente"
                  :title="modelTriggerTitle"
                >
                  <span class="chat-model-trigger-label">{{ modelTriggerLabel }}</span>
                  <template #icon><n-icon><ChevronDownOutline /></n-icon></template>
                </n-button>
              </n-dropdown>
              <span v-if="agentKeyMissing" class="muted small chat-model-warn" title="Configura la API key en Ajustes">
                Sin API key
              </span>
            </div>
          </div>
          <n-button
            v-if="streaming"
            circle
            class="touch-target chat-send-button"
            aria-label="Detener la respuesta"
            @click="stopStreaming"
          >
            <template #icon><n-icon><StopOutline /></n-icon></template>
          </n-button>
          <n-button
            v-else
            type="primary"
            circle
            class="touch-target chat-send-button"
            aria-label="Enviar mensaje"
            :disabled="!canSend"
            @click="send"
          >
            <template #icon><n-icon><SendOutline /></n-icon></template>
          </n-button>
        </div>
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
import type { DropdownOption } from "naive-ui";
import {
  NButton,
  NDropdown,
  NIcon,
  NInput,
  NSelect,
  NModal,
  NPopover,
  NProgress,
  NScrollbar,
  NSpin,
  useMessage,
} from "naive-ui";
import {
  BookOutline,
  ChatbubbleEllipsesOutline,
  CheckmarkOutline,
  ChevronDownOutline,
  ChevronForwardOutline,
  CloseOutline,
  CopyOutline,
  RefreshOutline,
  SendOutline,
  StopOutline,
} from "@vicons/ionicons5";
import AppLayout from "@/components/AppLayout.vue";
import CleanupProposalCard from "@/components/CleanupProposalCard.vue";
import CleanupProposalPanel from "@/components/CleanupProposalPanel.vue";
import { useAppServices } from "@/app/services";
import { useProviders } from "@/composables/useProviders";
import { MarkdownRender } from "markstream-vue";
import "markstream-vue/index.css";
import type { AgentChatEvent, AgentChatOption, AgentChatUsage, AgentCleanupProposal, AgentSessionMessage } from "@/api/types";
import type { ServerSettings } from "@/api/types";
import type { Novel } from "@/domain";

// Stable identity for transcript items. Every key in the v-for must survive
// the in-place updates of a live turn (tool rows flipping status, the tail
// receiving deltas), so Vue patches only what actually changed. A monotonic
// counter is enough: ids only need to be unique within one page session.
let lastItemId = 0;
function uid(): string {
  lastItemId += 1;
  return `item-${lastItemId}`;
}

type ToolStatus = "running" | "done" | "error" | "cancelled";

type ToolChatItem = {
  id: string;
  kind: "tool";
  name: string;
  args?: string;
  status: ToolStatus;
  startedAt: number;
  finishedAt?: number;
  summary?: string;
  detail?: string;
  open?: boolean;
};

type ProposalChatItem = {
  id: string;
  kind: "proposal";
  proposal: AgentCleanupProposal;
  resolved: boolean;
  outcome?: "applied" | "discarded";
};

type ChatItem =
  | { id: string; kind: "message"; role: "user"; content: string }
  | { id: string; kind: "message"; role: "assistant"; content: string }
  | ToolChatItem
  | { id: string; kind: "question"; question: string; options: AgentChatOption[]; answered: boolean }
  | ProposalChatItem
  // retryRegenerate marks an error row that came from a regenerate: its retry
  // must re-run the turn the same way (drop answer, re-ask), not append a
  // second copy of the user's message.
  | { id: string; kind: "error"; content: string; retry?: string; retryRegenerate?: boolean };

type UserChatItem = Extract<ChatItem, { kind: "message"; role: "user" }>;

// A turn is one user message plus everything the agent did about it. Derived
// from the flat item list (the mutation code keeps operating on `items`), so
// persistence, history replay and the live stream share one representation.
type Turn = {
  id: string;
  active: boolean;
  user: UserChatItem | null;
  agent: ChatItem[];
  toolCount: number;
  toolMs: number;
};

type CleanApplyResult = {
  modified: number;
  total: number;
  skipped: number;
  notFound: number;
  failed: number;
};

type PickerNovel = Pick<
  Novel,
  "id" | "sourceTitle" | "targetTitle" | "sourceAuthor" | "targetAuthor"
>;

const { api } = useAppServices();
const message = useMessage();
const { providers, byId, loading: providersLoading, reload: reloadProviders } = useProviders();

const items = ref<ChatItem[]>([]);
const draft = ref("");

// Media scoping of the assistant toolset (default: novels). Persisted so the
// choice survives navigation; "both" loads every tool schema into the system
// prompt, which is why it is opt-in.
const mediaOptions = [
  { label: "Novelas", value: "novels" },
  { label: "Cómics", value: "comics" },
  { label: "Ambos", value: "both" },
];
const mediaScope = ref<"novels" | "comics" | "both">(
  ((): "novels" | "comics" | "both" => {
    const saved = localStorage.getItem("yara-chat-media");
    return saved === "comics" || saved === "both" ? saved : "novels";
  })(),
);
function onMediaChange(value: string) {
  mediaScope.value = (value as typeof mediaScope.value);
  localStorage.setItem("yara-chat-media", mediaScope.value);
}
const streaming = ref(false);
const sessionId = ref("");
const lastUsage = ref<AgentChatUsage | null>(null);
const lastModel = ref("");
const lastContextWindow = ref<number | null>(null);
const selectedNovel = ref<{ id: string; title: string } | null>(null);
const scrollContainer = ref<InstanceType<typeof NScrollbar> | null>(null);
// Assistant model selection, independent from the translation provider.
// Empty agentProvider follows the translation provider; empty agentModel
// uses the effective provider's default. Persisted in settings.
const settings = ref<ServerSettings | null>(null);
const agentProvider = ref("");
const agentModel = ref("");
const agentSettingsReady = ref(false);
const savingAgentSelection = ref(false);
// The proposal currently shown in the artifact side panel. It references the
// same reactive item as the transcript, so a decision made in the panel
// updates the chip (and vice versa) with no extra wiring.
const activeProposal = ref<ProposalChatItem | null>(null);

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

// Token usage of the last finished turn, as reported by the provider via the
// `done` event. Shown as a quiet header ring; absent (ring rests at 0) when
// the provider reported no usage. Prompt is the last step's input size
// (context held at answer time); completion/total accumulate across the
// turn's steps.

const usageTitle = computed(() => {
  const u = lastUsage.value;
  if (!u) return "Uso del contexto sin datos todavía";
  const parts = [
    `${u.promptTokens.toLocaleString("es")} tokens de entrada (contexto del último paso)`,
    `${u.completionTokens.toLocaleString("es")} de salida`,
    `${u.totalTokens.toLocaleString("es")} en total en este turno`,
  ];
  if (u.cachedTokens) parts.push(`${u.cachedTokens.toLocaleString("es")} de caché`);
  if (u.reasoningTokens) parts.push(`${u.reasoningTokens.toLocaleString("es")} de razonamiento`);
  return parts.join(" · ");
});

// Percentage of the serving model's context window held at answer time.
// The ring is always present, so it rests at 0 when the provider reported no
// usage or the window is unknown.
const usagePercent = computed(() => {
  const u = lastUsage.value;
  const window = lastContextWindow.value;
  if (!u || !window || window <= 0) return 0;
  const pct = (u.promptTokens / window) * 100;
  if (!Number.isFinite(pct) || pct < 0) return 0;
  return Math.min(100, Math.round(pct * 10) / 10);
});

const usagePercentLabel = computed(() => {
  if (!lastUsage.value) return "—";
  return `${usagePercent.value.toLocaleString("es", { maximumFractionDigits: 1 })} %`;
});

const usageTokensLabel = computed(() => {
  const u = lastUsage.value;
  if (!u) return "—";
  return u.promptTokens.toLocaleString("es");
});

// Ring palette: the one accent of the warm-stone ramp (aged stone) at rest,
// shifting to the semantic warn/danger steps as the window fills — color as
// state signal only, per DESIGN.md, never the library blue.
const usageRingColor = computed(() => {
  const pct = usagePercent.value;
  if (pct >= 90) return "var(--danger)";
  if (pct >= 70) return "var(--warning)";
  return "var(--accent-link)";
});

const usageRingRail = computed(() => {
  const pct = usagePercent.value;
  if (pct >= 90) return "color-mix(in oklab, var(--danger) 18%, transparent)";
  if (pct >= 70) return "color-mix(in oklab, var(--warning) 18%, transparent)";
  return "color-mix(in oklab, var(--accent-link) 15%, transparent)";
});

// The usage reading is display state, not conversation data: it is mirrored
// into localStorage so reloading the page restores the ring immediately
// instead of waiting for the next turn's `done` event. Cleared on reset.
const USAGE_STORAGE_KEY = "yara.chat.usage";

type PersistedUsage = {
  usage: AgentChatUsage;
  model: string;
  contextWindow: number;
};

function persistUsage(): void {
  try {
    if (!lastUsage.value) return;
    const payload: PersistedUsage = {
      usage: lastUsage.value,
      model: lastModel.value,
      contextWindow: lastContextWindow.value ?? 0,
    };
    localStorage.setItem(USAGE_STORAGE_KEY, JSON.stringify(payload));
  } catch {
    // Storage full or blocked (private mode): the ring just starts empty.
  }
}

function loadPersistedUsage(): void {
  try {
    const raw = localStorage.getItem(USAGE_STORAGE_KEY);
    if (!raw) return;
    const parsed = JSON.parse(raw) as Partial<PersistedUsage>;
    if (!parsed.usage || typeof parsed.usage.promptTokens !== "number") return;
    lastUsage.value = parsed.usage;
    lastModel.value = parsed.model ?? "";
    lastContextWindow.value =
      typeof parsed.contextWindow === "number" && parsed.contextWindow > 0
        ? parsed.contextWindow
        : null;
  } catch {
    // Corrupt or unavailable storage: start without a reading.
  }
}

function clearPersistedUsage(): void {
  try {
    localStorage.removeItem(USAGE_STORAGE_KEY);
  } catch {
    // Nothing to recover from; the in-memory values are cleared regardless.
  }
}

// The last assistant message is the one still receiving token deltas: it is
// the only item rendered "live" (non-final markdown, caret, no aria-live).
const tailId = computed(() => {
  if (!streaming.value) return "";
  const last = items.value[items.value.length - 1];
  if (last?.kind !== "message" || last.role !== "assistant") return "";
  return last.id;
});

// ---------------------------------------------------------------------------
// Turn grouping (derived view over `items`)
// ---------------------------------------------------------------------------

const turns = computed<Turn[]>(() => {
  const list: Turn[] = [];
  let current: Turn | null = null;
  for (const item of items.value) {
    if (item.kind === "message" && item.role === "user") {
      current = { id: item.id, active: false, user: item, agent: [], toolCount: 0, toolMs: 0 };
      list.push(current);
      continue;
    }
    if (!current) {
      // Defensive: a trail whose first stored message is an assistant one.
      current = { id: `turn-${item.id}`, active: false, user: null, agent: [], toolCount: 0, toolMs: 0 };
      list.push(current);
    }
    current.agent.push(item);
    if (item.kind === "tool") {
      current.toolCount += 1;
      // Only finished steps contribute: the turn summary is a post-hoc stat.
      if (item.finishedAt && item.startedAt) {
        current.toolMs += item.finishedAt - item.startedAt;
      }
    }
  }
  if (list.length > 0) {
    list[list.length - 1].active = streaming.value;
  }
  return list;
});

// Turns the user explicitly re-expanded. A Set of ids, replaced on toggle so
// the computed views depending on it re-run.
const expandedTurns = ref<Set<string>>(new Set());

function isTurnCollapsed(turn: Turn): boolean {
  if (turn.active || turn.toolCount < 3) return false;
  return !expandedTurns.value.has(turn.id);
}

function toggleTurn(turnId: string): void {
  const next = new Set(expandedTurns.value);
  if (next.has(turnId)) {
    next.delete(turnId);
  } else {
    next.add(turnId);
  }
  expandedTurns.value = next;
}

// Collapsed turns keep the final answer, questions, proposals and errors
// visible; only the intermediate narration and the tool steps fold into the
// "N pasos" summary chip.
function turnVisibleItems(turn: Turn): ChatItem[] {
  if (!isTurnCollapsed(turn)) return turn.agent;
  const kept = turn.agent.filter((item) => item.kind !== "tool");
  let lastMessage = -1;
  for (let i = kept.length - 1; i >= 0; i -= 1) {
    if (kept[i].kind === "message") {
      lastMessage = i;
      break;
    }
  }
  return kept.filter((item, index) => item.kind !== "message" || index === lastMessage);
}

// Per-item v-memo dependencies. Scoped per kind so a ticking tool duration
// re-renders one row while the rest of the transcript stays cached, and an
// idle transcript re-renders nothing at all.
function itemDeps(item: ChatItem): unknown[] {
  switch (item.kind) {
    case "message":
      return [item.content, item.id === tailId.value, item.id === copiedId.value];
    case "tool":
      return [item.status, item.summary, item.detail, item.open, toolTimeLabel(item)];
    case "question":
      return [item.question, item.answered];
    case "proposal":
      return [item.resolved, item.outcome];
    default:
      return [item.content, streaming.value];
  }
}

// ---------------------------------------------------------------------------
// Final-answer actions (copy / regenerate)
// ---------------------------------------------------------------------------

// The action bar belongs to the LAST assistant message of a finished turn:
// the answer, not the narration between steps. `turn.active` covers the live
// turn (its last message is still the tail); the identity check covers turns
// that ended with a tool call, question or proposal after the last text.
function isTurnFinalMessage(turn: Turn, item: ChatItem): boolean {
  if (turn.active || streaming.value) return false;
  if (item.kind !== "message" || item.role !== "assistant") return false;
  return turn.agent[turn.agent.length - 1] === item;
}

// Regenerate rewrites the transcript's tail: the backend truncates the session
// to just before the LAST user message, so only the newest turn can be
// regenerated — re-running an older answer would erase every turn after it.
function isLastTurn(turn: Turn): boolean {
  const list = turns.value;
  return list.length > 0 && list[list.length - 1].id === turn.id;
}

// Icon feedback id plus its timer: the checkmark shows ~1.5s after a copy.
const copiedId = ref("");
let copyTimer: ReturnType<typeof setTimeout> | null = null;

async function copyMessage(item: ChatItem): Promise<void> {
  if (item.kind !== "message" || !item.content) return;
  try {
    await navigator.clipboard.writeText(item.content);
    copiedId.value = item.id;
    if (copyTimer) clearTimeout(copyTimer);
    copyTimer = setTimeout(() => {
      copiedId.value = "";
      copyTimer = null;
    }, 1500);
  } catch {
    message.error("No se pudo copiar la respuesta");
  }
}

// Regenerate rewrites the transcript's tail instead of appending a new turn:
// the last turn's agent-side items (steps, narration, the answer itself) are
// dropped from the local transcript and the SAME question is re-sent through
// the backend's `regenerate` path, which truncates the stored session to just
// before that user message and re-runs the turn. Net effect, exactly like
// keryx: the old answer is replaced, no second user bubble appears.
function regenerateTurn(turn: Turn): void {
  if (streaming.value || !turn.user || !isLastTurn(turn)) return;
  reaskLastTurn();
}

// The shared drop-and-re-run used by the regenerate button and by the retry
// on an error row that came from a regenerate. Everything after the last user
// message goes — including a restored previous answer and the error row —
// so the retry lands on a clean slate.
function reaskLastTurn(): void {
  const list = turns.value;
  const turn = list[list.length - 1];
  if (!turn || !turn.user || streaming.value) return;
  const userIdx = items.value.indexOf(turn.user);
  if (userIdx === -1) return;
  if (activeProposal.value && turn.agent.includes(activeProposal.value)) {
    activeProposal.value = null;
  }
  const removed = turn.agent.slice();
  items.value.splice(userIdx + 1, turn.agent.length);
  void sendMessage(turn.user.content, { regenerate: true, restoreItems: removed });
}

// ---------------------------------------------------------------------------
// Tool step presentation
// ---------------------------------------------------------------------------

const TOOL_SUMMARY_MAX = 72;
const TOOL_ARGS_MAX = 56;
const TOOL_DETAIL_MAX = 4000;

function shorten(text: string, max: number): string {
  const t = text.trim();
  return t.length <= max ? t : `${t.slice(0, max - 1)}…`;
}

// One-line result summary for a tool row. Tool results are JSON envelopes
// ({"returned": n}, {"hits": [...]}, {"ok": true} …) or an "error: …" string,
// so the summary reads the common keys and falls back to the first line.
function summarizeToolResult(raw: string): string {
  const text = (raw ?? "").trim();
  if (!text) return "";
  if (/^error:/i.test(text)) {
    return shorten(text.replace(/^error:\s*/i, ""), TOOL_SUMMARY_MAX);
  }
  try {
    const parsed = JSON.parse(text) as unknown;
    if (Array.isArray(parsed)) {
      return `${parsed.length} ${parsed.length === 1 ? "elemento" : "elementos"}`;
    }
    if (parsed && typeof parsed === "object") {
      const obj = parsed as Record<string, unknown>;
      if (typeof obj.returned === "number") return `${obj.returned} resultados`;
      if (typeof obj.total === "number") return `${obj.total} resultados`;
      if (Array.isArray(obj.hits)) return `${obj.hits.length} coincidencias`;
      if (obj.ok === false && typeof obj.error === "string") {
        return shorten(obj.error, TOOL_SUMMARY_MAX);
      }
      if (obj.ok === true) return "hecho";
    }
    return "";
  } catch {
    // Not JSON — a plain sentence, take its first line below.
  }
  return shorten(text.split("\n")[0] ?? "", TOOL_SUMMARY_MAX);
}

// Compact preview of the call arguments, coding-agent style
// ("Título: X · Modo: regex"). Objects are trimmed to their first entries.
function toolArgsHint(args?: string): string {
  const raw = (args ?? "").trim();
  if (!raw) return "";
  try {
    const parsed = JSON.parse(raw) as unknown;
    if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
      const entries = Object.entries(parsed as Record<string, unknown>)
        .filter(([, v]) => v !== null && v !== undefined && v !== "")
        .slice(0, 2)
        .map(([k, v]) => `${k}: ${typeof v === "object" ? "…" : String(v)}`);
      if (entries.length > 0) return shorten(entries.join(" · "), TOOL_ARGS_MAX);
      return "";
    }
  } catch {
    // Not JSON — fall through to the raw preview.
  }
  return shorten(raw, TOOL_ARGS_MAX);
}

function formatDuration(ms: number): string {
  if (!Number.isFinite(ms) || ms <= 0) return "";
  if (ms < 1000) return `${Math.max(1, Math.round(ms))} ms`;
  const s = ms / 1000;
  return `${s < 10 ? s.toFixed(1) : Math.round(s)} s`;
}

function toolTimeLabel(item: ToolChatItem): string {
  if (item.status === "running") {
    const elapsed = now.value - item.startedAt;
    return elapsed < 1000 ? "" : formatDuration(elapsed);
  }
  // Replayed-from-history steps carry no timing.
  if (!item.startedAt || !item.finishedAt) return "";
  return formatDuration(item.finishedAt - item.startedAt);
}

function toggleTool(item: ToolChatItem): void {
  if (!item.detail) return;
  item.open = !item.open;
}

// ---------------------------------------------------------------------------
// Clock: drives the running-step duration and the "Pensando… Ns" counter.
// Ticked once per second and only while a turn is in flight.
// ---------------------------------------------------------------------------

const now = ref(Date.now());
let nowTimer: ReturnType<typeof setInterval> | null = null;

function startTicker(): void {
  if (nowTimer) return;
  nowTimer = setInterval(() => {
    now.value = Date.now();
  }, 1000);
}

function stopTicker(): void {
  if (nowTimer) {
    clearInterval(nowTimer);
    nowTimer = null;
  }
}

let turnStartedAt = 0;
const thinkingSeconds = computed(() => {
  if (!waitingForFirstToken.value) return 0;
  return Math.max(0, Math.floor((now.value - turnStartedAt) / 1000));
});

function novelTitle(novel: PickerNovel): string {
  return novel.targetTitle || novel.sourceTitle || "(sin título)";
}

function novelAuthor(novel: PickerNovel): string {
  return novel.targetAuthor || novel.sourceAuthor || "";
}

// Effective provider driving the model list and the key warning: the
// override when set, otherwise the translation provider.
const effectiveAgentProviderId = computed(
  () => agentProvider.value.trim() || settings.value?.ai.provider || "",
);

// Cascading menu: each provider opens a submenu with its models. The first
// item clears the override (follow the translation provider + its default
// model). Keys encode the choice: "default" resets, "provider:model" picks.
const MENU_DEFAULT_KEY = "default";

function modelMenuKey(providerId: string, model: string): string {
  return `${providerId}:${model}`;
}

function splitModelMenuKey(key: string): { providerId: string; model: string } | null {
  if (key === MENU_DEFAULT_KEY) return null;
  const sep = key.indexOf(":");
  if (sep <= 0) return null;
  return { providerId: key.slice(0, sep), model: key.slice(sep + 1) };
}

// Selected leaf: which provider/model is currently chosen, for the trigger
// label and the checkmark rendered on the active submenu item.
const selectedModelMenuKey = computed(() => {
  if (!agentProvider.value) return MENU_DEFAULT_KEY;
  return modelMenuKey(agentProvider.value, agentModel.value);
});

const modelMenuOptions = computed<DropdownOption[]>(() => {
  const current = selectedModelMenuKey.value;
  const options: DropdownOption[] = [
    {
      label: "Mismo que traducción",
      key: MENU_DEFAULT_KEY,
      checked: current === MENU_DEFAULT_KEY,
    },
  ];
  for (const p of providers.value) {
    const info = byId.value.get(p.id);
    const models = info?.models ?? [];
    if (models.length === 0) {
      options.push({
        label: p.name,
        key: modelMenuKey(p.id, ""),
        checked: current === modelMenuKey(p.id, ""),
      });
      continue;
    }
    options.push({
      label: p.name,
      key: `provider:${p.id}`,
      children: models.map((m) => ({
        label: m,
        key: modelMenuKey(p.id, m),
        checked: current === modelMenuKey(p.id, m),
      })),
    });
  }
  return options;
});

const modelTriggerLabel = computed(() => {
  if (agentModel.value) return agentModel.value;
  const info = byId.value.get(effectiveAgentProviderId.value);
  return info?.defaultModel || "Modelo por defecto";
});

const modelTriggerTitle = computed(() => {
  const info = byId.value.get(effectiveAgentProviderId.value);
  if (!info) return "Mismo que traducción";
  if (agentModel.value) return `${info.name} · ${agentModel.value}`;
  return `${info.name} · ${info.defaultModel || "modelo por defecto"}`;
});

function onModelMenuSelect(key: string): void {
  if (key === MENU_DEFAULT_KEY) {
    agentProvider.value = "";
    agentModel.value = "";
    void saveAgentSelection();
    return;
  }
  const picked = splitModelMenuKey(key);
  if (!picked) return;
  agentProvider.value = picked.providerId;
  agentModel.value = picked.model;
  void saveAgentSelection();
}

// A provider without a usable key (neither personal nor shared) fails the
// turn server-side; warn inline instead of letting the first message error.
const agentKeyMissing = computed(() => {
  const info = byId.value.get(effectiveAgentProviderId.value);
  if (!info) return false;
  return !info.apiKeyConfigured && !info.sharedKeyAvailable;
});

async function loadAgentSettings(): Promise<void> {
  try {
    const [s] = await Promise.all([api.settings.get(), reloadProviders()]);
    settings.value = s;
    agentProvider.value = s.agentProvider ?? "";
    agentModel.value = s.agentModel ?? "";
  } catch {
    // The selector falls back to the translation defaults; chat still works.
  } finally {
    agentSettingsReady.value = true;
  }
}

async function saveAgentSelection(): Promise<void> {
  if (!agentSettingsReady.value || !settings.value || savingAgentSelection.value) return;
  savingAgentSelection.value = true;
  try {
    settings.value = await api.settings.update({
      ...settings.value,
      agentProvider: agentProvider.value.trim(),
      agentModel: agentModel.value.trim(),
    });
  } catch (err) {
    message.error(
      `No se pudo guardar el modelo del asistente: ${err instanceof Error ? err.message : String(err)}`,
    );
  } finally {
    savingAgentSelection.value = false;
  }
}

// Streaming coalescing. Deltas arrive far faster than a frame, so mutating
// the reactive message on every one of them re-renders the whole bubble (and
// forces a layout for the scroll pin) dozens of times per second. Deltas are
// accumulated into a local string and the reactive update happens once per
// animation frame: rAF is aligned with the paint, so nothing renders into a
// frame the compositor is about to discard, and the text lands ~45 ms sooner
// than it did on the old 60 ms timer. Parsing itself is incremental:
// markstream-vue handles partial markdown instead of re-running marked over
// the whole message.
let streamRaf: number | null = null;
let streamDirty = false;

function scheduleStreamFlush(apply: () => void): void {
  streamDirty = true;
  if (streamRaf !== null) return;
  streamRaf = requestAnimationFrame(() => {
    streamRaf = null;
    if (!streamDirty) return;
    streamDirty = false;
    apply();
  });
}

function flushStreamNow(apply: () => void): void {
  if (streamRaf !== null) {
    cancelAnimationFrame(streamRaf);
    streamRaf = null;
  }
  streamDirty = false;
  apply();
}

// Distance from the bottom, in px, within which the transcript still counts
// as "pinned". Beyond this the user has deliberately scrolled up to re-read,
// so streaming must not drag them back down.
const STICK_THRESHOLD_PX = 64;

function isPinnedToBottom(): boolean {
  const container = (scrollContainer.value?.scrollbarInstRef as any)?.containerRef as HTMLElement | null | undefined;
  if (!container) return true;
  return container.scrollHeight - container.scrollTop - container.clientHeight <= STICK_THRESHOLD_PX;
}

// Call before mutating `items` so the pinned check reflects the pre-update
// geometry, then restore the pin once Vue has flushed the DOM.
function scrollToBottom(force = false): void {
  if (!force && !isPinnedToBottom()) return;
  void nextTick(() => {
    const container = (scrollContainer.value?.scrollbarInstRef as any)?.containerRef as HTMLElement | null | undefined;
    if (container) container.scrollTop = container.scrollHeight;
  });
}

function stopStreaming(): void {
  abortController?.abort();
  // Stop the UI immediately. Without this the button stays inert until the
  // pending read() rejects, which is bounded only by the server's per-turn
  // timeout — up to eight minutes of a frozen page after the user asked to
  // stop.
  if (streamRaf !== null) {
    cancelAnimationFrame(streamRaf);
    streamRaf = null;
  }
  streamDirty = false;
  streaming.value = false;
  settleRunningTools("cancelled");
  stopTicker();
}

// settleRunningTools closes any step left mid-flight. A turn can end without
// a tool_result (a mid-stream error, an abort, a malformed ask_user) and a
// row stuck on the spinner reads as a hang. The steps are kept — marked
// cancelled/errored — instead of removed, so the transcript stays auditable.
function settleRunningTools(status: ToolStatus): void {
  const finishedAt = Date.now();
  for (const item of items.value) {
    if (item.kind === "tool" && item.status === "running") {
      item.status = status;
      item.finishedAt = finishedAt;
    }
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

// parseAgentProposal rebuilds a persisted propose_cleanup call into an
// approval card. Only the rule and scope travel in the tool args; the card
// re-runs the live preview itself, so the dry-run counts are not needed here.
function parseAgentProposal(args?: string): AgentCleanupProposal | null {
  if (!args) return null;
  try {
    const p = JSON.parse(args) as Partial<AgentCleanupProposal>;
    if (!p.novelId?.trim() || !p.mode?.trim() || !p.applyTo?.trim()) return null;
    return {
      novelId: p.novelId,
      novelTitle: p.novelTitle ?? "",
      mode: p.mode,
      searchText: p.searchText,
      replaceText: p.replaceText,
      caseSensitive: !!p.caseSensitive,
      useRegex: !!p.useRegex,
      applyTo: p.applyTo,
      chapterIds: p.chapterIds,
      fromOrder: p.fromOrder,
      toOrder: p.toOrder,
      considered: p.considered ?? 0,
      affected: p.affected ?? 0,
      removedLines: p.removedLines ?? 0,
    };
  } catch {
    return null;
  }
}

function mapHistory(messages: AgentSessionMessage[]): void {
  const mapped: ChatItem[] = [];
  // Index of the last ask_user card, so the user's next message can mark it
  // answered instead of leaving a stale clickable card in the transcript.
  let lastQuestionIndex = -1;
  // Same idea for cleanup proposal cards: any subsequent user message means
  // the proposal was acted on or abandoned, so it renders closed after a
  // reload instead of offering buttons that no longer apply.
  let lastProposalIndex = -1;
  for (const message of messages) {
    if (message.role === "user") {
      if (lastQuestionIndex >= 0 && mapped[lastQuestionIndex]?.kind === "question") {
        (mapped[lastQuestionIndex] as Extract<ChatItem, { kind: "question" }>).answered = true;
        lastQuestionIndex = -1;
      }
      if (lastProposalIndex >= 0 && mapped[lastProposalIndex]?.kind === "proposal") {
        (mapped[lastProposalIndex] as ProposalChatItem).resolved = true;
        lastProposalIndex = -1;
      }
      mapped.push({ id: uid(), kind: "message", role: "user", content: message.content ?? "" });
      continue;
    }
    if (message.role === "assistant") {
      // Order matters: within a persisted step the narration (content) was
      // streamed before the tool calls it triggered, so replay text first,
      // then the step rows — same sequence the user watched live.
      if (message.content) {
        mapped.push({ id: uid(), kind: "message", role: "assistant", content: message.content });
        lastQuestionIndex = -1;
        lastProposalIndex = -1;
      }
      // Persisted tool calls replay as completed step rows, matching what the
      // user saw while streaming — steps are part of the record now, not an
      // ephemeral shimmer. Timing and result detail were not persisted, so
      // the row shows the label and the args hint only.
      for (const call of message.toolCalls ?? []) {
        if (call.name === "ask_user") {
          const parsed = parseAskUserQuestion(call.args);
          if (parsed) {
            lastQuestionIndex = mapped.length;
            mapped.push({
              id: uid(),
              kind: "question",
              question: parsed.question,
              options: parsed.options,
              answered: false,
            });
          }
          continue;
        }
        if (call.name === "propose_cleanup") {
          const parsed = parseAgentProposal(call.args);
          if (parsed) {
            lastProposalIndex = mapped.length;
            mapped.push({
              id: uid(),
              kind: "proposal",
              proposal: parsed,
              resolved: false,
            });
          }
          continue;
        }
        mapped.push({
          id: uid(),
          kind: "tool",
          name: call.name,
          args: call.args,
          status: "done",
          startedAt: 0,
        });
      }
      continue;
    }
  }
  items.value = mapped;
  // A reload in the middle of a review restores the split view: the newest
  // proposal still awaiting a decision reopens in the artifact panel.
  for (let i = mapped.length - 1; i >= 0; i -= 1) {
    const item = mapped[i];
    if (item.kind === "proposal" && !item.resolved) {
      activeProposal.value = item;
      break;
    }
  }
}

onMounted(async () => {
  void loadAgentSettings();
  // Restore the ring before the session request resolves, so a reload shows
  // the last reading immediately instead of an empty ring until the next turn.
  loadPersistedUsage();
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
  if (streamRaf !== null) {
    cancelAnimationFrame(streamRaf);
    streamRaf = null;
  }
  stopTicker();
  if (copyTimer) clearTimeout(copyTimer);
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
    //
    // Append the error instead of replacing the transcript: the session still
    // exists server-side, so wiping the visible conversation over a transient
    // network error loses the user's context for nothing.
    const detail =
      error instanceof Error && error.message
        ? error.message
        : "no se pudo reiniciar el chat";
    items.value.push({ id: uid(), kind: "error", content: detail });
    return;
  }
  sessionId.value = "";
  selectedNovel.value = null;
  activeProposal.value = null;
  expandedTurns.value = new Set();
  lastUsage.value = null;
  lastModel.value = "";
  lastContextWindow.value = null;
  clearPersistedUsage();
  items.value = [];
}

function send(): void {
  void sendMessage(draft.value.trim());
}

// retryTurn resends the message that produced an error item. The partial
// answer (if any) stays in the transcript, so the retry reads as a follow-up
// rather than a rewrite of what the user already saw. An error that came from
// a regenerate retries the same way (drop and re-run) instead of appending a
// second copy of the question.
function retryTurn(item: ChatItem): void {
  if (item.kind !== "error" || !item.retry || streaming.value) return;
  if (item.retryRegenerate) {
    reaskLastTurn();
    return;
  }
  void sendMessage(item.retry);
}

async function chooseOption(
  item: Extract<ChatItem, { kind: "question" }>,
  option: AgentChatOption,
): Promise<void> {
  if (item.answered || streaming.value) return;
  item.answered = true;
  await sendMessage(option.value);
}

// The approve/cancel decision is applied by the frontend itself (preview and
// clean endpoints, same ones the CleanTab uses); the message that follows
// informs the model and makes the transcript self-describing on reload. The
// panel emits these with the active item, which may have changed between
// render and click — hence the null/guard rather than a captured reference.
async function onProposalApplied(
  item: ProposalChatItem | null,
  result: CleanApplyResult,
): Promise<void> {
  if (!item || item.resolved || streaming.value) return;
  item.resolved = true;
  item.outcome = "applied";
  if (activeProposal.value === item) activeProposal.value = null;
  const issues: string[] = [];
  if (result.skipped) issues.push(`${result.skipped} sin contenido aplicable`);
  if (result.notFound) issues.push(`${result.notFound} no encontrados`);
  if (result.failed) issues.push(`${result.failed} fallaron al guardar`);
  const detail = issues.length > 0 ? ` (${issues.join(", ")})` : "";
  await sendMessage(`Limpieza aplicada: ${result.modified} capítulos modificados.${detail}`);
}

function onProposalDiscarded(item: ProposalChatItem | null): void {
  if (!item || item.resolved || streaming.value) return;
  item.resolved = true;
  item.outcome = "discarded";
  if (activeProposal.value === item) activeProposal.value = null;
  void sendMessage("Limpieza descartada sin aplicar cambios.");
}

async function sendMessage(
  message: string,
  opts?: {
    // Regenerate re-runs the last turn: no new user bubble (the turn's user
    // item is already in the transcript) and the backend truncates the stored
    // session before re-asking, so the answer is replaced, not appended.
    regenerate?: boolean;
    // Items a regenerate just dropped. Restored if the re-run fails without
    // producing anything, so a network error cannot silently cost the user
    // the answer they already had (the server kept it, so local and stored
    // transcript stay in agreement).
    restoreItems?: ChatItem[];
  },
): Promise<void> {
  if (!message || streaming.value) return;

  const regenerate = opts?.regenerate === true;
  const restoreItems = opts?.restoreItems ?? [];

  draft.value = "";
  // A new user message closes any pending proposal card: the decision either
  // happened through the card or the proposal was abandoned (mirrors how a
  // reload replays the transcript).
  for (const item of items.value) {
    if (item.kind === "proposal") item.resolved = true;
  }
  if (!regenerate) {
    items.value.push({ id: uid(), kind: "message", role: "user", content: message });
  }
  // The user item this turn answers: the one just pushed, or (on regenerate)
  // the one already sitting at the tail. Restore anchors on it so a dropped
  // answer lands back inside its own turn even if the text repeats.
  const anchorUser: UserChatItem | null = items.value
    .slice()
    .reverse()
    .find((it): it is UserChatItem => it.kind === "message" && it.role === "user") ?? null;
  scrollToBottom();

  streaming.value = true;
  turnStartedAt = Date.now();
  startTicker();
  // Captured locally. stopStreaming clears `streaming` while this turn's
  // promise is still pending, so the user can send again immediately; the
  // catch/finally below then run against the NEW turn's controller. Reading
  // the module-level one made an aborted turn report a phantom network error
  // on the fresh turn, push a bogus "cut off mid-answer" notice, and finally
  // null out the controller the Stop button now controls.
  const controller = new AbortController();
  abortController = controller;

  let assistantText = "";
  // Text already committed to the transcript for the step in progress. A
  // terminal step (ask_user) is not the final answer, so the next user reply
  // starts a new assistant bubble instead of overwriting this one.
  let committedText = "";
  let sawDone = false;
  let sawError = false;
  // Whether the re-run produced anything at all (a delta, a step, a question).
  // Only an answer-less failure is worth restoring the dropped items for: if
  // new content arrived it replaced them legitimately.
  let producedContent = false;

  const commitAssistant = (): void => {
    if (assistantText === committedText) return;
    committedText = assistantText;
    if (!assistantText) return;
    const last = items.value[items.value.length - 1];
    // Reuse the bubble already carrying this text — whether it is empty or was
    // filled by a live delta or the `done` event. Idempotence matters here
    // because several exit paths call this (done, error, catch, finally): the
    // guard must match on the text, not on emptiness, or a `done` that already
    // wrote the final answer makes the next call push a duplicate bubble.
    if (last && last.kind === "message" && last.role === "assistant") {
      if (!last.content) {
        last.content = assistantText;
        return;
      }
      if (last.content === assistantText) return;
    }
    items.value.push({ id: uid(), kind: "message", role: "assistant", content: assistantText });
  };

  // Puts a regenerate's dropped items back after the last user message. Only
  // called when the re-run produced nothing: the old answer is still the
  // stored truth server-side (a failed turn saves nothing), so keeping it on
  // screen keeps local and persisted transcripts in agreement.
  const restoreDropped = (): void => {
    if (restoreItems.length === 0) return;
    const at = anchorUser ? items.value.indexOf(anchorUser) + 1 : items.value.length;
    items.value.splice(at < 0 ? items.value.length : at, 0, ...restoreItems);
    restoreItems.length = 0;
  };

  const handleEvent = (event: AgentChatEvent): void => {
    switch (event.type) {
      case "session":
        if (event.sessionId) sessionId.value = event.sessionId;
        break;
      case "text_delta": {
        producedContent = true;
        assistantText += event.text ?? "";
        scheduleStreamFlush(() => {
          if (assistantText === committedText) return;
          // The pin check runs against the pre-mutation geometry (the DOM
          // still reflects the previous frame), so a user parked at the
          // bottom keeps following the answer and a user reading back is
          // never dragged down.
          const pinned = isPinnedToBottom();
          const last = items.value[items.value.length - 1];
          if (last && last.kind === "message" && last.role === "assistant") {
            last.content = assistantText;
          } else {
            items.value.push({ id: uid(), kind: "message", role: "assistant", content: assistantText });
          }
          committedText = assistantText;
          if (pinned) scrollToBottom(true);
        });
        break;
      }
      case "tool_call": {
        producedContent = true;
        flushStreamNow(commitAssistant);
        assistantText = "";
        committedText = "";
        items.value.push({
          id: uid(),
          kind: "tool",
          name: event.tool ?? "tool",
          args: event.args,
          status: "running",
          startedAt: Date.now(),
        });
        scrollToBottom();
        break;
      }
      case "tool_result": {
        // Results and errors go back to the model (which self-corrects across
        // steps); the UI keeps the step as a completed row with its duration
        // and a one-line summary, expandable to the raw result. Matching the
        // newest RUNNING step of that name: two steps of the same tool can
        // run in one turn, and a stale match would report the wrong duration.
        const raw = event.result ?? "";
        const failed = /^error:/i.test(raw.trim());
        for (let i = items.value.length - 1; i >= 0; i -= 1) {
          const item = items.value[i];
          if (item.kind === "tool" && item.status === "running" && item.name === (event.tool ?? "tool")) {
            item.status = failed ? "error" : "done";
            item.finishedAt = Date.now();
            item.summary = summarizeToolResult(raw);
            item.detail = raw.length > TOOL_DETAIL_MAX ? `${raw.slice(0, TOOL_DETAIL_MAX)}\n…` : raw;
            break;
          }
        }
        scrollToBottom();
        break;
      }
      case "question": {
        producedContent = true;
        flushStreamNow(commitAssistant);
        assistantText = "";
        committedText = "";
        items.value.push({
          id: uid(),
          kind: "question",
          question: event.question ?? "",
          options: event.options ?? [],
          answered: false,
        });
        scrollToBottom();
        break;
      }
      case "proposal": {
        // A cleanup proposal ends the turn: a compact chip lands in the
        // transcript and the artifact panel opens with the live diff preview.
        // Nothing is applied until the user approves it in the panel.
        producedContent = true;
        flushStreamNow(commitAssistant);
        assistantText = "";
        committedText = "";
        if (event.proposal) {
          const item: ProposalChatItem = {
            id: uid(),
            kind: "proposal",
            proposal: event.proposal,
            resolved: false,
          };
          items.value.push(item);
          activeProposal.value = item;
        }
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
            items.value.push({ id: uid(), kind: "message", role: "assistant", content: finalText });
          }
          committedText = finalText;
          assistantText = finalText;
        });
        settleRunningTools("cancelled");
        sessionId.value = event.sessionId ?? sessionId.value;
        if (event.usage) {
          lastUsage.value = event.usage;
          lastModel.value = event.model ?? "";
          lastContextWindow.value = event.contextWindow ?? null;
          persistUsage();
        }
        streaming.value = false;
        scrollToBottom();
        break;
      }
      case "error": {
        sawError = true;
        flushStreamNow(commitAssistant);
        if (!producedContent) restoreDropped();
        const detail = event.error || event.code || "error del servidor";
        items.value.push({
          id: uid(),
          kind: "error",
          content: detail,
          retry: message,
          retryRegenerate: regenerate || undefined,
        });
        settleRunningTools("error");
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
        media: mediaScope.value,
        message,
        regenerate: regenerate || undefined,
      },
      handleEvent,
      controller.signal,
    );
  } catch (error) {
    // An intentional stop is not a failure: keep whatever partial answer
    // already streamed in and stay silent. Read this turn's own controller,
    // not the module-level one, which a turn started after this one has
    // already replaced.
    if (!controller.signal.aborted) {
      flushStreamNow(commitAssistant);
      if (!producedContent) restoreDropped();
      const detail =
        error instanceof Error && error.message
          ? error.message
          : "no se pudo contactar al servidor";
      items.value.push({
        id: uid(),
        kind: "error",
        content: detail,
        retry: message,
        retryRegenerate: regenerate || undefined,
      });
    }
  } finally {
    flushStreamNow(commitAssistant);
    // A stream that ends without a done event was cut off (proxy timeout,
    // dropped connection). Without this the partial answer is left on screen
    // looking exactly like a finished one.
    if (!sawDone && !sawError && !controller.signal.aborted) {
      if (!producedContent) restoreDropped();
      items.value.push({
        id: uid(),
        kind: "error",
        content: "La respuesta se cortó a mitad. Vuelve a intentarlo.",
        retry: message,
        retryRegenerate: regenerate || undefined,
      });
    }
    settleRunningTools("cancelled");
    // Only tear down shared state if this turn still owns it. A turn that was
    // stopped and superseded must not clear the new turn's controller, flip
    // its streaming flag or stop the ticker its step durations depend on.
    if (abortController === controller) {
      streaming.value = false;
      abortController = null;
      stopTicker();
    }
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
   the composer below the fold while the transcript still refuses to scroll. */
.chat-page {
  display: flex;
  flex-direction: column;
  /* La altura la fija el encadenamiento flex desde AppLayout (ver el bloque
     <style> no-scoped al final del archivo), no un calc() con offsets del
     header que se desajustan por píxeles y cambiaban con cada breakpoint. */
  flex: 1;
  min-height: 0;
}

.page-header {
  display: flex;
  align-items: center;
  /* Single right-aligned group (ring + new-chat button); no title on the
     left, so space-between would have pinned the group to the edge anyway. */
  justify-content: flex-end;
  gap: 0.5rem;
  flex-wrap: wrap;
  margin-bottom: 0.375rem;
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

.page-actions {
  display: flex;
  align-items: center;
  gap: 0.625rem;
}

/* Action bar under a finished turn's final answer: quiet ghost icons, same
   rhythm as the rest of the chrome; only the copy icon reacts (checkmark). */
.turn-actions {
  display: flex;
  align-items: center;
  gap: 0.125rem;
  margin-top: 0.125rem;
}

.turn-action {
  color: var(--text-tertiary);
  transition: background-color 0.15s ease-out, color 0.15s ease-out;
}

.turn-action:hover:not(:disabled) {
  color: var(--text-primary);
  background: var(--mock-row);
}

.turn-action:disabled {
  opacity: 0.5;
}

/* Context ring: the circle type renders 120px by default, so the width is
   pinned here (scoped :deep beats the component stylesheet). Kept small on
   purpose — it is a glanceable status, not a chart; the popover carries the
   numbers. */
.chat-usage-ring {
  display: inline-flex;
  align-items: center;
  cursor: default;
  flex-shrink: 0;
}

.chat-usage-ring :deep(.n-progress--circle) {
  width: 20px;
}

/* Popover body: two quiet label/value rows plus the model id, no cost. */
.chat-usage-pop {
  min-width: 11rem;
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
  padding: 0.125rem 0;
}

.chat-usage-row {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 1rem;
  font-size: 0.8125rem;
}

.chat-usage-model {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 14rem;
}

/* Split layout: the transcript and the artifact side panel share the middle
   of the page; the composer spans the full width below. */
.chat-body {
  flex: 1;
  min-height: 0;
  display: flex;
  gap: 0.75rem;
}

.chat-scroll {
  flex: 1;
  min-width: 0;
  min-height: 0;
  display: flex;
  flex-direction: column;
  /* El scroll es manejado por n-scrollbar; no overflow nativo. */
  padding: 0.25rem 0.125rem 0.5rem;
  overscroll-behavior: contain;
}

.chat-scroll-inner {
  display: flex;
  flex-direction: column;
  gap: 1.125rem;
}

/* The artifact panel: fixed share of the row, its own internal scroll (the
   CleanupProposalPanel body owns the scrollbar). */
.chat-artifact {
  flex-shrink: 0;
  width: clamp(340px, 42%, 640px);
  min-height: 0;
  overflow: hidden;
  border: 1px solid var(--divide);
  background: var(--surface-elevated);
  border-radius: var(--radius-lg);
  padding: 0.875rem 1rem;
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

/* --- Turns ----------------------------------------------------------------
   One turn = the user's bubble + ONE assistant bubble holding the whole
   turn: narration, step rows and the final answer. The bubble closes when
   the turn ends, so "what the assistant did about my question" is a single
   scannable object. It is deliberately wider than the user bubble: agent
   answers carry tables and long lists that need the measure. */

.turn {
  display: flex;
  flex-direction: column;
  gap: 0.625rem;
}

.turn-user {
  display: flex;
  justify-content: flex-end;
}

.turn-bubble {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  min-width: 0;
  max-width: min(54rem, 100%);
  border: 1px solid var(--divide);
  border-radius: var(--radius-lg);
  background: var(--surface-elevated);
  padding: 0.75rem 1rem;
}

/* display:contents keeps the (memoized) item wrappers out of the layout:
   the rows below become the flex children of .turn-bubble directly. */
.turn-item {
  display: contents;
}

/* Assistant prose: reading width inside the bubble, no chrome of its own —
   the bubble is the container now, so code blocks and tables get the room. */
.turn-text {
  font-size: 0.9375rem;
  line-height: 1.55;
  overflow-wrap: anywhere;
}

/* The caret is a ::after on the last block markdown produced — no DOM
   measurement per token, and it disappears the moment the item stops being
   the streaming tail (class removed / final set). */
.chat-markdown--streaming :deep(.markstream-vue > :last-child)::after {
  content: "";
  display: inline-block;
  width: 0.45em;
  height: 1em;
  margin-left: 0.1em;
  vertical-align: -0.12em;
  background: var(--text-primary);
  animation: stream-caret 1.1s steps(1, end) infinite;
}

@keyframes stream-caret {
  0%, 55% { opacity: 1; }
  56%, 100% { opacity: 0; }
}

/* Collapsed-steps chip: quiet pill, the only entry point to a fold. */
.turn-steps {
  display: inline-flex;
  align-items: center;
  gap: 0.375rem;
  align-self: flex-start;
  border: 1px solid var(--divide);
  background: transparent;
  border-radius: var(--radius-pill);
  padding: 0.1875rem 0.625rem;
  font-size: 0.75rem;
  color: var(--text-secondary);
  cursor: pointer;
  transition: background-color 0.15s ease-out;
}

.turn-steps:hover {
  background: var(--mock-row);
}

/* Tool step row: one line, state on the left, duration on the right;
   expands to the raw result when there is one to read. Inside the bubble it
   sits on a lower tonal step instead of its own bordered card. */
.tool-row {
  display: flex;
  flex-direction: column;
  min-width: 0;
  border: 1px solid var(--divide);
  border-radius: var(--radius-md);
  background: var(--surface-base);
  overflow: hidden;
}

.tool-row--error {
  border-color: var(--danger);
}

.tool-row--cancelled {
  opacity: 0.66;
}

.tool-row-main {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  width: 100%;
  min-height: 2rem;
  padding: 0.3125rem 0.625rem;
  border: none;
  background: transparent;
  color: inherit;
  font: inherit;
  font-size: 0.8125rem;
  text-align: left;
  cursor: pointer;
  transition: background-color 0.15s ease-out;
}

.tool-row-main:disabled {
  cursor: default;
}

.tool-row-main:hover:not(:disabled) {
  background: var(--mock-row);
}

.tool-row-state {
  display: inline-flex;
  align-items: center;
  flex: 0 0 auto;
  color: var(--text-tertiary);
}

.tool-row--error .tool-row-state {
  color: var(--danger);
}

.tool-row-spinner {
  width: 0.75rem;
  height: 0.75rem;
  border: 1.5px solid color-mix(in oklab, var(--text-tertiary) 30%, transparent);
  border-top-color: var(--text-secondary);
  border-radius: var(--radius-pill);
  animation: tool-spin 0.8s linear infinite;
}

@keyframes tool-spin {
  to { transform: rotate(360deg); }
}

.tool-row-name {
  flex: 0 0 auto;
  font-weight: 500;
  color: var(--text-secondary);
  white-space: nowrap;
}

.tool-row-args {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text-tertiary);
}

.tool-row-summary {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text-tertiary);
}

.tool-row-time {
  flex: 0 0 auto;
  margin-left: auto;
  color: var(--text-tertiary);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

.tool-row-chevron {
  flex: 0 0 auto;
  font-size: 0.6875rem;
  color: var(--text-tertiary);
}

.tool-row-detail {
  margin: 0;
  padding: 0.5rem 0.75rem;
  border-top: 1px solid var(--divide);
  background: var(--background);
  font-family: "SFMono-Regular", ui-monospace, Menlo, Monaco, Consolas, monospace;
  font-size: 0.75rem;
  line-height: 1.5;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  max-height: 14rem;
  overflow-y: auto;
}

/* Question card: inside the bubble it carries its own tonal step (it is an
   interactive object, not prose) and fills the bubble's width. */
.turn-question {
  border: 1px solid var(--divide);
  background: var(--surface-base);
  border-radius: var(--radius-md);
  padding: 0.625rem 0.875rem;
  font-size: 0.9375rem;
  line-height: 1.55;
  overflow-wrap: anywhere;
}

.chat-bubble {
  max-width: 100%;
  border-radius: var(--radius-lg);
  padding: 0.625rem 0.875rem;
  font-size: 0.9375rem;
  line-height: 1.55;
  overflow-wrap: anywhere;
}

/* User turns stay narrower than the agent column: they are typically a
   question or a short instruction, and the asymmetry is what makes the
   thread readable without alternating heavy blocks. */
.chat-bubble--user {
  max-width: min(38rem, 82%);
  background: var(--btn-primary-bg);
  color: var(--btn-primary-fg);
  white-space: pre-wrap;
}

/* Pending state: shown inside the active turn's rail until the first token.
   The counter only appears once a second has actually passed. */
.turn-pending {
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

/* markstream-vue sizes its root and its paragraphs from --ms-text-body
   (1rem), which outsize the turn's own 0.9375rem. Rebase it so the agent
   text and the user bubble read at the same text size. */
.chat-markdown :deep(.markstream-vue) {
  --ms-text-body: 0.9375rem;
  font-size: 0.9375rem;
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

.chat-error {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 0.5rem;
  border: 1px solid var(--danger);
  color: var(--danger);
  border-radius: var(--radius-md);
  padding: 0.625rem 0.75rem;
  font-size: 0.875rem;
  line-height: 1.5;
}

.chat-error-text {
  margin: 0;
}

.chat-error-retry {
  color: var(--danger);
}

.chat-composer {
  padding-top: 0.75rem;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.chat-composer-row {
  display: flex;
  align-items: center;
  gap: 0.625rem;
  min-height: 4.5rem;
  padding: 0.5rem;
  border: 1px solid var(--divide);
  border-radius: var(--radius-pill);
  background: var(--surface-elevated);
}

.chat-attach-trigger {
  flex: 0 0 3rem;
  width: 3rem;
  height: 3rem;
  color: var(--aged-stone, var(--text-secondary));
  background: var(--background);
  transition: background-color 0.15s ease, color 0.15s ease;
}

.chat-attach-trigger:hover {
  color: var(--text-primary);
  background: var(--mock-row);
}

.chat-input-shell {
  display: flex;
  align-items: center;
  flex: 1;
  gap: 0.5rem;
  min-width: 0;
  min-height: 3.125rem;
  padding: 0.25rem 0.375rem 0.25rem 1rem;
  border: 1px solid var(--divide);
  border-radius: var(--radius-pill);
  background: var(--surface-base);
  transition: border-color 0.15s ease;
}

.chat-input-shell:focus-within {
  border-color: var(--border-strong);
}

.chat-input {
  flex: 1;
  min-width: 0;
}

.chat-input :deep(.n-input) {
  background: transparent;
}

.chat-input :deep(.n-input__textarea-el) {
  padding-block: 0.625rem;
  line-height: 1.45;
}

.chat-input :deep(.n-input__placeholder) {
  color: var(--text-tertiary);
}

.chat-input-actions {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  flex: 0 0 auto;
}

.chat-model-trigger {
  flex-shrink: 0;
  max-width: 12rem;
}

.chat-model-trigger-label {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.chat-model-warn {
  white-space: nowrap;
  color: var(--warning, var(--text-tertiary));
}

.chat-send-button {
  flex: 0 0 3rem;
  width: 3rem;
  height: 3rem;
  color: var(--surface-base) !important;
  background: var(--text-primary) !important;
  transition: background-color 0.15s ease, opacity 0.15s ease;
}

.chat-send-button:hover:not(:disabled) {
  background: color-mix(in oklab, var(--text-primary) 82%, transparent) !important;
}

.chat-send-button:disabled {
  opacity: 0.58;
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
  /* The artifact panel has no room beside the transcript on a phone: it
     becomes a full-screen overlay with its own close button. */
  .chat-artifact {
    position: fixed;
    inset: 0;
    z-index: 60;
    width: auto;
    border: none;
    border-radius: 0;
    background: var(--background);
    padding: 1rem;
  }

  .page-title {
    font-size: 1.5rem;
  }

  /* Narrow viewports: the measure cap has to yield so bubbles use the width
     they actually have. User turns keep the tighter share. */
  .chat-bubble--user {
    max-width: 88%;
  }

  .turn-bubble {
    padding: 0.625rem 0.75rem;
  }

  /* Keep the controls thumb-sized while allowing the text field to take the
     width that remains on narrow phones. */
  .chat-composer-row {
    gap: 0.375rem;
    padding: 0.375rem;
  }

  .chat-attach-trigger,
  .chat-send-button {
    flex-basis: 2.75rem;
    width: 2.75rem;
    height: 2.75rem;
  }

  /* Two-row shell on phones: the field takes the whole first row and the
     media/model selectors drop to a second one, so writing keeps full width. */
  .chat-input-shell {
    flex-wrap: wrap;
    gap: 0.25rem 0.5rem;
    padding: 0.375rem 0.625rem;
    border-radius: var(--radius-lg);
  }

  .chat-input {
    flex-basis: 100%;
  }

  /* The long placeholder wraps to two lines inside a one-row field: keep the
     field tall enough for both lines plus its block padding. */
  .chat-input :deep(.n-input__textarea-el) {
    min-height: calc(1.45em * 2 + 1.25em);
  }

  /* Selectors must shrink inside the shell, not push past it (desktop makes
     this row content-sized); "Sin API key" wraps to its own line if needed. */
  .chat-input-actions {
    flex: 1 1 auto;
    flex-wrap: wrap;
    row-gap: 0.375rem;
  }

  .chat-model-trigger {
    max-width: 7.5rem;
  }

  .chat-model-trigger-label {
    max-width: 4.5rem;
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
  .chat-picker-item,
  .chat-attach-trigger,
  .chat-input-shell,
  .chat-send-button,
  .turn-steps,
  .turn-action,
  .tool-row-main {
    transition: none;
  }

  .chat-pending-dot {
    animation: none;
    opacity: 0.7;
  }

  /* The caret stops blinking and reads as a static end-of-text marker. */
  .chat-markdown--streaming :deep(.markstream-vue > :last-child)::after {
    animation: none;
    opacity: 0.6;
  }

  /* The spinner holds still; the row's own duration still ticks. */
  .tool-row-spinner {
    animation: none;
  }
}
</style>

<!-- No-scoped: reaches AppLayout internals, which live outside this
     component's scoped DOM. Guarded with :has() so only the chat page gets
     the height chain; every other page keeps the auto-height, scrollable
     .page-container untouched. -->
<style>
.page-container:has(> .chat-page) {
  /* Resolves against n-scrollbar-container, which naive-ui sizes at 100%
     of .app-scroll (flex: 1 of .app-main). With this, .chat-page's flex: 1
     fills the viewport exactly and the page-level scrollbar disappears
     regardless of the topbar's real height, its border or the breakpoint
     paddings: no magic numbers to keep in sync. */
  height: 100%;
  display: flex;
  flex-direction: column;
}
</style>

<style>
.chat-media-select {
  width: 6.5rem;
}
</style>
