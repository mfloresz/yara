import { ref, onScopeDispose, type Ref } from "vue";
import { useAppServices } from "@/app/services";
import { useOfflineCache } from "@/composables/useOfflineCache";

export function useReadingProgress(
  novelId: Ref<string>,
  activeChapterId: Ref<string | null>,
  scrollPercent: Ref<number>,
) {
  const { api } = useAppServices();
  const {
    cachedReadingProgress,
    loadCachedReadingProgress,
    saveReadingProgress,
  } = useOfflineCache(novelId);

  const savedChapterId = ref<string | null>(null);
  const savedScrollPercent = ref(0);
  const isLoaded = ref(false);
  let saveTimer: ReturnType<typeof setInterval> | null = null;

  async function load() {
    if (!novelId.value) {
      isLoaded.value = true;
      return;
    }
    try {
      const result = await api.readingProgress.get(novelId.value);
      const local =
        cachedReadingProgress.value[novelId.value] ??
        (await loadCachedReadingProgress())[novelId.value];
      const progress = local && !local.synced ? local : result ?? local;
      if (progress?.chapterId) {
        savedChapterId.value = progress.chapterId;
        savedScrollPercent.value = progress.scrollPercent ?? 0;
      } else {
        savedChapterId.value = null;
        savedScrollPercent.value = 0;
      }
    } catch {
      const local =
        cachedReadingProgress.value[novelId.value] ??
        (await loadCachedReadingProgress())[novelId.value];
      savedChapterId.value = local?.chapterId ?? null;
      savedScrollPercent.value = local?.scrollPercent ?? 0;
    }
    isLoaded.value = true;
  }

  async function flush() {
    if (!novelId.value || !activeChapterId.value) return;

    const chapterId = activeChapterId.value;
    const percent = scrollPercent.value;

    // Guardar primero en IndexedDB garantiza que cerrar la app o perder la
    // conexión no destruya el último progreso conocido.
    await saveReadingProgress(novelId.value, chapterId, percent, false);

    try {
      await api.readingProgress.update(novelId.value, {
        chapterId,
        scrollPercent: percent,
      });
      await saveReadingProgress(novelId.value, chapterId, percent, true);
    } catch {
      // El registro queda pendiente y useOfflineCache lo sincronizará al volver
      // la conexión o en el siguiente intento.
    }
  }

  function startAutoSave() {
    stopAutoSave();
    saveTimer = setInterval(() => {
      void flush();
    }, 30_000);
  }

  function stopAutoSave() {
    if (saveTimer !== null) {
      clearInterval(saveTimer);
      saveTimer = null;
    }
  }

  onScopeDispose(() => {
    stopAutoSave();
    void flush();
  });

  return {
    savedChapterId,
    savedScrollPercent,
    isLoaded,
    load,
    flush,
    startAutoSave,
    stopAutoSave,
  };
}
