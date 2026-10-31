<script setup lang="ts">
// "Validation": the checks Go ran on the text in the editor. Errors block
// saving, warnings do not; a line number jumps to that line in the editor.
import { computed } from 'vue'
import type { Check } from '../../bindings/github.com/touhidurabir/visor/internal/validate'

const props = defineProps<{ checks: Check[] | null; checking?: boolean; failed?: string | null }>()
const emit = defineEmits<{ goto: [line: number] }>()

const errors = computed(() => (props.checks ?? []).filter((c) => c.Level === 'error').length)
const warnings = computed(() => (props.checks ?? []).filter((c) => c.Level === 'warning').length)
const summary = computed(() => {
  if (props.failed) return 'Couldn’t run the checks'
  if (!props.checks) return 'Checking…'
  if (errors.value) return `${errors.value} ${errors.value === 1 ? 'error' : 'errors'} to fix`
  if (warnings.value) return `${warnings.value} ${warnings.value === 1 ? 'warning' : 'warnings'}`
  return 'All checks pass'
})
const MARK = { ok: '✓', warning: '⚠', error: '✗' } as const
const WORD = { ok: 'Passed', warning: 'Warning', error: 'Error' } as const
</script>

<template>
  <section class="card" aria-labelledby="checks-title">
    <div class="head">
      <strong id="checks-title">Validation</strong>
      <span :class="['sum', errors ? 'bad' : warnings ? 'warn' : 'good']" role="status">{{ summary }}</span>
    </div>
    <p v-if="failed" class="bad">{{ failed }}</p>
    <ul v-else-if="checks">
      <li v-for="(c, i) in checks" :key="i" :class="c.Level">
        <span class="mark" aria-hidden="true">{{ MARK[c.Level] }}</span>
        <span class="sr">{{ WORD[c.Level] }}: </span>
        <span class="msg">
          <button v-if="c.Line > 0" type="button" class="line" :aria-label="`Go to line ${c.Line}`" @click="emit('goto', c.Line)">line {{ c.Line }}</button>
          {{ c.Message }}
        </span>
      </li>
    </ul>
    <p v-else class="muted">Checking…</p>
  </section>
</template>

<style scoped>
.card { border: 1px solid var(--line); border-radius: 10px; padding: 16px; display: flex; flex-direction: column; gap: 10px; min-width: 0; }
.head { display: flex; flex-wrap: wrap; align-items: baseline; justify-content: space-between; gap: 8px; }
.sum { font-size: 12px; font-weight: 600; }
.sum.good { color: var(--running-fg); }
.sum.warn { color: var(--warning-fg); }
.sum.bad, p.bad { color: var(--failing-fg); }
p { margin: 0; }
.muted { color: var(--text-muted); }
ul { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 6px; }
li { display: flex; gap: 8px; }
.mark { flex: none; width: 14px; text-align: center; }
.msg { min-width: 0; overflow-wrap: anywhere; user-select: text; }
li.ok { color: var(--running-fg); }
li.warning { padding: 8px 10px; margin: 0 -2px; border-radius: var(--radius-sm); background: var(--warning-bg); color: var(--warning-fg); }
li.error { padding: 8px 10px; margin: 0 -2px; border-radius: var(--radius-sm); background: var(--failing-bg); color: var(--failing-fg); }
.line { padding: 0 6px; margin-right: 4px; border: 1px solid currentColor; border-radius: 999px; background: transparent; color: inherit; font: inherit; font-size: 11px; cursor: pointer; }
.line:hover { background: var(--bg-hover); color: var(--text); }
.sr { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); white-space: nowrap; }
</style>
