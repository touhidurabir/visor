<script setup lang="ts">
// "Paths in this config" (spec §5.3): working directories open in Finder,
// command binaries are revealed in Finder, log files open in their default
// app. Missing paths get a badge and no action.
import type { PathView } from '../../bindings/github.com/touhidurabir/visor/internal/app'

defineProps<{ paths: PathView[] }>()
const emit = defineEmits<{ open: [path: string]; reveal: [path: string] }>()

const ACTIONS: Record<string, { label: string; event: 'open' | 'reveal' }> = {
  dir: { label: 'Open in Finder', event: 'open' },
  bin: { label: 'Reveal in Finder', event: 'reveal' },
  log: { label: 'Open in editor', event: 'open' },
}
const action = (p: PathView) => ACTIONS[p.Kind] ?? ACTIONS.log

function act(p: PathView) {
  if (action(p).event === 'reveal') emit('reveal', p.Path)
  else emit('open', p.Path)
}
</script>

<template>
  <section class="card" aria-labelledby="paths-title">
    <strong id="paths-title">Paths in this config</strong>
    <p v-if="!paths.length" class="muted">This config names no folders, commands or log files.</p>
    <div v-for="p in paths" :key="p.Kind + p.Path" class="row">
      <div class="what">
        <div class="label">{{ p.Label }}</div>
        <div class="mono path" :title="p.Path">{{ p.Path }}</div>
      </div>
      <span v-if="!p.Exists" class="badge missing">missing</span>
      <button
        type="button"
        class="btn"
        :disabled="!p.Exists"
        :title="p.Exists ? undefined : p.Kind === 'bin' ? 'Visor can’t find this command' : 'This path doesn’t exist yet'"
        @click="act(p)"
      >
        {{ action(p).label }}
      </button>
    </div>
  </section>
</template>

<style scoped>
.card { border: 1px solid var(--line); border-radius: 10px; padding: 16px; display: flex; flex-direction: column; gap: 10px; }
.row { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; padding-top: 8px; border-top: 1px solid var(--stopped-bg); }
.what { flex: 1 1 240px; min-width: 0; }
.label { color: var(--text-muted); font-size: 12px; }
.mono { font-family: var(--font-mono); font-size: 12px; }
.path { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; user-select: text; }
.muted { margin: 0; color: var(--text-muted); }
.badge { display: inline-flex; align-items: center; padding: 2px 8px; border-radius: 999px; font-size: 12px; font-weight: 600; white-space: nowrap; }
.missing { background: var(--failing-bg); color: var(--failing-fg); }
.btn { display: inline-flex; align-items: center; min-height: 32px; padding: 0 12px; border-radius: var(--radius-sm); border: 1px solid var(--line-strong); background: var(--bg); color: var(--text); font: inherit; white-space: nowrap; cursor: pointer; }
.btn:hover:not(:disabled) { background: var(--bg-hover); }
.btn:disabled { color: var(--text-faint); cursor: default; }
</style>
