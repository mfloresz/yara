<template>
  <section class="stack-md tab-panel" aria-labelledby="tab-export">
    <h2 id="tab-export" class="sr-only">Exportar</h2>
    <n-card title="Exportar a EPUB">
      <div class="stack-md">
        <div style="min-width: 220px; max-width: 320px">
          <label class="small muted">Fuente del contenido</label>
          <n-select v-model:value="source" :options="exportSourceOptions" />
        </div>

        <n-progress v-if="building" :percentage="progress" :show-indicator="true" />
        <n-alert v-if="feedback" :type="feedback.startsWith('Error:') ? 'error' : 'success'">{{ feedback }}</n-alert>
        <n-button type="primary" :loading="building" :disabled="building" @click="buildAndDownload">
          <template #icon><n-icon><DownloadOutline /></n-icon></template>
          Descargar EPUB
        </n-button>
      </div>
    </n-card>
  </section>
</template>

<script setup lang="ts">
import { toRef } from "vue";
import { NAlert, NButton, NCard, NIcon, NProgress, NSelect } from "naive-ui";
import { DownloadOutline } from "@vicons/ionicons5";
import type { Novel } from "@/domain";
import { useExportFlow, type ExportSource } from "@/composables/useExportFlow";

const props = defineProps<{
  novel: Novel;
}>();

const exportSourceOptions: { value: ExportSource; label: string }[] = [
  { value: "refined", label: "Refinados" },
  { value: "translated", label: "Traducidos" },
  { value: "original", label: "Originales" },
];

const novelRef = toRef(props, "novel");
const { source, building, progress, feedback, buildAndDownload } = useExportFlow(novelRef);
</script>
