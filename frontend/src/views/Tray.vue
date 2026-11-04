<script setup lang="ts">
// The menu-bar popover (#/tray). Failing processes come first, in a "Needs
// attention" group; the rest follow under their config file, each with its
// Watch switch (lib/tray.ts). A row shows only the actions that apply to
// its state. The footer has Restart all, a config change check and a way
// into the main window. Kept live by the visor:overview event. The bell
// shows the unread count (visor:notifications) and opens the notifications,
// so a failure is noticed even when macOS shows no banner. Laid out as one
// section per server: this Mac for now; remote servers add sections.
import { computed, ref } from 'vue'
import { ServerService, ShellService, type ConfigView, type ProcessView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { errorMessage, useOverview } from '../lib/overview'
import { useApplyFlow } from '../lib/apply-flow'
import { connectionProblem } from '../lib/connection'
import { useNotifications } from '../lib/notifications'
import { stateLabel } from '../lib/state'
import { VERB_LABEL, downLine, footerReason, rowLook, rowName, trayActions, trayGroups, trayHeader, type TrayVerb } from '../lib/tray'

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
// Not connected: what to say (never "isn't running" for a password or
// permission problem), and why the supervisord actions are off.
const problem = computed(() => connectionProblem(server.value))
const offline = computed(() => problem.value?.reason)

const { unread } = useNotifications()

// One section per connected server; only this Mac until remote servers.
const sections = computed(() => [
  { key: 'local', name: server.value?.Name ?? 'Local', ...trayHeader(server.value, overview.value?.Stats, problem.value) },
])

const groups = computed(() =>
  trayGroups(overview.value?.Configs ?? []).map((g) => ({
    ...g,
    rows: g.processes.map((p) => ({ p, label: rowName(g, p), look: rowLook(p), actions: trayActions(p.State) })),
  })),
)

function openNotifications() {
  void ShellService.OpenNotifications()
}

const isBusy = (p: ProcessView) => restartingAll.value || busy.value.has(p.FullName)

const calls: Record<TrayVerb, (name: string) => Promise<void>> = {
  start: (n) => ServerService.Start(n),
  stop: (n) => ServerService.Stop(n),
  restart: (n) => ServerService.Restart(n),
}

async function act(verb: TrayVerb, p: ProcessView, label: string) {
  const name = p.FullName
  busy.value = new Set(busy.value).add(name)
  try {
    await calls[verb](name)
    if (notice.value?.kind === 'error') notice.value = null
  } catch (e) {
    notice.value = { kind: 'error', text: `Couldn't ${verb} ${label}: ${errorMessage(e)}` }
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

/** Try again: reconnects as the dashboard's banner does, and says so when nothing changed. */
const retrying = ref(false)
async function tryAgain() {
  if (retrying.value) return
  retrying.value = true
  try {
    const ov = await ServerService.Reconnect()
    overview.value = ov
    const still = connectionProblem(ov.Server)
    notice.value = still ? { kind: 'info', text: `Still not connected: ${still.title}.` } : null
  } catch (e) {
    notice.value = { kind: 'error', text: `Couldn't reconnect: ${errorMessage(e)}` }
  } finally {
    retrying.value = false
  }
}

/** Shows the main window at Doctor (Go's ShellService.OpenDoctor). */
async function openDoctor() {
  try {
    await ShellService.OpenDoctor()
  } catch (e) {
    notice.value = { kind: 'error', text: `Couldn't open Doctor: ${errorMessage(e)}` }
  }
}

function openVisor() {
  void ShellService.OpenVisor()
}
</script>

<template>
  <div class="popover">
    <header v-for="sec in sections" :key="sec.key" class="head">
      <div class="who">
        <div class="title-row">
          <span :class="['dot', `d-${sec.tone}`]" aria-hidden="true" />
          <strong class="title">{{ sec.name }}</strong>
          <span v-if="sec.badge" class="badge b-bad">{{ sec.badge }}</span>
        </div>
        <span v-if="sec.meta" class="meta">{{ sec.meta }}</span>
      </div>
      <button
        type="button"
        class="btn quiet icon bell"
        data-testid="tray-bell"
        :aria-label="unread ? `Notifications, ${unread} unread` : 'Notifications'"
        :title="unread ? `${unread} unread` : 'Notifications'"
        @click="openNotifications"
      >
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9" /><path d="M10.3 21a1.94 1.94 0 0 0 3.4 0" />
        </svg>
        <span v-if="unread" class="count" aria-hidden="true">{{ unread > 99 ? '99+' : unread }}</span>
      </button>
    </header>

    <div class="list">
      <div v-if="loadError && !overview" class="card" role="alert">
        <strong class="card-title">Couldn't load your processes</strong>
        <span class="raw">{{ loadError }}</span>
        <div class="acts"><button type="button" class="btn sm" @click="reload">Try again</button></div>
      </div>
      <p v-else-if="!overview" class="meta pad">Loading…</p>
      <div v-else-if="problem" class="card" role="alert" aria-labelledby="tray-down-title">
        <strong id="tray-down-title" class="card-title">{{ problem.title }}</strong>
        <span class="muted">{{ downLine(problem) }}</span>
        <div class="acts">
          <button type="button" class="btn primary sm" @click="openDoctor">Open Doctor</button>
          <button type="button" class="btn sm" :disabled="retrying" @click="tryAgain">{{ retrying ? 'Trying…' : 'Try again' }}</button>
        </div>
        <details v-if="problem.raw">
          <summary class="meta">Show details</summary>
          <div class="raw code" data-testid="connection-error">{{ problem.raw }}</div>
        </details>
      </div>
      <p v-else-if="groups.length === 0" class="meta pad">No processes yet.</p>
      <section v-for="g in problem ? [] : groups" :key="g.key" :aria-label="g.name" class="group">
        <div class="group-head">
          <h2 :class="['gname', { 't-bad': !g.config }]">{{ g.name }}</h2>
          <button
            v-if="g.config?.Path"
            type="button"
            :class="['btn quiet sm watch', { on: g.config.Watched }]"
            :aria-pressed="g.config.Watched"
            :aria-label="`Watch ${g.config.FileName}`"
            :title="g.config.Watched ? 'Visor notifies you about problems in this file. Click to stop.' : 'Visor stays quiet about this file. Click to watch it.'"
            :disabled="watching.has(g.config.Path)"
            @click="toggleWatch(g.config)"
          >
            <svg v-if="g.config.Watched" width="13" height="13" viewBox="0 0 24 24" fill="currentColor" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              <path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9" /><path d="M10.3 21a1.94 1.94 0 0 0 3.4 0" fill="none" />
            </svg>
            <svg v-else width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              <path d="M8.7 3A6 6 0 0 1 18 8c0 2.7.4 4.7.9 6.1" /><path d="M17 17H3s3-2 3-9c0-.5.1-1 .2-1.5" /><path d="M10.3 21a1.94 1.94 0 0 0 3.4 0" /><path d="M2 2l20 20" />
            </svg>
            {{ g.config.Watched ? 'Watching' : 'Watch' }}
          </button>
        </div>
        <p v-if="!g.rows.length && g.moved" class="meta moved">Listed under Needs attention</p>
        <div v-for="{ p, label, look, actions } in g.rows" :key="p.FullName" :class="['prow', { bad: look.failing }]" data-testid="tray-row">
          <span :class="['dot', `d-${look.tone}`]" aria-hidden="true" />
          <span class="name" :title="p.FullName">{{ label }}</span>
          <span :class="['time', look.timeClass]" :title="stateLabel(p.State)" data-testid="tray-time">{{ look.time }}</span>
          <span class="acts-row">
            <button
              v-for="v in actions"
              :key="v"
              type="button"
              class="btn quiet icon sm act"
              :aria-label="`${VERB_LABEL[v]} ${label}`"
              :title="`${VERB_LABEL[v]} ${label}`"
              :disabled="isBusy(p)"
              @click="act(v, p, label)"
            >
              <svg v-if="v === 'start'" width="16" height="16" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M7 4.5v15l12.5-7.5z" /></svg>
              <svg v-else-if="v === 'stop'" width="16" height="16" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><rect x="5.5" y="5.5" width="13" height="13" rx="2" /></svg>
              <svg v-else width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                <path d="M20 12a8 8 0 1 1-2.3-5.7" /><path d="M20 4v5h-5" />
              </svg>
            </button>
          </span>
        </div>
      </section>
    </div>

    <div v-if="pending" class="notice info" role="alertdialog" aria-label="Apply config changes">
      <div class="body">
        <strong>{{ pending.title }}</strong>
        <span class="lines">{{ pending.message }}</span>
        <div class="acts">
          <button type="button" class="btn sm" @click="flow.cancel">Cancel</button>
          <button type="button" class="btn primary sm" :disabled="flow.running.value" @click="flow.confirm">Apply</button>
        </div>
      </div>
    </div>
    <div v-else-if="report" :class="['notice', report.failed ? 'error' : 'info']" :role="report.failed ? 'alert' : 'status'">
      <div class="body">
        <span class="lines">{{ report.title }}</span>
        <span v-for="(e, i) in report.errors" :key="i" class="lines">{{ e }}</span>
      </div>
      <button type="button" class="btn quiet icon sm close" aria-label="Dismiss" title="Dismiss" @click="flow.dismiss">
        <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" aria-hidden="true"><path d="M5 5l14 14M19 5L5 19" /></svg>
      </button>
    </div>
    <div v-if="notice" :class="['notice', notice.kind]" :role="notice.kind === 'error' ? 'alert' : 'status'">
      <span class="body lines">{{ notice.text }}</span>
      <button type="button" class="btn quiet icon sm close" aria-label="Dismiss" title="Dismiss" @click="notice = null">
        <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" aria-hidden="true"><path d="M5 5l14 14M19 5L5 19" /></svg>
      </button>
    </div>

    <footer class="foot">
      <div class="row">
        <button type="button" class="btn sm" :disabled="restartingAll || !overview || !!problem" :title="offline" @click="restartAll">
          {{ restartingAll ? 'Restarting…' : 'Restart all' }}
        </button>
        <button type="button" class="btn sm" :disabled="flow.running.value || !!pending || !overview || !!problem" :title="offline" @click="flow.applyAll">
          {{ flow.running.value ? 'Working…' : 'Apply config changes' }}
        </button>
      </div>
      <span v-if="problem" class="meta reason" data-testid="tray-offline">{{ footerReason(problem) }}</span>
      <button type="button" class="open" @click="openVisor">
        Open Visor…
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M7 17 17 7M9 7h8v8" /></svg>
      </button>
    </footer>
  </div>
</template>

<style scoped>
/* theme.ts sets data-theme on <html>; the popover is its own window, and
   the tokens follow it. */
.popover {
  display: flex;
  flex-direction: column;
  height: 100vh;
  box-sizing: border-box;
  background: var(--raised);
  color: var(--ink);
  overflow: hidden;
}
.meta { color: var(--ink3); font-size: 12.5px; }
.muted { color: var(--ink2); }

.head {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 12px 12px 10px 14px;
  border-bottom: 1px solid var(--line);
}
.who { display: flex; flex: 1; flex-direction: column; gap: 2px; min-width: 0; }
.title-row { display: flex; align-items: center; gap: 8px; min-width: 0; }
.title { font-size: 14px; }
.title-row .badge { height: 20px; }
.bell { position: relative; flex: none; }
.count {
  position: absolute;
  top: 0;
  right: -2px;
  min-width: 17px;
  height: 17px;
  padding: 0 4px;
  box-sizing: border-box;
  border: 2px solid var(--raised);
  border-radius: 9px;
  background: var(--danger-bg);
  color: var(--on-accent);
  font-size: 10px;
  font-weight: 700;
  line-height: 13px;
  text-align: center;
}

.list {
  flex: 1;
  min-height: 0;
  overflow: auto;
  padding: 6px 6px 8px;
}
.pad { margin: 0; padding: 8px; }
.group-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  min-height: var(--control-sm);
  padding: 6px 0 1px 8px;
}
.gname {
  margin: 0;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--ink3);
  font-size: 12px;
  font-weight: 600;
}
.gname.t-bad { color: var(--bad); }
.watch { flex: none; gap: 4px; padding: 0 7px; color: var(--ink3); font-size: 12px; }
.watch.on, .watch.on:hover:not(:disabled) { color: var(--accent-text); }
.moved { margin: 0; padding: 2px 8px 6px; font-size: 12px; }
.prow {
  display: flex;
  align-items: center;
  gap: 8px;
  height: 32px;
  padding: 0 4px 0 8px;
  border-radius: 6px;
}
.prow:hover { background: var(--hover); }
.prow.bad { background: var(--bad-soft); }
.name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  font-family: var(--mono);
  font-size: 12.5px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.time { font-size: 12px; white-space: nowrap; }
.time.t-bad { font-weight: 600; }
.acts-row { display: inline-flex; flex: none; justify-content: flex-end; width: calc(2 * var(--control-sm)); }
.act { color: var(--ink2); }
.prow.bad .act:hover:not(:disabled) { background: var(--raised); }
/* Off while an action runs: pale, not just faded. */
.act:disabled { color: var(--idle-dot); }

.card {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin: 8px;
  padding: 14px;
  border-radius: 10px;
  background: var(--bad-soft);
}
.card-title { font-size: 14px; }
.acts { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; padding-top: 2px; }
summary { width: max-content; cursor: pointer; }
.raw { font-size: 12px; overflow-wrap: anywhere; user-select: text; }
.code {
  margin-top: 6px;
  padding: 8px 10px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--sunken);
  color: var(--ink);
  font-family: var(--mono);
  font-size: 11.5px;
  line-height: 1.6;
  white-space: pre-wrap;
}

.notice {
  display: flex;
  align-items: flex-start;
  gap: 6px;
  margin: 0 8px 8px;
  padding: 6px 4px 6px 10px;
  border-radius: var(--radius-sm);
  font-size: 12.5px;
  user-select: text;
}
.notice .body { display: flex; flex: 1; flex-direction: column; gap: 4px; min-width: 0; padding-top: 4px; }
.notice .lines { white-space: pre-line; }
.notice .acts { margin-top: 2px; padding-bottom: 2px; }
.notice.error { background: var(--bad-soft); color: var(--bad); }
.notice.info { background: var(--accent-soft); color: var(--ink); }
.notice .close { flex: none; color: inherit; }

.foot {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 8px;
  border-top: 1px solid var(--line);
}
.row { display: flex; gap: 6px; }
.row .btn { flex: 1; font-weight: 400; }
.reason { padding: 0 4px; }
.open {
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: var(--control);
  padding: 0 8px;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: var(--ink);
  font: inherit;
  font-weight: 600;
  cursor: pointer;
}
.open:hover { background: var(--hover); }
.open svg { color: var(--ink3); }
</style>
