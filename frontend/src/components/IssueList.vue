<script setup lang="ts">
// Up to 10 open issues, newest first, each with Logs, a contextual action
// (Restart for a process that is down, Mark resolved otherwise) and Dismiss.
import { RouterLink, type RouteLocationRaw } from 'vue-router'
import type { IssueView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { duration } from '../lib/format'

defineProps<{ issues: IssueView[]; nowMs: number; logsTo: (i: IssueView) => RouteLocationRaw | null }>()
const emit = defineEmits<{ restart: [i: IssueView]; resolve: [i: IssueView]; dismiss: [i: IssueView] }>()

const KINDS: Record<string, { label: string; cls: string }> = {
  fatal: { label: 'FATAL', cls: 'failing' },
  unexpected_exit: { label: 'EXITED', cls: 'failing' },
  crash_loop: { label: 'CRASH LOOP', cls: 'warning' },
  unreachable: { label: 'UNREACHABLE', cls: 'failing' },
}
const kind = (i: IssueView) => KINDS[i.Kind] ?? { label: i.Kind.toUpperCase(), cls: 'stopped' }
const restartable = (i: IssueView) => i.Process !== '' && (i.Kind === 'fatal' || i.Kind === 'unexpected_exit')
</script>

<template>
  <div class="box">
    <p v-if="!issues.length" class="none">No open issues.</p>
    <div v-for="i in issues" :key="i.ID" class="issue">
      <span :class="['badge', kind(i).cls]"><span class="dot" aria-hidden="true" />{{ kind(i).label }}</span>
      <div class="body">
        <div>
          <span v-if="i.Process" class="proc">{{ i.Process }}</span>
          <span v-else class="proc">supervisord</span>
          <span v-if="i.Config" class="muted" :title="i.Config"> · {{ i.Config.split('/').pop() }}</span>
        </div>
        <div class="msg">{{ i.Message }}</div>
      </div>
      <span class="when">since {{ duration(nowMs - i.OpenedMs) }}</span>
      <span class="notified">{{ i.Notified ? 'Notified you' : 'Not notified' }}</span>
      <div class="acts">
        <RouterLink v-if="logsTo(i)" class="btn" :to="logsTo(i)!">Logs</RouterLink>
        <button v-if="restartable(i)" type="button" class="btn" @click="emit('restart', i)">Restart</button>
        <button v-else type="button" class="btn" @click="emit('resolve', i)">Mark resolved</button>
        <button type="button" class="btn" @click="emit('dismiss', i)">Dismiss</button>
      </div>
    </div>
    <div class="foot">Showing the 10 most recent. Older and resolved issues are on the Issues page.</div>
  </div>
</template>

<style scoped>
.box { border: 1px solid var(--line); border-radius: 10px; display: flex; flex-direction: column; }
.issue { display: flex; flex-wrap: wrap; align-items: center; gap: 12px; padding: 12px 14px; border-bottom: 1px solid var(--line); }
.none { margin: 0; padding: 12px 14px; border-bottom: 1px solid var(--line); color: var(--text-muted); }
.body { flex: 1 1 320px; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
.proc { font-family: var(--font-mono); font-size: 12px; font-weight: 600; }
.muted, .when, .notified, .foot { color: var(--text-muted); }
.msg { color: var(--stopped-fg); }
.when { min-width: 120px; }
.notified { min-width: 110px; font-size: 12px; }
.foot { padding: 10px 14px; }
.acts { display: flex; gap: 6px; }
.btn { display: inline-flex; align-items: center; min-height: 32px; padding: 0 12px; border-radius: var(--radius-sm); border: 1px solid var(--line-strong); background: var(--bg); color: var(--text); font: inherit; text-decoration: none; cursor: pointer; }
.btn:hover { background: var(--bg-hover); }
.badge { display: inline-flex; align-items: center; gap: 6px; padding: 2px 8px; border-radius: 999px; font-size: 12px; font-weight: 600; white-space: nowrap; }
.dot { width: 8px; height: 8px; border-radius: 50%; flex: none; }
.failing { background: var(--failing-bg); color: var(--failing-fg); }
.failing .dot { background: var(--failing-dot); }
.warning { background: var(--warning-bg); color: var(--warning-fg); }
.warning .dot { background: var(--warning-dot); }
.stopped { background: var(--stopped-bg); color: var(--stopped-fg); }
.stopped .dot { background: var(--stopped-dot); }
</style>
