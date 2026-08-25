import { computed, onScopeDispose, ref, watch } from "vue";
import type { ApiClient } from "@/api/client";
import { useAppServices } from "@/app/services";
import { onJobChanged } from "@/utils/job-events";
import { createPoller } from "@/utils/poller";

const hasActiveState = ref(false);
const loading = ref(false);

let apiClient: ApiClient | null = null;
let unsubscribeJobChanged: (() => void) | null = null;
let listeners = 0;
let poller: ReturnType<typeof createPoller<{ hasActive: boolean }>> | null = null;

export function useActiveJobStatus() {
  const { api } = useAppServices();
  apiClient = api;

  listeners++;
  if (listeners === 1) {
    poller = createPoller<{ hasActive: boolean }>({
      fetcher: () => apiClient!.jobs.status(),
      shouldPoll: (latest) => latest?.hasActive ?? false,
    });
    watch(poller.data, (latest) => {
      hasActiveState.value = latest?.hasActive ?? false;
    });
    watch(poller.loading, (v) => (loading.value = v));
    unsubscribeJobChanged = onJobChanged(() => {
      void poller!.refresh();
    });
  }

  onScopeDispose(() => {
    listeners = Math.max(0, listeners - 1);
    if (listeners === 0) {
      poller = null;
      apiClient = null;
      unsubscribeJobChanged?.();
      unsubscribeJobChanged = null;
      hasActiveState.value = false;
      loading.value = false;
    }
  });

  return {
    hasActive: computed(() => hasActiveState.value),
    loading,
    refreshStatus: () => poller?.refresh() ?? Promise.resolve(),
  };
}
