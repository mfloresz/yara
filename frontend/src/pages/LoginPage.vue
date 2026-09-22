<template>
  <div class="login-page">
    <div class="login-shell">
      <!-- Panel visual: hero.webp a sangre con scrim funcional para legibilidad
           (el Overlay Scrim del sistema existe justo para esto). -->
      <aside class="login-visual" aria-label="Yara, tus novelas siempre contigo">
        <img class="visual-img" src="/hero.webp" alt="" fetchpriority="high" />
        <div class="visual-scrim" aria-hidden="true" />
        <div class="visual-content">
          <p class="visual-kicker">Yara · Biblioteca privada</p>
          <div class="visual-rule" aria-hidden="true" />
          <blockquote class="visual-quote">
            “Las mejores historias no solo se leen, se viven.”
          </blockquote>
        </div>
      </aside>

      <!-- Panel funcional: alterna entre login e invitación sin salir del diálogo. -->
      <section class="login-panel">
        <!-- ============ Vista: iniciar sesión ============ -->
        <div v-if="view === 'login'" key="login" class="panel-view">
          <header class="brand">
            <span class="brand-mark" aria-hidden="true">Y</span>
            <h1 ref="loginHeading" tabindex="-1" class="brand-word">Yara</h1>
          </header>
          <p class="tagline muted">Tus novelas, siempre contigo</p>

          <form class="login-form" @submit.prevent="submitLogin">
            <div>
              <label class="small muted" for="login-email">Email</label>
              <n-input
                id="login-email"
                v-model:value="email"
                type="text"
                autocomplete="email"
                placeholder="tu@email.com"
                @keydown.enter="submitLogin"
              >
                <template #prefix>
                  <n-icon :component="MailOutline" />
                </template>
              </n-input>
            </div>
            <div>
              <label class="small muted" for="login-password">Contraseña</label>
              <n-input
                id="login-password"
                v-model:value="password"
                type="password"
                show-password-on="click"
                autocomplete="current-password"
                placeholder="••••••••••"
                @keydown.enter="submitLogin"
              >
                <template #prefix>
                  <n-icon :component="LockClosedOutline" />
                </template>
              </n-input>
            </div>

            <label class="remember-row small">
              <n-checkbox v-model:checked="rememberMe">Recordarme</n-checkbox>
            </label>

            <n-alert v-if="loginError" type="error" :title="loginError" />
            <n-alert v-if="loginInfo" type="success" :title="loginInfo" />

            <n-button
              type="primary"
              block
              attr-type="submit"
              class="pill-btn touch-target"
              :loading="loginLoading"
            >
              Iniciar sesión
            </n-button>

            <button type="button" class="link-btn small" @click="router.push('/reset-password')">
              ¿Olvidaste tu contraseña?
            </button>
          </form>

          <footer class="panel-footer small muted">
            ¿No tienes cuenta?
            <button type="button" class="link-btn" @click="switchView('invite')">
              Solicita una invitación
            </button>
          </footer>
        </div>

        <!-- ============ Vista: invitación (registro privado) ============ -->
        <div v-else key="invite" class="panel-view">
          <button type="button" class="back-btn small" @click="switchView('login')">
            <n-icon :component="ArrowBackOutline" /> Volver
          </button>

          <header class="brand">
            <span class="brand-mark" aria-hidden="true">Y</span>
            <span class="brand-word">Yara</span>
          </header>

          <!-- Instalación fresca: la primera cuenta es administradora. -->
          <template v-if="needsSetup">
            <h1 class="panel-title">Configuración inicial</h1>
            <n-alert type="info" title="Sin usuarios todavía">
              Esta instalación aún no tiene usuarios. La primera cuenta creada se
              convertirá en administrador.
            </n-alert>
            <form class="login-form" @submit.prevent="submitSetup">
              <div>
                <label class="small muted" for="setup-name">Nombre</label>
                <n-input id="setup-name" v-model:value="setupName" placeholder="Nombre" />
              </div>
              <div>
                <label class="small muted" for="setup-email">Email</label>
                <n-input id="setup-email" v-model:value="setupEmail" type="text" placeholder="tu@email.com" />
              </div>
              <div>
                <label class="small muted" for="setup-password">Contraseña</label>
                <n-input
                  id="setup-password"
                  v-model:value="setupPassword"
                  type="password"
                  show-password-on="click"
                  placeholder="Mínimo 8 caracteres"
                />
              </div>
              <n-alert v-if="inviteError" type="error" :title="inviteError" />
              <n-button
                type="primary"
                block
                attr-type="submit"
                class="pill-btn touch-target"
                :loading="inviteLoading"
              >
                Crear cuenta de administrador
              </n-button>
            </form>
          </template>

          <template v-else>
            <h1 ref="inviteHeading" tabindex="-1" class="panel-title">Crear cuenta</h1>
            <p class="small muted invite-sub">
              Yara es una aplicación privada. Necesitas una invitación para crear una cuenta.
            </p>

            <!-- Paso 1: pegar y validar el enlace. -->
            <template v-if="!inviteToken || !inviteValidation?.valid">
              <div class="how-box">
                <p class="small how-title">
                  <n-icon :component="InformationCircleOutline" /> ¿Cómo funciona?
                </p>
                <ol class="small muted how-list">
                  <li>Un administrador te envía un enlace de invitación.</li>
                  <li>Validamos que sea válido y no esté vencido ni usado.</li>
                  <li>Sólo te pedimos elegir una contraseña.</li>
                </ol>
              </div>
              <form class="login-form" @submit.prevent="submitInviteToken">
                <div>
                  <label class="small muted" for="invite-link">Enlace de invitación</label>
                  <n-input
                    id="invite-link"
                    v-model:value="manualToken"
                    placeholder="Pega aquí el enlace que te compartieron"
                  >
                    <template #prefix>
                      <n-icon :component="LinkOutline" />
                    </template>
                  </n-input>
                </div>
                <n-alert
                  v-if="inviteValidation && !inviteValidation.valid"
                  type="error"
                  title="Invitación no válida"
                >
                  El enlace es incorrecto, ya fue usado o ha expirado.
                </n-alert>
                <n-alert v-if="inviteError" type="error" :title="inviteError" />
                <n-button
                  type="primary"
                  block
                  attr-type="submit"
                  class="pill-btn touch-target"
                  :loading="inviteLoading"
                >
                  Validar enlace
                </n-button>
              </form>
            </template>

            <!-- Paso 2: enlace válido → elegir contraseña. -->
            <template v-else>
              <n-alert type="success" :title="`Invitación para ${inviteValidation.email}`">
                Elige una contraseña para completar tu cuenta.
              </n-alert>
              <form class="login-form" @submit.prevent="submitInviteAccept">
                <div>
                  <label class="small muted" for="invite-password">Contraseña</label>
                  <n-input
                    id="invite-password"
                    v-model:value="invitePassword"
                    type="password"
                    show-password-on="click"
                    placeholder="Mínimo 8 caracteres"
                  >
                    <template #prefix>
                      <n-icon :component="LockClosedOutline" />
                    </template>
                  </n-input>
                </div>
                <n-alert v-if="inviteError" type="error" :title="inviteError" />
                <n-button
                  type="primary"
                  block
                  attr-type="submit"
                  class="pill-btn touch-target"
                  :loading="inviteLoading"
                >
                  Crear cuenta
                </n-button>
              </form>
            </template>
          </template>

          <footer class="panel-footer small muted">
            ¿Ya tienes una cuenta?
            <button type="button" class="link-btn" @click="switchView('login')">
              Iniciar sesión
            </button>
          </footer>
        </div>
      </section>
    </div>
  </div>
</template>

<script setup lang="ts">
import { nextTick, onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { NAlert, NButton, NCheckbox, NIcon, NInput } from "naive-ui";
import {
  ArrowBackOutline,
  InformationCircleOutline,
  LinkOutline,
  LockClosedOutline,
  MailOutline,
} from "@vicons/ionicons5";
import type { InvitationValidation } from "@/api/types";
import { useAppServices } from "@/app/services";

type PanelView = "login" | "invite";

const REMEMBER_EMAIL_KEY = "yara_remember_email";

const router = useRouter();
const route = useRoute();
const { api, login } = useAppServices();

const view = ref<PanelView>("login");
const loginHeading = ref<HTMLElement | null>(null);
const inviteHeading = ref<HTMLElement | null>(null);

// --- Login ---
const email = ref("");
const password = ref("");
const rememberMe = ref(false);
const loginLoading = ref(false);
const loginError = ref<string | null>(null);
const loginInfo = ref<string | null>(null);

// --- Invitación ---
const needsSetup = ref(false);
const inviteLoading = ref(false);
const inviteError = ref<string | null>(null);
const manualToken = ref("");
const inviteToken = ref("");
const inviteValidation = ref<InvitationValidation | null>(null);
const invitePassword = ref("");
const setupName = ref("");
const setupEmail = ref("");
const setupPassword = ref("");

onMounted(async () => {
  try {
    const stored = localStorage.getItem(REMEMBER_EMAIL_KEY);
    if (stored) {
      email.value = stored;
      rememberMe.value = true;
    }
  } catch {
    // Sin almacenamiento disponible: el login sigue funcionando.
  }
  try {
    const status = await api.auth.setupStatus();
    needsSetup.value = status.needsSetup;
  } catch {
    needsSetup.value = false;
  }
});

function switchView(next: PanelView) {
  view.value = next;
  loginError.value = null;
  inviteError.value = null;
  // Mueve el foco al título de la vista entrante: evita restos de foco
  // en botones desmontados y saltos de scroll inesperados.
  nextTick(() => {
    const el = next === "login" ? loginHeading.value : inviteHeading.value;
    el?.focus({ preventScroll: true });
  });
}

async function submitLogin() {
  loginLoading.value = true;
  loginError.value = null;
  loginInfo.value = null;
  try {
    await login({ email: email.value, password: password.value });
    try {
      if (rememberMe.value) {
        localStorage.setItem(REMEMBER_EMAIL_KEY, email.value);
      } else {
        localStorage.removeItem(REMEMBER_EMAIL_KEY);
      }
    } catch {
      // No bloquear el acceso si el almacenamiento falla.
    }
    await router.push(String(route.query.redirect || "/"));
  } catch (err) {
    loginError.value = err instanceof Error ? err.message : String(err);
  } finally {
    loginLoading.value = false;
  }
}

function extractToken(input: string): string {
  const trimmed = input.trim();
  const marker = "/invite/";
  const idx = trimmed.indexOf(marker);
  if (idx >= 0) {
    return trimmed.slice(idx + marker.length).replace(/\/+$/, "");
  }
  return trimmed;
}

async function submitInviteToken() {
  inviteToken.value = extractToken(manualToken.value);
  if (!inviteToken.value) {
    inviteError.value = "Introduce un enlace o código de invitación";
    return;
  }
  inviteError.value = null;
  inviteLoading.value = true;
  try {
    inviteValidation.value = await api.auth.validateInvitation(inviteToken.value);
  } catch (err) {
    inviteError.value = err instanceof Error ? err.message : String(err);
  } finally {
    inviteLoading.value = false;
  }
}

async function submitInviteAccept() {
  inviteLoading.value = true;
  inviteError.value = null;
  try {
    await api.auth.acceptInvitation({ token: inviteToken.value, password: invitePassword.value });
    // La invitación no inicia sesión: el usuario entra con su nueva cuenta.
    email.value = inviteValidation.value?.email ?? "";
    loginInfo.value = "Cuenta creada. Inicia sesión con tu nueva contraseña.";
    invitePassword.value = "";
    inviteValidation.value = null;
    inviteToken.value = "";
    manualToken.value = "";
    switchView("login");
  } catch (err) {
    inviteError.value = err instanceof Error ? err.message : String(err);
  } finally {
    inviteLoading.value = false;
  }
}

async function submitSetup() {
  inviteLoading.value = true;
  inviteError.value = null;
  try {
    // El backend convierte al primer usuario en administrador.
    await api.auth.register({
      email: setupEmail.value,
      password: setupPassword.value,
      name: setupName.value,
    });
    await login({ email: setupEmail.value, password: setupPassword.value });
    await router.push("/");
  } catch (err) {
    inviteError.value = err instanceof Error ? err.message : String(err);
  } finally {
    inviteLoading.value = false;
  }
}
</script>

<style scoped>
.login-page {
  min-height: 100dvh;
  background: var(--surface-elevated);
}

/* Split a sangre: sin tarjeta centrada, la imagen y el formulario
   ocupan cada uno su mitad del viewport (5:4). Alto fijo: la página
   nunca hace scroll en escritorio; el panel desplaza por dentro solo
   si el viewport es muy bajo. */
.login-shell {
  width: 100%;
  height: 100dvh;
  display: grid;
  grid-template-columns: minmax(0, 5fr) minmax(0, 4fr);
  overflow: hidden;
}

/* Panel visual: foto a sangre; el texto vive sobre el scrim funcional. */
.login-visual {
  position: relative;
  overflow: hidden;
  min-height: 100%;
}

.visual-img {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.visual-scrim {
  position: absolute;
  inset: 0;
  background:
    linear-gradient(
      to bottom,
      color-mix(in oklab, #141413 52%, transparent) 0%,
      transparent 32%,
      transparent 52%,
      color-mix(in oklab, #141413 78%, transparent) 100%
    );
}

.visual-content {
  position: relative;
  height: 100%;
  padding: 2rem 1.75rem;
  display: flex;
  flex-direction: column;
  color: #fafaf9;
}

.visual-kicker {
  margin: 0;
  font-size: 0.75rem;
  letter-spacing: 0.14em;
  text-transform: uppercase;
  opacity: 0.72;
}

.visual-rule {
  margin: 1rem 0;
  border-top: 1px solid color-mix(in oklab, currentColor 28%, transparent);
}

.visual-quote {
  margin: auto 0 0;
  font-size: 1.0625rem;
  line-height: 1.65;
  font-style: italic;
  text-wrap: balance;
}

/* Panel funcional */
.login-panel {
  display: flex;
  overflow-y: auto;
  padding: 1.5rem clamp(1.5rem, 5vw, 4rem);
}

.panel-view {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
  width: 100%;
  max-width: 400px;
  /* margin auto: centra si cabe, deja crecer con scroll interno si no. */
  margin: auto;
  animation: panel-in 0.18s ease-out;
}

.brand {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 0.5rem;
}

.brand-mark {
  display: inline-grid;
  place-items: center;
  width: 2rem;
  height: 2rem;
  border-radius: 50%;
  background: var(--btn-primary-bg);
  color: var(--btn-primary-fg);
  font-weight: 800;
  font-size: 1.125rem;
  line-height: 1;
}

.brand-word {
  margin: 0;
  font-size: 1.75rem;
  font-weight: 700;
  letter-spacing: -0.02em;
}

.tagline {
  margin: 0;
  text-align: center;
}

.panel-title {
  margin: 0;
  text-align: center;
  font-size: 1.25rem;
  font-weight: 700;
  letter-spacing: -0.02em;
}

.invite-sub {
  margin: 0;
  text-align: center;
}

.login-form {
  display: flex;
  flex-direction: column;
  gap: 1rem;
}

.remember-row {
  display: flex;
  align-items: center;
}

.back-btn {
  align-self: flex-start;
  display: inline-flex;
  align-items: center;
  gap: 0.375rem;
  background: none;
  border: none;
  padding: 0.375rem 0;
  min-height: 44px;
  color: var(--text-secondary);
  cursor: pointer;
}

.back-btn:hover {
  color: var(--text-primary);
}

.how-box {
  background: var(--surface-muted);
  border: 1px solid var(--divide);
  border-radius: var(--radius-md);
  padding: 0.625rem 0.875rem;
}

.how-title {
  margin: 0 0 0.5rem;
  display: flex;
  align-items: center;
  gap: 0.375rem;
  font-weight: 600;
}

.how-list {
  margin: 0;
  padding-left: 1.25rem;
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
}

.link-btn {
  background: none;
  border: none;
  padding: 0;
  color: var(--accent-link);
  cursor: pointer;
  font: inherit;
  min-height: 44px;
}

.link-btn:hover {
  color: var(--accent-link-hover);
  text-decoration: underline;
}

.panel-footer {
  margin-top: 1rem;
  text-align: center;
}

.panel-footer .link-btn {
  min-height: 0;
  padding: 0.25rem 0.125rem;
}

/* Botones píldora según DESIGN.md (Naive los dibuja cuadrados por defecto). */
.login-panel :deep(.n-button.pill-btn) {
  border-radius: var(--radius-pill);
}

.login-panel :deep(.n-button.pill-btn .n-button__content) {
  font-weight: 600;
}

@keyframes panel-in {
  from {
    opacity: 0;
    transform: translateY(4px);
  }
  to {
    opacity: 1;
    transform: none;
  }
}

@media (max-width: 860px) {
  .login-shell {
    grid-template-columns: 1fr;
    height: auto;
    min-height: 100dvh;
    overflow: visible;
  }

  .login-visual {
    min-height: 30dvh;
  }

  .visual-content {
    padding: 1.5rem 1.25rem;
  }

  .visual-quote {
    font-size: 1rem;
  }

  .login-panel {
    justify-content: flex-start;
    overflow: visible;
    padding: 2rem 1.25rem;
  }

  .brand-word {
    font-size: 1.5rem;
  }
}

@media (prefers-reduced-motion: reduce) {
  .panel-view {
    animation: none;
  }
}
</style>
