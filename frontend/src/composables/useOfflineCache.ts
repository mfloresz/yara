import { ref, computed, watch, type Ref } from "vue";
import { useAppServices } from "@/app/services";
import type { Novel, Chapter } from "@/domain";
import type { ChapterSummary } from "@/api/types";

// Nombre de la base de datos IndexedDB
const DB_NAME = "yara-offline-cache";
const DB_VERSION = 1;

// Nombres de los object stores
const NOVELS_STORE = "novels";
const CHAPTERS_STORE = "chapters";
const READING_PROGRESS_STORE = "readingProgress";
const PENDING_SYNC_STORE = "pendingSync";

// Tipos para el cache offline
export interface CachedNovel {
  id: string;
  novel: Novel;
  chapters: Chapter[];
  cachedAt: number;
  lastSyncedAt?: number;
}

export interface CachedReadingProgress {
  id: string;
  novelId: string;
  chapterId: string;
  scrollPercent: number;
  updatedAt: number;
  synced: boolean;
}

export interface SyncStatus {
  isOnline: boolean;
  pendingChanges: number;
  lastSyncAt?: number;
  cachedNovels: number;
}

// Database singleton
let dbPromise: Promise<IDBDatabase> | null = null;

function openDB(): Promise<IDBDatabase> {
  if (dbPromise) return dbPromise;

  dbPromise = new Promise((resolve, reject) => {
    const request = indexedDB.open(DB_NAME, DB_VERSION);

    request.onerror = () => reject(request.error);
    request.onsuccess = () => resolve(request.result);

    request.onupgradeneeded = (event) => {
      const db = request.result;
      
      // Crear object stores si no existen
      if (!db.objectStoreNames.contains(NOVELS_STORE)) {
        const novelsStore = db.createObjectStore(NOVELS_STORE, { keyPath: "id" });
        novelsStore.createIndex("cachedAt", "cachedAt", { unique: false });
      }

      if (!db.objectStoreNames.contains(CHAPTERS_STORE)) {
        const chaptersStore = db.createObjectStore(CHAPTERS_STORE, { keyPath: "id" });
        chaptersStore.createIndex("novelId", "novelId", { unique: false });
        chaptersStore.createIndex("chapterOrder", "chapterOrder", { unique: false });
      }

      if (!db.objectStoreNames.contains(READING_PROGRESS_STORE)) {
        const progressStore = db.createObjectStore(READING_PROGRESS_STORE, { keyPath: "novelId" });
        progressStore.createIndex("updatedAt", "updatedAt", { unique: false });
        progressStore.createIndex("synced", "synced", { unique: false });
      }

      if (!db.objectStoreNames.contains(PENDING_SYNC_STORE)) {
        db.createObjectStore(PENDING_SYNC_STORE, { keyPath: "id", autoIncrement: true });
      }
    };
  });

  return dbPromise;
}

// Función para obtener una transacción
async function getTransaction(mode: IDBTransactionMode, stores: string[]): Promise<IDBTransaction> {
  const db = await openDB();
  return db.transaction(stores, mode);
}

export function useOfflineCache(novelId: Ref<string>) {
  const { api } = useAppServices();

  const cachedNovels = ref<Record<string, CachedNovel>>({});
  const cachedReadingProgress = ref<Record<string, CachedReadingProgress>>({});
  const isOnline = ref(navigator.onLine);
  const pendingSyncCount = ref(0);
  const isLoading = ref(false);
  const error = ref<string | null>(null);

  // Escuchar cambios de conexión
  window.addEventListener("online", () => {
    isOnline.value = true;
    syncPendingChanges();
  });

  window.addEventListener("offline", () => {
    isOnline.value = false;
  });

  // Cargar todas las novelas cacheadas
  async function loadCachedNovels(): Promise<Record<string, CachedNovel>> {
    try {
      const db = await openDB();
      const transaction = db.transaction(NOVELS_STORE, "readonly");
      const store = transaction.objectStore(NOVELS_STORE);
      
      return new Promise((resolve, reject) => {
        const request = store.getAll();
        request.onsuccess = () => {
          const novels: Record<string, CachedNovel> = {};
          request.result.forEach((cached: CachedNovel) => {
            novels[cached.id] = cached;
          });
          resolve(novels);
        };
        request.onerror = () => reject(request.error);
      });
    } catch {
      return {};
    }
  }

  // Cargar el progreso de lectura cacheado
  async function loadCachedReadingProgress(): Promise<Record<string, CachedReadingProgress>> {
    try {
      const db = await openDB();
      const transaction = db.transaction(READING_PROGRESS_STORE, "readonly");
      const store = transaction.objectStore(READING_PROGRESS_STORE);
      
      return new Promise((resolve, reject) => {
        const request = store.getAll();
        request.onsuccess = () => {
          const progress: Record<string, CachedReadingProgress> = {};
          request.result.forEach((p: CachedReadingProgress) => {
            const current = progress[p.novelId];
            if (!current || p.updatedAt > current.updatedAt) {
              progress[p.novelId] = p;
            }
          });
          resolve(progress);
        };
        request.onerror = () => reject(request.error);
      });
    } catch {
      return {};
    }
  }

  // Guardar una novela completa en caché
  async function cacheNovel(novel: Novel, chapters: Chapter[]): Promise<void> {
    try {
      const db = await openDB();
      const transaction = db.transaction([NOVELS_STORE, CHAPTERS_STORE], "readwrite");
      
      const cachedNovel: CachedNovel = {
        id: novel.id,
        novel,
        chapters,
        cachedAt: Date.now(),
        lastSyncedAt: Date.now(),
      };

      // Guardar novela
      transaction.objectStore(NOVELS_STORE).put(cachedNovel);

      // Guardar capítulos
      const chaptersStore = transaction.objectStore(CHAPTERS_STORE);
      chapters.forEach((chapter) => {
        chaptersStore.put({ ...chapter, novelId: novel.id });
      });

      await new Promise<void>((resolve, reject) => {
        transaction.oncomplete = () => resolve();
        transaction.onerror = () => reject(transaction.error);
      });

      // Actualizar el estado local
      cachedNovels.value = { ...cachedNovels.value, [novel.id]: cachedNovel };
    } catch (err) {
      error.value = `Error al guardar novela: ${err}`;
      throw err;
    }
  }

  // Obtener una novela cacheada
  async function getCachedNovel(novelId: string): Promise<CachedNovel | null> {
    try {
      const db = await openDB();
      const transaction = db.transaction([NOVELS_STORE, CHAPTERS_STORE], "readonly");
      
      const novelsStore = transaction.objectStore(NOVELS_STORE);
      const cached = await new Promise<CachedNovel | undefined>((resolve, reject) => {
        const request = novelsStore.get(novelId);
        request.onsuccess = () => resolve(request.result);
        request.onerror = () => reject(request.error);
      });

      if (!cached) return null;

      // Obtener capítulos
      const chaptersStore = transaction.objectStore(CHAPTERS_STORE);
      const chaptersIndex = chaptersStore.index("novelId");
      const chapters = await new Promise<Chapter[]>((resolve, reject) => {
        const request = chaptersIndex.getAll(novelId);
        request.onsuccess = () => resolve(request.result);
        request.onerror = () => reject(request.error);
      });

      return { ...cached, chapters };
    } catch {
      return null;
    }
  }

  // Guardar progreso de lectura localmente
  async function saveReadingProgress(
    novelId: string,
    chapterId: string,
    scrollPercent: number,
    synced: boolean = false
  ): Promise<void> {
    try {
      const db = await openDB();
      const transaction = db.transaction(READING_PROGRESS_STORE, "readwrite");
      const store = transaction.objectStore(READING_PROGRESS_STORE);

      const progress: CachedReadingProgress = {
        id: `${novelId}-${chapterId}`,
        novelId,
        chapterId,
        scrollPercent,
        updatedAt: Date.now(),
        synced,
      };

      // Mantener solo el último progreso de cada novela. La base existente
      // usa el id del capítulo como clave, por lo que eliminamos versiones
      // anteriores antes de guardar el nuevo valor.
      const existing = await new Promise<CachedReadingProgress[]>((resolve, reject) => {
        const request = store.getAll();
        request.onsuccess = () => resolve(request.result);
        request.onerror = () => reject(request.error);
      });
      existing
        .filter((item) => item.novelId === novelId)
        .forEach((item) => store.delete(item.id));
      store.put(progress);

      await new Promise<void>((resolve, reject) => {
        transaction.oncomplete = () => resolve();
        transaction.onerror = () => reject(transaction.error);
      });

      // Actualizar el estado local
      cachedReadingProgress.value = { ...cachedReadingProgress.value, [novelId]: progress };

      pendingSyncCount.value = Object.values(cachedReadingProgress.value).filter(
        (item) => !item.synced,
      ).length;
    } catch (err) {
      error.value = `Error al guardar progreso: ${err}`;
      throw err;
    }
  }

  // Sincronizar progreso de lectura con el servidor
  async function syncReadingProgress(novelId: string): Promise<void> {
    try {
      const progress = cachedReadingProgress.value[novelId];
      if (!progress || progress.synced) return;

      await api.readingProgress.update(novelId, {
        chapterId: progress.chapterId,
        scrollPercent: progress.scrollPercent,
      });

      // Marcar como sincronizado
      progress.synced = true;
      cachedReadingProgress.value = { ...cachedReadingProgress.value, [novelId]: progress };
      pendingSyncCount.value = Math.max(0, pendingSyncCount.value - 1);

      // Actualizar en IndexedDB
      const db = await openDB();
      const transaction = db.transaction(READING_PROGRESS_STORE, "readwrite");
      transaction.objectStore(READING_PROGRESS_STORE).put(progress);
      await new Promise<void>((resolve) => {
        transaction.oncomplete = () => resolve();
      });
    } catch (err) {
      error.value = `Error al sincronizar progreso: ${err}`;
      throw err;
    }
  }

  // Sincronizar todas las novelas pendientes
  async function syncPendingChanges(): Promise<void> {
    if (!isOnline.value || pendingSyncCount.value === 0) return;

    try {
      // Sincronizar todos los progresos de lectura pendientes
      const db = await openDB();
      const transaction = db.transaction(READING_PROGRESS_STORE, "readonly");
      const store = transaction.objectStore(READING_PROGRESS_STORE);
      const index = store.index("synced");

      const unsynced = await new Promise<CachedReadingProgress[]>((resolve, reject) => {
        const request = index.getAll(0);
        request.onsuccess = () => resolve(request.result);
        request.onerror = () => reject(request.error);
      });
      const latestByNovel = new Map<string, CachedReadingProgress>();
      for (const progress of unsynced) {
        const current = latestByNovel.get(progress.novelId);
        if (!current || progress.updatedAt > current.updatedAt) {
          latestByNovel.set(progress.novelId, progress);
        }
      }

      for (const progress of latestByNovel.values()) {
        try {
          await api.readingProgress.update(progress.novelId, {
            chapterId: progress.chapterId,
            scrollPercent: progress.scrollPercent,
          });

          // Marcar como sincronizado en la DB
          const updateTx = db.transaction(READING_PROGRESS_STORE, "readwrite");
          const updateStore = updateTx.objectStore(READING_PROGRESS_STORE);
          updateStore.put({ ...progress, synced: true });
          await new Promise<void>((resolve, reject) => {
            updateTx.oncomplete = () => resolve();
            updateTx.onerror = () => reject(updateTx.error);
          });

          // Actualizar el estado local
          cachedReadingProgress.value = {
            ...cachedReadingProgress.value,
            [progress.novelId]: { ...progress, synced: true },
          };
        } catch {
          // Mantener el progreso pendiente para el siguiente intento.
        }
      }

      pendingSyncCount.value = Object.values(cachedReadingProgress.value).filter(
        (item) => !item.synced,
      ).length;
    } catch {
      // Error silencioso
    }
  }

  // Verificar si una novela está cacheada
  const isNovelCached = computed(() => {
    return !!cachedNovels.value[novelId.value];
  });

  // Obtener el estado de sincronización
  const syncStatus = computed<SyncStatus>(() => ({
    isOnline: isOnline.value,
    pendingChanges: pendingSyncCount.value,
    lastSyncAt: Object.values(cachedNovels.value)
      .map((n) => n.lastSyncedAt)
      .filter((t): t is number => t !== undefined)
      .sort((a, b) => b - a)[0],
    cachedNovels: Object.keys(cachedNovels.value).length,
  }));

  // Cargar datos iniciales
  async function init(): Promise<void> {
    isLoading.value = true;
    error.value = null;

    try {
      cachedNovels.value = await loadCachedNovels();
      cachedReadingProgress.value = await loadCachedReadingProgress();
      
      // Contar pendientes de sincronización
      const unsynced = Object.values(cachedReadingProgress.value).filter((p) => !p.synced);
      pendingSyncCount.value = unsynced.length;
      if (pendingSyncCount.value > 0 && isOnline.value) {
        void syncPendingChanges();
      }
    } catch (err) {
      error.value = `Error al cargar caché: ${err}`;
    } finally {
      isLoading.value = false;
    }
  }

  // Descargar novela completa (novela + capítulos) desde el servidor
  async function downloadNovelForOffline(novelId: string): Promise<CachedNovel | null> {
    try {
      isLoading.value = true;
      error.value = null;

      // Obtener novela completa con todos los capítulos
      const result = await api.novels.getFull(novelId);
      if (!result) return null;

      const { novel, chapters } = result;

      // Guardar en caché
      await cacheNovel(novel, chapters);

      return { id: novel.id, novel, chapters, cachedAt: Date.now(), lastSyncedAt: Date.now() };
    } catch (err) {
      error.value = `Error al descargar novela: ${err}`;
      return null;
    } finally {
      isLoading.value = false;
    }
  }

  // Eliminar una novela de la caché
  async function removeCachedNovel(novelId: string): Promise<void> {
    try {
      const db = await openDB();
      const transaction = db.transaction([NOVELS_STORE, CHAPTERS_STORE, READING_PROGRESS_STORE], "readwrite");

      // Eliminar novela
      transaction.objectStore(NOVELS_STORE).delete(novelId);

      // Eliminar capítulos
      const chaptersStore = transaction.objectStore(CHAPTERS_STORE);
      const index = chaptersStore.index("novelId");
      const chaptersToDelete = await new Promise<Chapter[]>((resolve, reject) => {
        const request = index.getAll(novelId);
        request.onsuccess = () => resolve(request.result);
        request.onerror = () => reject(request.error);
      });

      chaptersToDelete.forEach((chapter) => {
        chaptersStore.delete(chapter.id);
      });

      // Eliminar progreso de lectura
      transaction.objectStore(READING_PROGRESS_STORE).delete(novelId);

      await new Promise<void>((resolve, reject) => {
        transaction.oncomplete = () => resolve();
        transaction.onerror = () => reject(transaction.error);
      });

      // Actualizar el estado local
      const newCached = { ...cachedNovels.value };
      delete newCached[novelId];
      cachedNovels.value = newCached;

      const newProgress = { ...cachedReadingProgress.value };
      delete newProgress[novelId];
      cachedReadingProgress.value = newProgress;
    } catch (err) {
      error.value = `Error al eliminar novela de caché: ${err}`;
      throw err;
    }
  }

  // Actualizar una novela cacheada (para cuando se sincroniza)
  async function updateCachedNovel(novel: Novel, chapters: Chapter[]): Promise<void> {
    const cachedNovel: CachedNovel = {
      id: novel.id,
      novel,
      chapters,
      cachedAt: Date.now(),
      lastSyncedAt: Date.now(),
    };

    cachedNovels.value = { ...cachedNovels.value, [novel.id]: cachedNovel };

    try {
      const db = await openDB();
      const transaction = db.transaction([NOVELS_STORE, CHAPTERS_STORE], "readwrite");

      transaction.objectStore(NOVELS_STORE).put(cachedNovel);

      const chaptersStore = transaction.objectStore(CHAPTERS_STORE);
      chapters.forEach((chapter) => {
        chaptersStore.put({ ...chapter, novelId: novel.id });
      });

      await new Promise<void>((resolve, reject) => {
        transaction.oncomplete = () => resolve();
        transaction.onerror = () => reject(transaction.error);
      });
    } catch (err) {
      error.value = `Error al actualizar novela en caché: ${err}`;
      throw err;
    }
  }

  // Watches
  watch(isOnline, (online) => {
    if (online) {
      syncPendingChanges();
    }
  });

  // Inicializar al crear el composable
  init();

  return {
    // Estados
    cachedNovels,
    cachedReadingProgress,
    isOnline,
    pendingSyncCount,
    isLoading,
    error,
    isNovelCached,
    syncStatus,

    // Métodos
    loadCachedNovels,
    loadCachedReadingProgress,
    cacheNovel,
    getCachedNovel,
    saveReadingProgress,
    syncReadingProgress,
    syncPendingChanges,
    downloadNovelForOffline,
    removeCachedNovel,
    updateCachedNovel,
  };
}

