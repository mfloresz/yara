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
}

void bootstrap();
