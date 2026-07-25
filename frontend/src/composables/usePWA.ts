import { ref, onMounted } from "vue";

export interface PWAInstallPrompt {
  deferredPrompt: any | null;
  canInstall: boolean;
  install: () => Promise<void>;
}

export function usePWA() {
  const canInstall = ref(false);
  const installPrompt = ref<any | null>(null);
  const isInstalled = ref(false);
  const updateAvailable = ref(false);
  const newVersionReady = ref(false);

  // Guardar el evento de instalación
  const saveBeforeInstallPromptEvent = (e: Event) => {
    e.preventDefault();
    installPrompt.value = e;
    canInstall.value = true;
  };

  // Instalar la PWA
  const install = async () => {
    if (!installPrompt.value) return;

    canInstall.value = false;

    try {
      const result = await installPrompt.value.prompt();
      if (result.outcome === "accepted") {
        isInstalled.value = true;
      }
    } catch {
      // Error silencioso
    } finally {
      installPrompt.value = null;
    }
  };

  // Verificar si la app ya está instalada
  const checkIfInstalled = () => {
    // Verificar si estamos en modo standalone
    const isStandalone = window.matchMedia("(display-mode: standalone)").matches;
    // Verificar si hay un service worker controlando la página
    const isSWControlled = navigator.serviceWorker?.controller !== null;
    
    isInstalled.value = isStandalone || isSWControlled;
  };

  // Verificar actualizaciones
  const checkForUpdates = async () => {
    try {
      const registration = await navigator.serviceWorker.getRegistration();
      if (!registration) return;

      registration.update().catch(() => {
        // Error silencioso
      });

      registration.addEventListener("updatefound", () => {
        updateAvailable.value = true;
      });
    } catch {
      // Error silencioso
    }
  };

  // Escuchar eventos de actualización
  const listenForUpdates = () => {
    navigator.serviceWorker.addEventListener("controllerchange", () => {
      newVersionReady.value = true;
    });
  };

  // Recargar para aplicar actualización
  const reloadForUpdate = () => {
    newVersionReady.value = false;
    window.location.reload();
  };

  // Registrar el service worker
  const registerServiceWorker = async () => {
    try {
      await navigator.serviceWorker.register("/sw.js");
      checkIfInstalled();
      checkForUpdates();
      listenForUpdates();
    } catch {
      // Error silencioso - el service worker no es soportado
    }
  };

  onMounted(() => {
    // Solo registrar en producción
    if (import.meta.env.PROD) {
      void registerServiceWorker();
    }

    // Escuchar el evento beforeinstallprompt
    window.addEventListener("beforeinstallprompt", saveBeforeInstallPromptEvent);
    
    // Verificar si ya está instalada
    checkIfInstalled();
  });

  return {
    canInstall,
    installPrompt,
    isInstalled,
    updateAvailable,
    newVersionReady,
    install,
    reloadForUpdate,
    checkIfInstalled,
  };
}
