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
        <n-alert type="info" class="section-note">
          Haz clic en un usuario para ver sus novelas, bloquearlo, generar un enlace de recuperación o eliminarlo.
        </n-alert>
        <n-data-table
          :columns="userColumns"
          :data="users"
          :loading="loading"
          :bordered="false"
          :row-props="userRowProps"
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

    <!-- Drawer derecho con stats y acciones del usuario. -->
    <n-drawer v-model:show="drawerOpen" :width="420" placement="right">
      <n-drawer-content :title="selectedUser ? selectedUser.email : 'Usuario'" closable>
        <div v-if="statsLoading">Cargando…</div>
        <div v-else-if="userStats" class="stack-md">
          <div class="row-between">
            <div>
              <div style="font-weight: 600">{{ userStats.user.email }}</div>
              <div class="small muted">{{ userStats.user.name }} · {{ userStats.user.role === "admin" ? "Admin" : "Usuario" }}</div>
            </div>
            <n-tag :type="userStats.user.blocked ? 'error' : 'success'" size="small">
              {{ userStats.user.blocked ? "Bloqueado" : "Activo" }}
            </n-tag>
          </div>
          <div class="small">
            {{ userStats.ownedCount }} novelas propias · {{ userStats.sharedCount }} compartidas (públicas)
          </div>
          <n-collapse>
            <n-collapse-item :title="`Novelas (${userStats.novels.length})`" name="novels">
              <div v-for="novel in userStats.novels" :key="novel.id" class="row-between" style="padding: 0.25rem 0">
                <span>{{ novel.targetTitle || novel.sourceTitle }}</span>
                <n-tag :type="novel.isPublic ? 'info' : 'default'" size="small">
                  {{ novel.isPublic ? "Pública" : "Privada" }}
                </n-tag>
              </div>
              <div v-if="userStats.novels.length === 0" class="small muted">Sin novelas.</div>
            </n-collapse-item>
          </n-collapse>
          <div class="stack-md">
            <n-button
              :type="userStats.user.blocked ? 'primary' : 'warning'"
              secondary
              :loading="blockLoading"
              @click="toggleBlock"
            >
              {{ userStats.user.blocked ? "Desbloquear" : "Bloquear" }}
            </n-button>
            <n-button :loading="resetLoading" @click="generateReset">
              Generar enlace de recuperación
            </n-button>
            <n-alert v-if="createdResetUrl" type="success" closable class="invite-url-alert">
              <template #header>Enlace de un solo uso — cópialo ahora</template>
              <span class="invite-url">{{ createdResetUrl }}</span>
              <n-button size="tiny" style="margin-left: 0.5rem" @click="copyResetUrl">
                Copiar
              </n-button>
            </n-alert>
            <n-button type="error" secondary @click="deleteModalOpen = true">
              Eliminar usuario…
            </n-button>
          </div>
        </div>
      </n-drawer-content>
    </n-drawer>

    <!-- Modal de eliminación con transferencia opcional. -->
    <n-modal v-model:show="deleteModalOpen" preset="card" title="Eliminar usuario" style="max-width: 480px">
      <div class="stack-md">
        <n-alert type="warning" title="Esta acción no se puede deshacer" />
        <n-radio-group v-model:value="deleteMode">
          <n-radio value="with-novels">Eliminar usuario y SUS novelas</n-radio>
          <n-radio value="transfer">Eliminar y mover novelas a otro usuario</n-radio>
        </n-radio-group>
        <n-select
          v-if="deleteMode === 'transfer'"
          v-model:value="transferTo"
          :options="transferOptions"
          placeholder="Elige el nuevo dueño"
        />
        <div class="row-between">
          <n-button secondary @click="deleteModalOpen = false">Cancelar</n-button>
          <n-button type="error" :loading="deleteLoading" @click="confirmDelete">
            Eliminar
          </n-button>
        </div>
      </div>
    </n-modal>
  </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { h, onMounted, reactive, ref } from "vue";
import {
  NAlert,
  NButton,
  NCard,
  NCollapse,
  NCollapseItem,
  NDataTable,
  NDrawer,
  NDrawerContent,
  NIcon,
  NInput,
  NModal,
  NRadio,
  NRadioGroup,
  NSelect,
  NSwitch,
  NTabPane,
  NTabs,
  NTag,
  useMessage,
  type DataTableColumns,
} from "naive-ui";
import { DownloadOutline } from "@vicons/ionicons5";
import type {
  AdminInvitation,
  AdminProviderKey,
  AdminUser,
  AdminUserStats,
} from "@/api/types";
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
    title: "Estado",
    key: "blocked",
    render(row) {
      if (row.blocked) {
        return h(NTag, { size: "small", type: "error" }, { default: () => "Bloqueado" });
      }
      return h(NTag, { size: "small", type: "success" }, { default: () => "Activo" });
    },
  },
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

function userRowProps(row: AdminUser) {
  return {
    style: "cursor: pointer",
    onClick: () => openUserDrawer(row),
  };
}

const drawerOpen = ref(false);
const selectedUser = ref<AdminUser | null>(null);
const userStats = ref<AdminUserStats | null>(null);
const statsLoading = ref(false);
const blockLoading = ref(false);
const resetLoading = ref(false);
const createdResetUrl = ref("");
const deleteModalOpen = ref(false);
const deleteMode = ref<"with-novels" | "transfer">("with-novels");
const transferTo = ref<string | null>(null);
const deleteLoading = ref(false);

const transferOptions = ref<{ label: string; value: string }[]>([]);

async function openUserDrawer(row: AdminUser) {
  selectedUser.value = row;
  userStats.value = null;
  createdResetUrl.value = "";
  deleteMode.value = "with-novels";
  transferTo.value = null;
  drawerOpen.value = true;
  transferOptions.value = users.value
    .filter((u) => u.id !== row.id)
    .map((u) => ({ label: `${u.email} (${u.role})`, value: u.id }));
  statsLoading.value = true;
  try {
    userStats.value = await api.admin.getUserStats(row.id);
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    statsLoading.value = false;
  }
}

async function toggleBlock() {
  if (!userStats.value) return;
  blockLoading.value = true;
  try {
    const updated = userStats.value.user.blocked
      ? await api.admin.unblockUser(userStats.value.user.id)
      : await api.admin.blockUser(userStats.value.user.id);
    userStats.value.user = { ...userStats.value.user, blocked: updated.blocked };
    message.success(updated.blocked ? "Usuario bloqueado" : "Usuario desbloqueado");
    await loadUsers();
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    blockLoading.value = false;
  }
}

async function generateReset() {
  if (!userStats.value) return;
  resetLoading.value = true;
  createdResetUrl.value = "";
  try {
    const result = await api.admin.createPasswordReset(userStats.value.user.id);
    createdResetUrl.value = result.resetUrl;
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    resetLoading.value = false;
  }
}

async function copyResetUrl() {
  try {
    await navigator.clipboard.writeText(createdResetUrl.value);
    message.success("Enlace copiado");
  } catch {
    message.error("No se pudo copiar el enlace");
  }
}

async function confirmDelete() {
  if (!userStats.value) return;
  if (deleteMode.value === "transfer" && !transferTo.value) {
    message.error("Elige el nuevo dueño de las novelas");
    return;
  }
  deleteLoading.value = true;
  try {
    await api.admin.deleteUser(userStats.value.user.id, {
      mode: deleteMode.value,
      transferToUserId: transferTo.value ?? undefined,
    });
    message.success("Usuario eliminado");
    deleteModalOpen.value = false;
    drawerOpen.value = false;
    await loadUsers();
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    deleteLoading.value = false;
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
    // Truncation guard: the server sends Content-Length (see router_backup.go).
    // Without this check a cut connection still saves a partial zip that
    // opens but is missing the tail entries (storage/ sorts last).
    const expected = Number(response.headers.get("Content-Length") ?? 0);
    if (expected > 0 && blob.size !== expected) {
      throw new Error(`descarga incompleta (${blob.size}/${expected} bytes) — reintenta`);
    }
    if (blob.size < 22) {
      throw new Error("descarga incompleta (archivo demasiado pequeño) — reintenta");
    }
    const magic = new Uint8Array(await blob.slice(0, 2).arrayBuffer());
    if (magic[0] !== 0x50 || magic[1] !== 0x4b) {
      throw new Error("el archivo descargado no es un zip válido — reintenta");
    }
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
