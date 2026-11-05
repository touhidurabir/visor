<script setup lang="ts">
// Config files removed from supervisor that are not back, newest first, each
// with Restore (I-5): the way back once the report's Undo is gone. Hidden
// when there are none.
import { onMounted, ref, watch } from 'vue'
import { ConfigService } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { saveErrorText } from '../lib/config-errors'
import { removedRows, type RemovedRow } from '../lib/removed'
import { showToast } from '../lib/toast'

/** stamp: change it to load the list again (after a remove elsewhere). */
const props = defineProps<{ stamp: number }>()
const emit = defineEmits<{ restored: [] }>()

const rows = ref<RemovedRow[]>([])
const busy = ref<string | null>(null)

async function load() {
  try {
    rows.value = removedRows(await ConfigService.RemovedFiles(), Date.now())
  } catch {
    rows.value = [] // nothing to offer; the dashboard shows the connection's state
  }
}

onMounted(load)
watch(() => props.stamp, load)

async function restore(r: RemovedRow) {
  if (busy.value) return
  busy.value = r.path
  try {
    const res = await ConfigService.Restore(r.path, r.backupID)
    const failed = res.Errors?.length ?? 0
    showToast(failed ? `Restored ${r.fileName}, but ${failed} could not apply.` : `Restored ${r.fileName}.`, failed ? 'error' : 'info')
    emit('restored')
  } catch (e) {
    showToast(saveErrorText(e, r.fileName), 'error')
  } finally {
    busy.value = null
    await load()
  }
}
</script>

<template>
  <section v-if="rows.length" aria-labelledby="removed-h" class="removed">
    <h2 id="removed-h" class="label">Recently removed</h2>
    <ul>
      <li v-for="r in rows" :key="r.path">
        <span class="name mono">{{ r.fileName }}</span>
        <span class="meta">{{ r.when }} · <span class="mono">{{ r.folder }}</span></span>
        <button type="button" class="btn sm" :disabled="busy !== null" :aria-label="`Restore ${r.fileName}`" @click="restore(r)">
          {{ busy === r.path ? 'Restoring…' : 'Restore' }}
        </button>
      </li>
    </ul>
  </section>
</template>

<style scoped>
.removed { display: flex; flex-direction: column; gap: 8px; }
.label { margin: 0; font-size: 11px; font-weight: 600; letter-spacing: 0.04em; text-transform: uppercase; color: var(--text-muted); }
ul { margin: 0; padding: 0; list-style: none; border: 1px solid var(--line); border-radius: var(--radius-md); }
li { display: flex; flex-wrap: wrap; align-items: center; gap: 4px 12px; padding: 6px 12px; }
li + li { border-top: 1px solid var(--line); }
.name { font-weight: 600; }
.meta { flex: 1 1 200px; color: var(--text-muted); min-width: 0; overflow-wrap: anywhere; }
.mono { font-family: var(--font-mono); font-size: 12px; }
</style>
