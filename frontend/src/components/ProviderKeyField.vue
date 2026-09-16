<template>
  <div class="stack-sm">
    <label class="small muted" :for="inputId">API Key</label>
    <n-input
      :id="inputId"
      :value="modelValue"
      type="password"
      show-password-on="click"
      autocomplete="new-password"
      :placeholder="configured ? '••••••••••••' : 'Pega una nueva API key'"
      :style="{ fontFamily: 'monospace' }"
      @update:value="$emit('update:modelValue', $event)"
    />
    <div class="row-gap" style="margin-top: 0.35rem">
      <n-tag v-if="configured" type="success" size="small" round>Configurada</n-tag>
      <n-tag v-else-if="sharedKeyAvailable" type="warning" size="small" round>Clave compartida</n-tag>
      <n-tag v-else type="default" size="small" round>Sin clave</n-tag>
      <span class="small muted">
        <template v-if="configured && updatedAt">actualizada {{ formatDate(updatedAt) }}</template>
        <template v-else-if="configured">activa</template>
        <template v-else-if="sharedKeyAvailable">del admin — puedes usar tu propia key si lo deseas</template>
        <template v-else>requerida para traducir</template>
      </span>
    </div>
    <div class="row-wrap" style="margin-top: 0.5rem">
      <n-button secondary size="small" :disabled="!modelValue.trim()" :loading="replacing" @click="$emit('replace')">
        Reemplazar key
      </n-button>
      <n-button type="error" secondary size="small" :disabled="!configured" :loading="deleting" @click="$emit('delete')">
        Eliminar key
      </n-button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { NButton, NInput, NTag } from "naive-ui";

withDefaults(
  defineProps<{
    modelValue: string;
    configured: boolean;
    sharedKeyAvailable?: boolean;
    updatedAt?: string;
    replacing?: boolean;
    deleting?: boolean;
    inputId?: string;
  }>(),
  {
    sharedKeyAvailable: false,
    updatedAt: undefined,
    replacing: false,
    deleting: false,
    inputId: "provider-api-key",
  },
);

defineEmits<{
  (e: "update:modelValue", value: string): void;
  (e: "replace"): void;
  (e: "delete"): void;
}>();

function formatDate(value?: string) {
  if (!value) return "";
  return new Date(value).toLocaleDateString();
}
</script>
