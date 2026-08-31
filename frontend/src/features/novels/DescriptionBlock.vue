<template>
  <div v-if="description" class="novel-description-wrapper">
    <div
      ref="el"
      class="markdown-preview muted small novel-description"
      :class="{ 'novel-description--collapsed': !expanded }"
      v-html="markdownToHtml(description)"
    />
    <n-button
      v-if="overflow || expanded"
      text
      size="small"
      class="novel-description-toggle"
      @click="expanded = !expanded"
    >
      {{ expanded ? 'Mostrar menos' : 'Mostrar más' }}
      <template #icon>
        <n-icon :size="12"><ChevronUpOutline v-if="expanded" /><ChevronDownOutline v-else /></n-icon>
      </template>
    </n-button>
  </div>
</template>

<script setup lang="ts">
import { toRef, watch } from "vue";
import { NButton, NIcon } from "naive-ui";
import { ChevronUpOutline, ChevronDownOutline } from "@vicons/ionicons5";
import { useDescriptionOverflow } from "@/composables/useDescriptionOverflow";
import { markdownToHtml } from "@/utils/markdown";

const props = defineProps<{
  description: string;
  resetKey?: string | number;
}>();

const { el, expanded, overflow } = useDescriptionOverflow(toRef(props, "description"));

watch(
  () => props.resetKey,
  () => {
    expanded.value = false;
  },
);
</script>

<style scoped>
.novel-description-wrapper {
  margin: 0.375rem 0 0;
  position: relative;
}

.novel-description {
  font-size: 0.875rem;
  line-height: 1.3;
}

.novel-description--collapsed {
  max-height: calc(0.875rem * 1.3 * 5);
  overflow: hidden;
  mask-image: linear-gradient(to bottom, black 60%, transparent 100%);
  -webkit-mask-image: linear-gradient(to bottom, black 60%, transparent 100%);
}

.novel-description :deep(p) {
  margin: 0 0 0.5rem;
}

.novel-description :deep(p:last-child) {
  margin-bottom: 0;
}

.novel-description-toggle {
  align-self: flex-start;
}

@media (max-width: 768px) {
  .novel-description {
    font-size: 0.8125rem;
  }
}
</style>
