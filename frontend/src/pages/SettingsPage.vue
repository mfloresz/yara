<template>
  <AppLayout>
    <div class="settings-page">
      <div class="settings-header">
        <div class="settings-header-text">
          <h1>Configuración</h1>
          <p class="muted">Toda la configuración es por usuario y se persiste en el backend.</p>
        </div>
        <div class="row-gap">
          <n-tag v-if="dirty" type="warning" size="small" round>Cambios sin guardar</n-tag>
          <n-button type="primary" :loading="saving" :disabled="!settings || !dirty" @click="save">
            <template #icon><n-icon><SaveOutline /></n-icon></template>
            Guardar
          </n-button>
        </div>
      </div>

      <n-alert v-if="error" type="error" :title="error" style="margin-bottom: 1rem" />
      <n-spin v-if="loading" style="display: flex; justify-content: center; padding: 2rem" :size="48" />

      <template v-else-if="settings">
        <div class="settings-body">
          <nav class="settings-nav" aria-label="Secciones de configuración">
            <button
              v-for="section in sections"
              :key="section.id"
              type="button"
              class="settings-nav-item"
              :class="{ 'settings-nav-item--active': activeSection === section.id }"
              :aria-current="activeSection === section.id ? 'true' : undefined"
              @click="goTo(section.id)"
            >
              <n-icon :size="17"><component :is="section.icon" /></n-icon>
              <span>{{ section.label }}</span>
            </button>
          </nav>

          <div class="settings-content">
            <SettingsSection
              id="settings-general"
              title="General"
              description="Apariencia de la interfaz."
            >
              <label class="small muted" for="theme-select">Tema</label>
              <n-select id="theme-select" v-model:value="settings.theme" :options="themeOptions" style="max-width: 320px" />
            </SettingsSection>

            <SettingsSection
              id="settings-provider"
              title="Proveedor de IA"
              description="Conexión, autenticación y rendimiento del proveedor activo."
            >
              <div class="settings-block-label">Conexión</div>
              <div class="row-wrap">
                <div style="flex: 1; min-width: 220px">
                  <label class="small muted" for="provider-select">Proveedor activo</label>
                  <n-select
                    id="provider-select"
                    v-model:value="settings.ai.provider"
                    :options="providerOptions"
                    :loading="providersLoading"
                    @update:value="onProviderChange"
                  />
                </div>
                <div style="flex: 1; min-width: 220px">
                  <label class="small muted" for="model-select">Modelo</label>
                  <n-select
                    v-if="modelOptions.length > 1"
                    id="model-select"
                    v-model:value="settings.ai.model"
                    :options="modelOptions"
                    :disabled="!settings.ai.provider"
                    filterable
                    tag
                  />
                  <n-input
                    v-else
                    id="model-select"
                    v-model:value="settings.ai.model"
                    :disabled="!settings.ai.provider"
                    placeholder="Ej: local-model"
                  />
                </div>
              </div>
              <div class="row-wrap">
                <div style="flex: 1; min-width: 220px">
                  <label class="small muted" for="base-url">Base URL</label>
                  <n-input id="base-url" v-model:value="settings.ai.baseUrl" :style="{ fontFamily: 'monospace' }" />
                </div>
                <FieldNumber v-model.number="timeoutSec" label="Timeout (segundos)" :min="10" wrapper-style="min-width: 180px; flex: 1" />
              </div>

              <n-divider style="margin: 1rem 0" />

              <div class="settings-block-label">Autenticación</div>
              <ProviderKeyField
                v-model="providerApiKey"
                :configured="providerConfigured"
                :shared-key-available="Boolean(activeProvider?.sharedKeyAvailable)"
                :updated-at="activeProvider?.apiKeyUpdatedAt"
                :replacing="replacingKey"
                :deleting="deletingKey"
                @replace="replaceKey"
                @delete="deleteKey"
              />

              <n-divider style="margin: 1rem 0" />

              <div class="settings-block-label">Rendimiento</div>
              <SettingsRow
                title="Traducción concurrente"
                description="Jobs paralelos por capítulo. Más de 5 rara vez mejora el throughput porque SQLite serializa escrituras. Desactivado = secuencial."
              >
                <n-switch v-model:value="concurrencyEnabled" aria-label="Activar traducción concurrente" />
              </SettingsRow>
              <n-collapse-transition :show="concurrencyEnabled">
                <div class="concurrency-slider">
                  <div class="row-between">
                    <span class="small muted">Jobs paralelos</span>
                    <n-tag size="small" round>{{ concurrencyCount }}</n-tag>
                  </div>
                  <n-slider v-model:value="concurrencyCount" :min="2" :max="10" :step="1" />
                  <div class="small muted">Recomendado 3–5 para OpenRouter / Opencode Go.</div>
                </div>
              </n-collapse-transition>
            </SettingsSection>

            <SettingsSection
              id="settings-translation"
              title="Traducción"
              description="Modelo de títulos, segmentación y verificaciones."
            >
              <SettingsRow
                title="Modelo diferente para títulos"
                description="Un modelo pequeño y económico para traducir solo títulos. Si falla, se usa el modelo de contenido."
              >
                <n-switch v-model:value="titleEnabled" aria-label="Usar modelo diferente para títulos" />
              </SettingsRow>
              <n-collapse-transition :show="titleEnabled">
                <div class="row-wrap" style="padding-bottom: 0.5rem">
                  <div style="flex: 1; min-width: 220px">
                    <label class="small muted" for="title-provider">Proveedor para títulos</label>
                    <n-select
                      id="title-provider"
                      v-model:value="settings.titleProvider"
                      :options="providerOptions"
                      :loading="providersLoading"
                      clearable
                      placeholder="Usar proveedor de contenido"
                      @update:value="onTitleProviderChange"
                    />
                  </div>
                  <div style="flex: 1; min-width: 220px">
                    <label class="small muted" for="title-model">Modelo para títulos</label>
                    <n-select
                      v-if="titleModelOptions.length > 1"
                      id="title-model"
                      v-model:value="settings.titleModel"
                      :options="titleModelOptions"
                      :disabled="!settings.titleProvider"
                      clearable
                      filterable
                      tag
                      placeholder="Usar modelo de contenido"
                    />
                    <n-input
                      v-else
                      id="title-model"
                      v-model:value="settings.titleModel"
                      :disabled="!settings.titleProvider"
                      placeholder="Ej: local-model"
                    />
                  </div>
                </div>
              </n-collapse-transition>

              <n-divider style="margin: 1rem 0" />

              <SettingsRow
                title="Auto segmentación"
                description="Divide capítulos largos antes de enviarlos al proveedor."
              >
                <n-switch v-model:value="settings.translation.autoSegment" aria-label="Activar auto segmentación" />
              </SettingsRow>
              <div class="row-wrap" :class="{ 'settings-disabled': !settings.translation.autoSegment }">
                <FieldNumber v-model="settings.translation.thresholdChars" label="Umbral auto" :min="1000" />
                <FieldNumber v-model="settings.translation.maxChars" label="Máx. por segmento" :min="500" />
                <FieldNumber v-model="settings.translation.minChars" label="Mín. por segmento" :min="100" />
              </div>
              <div class="row-wrap">
                <FieldNumber v-model="settings.translation.maxRetries" label="Máx. reintentos" :min="0" />
              </div>

              <n-divider style="margin: 1rem 0" />

              <SettingsRow
                title="Verificación posterior"
                description="Activa verificación en flujos compatibles."
              >
                <n-switch v-model:value="settings.translation.enableCheck" aria-label="Activar verificación posterior" />
              </SettingsRow>
              <SettingsRow
                title="Incluir títulos previos"
                description="Añade títulos anteriores como contexto adicional."
              >
                <n-switch v-model:value="settings.translation.includePreviousChapterTitles" aria-label="Incluir títulos previos" />
              </SettingsRow>
            </SettingsSection>

            <SettingsSection
              id="settings-prompts"
              :title="`Prompts generales (${prompts.length})`"
              description="Personaliza system y user prompts. Restablece al valor global cuando quieras."
            >
              <n-empty v-if="prompts.length === 0" description="Sin prompts personalizables" />
              <n-collapse v-else :default-expanded-names="prompts.length > 0 ? [prompts[0].id] : []">
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
                    <SettingsRow title="Activo" description="Incluye este prompt en los flujos de traducción.">
                      <n-switch v-model:value="prompt.active" :aria-label="`Activar prompt ${prompt.label || prompt.key}`" />
                    </SettingsRow>
                    <div class="row-between">
                      <span class="small muted">Restablece tu personalización al valor global</span>
                      <n-button size="small" quaternary :loading="resettingPrompt === prompt.key" @click="resetPrompt(prompt.key)">Restablecer</n-button>
                    </div>
                  </div>
                </n-collapse-item>
              </n-collapse>
            </SettingsSection>

            <SettingsSection
              id="settings-worker"
              :title="`Tokens de Browser Worker (${workerTokens.length})`"
              description="Autenticación para la extensión del navegador. Cada token está vinculado a tu cuenta y puede revocarse."
            >
              <div v-if="workerTokensLoading" style="text-align: center; padding: 1rem">
                <n-spin :size="24" />
              </div>

              <n-empty v-else-if="workerTokens.length === 0" description="No hay tokens activos. Usa la extensión del navegador para autenticarte." />

              <div v-else class="token-list">
                <div v-for="token in workerTokens" :key="token.id" class="token-row">
                  <div class="token-main">
                    <div class="token-label">{{ token.label }}</div>
                    <div class="small muted mono">{{ token.extensionId.substring(0, 12) }}… · creado {{ formatDateTime(token.createdAt) }} · {{ token.lastUsedAt ? `usado ${formatDateTime(token.lastUsedAt)}` : 'nunca usado' }}</div>
                  </div>
                  <n-tag :type="token.revoked ? 'error' : 'success'" size="small" round>
                    {{ token.revoked ? 'Revocado' : 'Activo' }}
                  </n-tag>
                  <div v-if="!token.revoked" class="row-gap">
                    <n-button
                      quaternary
                      circle
                      size="small"
                      class="touch-target"
                      :loading="revokingTokenId === token.id"
                      :aria-label="`Revocar token ${token.label}`"
                      title="Revocar"
                      @click="revokeToken(token.id)"
                    >
                      <template #icon><n-icon><BanOutline /></n-icon></template>
                    </n-button>
                    <n-button
                      quaternary
                      circle
                      size="small"
                      type="error"
                      class="touch-target"
                      :loading="deletingTokenId === token.id"
                      :aria-label="`Eliminar token ${token.label}`"
                      title="Eliminar"
                      @click="deleteToken(token.id)"
                    >
                      <template #icon><n-icon><TrashOutline /></n-icon></template>
                    </n-button>
                  </div>
                </div>
              </div>

              <div class="row-wrap" style="margin-top: 0.75rem">
                <n-button size="small" secondary :loading="workerTokensLoading" @click="loadWorkerTokens">
                  <template #icon><n-icon><RefreshOutline /></n-icon></template>
                  Recargar
                </n-button>
              </div>
            </SettingsSection>

            <SettingsSection
              id="settings-account"
              title="Cuenta y sesión"
              description="Acciones que afectan a todos tus dispositivos."
              danger
            >
              <SettingsRow
                title="Cerrar sesión en todos los dispositivos"
                description="Invalida todos los tokens de tu cuenta, incluidas otras sesiones y navegadores. Este dispositivo también se desconecta."
              >
                <n-button secondary type="warning" :loading="loggingOutEverywhere" @click="logoutEverywhere">
                  Cerrar todas las sesiones
                </n-button>
              </SettingsRow>
            </SettingsSection>
          </div>
        </div>

        <div class="settings-mobile-bar">
          <n-tag v-if="dirty" type="warning" size="small" round>Sin guardar</n-tag>
          <n-button type="primary" block :loading="saving" :disabled="!dirty" @click="save">
            <template #icon><n-icon><SaveOutline /></n-icon></template>
            Guardar cambios
          </n-button>
        </div>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import {
  NAlert,
  NButton,
  NCollapse,
  NCollapseItem,
  NCollapseTransition,
  NDivider,
  NEmpty,
  NIcon,
  NInput,
  NSelect,
  NSlider,
  NSpin,
  NSwitch,
  NTag,
  useMessage,
} from "naive-ui";
import {
  BanOutline,
  DocumentTextOutline,
  GlobeOutline,
  LanguageOutline,
  PersonOutline,
  RefreshOutline,
  SaveOutline,
  ServerOutline,
  SettingsOutline,
  TrashOutline,
} from "@vicons/ionicons5";
import AppLayout from "@/components/AppLayout.vue";
import FieldNumber from "@/components/FieldNumber.vue";
import ProviderKeyField from "@/components/ProviderKeyField.vue";
import SettingsRow from "@/components/SettingsRow.vue";
import SettingsSection from "@/components/SettingsSection.vue";
import { applyTheme } from "@/app/auth";
import { useAppServices } from "@/app/services";
import { useProviders } from "@/composables/useProviders";
import { useRouter } from "vue-router";
import type { GeneralPromptRecord, ServerSettings, WorkerToken } from "@/api/types";

const message = useMessage();
const router = useRouter();
const { api, loadProviders, logoutAll } = useAppServices();
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
const concurrencyEnabled = ref(false);
const concurrencyCount = ref(3);

const workerTokens = ref<WorkerToken[]>([]);
const workerTokensLoading = ref(false);
const revokingTokenId = ref<string | null>(null);
const deletingTokenId = ref<string | null>(null);

const loggingOutEverywhere = ref(false);

const sections = [
  { id: "general", label: "General", icon: SettingsOutline },
  { id: "provider", label: "Proveedor IA", icon: ServerOutline },
  { id: "translation", label: "Traducción", icon: LanguageOutline },
  { id: "prompts", label: "Prompts", icon: DocumentTextOutline },
  { id: "worker", label: "Browser Worker", icon: GlobeOutline },
  { id: "account", label: "Cuenta", icon: PersonOutline },
] as const;

type SectionId = (typeof sections)[number]["id"];

const activeSection = ref<SectionId>("general");
let spy: IntersectionObserver | null = null;

function goTo(id: SectionId) {
  activeSection.value = id;
  document.getElementById(`settings-${id}`)?.scrollIntoView({ behavior: "smooth", block: "start" });
}

function setupSpy() {
  spy?.disconnect();
  const els = sections
    .map((s) => document.getElementById(`settings-${s.id}`))
    .filter((el): el is HTMLElement => el !== null);
  if (els.length === 0 || typeof IntersectionObserver === "undefined") return;
  spy = new IntersectionObserver(
    (entries) => {
      for (const entry of entries) {
        if (entry.isIntersecting) {
          activeSection.value = entry.target.id.replace("settings-", "") as SectionId;
        }
      }
    },
    { rootMargin: "-30% 0px -60% 0px" },
  );
  els.forEach((el) => spy?.observe(el));
}

onBeforeUnmount(() => spy?.disconnect());

watch(settings, (value) => {
  if (value) void nextTick(() => setupSpy());
});

async function logoutEverywhere() {
  loggingOutEverywhere.value = true;
  try {
    await logoutAll();
    message.success("Todas las sesiones fueron cerradas");
    await router.push("/login");
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err);
  } finally {
    loggingOutEverywhere.value = false;
  }
}

const themeOptions = [
  { label: "Sistema", value: "system" },
  { label: "Claro", value: "light" },
  { label: "Oscuro", value: "dark" },
];

onMounted(() => {
  void load();
  void loadWorkerTokens();
});

async function loadWorkerTokens() {
  workerTokensLoading.value = true;
  try {
    workerTokens.value = await api.workerTokens.list();
  } catch {
    // list stays empty; user can retry
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

const baseline = ref("");

function snapshot() {
  return JSON.stringify([
    settings.value,
    prompts.value.map((p) => ({ key: p.key, active: p.active, system: p.prompt.systemPrompt, user: p.prompt.userPrompt })),
    timeoutSec.value,
    titleEnabled.value,
    concurrencyEnabled.value,
    concurrencyCount.value,
  ]);
}

const dirty = computed(() => baseline.value !== "" && baseline.value !== snapshot());

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
    const cc = settingsResponse.ai.concurrency ?? 1;
    concurrencyEnabled.value = cc > 1;
    concurrencyCount.value = cc > 1 ? Math.min(10, Math.max(2, cc)) : 3;
    baseline.value = snapshot();
    await nextTick();
    setupSpy();
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
  // sync concurrency from the selected provider's stored settings
  const prov = providers.value.find((p) => p.id === settings.value!.ai.provider);
  const cc = prov?.concurrency ?? 1;
  concurrencyEnabled.value = cc > 1;
  concurrencyCount.value = cc > 1 ? Math.min(10, Math.max(2, cc)) : 3;
  if (settings.value) {
    settings.value.ai.concurrency = concurrencyEnabled.value ? concurrencyCount.value : 1;
  }
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
    settings.value.ai.concurrency = concurrencyEnabled.value
      ? Math.min(10, Math.max(2, concurrencyCount.value || 3))
      : 1;
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
        concurrency: settings.value.ai.concurrency,
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

const resettingPrompt = ref<string | null>(null);

async function resetPrompt(key: GeneralPromptRecord["key"]) {
  resettingPrompt.value = key;
  try {
    await api.prompts.reset(key);
    prompts.value = await api.prompts.list();
    baseline.value = snapshot();
    message.success("Prompt restablecido al valor global");
  } catch (err) {
    message.error(`Error al restablecer: ${err instanceof Error ? err.message : String(err)}`);
  } finally {
    resettingPrompt.value = null;
  }
}

function formatDateTime(value?: string) {
  if (!value) return "";
  return new Date(value).toLocaleString();
}
</script>

<style scoped>
.settings-page {
  max-width: 1120px;
  margin: 0 auto;
}

.settings-page h1 {
  margin: 0 0 0.25rem;
  font-size: 1.75rem;
  font-weight: 700;
  letter-spacing: -0.02em;
}

.settings-header-text p {
  margin: 0;
}

.settings-header {
  position: sticky;
  top: 56px;
  z-index: 20;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  flex-wrap: wrap;
  padding: 0.75rem 0;
  margin-bottom: 1.25rem;
  background: color-mix(in oklab, var(--surface-elevated) 92%, transparent);
  backdrop-filter: blur(12px);
  border-bottom: 1px solid var(--divide);
}

.settings-body {
  display: grid;
  grid-template-columns: 230px minmax(0, 1fr);
  gap: 2rem;
  align-items: start;
}

.settings-nav {
  position: sticky;
  top: 148px;
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
}

.settings-nav-item {
  display: flex;
  align-items: center;
  gap: 0.625rem;
  width: 100%;
  padding: 0.625rem 0.75rem;
  border: none;
  border-radius: var(--radius-md);
  background: transparent;
  color: var(--text-secondary);
  font-size: 0.9375rem;
  font-weight: 500;
  cursor: pointer;
  text-align: left;
  transition: background 0.15s ease, color 0.15s ease;
  min-height: 44px;
}

.settings-nav-item:hover {
  background: var(--mock-row);
  color: var(--foreground);
}

.settings-nav-item--active {
  background: var(--mock-row);
  color: var(--foreground);
  font-weight: 700;
}

.settings-content {
  display: flex;
  flex-direction: column;
  gap: 1.5rem;
  min-width: 0;
}

.settings-block-label {
  font-size: 0.75rem;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--text-tertiary);
  margin-bottom: 0.5rem;
}

.settings-disabled {
  opacity: 0.55;
  pointer-events: none;
}

.concurrency-slider {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  padding: 0.5rem 0 0.25rem;
  max-width: 420px;
}

.token-list {
  display: flex;
  flex-direction: column;
  border: 1px solid var(--divide);
  border-radius: var(--radius-md);
  overflow: hidden;
}

.token-row {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  padding: 0.625rem 0.75rem;
  background: var(--surface-base);
}

.token-row + .token-row {
  border-top: 1px solid var(--divide);
}

.token-main {
  flex: 1;
  min-width: 0;
}

.token-label {
  font-weight: 600;
  font-size: 0.9375rem;
}

.settings-mobile-bar {
  display: none;
}

@media (max-width: 900px) {
  .settings-page h1 {
    font-size: 1.5rem;
  }

  .settings-header {
    top: 52px;
  }

  .settings-body {
    grid-template-columns: 1fr;
    gap: 1rem;
  }

  .settings-nav {
    top: 129px;
    z-index: 15;
    flex-direction: row;
    overflow-x: auto;
    padding: 0.5rem 0;
    margin: 0 -1rem;
    padding-left: 1rem;
    padding-right: 1rem;
    background: color-mix(in oklab, var(--surface-elevated) 92%, transparent);
    backdrop-filter: blur(12px);
    border-bottom: 1px solid var(--divide);
  }

  .settings-nav-item {
    width: auto;
    white-space: nowrap;
    border: 1px solid var(--divide);
    border-radius: var(--radius-pill);
    padding: 0.5rem 0.875rem;
    background: var(--surface-base);
  }

  .settings-nav-item--active {
    background: var(--mock-row-strong);
  }

  .settings-mobile-bar {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    position: sticky;
    bottom: 0;
    padding: 0.75rem 0 calc(0.75rem + env(safe-area-inset-bottom));
    margin-top: 1.5rem;
    background: color-mix(in oklab, var(--surface-elevated) 94%, transparent);
    backdrop-filter: blur(12px);
    border-top: 1px solid var(--divide);
  }
}

@media (prefers-reduced-motion: reduce) {
  .settings-nav-item {
    transition: none;
  }
}
</style>
