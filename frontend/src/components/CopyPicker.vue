<script setup lang="ts">
// Add new → "Copy an existing config…": the configs of the include folder
// (from the live overview, as the dashboard lists them); choosing one drafts
// a copy under free names and opens it in Write it. Nothing is written.
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import type { ConfigView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { describeError } from '../lib/faults'
import { copyFrom, draft } from '../lib/new-config'
import { useOverview } from '../lib/overview'
import DialogShell from './DialogShell.vue'

const emit = defineEmits<{ close: [] }>()
const router = useRouter()
const { overview } = useOverview()

// Only real files: the overview's last row holds processes no file defines.
const configs = computed(() => (overview.value?.Configs ?? []).filter((c) => c.Path))
const busy = ref<string | null>(null)
const failure = ref<string | null>(null)
const replaces = draft.isDirty()

/** The config's program names, as the dashboard knows them. */
const programs = (c: ConfigView) => [...new Set((c.Processes ?? []).map((p) => p.Program).filter(Boolean))]

async function pick(c: ConfigView) {
  busy.value = c.Path
  failure.value = null
  try {
    await copyFrom(c.Path, c.FileName)
    emit('close')
    await router.push('/new')
  } catch (e) {
    failure.value = `Visor can’t copy ${c.FileName}. ${describeError(e)}`
  } finally {
    busy.value = null
  }
}
</script>

<template>
  <DialogShell title="Copy an existing config" @close="emit('close')">
    <p class="lead">Its programs get new names (with <span class="mono">-copy</span>) so both can run. Nothing is written until you save.</p>
    <p v-if="replaces" class="warn" role="status">This replaces what you have written so far.</p>
    <p v-if="!overview" class="muted">Loading…</p>
    <p v-else-if="!configs.length" class="muted">There are no configs in the include folder yet.</p>
    <ul v-else class="list" aria-label="Configs to copy">
      <li v-for="c in configs" :key="c.Path">
        <button type="button" class="item" :disabled="!!busy" @click="pick(c)">
          <span class="name mono">{{ c.FileName }}</span>
          <span class="meta">{{ busy === c.Path ? 'Copying…' : `${programs(c).length} ${programs(c).length === 1 ? 'program' : 'programs'}` }}</span>
          <span v-if="programs(c).length" class="summary mono">{{ programs(c).join(', ') }}</span>
        </button>
      </li>
    </ul>
    <p v-if="failure" class="bad" role="alert">{{ failure }}</p>
    <template #actions>
      <button type="button" class="btn" @click="emit('close')">Cancel</button>
    </template>
  </DialogShell>
</template>

<style scoped>
p { margin: 0; }
.lead { color: var(--ink2); line-height: 1.5; }
.muted { color: var(--ink2); }
.warn { color: var(--warn); }
.bad { color: var(--bad); }
.mono { font-family: var(--mono); font-size: 12.5px; }
.list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; border: 1px solid var(--line); border-radius: 10px; overflow: hidden; }
.list li + li { border-top: 1px solid var(--line); }
.item { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 2px 12px; width: 100%; padding: 10px 14px; border: 0; background: var(--raised); color: var(--ink); font: inherit; text-align: left; cursor: pointer; }
.item:hover:not(:disabled) { background: var(--hover); }
.item:disabled { cursor: default; }
.name { font-weight: 600; }
.meta { color: var(--ink3); font-size: 12.5px; }
.summary { grid-column: 1 / -1; color: var(--ink2); font-size: 12.5px; line-height: 1.45; }
</style>
