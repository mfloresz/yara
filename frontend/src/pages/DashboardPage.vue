<template>
  <AppLayout>
    <div class="stack-lg">
      <header class="page-header">
        <div>
          <p class="muted small">{{ novels.length }} novela{{ novels.length === 1 ? '' : 's' }}</p>
        </div>
        <div class="page-actions">
          <n-input
            v-model:value="searchQuery"
            placeholder="Buscar novela..."
            clearable
            class="search-input"
          >
            <template #prefix>
              <n-icon><SearchOutline /></n-icon>
            </template>
          </n-input>
          <div class="sort-controls">
            <n-select
              v-model:value="sortField"
              :options="sortOptions"
              class="sort-select"
              placeholder="Ordenar por"
              @update:value="onSortChange"
            />
            <n-button
              quaternary
              circle
              size="small"
              :aria-label="sortOrder === 'asc' ? 'Cambiar a orden descendente' : 'Cambiar a orden ascendente'"
              @click="toggleSortOrder"
            >
              <template #icon>
                <n-icon>
                   <ArrowUpOutline v-if="sortOrder === 'asc'" />
                   <ArrowDownOutline v-else />
                </n-icon>
              </template>
            </n-button>
          </div>
          <n-button
            :secondary="!groupBySeries"
            :type="groupBySeries ? 'primary' : 'default'"
            quaternary
            circle
            size="small"
            class="group-toggle"
            :aria-label="groupBySeries ? 'Desagrupar por serie' : 'Agrupar por serie'"
            @click="groupBySeries = !groupBySeries"
          >
            <template #icon>
              <n-icon>
                <PricetagsOutline v-if="groupBySeries" />
                <PricetagOutline v-else />
              </n-icon>
            </template>
          </n-button>
          <n-button class="page-action desktop-only" secondary @click="importOpen = true">
            <template #icon><n-icon><CloudUploadOutline /></n-icon></template>
            Importar EPUB
          </n-button>
          <n-button class="page-action desktop-only" secondary @click="importUrlOpen = true">
            <template #icon><n-icon><GlobeOutline /></n-icon></template>
            Desde URL
          </n-button>
          <n-button type="primary" class="page-action desktop-only" @click="createOpen = true">
            <template #icon><n-icon><AddOutline /></n-icon></template>
            Nueva novela
          </n-button>
        </div>
      </header>

      <div v-if="loading" class="library-grid" role="status" aria-label="Cargando biblioteca">
        <LibrarySkeleton />
      </div>

      <n-card v-else-if="sortedNovels.length === 0">
        <div class="empty-state">
          <div class="empty-state-icon">
            <n-icon :size="40"><BookOutline /></n-icon>
          </div>
          <div>
            <h2 class="empty-state-title">Sin novelas</h2>
            <p class="muted empty-state-body">Crea una novela manualmente, importa un EPUB o descarga uno desde internet.</p>
          </div>
          <div class="empty-state-actions">
            <n-button type="primary" @click="createOpen = true">
              <template #icon><n-icon><AddOutline /></n-icon></template>
              Nueva novela
            </n-button>
            <n-button secondary @click="importOpen = true">
              <template #icon><n-icon><CloudUploadOutline /></n-icon></template>
              Importar EPUB
            </n-button>
            <n-button secondary @click="importUrlOpen = true">
              <template #icon><n-icon><GlobeOutline /></n-icon></template>
              Desde URL
            </n-button>
          </div>
        </div>
      </n-card>

      <template v-if="!groupBySeries">
        <div class="library-grid" role="list">
          <div v-if="sorting" class="sorting-overlay" aria-hidden="true">
            <LibrarySkeleton />
          </div>
          <template v-else>
          <NovelCard
            v-for="novel in sortedNovels"
            :key="novel.id"
            :novel="novel"

          />
          </template>
        </div>
      </template>
      <template v-else>
        <section v-for="group in groupedNovels.groups" :key="group.key" class="series-group">
          <div class="series-header">
            <span class="series-name">{{ group.series }}</span>
            <span class="series-author small muted">{{ group.author }}</span>
          </div>
          <div class="library-grid" role="group" :aria-label="`${group.series} — ${group.author}`">
            <NovelCard
              v-for="novel in group.novels"
              :key="novel.id"
              :novel="novel"

            />
          </div>
        </section>
        <section v-if="groupedNovels.ungrouped.length" class="series-group">
          <div class="series-header">
            <span class="series-name series-ungrouped"><i>Sin serie</i></span>
          </div>
          <div class="library-grid" role="group" aria-label="Novelas sin serie">
            <NovelCard
              v-for="novel in groupedNovels.ungrouped"
              :key="novel.id"
              :novel="novel"

            />
          </div>
        </section>
      </template>
    </div>

    <n-dropdown
      trigger="click"
      :options="novelMenuDropdownItems"
      :disabled="!selectedNovel"
      @select="handleNovelMenuSelect"
    >
      <span ref="novelMenuAnchor" />
    </n-dropdown>

    <n-modal v-model:show="createOpen" preset="card" title="Nueva novela" style="width: min(620px, 96vw)">
      <div class="stack-md">
        <div class="row-wrap">
          <div style="flex: 1; min-width: 220px">
            <label class="small muted">Título</label>
            <n-input v-model:value="form.sourceTitle" />
          </div>
          <div style="flex: 1; min-width: 220px">
            <label class="small muted">Autor</label>
            <n-input v-model:value="form.sourceAuthor" />
          </div>
        </div>
        <div>
          <label class="small muted">Descripción</label>
          <n-input v-model:value="form.sourceDescription" type="textarea" :rows="4" />
        </div>
        <div class="row-wrap">
          <div style="flex: 1; min-width: 220px">
            <label class="small muted">Idioma origen</label>
            <n-select v-model:value="form.sourceLanguage" :options="languageOptions" placeholder="Selecciona idioma" />
          </div>
          <div style="flex: 1; min-width: 220px">
            <label class="small muted">Idioma destino</label>
            <n-select v-model:value="form.targetLanguage" :options="languageOptionsNoAuto" placeholder="Selecciona idioma" />
          </div>
        </div>
        <n-alert v-if="createError" type="error" :title="createError" />
      </div>
      <template #footer>
        <n-button secondary @click="createOpen = false">Cancelar</n-button>
        <n-button type="primary" :loading="creating" :disabled="!canCreate" @click="submitCreate">Crear</n-button>
      </template>
    </n-modal>

    <n-modal v-model:show="importOpen" preset="card" title="Importar novela desde EPUB" style="width: min(640px, 96vw)">
      <div class="stack-md">
        <input type="file" accept=".epub" @change="handleImportFile" />
        <n-alert v-if="importPreviewLoading" type="info" title="Analizando EPUB…" />

        <template v-if="importPreview">
          <n-card size="small">
            <div class="stack-md small">
              <div><strong>Título detectado:</strong> {{ importPreview.title }}</div>
              <div v-if="importPreview.author"><strong>Autor detectado:</strong> {{ importPreview.author }}</div>
              <div><strong>Capítulos encontrados:</strong> {{ importPreview.chapterCount }}</div>
            </div>
          </n-card>

          <div class="row-wrap">
            <div style="flex: 1; min-width: 220px">
              <label class="small muted">Idioma origen</label>
              <n-select v-model:value="importSourceLang" :options="languageOptions" placeholder="Automático" />
            </div>
            <div style="flex: 1; min-width: 220px">
              <label class="small muted">Idioma destino</label>
              <n-select v-model:value="importTargetLang" :options="languageOptionsNoAuto" placeholder="Requerido" />
            </div>
          </div>
        </template>

        <n-alert v-if="importError" type="error" :title="importError" />
      </div>
      <template #footer>
        <n-button secondary @click="resetImport">Cancelar</n-button>
        <n-button type="primary" :loading="importing" :disabled="!importFile || !importTargetLang" @click="submitImport">Importar</n-button>
      </template>
    </n-modal>

    <ImportUrlDialog
      :open="importUrlOpen"
      @update:open="importUrlOpen = $event"
      @preview="onUrlPreviewed"
    />

    <ImportUrlConfirmDialog
      :open="importUrlConfirmOpen"
      :preview="urlPreview"
      @update:open="importUrlConfirmOpen = $event"
      @back="onBackToUrlDialog"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, h } from "vue";
import { useRouter } from "vue-router";
import {
  NSelect,
  NButton,
  NCard,
  NModal,
  NInput,
  NAlert,
  NIcon,
  NDropdown,
  useMessage,
} from "naive-ui";
import {
  ArrowUpOutline,
  ArrowDownOutline,
  PricetagOutline,
  PricetagsOutline,
  CloudUploadOutline,
  GlobeOutline,
  AddOutline,
  BookOutline,
  CreateOutline,
  TrashOutline,
  CopyOutline,
  SearchOutline,
} from "@vicons/ionicons5";
import AppLayout from "@/components/AppLayout.vue";
import NovelCard from "@/components/NovelCard.vue";
import LibrarySkeleton from "@/components/LibrarySkeleton.vue";
import { useNovels } from "@/composables/useNovels";
import { LANGUAGES } from "@/config/languages";
import { getNovelDisplayTitle, getNovelDisplayAuthor, getNovelDisplaySeries, getNovelDisplayNumber, type Novel } from "@/domain";
import { useAppServices } from "@/app/services";
import ImportUrlDialog from "@/features/novels/ImportUrlDialog.vue";
import ImportUrlConfirmDialog from "@/features/novels/ImportUrlConfirmDialog.vue";
import type { PreviewUrlResult } from "@/api/types";

type SortField = "title" | "created" | "lastRead";

const sortOptions = [
  { label: "Título", value: "title" },
  { label: "Fecha Adición", value: "created" },
  { label: "Fecha Lectura", value: "lastRead" },
];

const sortField = ref<SortField>("title");
const sortOrder = ref<"asc" | "desc">("asc");
const sorting = ref(false);
const searchQuery = ref("");
let sortTimeout: ReturnType<typeof setTimeout> | null = null;

function toggleSortOrder() {
  sortOrder.value = sortOrder.value === "asc" ? "desc" : "asc";
}

function onSortChange() {
  sorting.value = true;
  if (sortTimeout) clearTimeout(sortTimeout);
  sortTimeout = setTimeout(() => {
    sorting.value = false;
  }, 300);
}

const sortedNovels = computed(() => {
  const list = [...novels.value];

  // Filter by search query
  if (searchQuery.value.trim()) {
    const query = searchQuery.value.toLowerCase().trim();
    const filtered = list.filter((novel) => {
      const title = getNovelDisplayTitle(novel).toLowerCase();
      const author = getNovelDisplayAuthor(novel).toLowerCase();
      return title.includes(query) || author.includes(query);
    });
    return sortNovels(filtered);
  }

  return sortNovels(list);
});

function sortNovels(list: Novel[]): Novel[] {
  const dir = sortOrder.value === "asc" ? 1 : -1;
  switch (sortField.value) {
    case "title":
      return [...list].sort((a, b) => dir * getNovelDisplayTitle(a).localeCompare(getNovelDisplayTitle(b)));
    case "created":
      return [...list].sort(
        (a, b) => dir * (new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime()),
      );
    case "lastRead":
      return [...list].sort((a, b) => {
        const aTime = a.lastReadAt;
        const bTime = b.lastReadAt;
        if (!aTime && !bTime) return 0;
        if (!aTime) return 1;
        if (!bTime) return -1;
        return dir * (new Date(bTime).getTime() - new Date(aTime).getTime());
      });
    default:
      return list;
  }
}

type NovelGroup = {
  key: string;
  author: string;
  series: string;
  novels: Novel[];
};

type GroupedResult = {
  groups: NovelGroup[];
  ungrouped: Novel[];
};

const groupBySeries = ref(false);

const groupedNovels = computed((): GroupedResult => {
  const groups = new Map<string, NovelGroup>();
  const ungrouped: Novel[] = [];

  for (const novel of sortedNovels.value) {
    const series = getNovelDisplaySeries(novel);
    const author = getNovelDisplayAuthor(novel);
    if (series) {
      const key = `${author}|${series}`;
      if (!groups.has(key)) {
        groups.set(key, { key, author, series, novels: [] });
      }
      groups.get(key)!.novels.push(novel);
    } else {
      ungrouped.push(novel);
    }
  }

  for (const group of groups.values()) {
    group.novels.sort((a, b) => {
      const numA = parseFloat(getNovelDisplayNumber(a));
      const numB = parseFloat(getNovelDisplayNumber(b));
      if (!isNaN(numA) && !isNaN(numB)) return numA - numB;
      if (!isNaN(numA)) return -1;
      if (!isNaN(numB)) return 1;
      return getNovelDisplayTitle(a).localeCompare(getNovelDisplayTitle(b));
    });
  }

  const sortedGroups = [...groups.values()].sort((a, b) => {
    const cmp = a.author.localeCompare(b.author);
    if (cmp !== 0) return cmp;
    return a.series.localeCompare(b.series);
  });

  return { groups: sortedGroups, ungrouped };
});

const router = useRouter();
const message = useMessage();
const { api, auth } = useAppServices();
const { novels, loading, listNovels, createNovel, importNovelFromEpub, deleteNovel } = useNovels();

const createOpen = ref(false);
const creating = ref(false);
const createError = ref<string | null>(null);
const importOpen = ref(false);
const importUrlOpen = ref(false);
const importUrlConfirmOpen = ref(false);
const urlPreview = ref<PreviewUrlResult | null>(null);
const importing = ref(false);
const importPreviewLoading = ref(false);
const importError = ref<string | null>(null);
const importFile = ref<File | null>(null);
const importTargetLang = ref<string | null>(null);
const importSourceLang = ref<string | null>(null);
const importPreview = ref<{ title: string; author: string; description: string; language: string; chapterCount: number } | null>(null);
const novelMenuAnchor = ref<HTMLElement | null>(null);
const selectedNovel = ref<Novel | null>(null);

const form = reactive({
  sourceTitle: "",
  sourceAuthor: "",
  sourceDescription: "",
  sourceLanguage: null as string | null,
  targetLanguage: null as string | null,
});

const languageOptions = LANGUAGES.map((l) => ({ label: l.name, value: l.code }));
const languageOptionsNoAuto = LANGUAGES.filter((l) => l.code !== "auto").map((l) => ({ label: l.name, value: l.code }));
const canCreate = computed(() => Boolean(form.sourceTitle.trim() && form.sourceLanguage && form.targetLanguage));

const novelMenuDropdownItems = computed(() => {
  const novel = selectedNovel.value;
  if (!novel) return [];
  const isOwner = novel.ownerId === auth.user.value?.id;
  const items: Array<{ label: string; key: string; icon?: () => any }> = [
    { label: "Leer", key: "read" },
  ];
  if (isOwner) {
    items.push(
      { label: "Editar", key: "edit" },
      { label: "Eliminar", key: "delete" },
    );
  } else {
    items.push({ label: "Copiar a mi biblioteca", key: "copy" });
  }
  return items;
});

function handleNovelMenuSelect(key: string) {
  const novel = selectedNovel.value;
  if (!novel) return;
  if (key === "read") router.push(`/novels/${novel.id}/read`);
  else if (key === "edit") router.push(`/novels/${novel.id}`);
  else if (key === "delete") askDeleteNovel(novel);
  else if (key === "copy") copyNovel(novel.id);
}

onMounted(() => {
  void listNovels(false, ["id", "sourceTitle", "targetTitle", "sourceAuthor", "targetAuthor", "sourceSeries", "targetSeries", "sourceNumber", "targetNumber", "coverPath", "ownerId", "lastReadAt", "createdAt"]);
});

function resetCreateForm() {
  form.sourceTitle = "";
  form.sourceAuthor = "";
  form.sourceDescription = "";
  form.sourceLanguage = null;
  form.targetLanguage = null;
  createError.value = null;
}

async function submitCreate() {
  if (!canCreate.value) return;
  creating.value = true;
  createError.value = null;
  try {
    const novel = await createNovel({
      sourceTitle: form.sourceTitle,
      sourceAuthor: form.sourceAuthor || undefined,
      sourceDescription: form.sourceDescription || undefined,
      sourceLanguage: form.sourceLanguage!,
      targetLanguage: form.targetLanguage!,
    });
    createOpen.value = false;
    resetCreateForm();
    await router.push(`/novels/${novel.id}`);
  } catch (err) {
    createError.value = err instanceof Error ? err.message : String(err);
  } finally {
    creating.value = false;
  }
}

async function handleImportFile(event: Event) {
  const file = (event.target as HTMLInputElement).files?.[0];
  if (!file) return;
  importError.value = null;
  importFile.value = file;
  importPreviewLoading.value = true;
  try {
    const data = await api.epubs.preview(file, file.name);
    importPreview.value = {
      title: data.title || file.name.replace(/\.epub$/i, ""),
      author: data.author || "",
      description: data.description || "",
      language: data.language || "",
      chapterCount: data.chapters?.length || 0,
    };
    importSourceLang.value = data.language || "";
  } catch (err) {
    importError.value = err instanceof Error ? err.message : String(err);
    importPreview.value = null;
  } finally {
    importPreviewLoading.value = false;
  }
}

function resetImport() {
  importOpen.value = false;
  importing.value = false;
  importPreviewLoading.value = false;
  importError.value = null;
  importPreview.value = null;
  importFile.value = null;
  importTargetLang.value = null;
  importSourceLang.value = null;
}

async function submitImport() {
  if (!importFile.value || !importTargetLang.value) return;
  importing.value = true;
  importError.value = null;
  try {
    const result = await importNovelFromEpub({
      file: importFile.value,
      fileName: importFile.value.name,
      sourceLanguage: importSourceLang.value || undefined,
      targetLanguage: importTargetLang.value,
    });
    resetImport();
    await router.push(`/novels/${result.novel.id}`);
  } catch (err) {
    importError.value = err instanceof Error ? err.message : String(err);
  } finally {
    importing.value = false;
  }
}

async function copyNovel(novelId: string) {
  try {
    await api.novels.copy(novelId);
    await listNovels(true, ["id", "sourceTitle", "targetTitle", "sourceAuthor", "targetAuthor", "sourceSeries", "targetSeries", "sourceNumber", "targetNumber", "coverPath", "ownerId", "lastReadAt", "createdAt"]);
    message.success("Novela copiada a tu biblioteca");
  } catch (err) {
    message.error("Error al copiar: " + (err instanceof Error ? err.message : String(err)));
  }
}

function openNovelMenu(event: Event, novel: Novel) {
  selectedNovel.value = novel;
}

function askDeleteNovel(novel: Novel) {
  selectedNovel.value = novel;
}

onMounted(() => {
  // Handle delete via selectedNovel watcher if needed
});

async function deleteNovelAction(novel: Novel) {
  try {
    await deleteNovel(novel.id);
    message.success("Novela eliminada");
  } catch (err) {
    message.error("Error al eliminar: " + (err instanceof Error ? err.message : String(err)));
  }
}

function onUrlPreviewed(preview: PreviewUrlResult) {
  urlPreview.value = preview;
  importUrlOpen.value = false;
  importUrlConfirmOpen.value = true;
}

function onBackToUrlDialog() {
  importUrlConfirmOpen.value = false;
  urlPreview.value = null;
  importUrlOpen.value = true;
}
</script>

<style scoped>
.page-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 1rem;
  flex-wrap: wrap;
}

.page-title {
  margin: 0;
  font-size: 1.75rem;
  font-weight: 700;
  letter-spacing: -0.02em;
}

.page-actions {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  flex-wrap: wrap;
}

.page-action {
  white-space: nowrap;
}

.sort-controls {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}

.sort-select {
  min-width: 8rem;
}

.search-input {
  min-width: 12rem;
  max-width: 16rem;
}

.mobile-only {
  display: none;
}

.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
  gap: 1rem;
  padding: 2.5rem 1rem;
}

.empty-state-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 3.5rem;
  height: 3.5rem;
  border-radius: var(--radius-lg);
  background: var(--surface-muted);
  color: var(--text-secondary);
}

.empty-state-title {
  margin: 0 0 0.25rem;
  font-size: 1.25rem;
}

.empty-state-body {
  margin: 0;
  max-width: 48ch;
}

.empty-state-actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: center;
  gap: 0.75rem;
}

.library-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
  gap: 1.5rem;
}

.series-group {
  margin-bottom: 2rem;
}

.series-header {
  display: flex;
  align-items: baseline;
  gap: 0.75rem;
  margin-bottom: 1rem;
  padding-bottom: 0.5rem;
  border-bottom: 1px solid var(--divide);
}

.series-name {
  font-weight: 700;
  font-size: 1.125rem;
}

.series-author {
  color: var(--text-tertiary);
}

.series-ungrouped {
  color: var(--text-tertiary);
  font-style: italic;
}

.sorting-overlay {
  display: contents;
}

@media (max-width: 640px) {
  .desktop-only {
    display: none;
  }

  .mobile-only {
    display: inline-flex;
  }

  .page-title {
    font-size: 1.5rem;
  }

  .library-grid {
    grid-template-columns: repeat(3, 1fr);
    gap: 1rem;
  }
}

@media (max-width: 380px) {
  .library-grid {
    grid-template-columns: repeat(2, 1fr);
  }
}
</style>
