import type { GeneralPromptRecord } from "@/api/types";
import type { Novel } from "@/domain";
import { safeUuid } from "@/utils/safe-uuid";
import {
  normalizePromptSettings,
  normalizeTranslationOptions,
  type ProjectSettings,
  type PromptSettings,
  type ServerTranslationDefaults,
} from "@/domain/project-settings";

function promptRecordsToSettings(
  records: GeneralPromptRecord[],
): PromptSettings {
  const next: PromptSettings = {};

  for (const record of records) {
    if (!record.active) continue;
    next[record.key] = { systemPrompt: record.prompt.systemPrompt };
  }

  return normalizePromptSettings(next);
}

function ensureGlossaryIds(
  glossary: unknown,
): Array<{ id: string; source: string; target: string; context?: string }> {
  if (!Array.isArray(glossary)) return [];
  return glossary.map((entry) => {
    const e = entry as Record<string, unknown>;
    return {
      id: (typeof e.id === "string" && e.id) || safeUuid(),
      source: typeof e.source === "string" ? e.source : "",
      target: typeof e.target === "string" ? e.target : "",
      context: typeof e.context === "string" ? e.context : undefined,
    };
  });
}

export function buildProjectSettings(
  novel?: Novel | null,
  globalPrompts?: GeneralPromptRecord[],
  translationDefaults?: ServerTranslationDefaults,
): ProjectSettings {
  const promptFallbacks = globalPrompts
    ? promptRecordsToSettings(globalPrompts)
    : undefined;

  return {
    notes: novel?.notes ?? "",
    glossary: ensureGlossaryIds(novel?.glossary),
    prompts: normalizePromptSettings({
      ...(promptFallbacks ?? {}),
      ...(novel?.prompts ?? {}),
    }),
    ai: {
      provider: novel?.aiOptions?.provider ?? "",
      model: novel?.aiOptions?.model ?? "",
      timeoutMs: novel?.aiOptions?.timeoutMs ?? undefined,
      titleEnabled: novel?.aiOptions?.titleEnabled ?? false,
      titleProvider: novel?.aiOptions?.titleProvider ?? "",
      titleModel: novel?.aiOptions?.titleModel ?? "",
    },
    translation: normalizeTranslationOptions({
      ...normalizeTranslationOptions(translationDefaults),
      ...(novel?.translationOptions ?? {}),
    }),
    cleanupRules: Array.isArray(novel?.cleanupRules) ? novel.cleanupRules : [],
  };
}
