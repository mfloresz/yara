import { jobStatusLabel } from "@/composables/useJobHelpers";
import type { Novel, TranslationJob } from "@/domain";

export const OPERATION_PREVIEW_CACHE_TTL_MS = 15 * 60 * 1000;

export type UpdateTone = "default" | "success" | "warning" | "info" | "error";

export interface NovelJobContext {
  checkJob?: TranslationJob;
  downloadJob?: TranslationJob;
  translateJob?: TranslationJob;
  updateResult?: { added: number; error?: string };
}

/** Estado del origen: si hay URL, si se verificó, si hay novedades. Nada de traducción aquí. */
export interface NovelOriginStatus {
  type: UpdateTone;
  text: string;
  tip: string;
}

/** Estado de la traducción: cuántos capítulos van, nada del origen aquí. */
export interface NovelTranslationStatus {
  tagType: UpdateTone;
  tagText: string;
  tagTip: string;
  showProgress: boolean;
  progressPct: number;
  translatedCount: number;
  chapterCount: number;
  progressTip: string;
}

export interface NovelOperationStatus {
  origin: NovelOriginStatus;
  translation: NovelTranslationStatus;
  canCheck: boolean;
  canDownload: boolean;
  canTranslate: boolean;
  checkLabel: string;
  downloadLabel: string;
  translateLabel: string;
  checkBusy: boolean;
  downloadBusy: boolean;
  translateBusy: boolean;
  checkDisabled: boolean;
  downloadDisabled: boolean;
  translateDisabled: boolean;
}

export function isActualizable(novel: Novel): boolean {
  return novel.canUpdate;
}

export function hasAnyActive(novelId: string, jobs: TranslationJob[]): boolean {
  return jobs.some(
    (j) =>
      j.novelId === novelId &&
      (j.operation === "check" || j.operation === "download" || j.operation === "translate" || j.operation === "refine"),
  );
}

export function isCheckStale(novel: Novel): boolean {
  if (!novel.lastCheckedAt) return true;
  const checkedAt = new Date(novel.lastCheckedAt).getTime();
  return Date.now() - checkedAt > OPERATION_PREVIEW_CACHE_TTL_MS;
}

export function isSameLanguage(novel: Novel): boolean {
  const a = (novel.sourceLanguage || "").trim().toLowerCase();
  const b = (novel.targetLanguage || "").trim().toLowerCase();
  return !!a && a === b;
}

export function translationRatio(novel: Novel): number {
  if (isSameLanguage(novel)) return 1;
  if (!novel.chapterCount) return 1;
  return novel.translatedCount / novel.chapterCount;
}

export function hasPendingTranslation(novel: Novel): boolean {
  if (isSameLanguage(novel)) return false;
  return novel.chapterCount > 0 && novel.translatedCount < novel.chapterCount;
}

export function hasNewChapters(novel: Novel): boolean {
  if (!isCheckStale(novel)) return (novel.lastCheckNewChapters ?? 0) > 0;
  return false;
}

function buildOriginStatus(novel: Novel, ctx: NovelJobContext): NovelOriginStatus {
  const { checkJob, downloadJob, updateResult } = ctx;
  if (downloadJob) {
    return { type: "warning", text: jobStatusLabel(downloadJob), tip: "Trayendo los capítulos nuevos desde el origen" };
  }
  if (checkJob) {
    return { type: "info", text: jobStatusLabel(checkJob), tip: "Buscando capítulos nuevos en el origen" };
  }
  if (updateResult) {
    if (updateResult.error) {
      return { type: "error", text: "Error", tip: `${updateResult.error}. Puedes reintentar la verificación.` };
    }
    if (updateResult.added === 0) {
      return { type: "success", text: "Sin novedades", tip: "El origen no tiene capítulos nuevos" };
    }
    return {
      type: "success",
      text: `+${updateResult.added} nuevos`,
      tip: `${updateResult.added} capítulos nuevos listos para descargar`,
    };
  }
  if (novel.lastCheckedAt && !isCheckStale(novel)) {
    const fresh = (novel.lastCheckNewChapters ?? 0) === 0;
    if (fresh) {
      return { type: "success", text: "Sin novedades", tip: "Revisado hace poco, sin capítulos nuevos" };
    }
    return {
      type: "info",
      text: `+${novel.lastCheckNewChapters} nuevos`,
      tip: `${novel.lastCheckNewChapters} capítulos nuevos detectados. Descárgalos para traerlos.`,
    };
  }
  if (isActualizable(novel) && novel.status !== "completed") {
    return { type: "warning", text: "Actualizable", tip: "Tiene URL de origen. Verifícala para detectar capítulos nuevos." };
  }
  if (novel.status === "completed") {
    return { type: "info", text: "Completada", tip: "Marcada como completada, ya no se verifica" };
  }
  return { type: "default", text: "—", tip: "Sin URL de origen, no se puede verificar" };
}

function buildTranslationStatus(novel: Novel, translateJob: TranslationJob | undefined): NovelTranslationStatus {
  const base = {
    showProgress: !isSameLanguage(novel) && novel.chapterCount > 0,
    progressPct: novel.chapterCount > 0 ? Math.round((novel.translatedCount / novel.chapterCount) * 100) : 0,
    translatedCount: novel.translatedCount,
    chapterCount: novel.chapterCount,
    progressTip: `${novel.translatedCount} de ${novel.chapterCount} capítulos traducidos`,
  };
  if (translateJob) {
    return { ...base, tagType: "info", tagText: jobStatusLabel(translateJob), tagTip: base.progressTip };
  }
  if (isSameLanguage(novel)) {
    return {
      ...base,
      tagType: "default",
      tagText: "Mismo idioma",
      tagTip: `Origen (${novel.sourceLanguage}) y destino (${novel.targetLanguage}) coinciden, no requiere traducción`,
    };
  }
  if (novel.chapterCount === 0) {
    return { ...base, tagType: "default", tagText: "Sin capítulos", tagTip: "Esta novela aún no tiene capítulos" };
  }
  if (hasPendingTranslation(novel)) {
    const pending = novel.chapterCount - novel.translatedCount;
    return {
      ...base,
      tagType: "info",
      tagText: `${pending} pendientes`,
      tagTip: `${base.progressTip} (${pending} pendientes)`,
    };
  }
  return { ...base, tagType: "success", tagText: "Traducida", tagTip: base.progressTip };
}

/** Single source of truth for every per-novel operations display: table cells, mobile card and row actions. */
export function buildNovelOperationStatus(novel: Novel, ctx: NovelJobContext): NovelOperationStatus {
  const { checkJob, downloadJob, translateJob } = ctx;
  const canCheck = isActualizable(novel) && novel.status !== "completed";
  const canDownload = hasNewChapters(novel);
  const canTranslate = hasPendingTranslation(novel);
  const pendingCount = novel.chapterCount - novel.translatedCount;

  return {
    origin: buildOriginStatus(novel, ctx),
    translation: buildTranslationStatus(novel, translateJob),
    canCheck,
    canDownload,
    canTranslate,
    checkLabel: checkJob ? jobStatusLabel(checkJob) : "Verificar origen",
    downloadLabel: downloadJob
      ? jobStatusLabel(downloadJob)
      : canDownload
        ? `Descargar +${novel.lastCheckNewChapters}`
        : "Descargar",
    translateLabel: translateJob
      ? jobStatusLabel(translateJob)
      : canTranslate
        ? `Traducir (${pendingCount} pendientes)`
        : "Traducir",
    checkBusy: !!checkJob,
    downloadBusy: !!downloadJob,
    translateBusy: !!translateJob,
    checkDisabled: !canCheck || !!downloadJob || !!translateJob,
    downloadDisabled: (!canDownload && !downloadJob) || !!checkJob || !!translateJob,
    translateDisabled: (!canTranslate && !translateJob) || !!checkJob || !!downloadJob,
  };
}
