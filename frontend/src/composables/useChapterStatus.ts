import type { Chapter } from "@/domain";
import type { ChapterSummary } from "@/api/types";

type ChapterStatus = Chapter["status"];
type TagType = "default" | "info" | "warning" | "success" | "error";

const STATUS_LABELS: Record<ChapterStatus, string> = {
  pending: "Pendiente",
  processing: "Procesando",
  translated: "Traducido",
  refined: "Refinado",
  done: "Completado",
  failed: "Error",
};

const STATUS_TAG_TYPES: Record<ChapterStatus, TagType> = {
  pending: "default",
  processing: "warning",
  translated: "success",
  refined: "info",
  done: "success",
  failed: "error",
};

export function chapterStatusLabel(status: ChapterStatus): string {
  return STATUS_LABELS[status] ?? status;
}

export function chapterTagType(status: ChapterStatus): TagType {
  return STATUS_TAG_TYPES[status] ?? "default";
}

export function resolvedChapterStatus(chapter: Chapter | ChapterSummary): ChapterStatus {
  if (chapter.status === "processing") return "processing";
  return chapter.status;
}
