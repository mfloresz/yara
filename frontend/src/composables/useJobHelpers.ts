import type { TranslationJob, TranslationJobStatus } from "@/domain";

export type TagType = "default" | "info" | "success" | "warning" | "error";

export function jobStatusLabel(job: TranslationJob): string {
  if (job.status === "pending") return "En cola";
  if (job.status !== "running") {
    return (
      {
        done: "Completado",
        cancelled: "Cancelado",
        failed: "Fallido",
      }[job.status] || job.status
    );
  }
  if (job.operation === "check") return "Buscando capítulos nuevos…";
  if (job.operation === "download") return "Descargando…";
  if (job.operation === "refine") return "Refinando…";
  return "Traduciendo…";
}

export function jobTagType(status: TranslationJobStatus): TagType {
  const map: Record<TranslationJobStatus, TagType> = {
    pending: "default",
    running: "info",
    done: "success",
    cancelled: "warning",
    failed: "error",
  };
  return map[status] ?? "default";
}

export function operationLabel(job: TranslationJob): string {
  switch (job.operation) {
    case "download":
      return "Descarga";
    case "check":
      return "Verificación";
    case "refine":
      return "Refinamiento";
    default:
      return "Traducción";
  }
}

export function showsProviderMeta(job: TranslationJob): boolean {
  return (
    job.operation !== "download" &&
    job.operation !== "check" &&
    Boolean(job.provider || job.model)
  );
}

export function showAutoSegmentMeta(job: TranslationJob): boolean {
  return (
    job.operation !== "refine" &&
    job.operation !== "download" &&
    job.operation !== "check" &&
    Boolean(
      job.autoSegmentChapterTitle || (job.autoSegmentCount ?? 0) > 1,
    )
  );
}

export function showAutoSegmentProgress(job: TranslationJob): boolean {
  return (job.autoSegmentCount ?? 0) > 1;
}

export function autoSegmentLabel(job: TranslationJob): string {
  const count = job.autoSegmentCount ?? 0;
  const current = job.autoSegmentCurrentIndex ?? 0;
  const chapter = (
    job.autoSegmentChapterTitle ||
    job.autoSegmentChapterId ||
    ""
  ).trim();
  if (count > 1 && current > 0)
    return `${chapter} · segmento ${current} de ${count}`;
  if (count > 1) return `${chapter} · ${count} segmentos`;
  return chapter || "";
}

export function jobFinishedChapterCount(job: TranslationJob): number {
  return job.completedChapters + job.failedChapters;
}

export function jobHasStartedWork(job: TranslationJob): boolean {
  return (
    job.status === "running" &&
    (jobFinishedChapterCount(job) > 0 ||
      Boolean(job.autoSegmentActive) ||
      Boolean(
        (
          job.autoSegmentChapterTitle ||
          job.autoSegmentChapterId ||
          ""
        ).trim(),
      ))
  );
}

export function jobShowsCompletedProgress(job: TranslationJob): boolean {
  return !jobHasStartedWork(job) || jobFinishedChapterCount(job) > 0;
}

export function jobProgress(job: TranslationJob): number {
  if (job.totalChapters <= 0) return 0;
  return Math.round(
    (jobFinishedChapterCount(job) / job.totalChapters) * 100,
  );
}

export function jobCurrentActivityLabel(job: TranslationJob): string {
  if (job.status === "pending") return "En cola…";
  if (job.status !== "running") return "";

  const chapter = (
    job.autoSegmentChapterTitle ||
    job.autoSegmentChapterId ||
    ""
  ).trim();
  const segmentCount = job.autoSegmentCount ?? 0;
  const currentSegment = job.autoSegmentCurrentIndex ?? 0;

  if (job.operation === "download") {
    if (chapter) return `Descargando capítulo: ${chapter}`;
    return "Descargando capítulos…";
  }

  if (job.operation === "check") {
    return "Buscando capítulos nuevos…";
  }

  if (segmentCount > 1 && chapter) {
    if (currentSegment > 0)
      return `Traduciendo ${chapter} · segmento ${currentSegment} de ${segmentCount}`;
    return `Preparando ${chapter} · ${segmentCount} segmentos`;
  }

  if (segmentCount > 1) {
    if (currentSegment > 0)
      return `Traduciendo segmento ${currentSegment} de ${segmentCount}`;
    return `Preparando ${segmentCount} segmentos`;
  }

  if (job.totalChapters === 1 && chapter)
    return `Traduciendo capítulo actual: ${chapter}`;
  if (job.totalChapters === 1) return "Traduciendo capítulo actual…";
  if (chapter) return `Traduciendo capítulo actual: ${chapter}`;
  return "Traduciendo capítulos…";
}

export function segmentCompletedLabel(job: TranslationJob): number {
  const completed = job.autoSegmentCompletedCount ?? 0;
  const current = job.autoSegmentCurrentIndex ?? 0;
  return Math.max(completed, current > 0 ? current - 1 : 0);
}

export function segmentProgress(job: TranslationJob): number {
  const total = job.autoSegmentCount ?? 0;
  if (total <= 0) return 0;
  return Math.round((segmentCompletedLabel(job) / total) * 100);
}
