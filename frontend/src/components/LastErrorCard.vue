<script setup lang="ts">
// The most recent failure in a config (spec §5.3): what supervisord says,
// the end of its stderr, Restart / Open stderr log / Open in editor, and
// the config line it comes from when Visor can tell.
import { computed } from 'vue'
import type { LastErrorView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { stateLabel } from '../lib/state'

const props = defineProps<{ error: LastErrorView; busy?: boolean }>()
const emit = defineEmits<{ restart: []; openStderr: []; openConfig: []; showLine: [line: number] }>()

const name = computed(() => props.error.Process.split(':').pop() ?? props.error.Process)
const badge = computed(() => (props.error.Since ? `${stateLabel(props.error.State)} for ${props.error.Since}` : stateLabel(props.error.State)))
// Colour plus text: BACKOFF is still retrying, so it warns; FATAL and EXITED fail.
const tone = computed(() => (props.error.State === 'BACKOFF' ? 'warn' : 'bad'))
</script>

<template>
  <section :class="['card', `tone-${tone}`]" aria-labelledby="last-error-title">
    <div class="head">
      <h2 id="last-error-title">Last error · <span class="mono name" :title="error.Process">{{ name }}</span></h2>
      <span :class="['badge', `b-${tone}`]"><span :class="['dot', `d-${tone}`]" aria-hidden="true" />{{ badge }}</span>
    </div>
    <p class="msg">{{ error.Message }}</p>
    <pre v-if="error.StderrTail" class="tail" tabindex="0" aria-label="Last lines of stderr">{{ error.StderrTail }}</pre>
    <div class="acts">
      <button type="button" class="btn primary" :disabled="busy" @click="emit('restart')">Restart</button>
      <button
        type="button"
        class="btn quiet"
        :disabled="!error.StderrLog"
        :title="error.StderrLog || 'This process has no stderr log file'"
        @click="emit('openStderr')"
      >
        Open stderr log
      </button>
      <button type="button" class="btn quiet" @click="emit('openConfig')">Open in editor</button>
      <span v-if="error.Line" class="cause">
        Caused by <button type="button" class="link" @click="emit('showLine', error.Line)">line {{ error.Line }}</button>; the fix is below.
      </span>
    </div>
  </section>
</template>

<style scoped>
.card { display: flex; flex-direction: column; gap: 10px; padding: 16px 18px; border: 1px solid var(--bad-dot); border-radius: 10px; background: var(--raised); }
.card.tone-warn { border-color: var(--warn-dot); }
.head { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 8px 10px; }
h2 { margin: 0; font-size: 15px; font-weight: 600; }
.name { font-family: var(--mono); font-size: 14px; }
.msg { margin: 0; user-select: text; }
.tail {
  margin: 0;
  padding: 10px 14px;
  max-height: 260px;
  overflow: auto;
  background: var(--log-bg);
  color: var(--log-ink);
  border-radius: 8px;
  font-family: var(--mono);
  font-size: 12.5px;
  line-height: 1.6;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  user-select: text;
}
.acts { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
.cause { margin-left: 6px; color: var(--ink3); font-size: 12.5px; }
.link { padding: 0; border: 0; background: none; color: var(--accent-text); font: inherit; cursor: pointer; }
.link:hover { text-decoration: underline; }
</style>
