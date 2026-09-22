import { computed, h } from "vue";
import { useRouter } from "vue-router";
import type {
  DropdownDividerOption,
  DropdownGroupOption,
  DropdownOption,
  DropdownRenderOption,
} from "naive-ui";
import { useAppServices } from "@/app/services";
import { useServerVersion } from "@/composables/useServerVersion";
import ThemeSwitcher from "@/components/ThemeSwitcher.vue";

type UserMenuOption =
  | DropdownOption
  | DropdownDividerOption
  | DropdownGroupOption
  | DropdownRenderOption;

export function useUserMenu() {
  const router = useRouter();
  const { auth, logout } = useAppServices();
  const { version: serverVersion } = useServerVersion();

  const userMenuOptions = computed<UserMenuOption[]>(() => {
    const options: UserMenuOption[] = [
      { label: auth.user.value?.email ?? "", key: "email", disabled: true },
      { type: "divider", key: "d1" },
      { label: "Configuración", key: "settings" },
    ];
    if (auth.isAdmin.value) {
      options.push({ label: "Administración", key: "admin" });
    }
    options.push({ label: "Cerrar sesión", key: "logout" });
    options.push({ type: "divider", key: "d-theme" });
    options.push({
      key: "theme",
      type: "render",
      render: () =>
        h("div", { class: "user-menu-theme" }, [
          h("div", { class: "user-menu-theme-label" }, "Tema"),
          h(ThemeSwitcher),
        ]),
    });
    options.push({
      label: serverVersion.value ? `Yara ${serverVersion.value}` : "Yara…",
      key: "version",
      disabled: true,
    });
    return options;
  });

  async function doLogout() {
    await logout();
    await router.push("/login");
  }

  function handleUserMenuSelect(key: string | number) {
    if (key === "settings") void router.push("/settings");
    else if (key === "admin") void router.push("/admin");
    else if (key === "logout") void doLogout();
  }

  return { userMenuOptions, handleUserMenuSelect, doLogout };
}
