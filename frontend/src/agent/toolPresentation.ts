// Shared presentation for agent tool rows in ChatPage.
// Keep this the single place that knows agent tool names: labels, one-line
// result summaries and compact argument previews. The transcript stays
// readable when the backend adds a tool — add one entry here, not a branch in
// the page.

export const TOOL_SUMMARY_MAX = 72;
export const TOOL_ARGS_MAX = 56;
export const TOOL_DETAIL_MAX = 4000;

// Tool identifiers are an implementation detail of the agent loop; the UI
// speaks the user's language, not the backend's. Covers every tool the
// backend exposes (novels + comics); unknown names fall back to "Trabajando".
export const TOOL_LABELS: Record<string, string> = {
  // Novels — read
  list_novels: "Consultando novelas",
  get_novel: "Leyendo la novela",
  get_novel_stats: "Consultando el progreso",
  get_novel_chapters: "Consultando capítulos",
  get_chapter: "Leyendo el capítulo",
  query_library: "Consultando la biblioteca",
  search_chapters: "Buscando en capítulos",
  search_library: "Buscando en la biblioteca",
  list_tags: "Consultando etiquetas",
  list_authors: "Consultando autores",
  list_series: "Consultando series",
  get_active_jobs: "Consultando tareas activas",
  get_novel_jobs: "Consultando tareas de la novela",
  get_glossary: "Consultando el glosario",
  get_reading_progress: "Consultando el progreso de lectura",
  list_novel_epubs: "Consultando ePub",
  // Novels — write
  update_novel: "Actualizando la novela",
  update_chapter: "Actualizando el capítulo",
  set_chapter_status: "Cambiando el estado",
  set_chapter_excluded: "Cambiando la visibilidad",
  bulk_set_chapter_status: "Actualizando estados en lote",
  bulk_set_chapter_excluded: "Actualizando visibilidad en lote",
  update_glossary: "Actualizando el glosario",
  generate_glossary: "Generando el glosario",
  translate_novel_description: "Traduciendo la descripción",
  set_reading_progress: "Actualizando el progreso de lectura",
  // Jobs / site
  create_job: "Creando tarea",
  cancel_job: "Cancelando tarea",
  retry_job: "Reintentando tarea",
  check_novel_updates: "Comprobando actualizaciones",
  update_novel_from_url: "Actualizando desde la web",
  build_epub: "Generando ePub",
  // Cleanup (cards own the decision UI; rows only as audit trail)
  preview_chapter_cleanup: "Vista previa de limpieza",
  propose_cleanup: "Propuesta de limpieza",
  // Comics
  list_comics: "Consultando cómics",
  get_comic: "Leyendo el cómic",
  get_comic_chapters: "Consultando capítulos del cómic",
  check_comic_updates: "Comprobando actualizaciones del cómic",
  // Conversational
  ask_user: "Preguntando",
};

export function toolLabel(name: string): string {
  return TOOL_LABELS[name] ?? "Trabajando";
}

export function shorten(text: string, max: number): string {
  const t = text.trim();
  return t.length <= max ? t : `${t.slice(0, max - 1)}…`;
}

// One-line result summary for a tool row. Tool results are JSON envelopes
// ({"returned": n}, {"hits": [...]}, {"ok": true} …) or an "error: …" string,
// so the summary reads the common keys and falls back to the first line.
export function summarizeToolResult(raw: string): string {
  const text = (raw ?? "").trim();
  if (!text) return "";
  if (/^error:/i.test(text)) {
    return shorten(text.replace(/^error:\s*/i, ""), TOOL_SUMMARY_MAX);
  }
  try {
    const parsed = JSON.parse(text) as unknown;
    if (Array.isArray(parsed)) {
      return `${parsed.length} ${parsed.length === 1 ? "elemento" : "elementos"}`;
    }
    if (parsed && typeof parsed === "object") {
      const obj = parsed as Record<string, unknown>;
      if (typeof obj.returned === "number") return `${obj.returned} resultados`;
      if (typeof obj.total === "number") return `${obj.total} resultados`;
      if (Array.isArray(obj.hits)) return `${obj.hits.length} coincidencias`;
      if (obj.ok === false && typeof obj.error === "string") {
        return shorten(obj.error, TOOL_SUMMARY_MAX);
      }
      if (obj.ok === true) return "hecho";
    }
    return "";
  } catch {
    // Not JSON — a plain sentence, take its first line below.
  }
  return shorten(text.split("\n")[0] ?? "", TOOL_SUMMARY_MAX);
}

// Compact preview of the call arguments, coding-agent style
// ("Título: X · Modo: regex"). Objects are trimmed to their first entries.
export function toolArgsHint(args?: string): string {
  const raw = (args ?? "").trim();
  if (!raw) return "";
  try {
    const parsed = JSON.parse(raw) as unknown;
    if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
      const entries = Object.entries(parsed as Record<string, unknown>)
        .filter(([, v]) => v !== null && v !== undefined && v !== "")
        .slice(0, 2)
        .map(([k, v]) => `${k}: ${typeof v === "object" ? "…" : String(v)}`);
      if (entries.length > 0) return shorten(entries.join(" · "), TOOL_ARGS_MAX);
      return "";
    }
  } catch {
    // Not JSON — fall through to the raw preview.
  }
  return shorten(raw, TOOL_ARGS_MAX);
}

export function formatDuration(ms: number): string {
  if (!Number.isFinite(ms) || ms <= 0) return "";
  if (ms < 1000) return `${Math.max(1, Math.round(ms))} ms`;
  const s = ms / 1000;
  return `${s < 10 ? s.toFixed(1) : Math.round(s)} s`;
}
