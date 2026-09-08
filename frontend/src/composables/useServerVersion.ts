import { onMounted, ref } from "vue";
import { getApiBaseUrl } from "@/utils/api-base-url";

// Versión del binario backend (ldflags main.Version), expuesta vía
// GET /healthz como {ok, version}. Cacheada a nivel de módulo para
// hacer un solo fetch por sesión del SPA.
const cachedVersion = ref<string | null>(null);
let inflight: Promise<string> | null = null;

async function fetchVersion(): Promise<string> {
  if (cachedVersion.value !== null) return cachedVersion.value;
  if (inflight) return inflight;
  inflight = fetch(`${getApiBaseUrl()}/healthz`, { credentials: "include" })
    .then((res) => (res.ok ? res.json() : null))
    .then((body) => {
      const version =
        body && typeof body.version === "string" && body.version.trim() !== ""
          ? body.version
          : "dev";
      cachedVersion.value = version;
      return version;
    })
    .catch(() => {
      cachedVersion.value = "dev";
      return "dev";
    })
    .finally(() => {
      inflight = null;
    });
  return inflight;
}

export function useServerVersion() {
  const version = ref<string | null>(cachedVersion.value);

  onMounted(async () => {
    if (cachedVersion.value !== null) {
      version.value = cachedVersion.value;
      return;
    }
    version.value = await fetchVersion();
  });

  return { version };
}
