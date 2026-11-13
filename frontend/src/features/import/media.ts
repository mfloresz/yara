// Per-media configuration shared by the import dialogs. The dialogs stay
// media-agnostic: text and the post-import redirect come from here, while the
// pages inject the actual preview/import calls (novels go through useNovels so
// its store cache stays fresh; comics call the API directly).
export type ImportMedia = "novels" | "comics";

export const IMPORT_MEDIA: Record<
  ImportMedia,
  {
    itemLabel: string;
    dialogTitle: string;
    urlPlaceholder: string;
    detailPath: (id: string) => string;
  }
> = {
  novels: {
    itemLabel: "novela",
    dialogTitle: "Importar novela desde URL",
    urlPlaceholder: "https://novelphoenix.com/novel/...",
    detailPath: (id) => `/novels/${id}`,
  },
  comics: {
    itemLabel: "cómic",
    dialogTitle: "Importar cómic desde URL",
    urlPlaceholder: "https://lector-mangas.lat/comics/...",
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
