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
      <div class="library-meta-row">
        <RouterLink
          :to="`/comics/${comic.id}/read`"
          class="library-title line-clamp-2"
          :title="getComicDisplayTitle(comic)"
        >
          {{ getComicDisplayTitle(comic) }}
        </RouterLink>
        <n-dropdown
          trigger="click"
          placement="bottom-end"
          :options="menuOptions"
          @select="handleMenuSelect"
        >
          <n-button
            class="card-menu-button"
            quaternary
            circle
            size="tiny"
            :aria-label="`Acciones de lectura de ${getComicDisplayTitle(comic)}`"
            aria-haspopup="menu"
          >
            <template #icon><n-icon :size="14"><OptionsOutline /></n-icon></template>
          </n-button>
        </n-dropdown>
      </div>
      <div class="library-subtitle small muted">
        <span class="comic-progress">
          {{ comic.downloadedCount }}/{{ comic.chapterCount }} cap.
        </span>
        <span v-if="statusBadge" class="reading-status-badge">{{ statusBadge }}</span>
      </div>
    </div>
  </article>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { RouterLink } from "vue-router";
import { NButton, NDropdown, NIcon, useDialog, useMessage } from "naive-ui";
import { OptionsOutline } from "@vicons/ionicons5";
import { getComicDisplayTitle, type Comic, type ReadingStatus } from "@/domain";
import { coverSrc, onCoverError } from "@/utils/cover";
import {
  readingStatusBadgeLabel,
  readingStatusMenuOptions,
  useReadingStatus,
} from "@/composables/useReadingStatus";

const props = defineProps<{
  comic: Comic;
}>();

const emit = defineEmits<{
  (e: "cleared"): void;
}>();

const { setStatus, clearProgress } = useReadingStatus();
const message = useMessage();
const dialog = useDialog();

const menuOptions = computed(() => readingStatusMenuOptions("comic", props.comic));
const statusBadge = computed(() => readingStatusBadgeLabel("comic", props.comic.readingStatus));

function handleMenuSelect(key: string) {
  const comicId = props.comic.id;
  if (key === "clear") {
    dialog.warning({
      title: "Quitar progreso",
      content: `¿Quitar el progreso de lectura de "${getComicDisplayTitle(props.comic)}"? Se olvidará tu posición y sus marcas.`,
      positiveText: "Quitar",
      negativeText: "Cancelar",
      onPositiveClick: async () => {
        try {
          await clearProgress("comic", comicId);
          props.comic.readingStatus = undefined;
          props.comic.lastReadAt = undefined;
          emit("cleared");
        } catch {
          message.error("No se pudo quitar el progreso.");
        }
      },
    });
    return;
  }
  const status = key as ReadingStatus;
  void setStatus("comic", comicId, status)
    .then(() => {
      props.comic.readingStatus = status;
    })
    .catch(() => {
      message.error("No se pudo actualizar el estado de lectura.");
    });
}
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

.library-meta-row {
  display: flex;
  align-items: flex-start;
  gap: 0.125rem;
  min-width: 0;
}

.card-menu-button {
  flex-shrink: 0;
  color: var(--text-secondary);
}

.library-title {
  flex: 1;
  min-width: 0;
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

.library-subtitle {
  display: flex;
  align-items: center;
  gap: 0.25rem;
  min-width: 0;
}

.reading-status-badge {
  display: inline-block;
  padding: 0.0625rem 0.375rem;
  border-radius: var(--radius-sm);
  border: 1px solid var(--divide);
  background: var(--surface-elevated);
  font-size: 0.6875rem;
  color: var(--text-secondary);
  white-space: nowrap;
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
