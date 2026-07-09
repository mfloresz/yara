<template>
  <AppLayout>
    <div class="stack-lg" style="max-width: 960px; margin: 0 auto">
      <div class="row-between">
        <div>
          <h1 style="margin: 0 0 0.25rem">Configuración</h1>
          <p class="muted" style="margin: 0">Toda la configuración es por usuario y se persiste en el backend.</p>
        </div>
        <n-button type="primary" :loading="saving" :disabled="!settings" @click="save">
          <template #icon><n-icon><SaveOutline /></n-icon></template>
          Guardar
        </n-button>
      </div>

      <n-alert v-if="error" type="error" :title="error" />
      <n-spin v-if="loading" style="display: flex; justify-content: center; padding: 2rem" :size="48" />

      <template v-else-if="settings">
        <n-card title="Tema" size="small">
          <n-select v-model:value="settings.theme" :options="themeOptions" />
        </n-card>

        <n-card title="Proveedor de IA" size="small">
          <div class="stack-md">
            <div class="row-wrap">
              <div style="flex: 1; min-width: 220px">
                <label class="small muted">Proveedor activo</label>
                <n-select
                  v-model:value="settings.ai.provider"
                  :options="providerOptions"
                  :loading="providersLoading"
                  @update:value="onProviderChange"
                />
              </div>
              <div style="flex: 1; min-width: 220px">
                <label class="small muted">Modelo</label>
                <n-select
                  v-if="modelOptions.length > 1"
                  v-model:value="settings.ai.model"
                  :options="modelOptions"
                  :disabled="!settings.ai.provider"
                />
                <n-input
                  v-else
                  v-model:value="settings.ai.model"
                  :disabled="!settings.ai.provider"
                  placeholder="Ej: local-model"
                />
              </div>
            </div>
            <div>
              <label class="small muted">Base URL</label>
              <n-input v-model:value="settings.ai.baseUrl" :style="{ fontFamily: 'monospace' }" />
            </div>
            <div>
              <label class="small muted">API Key</label>
              <n-input
                v-model:value="providerApiKey"
                type="password"
                show-password-on="click"
                :placeholder="providerConfigured ? '••••••••••••' : 'Pega una nueva API key'"
              />
              <div class="small muted" style="margin-top: 0.35rem">
                <span v-if="providerConfigured">API key configurada{{ activeProvider?.apiKeyUpdatedAt ? ` · actualizada ${formatDate(activeProvider.apiKeyUpdatedAt)}` : '' }}</span>
                <span v-else>No hay API key configurada para este provider.</span>
              </div>
              <div class="row-wrap" style="margin-top: 0.75rem">
                <n-button secondary :disabled="!providerApiKey.trim()" :loading="replacingKey" @click="replaceKey">Reemplazar key</n-button>
                <n-button type="error" secondary :disabled="!providerConfigured" :loading="deletingKey" @click="deleteKey">Eliminar key</n-button>
              </div>
            </div>
            <div class="row-wrap">
              <FieldNumber v-model.number="timeoutSec" label="Timeout (segundos)" :min="10" wrapper-style="min-width: 180px; flex: 1" />
            </div>
          </div>
        </n-card>

        <n-card title="Modelo para títulos" size="small">
          <div class="stack-md">
            <div class="row-between">
              <div>
                <strong>Usar modelo diferente para traducir títulos</strong>
                <div class="small muted">Permite usar un modelo más pequeño y económico para traducir solo los títulos de los capítulos, ya que al ser una línea no requiere un modelo grande. Si falla, se usará el modelo de traducción de contenido.</div>
              </div>
              <n-switch v-model:value="titleEnabled" style="flex-shrink: 0" />
            </div>
            <div v-if="titleEnabled" class="row-wrap">
              <div style="flex: 1; min-width: 220px">
                <label class="small muted">Proveedor para títulos</label>
                <n-select
                  v-model:value="settings.titleProvider"
                  :options="providerOptions"
                  :loading="providersLoading"
                  clearable
                  placeholder="Usar proveedor de contenido"
                  @update:value="onTitleProviderChange"
                />
              </div>
              <div style="flex: 1; min-width: 220px">
                <label class="small muted">Modelo para títulos</label>
                <n-select
                  v-if="titleModelOptions.length > 1"
                  v-model:value="settings.titleModel"
                  :options="titleModelOptions"
                  :disabled="!settings.titleProvider"
                  clearable
                  placeholder="Usar modelo de contenido"
                />
                <n-input
                  v-else
                  v-model:value="settings.titleModel"
                  :disabled="!settings.titleProvider"
                  placeholder="Ej: local-model"
                />
              </div>
            </div>
          </div>
        </n-card>

        <n-card title="Parámetros globales de traducción" size="small">
          <div class="stack-md">
            <div class="row-between">
              <div>
                <div style="font-weight: 600">Auto segmentación</div>
                <div class="small muted">Divide capítulos largos antes de enviarlos al proveedor AI.</div>
              </div>
              <n-switch v-model:value="settings.translation.autoSegment" />
            </div>

            <div class="row-wrap">
              <FieldNumber v-model="settings.translation.thresholdChars" label="Umbral auto" :min="1000" />
              <FieldNumber v-model="settings.translation.maxChars" label="Máx. por segmento" :min="500" />
              <FieldNumber v-model="settings.translation.minChars" label="Mín. por segmento" :min="100" />
            </div>
            <div class="row-wrap">
              <FieldNumber v-model="settings.translation.maxRetries" label="Máx. reintentos" :min="0" />
              <FieldNumber v-model="settings.translation.concurrency" label="Concurrencia" :min="1" />
            </div>

            <div class="row-between">
              <div>
                <div style="font-weight: 600">Enable check</div>
                <div class="small muted">Activa verificación posterior en flujos compatibles.</div>
              </div>
              <n-switch v-model:value="settings.translation.enableCheck" />
            </div>

            <div class="row-between">
              <div>
                <div style="font-weight: 600">Incluir títulos previos</div>
                <div class="small-muted">Añade títulos anteriores como contexto adicional.</div>
              </div>
              <n-switch v-model:value="settings.translation.includePreviousChapterTitles" />
            </div>
          </div>
        </n-card>

        <n-card title="Backup" size="small">
          <div class="row-between">
            <div>
              <div style="font-weight: 600">Descargar backup</div>
              <div class="small muted">Descarga un archivo .zip con la base de datos y todos los datos del servidor.</div>
            </div>
            <a href="/api/backup/download" target="_blank" rel="noopener" style="text-decoration: none">
              <n-button secondary>
                <template #icon><n-icon><DownloadOutline /></n-icon></template>
                Descargar
              </n-button>
            </a>
          </div>
        </n-card>

        <n-card title="Prompts generales" size="small">
          <n-collapse :default-expanded-names="prompts.length > 0 ? [prompts[0].id] : []">
            <n-collapse-item v-for="prompt in prompts" :key="prompt.id" :name="prompt.id">
              <template #header>
                <div style="display: flex; align-items: center; gap: 0.75rem">
                  <strong>{{ prompt.label || prompt.key }}</strong>
                  <n-tag :type="prompt.active ? 'success' : 'default'" size="small" round>
                    {{ prompt.active ? 'Activo' : 'Inactivo' }}
                  </n-tag>
                </div>
              </template>
              <div class="stack-md">
                <div>
                  <label class="small muted">System prompt</label>
                  <n-input v-model:value="prompt.prompt.systemPrompt" type="textarea" :rows="5" :style="{ fontFamily: 'monospace' }" />
                </div>
                <div>
                  <label class="small muted">User prompt</label>
                  <n-input v-model:value="prompt.prompt.userPrompt" type="textarea" :rows="5" :style="{ fontFamily: 'monospace' }" />
                </div>
                <div class="row-between">
                  <span class="small muted">Activo</span>
                  <n-switch v-model:value="prompt.active" />
                </div>
              </div>
            </n-collapse-item>
          </n-collapse>
        </n-card>

        <n-card title="Tokens de Browser Worker" size="small">
          <div class="stack-md">
            <p class="small muted" style="margin: 0">
              Tokens de autenticación para la extensión del navegador. Cada token está vinculado a tu cuenta y puede ser revocado en cualquier momento.
            </p>

            <div v-if="workerTokensLoading" style="text-align: center; padding: 1rem">
              <n-spin :size="24" />
            </div>

            <div v-else-if="workerTokens.length === 0" class="empty-tokens">
              <p>No hay tokens activos. Usa la extensión del navegador para autenticarte.</p>
            </div>

            <n-data-table
              v-else
              :columns="workerTokenColumns"
              :data="workerTokens"
              size="small"
              :bordered="false"
            />

            <div class="row-wrap" style="margin-top: 0.5rem">
              <n-button
                size="small"
                secondary
                :loading="workerTokensLoading"
                @click="loadWorkerTokens"
              >
                <template #icon><n-icon><RefreshOutline /></n-icon></template>
                Recargar
              </n-button>
            </div>
          </div>
        </n-card>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, h } from "vue";
import {
  NButton,
  NCard,
  NInput,
  NSwitch,
  NSelect,
  NTag,
  NAlert,
  NSpin,
  NCollapse,
  NCollapseItem,
  NIcon,
  NDataTable,
  useMessage,
  type DataTableColumns,
} from "naive-ui";
import {
  SaveOutline,
  DownloadOutline,
  RefreshOutline,
  BanOutline,
  TrashOutline,
} from "@vicons/ionicons5";
import AppLayout from "@/components/AppLayout.vue";
import FieldNumber from "@/components/FieldNumber.vue";
import { applyTheme } from "@/app/auth";
import { useAppServices } from "@/app/services";
import { useProviders } from "@/composables/useProviders";
import type { GeneralPromptRecord, ServerSettings, WorkerToken } from "@/api/types";

const message = useMessage();
const { api, loadProviders } = useAppServices();
const { providers, byId, loading: providersLoading, reload: reloadProviders } = useProviders();
const loading = ref(true);
const saving = ref(false);
const replacingKey = ref(false);
const deletingKey = ref(false);
const error = ref<string | null>(null);
const settings = ref<ServerSettings | null>(null);
const prompts = ref<GeneralPromptRecord[]>([]);
const providerApiKey = ref("");
const timeoutSec = ref(120);
const titleEnabled = ref(false);

const workerTokens = ref<WorkerToken[]>([]);
const workerTokensLoading = ref(false);
const revokingTokenId = ref<string | null>(null);
const deletingTokenId = ref<string | null>(null);

const themeOptions = [
  { label: "Sistema", value: "system" },
  { label: "Claro", value: "light" },
  { label: "Oscuro", value: "dark" },
];

const workerTokenColumns: DataTableColumns<WorkerToken> = [
  { title: "Etiqueta", key: "label" },
  {
    title: "Extensión",
    key: "extensionId",
    render(row) {
      return h("span", { class: "mono small" }, row.extensionId.substring(0, 12) + "...");
    },
  },
  {
    title: "Creado",
    key: "createdAt",
    render(row) {
      return formatDate(row.createdAt);
    },
  },
  {
    title: "Último uso",
    key: "lastUsedAt",
    render(row) {
      return row.lastUsedAt ? formatDate(row.lastUsedAt) : "Nunca";
    },
  },
  {
    title: "Estado",
    key: "revoked",
    render(row) {
      return h(NTag, {
        type: row.revoked ? "error" : "success",
        size: "small",
        round: true,
      }, { default: () => row.revoked ? "Revocado" : "Activo" });
    },
  },
  {
    title: "Acciones",
    key: "actions",
    width: 120,
    render(row) {
      if (row.revoked) return null;
      return h("div", { class: "row-gap" }, [
        h(NButton, {
          quaternary: true,
          circle: true,
          size: "small",
          loading: revokingTokenId.value === row.id,
          onClick: () => revokeToken(row.id),
          title: "Revocar",
        }, { icon: () => h(NIcon, null, { default: () => h(BanOutline) }) }),
        h(NButton, {
          quaternary: true,
          circle: true,
          size: "small",
          type: "error",
          loading: deletingTokenId.value === row.id,
          onClick: () => deleteToken(row.id),
          title: "Eliminar",
        }, { icon: () => h(NIcon, null, { default: () => h(TrashOutline) }) }),
      ]);
    },
  },
];

onMounted(() => {
  void load();
  void loadWorkerTokens();
});

async function loadWorkerTokens() {
  workerTokensLoading.value = true;
  try {
    workerTokens.value = await api.workerTokens.list();
  } catch (err) {
    console.error("Failed to load worker tokens:", err);
  } finally {
    workerTokensLoading.value = false;
  }
}

async function revokeToken(tokenId: string) {
  revokingTokenId.value = tokenId;
  try {
    await api.workerTokens.revoke(tokenId);
    await loadWorkerTokens();
    message.success("Token revocado");
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err);
  } finally {
    revokingTokenId.value = null;
  }
}

async function deleteToken(tokenId: string) {
  deletingTokenId.value = tokenId;
  try {
    await api.workerTokens.delete(tokenId);
    await loadWorkerTokens();
    message.success("Token eliminado");
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err);
  } finally {
    deletingTokenId.value = null;
  }
}

async function load() {
  loading.value = true;
  error.value = null;
  try {
    const [settingsResponse, promptsResponse] = await Promise.all([
      api.settings.get(),
      api.prompts.list(),
      reloadProviders(),
    ]);
    settings.value = settingsResponse;
    prompts.value = promptsResponse;
    providerApiKey.value = "";
    timeoutSec.value = settingsResponse.ai.timeoutMs
      ? Math.round(settingsResponse.ai.timeoutMs / 1000)
      : 120;
    titleEnabled.value = Boolean(settingsResponse.titleProvider);
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err);
  } finally {
    loading.value = false;
  }
}

const providerOptions = computed(() => providers.value.map((p) => ({ label: p.name, value: p.id })));
const activeProvider = computed(() => {
  if (!settings.value) return null;
  return providers.value.find((provider) => provider.id === settings.value?.ai.provider) ?? null;
});
const providerConfigured = computed(() => Boolean(activeProvider.value?.apiKeyConfigured));
const modelOptions = computed(() => {
  if (!settings.value) return [];
  const info = byId.value.get(settings.value.ai.provider);
  return (info?.models ?? []).map((m) => ({ label: m, value: m }));
});
const titleModelOptions = computed(() => {
  if (!settings.value) return [];
  const info = byId.value.get(settings.value.titleProvider);
  return (info?.models ?? []).map((m) => ({ label: m, value: m }));
});

function onProviderChange() {
  if (!settings.value) return;
  const info = byId.value.get(settings.value.ai.provider);
  if (!info) return;
  if (!info.models.includes(settings.value.ai.model)) {
    settings.value.ai.model = info.defaultModel;
  }
  settings.value.ai.baseUrl = info.baseUrl;
  providerApiKey.value = "";
}

function onTitleProviderChange() {
  if (!settings.value) return;
  const info = byId.value.get(settings.value.titleProvider);
  if (!info) return;
  if (!info.models.includes(settings.value.titleModel)) {
    settings.value.titleModel = info.defaultModel;
  }
}

async function replaceKey() {
  if (!settings.value || !providerApiKey.value.trim()) return;
  replacingKey.value = true;
  try {
    await api.providers.replaceKey(settings.value.ai.provider, providerApiKey.value.trim());
    providerApiKey.value = "";
    await loadProviders();
    message.success("API key actualizada");
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err);
  } finally {
    replacingKey.value = false;
  }
}

async function deleteKey() {
  if (!settings.value) return;
  deletingKey.value = true;
  try {
    await api.providers.deleteKey(settings.value.ai.provider);
    await loadProviders();
    message.success("API key eliminada");
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err);
  } finally {
    deletingKey.value = false;
  }
}

async function save() {
  if (!settings.value) return;
  saving.value = true;
  error.value = null;
  try {
    settings.value.ai.timeoutMs = timeoutSec.value * 1000;
    if (!titleEnabled.value) {
      settings.value.titleProvider = "";
      settings.value.titleModel = "";
    }
    settings.value = await api.settings.update(settings.value);
    applyTheme(settings.value.theme);
    await Promise.all([
      api.providers.update(settings.value.ai.provider, {
        model: settings.value.ai.model,
        baseUrl: settings.value.ai.baseUrl,
        timeoutMs: settings.value.ai.timeoutMs,
      }),
      Promise.all(
        prompts.value.map((prompt) =>
          api.prompts.upsert({
            key: prompt.key,
            label: prompt.label,
            description: prompt.description,
            prompt: prompt.prompt,
            active: prompt.active,
          }),
        ),
      ),
    ]);
    await Promise.all([reloadProviders(), load()]);
    message.success("Configuración guardada");
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err);
  } finally {
    saving.value = false;
  }
}

function formatDate(value?: string) {
  if (!value) return "";
  return new Date(value).toLocaleString();
}
</script>
