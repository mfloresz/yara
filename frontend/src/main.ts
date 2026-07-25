import { createApp } from "vue";
import { create, NMessageProvider, NDialogProvider } from "naive-ui";
import App from "./app/App.vue";
import { router } from "./router";
import { getStoredTheme, applyTheme } from "./app/auth";
import { appServicesKey, createAppServices } from "./app/services";
import "./app/styles.css";

const naive = create();

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
      await navigator.serviceWorker.register(swUrl);
      console.log(`Service Worker registrado: ${swUrl}`);
    } catch (error) {
      console.error('Error al registrar Service Worker:', error);
    }
  }
}

void bootstrap();
