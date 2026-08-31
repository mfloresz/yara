import { ref, type Ref } from "vue";
import type { Novel } from "@/domain";
import { useAppServices } from "@/app/services";

export type ExportSource = "refined" | "translated" | "original";

export function useExportFlow(novel: Ref<Novel | null>) {
  const { api } = useAppServices();
  const source = ref<ExportSource>("refined");
  const building = ref(false);
  const progress = ref(0);
  const feedback = ref<string | null>(null);

  async function buildAndDownload() {
    if (!novel.value) return;
    building.value = true;
    feedback.value = null;
    progress.value = 10;
    try {
      const result = await api.epubs.build({
        novelId: novel.value.id,
        source: source.value,
      });
      progress.value = 80;
      const blob = await api.epubs.download(result.id, result.updatedAt);
      const fileName = result.fileName || `${novel.value.sourceTitle || "libro"}.epub`;
      const anchor = document.createElement("a");
      anchor.href = URL.createObjectURL(blob);
      anchor.download = fileName;
      document.body.appendChild(anchor);
      anchor.click();
      anchor.remove();
      URL.revokeObjectURL(anchor.href);
      progress.value = 100;
      feedback.value = `EPUB generado y guardado en el servidor.`;
    } catch (err) {
      feedback.value = `Error: ${err instanceof Error ? err.message : String(err)}`;
    } finally {
      building.value = false;
      window.setTimeout(() => {
        progress.value = 0;
      }, 1500);
    }
  }

  return {
    source,
    building,
    progress,
    feedback,
    buildAndDownload,
  };
}
