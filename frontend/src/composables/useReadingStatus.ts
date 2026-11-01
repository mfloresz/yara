import { useAppServices } from "@/app/services";
import type { DropdownOption } from "naive-ui";
import type { ReadingStatus } from "@/domain";

export type ReadingStatusMedia = "novel" | "comic";

/**
 * Explicit per-user reading marks (reading / paused / finished) and progress
 * clearing, shared by the library cards and the detail pages.
 */
export function useReadingStatus() {
  const { api } = useAppServices();

  async function setStatus(
    media: ReadingStatusMedia,
    id: string,
    status: ReadingStatus,
  ): Promise<void> {
    if (media === "novel") {
      await api.readingProgress.update(id, { status });
    } else {
      await api.comics.readingProgress.update(id, { status });
    }
  }

  async function clearProgress(media: ReadingStatusMedia, id: string): Promise<void> {
    if (media === "novel") {
      await api.readingProgress.remove(id);
    } else {
      await api.comics.readingProgress.remove(id);
    }
  }

  return { setStatus, clearProgress };
}

interface ReadingStatusTarget {
  readingStatus?: ReadingStatus;
  /** Server-derived presence of a progress row (lastReadAt). */
  lastReadAt?: string;
}

/**
 * Menu actions for a work's reading mark. Options adapt to the current
 * status: reading → mark finished / pause; paused → resume / finish;
 * finished → read again.
 */
export function readingStatusMenuOptions(
  media: ReadingStatusMedia,
  target: ReadingStatusTarget,
): DropdownOption[] {
  const finishedLabel = media === "novel" ? "Marcar como leída" : "Marcar como leído";
  const status = target.readingStatus ?? "";
  const options: DropdownOption[] = [];
  if (status === "paused") {
    options.push({ label: "Reanudar", key: "reading" });
  } else if (status === "finished") {
    options.push({ label: "Volver a leer", key: "reading" });
  }
  if (status !== "finished") {
    options.push({ label: finishedLabel, key: "finished" });
  }
  if (target.lastReadAt && (status === "" || status === "reading")) {
    options.push({ label: "Pausar", key: "paused" });
  }
  if (target.lastReadAt) {
    options.push({ label: "Quitar progreso", key: "clear" });
  }
  return options;
}

export function readingStatusBadgeLabel(
  media: ReadingStatusMedia,
  status?: ReadingStatus,
): string {
  if (status === "finished") return media === "novel" ? "Leída" : "Leído";
  if (status === "paused") return "En pausa";
  return "";
}
