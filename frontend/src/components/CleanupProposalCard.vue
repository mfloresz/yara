<template>
  <button
    type="button"
    class="proposal-chip"
    aria-label="Abrir vista previa de la propuesta de limpieza"
    @click="emit('open')"
  >
    <span class="proposal-chip-head">
      <n-icon :size="14" aria-hidden="true"><SparklesOutline /></n-icon>
      <span class="proposal-chip-title">Propuesta de limpieza</span>
      <n-tag size="small" round :type="statusType" :bordered="false">{{ statusLabel }}</n-tag>
    </span>
    <span class="proposal-chip-novel">{{ proposal.novelTitle || proposal.novelId }}</span>
    <span class="proposal-chip-rule">
      {{ ruleText }}
      <span class="muted">· {{ applyToLabel }}</span>
    </span>
  </button>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { NIcon, NTag } from "naive-ui";
import { SparklesOutline } from "@vicons/ionicons5";
import type { AgentCleanupProposal } from "@/api/types";
import { CLEAN_APPLY_TO_LABELS, describeCleanupRule } from "@/utils/cleaner";

const props = defineProps<{
  proposal: AgentCleanupProposal;
  resolved: boolean;
  outcome?: "applied" | "discarded";
}>();

const emit = defineEmits<{
  open: [];
}>();

const ruleText = computed(() => describeCleanupRule(props.proposal));
const applyToLabel = computed(() => CLEAN_APPLY_TO_LABELS[props.proposal.applyTo] ?? props.proposal.applyTo);

const status = computed(() => {
  if (!props.resolved) return "pending";
  return props.outcome ?? "closed";
});

const statusLabel = computed(() => {
  switch (status.value) {
    case "pending":
      return "Pendiente";
    case "applied":
      return "Aplicada";
    case "discarded":
      return "Descartada";
    default:
      return "Cerrada";
  }
});

const statusType = computed(() => {
  switch (status.value) {
    case "pending":
      return "warning";
    case "applied":
      return "success";
    default:
      return "default";
  }
});
</script>

<style scoped>
.proposal-chip {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 0.2rem;
  width: 100%;
  max-width: min(30rem, 100%);
  border: 1px solid var(--divide);
  background: var(--surface-elevated);
  border-radius: var(--radius-lg);
  padding: 0.625rem 0.75rem;
  font-size: 0.875rem;
  line-height: 1.45;
  text-align: left;
  cursor: pointer;
  transition: background-color 0.15s ease-out, border-color 0.15s ease-out;
}

.proposal-chip:hover {
  background: var(--mock-row);
  border-color: var(--border-strong);
}

.proposal-chip-head {
  display: inline-flex;
  align-items: center;
  gap: 0.375rem;
}

.proposal-chip-title {
  font-weight: 600;
  color: var(--foreground);
}

.proposal-chip-novel {
  color: var(--text-secondary);
  font-size: 0.8125rem;
}

.proposal-chip-rule {
  font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, monospace);
  font-size: 0.8125rem;
  color: var(--foreground);
  overflow-wrap: anywhere;
}

@media (prefers-reduced-motion: reduce) {
  .proposal-chip {
    transition: none;
  }
}
</style>
