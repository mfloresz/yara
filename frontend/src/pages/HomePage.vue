<template>
  <AppLayout>
    <section class="home" aria-label="Inicio">
      <header class="home-header">
        <h1 class="home-title">Inicio</h1>
        <p class="home-subtitle muted">
          Tu biblioteca de novelas y cómics, lista para continuar.
        </p>
      </header>

      <div v-if="hasActive" class="home-jobs">
        <RouterLink to="/operations" class="home-jobs-link">
          <n-icon :size="16"><FlashOutline /></n-icon>
          Hay trabajos en curso — ver Operaciones
        </RouterLink>
      </div>

      <!-- Continuar leyendo: novelas -->
      <section v-if="novels.length" class="home-section" aria-label="Continuar leyendo novelas">
        <div class="home-section-head">
          <h2 class="home-section-title">Novelas</h2>
          <RouterLink to="/library" class="home-section-more">
            Ver biblioteca
          </RouterLink>
        </div>
        <div class="home-grid">
          <NovelCard v-for="novel in novels" :key="novel.id" :novel="novel" />
        </div>
      </section>

      <!-- Continuar leyendo: cómics -->
      <section v-if="comics.length" class="home-section" aria-label="Continuar leyendo cómics">
        <div class="home-section-head">
          <h2 class="home-section-title">Cómics</h2>
          <RouterLink to="/library/comics" class="home-section-more">
            Ver biblioteca
          </RouterLink>
        </div>
        <div class="home-grid">
          <ComicCard v-for="comic in comics" :key="comic.id" :comic="comic" />
        </div>
      </section>

      <n-empty
        v-if="loaded && !novels.length && !comics.length"
        description="Aún no has empezado a leer. Importa tu primera novela o cómic desde la biblioteca."
        class="home-empty"
      >
        <template #extra>
          <n-button tag="a" href="/library" type="primary" secondary>
            Ir a la biblioteca
          </n-button>
        </template>
      </n-empty>
    </section>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, ref } from "vue";
import { RouterLink } from "vue-router";
import { NButton, NEmpty, NIcon } from "naive-ui";
import { FlashOutline } from "@vicons/ionicons5";
import AppLayout from "@/components/AppLayout.vue";
import NovelCard from "@/components/NovelCard.vue";
import ComicCard from "@/components/ComicCard.vue";
import { useAppServices } from "@/app/services";
import { useActiveJobStatus } from "@/composables/useActiveJobStatus";
import type { Novel, Comic } from "@/domain";

const { api } = useAppServices();
const { hasActive } = useActiveJobStatus();

const novels = ref<Novel[]>([]);
const comics = ref<Comic[]>([]);
const loaded = ref(false);

onMounted(async () => {
  try {
    const [novelPage, comicPage] = await Promise.all([
      api.novels.list({ sort: "lastRead", order: "desc", limit: 12 }),
      api.comics.list({ sort: "lastRead", order: "desc", limit: 12 }),
    ]);
    novels.value = novelPage.items.filter((n) => (n as { lastReadAt?: string }).lastReadAt);
    comics.value = comicPage.items.filter((c) => (c as { lastReadAt?: string }).lastReadAt);
  } finally {
    loaded.value = true;
  }
});
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
  flex-direction: column;
  gap: 0.25rem;
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

.home-section-more {
  font-size: 0.8125rem;
  color: var(--accent-link);
}

.home-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(132px, 1fr));
  gap: 1rem;
}

@media (max-width: 640px) {
  .home {
    padding-top: 1rem;
    gap: 1.25rem;
  }

  .home-grid {
    grid-template-columns: repeat(auto-fill, minmax(104px, 1fr));
    gap: 0.75rem;
  }
}
</style>
