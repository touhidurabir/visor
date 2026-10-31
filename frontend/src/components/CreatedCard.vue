<script setup lang="ts">
// The result of "Save & apply" for a new config, with a link to open it.
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import type { ApplyResultView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { configRoute } from '../lib/links'

const props = defineProps<{ path: string; fileName: string; result: ApplyResultView }>()
defineEmits<{ open: [] }>()

const problems = computed(() => props.result.Errors ?? [])
const added = computed(() => (props.result.Added ?? []).join(', '))
</script>

<template>
  <section class="card" :class="problems.length ? 'bad' : 'ok'" aria-labelledby="created-title">
    <strong id="created-title">{{ problems.length ? 'Saved, with problems' : 'Saved and applied' }}</strong>
    <div class="mono path">{{ path }}</div>
    <div v-if="added">
      supervisord added <span class="mono">{{ added }}</span>.
      <template v-if="!problems.length">Its processes are starting, and Visor will tell you if one fails.</template>
    </div>
    <div v-else-if="!problems.length">supervisord already knew about it, so nothing needed to change.</div>
    <ul v-if="problems.length" class="plain">
      <li v-for="e in problems" :key="e">{{ e }}</li>
    </ul>
    <p v-if="problems.length" class="muted">The file is saved. Open it to fix the problem and save again.</p>
    <div><RouterLink class="btn" :to="configRoute(path)" @click="$emit('open')">Open it</RouterLink></div>
  </section>
</template>

<style scoped>
.card { border: 1px solid var(--line); border-radius: 10px; padding: 16px; display: flex; flex-direction: column; gap: 10px; min-width: 0; }
.card.ok { border-color: var(--watch-on-border); background: var(--running-bg); }
.card.bad { border-color: var(--error-card-border); background: var(--error-card-bg); }
.mono { font-family: var(--font-mono); font-size: 12px; }
.path { overflow-wrap: anywhere; user-select: text; }
.plain { margin: 0; padding-left: 18px; color: var(--failing-fg); user-select: text; }
.muted { margin: 0; color: var(--text-muted); }
.btn { display: inline-flex; align-items: center; min-height: 32px; padding: 0 12px; border-radius: var(--radius-sm); border: 1px solid var(--line-strong); background: var(--bg); color: var(--text); font: inherit; text-decoration: none; box-sizing: border-box; }
.btn:hover { background: var(--bg-hover); }
</style>
