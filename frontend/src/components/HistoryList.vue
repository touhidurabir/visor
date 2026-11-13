<script setup lang="ts">
// State changes of one process, newest first, with the time and the reason
// (spec §5.3 History tab). `version` changes whenever the process's state
// does, so the list reloads.
import { ref, watch } from 'vue'
import { type TransitionView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { IssueService } from '../lib/api'
import { describeError } from '../lib/faults'
import { historyNote, historyTime } from '../lib/format'
import StateBadge from './StateBadge.vue'

const props = defineProps<{ process: string; version: string; nowMs: number }>()

const LIMIT = 200
const rows = ref<TransitionView[] | null>(null)
const error = ref<string | null>(null)
let gen = 0

async function load() {
  const my = ++gen
  try {
    const list = await IssueService.History(props.process, LIMIT)
    if (my !== gen) return
    rows.value = list ?? []
    error.value = null
  } catch (e) {
    if (my === gen) error.value = describeError(e)
  }
}

watch(() => props.process, () => {
  rows.value = null
  void load()
})
watch(() => props.version, () => void load())
void load()
</script>

<template>
  <p v-if="error" class="err" role="alert">Couldn’t load the history. {{ error }}</p>
  <p v-else-if="rows === null" class="muted">Loading…</p>
  <p v-else-if="!rows.length" class="muted">
    No state changes recorded for <span class="mono">{{ process }}</span> yet. Visor records them while it runs.
  </p>
  <div v-else class="card">
    <div v-for="(t, i) in rows" :key="t.AtMs + '-' + i" class="row">
      <span class="at">{{ historyTime(t.AtMs, nowMs) }}</span>
      <StateBadge :state="t.To" />
      <span class="note">{{ historyNote(t) }}</span>
    </div>
    <div v-if="rows.length === LIMIT" class="more">Showing the {{ LIMIT }} most recent changes.</div>
  </div>
</template>

<style scoped>
.card { border: 1px solid var(--line); border-radius: 10px; display: flex; flex-direction: column; }
.row { display: flex; flex-wrap: wrap; align-items: center; gap: 14px; padding: 11px 16px; border-bottom: 1px solid var(--stopped-bg); }
.row:last-child { border-bottom: 0; }
.at { width: 130px; color: var(--text-muted); }
.note { color: var(--stopped-fg); user-select: text; }
.more { padding: 10px 16px; color: var(--text-muted); border-top: 1px solid var(--stopped-bg); }
.muted { margin: 0; color: var(--text-muted); }
.err { margin: 0; color: var(--failing-fg); }
.mono { font-family: var(--font-mono); font-size: 12px; }
</style>
