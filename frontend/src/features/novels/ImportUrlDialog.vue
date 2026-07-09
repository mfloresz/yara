<template>
  <n-modal v-model:show="visible" preset="card" title="Importar novela desde URL" :style="{ width: 'min(520px, 96vw)' }" @after-leave="reset">
    <div class="stack-md">
      <div>
        <label class="small muted">URL de la novela</label>
        <n-input
          v-model:value="url"
          placeholder="https://novelphoenix.com/novel/..."
          :disabled="loading"
          @keydown.enter="handleSearch"
        />
        <div class="small muted" style="margin-top: 0.35rem">
          Sitios soportados: {{ supportedSites.join(", ") }}
        </div>
      </div>

      <n-alert v-if="error" type="error">{{ error }}</n-alert>
    </div>
    <template #action>
      <n-button secondary :disabled="loading" @click="visible = false">Cancelar</n-button>
      <n-button
        type="primary"
        :loading="loading"
        :disabled="!url.trim()"
        @click="handleSearch"
      >
        <template #icon><n-icon><SearchOutline /></n-icon></template>
        {{ loading ? 'Buscando...' : 'Buscar' }}
      </n-button>
    </template>
  </n-modal>
</template>

<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { NModal, NInput, NAlert, NButton, NIcon } from "naive-ui";
import { SearchOutline } from "@vicons/ionicons5";
import { useNovels } from "@/composables/useNovels";
import type { PreviewUrlResult } from "@/api/types";

const props = defineProps<{ open: boolean }>();
const emit = defineEmits<{
  "update:open": [value: boolean];
  "preview": [preview: PreviewUrlResult];
}>();

const { previewNovelFromUrl } = useNovels();

const visible = computed({
  get: () => props.open,
  set: (value) => emit("update:open", value),
});

const supportedSites = ["novelfire.net", "novelphoenix.com", "novelbin.com"];

const url = ref("");
const loading = ref(false);
const error = ref<string | null>(null);

function reset() {
  url.value = "";
  loading.value = false;
  error.value = null;
}

watch(visible, (open) => {
  if (open) reset();
});

async function handleSearch() {
  if (!url.value.trim()) return;
  loading.value = true;
  error.value = null;
  try {
    const preview = await previewNovelFromUrl(url.value.trim());
    emit("preview", preview);
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err);
  } finally {
    loading.value = false;
  }
}
</script>
