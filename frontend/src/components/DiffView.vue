<script setup lang="ts">
// A line diff, red for removed and green for added lines, long unchanged
// runs folded. The +/- marks keep it readable without colour.
import { computed } from 'vue'
import { diffLines, withContext, type DiffLine } from '../lib/diff'

const props = withDefaults(
  defineProps<{ from?: string; to?: string; lines?: DiffLine[]; context?: number; label?: string; empty?: string }>(),
  { from: '', to: '', context: 3, label: 'Changes', empty: 'No changes.' },
)

const rows = computed(() => withContext(props.lines ?? diffLines(props.from, props.to), props.context))
const changed = computed(() => rows.value.some((r) => r.kind === '+' || r.kind === '-'))
</script>

<template>
  <div class="diff" role="group" :aria-label="label">
    <p v-if="!changed" class="none">{{ empty }}</p>
    <pre v-else class="mono"><template v-for="(r, i) in rows" :key="i"><span v-if="r.kind === 'gap'" class="row gap">… {{ r.hidden }} unchanged {{ r.hidden === 1 ? 'line' : 'lines' }}</span><span v-else :class="['row', r.kind === '+' ? 'add' : r.kind === '-' ? 'del' : 'same']"><span class="mark" aria-hidden="true">{{ r.kind }}</span>{{ r.text || ' ' }}<span v-if="r.kind !== ' '" class="sr"> ({{ r.kind === '+' ? 'added' : 'removed' }})</span></span></template></pre>
  </div>
</template>

<style scoped>
.diff { min-width: 0; }
.none { margin: 0; color: var(--text-muted); }
pre { margin: 0; padding: 8px 0; border: 1px solid var(--line); border-radius: var(--radius-md); background: var(--bg); overflow: auto; line-height: 1.6; max-height: 360px; user-select: text; }
.mono { font-family: var(--font-mono); font-size: 12px; }
.row { display: block; padding: 0 12px 0 0; white-space: pre-wrap; overflow-wrap: anywhere; }
.mark { display: inline-block; width: 22px; text-align: center; color: var(--text-muted); user-select: none; }
.add { background: var(--running-bg); color: var(--running-fg); }
.del { background: var(--failing-bg); color: var(--failing-fg); }
.same { color: var(--text-muted); }
.gap { padding-left: 22px; color: var(--text-faint); font-style: italic; }
.sr { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); white-space: nowrap; }
</style>
