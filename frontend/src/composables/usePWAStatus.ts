import { ref, onMounted, onUnmounted, computed } from "vue";

/**
 * Composable para manejar el estado global de PWA y conectividad
 */
export function usePWAStatus() {
  const isOnline = ref(navigator.onLine);
  const isPWAInstalled = ref(false);
  const canInstallPWA = ref(false);
  const offlineMode = ref(false);

  // Verificar si estamos en modo PWA
  function checkPWAInstalled() {
    const isStandalone = window.matchMedia("(display-mode: standalone)").matches;
    isPWAInstalled.value = isStandalone;
    offlineMode.value = !isOnline.value;
  }

  // Event listeners
  const updateOnlineStatus = () => {
    isOnline.value = navigator.onLine;
    offlineMode.value = !isOnline.value;
  };

  onMounted(() => {
    checkPWAInstalled();
    window.addEventListener("online", updateOnlineStatus);
    window.addEventListener("offline", updateOnlineStatus);
    
    // Verificar si estamos en modo standalone
    window.matchMedia("(display-mode: standalone)").addEventListener("change", (e) => {
      isPWAInstalled.value = e.matches;
    });
  });

  onUnmounted(() => {
    window.removeEventListener("online", updateOnlineStatus);
    window.removeEventListener("offline", updateOnlineStatus);
  });

  return {
    isOnline,
    isPWAInstalled,
    canInstallPWA,
    offlineMode,
    checkPWAInstalled,
  };
}
