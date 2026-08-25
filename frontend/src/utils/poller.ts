import { onScopeDispose, ref, watch, type Ref } from "vue";

export interface PollerOptions<T> {
  fetcher: () => Promise<T>;
  shouldPoll: (latest: T | undefined) => boolean;
  intervalMs?: number;
  enabled?: Ref<boolean>;
}

export interface PollerHandle<T> {
  data: Ref<T | undefined>;
  loading: Ref<boolean>;
  refresh: () => Promise<void>;
}

const DEFAULT_INTERVAL_MS = 2000;

export function createPoller<T>(options: PollerOptions<T>): PollerHandle<T> {
  const intervalMs = options.intervalMs ?? DEFAULT_INTERVAL_MS;
  const data = ref<T | undefined>(undefined) as Ref<T | undefined>;
  const loading = ref(false);

  let intervalId: number | null = null;
  let inflight = false;

  function stop() {
    if (intervalId !== null) {
      window.clearInterval(intervalId);
      intervalId = null;
    }
  }

  function start() {
    stop();
    intervalId = window.setInterval(() => {
      void refresh();
    }, intervalMs);
  }

  function sync() {
    if (options.enabled && !options.enabled.value) {
      stop();
      return;
    }
    if (options.shouldPoll(data.value)) {
      if (intervalId === null) start();
      return;
    }
    stop();
  }

  async function refresh() {
    if (options.enabled && !options.enabled.value) return;
    if (inflight) return;
    inflight = true;
    loading.value = true;
    try {
      const result = await options.fetcher();
      data.value = result;
    } catch {
      // ignore network errors while polling
    } finally {
      loading.value = false;
      inflight = false;
      sync();
    }
  }

  if (options.enabled) {
    watch(
      options.enabled,
      (enabled) => {
        if (enabled) {
          void refresh();
          return;
        }
        stop();
        loading.value = false;
        data.value = undefined;
      },
      { immediate: true },
    );
  } else {
    void refresh();
  }

  onScopeDispose(stop);

  return { data, loading, refresh };
}
