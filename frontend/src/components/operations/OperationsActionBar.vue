<template>
  <n-card
    size="small"
    class="ops-actionbar"
    content-style="padding: 10px 12px;"
    role="toolbar"
    aria-label="Acciones en lote"
  >
    <n-flex justify="space-between" align="center" wrap :size="10">
      <n-flex align="center" :size="8" :wrap="false">
        <n-button size="small" quaternary circle aria-label="Limpiar selección" @click="$emit('clear')">
          <template #icon><n-icon><CloseOutline /></n-icon></template>
        </n-button>
        <n-text depth="3" class="ops-actionbar-summary">
          {{ selectedCount }} sel. · {{ withUpdates }} con novedades · {{ translatable }} por traducir · {{ actualizable }} verificables
        </n-text>
      </n-flex>
      <n-flex :size="8" wrap>
        <n-button
          size="small"
          secondary
          class="ops-actionbar-btn"
          :loading="checking"
          :disabled="actualizable === 0"
          @click="$emit('verify')"
        >
          <template #icon><n-icon><RefreshOutline /></n-icon></template>
          Verificar{{ actualizable ? ` (${actualizable})` : "" }}
        </n-button>
        <n-button
          size="small"
          class="ops-actionbar-btn"
          :loading="downloading"
          :disabled="withUpdates === 0"
          @click="$emit('download')"
        >
          <template #icon><n-icon><DownloadOutline /></n-icon></template>
          Descargar{{ withUpdates ? ` (${withUpdates})` : "" }}
        </n-button>
        <n-button
          size="small"
          type="primary"
          class="ops-actionbar-btn"
          :loading="translating"
          :disabled="translatable === 0"
          @click="$emit('translate')"
        >
          <template #icon><n-icon><PlayOutline /></n-icon></template>
          Traducir{{ translatable ? ` (${translatable})` : "" }}
        </n-button>
        <n-button
          size="small"
          type="error"
          ghost
          class="ops-actionbar-btn"
          :loading="deleting"
          :disabled="selectedCount === 0"
          @click="$emit('remove')"
        >
          <template #icon><n-icon><TrashOutline /></n-icon></template>
          Eliminar
        </n-button>
      </n-flex>
    </n-flex>
  </n-card>
</template>

<script setup lang="ts">
import { NButton, NCard, NFlex, NIcon, NText } from "naive-ui";
import { CloseOutline, DownloadOutline, PlayOutline, RefreshOutline, TrashOutline } from "@vicons/ionicons5";

defineProps<{
  selectedCount: number;
  withUpdates: number;
  translatable: number;
  actualizable: number;
  checking: boolean;
  downloading: boolean;
  translating: boolean;
  deleting: boolean;
}>();

defineEmits<{
  verify: [];
  download: [];
  translate: [];
  remove: [];
  clear: [];
}>();
</script>

<style scoped>
.ops-actionbar {
  position: sticky;
  bottom: calc(12px + env(safe-area-inset-bottom, 0px));
  z-index: 1;
}

.ops-actionbar-summary {
  font-size: 12px;
}

.ops-actionbar-btn {
  min-height: 40px;
}

@media (max-width: 640px) {
  .ops-actionbar-btn {
    min-height: 44px;
    flex: 1 1 auto;
  }
}

@media (prefers-reduced-motion: reduce) {
  .ops-actionbar-btn {
    transition: none;
  }
}
</style>
