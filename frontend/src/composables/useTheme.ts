import { computed, ref } from "vue";
import { applyTheme, getStoredTheme } from "@/app/auth";

export type ThemeMode = "light" | "dark" | "system";

export const themeLabels: Record<ThemeMode, string> = {
  system: "Sistema",
  light: "Claro",
  dark: "Oscuro",
};

export const themeOrder: ThemeMode[] = ["system", "light", "dark"];

// Estado compartido a nivel de módulo: el topbar de escritorio y el drawer
// móvil (y ahora el menú de usuario) leen/escriben el mismo valor.
const themeState = ref<ThemeMode>(getStoredTheme());

export function useTheme() {
  // setAuth() aplica el tema del usuario vía applyTheme() directamente,
  // así que re-sincronizamos con localStorage en cada setup.
  const stored = getStoredTheme();
  if (stored !== themeState.value) themeState.value = stored;

  const themeLabel = computed(() => themeLabels[themeState.value]);

  function setTheme(mode: ThemeMode) {
    if (themeState.value === mode) return;
    themeState.value = mode;
    applyTheme(mode);
  }

  function cycleTheme() {
    setTheme(themeOrder[(themeOrder.indexOf(themeState.value) + 1) % themeOrder.length]);
  }

  return { theme: themeState, themeLabel, setTheme, cycleTheme };
}
