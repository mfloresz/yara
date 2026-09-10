import { watchEffect, type Ref } from "vue";

export function setDocumentTitle(suffix?: string | null): void {
  const clean = suffix?.trim();
  document.title = clean ? `Yara - ${clean}` : "Yara";
}

export function useDocumentTitle(source: Ref<string | null | undefined> | (() => string | null | undefined)): void {
  watchEffect(() => {
    const value = typeof source === "function" ? source() : source.value;
    setDocumentTitle(value);
  });
}
