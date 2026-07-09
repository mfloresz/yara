import { computed, ref, readonly } from "vue";
import { darkTheme } from "naive-ui";
import type { GlobalThemeOverrides } from "naive-ui";
import type { AuthResponse, AuthUser } from "@/api/types";
import {
  pixeoThemeOverrides,
  pixeoDarkThemeOverrides,
} from "@/theme/naive-theme";

const THEME_KEY = "theme";

const user = ref<AuthUser | null>(null);
const ready = ref(false);
const _currentTheme = ref<"light" | "dark">("light");

let systemThemeListenerAttached = false;

export const currentTheme = readonly(_currentTheme);

export const currentNaiveTheme = computed(() =>
  _currentTheme.value === "dark" ? darkTheme : null,
);

export const currentThemeOverrides = computed<GlobalThemeOverrides>(() =>
  _currentTheme.value === "dark"
    ? pixeoDarkThemeOverrides
    : pixeoThemeOverrides,
);

export const authState = {
  user,
  ready,
  isAuthenticated: computed(() => Boolean(user.value)),
};

export function getStoredTheme(): "light" | "dark" | "system" {
  return (
    (localStorage.getItem(THEME_KEY) as "light" | "dark" | "system" | null) ||
    "system"
  );
}

export function applyTheme(theme: "light" | "dark" | "system") {
  const root = document.documentElement;
  const media = window.matchMedia("(prefers-color-scheme: dark)");
  const effective =
    theme === "system" ? (media.matches ? "dark" : "light") : theme;

  root.classList.toggle("dark", effective === "dark");
  root.style.colorScheme = effective;
  _currentTheme.value = effective;
  localStorage.setItem(THEME_KEY, theme);

  if (!systemThemeListenerAttached) {
    media.addEventListener("change", () => {
      const storedTheme = getStoredTheme();
      if (storedTheme === "system") applyTheme("system");
    });
    systemThemeListenerAttached = true;
  }
}

export function setAuth(result: AuthResponse) {
  user.value = result.user;
  applyTheme(result.user.theme || "system");
}

export function setAuthReady() {
  ready.value = true;
}

export function clearAuth() {
  user.value = null;
  applyTheme(getStoredTheme());
}
