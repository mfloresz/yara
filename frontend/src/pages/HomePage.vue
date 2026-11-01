<template>
  <AppLayout>
    <section class="home" aria-label="Inicio">
      <header class="home-header">
        <div class="home-heading">
          <p class="home-date">{{ today }}</p>
          <h1 class="home-title">{{ greeting }}</h1>
          <p class="home-subtitle muted">Continúa donde lo dejaste.</p>
        </div>
        <nav class="home-links" aria-label="Ir a la biblioteca">
          <RouterLink to="/novels" class="home-link">
            <n-icon :size="15"><BookOutline /></n-icon>
            Novelas
          </RouterLink>
          <RouterLink to="/comics" class="home-link">
            <n-icon :size="15"><ImagesOutline /></n-icon>
            Cómics
          </RouterLink>
        </nav>
      </header>

      <div v-if="hasActive" class="home-jobs">
        <RouterLink to="/operations" class="home-jobs-link">
          <n-icon :size="16"><FlashOutline /></n-icon>
          Hay trabajos en curso — ver Operaciones
        </RouterLink>
      </div>

      <!-- Continuar leyendo: novelas y cómics con progreso registrado,
           ordenados por lo más recientemente leído. -->
      <section v-if="visibleEntries.length" class="home-section" aria-label="Continuar leyendo">
        <div class="home-section-head">
          <h2 class="home-section-title">Continuar leyendo</h2>
        </div>
        <div class="home-shelf">
          <div v-for="entry in visibleEntries" :key="entry.kind + entry.id" class="home-shelf-item">
            <NovelCard
              v-if="entry.kind === 'novel'"
              :novel="entry.novel"
              @cleared="removeEntry(entry)"
            />
            <ComicCard v-else :comic="entry.comic" @cleared="removeEntry(entry)" />
            <span class="home-shelf-kind muted small">
              {{ entry.kind === "novel" ? "Novela" : "Cómic" }}
            </span>
          </div>
        </div>
      </section>

      <n-empty
        v-if="loaded && !visibleEntries.length"
        class="home-empty"
        :description="
          loadError ??
          'Aún no has empezado a leer. Abre una novela o un cómic y aparecerá aquí para retomarlo.'
        "
      >
        <template #extra>
          <div class="home-empty-actions">
            <n-button tag="a" href="/novels" type="primary" secondary>
              Ir a Novelas
            </n-button>
            <n-button tag="a" href="/comics" secondary>
              Ir a Cómics
            </n-button>
          </div>
        </template>
      </n-empty>
    </section>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { RouterLink } from "vue-router";
import { NButton, NEmpty, NIcon } from "naive-ui";
import { BookOutline, FlashOutline, ImagesOutline } from "@vicons/ionicons5";
import AppLayout from "@/components/AppLayout.vue";
import NovelCard from "@/components/NovelCard.vue";
import ComicCard from "@/components/ComicCard.vue";
import { useAppServices } from "@/app/services";
import { useActiveJobStatus } from "@/composables/useActiveJobStatus";
import { isActivelyReading, type Novel, type Comic } from "@/domain";

const { api } = useAppServices();
const { hasActive } = useActiveJobStatus();

type ContinueEntry =
  | { kind: "novel"; id: string; lastReadAt: string; novel: Novel }
  | { kind: "comic"; id: string; lastReadAt: string; comic: Comic };

const entries = ref<ContinueEntry[]>([]);
const loaded = ref(false);
const loadError = ref<string | null>(null);

// Filtrado reactivo: marcar "Leída"/"En pausa" desde una tarjeta saca la
// entrada del estante al instante, sin esperar a recargar.
const visibleEntries = computed(() =>
  entries.value.filter((entry) =>
    isActivelyReading(entry.kind === "novel" ? entry.novel.readingStatus : entry.comic.readingStatus),
  ),
);

const today = new Intl.DateTimeFormat("es", {
  weekday: "long",
  day: "numeric",
  month: "long",
}).format(new Date());

function timeGreeting() {
  const h = new Date().getHours();
  if (h < 5) return "Buenas noches";
  if (h < 12) return "Buenos días";
  if (h < 19) return "Buenas tardes";
  return "Buenas noches";
}
const greeting = timeGreeting();

onMounted(async () => {
  const [novelsRes, comicsRes] = await Promise.allSettled([
    // Convención de la API: para lastRead, "asc" ordena lo más reciente
    // primero; los elementos sin progreso quedan al final y el filtro los
    // descarta.
    api.novels.list({ sort: "lastRead", order: "asc", limit: 12 }),
    api.comics.list({ sort: "lastRead", order: "asc", limit: 12 }),
  ]);
  const merged: ContinueEntry[] = [];
  if (novelsRes.status === "fulfilled") {
    for (const n of novelsRes.value.items) {
      // El filtro por estado de lectura vive en visibleEntries; aquí solo se
      // exige que exista actividad de lectura.
      if (n.lastReadAt) {
        merged.push({ kind: "novel", id: n.id, lastReadAt: n.lastReadAt, novel: n });
      }
    }
  }
  if (comicsRes.status === "fulfilled") {
    for (const c of comicsRes.value.items) {
      if (c.lastReadAt) {
        merged.push({ kind: "comic", id: c.id, lastReadAt: c.lastReadAt, comic: c });
      }
    }
  }
  if (novelsRes.status === "rejected" && comicsRes.status === "rejected") {
    loadError.value = "No se pudo cargar tu actividad de lectura.";
  }
  merged.sort((a, b) => b.lastReadAt.localeCompare(a.lastReadAt));
  entries.value = merged.slice(0, 12);
  loaded.value = true;
});

function removeEntry(entry: ContinueEntry) {
  entries.value = entries.value.filter((e) => e !== entry);
}
</script>

<style scoped>
.home {
  display: flex;
  flex-direction: column;
  gap: 1.75rem;
  padding: 1.5rem 0 3rem;
}

.home-header {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 1rem;
  flex-wrap: wrap;
}

.home-heading {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
}

.home-date {
  margin: 0;
  font-size: 0.75rem;
  text-transform: capitalize;
  letter-spacing: 0.08em;
  color: var(--text-secondary);
}

.home-title {
  font-size: 1.75rem;
  font-weight: 700;
  letter-spacing: -0.02em;
  margin: 0;
}

.home-subtitle {
  margin: 0;
  font-size: 0.9375rem;
}

.home-links {
  display: inline-flex;
  gap: 0.5rem;
}

.home-link {
  display: inline-flex;
  align-items: center;
  gap: 0.4rem;
  padding: 0.4rem 0.875rem;
  border: 1px solid var(--divide);
  border-radius: var(--radius-pill);
  background: var(--surface-elevated);
  color: var(--foreground);
  font-size: 0.875rem;
  text-decoration: none;
}

.home-link:hover {
  border-color: var(--accent-link);
  color: var(--accent-link);
}

.home-jobs {
  border: 1px solid color-mix(in oklab, var(--accent-link, #57544c) 30%, var(--divide));
  background: color-mix(in oklab, var(--surface-elevated) 92%, var(--accent-link, #57544c));
  border-radius: var(--radius-md);
  padding: 0.5rem 0.875rem;
}

.home-jobs-link {
  display: inline-flex;
  align-items: center;
  gap: 0.5rem;
  color: var(--accent-link);
  font-size: 0.875rem;
  font-weight: 500;
}

.home-section {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
}

.home-section-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 1rem;
}

.home-section-title {
  font-size: 1.125rem;
  font-weight: 600;
  margin: 0;
}

/* Estante horizontal: el último lo leído primero, sin
   ocupar la página entera. */
.home-shelf {
  display: flex;
  gap: 1rem;
  overflow-x: auto;
  padding-bottom: 0.5rem;
  scroll-snap-type: x proximity;
}

.home-shelf-item {
  display: flex;
  flex-direction: column;
  flex: 0 0 138px;
  scroll-snap-align: start;
}

.home-shelf-kind {
  padding-top: 0.25rem;
}

.home-empty-actions {
  display: inline-flex;
  gap: 0.5rem;
}

@media (max-width: 640px) {
  .home {
    padding-top: 1rem;
    gap: 1.25rem;
  }

  .home-shelf-item {
    flex-basis: 112px;
  }

  .home-shelf {
    gap: 0.75rem;
  }
}
</style>
