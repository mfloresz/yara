export const STORAGE_KEYS = {
  theme: "theme",
  authUser: "yara.auth.user",
  readerSettings: "reader-settings",
  // Per-user dashboard view preferences (grid/list, sort, etc.)
  // key is built as: `${dashboardPrefsPrefix}<userId>`
  dashboardPrefsPrefix: "yara.dashboard.prefs.",
} as const;

export function dashboardPrefsKey(userId: string): string {
  return `${STORAGE_KEYS.dashboardPrefsPrefix}${userId}`;
}
