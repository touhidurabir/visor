<script setup lang="ts">
// Colour plus a text label, never colour alone (spec §9). `status` is the
// Go side's running/failing/stopped classification; without it the state
// name decides.
import { computed } from 'vue'

const props = defineProps<{ state: string; status?: string }>()

const tone = computed(() => {
  if (['STARTING', 'BACKOFF', 'STOPPING'].includes(props.state)) return 'warning'
  if (props.status === 'running' || props.status === 'failing' || props.status === 'stopped') return props.status
  if (props.state === 'RUNNING') return 'running'
  if (props.state === 'FATAL') return 'failing'
  return 'stopped'
})
</script>

<template>
  <span :class="['badge', tone]"><span class="dot" aria-hidden="true" />{{ state }}</span>
</template>

<style scoped>
.badge {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 2px 8px;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 600;
  white-space: nowrap;
}
.dot { width: 8px; height: 8px; border-radius: 50%; flex: none; }
.running { background: var(--running-bg); color: var(--running-fg); }
.running .dot { background: var(--running-dot); }
.failing { background: var(--failing-bg); color: var(--failing-fg); }
.failing .dot { background: var(--failing-dot); }
.warning { background: var(--warning-bg); color: var(--warning-fg); }
.warning .dot { background: var(--warning-dot); }
.stopped { background: var(--stopped-bg); color: var(--stopped-fg); }
.stopped .dot { background: var(--stopped-dot); }
</style>
