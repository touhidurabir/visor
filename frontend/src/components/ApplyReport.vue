<script setup lang="ts">
// The result of an apply or a remove: what happened, any per-group failures,
// and Undo after a remove.
import type { Report } from '../lib/apply-flow'

defineProps<{ report: Report; busy?: boolean }>()
const emit = defineEmits<{ undo: []; dismiss: [] }>()
</script>

<template>
  <div :class="['report', { failed: report.failed }]" :role="report.failed ? 'alert' : 'status'">
    <div class="top">
      <strong class="title">{{ report.title }}</strong>
      <button v-if="report.undo" type="button" class="btn sm" :disabled="busy" @click="emit('undo')">Undo</button>
      <button type="button" class="btn sm" @click="emit('dismiss')">Dismiss</button>
    </div>
    <ul v-if="report.errors.length">
      <li v-for="(e, i) in report.errors" :key="i">{{ e }}</li>
    </ul>
  </div>
</template>

<style scoped>
.report { padding: 10px 14px; border-radius: var(--radius-md); background: var(--running-bg); color: var(--running-fg); user-select: text; }
.report.failed { background: var(--failing-bg); color: var(--failing-fg); }
.top { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
.title { flex: 1 1 300px; font-weight: 600; }
ul { margin: 6px 0 0; padding-left: 18px; }
</style>
