<template>
  <div class="auth-page">
    <n-card class="auth-card" size="small">
      <template #header>Cambiar contraseña</template>
      <div class="stack-md">
        <template v-if="token && validation">
          <n-alert v-if="!validation.valid" type="error" title="Enlace no válido">
            El enlace es incorrecto, ya fue usado o ha expirado. Pide uno nuevo al administrador.
          </n-alert>
          <template v-else>
            <n-alert type="info" :title="`Restablecer acceso${validation.email ? ` para ${validation.email}` : ''}`">
              Elige una contraseña nueva. Se cerrarán todas tus sesiones, incluidas las extensiones.
            </n-alert>
            <div>
              <label class="small muted">Nueva contraseña</label>
              <n-input
                v-model:value="password"
                type="password"
                show-password-on="click"
                placeholder="Mínimo 8 caracteres"
              />
            </div>
            <div>
              <label class="small muted">Repetir contraseña</label>
              <n-input
                v-model:value="confirm"
                type="password"
                show-password-on="click"
                placeholder="Repite la contraseña"
              />
            </div>
            <n-alert v-if="error" type="error" :title="error" />
            <n-button type="primary" block :loading="loading" @click="submit">
              Cambiar contraseña e ir a iniciar sesión
            </n-button>
          </template>
        </template>
        <template v-else>
          <n-alert type="info" title="Recuperación por enlace">
            Pega el enlace de recuperación que te dio el administrador.
          </n-alert>
          <div>
            <label class="small muted">Enlace o código</label>
            <n-input v-model:value="manualToken" placeholder="https://…/reset-password/…" />
          </div>
          <n-alert v-if="error" type="error" :title="error" />
          <n-button block :loading="loading" @click="submitManualToken">
            Validar enlace
          </n-button>
          <n-button block secondary @click="router.push('/login')">
            Volver a iniciar sesión
          </n-button>
        </template>
      </div>
    </n-card>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { NCard, NInput, NButton, NAlert, useMessage } from "naive-ui";
import type { PasswordResetValidation } from "@/api/types";
import { useAppServices } from "@/app/services";
import { clearAuth } from "@/app/auth";

const route = useRoute();
const router = useRouter();
const { api } = useAppServices();
const message = useMessage();

const validation = ref<PasswordResetValidation | null>(null);
const token = ref("");
const manualToken = ref("");
const password = ref("");
const confirm = ref("");
const loading = ref(false);
const error = ref<string | null>(null);

onMounted(async () => {
  const routeToken = typeof route.params.token === "string" ? route.params.token : "";
  if (routeToken) {
    token.value = routeToken;
    await validate();
  }
});

function extractToken(input: string): string {
  const trimmed = input.trim();
  const marker = "/reset-password/";
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
    validation.value = await api.auth.validatePasswordReset(token.value);
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err);
  } finally {
    loading.value = false;
  }
}

async function submitManualToken() {
  token.value = extractToken(manualToken.value);
  if (!token.value) {
    error.value = "Introduce un enlace o código de recuperación";
    return;
  }
  await validate();
}

async function submit() {
  if (password.value !== confirm.value) {
    error.value = "Las contraseñas no coinciden";
    return;
  }
  loading.value = true;
  error.value = null;
  try {
    await api.auth.acceptPasswordReset({ token: token.value, password: password.value });
    clearAuth();
    message.success("Contraseña cambiada. Todas las sesiones se cerraron. Inicia sesión de nuevo.");
    await router.push("/login");
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
</style>
