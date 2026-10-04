<template>
  <div class="proposal-panel">
    <header class="proposal-panel-head">
      <div class="proposal-panel-heading">
        <span class="proposal-panel-title">
          <n-icon :size="14" aria-hidden="true"><SparklesOutline /></n-icon>
          Propuesta de limpieza
        </span>
        <span class="small muted">{{ novelLabel }}</span>
      </div>
      <div class="proposal-panel-tools">
        <n-tag size="small" round :type="statusType" :bordered="false">{{ statusLabel }}</n-tag>
        <n-button quaternary circle size="tiny" aria-label="Cerrar vista previa" @click="emit('close')">
          <template #icon><n-icon :size="12"><CloseOutline /></n-icon></template>
        </n-button>
      </div>
    </header>

    <p class="proposal-panel-rule">
      {{ ruleText }}
      <span class="muted">· {{ applyToLabel }}</span>
    </p>

    <div class="proposal-panel-body">
      <n-spin v-if="loading" size="small" class="proposal-panel-loading" />
      <template v-else-if="loadError">
        <div class="proposal-panel-error">{{ loadError }}</div>
        <n-button size="small" secondary style="margin-top: 0.5rem" @click="load">Reintentar</n-button>
      </template>
      <template v-else-if="items.length === 0">
        <p class="muted small">Ninguno de los {{ total }} capítulos se vería afectado.</p>
      </template>
      <CleanDiffList v-else :items="items" class="proposal-panel-diffs" />
    </div>

    <footer class="proposal-panel-footer">
      <span class="small muted">
        <template v-if="items.length === 0">Ningún capítulo afectado.</template>
        <template v-else>Se modificarán {{ items.length }} de {{ total }} capítulos.</template>
      </span>
      <div v-if="!resolved" class="proposal-panel-actions">
        <n-button size="small" secondary :disabled="applying || busy" @click="discard">
          Descartar
        </n-button>
        <n-button
          size="small"
          type="primary"
          :loading="applying"
          :disabled="busy || loading || items.length === 0"
          @click="approve"
        >
          Aplicar a {{ items.length }} capítulos
        </n-button>
      </div>
    </footer>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { NButton, NIcon, NSpin, NTag } from "naive-ui";
import { CloseOutline, SparklesOutline } from "@vicons/ionicons5";
import type { AgentCleanupProposal, ChapterSummary, CleanPreviewItem } from "@/api/types";
import { CLEAN_APPLY_TO_LABELS, describeCleanupRule } from "@/utils/cleaner";
import { useAppServices } from "@/app/services";
import CleanDiffList from "@/components/CleanDiffList.vue";

type CleanApplyResult = {
  modified: number;
  total: number;
  skipped: number;
  notFound: number;
  failed: number;
};

const props = defineProps<{
  proposal: AgentCleanupProposal;
  resolved: boolean;
  outcome?: "applied" | "discarded";
  busy: boolean;
}>();

const emit = defineEmits<{
  applied: [result: CleanApplyResult];
  discarded: [];
  close: [];
}>();

const { api } = useAppServices();

const loading = ref(false);
const loadError = ref("");
const applying = ref(false);
const items = ref<CleanPreviewItem[]>([]);
const total = ref(0);
const fetchedTitle = ref("");

// The proposal event carries the novel title, but a panel rebuilt from the
// persisted session after a reload only has the rule args — fetch the title
// once so the panel stays self-describing.
const novelLabel = computed(() => props.proposal.novelTitle || fetchedTitle.value || props.proposal.novelId);

const ruleText = computed(() => describeCleanupRule(props.proposal));
const applyToLabel = computed(() => CLEAN_APPLY_TO_LABELS[props.proposal.applyTo] ?? props.proposal.applyTo);

const statusLabel = computed(() => {
  if (!props.resolved) return "Pendiente";
  if (props.outcome === "applied") return "Aplicada";
  if (props.outcome === "discarded") return "Descartada";
  return "Cerrada";
});
const statusType = computed(() => {
  if (!props.resolved) return "warning";
  return props.outcome === "applied" ? "success" : "default";
});

// Scope → chapter ids. Explicit ids come resolved in the proposal; a
// fromOrder/toOrder range is resolved here from the summaries, excluding
// hidden chapters the same way the backend's resolution does.
async function resolveChapterIds(): Promise<string[]> {
  const p = props.proposal;
  if (p.chapterIds && p.chapterIds.length > 0) return p.chapterIds;
  const from = p.fromOrder ?? 0;
  const to = p.toOrder ?? Number.MAX_SAFE_INTEGER;
  const summaries: ChapterSummary[] = await api.chapters.list(p.novelId);
  return summaries
    .filter((c) => !c.excluded && c.chapterOrder >= from && c.chapterOrder <= to)
    .map((c) => c.id);
}

async function load(): Promise<void> {
  loading.value = true;
  loadError.value = "";
  try {
    if (!props.proposal.novelTitle) {
      void api.novels.get(props.proposal.novelId).then((novel) => {
        fetchedTitle.value = novel ? novel.targetTitle || novel.sourceTitle || "" : "";
      }).catch(() => {});
    }
    const chapterIds = await resolveChapterIds();
    if (chapterIds.length === 0) {
      items.value = [];
      total.value = 0;
      return;
    }
    const p = props.proposal;
    const res = await api.chapters.cleanPreviewBulk(p.novelId, {
      chapterIds,
      mode: p.mode,
      searchText: p.searchText ?? "",
      replaceText: p.replaceText,
      caseSensitive: p.caseSensitive,
      useRegex: p.useRegex,
      applyTo: p.applyTo,
    });
    items.value = res.items;
    total.value = res.total;
  } catch (err) {
    loadError.value = err instanceof Error ? err.message : String(err);
  } finally {
    loading.value = false;
  }
}

async function approve(): Promise<void> {
  const chapterIds = items.value.map((item) => item.chapterId);
  if (chapterIds.length === 0) return;
  applying.value = true;
  try {
    const p = props.proposal;
    const result = await api.chapters.clean(p.novelId, {
      chapterIds,
      mode: p.mode,
      searchText: p.searchText ?? "",
      replaceText: p.replaceText,
      caseSensitive: p.caseSensitive,
      useRegex: p.useRegex,
      applyTo: p.applyTo,
    });
    emit("applied", result);
  } catch (err) {
    loadError.value = err instanceof Error ? err.message : String(err);
  } finally {
    applying.value = false;
  }
}

function discard(): void {
  emit("discarded");
}

onMounted(() => {
  void load();
});

// The panel stays mounted while the user switches between proposal chips, so
// a different selection must refetch instead of showing the previous diff.
watch(() => props.proposal, () => {
  void load();
});
</script>

<style scoped>
.proposal-panel {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  font-size: 0.9375rem;
  line-height: 1.5;
}

.proposal-panel-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 0.5rem;
}

.proposal-panel-heading {
  display: flex;
  flex-direction: column;
  gap: 0.125rem;
  min-width: 0;
}

.proposal-panel-title {
  display: inline-flex;
  align-items: center;
  gap: 0.375rem;
  font-weight: 600;
}

.proposal-panel-tools {
  display: inline-flex;
  align-items: center;
  gap: 0.375rem;
  flex-shrink: 0;
}

.proposal-panel-rule {
  margin: 0.375rem 0 0.75rem;
  font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, monospace);
  font-size: 0.8125rem;
  overflow-wrap: anywhere;
}

.proposal-panel-body {
  flex: 1;
  min-height: 0;
  overflow: auto;
  border: 1px solid var(--divide);
  border-radius: var(--radius-md);
  background: var(--surface-base);
  padding: 0.75rem;
}

/* The diff list caps itself at 62vh for the modal use; inside the panel the
   body owns the scroll, so the cap is lifted. */
.proposal-panel-diffs {
  max-height: none;
}

.proposal-panel-loading {
  display: block;
  margin: 2rem auto;
}

.proposal-panel-error {
  border: 1px solid var(--danger);
  color: var(--danger);
  border-radius: var(--radius-md);
  padding: 0.5rem 0.625rem;
  font-size: 0.8125rem;
}

.proposal-panel-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  flex-wrap: wrap;
  padding-top: 0.75rem;
}

.proposal-panel-actions {
  display: inline-flex;
  gap: 0.5rem;
}
</style>
