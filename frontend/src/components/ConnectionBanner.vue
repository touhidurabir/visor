<script setup lang="ts">
// Shown on the dashboard while Visor has no connection to supervisord: a
// plain title and line (lib/connection.ts), Open Doctor and Try again, and
// the raw error under "Show details". Try again asks the dashboard to
// reconnect; this component only reports the click.
import { RouterLink } from 'vue-router'
import type { ConnectionProblem } from '../lib/connection'

defineProps<{ problem: ConnectionProblem; busy?: boolean }>()
const emit = defineEmits<{ retry: [] }>()
</script>

<template>
  <div class="down" role="alert" aria-labelledby="down-title">
    <span class="mark" aria-hidden="true">
      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round">
        <path d="M12 3v8" /><path d="M6.3 7.3a8 8 0 1 0 11.4 0" />
      </svg>
    </span>
    <div class="body">
      <div class="words">
        <strong id="down-title" class="title">{{ problem.title }}</strong>
        <span class="line"
          ><span v-for="(seg, i) in problem.line" :key="i" :class="{ mono: seg.mono }">{{ seg.text }}</span></span
        >
      </div>
      <div class="acts">
        <RouterLink class="btn primary" to="/doctor">Open Doctor</RouterLink>
        <button type="button" class="btn" :disabled="busy" @click="emit('retry')">{{ busy ? 'Trying…' : 'Try again' }}</button>
        <span v-if="problem.retrying" class="meta">Visor keeps checking in the background.</span>
      </div>
      <details v-if="problem.raw">
        <summary class="meta">Show details</summary>
        <div class="raw" data-testid="connection-error">{{ problem.raw }}</div>
      </details>
    </div>
  </div>
</template>

<style scoped>
/* A div, not a section: the dashboard lays its own sections out as columns. */
.down { display: flex; gap: 14px; padding: 18px 20px; border-radius: 10px; background: var(--bad-soft); color: var(--ink); }
.mark { display: flex; flex: none; align-items: center; justify-content: center; width: 36px; height: 36px; border-radius: 50%; background: var(--raised); color: var(--bad); }
.body { display: flex; flex: 1; flex-direction: column; gap: 10px; min-width: 0; }
.words { display: flex; flex-direction: column; gap: 2px; }
.title { font-size: 16px; font-weight: 600; }
.line { color: var(--ink2); }
.mono { font-family: var(--mono); font-size: 12.5px; }
.acts { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
.meta { color: var(--ink3); font-size: 12.5px; }
summary { width: max-content; cursor: pointer; }
.raw {
  margin-top: 8px;
  padding: 10px 12px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--sunken);
  color: var(--ink);
  font-family: var(--mono);
  font-size: 12.5px;
  line-height: 1.6;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  user-select: text;
}
</style>
