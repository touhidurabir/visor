<script setup lang="ts">
// The menu-bar popover (#/tray). Failing processes come first, in a "Needs
// attention" group; the rest follow under their config file, each with its
// Watch bell (lib/tray.ts). A row has two lines (the name; the state and
// time) and only the word buttons that apply to its state. Copies of one
// program are one "name ×N" row that unfolds, and its buttons act on every
// copy. The footer has Restart all, Apply changes and Open Visor…. Kept live by the visor:overview event. The bell
// shows the unread count (visor:notifications) and opens the notifications,
// so a failure is noticed even when macOS shows no banner. Laid out as one
// section per server: this Mac for now; remote servers add sections.
import McpAsk from '../components/McpAsk.vue'
import { computed, ref } from 'vue'
import { Window } from '@wailsio/runtime'
import { isMac, platform } from '../lib/platform'
import { ShellService, type ConfigView, type ProcessView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { ServerService } from '../lib/api'
import { errorMessage, useOverview } from '../lib/overview'
import { useApplyFlow } from '../lib/apply-flow'
import { connectionProblem } from '../lib/connection'
import { useNotifications } from '../lib/notifications'
import { Events } from '@wailsio/runtime'
import { VisorTabs } from '../../bindings/events'
import ProductionPrompt from '../components/ProductionPrompt.vue'
import TrayServer from '../components/TrayServer.vue'
import { refreshTabs } from '../lib/servers'
import { serverTabs } from '../lib/tabs'
import { VERB_LABEL, actionTargets, downLine, foldLabel, footerReason, itemActions, itemLook, rowLook, trayActions, trayGroups, trayHeader, type TrayItem, type TrayVerb } from '../lib/tray'

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

// The open server tabs, each a section under this computer's (they follow
// the main window's tabs through visor:tabs).
void refreshTabs()
Events.On(VisorTabs, () => void refreshTabs())

// One section per connected server; only this Mac until remote servers.
const local = computed(() => ({ name: server.value?.Name ?? 'Local', ...trayHeader(server.value, overview.value?.Stats, problem.value) }))
// With servers open, this computer folds like each of them, and every
// section starts folded; the header then names them all.
const grouped = computed(() => serverTabs.value.length > 0)
const localOpen = ref(false)
const showLocal = computed(() => !grouped.value || localOpen.value)
const sections = computed(() => [
  grouped.value
    ? { key: 'all', name: `${platform().local} and ${serverTabs.value.length} ${serverTabs.value.length === 1 ? 'server' : 'servers'}`, tone: '', badge: null, meta: '' }
    : { key: 'local', ...local.value },
])

const groups = computed(() =>
  trayGroups(overview.value?.Configs ?? []).map((g, gi) => ({
    ...g,
    rows: g.items.map((item, ri) => ({
      item,
      // The state line's id, for the fold button's description (a path may hold spaces).
      subId: `tray-sub-${gi}-${ri}`,
      look: itemLook(item),
      actions: itemActions(item),
      copies: item.multi ? item.procs.map((p) => ({ p, look: rowLook(p), actions: trayActions(p.State) })) : [],
    })),
  })),
)

// Rows of copies the user unfolded, by TrayItem.key.
const unfolded = ref(new Set<string>())
function toggleFold(key: string) {
  const next = new Set(unfolded.value)
  if (!next.delete(key)) next.add(key)
  unfolded.value = next
}

function openNotifications() {
  void ShellService.OpenNotifications()
}

const isBusy = (procs: ProcessView[]) => restartingAll.value || procs.some((p) => busy.value.has(p.FullName))

const calls: Record<TrayVerb, (name: string) => Promise<void>> = {
  start: (n) => ServerService.Start(n),
  stop: (n) => ServerService.Stop(n),
  restart: (n) => ServerService.Restart(n),
}

/**
 * Runs a row's button: on its process, or on every copy it applies to
 * (actionTargets), side by side with the per-process calls; never the
 * group calls, as a [group:x] can hold other programs.
 */
async function act(verb: TrayVerb, item: Pick<TrayItem, 'name' | 'procs' | 'multi'>) {
  const targets = actionTargets(verb, item)
  if (!targets.length) return
  const names = targets.map((p) => p.FullName)
  busy.value = new Set([...busy.value, ...names])
  try {
    const results = await Promise.allSettled(names.map((n) => calls[verb](n)))
    const failed = results.flatMap((r, i) => (r.status === 'rejected' ? [{ p: targets[i], e: r.reason as unknown }] : []))
    if (failed.length) {
      const who = item.multi ? failed[0].p.Name : item.name
      const more = failed.length > 1 ? ` (and ${failed.length - 1} more)` : ''
      notice.value = { kind: 'error', text: `Couldn't ${verb} ${who}: ${errorMessage(failed[0].e)}${more}` }
    } else if (notice.value?.kind === 'error') notice.value = null
  } finally {
    const next = new Set(busy.value)
    for (const n of names) next.delete(n)
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

/** Hides the popover (Linux, where losing focus doesn't). */
function closePopover() {
  void Window.Hide()
}
</script>

<template>
  <div class="popover">
    <header v-for="sec in sections" :key="sec.key" class="head">
      <div class="who">
        <div class="title-row">
          <span v-if="sec.tone" :class="['dot', `d-${sec.tone}`]" aria-hidden="true" />
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
      <!-- Linux: the popover stays open when it loses focus, so it has a close button. -->
      <button v-if="!isMac()" type="button" class="btn quiet icon" data-testid="tray-close" aria-label="Close" title="Close (Esc)" @click="closePopover">
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" aria-hidden="true"><path d="M6 6l12 12M18 6L6 18" /></svg>
      </button>
    </header>

    <div class="list">
      <button v-if="grouped" type="button" class="local-head" :aria-expanded="localOpen" data-testid="tray-local" @click="localOpen = !localOpen">
        <span :class="['dot', `d-${local.tone}`]" aria-hidden="true" />
        <span class="who-col">
          <strong>{{ platform().local }}</strong>
          <span :class="['sub', local.badge ? 't-bad' : 'meta']">{{ local.badge ?? local.meta }}</span>
        </span>
        <svg :class="['chev', { open: localOpen }]" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M9 6l6 6-6 6" /></svg>
      </button>
      <template v-if="showLocal">
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
          <h2 :class="['eyebrow', { 't-bad': !g.config }]">{{ g.name }}</h2>
          <button
            v-if="g.config?.Path"
            type="button"
            :class="['btn quiet icon watch', { on: g.config.Watched }]"
            :aria-pressed="g.config.Watched"
            :aria-label="`${g.config.Watched ? 'Watching' : 'Watch'} ${g.config.FileName}`"
            :title="g.config.Watched ? 'Visor notifies you about problems in this file. Click to stop.' : 'Visor stays quiet about this file. Click to watch it.'"
            :disabled="watching.has(g.config.Path)"
            @click="toggleWatch(g.config)"
          >
            <svg v-if="g.config.Watched" width="18" height="18" viewBox="0 0 24 24" fill="currentColor" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              <path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9" /><path d="M10.3 21a1.94 1.94 0 0 0 3.4 0" fill="none" />
            </svg>
            <svg v-else width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              <path d="M8.7 3A6 6 0 0 1 18 8c0 2.7.4 4.7.9 6.1" /><path d="M17 17H3s3-2 3-9c0-.5.1-1 .2-1.5" /><path d="M10.3 21a1.94 1.94 0 0 0 3.4 0" /><path d="M2 2l20 20" />
            </svg>
          </button>
        </div>
        <p v-if="!g.rows.length && g.moved" class="meta moved">Listed under Needs attention</p>
        <div v-for="{ item, subId, look, actions, copies } in g.rows" :key="item.key">
          <div :class="['prow', { bad: look.failing }]" data-testid="tray-row">
            <span :class="['dot', `d-${look.tone}`]" aria-hidden="true" />
            <button
              v-if="item.multi"
              type="button"
              class="fold"
              :aria-expanded="unfolded.has(item.key)"
              :aria-label="foldLabel(item, unfolded.has(item.key))"
              :aria-describedby="subId"
              @click="toggleFold(item.key)"
            >
              <span class="name">
                <span class="n">{{ item.name }}</span><span class="count-x">×{{ item.procs.length }}</span>
                <svg :class="['chev', { open: unfolded.has(item.key) }]" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M9 6l6 6-6 6" /></svg>
              </span>
              <span :id="subId" :class="['sub', look.lineClass]" data-testid="tray-time">{{ look.line }}</span>
            </button>
            <div v-else class="who-col">
              <span class="name" :title="item.procs[0].FullName"><span class="n">{{ item.name }}</span></span>
              <span :class="['sub', look.lineClass]" data-testid="tray-time">{{ look.line }}</span>
            </div>
            <span class="acts-row">
              <button
                v-for="v in actions"
                :key="v"
                type="button"
                class="btn sm"
                :aria-label="`${VERB_LABEL[v]} ${item.name}`"
                :title="item.multi ? `${VERB_LABEL[v]} every copy of ${item.name}` : undefined"
                :disabled="isBusy(item.procs)"
                @click="act(v, item)"
              >
                {{ VERB_LABEL[v] }}
              </button>
            </span>
          </div>
          <div v-if="item.multi && unfolded.has(item.key)" class="copies" role="group" :aria-label="`Copies of ${item.name}`">
            <div v-for="c in copies" :key="c.p.FullName" :class="['prow copy', { bad: c.look.failing }]" data-testid="tray-row">
              <span :class="['dot', `d-${c.look.tone}`]" aria-hidden="true" />
              <div class="who-col">
                <span class="name" :title="c.p.FullName"><span class="n">{{ c.p.Name }}</span></span>
                <span :class="['sub', c.look.lineClass]" data-testid="tray-time">{{ c.look.line }}</span>
              </div>
              <span class="acts-row">
                <button
                  v-for="v in c.actions"
                  :key="v"
                  type="button"
                  class="btn sm"
                  :aria-label="`${VERB_LABEL[v]} ${c.p.Name}`"
                  :disabled="isBusy([c.p])"
                  @click="act(v, { name: c.p.Name, procs: [c.p], multi: false })"
                >
                  {{ VERB_LABEL[v] }}
                </button>
              </span>
            </div>
          </div>
        </div>
      </section>
      </template>
      <!-- Open servers, under this computer (in the scrolling list). -->
      <TrayServer v-for="t in serverTabs" :key="t.id" :tab="t" />
    </div>
    <ProductionPrompt />

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
        <button
          type="button"
          class="btn sm"
          :disabled="flow.running.value || !!pending || !overview || !!problem"
          :title="offline ?? 'Apply config changes'"
          @click="flow.applyAll"
        >
          {{ flow.running.value ? 'Working…' : 'Apply changes' }}
        </button>
        <span class="spacer" />
        <button type="button" class="btn sm" @click="openVisor">Open Visor…</button>
      </div>
      <span v-if="problem" class="meta reason" data-testid="tray-offline">{{ footerReason(problem) }}</span>
    </footer>
    <McpAsk compact />
  </div>
</template>

<style scoped>
/* theme.ts sets data-theme on <html>; the popover is its own window, and
   the tokens follow it. The popover takes getvisor.dev's palette (navy in
   dark, cool grey in light; site src/styles/tokens.css), so it matches the
   site's drawing of it; the accent stays the app's. */
.popover {
  --line: #dce2ea;
  --line2: #cdd5df;
  --hover: #eef2f6;
  --ink: #0e1621;
  --ink2: #45526a;
  --ink3: #59667e;
}
:root[data-theme='dark'] .popover {
  --raised: #111925;
  --sunken: #0e1520;
  --hover: #172232;
  --line: #243246;
  --line2: #2e3f57;
  --ink: #eaf0f7;
  --ink2: #a9b6c7;
  --ink3: #8d9bae;
  --bad: #ff8080;
  --bad-soft: rgba(255, 128, 128, 0.08);
  --ok-dot: #45d49a;
  --bad-dot: #ff8080;
  --idle-dot: #8d9bae;
}
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
  padding: 4px 6px 8px;
}
.pad { margin: 0; padding: 8px; }
.group { padding-top: 6px; }
.group-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  min-height: var(--control);
  padding: 4px 2px 0 12px;
}
/* The config's name as a small eyebrow; Needs attention in red. */
.eyebrow {
  margin: 0;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--ink3);
  font-size: 11.5px;
  font-weight: 600;
  letter-spacing: .08em;
  text-transform: uppercase;
}
.eyebrow.t-bad { color: var(--bad); }
.watch { flex: none; color: var(--ink3); }
.watch.on, .watch.on:hover:not(:disabled) { color: var(--accent-text); }
.watch:disabled { color: var(--idle-dot); }
.moved { margin: 0; padding: 2px 10px 6px; font-size: 12px; }

/* A row: dot, the name over the state and time, the word buttons. */
.prow {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 8px 8px 8px 12px;
  border-radius: 8px;
}
.prow:hover { background: var(--hover); }
.prow.bad { background: var(--bad-soft); }
.who-col, .fold {
  display: flex;
  flex: 1;
  flex-direction: column;
  align-items: flex-start;
  gap: 1px;
  min-width: 0;
}
.fold {
  padding: 0;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: pointer;
}
.fold:hover .n { text-decoration: underline; }
.name {
  display: flex;
  align-items: center;
  gap: 6px;
  max-width: 100%;
  min-width: 0;
  color: var(--ink);
  font-family: var(--mono);
  font-size: 13px;
  font-weight: 500;
}
.n { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.count-x { flex: none; color: var(--ink3); font-weight: 400; }
.chev { flex: none; color: var(--ink3); transition: transform .15s; }
/* This computer's folding row, as TrayServer's (when servers are open). */
.local-head { display: flex; align-items: center; gap: 8px; width: calc(100% - 12px); min-height: 40px; margin: 4px 6px; padding: 6px 8px 6px 10px; border: 0; border-radius: 8px; background: transparent; color: var(--ink); font: inherit; text-align: left; cursor: pointer; }
.local-head:hover { background: var(--hover); }
.local-head .who-col { flex: 1; }
.chev.open { transform: rotate(90deg); }
.sub {
  max-width: 100%;
  overflow: hidden;
  font-size: 12px;
  line-height: 16px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.sub.t-bad { font-weight: 600; }
.acts-row { display: inline-flex; flex: none; gap: 6px; }
.popover :deep(.btn:not(.primary):not(.quiet):not(:disabled)) { border-color: var(--line2); background: var(--sunken); }
.popover :deep(.btn:not(.primary):not(.quiet):hover:not(:disabled)) { background: var(--hover); }
.popover :deep(.btn) { border-radius: 9px; font-weight: 600; }
/* The copies of a "name ×N" row, indented under it on a guide line. */
.copies { margin: 0 0 4px 22px; padding-left: 8px; border-left: 1px solid var(--line); }
.prow.copy { padding-top: 5px; padding-bottom: 5px; }
.prow.copy .name { font-size: 12.5px; font-weight: 400; }
@media (prefers-reduced-motion: reduce) {
  .chev { transition: none; }
}

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
  padding: 8px 10px;
  border-top: 1px solid var(--line);
}
.row { display: flex; align-items: center; gap: 6px; }
.spacer { flex: 1; }
.reason { padding: 0 2px; }
</style>
