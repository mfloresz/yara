<template>
  <section class="stack-md tab-panel" aria-labelledby="tab-errors">
    <h2 id="tab-errors" class="sr-only">Historial de errores</h2>
    <n-card v-if="jobs.length === 0">
      <div class="stack-md" style="align-items: center; text-align: center; padding: 2rem 1rem">
        <n-icon :size="40" color="var(--text-tertiary)"><TimeOutline /></n-icon>
        <div>
          <h3 style="margin: 0 0 0.5rem">Aún no hay errores</h3>
          <p class="muted">Cuando un trabajo falle, verás los detalles aquí.</p>
        </div>
      </div>
    </n-card>
    <n-card v-for="job in jobs" :key="job.id">
      <div class="stack-md">
        <div class="row-between">
          <div>
            <div style="font-weight: 600">{{ job.completedChapters }}/{{ job.totalChapters }} completados · {{ job.failedChapters }} fallidos</div>
            <div class="small muted">{{ job.provider || 'provider por defecto' }} · {{ job.model || 'model por defecto' }} · {{ formatDate(job.createdAt) }}</div>
          </div>
          <div class="row-wrap">
            <n-tag :type="jobTagType(job.status)" size="small" round>{{ jobStatusLabel(job) }}</n-tag>
            <n-button v-if="job.status === 'running' || job.status === 'pending'" size="small" type="error" secondary @click="emit('cancel-job', job.id)">Cancelar</n-button>
          </div>
        </div>
        <n-progress v-if="jobShowsCompletedProgress(job)" :percentage="jobProgress(job)" :show-indicator="true" />
        <n-progress v-else :show-indicator="false" :status="'info'" :percentage="100" />
        <div v-if="jobCurrentActivityLabel(job)" class="small muted">
          {{ jobCurrentActivityLabel(job) }}
        </div>
        <n-alert v-if="job.errorMessage?.trim()" :type="job.status === 'failed' ? 'error' : 'warning'" :closable="false">
          <div class="stack-sm" style="gap: 0.25rem">
            <strong>{{ job.status === 'failed' ? 'Motivo del fallo del trabajo' : 'Aviso del trabajo' }}</strong>
            <span class="mono small" style="white-space: pre-wrap; word-break: break-word">{{ job.errorMessage }}</span>
          </div>
        </n-alert>
        <div v-if="failedChapters(job).length > 0" class="job-failed-chapters">
          <div class="row-between" @click="toggleJobFailedChapters(job.id)" style="cursor: pointer; user-select: none">
            <div class="row-wrap">
              <n-icon :size="14"><ChevronDownOutline v-if="expandedJobId === job.id" /><ChevronForwardOutline v-else /></n-icon>
              <strong>Capítulos fallidos ({{ failedChapters(job).length }})</strong>
            </div>
            <span class="small muted">{{ expandedJobId === job.id ? 'Ocultar' : 'Ver' }} detalles</span>
          </div>
          <div v-if="expandedJobId === job.id" class="stack-sm" style="margin-top: 0.5rem">
            <div v-for="chapter in failedChapters(job)" :key="chapter.id" class="job-failed-chapter-item">
              <div class="row-between" style="align-items: flex-start; gap: 0.75rem">
                <div style="min-width: 0; flex: 1">
                  <div class="row-wrap">
                    <span class="mono small muted">#{{ chapter.chapterOrder }}</span>
                    <n-button text style="padding: 0; text-align: left" @click="router.push(`/novels/${chapter.novelId}/chapters/${chapter.id}`)">
                      {{ chapter.title }}
                    </n-button>
                  </div>
                  <div v-if="chapter.errorMessage?.trim()" class="small job-failed-chapter-error mono">
                    {{ chapter.errorMessage }}
                  </div>
                  <div v-else class="small muted" style="font-style: italic">
                    Sin detalles disponibles para este error.
                  </div>
                </div>
                <n-tag type="error" size="small" round>{{ chapterStatusLabel(resolvedChapterStatus(chapter)) }}</n-tag>
              </div>
            </div>
          </div>
        </div>
      </div>
    </n-card>
  </section>
</template>

<script setup lang="ts">
import { ref } from "vue";
import { useRouter } from "vue-router";
import { NAlert, NButton, NCard, NIcon, NProgress, NTag } from "naive-ui";
import { ChevronDownOutline, ChevronForwardOutline, TimeOutline } from "@vicons/ionicons5";
import type { ChapterSummary } from "@/api/types";
import type { TranslationJob } from "@/domain";
import { chapterStatusLabel, resolvedChapterStatus } from "@/composables/useChapterStatus";
import {
  jobStatusLabel,
  jobTagType,
  jobShowsCompletedProgress,
  jobProgress,
  jobCurrentActivityLabel,
} from "@/composables/useJobHelpers";

const props = defineProps<{
  jobs: TranslationJob[];
  allSummaries: ChapterSummary[];
}>();

const emit = defineEmits<{
  (e: "cancel-job", jobId: string): void;
}>();

const router = useRouter();
const expandedJobId = ref<string | null>(null);

function failedChapters(job: TranslationJob) {
  if (!job.chapterIds || job.chapterIds.length === 0) return [];
  const idSet = new Set(job.chapterIds);
  return props.allSummaries
    .filter((chapter) => idSet.has(chapter.id) && chapter.status === "failed")
    .sort((a, b) => a.chapterOrder - b.chapterOrder);
}

function toggleJobFailedChapters(jobId: string) {
  expandedJobId.value = expandedJobId.value === jobId ? null : jobId;
}

function formatDate(value: string) {
  return new Date(value).toLocaleString();
}
</script>

<style scoped>
.job-failed-chapters {
  border: 1px solid var(--divide);
  border-radius: var(--radius-md);
  padding: 0.75rem 1rem;
  background: color-mix(in oklab, var(--text-primary) 4%, transparent);
}

.job-failed-chapter-item {
  padding: 0.65rem 0;
  border-bottom: 1px solid var(--divide);
}

.job-failed-chapter-item:last-child {
  border-bottom: none;
  padding-bottom: 0;
}

.job-failed-chapter-error {
  margin-top: 0.35rem;
  padding: 0.5rem 0.65rem;
  background: color-mix(in oklab, #dc2626 10%, transparent);
  border-left: 3px solid #dc2626;
  border-radius: var(--radius-sm);
  color: #7f1d1d;
  white-space: pre-wrap;
  word-break: break-word;
  font-size: 0.875rem;
}
</style>
