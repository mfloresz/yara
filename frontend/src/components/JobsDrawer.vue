<template>
  <n-drawer :show="visible" :width="drawerWidth" placement="right" @update:show="$emit('update:visible', $event)">
    <n-drawer-content>
      <template #header>
        <span class="jobs-header">Trabajos activos</span>
      </template>

      <div class="stack-md">
        <div v-if="jobs.length === 0 && !loading" class="jobs-empty">
          <n-icon :size="40" style="color: var(--text-tertiary)"><CheckmarkCircleOutline /></n-icon>
          <div>
            <h3 class="jobs-empty-title">Sin trabajos activos</h3>
            <p class="muted small" style="margin: 0">Inicia una traducción o refinamiento desde una novela para ver el progreso aquí.</p>
          </div>
        </div>

        <template v-else-if="jobs.length === 0 && loading">
          <div v-for="n in 3" :key="n" class="job-skeleton" aria-hidden="true">
            <div class="job-skeleton-line job-skeleton-line--lg"></div>
            <div class="job-skeleton-line job-skeleton-line--sm"></div>
            <div class="job-skeleton-bar"></div>
          </div>
        </template>

        <n-card v-for="job in jobs" :key="job.id" size="small">
          <div class="stack-md">
            <div class="row-between" style="align-items: flex-start">
              <div style="min-width: 0; flex: 1">
                <n-button
                  text
                  tag="a"
                  class="job-title"
                  @click="openNovel(job)"
                >
                  {{ job.novelTitle || job.novelId }}
                </n-button>
                <div class="small muted" style="margin-top: 0.2rem">
                  {{ operationLabel(job) }}
                  <template v-if="showsProviderMeta(job)">
                    <span> · </span><span v-if="job.provider">{{ job.provider }}</span><span v-if="job.provider && job.model">/</span><span v-if="job.model">{{ job.model }}</span>
                  </template>
                </div>
              </div>
              <n-tag :type="jobTagType(job.status)" size="small" round>
                {{ jobStatusLabel(job.status) }}
              </n-tag>
            </div>

            <div class="stack-sm">
              <div class="row-between small">
                <span class="muted">Progreso</span>
                <span>
                  <strong>{{ job.completedChapters }}</strong>/{{ job.totalChapters }}
                  <span v-if="job.failedChapters > 0" class="failed-chapters"> · {{ job.failedChapters }} fallidos</span>
                </span>
              </div>
              <n-progress v-if="jobShowsCompletedProgress(job)" :percentage="jobProgress(job)" :show-indicator="false" />
              <n-spin v-else :size="16" />
              <div v-if="jobCurrentActivityLabel(job)" class="small muted">
                {{ jobCurrentActivityLabel(job) }}
              </div>
              <div v-if="job.status === 'failed' && job.errorMessage" class="job-error small">
                <n-icon :size="15"><AlertCircleOutline /></n-icon>
                <span>{{ job.errorMessage }}</span>
              </div>
            </div>

            <div v-if="showAutoSegmentMeta(job)" class="stack-sm jobs-segment">
              <div class="small muted">
                {{ autoSegmentLabel(job) }}
              </div>
              <div v-if="showAutoSegmentProgress(job)" class="stack-sm">
                <div class="row-between small">
                  <span class="muted">Segmentos</span>
                  <span><strong>{{ segmentCompletedLabel(job) }}</strong>/{{ job.autoSegmentCount }}</span>
                </div>
                <n-progress :percentage="segmentProgress(job)" :show-indicator="false" style="height: 8px" />
              </div>
            </div>

            <div class="row-between small">
              <span class="muted mono">#{{ job.id }}</span>
              <n-button
                v-if="job.status === 'running' || job.status === 'pending'"
                size="small"
                type="error"
                secondary
                :loading="cancellingId === job.id"
                @click="cancel(job)"
              >
                <template #icon>
                  <n-icon><StopOutline /></n-icon>
                </template>
                Cancelar
              </n-button>
            </div>
          </div>
        </n-card>
      </div>
    </n-drawer-content>
  </n-drawer>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import {
  NDrawer,
  NDrawerContent,
  NCard,
  NTag,
  NProgress,
  NSpin,
  NIcon,
  NButton,
} from "naive-ui";
import { AlertCircleOutline, CheckmarkCircleOutline, StopOutline } from "@vicons/ionicons5";
import { useActiveJobs } from "@/composables/useActiveJobs";
import type { TranslationJob } from "@/domain";

const router = useRouter();
const visible = defineModel<boolean>("visible", { required: true });
const { jobs, loading, cancelJob } = useActiveJobs({ enabled: visible });
const cancellingId = ref<string | null>(null);

const windowWidth = ref(typeof window !== "undefined" ? window.innerWidth : 1024);
function handleResize() {
  windowWidth.value = window.innerWidth;
}
onMounted(() => window.addEventListener("resize", handleResize));
onBeforeUnmount(() => window.removeEventListener("resize", handleResize));

const drawerWidth = computed(() => (windowWidth.value <= 480 ? "100%" : 420));

function jobFinishedChapterCount(job: TranslationJob) {
  return job.completedChapters + job.failedChapters;
}

function jobHasStartedWork(job: TranslationJob) {
  return job.status === "running" && (
    jobFinishedChapterCount(job) > 0 ||
    Boolean(job.autoSegmentActive) ||
    Boolean((job.autoSegmentChapterTitle || job.autoSegmentChapterId || "").trim())
  );
}

function jobShowsCompletedProgress(job: TranslationJob) {
  return !jobHasStartedWork(job) || jobFinishedChapterCount(job) > 0;
}

function jobProgress(job: TranslationJob) {
  if (job.totalChapters <= 0) return 0;
  return Math.round((jobFinishedChapterCount(job) / job.totalChapters) * 100);
}

function jobStatusLabel(status: TranslationJob["status"]) {
  return {
    pending: "Pendiente",
    running: "En progreso",
    done: "Completado",
    cancelled: "Cancelado",
    failed: "Fallido",
  }[status] || status;
}

function operationLabel(job: TranslationJob) {
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

function showsProviderMeta(job: TranslationJob) {
  return job.operation !== "download" && job.operation !== "check" && Boolean(job.provider || job.model);
}

function jobTagType(status: TranslationJob["status"]) {
  return ({
    pending: "default",
    running: "info",
    done: "success",
    cancelled: "warning",
    failed: "error",
  }[status] || "default") as "default" | "info" | "success" | "warning" | "error";
}

function showAutoSegmentMeta(job: TranslationJob) {
  return job.operation !== "refine" && job.operation !== "download" && job.operation !== "check" && Boolean(job.autoSegmentChapterTitle || (job.autoSegmentCount ?? 0) > 1);
}

function showAutoSegmentProgress(job: TranslationJob) {
  return (job.autoSegmentCount ?? 0) > 1;
}

function segmentCompletedLabel(job: TranslationJob) {
  const completed = job.autoSegmentCompletedCount ?? 0;
  const current = job.autoSegmentCurrentIndex ?? 0;
  return Math.max(completed, current > 0 ? current - 1 : 0);
}

function segmentProgress(job: TranslationJob) {
  const total = job.autoSegmentCount ?? 0;
  if (total <= 0) return 0;
  return Math.round((segmentCompletedLabel(job) / total) * 100);
}

function autoSegmentLabel(job: TranslationJob) {
  const count = job.autoSegmentCount ?? 0;
  const current = job.autoSegmentCurrentIndex ?? 0;
  const chapter = (job.autoSegmentChapterTitle || job.autoSegmentChapterId || "").trim();
  if (count > 1 && current > 0) return `${chapter} · segmento ${current} de ${count}`;
  if (count > 1) return `${chapter} · ${count} segmentos`;
  return chapter || "";
}

function jobCurrentActivityLabel(job: TranslationJob) {
  if (job.status === "pending") return "En cola…";
  if (job.status !== "running") return "";

  const chapter = (job.autoSegmentChapterTitle || job.autoSegmentChapterId || "").trim();
  const segmentCount = job.autoSegmentCount ?? 0;
  const currentSegment = job.autoSegmentCurrentIndex ?? 0;

  if (job.operation === "download") {
    if (chapter) return `Descargando capítulo: ${chapter}`;
    return "Descargando capítulos…";
  }

  if (job.operation === "check") {
    return "Verificando capítulos…";
  }

  if (segmentCount > 1 && chapter) {
    if (currentSegment > 0) return `Traduciendo ${chapter} · segmento ${currentSegment} de ${segmentCount}`;
    return `Preparando ${chapter} · ${segmentCount} segmentos`;
  }

  if (segmentCount > 1) {
    if (currentSegment > 0) return `Traduciendo segmento ${currentSegment} de ${segmentCount}`;
    return `Preparando ${segmentCount} segmentos`;
  }

  if (job.totalChapters === 1 && chapter) return `Traduciendo capítulo actual: ${chapter}`;
  if (job.totalChapters === 1) return "Traduciendo capítulo actual…";
  if (chapter) return `Traduciendo capítulo actual: ${chapter}`;
  return "Traduciendo capítulos…";
}

function openNovel(job: TranslationJob) {
  visible.value = false;
  void router.push(`/novels/${job.novelId}`);
}

async function cancel(job: TranslationJob) {
  cancellingId.value = job.id;
  try {
    await cancelJob(job.id);
  } catch (err) {
    console.error("Failed to cancel job:", err);
  } finally {
    cancellingId.value = null;
  }
}
</script>

<style scoped>
.jobs-header {
  font-weight: 600;
  font-size: 1.1rem;
}

.jobs-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
  gap: 0.75rem;
  padding: 2rem 1rem;
  border: 1px dashed var(--divide);
  border-radius: var(--radius-md);
}

.jobs-empty-title {
  margin: 0 0 0.35rem;
}

.job-title {
  padding: 0;
  font-weight: 600;
  text-align: left;
}

.failed-chapters {
  color: var(--danger);
}

.job-error {
  display: flex;
  align-items: flex-start;
  gap: 0.4rem;
  color: var(--danger);
  line-height: 1.4;
}

.job-error .n-icon {
  flex-shrink: 0;
  margin-top: 0.1rem;
}

.job-skeleton {
  border: 1px solid var(--divide);
  border-radius: var(--radius-md);
  padding: 1rem;
  display: flex;
  flex-direction: column;
  gap: 0.625rem;
  background: var(--surface-elevated);
}

.job-skeleton-line {
  height: 0.75rem;
  border-radius: var(--radius-pill);
  background: var(--mock-row-strong);
}

.job-skeleton-line--lg {
  width: 55%;
  height: 1rem;
}

.job-skeleton-line--sm {
  width: 35%;
}

.job-skeleton-bar {
  height: 8px;
  border-radius: var(--radius-pill);
  background: var(--mock-row-strong);
}

.job-skeleton-line,
.job-skeleton-bar {
  animation: jobs-pulse 1.4s ease-in-out infinite;
}

@keyframes jobs-pulse {
  0%,
  100% {
    opacity: 1;
  }
  50% {
    opacity: 0.55;
  }
}

@media (prefers-reduced-motion: reduce) {
  .job-skeleton-line,
  .job-skeleton-bar {
    animation: none;
  }
}
</style>
