<template>
  <div class="auth-page">
    <n-card class="auth-card" size="small">
      <template #header>Crear cuenta</template>
      <div class="stack-md">
        <div>
          <label class="small muted">Nombre</label>
          <n-input v-model:value="name" placeholder="Nombre" />
        </div>
        <div>
          <label class="small muted">Email</label>
          <n-input v-model:value="email" type="text" placeholder="Email" />
        </div>
        <div>
          <label class="small muted">Contraseña</label>
          <n-input v-model:value="password" type="password" show-password-on="click" placeholder="Contraseña" />
        </div>
        <n-alert v-if="error" type="error" :title="error" />
        <n-button type="primary" block :loading="loading" @click="submit">Crear cuenta</n-button>
        <n-button block secondary @click="router.push('/login')">Ya tengo cuenta</n-button>
      </div>
    </n-card>
  </div>
</template>

<script setup lang="ts">
import { ref } from "vue";
import { useRouter } from "vue-router";
import { NCard, NInput, NButton, NAlert } from "naive-ui";
import { useAppServices } from "@/app/services";

const router = useRouter();
const { register } = useAppServices();
const name = ref("");
const email = ref("");
const password = ref("");
const loading = ref(false);
const error = ref<string | null>(null);

async function submit() {
  loading.value = true;
  error.value = null;
  try {
    await register({ name: name.value, email: email.value, password: password.value });
    await router.push("/");
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err);
  } finally {
    loading.value = false;
  }
}
</script>

<style scoped>
.auth-page {
  min-height: 100vh;
  display: grid;
  place-items: center;
  padding: 1rem;
}

.auth-card {
  width: 100%;
  max-width: 420px;
}

@media (max-width: 640px) {
  .auth-page {
    padding: 0.75rem;
  }
}
</style>
