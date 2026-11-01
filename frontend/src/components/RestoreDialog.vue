<script setup lang="ts">
// "Restore a backup…": every copy Visor saved before a write (newest first),
// each with the change restoring it would make to the file as it is now.
// Restoring goes through the same checks, backup and apply as Save.
import { computed, ref } from 'vue'
import type { BackupView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { diffLines, diffStats } from '../lib/diff'
import { historyTime } from '../lib/format'
import DialogShell from './DialogShell.vue'
import DiffView from './DiffView.vue'

const props = defineProps<{ fileName: string; backups: BackupView[]; current: string | null; dirty: boolean; busy: boolean; writable: boolean }>()
const emit = defineEmits<{ restore: [id: string]; close: [] }>()

const pickedId = ref<string>(props.backups[0]?.ID ?? '')
const picked = computed(() => props.backups.find((b) => b.ID === pickedId.value))
const now = Date.now()
const label = (b: BackupView) => historyTime(new Date(b.At).getTime(), now)
const lines = computed(() => (picked.value ? diffLines(props.current ?? '', picked.value.Content) : []))
const stats = (b: BackupView) => diffStats(diffLines(props.current ?? '', b.Content))
</script>

<template>
  <DialogShell :title="`Restore a backup of ${fileName}`" wide @close="emit('close')">
    <p v-if="!backups.length" class="muted">
      There are no backups of this file yet. Visor saves one before every change it makes, so the first appears after your next save.
    </p>
    <div v-else class="cols">
      <ul class="list" role="listbox" aria-label="Backups, newest first">
        <li v-for="b in backups" :key="b.ID" role="presentation">
          <button
            type="button"
            role="option"
            :aria-selected="b.ID === pickedId"
            :class="['item', { on: b.ID === pickedId }]"
            @click="pickedId = b.ID"
          >
            <span class="when">{{ label(b) }}</span>
            <span v-if="b.Removed" class="tag">removed from supervisor</span>
            <span v-else-if="current !== null" class="stat">+{{ stats(b).added }} −{{ stats(b).removed }}</span>
          </button>
        </li>
      </ul>
      <div class="detail">
        <template v-if="picked">
          <strong>Restoring this version changes the file like this</strong>
          <DiffView :lines="lines" label="Changes from restoring this backup" empty="This backup is the same as the file now." />
          <p v-if="dirty" class="note">Your unsaved edits in the editor are replaced by the restored version.</p>
        </template>
      </div>
    </div>
    <template #actions>
      <button type="button" class="btn" @click="emit('close')">Cancel</button>
      <button
        v-if="backups.length"
        type="button"
        class="btn primary"
        :disabled="busy || !picked || !writable"
        :title="writable ? undefined : 'Visor can’t write this file'"
        @click="picked && emit('restore', picked.ID)"
      >
        {{ busy ? 'Restoring…' : 'Restore this version' }}
      </button>
    </template>
  </DialogShell>
</template>

<style scoped>
.muted, .note { margin: 0; color: var(--text-muted); line-height: 1.45; }
.note { color: var(--warning-fg); }
.cols { display: grid; grid-template-columns: minmax(180px, 220px) minmax(0, 1fr); gap: 16px; align-items: start; }
.list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 4px; max-height: 360px; overflow: auto; }
.item { display: flex; flex-direction: column; gap: 2px; width: 100%; padding: 8px 10px; text-align: left; border: 1px solid transparent; border-radius: var(--radius-sm); background: transparent; color: var(--text); font: inherit; cursor: pointer; }
.item:hover { background: var(--bg-hover); }
.item.on, .item.on:hover { background: var(--selected-bg); border-color: var(--accent-text); }
.when { font-weight: 600; }
.stat, .tag { font-size: 12px; color: var(--text-muted); font-family: var(--font-mono); }
.tag { font-family: inherit; color: var(--warning-fg); }
.detail { display: flex; flex-direction: column; gap: 8px; min-width: 0; }
.btn { min-height: 32px; padding: 0 14px; border-radius: var(--radius-sm); border: 1px solid var(--line-strong); background: var(--bg); color: var(--text); font: inherit; cursor: pointer; }
.btn:hover:not(:disabled) { background: var(--bg-hover); }
.btn:disabled { color: var(--text-faint); cursor: default; }
.btn.primary { background: var(--accent); border-color: var(--accent); color: var(--on-accent); font-weight: 600; }
.btn.primary:hover:not(:disabled) { background: var(--accent-hover); border-color: var(--accent-hover); }
.btn.primary:disabled { opacity: 0.6; color: var(--on-accent); }
@media (max-width: 640px) { .cols { grid-template-columns: 1fr; } }
</style>
