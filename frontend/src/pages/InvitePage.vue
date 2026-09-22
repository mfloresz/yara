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

      <!-- Panel funcional. -->
      <section class="login-panel">
        <!-- Instalación fresca: el primer usuario se registra como admin. -->
        <div v-if="needsSetup" key="setup" class="panel-view">
          <header class="brand">
            <span class="brand-mark" aria-hidden="true">Y</span>
            <span class="brand-word">Yara</span>
          </header>
          <h1 class="panel-title">Configuración inicial</h1>
          <div class="how-box">
            <p class="small how-title">
              <n-icon :component="InformationCircleOutline" /> Sin usuarios todavía
            </p>
            <p class="small muted how-text">
              Esta instalación aún no tiene usuarios. La primera cuenta creada se
              convertirá en administrador.
            </p>
          </div>
          <form class="login-form" @submit.prevent="submitSetup">
            <div>
              <label class="small muted" for="setup-name">Nombre</label>
              <n-input id="setup-name" v-model:value="name" placeholder="Nombre" />
            </div>
            <div>
              <label class="small muted" for="setup-email">Email</label>
              <n-input id="setup-email" v-model:value="email" type="text" placeholder="tu@email.com" />
            </div>
            <div>
              <label class="small muted" for="setup-password">Contraseña</label>
              <n-input
                id="setup-password"
                v-model:value="password"
                type="password"
                show-password-on="click"
                placeholder="Mínimo 8 caracteres"
              />
            </div>
            <n-alert v-if="error" type="error" :title="error" />
            <n-button
              type="primary"
              block
              attr-type="submit"
              class="pill-btn touch-target"
              :loading="loading"
            >
              Crear cuenta de administrador
            </n-button>
          </form>
          <footer class="panel-footer small muted">
            ¿Ya tienes una cuenta?
            <button type="button" class="link-btn" @click="router.push('/login')">
              Iniciar sesión
            </button>
          </footer>
        </div>

        <!-- Canje de invitación. -->
        <div v-else key="invite" class="panel-view">
          <header class="brand">
            <span class="brand-mark" aria-hidden="true">Y</span>
            <span class="brand-word">Yara</span>
          </header>
          <h1 class="panel-title">Crear cuenta</h1>

          <template v-if="token && validation">
            <!-- Enlace inválido: solo error y salidas, sin formulario. -->
            <template v-if="!validation.valid">
              <n-alert type="error" title="Invitación no válida">
                El enlace es incorrecto, ya fue usado o ha expirado.
              </n-alert>
              <button type="button" class="link-btn small" @click="resetToken">
                Usar otro enlace
              </button>
            </template>

            <!-- Enlace válido: elegir contraseña. -->
            <template v-else>
              <div class="how-box">
                <p class="small how-title">
                  <n-icon :component="InformationCircleOutline" /> Invitación para {{ validation.email }}
                </p>
                <p class="small muted how-text">
                  Elige una contraseña para completar tu cuenta.
                </p>
              </div>
              <form class="login-form" @submit.prevent="submitAccept">
                <div>
                  <label class="small muted" for="invite-password">Contraseña</label>
                  <n-input
                    id="invite-password"
                    v-model:value="password"
                    type="password"
                    show-password-on="click"
                    placeholder="Mínimo 8 caracteres"
                  />
                </div>
                <n-alert v-if="error" type="error" :title="error" />
                <n-button
                  type="primary"
                  block
                  attr-type="submit"
                  class="pill-btn touch-target"
                  :loading="loading"
                >
                  Crear cuenta
                </n-button>
              </form>
            </template>
          </template>

          <!-- Sin enlace: explicación + pegar enlace. -->
          <template v-else>
            <p class="small muted invite-sub">
              Yara es una aplicación privada. Necesitas una invitación para crear una cuenta.
            </p>
            <div class="how-box">
              <p class="small how-title">
                <n-icon :component="InformationCircleOutline" /> ¿Cómo funciona?
              </p>
              <ol class="small muted how-list">
                <li>Un administrador te envía un enlace de invitación.</li>
                <li>Pégalo aquí: validamos que no esté vencido ni usado.</li>
                <li>Sólo te pedimos elegir una contraseña.</li>
              </ol>
            </div>
            <form class="login-form" @submit.prevent="submitManualToken">
              <div>
                <label class="small muted" for="invite-link">Enlace o código de invitación</label>
                <n-input
                  id="invite-link"
                  v-model:value="manualToken"
                  placeholder="https://…/invite/…"
                />
              </div>
              <n-alert v-if="error" type="error" :title="error" />
              <n-button
                type="primary"
                block
                attr-type="submit"
                class="pill-btn touch-target"
                :loading="loading"
              >
                Validar invitación
              </n-button>
            </form>
          </template>

          <footer class="panel-footer small muted">
            ¿Ya tienes una cuenta?
            <button type="button" class="link-btn" @click="router.push('/login')">
              Iniciar sesión
            </button>
          </footer>
        </div>
      </section>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { NInput, NButton, NAlert, NIcon } from "naive-ui";
import { InformationCircleOutline } from "@vicons/ionicons5";
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

function resetToken() {
  token.value = "";
  manualToken.value = "";
  validation.value = null;
  password.value = "";
  error.value = null;
}

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

.how-text {
  margin: 0;
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
  /* Móvil: la imagen pasa a ser fondo completo y el formulario un card
     central opaque (Bone Paper, sin glass). La vista queda bloqueada a
     100dvh sin scroll: el card se compacta para caber. */
  .login-page {
    height: 100dvh;
    overflow: hidden;
    background: #141413;
  }

  .login-shell {
    position: relative;
    grid-template-columns: 1fr;
    place-items: center;
    height: 100dvh;
    min-height: 100dvh;
    overflow: hidden;
    padding:
      calc(1rem + env(safe-area-inset-top))
      1rem
      calc(1rem + env(safe-area-inset-bottom));
  }

  /* Fondo a sangre: ocupa todo el viewport detrás del card. */
  .login-visual {
    position: absolute;
    inset: 0;
    min-height: 0;
  }

  /* El texto sobre foto competiría con el card: se oculta en móvil. */
  .visual-content {
    display: none;
  }

  /* Scrim uniforme para que la foto no compita con el card. */
  .visual-scrim {
    background:
      linear-gradient(
        to bottom,
        color-mix(in oklab, #141413 62%, transparent) 0%,
        color-mix(in oklab, #141413 48%, transparent) 50%,
        color-mix(in oklab, #141413 66%, transparent) 100%
      );
  }

  /* Card central: superficie elevada opaca, borde 1px, sin sombra
     (Flat-Except-The-Book). */
  .login-panel {
    position: relative;
    z-index: 1;
    width: 100%;
    max-width: 400px;
    max-height: calc(
      100dvh - 2rem - env(safe-area-inset-top) - env(safe-area-inset-bottom)
    );
    overflow: hidden;
    justify-content: center;
    background: var(--surface-elevated);
    border: 1px solid var(--divide);
    border-radius: var(--radius-xl);
    padding: 1.25rem 1.25rem 1rem;
  }

  .panel-view {
    gap: 0.625rem;
    margin: 0;
    max-width: none;
  }

  .brand-word {
    font-size: 1.5rem;
  }

  .tagline {
    font-size: 0.875rem;
  }

  .login-form {
    gap: 0.75rem;
  }

  .how-box {
    padding: 0.5rem 0.75rem;
  }

  .how-title {
    margin-bottom: 0.375rem;
  }

  .how-list {
    gap: 0.125rem;
  }

  .panel-footer {
    margin-top: 0.25rem;
  }
}

/* Viewports bajos (landscape, teclado abierto): compactar más antes
   que introducir scroll. */
@media (max-width: 860px) and (max-height: 700px) {
  .login-panel {
    padding: 1rem 1.125rem 0.875rem;
  }

  .panel-view {
    gap: 0.5rem;
  }

  .login-form {
    gap: 0.625rem;
  }

  .tagline,
  .invite-sub {
    display: none;
  }

  .how-box {
    display: none;
  }

  .panel-footer {
    margin-top: 0;
  }
}

/* Último recurso en landscape muy bajo: scroll interno del card,
   la página sigue sin hacer scroll. */
@media (max-width: 860px) and (max-height: 460px) {
  .login-panel {
    overflow-y: auto;
  }
}

@media (prefers-reduced-motion: reduce) {
  .panel-view {
    animation: none;
  }
}
</style>
