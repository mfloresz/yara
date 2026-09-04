<template>
  <AppLayout>
    <div class="admin-page">
    <div class="page-head">
      <h1 class="page-title">Administración</h1>
      <p class="small muted">Gestión de usuarios, invitaciones y recursos compartidos.</p>
    </div>

    <n-tabs type="line" animated>
      <!-- Usuarios -->
      <n-tab-pane name="users" tab="Usuarios">
        <n-data-table
          :columns="userColumns"
          :data="users"
          :loading="loading"
          :bordered="false"
          size="small"
        />
      </n-tab-pane>

      <!-- Invitaciones -->
      <n-tab-pane name="invitations" tab="Invitaciones">
        <div class="stack-md">
          <n-card size="small" title="Nueva invitación">
            <div class="invite-form">
              <n-input v-model:value="inviteEmail" type="text" placeholder="email@ejemplo.com" />
              <n-select v-model:value="inviteRole" :options="roleOptions" style="width: 140px" />
              <n-button type="primary" :loading="inviteLoading" @click="createInvitation">
                Crear
              </n-button>
            </div>
            <n-alert v-if="createdInvitationUrl" type="success" closable class="invite-url-alert">
              <template #header>Invitación creada — cópiala ahora</template>
              <span class="invite-url">{{ createdInvitationUrl }}</span>
              <n-button size="tiny" style="margin-left: 0.5rem" @click="copyInvitationUrl">
                Copiar
              </n-button>
            </n-alert>
            <n-alert v-if="inviteError" type="error" :title="inviteError" />
          </n-card>

          <n-data-table
            :columns="invitationColumns"
            :data="invitations"
            :loading="loading"
            :bordered="false"
            size="small"
          />
        </div>
      </n-tab-pane>

      <!-- Claves de proveedor -->
      <n-tab-pane name="provider-keys" tab="Claves de proveedor">
        <n-alert type="info" class="section-note">
          Una clave compartida se usa cuando el usuario no ha configurado la suya
          propia. La clave propia del usuario siempre tiene prioridad.
        </n-alert>
        <n-data-table
          :columns="providerKeyColumns"
          :data="providerKeys"
          :loading="loading"
          :bordered="false"
          size="small"
        />
      </n-tab-pane>

      <!-- Prompts globales -->
      <n-tab-pane name="prompts" tab="Prompts globales">
        <n-alert type="info" class="section-note">
          Los prompts globales son los valores por defecto para los usuarios que
          no han personalizado los suyos. Los prompts personalizados por novela
          siempre tienen prioridad.
        </n-alert>
        <div class="stack-md">
          <n-card v-for="item in promptItems" :key="item.key" size="small" :title="item.label">
            <div class="stack-md">
              <div>
                <label class="small muted">System prompt</label>
                <n-input
                  v-model:value="item.systemPrompt"
                  type="textarea"
                  :autosize="{ minRows: 3, maxRows: 12 }"
                />
              </div>
              <div>
                <label class="small muted">User prompt</label>
                <n-input
                  v-model:value="item.userPrompt"
                  type="textarea"
                  :autosize="{ minRows: 2, maxRows: 8 }"
                />
              </div>
              <div class="prompt-actions">
                <n-tag v-if="item.hasOverride" size="small" type="warning">Con override</n-tag>
                <n-tag v-else size="small">Valor integrado</n-tag>
                <n-button size="small" type="primary" :loading="item.saving" @click="savePrompt(item)">
                  Guardar
                </n-button>
                <n-button
                  v-if="item.hasOverride"
                  size="small"
                  quaternary
                  @click="deletePrompt(item)"
                >
                  Eliminar override
                </n-button>
              </div>
            </div>
          </n-card>
        </div>
      </n-tab-pane>
      <!-- Backup -->
      <n-tab-pane name="backup" tab="Backup">
        <n-card size="small" title="Backup del servidor">
          <div class="row-between">
            <div>
              <div style="font-weight: 600">Descargar backup</div>
              <div class="small muted">Descarga un archivo .zip con la base de datos y todos los datos del servidor.</div>
            </div>
            <a href="#" @click.prevent="downloadBackup" style="text-decoration: none">
              <n-button secondary>
                <template #icon><n-icon><DownloadOutline /></n-icon></template>
                Descargar
              </n-button>
            </a>
          </div>
        </n-card>
      </n-tab-pane>
    </n-tabs>
  </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { h, onMounted, reactive, ref } from "vue";
import {
  NAlert,
  NButton,
  NCard,
  NDataTable,
  NIcon,
  NInput,
  NSelect,
  NSwitch,
  NTabPane,
  NTabs,
  NTag,
  useMessage,
  type DataTableColumns,
} from "naive-ui";
import { DownloadOutline } from "@vicons/ionicons5";
import type { AdminInvitation, AdminProviderKey, AdminUser } from "@/api/types";
import { useAppServices } from "@/app/services";
import AppLayout from "@/components/AppLayout.vue";

const { api } = useAppServices();
const message = useMessage();

const loading = ref(false);
const users = ref<AdminUser[]>([]);
const invitations = ref<AdminInvitation[]>([]);
const providerKeys = ref<AdminProviderKey[]>([]);

const inviteEmail = ref("");
const inviteRole = ref("user");
const inviteLoading = ref(false);
const inviteError = ref<string | null>(null);
const createdInvitationUrl = ref("");

const roleOptions = [
  { label: "Usuario", value: "user" },
  { label: "Administrador", value: "admin" },
];

const promptItems = reactive(
  [
    { key: "translation", label: "Traducción" },
    { key: "title", label: "Traducción de título" },
    { key: "refine", label: "Refinamiento" },
    { key: "check", label: "Verificación" },
    { key: "glossary", label: "Glosario" },
  ].map((entry) => ({
    ...entry,
    systemPrompt: "",
    userPrompt: "",
    hasOverride: false,
    saving: false,
  })),
);

const userColumns: DataTableColumns<AdminUser> = [
  { title: "Email", key: "email" },
  { title: "Nombre", key: "name" },
  {
    title: "Rol",
    key: "role",
    render(row) {
      return h(
        NTag,
        { size: "small", type: row.role === "admin" ? "warning" : "default" },
        { default: () => (row.role === "admin" ? "Admin" : "Usuario") },
      );
    },
  },
  {
    title: "Acciones",
    key: "actions",
    render(row) {
      const promote = h(
        NButton,
        {
          size: "tiny",
          quaternary: true,
          disabled: row.role === "admin",
          onClick: () => changeRole(row, "admin"),
        },
        { default: () => "Hacer admin" },
      );
      const demote = h(
        NButton,
        {
          size: "tiny",
          quaternary: true,
          type: "warning",
          disabled: row.role === "user",
          onClick: () => changeRole(row, "user"),
        },
        { default: () => "Quitar admin" },
      );
      return h("div", { class: "row-actions" }, [promote, demote]);
    },
  },
];

const invitationColumns: DataTableColumns<AdminInvitation> = [
  { title: "Email", key: "email" },
  {
    title: "Rol",
    key: "role",
    render(row) {
      return row.role === "admin" ? "Admin" : "Usuario";
    },
  },
  {
    title: "Estado",
    key: "status",
    render(row) {
      if (row.usedAt) return "Usada";
      return "Pendiente";
    },
  },
  { title: "Expira", key: "expiresAt" },
  {
    title: "Acciones",
    key: "actions",
    render(row) {
      if (row.usedAt) return "";
      return h(
        NButton,
        {
          size: "tiny",
          quaternary: true,
          type: "error",
          onClick: () => deleteInvitation(row),
        },
        { default: () => "Revocar" },
      );
    },
  },
];

const providerKeyDrafts = reactive<Record<string, { apiKey: string }>>({});

function providerKeyColumnsFinal(): DataTableColumns<AdminProviderKey> {
  return [
    { title: "Proveedor", key: "label" },
    { title: "ID", key: "provider" },
    {
      title: "Clave",
      key: "configured",
      render(row) {
        return row.configured ? "Configurada" : "Sin clave";
      },
    },
    {
      title: "Compartida",
      key: "shared",
      render(row) {
        return h(NSwitch, {
          value: row.shared,
          size: "small",
          onUpdateValue: (value: boolean) => toggleShared(row, value),
        });
      },
    },
    {
      title: "Nueva clave",
      key: "apiKey",
      render(row) {
        if (!providerKeyDrafts[row.provider]) {
          providerKeyDrafts[row.provider] = { apiKey: "" };
        }
        return h(NInput, {
          value: providerKeyDrafts[row.provider].apiKey,
          type: "password",
          showPasswordOn: "click",
          placeholder: "sk-…",
          onUpdateValue: (value: string) => {
            providerKeyDrafts[row.provider].apiKey = value;
          },
        });
      },
    },
    {
      title: "Acciones",
      key: "actions",
      render(row) {
        return h("div", { class: "row-actions" }, [
          h(
            NButton,
            {
              size: "tiny",
              type: "primary",
              onClick: () => saveProviderKey(row),
            },
            { default: () => "Guardar clave" },
          ),
          h(
            NButton,
            {
              size: "tiny",
              quaternary: true,
              type: "error",
              disabled: !row.configured,
              onClick: () => deleteProviderKey(row),
            },
            { default: () => "Eliminar clave" },
          ),
        ]);
      },
    },
  ];
}

const providerKeyColumns = ref<DataTableColumns<AdminProviderKey>>(
  providerKeyColumnsFinal(),
);

onMounted(loadAll);

async function loadAll() {
  loading.value = true;
  try {
    await Promise.all([
      loadUsers(),
      loadInvitations(),
      loadProviderKeys(),
      loadPrompts(),
    ]);
  } finally {
    loading.value = false;
  }
}

async function loadUsers() {
  users.value = await api.admin.listUsers();
}

async function loadInvitations() {
  invitations.value = await api.admin.listInvitations();
}

async function loadProviderKeys() {
  providerKeys.value = await api.admin.listProviderKeys();
  providerKeyColumns.value = providerKeyColumnsFinal();
}

async function loadPrompts() {
  const prompts = await api.admin.listEffectivePrompts();
  for (const item of promptItems) {
    const found = prompts.find((p) => p.key === item.key);
    item.hasOverride = found?.hasOverride ?? false;
    item.systemPrompt = found?.systemPrompt ?? "";
    item.userPrompt = found?.userPrompt ?? "";
  }
}

async function changeRole(user: AdminUser, role: "admin" | "user") {
  try {
    await api.admin.updateUserRole(user.id, role);
    message.success(`Rol actualizado para ${user.email}`);
    await loadUsers();
  } catch (err) {
    message.error(errorMessage(err));
  }
}

async function createInvitation() {
  inviteLoading.value = true;
  inviteError.value = null;
  createdInvitationUrl.value = "";
  try {
    const result = await api.admin.createInvitation({
      email: inviteEmail.value.trim(),
      role: inviteRole.value,
    });
    createdInvitationUrl.value = result.invitationUrl;
    inviteEmail.value = "";
    await loadInvitations();
  } catch (err) {
    inviteError.value = errorMessage(err);
  } finally {
    inviteLoading.value = false;
  }
}

async function copyInvitationUrl() {
  try {
    await navigator.clipboard.writeText(createdInvitationUrl.value);
    message.success("Enlace copiado");
  } catch {
    message.error("No se pudo copiar el enlace");
  }
}

async function deleteInvitation(invitation: AdminInvitation) {
  try {
    await api.admin.deleteInvitation(invitation.id);
    await loadInvitations();
  } catch (err) {
    message.error(errorMessage(err));
  }
}

async function toggleShared(provider: AdminProviderKey, shared: boolean) {
  try {
    await api.admin.upsertProviderKey(provider.provider, { shared });
    await loadProviderKeys();
  } catch (err) {
    message.error(errorMessage(err));
  }
}

async function saveProviderKey(provider: AdminProviderKey) {
  const draft = providerKeyDrafts[provider.provider];
  const apiKey = draft?.apiKey ?? "";
  if (!apiKey.trim() && !provider.configured) {
    message.error("Introduce una clave para configurar el proveedor");
    return;
  }
  try {
    await api.admin.upsertProviderKey(provider.provider, {
      apiKey: apiKey.trim() || undefined,
      shared: provider.shared,
    });
    if (draft) draft.apiKey = "";
    message.success(`Clave actualizada para ${provider.provider}`);
    await loadProviderKeys();
  } catch (err) {
    message.error(errorMessage(err));
  }
}

async function deleteProviderKey(provider: AdminProviderKey) {
  try {
    await api.admin.deleteProviderKey(provider.provider);
    message.success(`Clave eliminada para ${provider.provider}`);
    await loadProviderKeys();
  } catch (err) {
    message.error(errorMessage(err));
  }
}

async function savePrompt(item: (typeof promptItems)[number]) {
  item.saving = true;
  try {
    await api.admin.upsertPromptOverride(item.key, {
      systemPrompt: item.systemPrompt,
      userPrompt: item.userPrompt,
    });
    await loadPrompts();
    message.success("Prompt global guardado");
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    item.saving = false;
  }
}

async function deletePrompt(item: (typeof promptItems)[number]) {
  try {
    await api.admin.deletePromptOverride(item.key);
    await loadPrompts();
    message.success("Override eliminado; se usará el valor integrado");
  } catch (err) {
    message.error(errorMessage(err));
  }
}

// v1 backup endpoint accepts POST only (the body is non-deterministic and
// the response is a streamed zip). POST and stream the response into a
// blob the browser can save.
async function downloadBackup() {
  try {
    const response = await fetch("/api/v1/admin/backups/export", {
      method: "POST",
      credentials: "include",
    });
    if (!response.ok) {
      throw new Error(`HTTP ${response.status}`);
    }
    const blob = await response.blob();
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    anchor.href = url;
    const fallbackName = `backup-${new Date().toISOString().replace(/[:.]/g, "-")}.zip`;
    const contentDisposition = response.headers.get("Content-Disposition") ?? "";
    const match = contentDisposition.match(/filename="?([^";]+)"?/);
    anchor.download = match?.[1] ?? fallbackName;
    document.body.appendChild(anchor);
    anchor.click();
    anchor.remove();
    URL.revokeObjectURL(url);
    message.success("Backup descargado");
  } catch (err) {
    message.error(`Error al descargar backup: ${err instanceof Error ? err.message : String(err)}`);
  }
}

function errorMessage(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err);
}
</script>

<style scoped>
.admin-page {
  display: grid;
  gap: 1rem;
  padding-top: 1.5rem;
  padding-bottom: 2rem;
}

.page-head {
  display: grid;
  gap: 0.25rem;
}

.page-title {
  font-size: 1.375rem;
  font-weight: 700;
  margin: 0;
}

.stack-md {
  display: grid;
  gap: 0.875rem;
}

.section-note {
  margin-bottom: 0.875rem;
}

.invite-form {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}

.invite-url-alert {
  margin-top: 0.75rem;
}

.invite-url {
  word-break: break-all;
}

.prompt-actions {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}

.row-actions {
  display: flex;
  gap: 0.25rem;
}
</style>
