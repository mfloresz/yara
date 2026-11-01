<template>
  <article class="library-card" role="listitem">
    <RouterLink
      :to="`/comics/${comic.id}`"
      class="library-cover-link"
      :aria-label="`Ver detalle de ${getComicDisplayTitle(comic)}`"
    >
      <div class="library-cover">
        <img
          :src="coverSrc(comic.coverPath)"
          :alt="`Portada de ${getComicDisplayTitle(comic)}`"
          loading="lazy"
          @error="onCoverError"
        />
        <span
          v-if="comic.chapterCount > 0 && comic.downloadedCount >= comic.chapterCount"
          class="comic-complete-badge"
        >
          Completo
        </span>
      </div>
    </RouterLink>

    <div class="library-meta">
      <RouterLink
        :to="`/comics/${comic.id}/read`"
        class="library-title line-clamp-2"
        :title="getComicDisplayTitle(comic)"
      >
        {{ getComicDisplayTitle(comic) }}
      </RouterLink>
      <div class="library-subtitle small muted">
        <span class="comic-progress">
          {{ comic.downloadedCount }}/{{ comic.chapterCount }} cap.
        </span>
      </div>
    </div>
  </article>
</template>

<script setup lang="ts">
import { RouterLink } from "vue-router";
import { getComicDisplayTitle, type Comic } from "@/domain";
import { coverSrc, onCoverError } from "@/utils/cover";

defineProps<{
  comic: Comic;
}>();
</script>

<style scoped>
.library-card {
  position: relative;
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.library-cover-link {
  display: block;
  border-radius: var(--radius-md);
}

.library-cover-link:focus-visible {
  outline-offset: 2px;
}

.library-cover {
  position: relative;
  width: 100%;
  aspect-ratio: 2 / 3;
  border-radius: var(--radius-md);
  overflow: hidden;
  border: 1px solid var(--divide);
  background: var(--surface-muted);
  box-shadow: 0 8px 20px rgba(0, 0, 0, 0.1);
  transition: transform 0.15s ease, box-shadow 0.15s ease;
}

.library-cover-link:hover .library-cover,
.library-cover-link:focus-visible .library-cover {
  transform: translateY(-3px);
  box-shadow: 0 14px 32px rgba(0, 0, 0, 0.14);
}

.library-cover img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.comic-complete-badge {
  position: absolute;
  top: 0.5rem;
  left: 0.5rem;
  padding: 0.2rem 0.45rem;
  border: 1px solid color-mix(in oklab, var(--success, #16a34a) 35%, var(--divide));
  border-radius: var(--radius-pill);
  background: color-mix(in oklab, var(--surface-elevated) 84%, var(--success, #16a34a));
  color: var(--success, #16a34a);
  font-size: 0.6875rem;
  line-height: 1.2;
  white-space: nowrap;
}

.library-meta {
  display: flex;
  flex-direction: column;
  gap: 0.125rem;
  padding: 0.375rem 0.125rem 0;
  min-width: 0;
}

.library-title {
  font-weight: 600;
  font-size: 0.8125rem;
  line-height: 1.3;
  color: var(--foreground);
  overflow: hidden;
  text-overflow: ellipsis;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
}

.library-title:hover {
  color: var(--accent-link);
}

.comic-progress {
  font-size: 0.6875rem;
  color: var(--text-secondary);
}

@media (max-width: 640px) {
  .library-title {
    font-size: 0.75rem;
  }

  .library-meta {
    padding: 0.25rem 0 0;
  }
}
</style>
