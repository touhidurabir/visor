<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ServerService, type ProcessView } from '../../bindings/github.com/touhidurabir/visor/internal/app'

const processes = ref<ProcessView[]>([])
const error = ref('')
const loading = ref(true)

async function load() {
  loading.value = true
  try {
    processes.value = (await ServerService.ListProcesses()) ?? []
    error.value = ''
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <section>
    <div v-if="error" class="error" role="alert">
      <strong>Cannot load processes</strong>
      <p>{{ error }}</p>
      <button type="button" @click="load">Try again</button>
    </div>
    <p v-else-if="loading" class="muted">Loading processes…</p>
    <p v-else-if="processes.length === 0" class="muted">No processes configured.</p>
    <table v-else>
      <thead>
        <tr><th>Name</th><th>Group</th><th>State</th></tr>
      </thead>
      <tbody>
        <tr v-for="p in processes" :key="p.FullName">
          <td>{{ p.Name }}</td>
          <td>{{ p.Group }}</td>
          <td :class="['state', p.State.toLowerCase()]">{{ p.State }}</td>
        </tr>
      </tbody>
    </table>
  </section>
</template>

<style scoped>
.muted { color: var(--text-muted); }
table { width: 100%; border-collapse: collapse; }
th, td { text-align: left; padding: 4px 8px; }
th { color: var(--text-muted); font-weight: 500; }
.state.running { color: #1a7f37; }
.state.fatal, .state.backoff { color: #cf222e; }
.error { padding: 12px; border: 1px solid #cf222e; border-radius: 6px; }
.error p { margin: 4px 0 8px; user-select: text; }
</style>
