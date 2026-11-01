import { createRouter, createWebHistory } from "vue-router";
import { authState } from "@/app/auth";
import { setDocumentTitle } from "@/composables/useDocumentTitle";

const LoginPage = () => import("@/pages/LoginPage.vue");
const InvitePage = () => import("@/pages/InvitePage.vue");
const HomePage = () => import("@/pages/HomePage.vue");
const DashboardPage = () => import("@/pages/DashboardPage.vue");
const ComicsLibraryPage = () => import("@/pages/ComicsLibraryPage.vue");
const ComicDetailPage = () => import("@/pages/ComicDetailPage.vue");
const ComicChapterPage = () => import("@/pages/ComicChapterPage.vue");
const ComicReaderPage = () => import("@/pages/ComicReaderPage.vue");
const SettingsPage = () => import("@/pages/SettingsPage.vue");
const OperationsPage = () => import("@/pages/OperationsPage.vue");
const NovelDetailPage = () => import("@/pages/NovelDetailPage.vue");
const ChapterPage = () => import("@/pages/ChapterPage.vue");
const ReaderPage = () => import("@/pages/ReaderPage.vue");
const AdminPage = () => import("@/pages/AdminPage.vue");
const ResetPasswordPage = () => import("@/pages/ResetPasswordPage.vue");
const ChatPage = () => import("@/pages/ChatPage.vue");

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: "/login",
      name: "login",
      component: LoginPage,
      meta: { guestOnly: true },
    },
    {
      path: "/register",
      redirect: "/invite",
    },
    {
      path: "/invite/:token?",
      name: "invite",
      component: InvitePage,
      meta: { guestOnly: true },
    },
    {
      path: "/reset-password/:token?",
      name: "reset-password",
      component: ResetPasswordPage,
      meta: { guestOnly: true },
    },
    {
      path: "/admin",
      name: "admin",
      component: AdminPage,
      meta: { requiresAuth: true, requiresAdmin: true, title: "Admin" },
    },
    {
      path: "/",
      name: "home",
      component: HomePage,
      meta: { requiresAuth: true, title: "Inicio" },
    },
    {
      path: "/novels",
      name: "dashboard",
      component: DashboardPage,
      meta: { requiresAuth: true, title: "Novelas" },
    },
    {
      path: "/comics",
      name: "comics-library",
      component: ComicsLibraryPage,
      meta: { requiresAuth: true, title: "Cómics" },
    },
    // Compat: la navegación anterior vivía bajo /library.
    { path: "/library", redirect: "/novels" },
    { path: "/library/comics", redirect: "/comics" },
    {
      path: "/settings",
      name: "settings",
      component: SettingsPage,
      meta: { requiresAuth: true, title: "Ajustes" },
    },
    {
      path: "/operations",
      name: "operations",
      component: OperationsPage,
      meta: { requiresAuth: true, title: "Operaciones" },
    },
    {
      path: "/chat",
      name: "chat",
      component: ChatPage,
      meta: { requiresAuth: true, title: "Asistente" },
    },
    {
      path: "/novels/:novelId",
      name: "novel-detail",
      component: NovelDetailPage,
      meta: { requiresAuth: true },
    },
    {
      path: "/novels/:novelId/chapters/:chapterId",
      name: "chapter-detail",
      component: ChapterPage,
      meta: { requiresAuth: true },
    },
    {
      path: "/novels/:novelId/read",
      name: "reader",
      component: ReaderPage,
      meta: { requiresAuth: true, immersive: true },
    },
    {
      path: "/comics/:comicId",
      name: "comic-detail",
      component: ComicDetailPage,
      meta: { requiresAuth: true },
    },
    {
      path: "/comics/:comicId/chapters/:chapterId",
      name: "comic-chapter-detail",
      component: ComicChapterPage,
      meta: { requiresAuth: true },
    },
    {
      path: "/comics/:comicId/read",
      name: "comic-reader",
      component: ComicReaderPage,
      meta: { requiresAuth: true, immersive: true },
    },
    { path: "/:pathMatch(.*)*", redirect: "/" },
  ],
  scrollBehavior() {
    return { top: 0 };
  },
});

router.beforeEach((to) => {
  // Deep-link de la extensión (?importUrl=...&into=1): la extensión abre "/",
  // pero quien consume el parámetro es la biblioteca de novelas.
  if (to.query.importUrl && to.path !== "/novels") {
    return { path: "/novels", query: to.query };
  }
  // Fail closed while the session is still loading: never render protected
  // content (in particular /admin) on an unverified role. The router is
  // attached after restoreSession in main.ts, so this branch only covers
  // programmatic edge navigations — the checks mirror the ready ones below.
  if (!authState.ready.value) {
    if (to.meta.requiresAuth && !authState.isAuthenticated.value) {
      return { name: "login", query: { redirect: to.fullPath } };
    }
    if (to.meta.requiresAdmin && !authState.isAdmin.value) {
      return { name: "dashboard" };
    }
    return true;
  }
  if (to.meta.requiresAuth && !authState.isAuthenticated.value) {
    return { name: "login", query: { redirect: to.fullPath } };
  }
  if (to.meta.guestOnly && authState.isAuthenticated.value) {
    // Sesión ya activa entrando a /login?redirect=/api/... (flujo de la
    // extensión): no tirar el redirect al dashboard, salir al destino real
    // con recarga completa porque no es una ruta del SPA.
    const redirect = String(to.query.redirect || "");
    if (redirect.startsWith("/api/")) {
      window.location.href = redirect;
      return false;
    }
    return { name: "dashboard" };
  }
  if (to.meta.requiresAdmin && !authState.isAdmin.value) {
    return { name: "dashboard" };
  }
  return true;
});

// After a deploy, a stale index.html can reference hashed chunks that no
// longer exist on the server. The dynamic import then fails and vue-router
// cancels the navigation silently. Reload once so the browser fetches the
// fresh chunk manifest; the sessionStorage flag guards against reload loops.
const CHUNK_RELOAD_KEY = "yara-chunk-reload";

router.onError((err) => {
  const msg = err instanceof Error ? err.message : String(err);
  if (
    /Failed to fetch dynamically imported module|Importing a module script failed|Loading chunk|ChunkLoadError/i.test(
      msg,
    )
  ) {
    if (!sessionStorage.getItem(CHUNK_RELOAD_KEY)) {
      sessionStorage.setItem(CHUNK_RELOAD_KEY, "1");
      window.location.reload();
    }
  }
});

// Static titles live here; novel-scoped routes (detail, chapter edition,
// reader) set `Yara - <novel title>` from their own page once the novel loads.
const DYNAMIC_TITLE_ROUTES = new Set(["novel-detail", "chapter-detail", "reader", "comic-detail", "comic-chapter-detail", "comic-reader"]);

router.afterEach((to) => {
  sessionStorage.removeItem(CHUNK_RELOAD_KEY);
  if (to.name != null && DYNAMIC_TITLE_ROUTES.has(String(to.name))) return;
  setDocumentTitle(typeof to.meta.title === "string" ? to.meta.title : null);
});
