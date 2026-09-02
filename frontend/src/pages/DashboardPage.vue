<template>
  <AppLayout>
    <div class="stack-lg">
      <header class="page-header">
        <div class="page-context">
          <h1 class="page-title">Biblioteca</h1>
          <p class="muted small" aria-live="polite">
            {{ novels.length }} novela{{ novels.length === 1 ? '' : 's' }}
            <span v-if="searchQuery.trim() && loadingMore" class="search-hint">&nbsp;· buscando…</span>
            <span v-else-if="searchQuery.trim()" class="search-hint">&nbsp;· filtrado</span>
          </p>
        </div>
        <div class="page-actions">
          <n-input
            v-model:value="searchQuery"
            placeholder="Buscar novela..."
            clearable
            class="search-input"
            :class="{ 'is-loading': searchQuery.trim() && loadingMore }"
            :input-props="{ type: 'search', inputmode: 'search', enterkeyhint: 'search' }"
            aria-label="Buscar novelas"
          >
            <template #prefix><n-icon><SearchOutline /></n-icon></template>
          </n-input>
          <n-dropdown
            trigger="click"
            :options="viewMenuOptions"
            class="view-menu"
            @select="handleViewMenuSelect"
          >
            <n-button secondary size="small" class="view-menu-button" aria-label="Opciones de vista">
              <template #icon><n-icon><OptionsOutline /></n-icon></template>
              Ver como
            </n-button>
          </n-dropdown>
          <n-dropdown trigger="click" :options="novelCreationOptions" @select="handleNovelCreationSelect">
            <n-button type="primary" class="create-menu-button">
              <template #icon><n-icon><AddOutline /></n-icon></template>
              Nueva novela
            </n-button>
          </n-dropdown>
        </div>
      </header>

      <div v-if="activeTagFilter" class="active-filters">
        <n-tag type="info" closable @close="clearTagFilter">
          Tag: {{ activeTagFilter }}
        </n-tag>
      </div>

      <div v-if="loading" class="library-grid" role="status" aria-label="Cargando biblioteca">
        <LibrarySkeleton />
      </div>

      <n-card v-else-if="sortedNovels.length === 0" role="status">
        <div v-if="searchQuery.trim()" class="empty-state">
          <div class="empty-state-icon">
            <n-icon :size="40"><SearchOutline /></n-icon>
          </div>
          <div>
            <h2 class="empty-state-title">Sin coincidencias</h2>
            <p class="muted empty-state-body">
              No se encontraron novelas para «{{ searchQuery.trim() }}». Prueba con otro término o importa una nueva.
            </p>
          </div>
          <div class="empty-state-actions">
            <n-button secondary @click="clearSearch">
              <template #icon><n-icon><CloseOutline /></n-icon></template>
              Limpiar búsqueda
            </n-button>
            <n-button type="primary" @click="createOpen = true">
              <template #icon><n-icon><AddOutline /></n-icon></template>
              Nueva novela
            </n-button>
          </div>
        </div>
        <div v-else class="empty-state">
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
          <NovelCard
            v-for="novel in sortedNovels"
            :key="novel.id"
            :novel="novel"
            :shared="isSharedNovel(novel)"
          />
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

      <div v-if="hasMore && !searchQuery.trim()" class="load-more-container">
        <n-button
          :loading="loadingMore"
          secondary
          @click="loadMoreNovels"
        >
          Cargar más novelas
        </n-button>
      </div>
    </div>

    <n-dropdown
      trigger="click"
      :options="novelCreationOptions"
      @select="handleNovelCreationSelect"
      placement="top-end"
    >
      <button
        type="button"
        class="create-fab"
        aria-label="Nueva novela"
      >
        <n-icon :size="22"><AddOutline /></n-icon>
      </button>
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

    <n-modal v-model:show="importZipOpen" preset="card" title="Importar novela desde ZIP" style="width: min(720px, 96vw)">
      <div class="stack-md">
        <n-alert type="info" title="Estructura requerida">
          El ZIP debe contener un archivo <code>metadata.json</code> y la carpeta <code>originals/</code>.
          Los idiomas y los datos de la novela se leen desde el metadata; no hace falta introducirlos aquí.
        </n-alert>
        <n-card size="small">
          <div class="stack-sm small">
            <strong>Ejemplo de estructura</strong>
            <pre class="zip-structure">novela.zip
├── metadata.json
├── cover.jpg              (opcional)
├── originals/
│   ├── 001-capitulo-1.html
│   └── 002-capitulo-2.html
└── translated/            (opcional)
    ├── 001-capitulo-1.html
    └── 002-capitulo-2.html</pre>
            <div class="muted">El número del nombre determina el orden. Los archivos de <code>translated/</code> deben coincidir con los de <code>originals/</code>.</div>
          </div>
        </n-card>
        <div class="small">
          <strong>metadata.json mínimo</strong>
          <pre class="zip-structure">{
  "sourceTitle": "Título original",
  "sourceLanguage": "en",
  "targetLanguage": "es"
}</pre>
          <div class="muted">También puedes incluir autor, descripción, serie, número, URL y títulos traducidos.</div>
        </div>
        <input type="file" accept=".zip,application/zip" @change="handleImportZipFile" />
        <div v-if="importZipFile" class="small muted">Archivo seleccionado: {{ importZipFile.name }}</div>
        <n-alert v-if="importZipError" type="error" :title="importZipError" />
      </div>
      <template #footer>
        <n-button secondary @click="resetImportZip">Cancelar</n-button>
        <n-button type="primary" :loading="importingZip" :disabled="!importZipFile" @click="submitImportZip">Importar ZIP</n-button>
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
import { computed, onMounted, reactive, ref, h, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import {
  NSelect,
  NButton,
  NCard,
  NModal,
  NInput,
  NAlert,
  NIcon,
  NDropdown,
  NTag,
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
  SearchOutline,
  ArchiveOutline,
  CloseOutline,
  OptionsOutline,
} from "@vicons/ionicons5";
import AppLayout from "@/components/AppLayout.vue";
import NovelCard from "@/components/NovelCard.vue";
import LibrarySkeleton from "@/pages/LibrarySkeleton.vue";
import { useNovels, type NovelListFilters } from "@/composables/useNovels";
import { useOfflineCache } from "@/composables/useOfflineCache";
import { LANGUAGES } from "@/config/languages";
import { getNovelDisplayTitle, getNovelDisplayAuthor, getNovelDisplaySeries, getNovelDisplayNumber, type Novel } from "@/domain";
import { useAppServices } from "@/app/services";
import { dashboardPrefsKey } from "@/app/storage-keys";
import ImportUrlDialog from "@/features/novels/ImportUrlDialog.vue";
import ImportUrlConfirmDialog from "@/features/novels/ImportUrlConfirmDialog.vue";
import type { PreviewUrlResult } from "@/api/types";

type SortField = "title" | "created" | "lastRead";
type ProgressFilter = "all" | "translated" | "completed" | "ongoing";

const sortOptions = [
  { label: "Título", value: "title" },
  { label: "Fecha Adición", value: "created" },
  { label: "Fecha Lectura", value: "lastRead" },
];

const progressOptions = [
  { label: "Todas (progreso)", value: "all" },
  { label: "Completamente traducidas", value: "translated" },
  { label: "Completadas", value: "completed" },
  { label: "En curso", value: "ongoing" },
];

function isProgressFilter(value: string | undefined): value is ProgressFilter {
  return value !== undefined && progressOptions.some((option) => option.value === value);
}

const sortField = ref<SortField>("title");
const sortOrder = ref<"asc" | "desc">("asc");
const groupBySeries = ref(false);
const searchQuery = ref("");
const preferenceKey = ref<string | null>(null);

// Library filters. showShared/progressFilter persist per user; activeTagFilter
// lives only in the URL (it comes from an external click on a tag).
const showShared = ref(true);
const progressFilter = ref<ProgressFilter>("all");
const activeTagFilter = ref<string | null>(null);

const route = useRoute();

// Effective filters sent to the backend on every list call.
function currentFilters(): NovelListFilters {
  return {
    shared: showShared.value ? "all" : "own",
    progress: progressFilter.value,
    tag: activeTagFilter.value,
  };
}

function applyFiltersFromRoute() {
  activeTagFilter.value = route.query.tag?.toString() ?? null;
  const urlShared = route.query.shared?.toString();
  if (urlShared === "own") showShared.value = false;
  else if (urlShared === "all") showShared.value = true;
  const urlProgress = route.query.progress?.toString();
  if (isProgressFilter(urlProgress)) progressFilter.value = urlProgress;
}

// Reflect showShared/progressFilter in the URL so the full filter state is
// shareable and survives back/forward navigation.
function syncFiltersToUrl() {
  const query: Record<string, string> = {};
  const tag = route.query.tag?.toString();
  if (tag) query.tag = tag;
  if (!showShared.value) query.shared = "own";
  if (progressFilter.value !== "all") query.progress = progressFilter.value;
  void router.replace({ path: "/", query });
}

function restorePreferences(userId?: string) {
  if (!userId) return;
  preferenceKey.value = dashboardPrefsKey(userId);
  try {
    const raw = localStorage.getItem(preferenceKey.value);
    if (!raw) return;
    const saved = JSON.parse(raw) as Partial<{
      sortField: SortField;
      sortOrder: "asc" | "desc";
      groupBySeries: boolean;
      showShared: boolean;
      progressFilter: ProgressFilter;
    }>;
    if (saved.sortField && sortOptions.some((option) => option.value === saved.sortField)) sortField.value = saved.sortField;
    if (saved.sortOrder === "asc" || saved.sortOrder === "desc") sortOrder.value = saved.sortOrder;
    if (typeof saved.groupBySeries === "boolean") groupBySeries.value = saved.groupBySeries;
    // URL wins over localStorage for the filters it carries.
    if (typeof saved.showShared === "boolean" && route.query.shared === undefined) showShared.value = saved.showShared;
    if (isProgressFilter(saved.progressFilter) && route.query.progress === undefined) progressFilter.value = saved.progressFilter;
  } catch {
    // Ignore invalid preferences and use defaults.
  }
}

function savePreferences() {
  if (!preferenceKey.value) return;
  localStorage.setItem(preferenceKey.value, JSON.stringify({
    sortField: sortField.value,
    sortOrder: sortOrder.value,
    groupBySeries: groupBySeries.value,
    showShared: showShared.value,
    progressFilter: progressFilter.value,
  }));
}
let searchTimeout: ReturnType<typeof setTimeout> | null = null;

// Watch searchQuery and debounce backend search
watch(searchQuery, (newQuery) => {
  if (searchTimeout) clearTimeout(searchTimeout);
  if (newQuery.trim()) {
    searchTimeout = setTimeout(() => {
      void searchNovels(newQuery.trim());
    }, 300);
  }
});

const LIST_SELECT = [
  "id",
  "sourceTitle",
  "targetTitle",
  "sourceAuthor",
  "targetAuthor",
  "sourceSeries",
  "targetSeries",
  "sourceNumber",
  "targetNumber",
  "coverPath",
  "ownerId",
  "isPublic",
  "lastReadAt",
  "createdAt",
  "tags",
  "status",
  "chapterCount",
  "translatedCount",
];

// The library is pre-sorted by the backend; changing the sort reloads page 0 so
// pagination stays consistent with the requested ordering.
async function reloadLibrary() {
  await listNovels(true, LIST_SELECT, sortField.value, sortOrder.value, currentFilters());
}

function toggleSortOrder() {
  sortOrder.value = sortOrder.value === "asc" ? "desc" : "asc";
  savePreferences();
  void reloadLibrary();
}

function onSortChange() {
  savePreferences();
  void reloadLibrary();
}

function toggleGroupBySeries() {
  groupBySeries.value = !groupBySeries.value;
  savePreferences();
}

function clearSearch() {
  searchQuery.value = "";
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
const message = useMessage();const { api, auth } = useAppServices();
const {
  novels,
  loading,
  hasMore,
  loadingMore,
  listNovels,
  loadMoreNovels,
  searchNovels,
  createNovel,
  importNovelFromEpub,
  importNovelFromZip,
  hydrateCachedNovels,
} = useNovels();
const offlineCache = useOfflineCache(ref(""));

const createOpen = ref(false);
const creating = ref(false);
const createError = ref<string | null>(null);
const importOpen = ref(false);
const importZipOpen = ref(false);
const importUrlOpen = ref(false);
const importUrlConfirmOpen = ref(false);
const urlPreview = ref<PreviewUrlResult | null>(null);
const importing = ref(false);
const importPreviewLoading = ref(false);
const importError = ref<string | null>(null);
const importFile = ref<File | null>(null);
const importTargetLang = ref<string | null>(null);
const importSourceLang = ref<string | null>(null);
const importingZip = ref(false);
const importZipError = ref<string | null>(null);
const importZipFile = ref<File | null>(null);
const importPreview = ref<{ title: string; author: string; description: string; language: string; chapterCount: number } | null>(null);

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

const novelCreationOptions = [
  { label: "Nueva novela", key: "create", icon: () => h(NIcon, null, { default: () => h(AddOutline) }) },
  { label: "Importar EPUB", key: "import-epub", icon: () => h(NIcon, null, { default: () => h(CloudUploadOutline) }) },
  { label: "Importar ZIP", key: "import-zip", icon: () => h(NIcon, null, { default: () => h(ArchiveOutline) }) },
  { label: "Desde URL", key: "import-url", icon: () => h(NIcon, null, { default: () => h(GlobeOutline) }) },
];

const viewMenuOptions = computed(() => [
  { type: "group", label: "Ordenar por", key: "sort-group", children: sortOptions.map((option) => ({
    label: `${option.label}${option.value === sortField.value ? "  ✓" : ""}`,
    key: `sort:${option.value}`,
  })) },
  { type: "divider", key: "d1" },
  {
    label: sortOrder.value === "asc" ? "Ascendente" : "Descendente",
    key: "toggle-order",
    icon: () => h(NIcon, null, { default: () => sortOrder.value === "asc" ? h(ArrowUpOutline) : h(ArrowDownOutline) }),
  },
  {
    label: `${groupBySeries.value ? "Agrupado por serie" : "Agrupar por serie"}${groupBySeries.value ? "  ✓" : ""}`,
    key: "toggle-group",
    icon: () => h(NIcon, null, { default: () => h(groupBySeries.value ? PricetagsOutline : PricetagOutline) }),
  },
  { type: "divider", key: "d2" },
  {
    label: `Solo propias${showShared.value ? "" : "  ✓"}`,
    key: "toggle-own",
  },
  { type: "divider", key: "d3" },
  ...progressOptions.map((option) => ({
    label: `${option.label}${progressFilter.value === option.value ? "  ✓" : ""}`,
    key: `progress:${option.value}`,
  })),
]);

function handleViewMenuSelect(key: string) {
  if (typeof key === "string" && key.startsWith("sort:")) {
    const value = key.slice("sort:".length) as SortField;
    if (value !== sortField.value) {
      sortField.value = value;
      onSortChange();
    }
    return;
  }
  if (typeof key === "string" && key.startsWith("progress:")) {
    const value = key.slice("progress:".length) as ProgressFilter;
    if (value !== progressFilter.value) {
      progressFilter.value = value;
      savePreferences();
      syncFiltersToUrl();
      void reloadLibrary();
    }
    return;
  }
  if (key === "toggle-order") toggleSortOrder();
  else if (key === "toggle-group") toggleGroupBySeries();
  else if (key === "toggle-own") {
    showShared.value = !showShared.value;
    savePreferences();
    syncFiltersToUrl();
    void reloadLibrary();
  }
}

function clearTagFilter() {
  const query = { ...route.query };
  delete query.tag;
  void router.replace({ path: "/", query });
}

function handleNovelCreationSelect(key: string) {
  if (key === "create") createOpen.value = true;
  else if (key === "import-epub") importOpen.value = true;
  else if (key === "import-zip") importZipOpen.value = true;
  else if (key === "import-url") importUrlOpen.value = true;
}

function isSharedNovel(novel: Novel) {
  return novel.ownerId !== auth.user.value?.id;
}

onMounted(() => {
  restorePreferences(auth.user.value?.id);
  applyFiltersFromRoute();
  void loadLibrary();
});

// Back/forward navigation: re-apply the filter state encoded in the URL and
// reload (the composable's signature cache skips the fetch when nothing changed).
watch(
  () => [route.query.tag, route.query.shared, route.query.progress],
  () => {
    applyFiltersFromRoute();
    void loadLibrary();
  },
);

async function loadLibrary() {
  try {
    await listNovels(false, LIST_SELECT, sortField.value, sortOrder.value, currentFilters());
  } catch {
    const cached = await offlineCache.loadCachedNovels();
    hydrateCachedNovels(Object.values(cached).map((item) => item.novel));
  }
}

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

function handleImportZipFile(event: Event) {
  const file = (event.target as HTMLInputElement).files?.[0];
  if (!file) return;
  importZipError.value = null;
  importZipFile.value = file;
}

function resetImportZip() {
  importZipOpen.value = false;
  importingZip.value = false;
  importZipError.value = null;
  importZipFile.value = null;
}

async function submitImportZip() {
  if (!importZipFile.value) return;
  importingZip.value = true;
  importZipError.value = null;
  try {
    const result = await importNovelFromZip(importZipFile.value);
    resetImportZip();
    await router.push(`/novels/${result.novel.id}`);
  } catch (err) {
    importZipError.value = err instanceof Error ? err.message : String(err);
  } finally {
    importingZip.value = false;
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
    await listNovels(true, LIST_SELECT, sortField.value, sortOrder.value, currentFilters());
    message.success("Novela copiada a tu biblioteca");
  } catch (err) {
    message.error("Error al copiar: " + (err instanceof Error ? err.message : String(err)));
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
.zip-structure {
  margin: 0;
  white-space: pre-wrap;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  line-height: 1.5;
  overflow-x: auto;
}

.page-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 1rem clamp(1rem, 3vw, 2rem);
  flex-wrap: wrap;
}

.page-context {
  flex: 0 1 auto;
  min-width: 7rem;
  padding-top: 0.6rem;
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
  justify-content: flex-end;
  gap: 0.5rem;
  flex: 1 1 42rem;
  width: 100%;
  min-width: 0;
  flex-wrap: wrap;
  box-sizing: border-box;
}

.create-menu-button {
  white-space: nowrap;
}

.search-input {
  width: clamp(12rem, 22vw, 17rem);
  transition: border-color 0.15s ease;
}

.search-input.is-loading :deep(.n-input__border),
.search-input.is-loading :deep(.n-input__state-border) {
  border-color: var(--accent-link);
}

.search-hint {
  color: var(--text-tertiary);
  font-style: italic;
}

.view-menu-button {
  white-space: nowrap;
  flex: 0 0 auto;
}

.create-fab {
  display: none;
}

@media (max-width: 640px) {
  .create-fab {
    position: fixed;
    right: 1rem;
    bottom: calc(1rem + env(safe-area-inset-bottom, 0px));
    z-index: 40;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 3.25rem;
    height: 3.25rem;
    padding: 0;
    border: 1px solid var(--divide);
    border-radius: 999px;
    background: var(--btn-primary-bg);
    color: var(--btn-primary-fg);
    box-shadow: 0 10px 24px rgba(0, 0, 0, 0.18);
    cursor: pointer;
    transition: background 0.15s ease;
  }
}

.create-fab:hover,
.create-fab:focus-visible {
  background: var(--btn-primary-hover-bg, #292524);
}

.create-fab:focus-visible {
  outline: 2.5px solid var(--accent-link);
  outline-offset: 2px;
}

@media (prefers-reduced-motion: reduce) {
  .create-fab,
  .search-input {
    transition: none;
  }
}

.load-more-container {
  display: flex;
  justify-content: center;
  padding-top: 1.5rem;
}

.active-filters {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
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

@media (max-width: 820px) {
  .page-header {
    display: block;
  }

  .page-context {
    padding-top: 0;
    margin-bottom: 0.75rem;
  }

  .page-actions {
    justify-content: flex-start;
  }
}

@media (max-width: 640px) {
  .page-actions {
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem;
    align-items: center;
  }

  .search-input {
    flex: 1 1 100%;
    min-width: 0;
    width: 100%;
    order: 1;
  }

  .create-menu-button {
    display: none;
  }

  .view-menu {
    order: 2;
    margin-left: auto;
  }

  .view-menu-button {
    padding-inline: 0.625rem;
  }

  .page-title {
    font-size: 1.5rem;
  }

  .library-grid {
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 1rem;
  }
}

@media (max-width: 380px) {
  .library-grid {
    grid-template-columns: repeat(2, 1fr);
  }
}
</style>
