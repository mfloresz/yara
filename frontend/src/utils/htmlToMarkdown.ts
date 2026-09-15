import TurndownService from "turndown";

let service: TurndownService | null = null;

function getService(): TurndownService {
  if (!service) {
    service = new TurndownService({
      headingStyle: "atx",
      codeBlockStyle: "fenced",
      bulletListMarker: "-",
      emDelimiter: "*",
    });
    // Tiptap emite <hr> para "***" — el separador de escena usado en novelas
    // es la línea "•••", así que lo restauramos al convertir de vuelta.
    service.addRule("sceneBreak", {
      filter: "hr",
      replacement: () => "\n\n•••\n\n",
    });
  }
  return service;
}

/** Convierte el HTML de Tiptap de vuelta a Markdown (fuente de verdad del backend). */
export function htmlToMarkdown(html: string): string {
  if (!html || html === "<p></p>") return "";
  return getService().turndown(html).trim();
}
