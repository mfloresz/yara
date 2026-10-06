import { computed, onScopeDispose, ref, unref, watch, type Ref } from "vue";
import type { TranslationJob } from "@/domain";
import { useAppServices } from "@/app/services";
import { emitJobChanged, onJobChanged } from "@/utils/job-events";
import { createPoller } from "@/utils/poller";

export function useActiveJobs(
  options: { enabled?: boolean | Ref<boolean> } = {},
) {
  const { api } = useAppServices();
  const jobs = ref<TranslationJob[]>([]);
  const isEnabled = computed(() => unref(options.enabled) ?? true);

  const poller = createPoller<TranslationJob[]>({
    fetcher: () => api.jobs.listActive(),
    shouldPoll: (latest) =>
      (latest ?? []).some(
        (job) => job.status === "pending" || job.status === "running",
      ),
    enabled: isEnabled,
  });

  watch(
    poller.data,
    (latest) => {
      const next = latest ?? [];
      // Los jobs activos se sondean cada 2s; si nada cambió, conservar la
      // identidad del array evita re-renderizar a todos los consumidores.
      if (JSON.stringify(jobs.value) === JSON.stringify(next)) return;
      jobs.value = next;
    },
    { immediate: true },
  );

  const unsubscribeJobChanged = onJobChanged(() => {
    void poller.refresh();
  });
  onScopeDispose(unsubscribeJobChanged);

  async function cancelJob(jobId: string) {
    const updated = await api.jobs.update(jobId, { status: "cancelled" });
    jobs.value = jobs.value.filter((item) => item.id !== jobId);
    emitJobChanged();
    return updated;
  }

  return {
    jobs,
    loading: poller.loading,
    activeCount: computed(() => jobs.value.length),
    listActiveJobs: () => poller.refresh().then(() => jobs.value),
    cancelJob,
  };
}
