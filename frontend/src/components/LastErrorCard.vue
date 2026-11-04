<script setup lang="ts">
// The most recent failure in a config (spec §5.3): what supervisord says,
// the end of its stderr, Restart / Open stderr log / Open in editor, and
// the config line it comes from when Visor can tell.
import { computed } from 'vue'
import type { LastErrorView } from '../../bindings/github.com/touhidurabir/visor/internal/app'

const props = defineProps<{ error: LastErrorView; busy?: boolean }>()
const emit = defineEmits<{ restart: []; openStderr: []; openConfig: []; showLine: [line: number] }>()

const name = computed(() => props.error.Process.split(':').pop() ?? props.error.Process)
const badge = computed(() => (props.error.Since ? `${props.error.State} for ${props.error.Since}` : props.error.State))
// Colour plus text: BACKOFF is still retrying, so it warns; FATAL and EXITED fail.
const tone = computed(() => (props.error.State === 'BACKOFF' ? 'warning' : 'failing'))
</script>

<template>
  <section class="card error-card" aria-labelledby="last-error-title">
    <div class="head">
      <strong id="last-error-title">Last error · <span class="mono" :title="error.Process">{{ name }}</span></strong>
      <span :class="['badge', tone]"><span class="dot" aria-hidden="true" />{{ badge }}</span>
    </div>
    <div>{{ error.Message }}</div>
    <pre v-if="error.StderrTail" class="tail" tabindex="0" aria-label="Last lines of stderr">{{ error.StderrTail }}</pre>
    <div class="acts">
      <button type="button" class="btn primary" :disabled="busy" @click="emit('restart')">Restart</button>
      <button
        type="button"
        class="btn"
        :disabled="!error.StderrLog"
        :title="error.StderrLog || 'This process has no stderr log file'"
        @click="emit('openStderr')"
      >
        Open stderr log
      </button>
      <button type="button" class="btn" @click="emit('openConfig')">Open in editor</button>
      <span v-if="error.Line" class="cause">
        Caused by <button type="button" class="link" @click="emit('showLine', error.Line)">line {{ error.Line }}</button>, see the fix below.
      </span>
    </div>
  </section>
</template>

<style scoped>
.card { border: 1px solid var(--line); border-radius: 10px; padding: 16px; display: flex; flex-direction: column; gap: 10px; }
.error-card { border-color: var(--error-card-border); background: var(--error-card-bg); }
.head { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 8px; }
.failing { background: var(--failing-bg); color: var(--failing-fg); }
.failing .dot { background: var(--failing-dot); }
.warning { background: var(--warning-bg); color: var(--warning-fg); }
.warning .dot { background: var(--warning-dot); }
.mono { font-family: var(--font-mono); font-size: 12px; }
.tail {
  margin: 0;
  padding: 10px;
  max-height: 260px;
  overflow: auto;
  background: var(--log-bg);
  color: var(--log-fg);
  border-radius: var(--radius-sm);
  font-family: var(--font-mono);
  font-size: 12px;
  white-space: pre-wrap;
  word-break: break-word;
  user-select: text;
}
.acts { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
.cause { margin-left: 4px; color: var(--text-muted); }
.link { padding: 0; border: 0; background: none; color: var(--accent-text); font: inherit; text-decoration: underline; cursor: pointer; }
</style>
