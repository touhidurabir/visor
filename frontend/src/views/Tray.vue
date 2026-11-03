<script setup lang="ts">
// The menu-bar popover (#/tray): every process grouped by config file, with
// one-click start/stop/restart, each config's Watch switch, Restart all, a
// config change check and a way into the main window. Kept live by the
// visor:overview event. The bell shows the unread count (visor:notifications)
// and opens the notifications, so a failure is noticed even when macOS shows
// no banner. Laid out as one section per server: this Mac for now; remote
// servers add sections.
import { computed, ref } from 'vue'
import { ServerService, ShellService, type ConfigView, type ProcessView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { errorMessage, useOverview } from '../lib/overview'
import { useApplyFlow } from '../lib/apply-flow'
import { formatDuration, shortSince } from '../lib/format'
import { useNotifications } from '../lib/notifications'

const { overview, error: loadError, reload } = useOverview()
const flow = useApplyFlow(() => reload())
const pending = flow.pending
const report = flow.report

// Full names with an action in flight, plus whole-popover actions.
const busy = ref(new Set<string>())
const restartingAll = ref(false)
// One inline message at a time: the latest action's failure or result.
const notice = ref<{ kind: 'error' | 'info'; text: string } | null>(null)

const server = computed(() => overview.value?.Server)

const { unread } = useNotifications()

// One section per connected server; only this Mac until remote servers.
const sections = computed(() => [
  { key: 'local', name: server.value?.Name ?? 'Local', health: server.value?.Health ?? 'unknown', failing: overview.value?.Stats.Failing ?? 0 },
])

function openNotifications() {
  void ShellService.OpenNotifications()
}
const stats = computed(() => overview.value?.Stats)
const configs = computed(() => (overview.value?.Configs ?? []).filter((c) => (c.Processes ?? []).length > 0))

const summary = computed(() => {
  const s = stats.value
  if (!s) return ''
  const parts = [`${s.Running} running`, `${s.Stopped} stopped`]
  if (server.value?.UptimeMs) parts.push(`supervisord up ${formatDuration(server.value.UptimeMs)}`)
  return parts.join(' · ')
})

// Rows sit under their config file, so the process name alone is enough
// (the full "group:name" is in the tooltip).
function label(p: ProcessView): string {
  return p.Name
}

function dotClass(p: ProcessView): string {
  if (p.Status === 'running' || p.Status === 'failing' || p.Status === 'stopped') return 'dot-' + p.Status
  return p.State === 'STARTING' || p.State === 'STOPPING' ? 'dot-warning' : 'dot-stopped'
}

const canStart = (p: ProcessView) => ['STOPPED', 'EXITED', 'FATAL'].includes(p.State)
const canStop = (p: ProcessView) => ['RUNNING', 'STARTING', 'BACKOFF'].includes(p.State)
const canRestart = (p: ProcessView) => p.State !== 'STOPPING'
const isBusy = (p: ProcessView) => restartingAll.value || busy.value.has(p.FullName)

type Verb = 'start' | 'stop' | 'restart'
const calls: Record<Verb, (name: string) => Promise<void>> = {
  start: (n) => ServerService.Start(n),
  stop: (n) => ServerService.Stop(n),
  restart: (n) => ServerService.Restart(n),
}

async function act(verb: Verb, p: ProcessView) {
  const name = p.FullName
  busy.value = new Set(busy.value).add(name)
  try {
    await calls[verb](name)
    if (notice.value?.kind === 'error') notice.value = null
  } catch (e) {
    notice.value = { kind: 'error', text: `Couldn't ${verb} ${label(p)}: ${errorMessage(e)}` }
  } finally {
    const next = new Set(busy.value)
    next.delete(name)
    busy.value = next
  }
}

async function restartAll() {
  restartingAll.value = true
  try {
    await ServerService.RestartAll()
    if (notice.value?.kind === 'error') notice.value = null
  } catch (e) {
    notice.value = { kind: 'error', text: `Couldn't restart all processes: ${errorMessage(e)}` }
  } finally {
    restartingAll.value = false
  }
}

// Configs with a Watch change in flight.
const watching = ref(new Set<string>())

async function toggleWatch(c: ConfigView) {
  const on = !c.Watched
  watching.value = new Set(watching.value).add(c.Path)
  try {
    await ServerService.SetWatch(c.Path, on)
    await reload()
  } catch (e) {
    notice.value = { kind: 'error', text: `Couldn't ${on ? 'watch' : 'stop watching'} ${c.FileName}: ${errorMessage(e)}` }
  } finally {
    const next = new Set(watching.value)
    next.delete(c.Path)
    watching.value = next
  }
}

function openVisor() {
  void ShellService.OpenVisor()
}
</script>

<template>
  <div class="popover">
    <header v-for="sec in sections" :key="sec.key" class="head">
      <div class="title-row">
        <span class="who">
          <span :class="['hdot', `h-${sec.health}`]" aria-hidden="true" />
          <strong class="title">{{ sec.name }}</strong>
          <span v-if="sec.failing > 0" class="failing">{{ sec.failing }} failing</span>
        </span>
        <button
          type="button"
          class="bell"
          data-testid="tray-bell"
          :aria-label="unread ? `Notifications, ${unread} unread` : 'Notifications'"
          :title="unread ? `${unread} unread` : 'Notifications'"
          @click="openNotifications"
        >
          <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9" /><path d="M10.3 21a1.94 1.94 0 0 0 3.4 0" />
          </svg>
          <span v-if="unread" class="badge" aria-hidden="true">{{ unread > 99 ? '99+' : unread }}</span>
        </button>
      </div>
      <span v-if="summary" class="sub">{{ summary }}</span>
    </header>

    <div class="list">
      <div v-if="loadError && !overview" class="panel" role="alert">
        <strong>Couldn't load your processes</strong>
        <p>{{ loadError }}</p>
        <button type="button" class="pbtn" @click="reload">Try again</button>
      </div>
      <p v-else-if="!overview" class="muted pad">Loading…</p>
      <div v-else-if="server?.Error" class="panel" role="alert">
        <strong>Can't reach supervisord</strong>
        <p>{{ server.Error }}</p>
        <p class="muted">Open Visor to see how to fix the connection.</p>
      </div>
      <p v-else-if="configs.length === 0" class="muted pad">No processes yet.</p>
      <section v-for="c in configs" :key="c.Path || c.FileName" :aria-label="c.FileName">
        <div class="file-row">
          <h2 class="file">{{ c.FileName }}</h2>
          <button
            v-if="c.Path"
            type="button"
            :class="['watch', { on: c.Watched }]"
            :aria-pressed="c.Watched"
            :aria-label="`Watch ${c.FileName}`"
            :title="c.Watched ? 'Visor notifies you about problems in this file. Click to stop.' : 'Visor stays quiet about this file. Click to watch it.'"
            :disabled="watching.has(c.Path)"
            @click="toggleWatch(c)"
          >
            <svg v-if="c.Watched" width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              <path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9" /><path d="M10.3 21a1.94 1.94 0 0 0 3.4 0" />
            </svg>
            <svg v-else width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              <path d="M8.7 3A6 6 0 0 1 18 8c0 2.7.4 4.7.9 6.1" /><path d="M17 17H3s3-2 3-9c0-.5.1-1 .2-1.5" /><path d="M10.3 21a1.94 1.94 0 0 0 3.4 0" /><path d="M2 2l20 20" />
            </svg>
            {{ c.Watched ? 'Watching' : 'Watch' }}
          </button>
        </div>
        <div v-for="p in c.Processes ?? []" :key="p.FullName" class="prow">
          <span :class="['dot', dotClass(p)]" aria-hidden="true" />
          <span class="name mono" :title="p.FullName">{{ label(p) }}</span>
          <span class="since" :title="p.State">{{ shortSince(p.Since) || p.State.toLowerCase() }}</span>
          <button
            type="button"
            class="ico"
            :aria-label="`Start ${label(p)}`"
            :title="`Start ${label(p)}`"
            :disabled="!canStart(p) || isBusy(p)"
            @click="act('start', p)"
          >
            <svg width="12" height="12" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M7 4v16l13-8z" /></svg>
          </button>
          <button
            type="button"
            class="ico"
            :aria-label="`Stop ${label(p)}`"
            :title="`Stop ${label(p)}`"
            :disabled="!canStop(p) || isBusy(p)"
            @click="act('stop', p)"
          >
            <svg width="11" height="11" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><rect x="5" y="5" width="14" height="14" rx="2" /></svg>
          </button>
          <button
            type="button"
            class="ico"
            :aria-label="`Restart ${label(p)}`"
            :title="`Restart ${label(p)}`"
            :disabled="!canRestart(p) || isBusy(p)"
            @click="act('restart', p)"
          >
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              <path d="M21 12a9 9 0 1 1-3-6.7L21 8" />
              <path d="M21 3v5h-5" />
            </svg>
          </button>
        </div>
      </section>
    </div>

    <div v-if="pending" class="notice info" role="alertdialog" aria-label="Apply config changes">
      <div class="body">
        <strong>{{ pending.title }}</strong>
        <span class="lines">{{ pending.message }}</span>
        <div class="row">
          <button type="button" class="pbtn" @click="flow.cancel">Cancel</button>
          <button type="button" class="pbtn strong" :disabled="flow.running.value" @click="flow.confirm">Apply</button>
        </div>
      </div>
    </div>
    <div v-else-if="report" :class="['notice', report.failed ? 'error' : 'info']" :role="report.failed ? 'alert' : 'status'">
      <div class="body">
        <span class="lines">{{ report.title }}</span>
        <span v-for="(e, i) in report.errors" :key="i" class="lines">{{ e }}</span>
      </div>
      <button type="button" class="ico close" aria-label="Dismiss" title="Dismiss" @click="flow.dismiss">
        <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" aria-hidden="true"><path d="M5 5l14 14M19 5L5 19" /></svg>
      </button>
    </div>
    <div v-if="notice" :class="['notice', notice.kind]" :role="notice.kind === 'error' ? 'alert' : 'status'">
      <span>{{ notice.text }}</span>
      <button type="button" class="ico close" aria-label="Dismiss" title="Dismiss" @click="notice = null">
        <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" aria-hidden="true"><path d="M5 5l14 14M19 5L5 19" /></svg>
      </button>
    </div>

    <footer class="foot">
      <div class="row">
        <button type="button" class="pbtn" :disabled="restartingAll || !overview || !!server?.Error" @click="restartAll">
          {{ restartingAll ? 'Restarting…' : 'Restart all' }}
        </button>
        <button type="button" class="pbtn" :disabled="flow.running.value || !!pending || !overview || !!server?.Error" @click="flow.applyAll">
          {{ flow.running.value ? 'Working…' : 'Apply config changes' }}
        </button>
      </div>
      <button type="button" class="pbtn strong" @click="openVisor">Open Visor…</button>
    </footer>
  </div>
</template>

<style scoped>
.popover {
  --panel: #ffffff;
  --line: #efeff2;
  --sub: #5e5e66;
  --icon: #45454c;
  --icon-off: #c2c2c8;
  --hover: #f2f2f5;
  --ico-hover: #e6e6ea;
  --btn: #ffffff;
  --btn-hover: #f5f5f7;
  --btn-line: #d2d2d7;
  --fail-text: #b42318;
  --fail-bg: #fdecea;
  --info-bg: #eef4fc;
  --accent: #0a5fcc;
  display: flex;
  flex-direction: column;
  height: 100vh;
  box-sizing: border-box;
  background: var(--panel);
  color: var(--text);
  overflow: hidden;
}
/* theme.ts sets data-theme on <html>; the popover is its own window. */
:root[data-theme='dark'] .popover {
  --panel: #1e1e20;
  --line: #2e2e32;
  --sub: #a1a1a6;
  --icon: #c7c7cc;
  --icon-off: #5a5a60;
  --hover: #2a2a2e;
  --ico-hover: #3a3a3f;
  --btn: #2c2c30;
  --btn-hover: #36363b;
  --btn-line: #48484e;
  --fail-text: #ff8a80;
  --fail-bg: #3b1d1b;
  --info-bg: #1b2a3d;
  --accent: #5aa0ff;
}

.head {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 12px 12px 10px;
  border-bottom: 1px solid var(--line);
}
.title-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.title { font-size: 14px; }
.who { display: inline-flex; align-items: center; gap: 8px; min-width: 0; }
.hdot { width: 8px; height: 8px; border-radius: 50%; background: var(--icon-off); flex-shrink: 0; }
.h-ok { background: #1e9e63; }
.h-warning { background: #d98a00; }
.h-error { background: #e5483b; }
.bell {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border: 0;
  border-radius: 7px;
  background: transparent;
  color: var(--icon);
  cursor: pointer;
}
.bell:hover { background: var(--ico-hover); }
.badge {
  position: absolute;
  top: -3px;
  right: -5px;
  min-width: 16px;
  height: 16px;
  padding: 0 4px;
  box-sizing: border-box;
  border-radius: 8px;
  background: #c8372b;
  color: #fff;
  font-size: 10px;
  font-weight: 700;
  line-height: 16px;
  text-align: center;
}
.failing { color: var(--fail-text); font-weight: 600; font-size: 12px; }
.sub, .muted { color: var(--sub); font-size: 12px; }
.list {
  flex: 1;
  min-height: 0;
  overflow: auto;
  padding: 4px 0;
}
.pad { padding: 8px 12px; margin: 0; }
.file-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 8px 12px 2px;
}
.file {
  margin: 0;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 11px;
  font-weight: 600;
  color: var(--sub);
}
.watch {
  display: inline-flex;
  flex: none;
  align-items: center;
  gap: 4px;
  height: 20px;
  padding: 0 7px;
  border: 1px solid var(--btn-line);
  border-radius: 999px;
  background: transparent;
  color: var(--sub);
  font: inherit;
  font-size: 11px;
  cursor: pointer;
}
.watch:hover:not(:disabled) { background: var(--ico-hover); }
.watch.on { border-color: var(--accent); color: var(--accent); }
.watch:disabled { opacity: 0.55; cursor: default; }
.prow {
  display: flex;
  align-items: center;
  gap: 8px;
  min-height: 30px;
  padding: 0 12px;
}
.prow:hover { background: var(--hover); }
.dot { flex: none; width: 8px; height: 8px; border-radius: 50%; }
.dot-running { background: var(--running-dot); }
.dot-failing { background: var(--failing-dot); }
.dot-warning { background: var(--warning-dot); }
.dot-stopped { background: var(--stopped-dot); }
.mono { font-family: ui-monospace, 'SF Mono', Menlo, monospace; font-size: 12px; }
.name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.since { color: var(--sub); font-size: 11px; white-space: nowrap; }
.ico {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: none;
  width: 26px;
  height: 26px;
  padding: 0;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: var(--icon);
  cursor: pointer;
}
.ico:hover:not(:disabled) { background: var(--ico-hover); }
.ico:disabled { color: var(--icon-off); cursor: default; }
.panel {
  margin: 8px 12px;
  padding: 10px 12px;
  border-radius: 8px;
  background: var(--fail-bg);
}
.panel p { margin: 4px 0 8px; font-size: 12px; user-select: text; }
.notice {
  display: flex;
  align-items: flex-start;
  gap: 6px;
  margin: 0 12px 8px;
  padding: 6px 4px 6px 10px;
  border-radius: 7px;
  font-size: 12px;
  user-select: text;
}
.notice span { flex: 1; padding-top: 4px; }
.notice .body { display: flex; flex: 1; flex-direction: column; gap: 4px; padding-top: 2px; min-width: 0; }
.notice .lines { white-space: pre-line; padding-top: 0; flex: none; }
.notice .body .row { margin-top: 4px; }
.notice.error { background: var(--fail-bg); color: var(--fail-text); }
.notice.info { background: var(--info-bg); }
.notice .close { width: 22px; height: 22px; color: inherit; }
.foot {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 10px 12px;
  border-top: 1px solid var(--line);
}
.row { display: flex; gap: 8px; }
.pbtn {
  display: inline-flex;
  flex: 1;
  align-items: center;
  justify-content: center;
  min-height: 30px;
  padding: 0 10px;
  border: 1px solid var(--btn-line);
  border-radius: 7px;
  background: var(--btn);
  color: var(--text);
  font: inherit;
  font-size: 12px;
  cursor: pointer;
}
.pbtn:hover:not(:disabled) { background: var(--btn-hover); }
.pbtn:disabled { opacity: 0.55; cursor: default; }
.pbtn.strong { font-weight: 600; }
.ico:focus-visible, .pbtn:focus-visible, .watch:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 1px;
}
</style>
