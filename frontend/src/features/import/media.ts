// Per-media configuration shared by the import dialogs. The dialogs stay
// media-agnostic: text, supported-site hints and the post-import redirect all
// come from here, while the pages inject the actual preview/import calls
// (novels go through useNovels so its store cache stays fresh; comics call the
// API directly).
export type ImportMedia = "novels" | "comics";

export const IMPORT_MEDIA: Record<
  ImportMedia,
  {
    itemLabel: string;
    dialogTitle: string;
    urlPlaceholder: string;
    sites: string[];
    detailPath: (id: string) => string;
  }
> = {
  novels: {
    itemLabel: "novela",
    dialogTitle: "Importar novela desde URL",
    urlPlaceholder: "https://novelphoenix.com/novel/...",
    sites: [
      "novelfire.net",
      "novelphoenix.com",
      "fenrirealm.com",
      "floraegarden.com",
      "cherrymist.cafe",
      "empirenovel.com",
      "69shuba.com",
      "skynovels.net",
      "skydemonorder.com",
      "literotica.com",
      "novelarrow.com",
      "wattpad.com",
      "webnovel.com",
      "inkitt.com",
      "gaydemon.com",
      "chrysanthemumgarden.com",
      "getinkspired.com",
    ],
    detailPath: (id) => `/novels/${id}`,
  },
  comics: {
    itemLabel: "cómic",
    dialogTitle: "Importar cómic desde URL",
    urlPlaceholder: "https://lector-mangas.lat/comics/...",
    sites: [
      "manhwaweb.com",
      "zonatmo.org",
      "samurai.j5z.xyz",
      "onfmangas.com",
      "lector-mangas.lat",
      "toonkotsu.com",
      "manhuaus.com",
      "manhwashot.lat",
      "mhscans.com",
      "manhuahot.com",
    ],
    detailPath: (id) => `/comics/${id}`,
  },
};

/** Normalized import result the confirm dialog consumes per media. */
export type ImportOutcome = {
  id: string;
  chaptersImported: number;
  downloadJob?: { totalChapters: number };
};

export type ImportUrlInput = {
  url: string;
  sourceLanguage?: string;
  targetLanguage?: string;
  startChapter?: number;
  endChapter?: number;
};

/** Outcome of appending chapters to an existing item ("add to existing"). */
export type IntoImportOutcome = {
  pendingChapters: number;
  message?: string;
};
