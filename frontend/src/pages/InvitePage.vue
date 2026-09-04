<template>
  <div class="auth-page">
    <n-card class="auth-card" size="small">
      <template #header>{{ pageTitle }}</template>

      <!-- Instalación fresca: el primer usuario se registra como admin. -->
      <div v-if="needsSetup" class="stack-md">
        <n-alert type="info" title="Configuración inicial">
          Esta instalación aún no tiene usuarios. La primera cuenta creada se
          convertirá en administrador.
        </n-alert>
        <div>
          <label class="small muted">Nombre</label>
          <n-input v-model:value="name" placeholder="Nombre" />
        </div>
        <div>
          <label class="small muted">Email</label>
          <n-input v-model:value="email" type="text" placeholder="Email" />
        </div>
        <div>
          <label class="small muted">Contraseña</label>
          <n-input
            v-model:value="password"
            type="password"
            show-password-on="click"
            placeholder="Contraseña (mínimo 8 caracteres)"
          />
        </div>
        <n-alert v-if="error" type="error" :title="error" />
        <n-button type="primary" block :loading="loading" @click="submitSetup">
          Crear cuenta de administrador
        </n-button>
        <n-button block secondary @click="router.push('/login')">
          Ya tengo cuenta
        </n-button>
      </div>

      <!-- Canje de invitación. -->
      <div v-else class="stack-md">
        <template v-if="token && validation">
          <n-alert v-if="!validation.valid" type="error" title="Invitación no válida">
            El enlace es incorrecto, ya fue usado o ha expirado.
          </n-alert>
          <template v-else>
            <n-alert type="info" :title="`Invitación para ${validation.email}`">
              Elige una contraseña para completar tu cuenta.
            </n-alert>
            <div>
              <label class="small muted">Contraseña</label>
              <n-input
                v-model:value="password"
                type="password"
                show-password-on="click"
                placeholder="Contraseña (mínimo 8 caracteres)"
              />
            </div>
            <n-alert v-if="error" type="error" :title="error" />
            <n-button
              type="primary"
              block
              :loading="loading"
              @click="submitAccept"
            >
              Crear cuenta
            </n-button>
          </template>
        </template>

        <template v-else>
          <n-alert type="info" title="Registro por invitación">
            El registro requiere un enlace de invitación. Pégalo aquí si lo
            tienes.
          </n-alert>
          <div>
            <label class="small muted">Enlace o código de invitación</label>
            <n-input v-model:value="manualToken" placeholder="https://…/invite/…" />
          </div>
          <n-alert v-if="error" type="error" :title="error" />
          <n-button block :loading="loading" @click="submitManualToken">
            Validar invitación
          </n-button>
          <n-button block secondary @click="router.push('/login')">
            Ya tengo cuenta
          </n-button>
        </template>
      </div>
    </n-card>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { NCard, NInput, NButton, NAlert } from "naive-ui";
import type { InvitationValidation } from "@/api/types";
import { useAppServices } from "@/app/services";

const route = useRoute();
const router = useRouter();
const { api, login } = useAppServices();

const needsSetup = ref(false);
const validation = ref<InvitationValidation | null>(null);
const token = ref("");
const manualToken = ref("");
const name = ref("");
const email = ref("");
const password = ref("");
const loading = ref(false);
const error = ref<string | null>(null);

const pageTitle = computed(() =>
  needsSetup.value ? "Configuración inicial" : "Crear cuenta",
);

onMounted(async () => {
  try {
    const status = await api.auth.setupStatus();
    needsSetup.value = status.needsSetup;
  } catch {
    needsSetup.value = false;
  }
  const routeToken =
    typeof route.params.token === "string" ? route.params.token : "";
  if (!needsSetup.value && routeToken) {
    token.value = routeToken;
    await validate();
  }
});

async function extractToken(input: string): Promise<string> {
  const trimmed = input.trim();
  const marker = "/invite/";
  const idx = trimmed.indexOf(marker);
  if (idx >= 0) {
    return trimmed.slice(idx + marker.length).replace(/\/+$/, "");
  }
  return trimmed;
}

async function validate() {
  error.value = null;
  loading.value = true;
  try {
    validation.value = await api.auth.validateInvitation(token.value);
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err);
  } finally {
    loading.value = false;
  }
}

async function submitManualToken() {
  token.value = await extractToken(manualToken.value);
  if (!token.value) {
    error.value = "Introduce un enlace o código de invitación";
    return;
  }
  await validate();
}

async function submitAccept() {
  loading.value = true;
  error.value = null;
  try {
    await api.auth.acceptInvitation({ token: token.value, password: password.value });
    // La invitación no inicia sesión: el usuario entra con su nueva cuenta.
    await router.push("/login");
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err);
  } finally {
    loading.value = false;
  }
}

async function submitSetup() {
  loading.value = true;
  error.value = null;
  try {
    // El backend convierte al primer usuario en administrador.
    await api.auth.register({ email: email.value, password: password.value, name: name.value });
    await login({ email: email.value, password: password.value });
    await router.push("/");
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err);
  } finally {
    loading.value = false;
  }
}
</script>

<style scoped>
.auth-page {
  min-height: 100vh;
  display: grid;
  place-items: center;
  padding: 1rem;
}

.auth-card {
  width: 100%;
  max-width: 420px;
}

@media (max-width: 640px) {
  .auth-page {
    padding: 0.75rem;
  }
}
</style>
