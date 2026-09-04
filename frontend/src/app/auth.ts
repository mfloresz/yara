import { computed, ref, readonly } from "vue";
import { darkTheme } from "naive-ui";
import type { GlobalThemeOverrides } from "naive-ui";
import type { AuthResponse, AuthUser } from "@/api/types";
import {
  pixeoThemeOverrides,
  pixeoDarkThemeOverrides,
} from "@/theme/naive-theme";
import { STORAGE_KEYS } from "@/app/storage-keys";

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
  isAdmin: computed(() => user.value?.role === "admin"),
};

export function getStoredTheme(): "light" | "dark" | "system" {
  return (
    (localStorage.getItem(STORAGE_KEYS.theme) as "light" | "dark" | "system" | null) ||
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
  localStorage.setItem(STORAGE_KEYS.theme, theme);

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
  localStorage.setItem(STORAGE_KEYS.authUser, JSON.stringify(result.user));
  applyTheme(result.user.theme || "system");
}

export function restoreStoredUser(): AuthUser | null {
  try {
    const raw = localStorage.getItem(STORAGE_KEYS.authUser);
    if (!raw) return null;
    return JSON.parse(raw) as AuthUser;
  } catch {
    localStorage.removeItem(STORAGE_KEYS.authUser);
    return null;
  }
}

export function setAuthReady() {
  ready.value = true;
}

export function clearAuth() {
  user.value = null;
  localStorage.removeItem(STORAGE_KEYS.authUser);
  applyTheme(getStoredTheme());
}
