import { createApp } from "vue";
import { create, NMessageProvider, NDialogProvider } from "naive-ui";
import { disableD2, disableInfographic, disableKatex, disableMermaid } from "markstream-vue";
import App from "./app/App.vue";
import { router } from "./router";
import { getStoredTheme, applyTheme } from "./app/auth";
import { appServicesKey, createAppServices } from "./app/services";
import "./app/styles.css";

const naive = create();

// The assistant renders prose, lists and code — never math, diagrams or
// infographics. Leaving those renderers on makes markstream-vue probe for
// KaTeX/Mermaid/D2 at runtime and log failures on every unmatched block.
// They are optional peer dependencies and are not installed, so switch them
// off explicitly rather than relying on their absence.
disableKatex();
disableMermaid();
disableD2();
disableInfographic();

async function bootstrap() {
  applyTheme(getStoredTheme());

  const app = createApp(App);
  const services = createAppServices();

  await services.restoreSession();

  app.use(router);
  app.use(naive);
  app.provide(appServicesKey, services);
  app.mount("#app");

  // Registrar service worker manualmente para asegurarnos que funcione
  if ('serviceWorker' in navigator) {
    try {
      const swUrl = import.meta.env.PROD ? '/sw.js' : '/sw-dev.js';
      const registration = await navigator.serviceWorker.register(swUrl);
      if (import.meta.env.PROD) {
        // The fresh SW takes over on its own (skipWaiting + clientsClaim).
        // Reload so the open page runs the new bundle instead of lingering
        // on chunks from the previous deploy. Skipped on first install,
        // where there is no previous controller to be stale from.
        const hadController = !!navigator.serviceWorker.controller;
        let reloaded = false;
        navigator.serviceWorker.addEventListener("controllerchange", () => {
          if (hadController && !reloaded) {
            reloaded = true;
            window.location.reload();
          }
        });
        // Browsers also check for SW updates on navigation; poll hourly so
        // long-lived tabs pick up deploys without navigating.
        setInterval(() => void registration.update(), 60 * 60 * 1000);
      }
    } catch {
      // SW registration is best-effort: PWA features fall back gracefully.
    }
  }
}

void bootstrap();
