import { createRouter, createWebHistory } from "vue-router";
import { authState } from "@/app/auth";

const LoginPage = () => import("@/pages/LoginPage.vue");
const InvitePage = () => import("@/pages/InvitePage.vue");
const DashboardPage = () => import("@/pages/DashboardPage.vue");
const SettingsPage = () => import("@/pages/SettingsPage.vue");
const OperationsPage = () => import("@/pages/OperationsPage.vue");
const NovelDetailPage = () => import("@/pages/NovelDetailPage.vue");
const ChapterPage = () => import("@/pages/ChapterPage.vue");
const ReaderPage = () => import("@/pages/ReaderPage.vue");
const AdminPage = () => import("@/pages/AdminPage.vue");

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
      path: "/admin",
      name: "admin",
      component: AdminPage,
      meta: { requiresAuth: true, requiresAdmin: true },
    },
    {
      path: "/",
      name: "dashboard",
      component: DashboardPage,
      meta: { requiresAuth: true },
    },
    {
      path: "/settings",
      name: "settings",
      component: SettingsPage,
      meta: { requiresAuth: true },
    },
    {
      path: "/operations",
      name: "operations",
      component: OperationsPage,
      meta: { requiresAuth: true },
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
      meta: { requiresAuth: true },
    },
    { path: "/:pathMatch(.*)*", redirect: "/" },
  ],
  scrollBehavior() {
    return { top: 0 };
  },
});

router.beforeEach((to) => {
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
    return { name: "dashboard" };
  }
  if (to.meta.requiresAdmin && !authState.isAdmin.value) {
    return { name: "dashboard" };
  }
  return true;
});
