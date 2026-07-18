/** Generate a UUID, falling back to a non-crypto id when running on an
 * insecure origin (HTTP on a remote host) where `crypto.randomUUID` is
 * unavailable. `crypto.randomUUID` only exists in secure contexts. */
export function safeUuid(): string {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
    return crypto.randomUUID();
  }
  return `id-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
}
