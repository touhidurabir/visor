<script setup lang="ts">
// The server dashboard (spec §5.2): header, changed-on-disk banner, the
// health strip (a sentence plus small counts) and the configs and processes
// table, failing rows first in each config (open issues live on the Issues
// page). While supervisord can't be reached, a banner says why in plain words,
// the strip shows dashes, and the actions that need supervisord are disabled.
// All data comes from the live overview; this view only wires menu ids to
// service calls.
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ServerService } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import type { ConfigView, ProcessView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import ApplyReport from '../components/ApplyReport.vue'
import ChangedBanner from '../components/ChangedBanner.vue'
import ConnectionBanner from '../components/ConnectionBanner.vue'
import ConfigTable from '../components/ConfigTable.vue'
import ActionMenu from '../components/ActionMenu.vue'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import SignalDialog from '../components/SignalDialog.vue'
import RecentlyRemoved from '../components/RecentlyRemoved.vue'
import { useApplyFlow } from '../lib/apply-flow'
import { describeError } from '../lib/faults'
import { filterConfigs } from '../lib/filter'
import { connectionProblem } from '../lib/connection'
import { disconnectedHealth, firstFailing, health, NO_COUNT, openIssuesLabel } from '../lib/health'
import { sortFailingFirst } from '../lib/sort'
import { formatDuration } from '../lib/format'
import { configGroups, bulkConfirm, serverMenu, withoutSupervisord, type BulkConfirm } from '../lib/menus'
import { useOverview } from '../lib/overview'
import { copyText } from '../lib/clipboard'
import { configRoute } from '../lib/links'
import { showToast } from '../lib/toast'
import { onMenu, takePendingMenu } from '../lib/menu'

const router = useRouter()
const { overview, error: loadError, reload } = useOverview()
// removedStamp reloads "Recently removed" after a remove or restore here,
// and when the config files change (one removed from another window).
const removedStamp = ref(0)
const flow = useApplyFlow(() => {
  reload()
  removedStamp.value++
})

const query = ref('')
const search = ref<HTMLInputElement>()
const table = ref<InstanceType<typeof ConfigTable>>()
const nowMs = ref(Date.now())
const signalFor = ref<ProcessView | null>(null)
const checking = ref(false)

let tick: ReturnType<typeof setInterval>
// Visor's menu bar: Find, Reload Config and Restart All… act here. A
// choice made on another page waits for the dashboard's data.
const menuOffs: Array<() => void> = []
function whenLoaded(run: () => void) {
  if (overview.value) return run()
  const stop = watch(overview, (v) => {
    if (!v) return
    stop()
    run()
  })
}
onMounted(() => {
  tick = setInterval(() => (nowMs.value = Date.now()), 10_000)
  window.addEventListener('keydown', onKey)
  menuOffs.push(
    onMenu('find', focusSearch),
    onMenu('reload', () => void checkForChanges()),
    onMenu('restart-all', () => bulk('restart')),
  )
  if (takePendingMenu('reload')) whenLoaded(() => void checkForChanges())
  if (takePendingMenu('restart-all')) whenLoaded(() => bulk('restart'))
})
onBeforeUnmount(() => {
  clearInterval(tick)
  window.removeEventListener('keydown', onKey)
  menuOffs.forEach((off) => off())
})

function focusSearch() {
  search.value?.focus()
  search.value?.select()
}

function onKey(e: KeyboardEvent) {
  if (!e.metaKey || e.altKey || e.ctrlKey || e.shiftKey) return
  if (e.key === 'f') {
    e.preventDefault()
    focusSearch()
  } else if (e.key === 'r') {
    e.preventDefault() // never reload the webview
    void checkForChanges()
  }
}

const configs = computed(() => overview.value?.Configs ?? [])
watch(
  () => configs.value.map((c) => c.Path).join('\n'),
  () => removedStamp.value++,
)
// Flow 1: failing processes first within each config.
const sorted = computed(() => sortFailingFirst(configs.value))
const shown = computed(() => filterConfigs(sorted.value, query.value))
const server = computed(() => overview.value?.Server)

const subtitle = computed(() => {
  const s = server.value
  if (!s) return ''
  return [s.Version && `supervisord ${s.Version}`, s.UptimeMs > 0 && `up ${formatDuration(s.UptimeMs)}`]
    .filter(Boolean)
    .join(' · ')
})
// Not connected: what to say, and why the supervisord actions are off.
const problem = computed(() => connectionProblem(server.value))
const offline = computed(() => problem.value?.reason)
const healthView = computed(() => {
  const ov = overview.value
  if (!ov) return null
  return problem.value ? disconnectedHealth(ov.Stats) : health(ov.Stats, sorted.value)
})
const BACK_NOTE = 'Processes appear once supervisord is back. Until then, the ⋯ menus keep only the file actions.'

/** "Show them": scrolls to the first failing row and focuses it, clearing a search that hides it. */
async function showFailing() {
  const p = firstFailing(sorted.value)
  if (!p) return
  if (!shown.value.some((c) => (c.Processes ?? []).some((q) => q.FullName === p.FullName))) query.value = ''
  await nextTick()
  await table.value?.focusProcess(p.FullName)
}

const menuItems = computed(() => (offline.value ? withoutSupervisord(serverMenu(), offline.value) : serverMenu()))

/** Try again: reconnects as Doctor's Check again does, and says so when nothing changed. */
const retrying = ref(false)
async function tryAgain() {
  if (retrying.value) return
  retrying.value = true
  try {
    let ov
    if (!(await run(async () => (ov = await ServerService.Reconnect())))) return
    const still = connectionProblem(ov!.Server)
    if (still) showToast(`Still not connected: ${still.title}.`)
  } finally {
    retrying.value = false
  }
}

const ifOk = (fn: () => unknown) => (ok: boolean) => {
  if (ok) fn()
}

/** Runs an action; a failure becomes an error toast in plain English. */
async function run(fn: () => Promise<unknown>): Promise<boolean> {
  try {
    await fn()
    return true
  } catch (e) {
    showToast(describeError(e), 'error')
    return false
  }
}

async function checkForChanges() {
  if (checking.value) return
  checking.value = true
  try {
    let diff
    if (!(await run(async () => (diff = await ServerService.CheckForChanges())))) return
    const n = (diff!.Added?.length ?? 0) + (diff!.Changed?.length ?? 0) + (diff!.Removed?.length ?? 0)
    showToast(n === 0 ? 'No changes: supervisord already runs every config.' : `${n} ${n === 1 ? 'group differs' : 'groups differ'} from what supervisord runs.`)
  } finally {
    checking.value = false
  }
}

function onServer(id: string) {
  switch (id) {
    case 'check':
      return void checkForChanges()
    case 'apply-all':
      return void flow.applyAll()
    case 'log':
      return void router.push('/issues/log')
    case 'restart':
      return void run(() => ServerService.RestartDaemon()).then(ifOk(() => showToast('Restarting supervisord…')))
    case 'shutdown':
      return void run(() => ServerService.ShutdownDaemon()).then(
        ifOk(() => showToast('supervisord shut down. Start it again from Doctor.', 'info', { label: 'Open Doctor', run: () => void router.push('/doctor') })),
      )
    case 'settings':
      return void router.push('/settings')
  }
}

const bulkCalls = { start: ServerService.StartAll, stop: ServerService.StopAll, restart: ServerService.RestartAll }
// A bulk action waiting for confirmation: every process, or one config's.
const pendingBulk = ref<{ call: () => Promise<unknown>; ask: BulkConfirm } | null>(null)

/** askFirst runs call, after confirming when ask says to. */
function askFirst(ask: BulkConfirm | null, call: () => Promise<unknown>) {
  if (ask) pendingBulk.value = { call, ask }
  else void run(call)
}

function bulk(what: keyof typeof bulkCalls) {
  askFirst(bulkConfirm(what, overview.value?.Stats.Processes ?? 0), bulkCalls[what])
}

function confirmBulk() {
  const p = pendingBulk.value
  pendingBulk.value = null
  if (p) void run(p.call)
}

function configLink(c: ConfigView, q: Record<string, string> = {}) {
  return router.push(configRoute(c.Path, q))
}

function onProcess(p: ProcessView, c: ConfigView, id: string) {
  switch (id) {
    case 'start':
      return void run(() => ServerService.Start(p.FullName))
    case 'stop':
      return void run(() => ServerService.Stop(p.FullName))
    case 'restart':
      return void run(() => ServerService.Restart(p.FullName))
    case 'signal':
      signalFor.value = p
      return
    case 'logs':
      if (c.Path) void configLink(c, { tab: 'logs', process: p.FullName })
      else showToast('This process has no config file, so Visor has no log page for it.', 'error')
      return
    case 'clear':
      return void run(() => ServerService.ClearLogs(p.FullName)).then(ifOk(() => showToast(`Cleared the logs of ${p.FullName}.`)))
    case 'copy-pid':
      return void run(() => copyText(String(p.PID))).then(ifOk(() => showToast(`Copied PID ${p.PID}.`)))
  }
}

function onConfig(c: ConfigView, id: string) {
  const groups = configGroups(c)
  const each = (fn: (g: string) => Promise<void>) => async () => {
    for (const g of groups) await fn(g)
  }
  switch (id) {
    case 'start-all':
      return void run(c.Path ? () => ServerService.StartFile(c.Path) : each(ServerService.StartGroup))
    // Like the config page's buttons, these ask first.
    case 'stop-all':
      return askFirst(bulkConfirm('stop', c.Processes?.length ?? 0, c.FileName), c.Path ? () => ServerService.StopFile(c.Path) : each(ServerService.StopGroup))
    case 'restart-all':
      return askFirst(bulkConfirm('restart', c.Processes?.length ?? 0, c.FileName), c.Path ? () => ServerService.RestartFile(c.Path) : each(ServerService.RestartGroup))
    case 'open-config':
      if (c.Path) void configLink(c, { tab: 'config' })
      return
    case 'describe':
      if (c.Path) void configLink(c, { tab: 'overview' })
      return
    case 'reveal':
      if (c.Path) void run(() => ServerService.Reveal(c.Path))
      return
    case 'open-log':
      if (c.Path) void run(() => openFirstLog(c.Path))
      return
    case 'apply':
      if (c.Path) void flow.applyFile(c.Path, c.FileName, groups)
      return
    case 'remove':
      if (c.Path) void flow.removeFile(c.Path, c.FileName)
      return
  }
}

/** Opens the stdout log of the file's first process that has one. */
async function openFirstLog(path: string) {
  const d = await ServerService.ConfigDetail(path)
  const log = (d.Logs ?? []).find((l) => l.Stdout)?.Stdout
  if (!log) throw new Error('No process in this file writes a log file.')
  await ServerService.OpenLog(log)
}

function onWatch(c: ConfigView, on: boolean) {
  void run(() => ServerService.SetWatch(c.Path, on)).then(ifOk(() => reload()))
}

function sendSignal(sig: string) {
  const p = signalFor.value
  signalFor.value = null
  if (p) void run(() => ServerService.Signal(p.FullName, sig)).then(ifOk(() => showToast(`Sent ${sig} to ${p.FullName}.`)))
}
</script>

<template>
  <div class="page">
    <div v-if="loadError" class="error" role="alert">
      <strong>Can’t load the dashboard</strong>
      <p>{{ loadError }}</p>
      <button type="button" class="btn" @click="reload">Try again</button>
    </div>
    <p v-else-if="!overview" class="muted">Loading…</p>
    <template v-else>
      <header>
        <div class="titles">
          <h1 class="title">{{ overview.Server.Name }}</h1>
          <p class="meta">
            {{ subtitle }}<template v-if="subtitle && overview.Server.ConfigPath"> · </template
            ><span class="mono">{{ overview.Server.ConfigPath }}</span>
          </p>
        </div>
        <div class="tools">
          <div class="search">
            <label for="q" class="sr-only">Search processes and config files</label>
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><circle cx="11" cy="11" r="7" /><path d="m20 20-3.5-3.5" /></svg>
            <input id="q" ref="search" v-model="query" class="input" type="search" placeholder="Search" autocomplete="off" />
            <span class="kbd" aria-hidden="true">⌘F</span>
          </div>
          <button type="button" class="btn" :disabled="checking || !!offline" :title="offline" @click="checkForChanges">
            {{ checking ? 'Checking…' : 'Check for changes' }}
          </button>
          <RouterLink class="btn primary" to="/new">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" aria-hidden="true"><path d="M12 5v14" /><path d="M5 12h14" /></svg>Add new
          </RouterLink>
          <ActionMenu :items="menuItems" label="Server actions" heading="Whole server" @select="onServer" />
        </div>
      </header>

      <ConnectionBanner v-if="problem" :problem="problem" :busy="retrying" @retry="tryAgain" />
      <div v-else-if="overview.Server.Error" class="error" role="alert">
        <strong>Can’t reach supervisord</strong>
        <p>{{ overview.Server.Error }}</p>
        <p>Check that supervisord is running, or open Doctor to find out why.</p>
      </div>

      <ChangedBanner v-if="configs.some((c) => c.Changed || c.Uncertain)" :configs="configs" :busy="flow.running.value" @apply="flow.applyAll" />
      <ApplyReport v-if="flow.report.value" :report="flow.report.value" :busy="flow.running.value" @undo="flow.undo" @dismiss="flow.dismiss" />

      <section v-if="healthView" class="health" aria-label="Health">
        <div class="verdict">
          <span :class="['mark', `m-${healthView.tone}`]" aria-hidden="true">
            <svg v-if="healthView.tone === 'ok'" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="m5 12.5 4.5 4.5L19 7.5" /></svg>
            <svg v-else-if="healthView.tone === 'idle'" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round"><path d="M7 12h10" /></svg>
            <svg v-else width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round"><path d="M12 7v6" /><path d="M12 17v.01" /></svg>
          </span>
          <div class="words">
            <span class="sentence">{{ healthView.sentence }}</span>
            <span class="meta"
              ><span v-for="(seg, i) in healthView.detail" :key="i" :class="{ mono: seg.mono }">{{ seg.text }}</span
              ><template v-if="healthView.failing > 0">{{ ' ' }}<button type="button" class="link" :aria-label="healthView.failing === 1 ? 'Show it: the failing process' : 'Show them: the failing processes'" @click="showFailing">
                  {{ healthView.failing === 1 ? 'Show it' : 'Show them' }}
                </button></template
              ></span
            >
          </div>
        </div>
        <dl class="counts" aria-label="Totals">
          <div v-for="n in healthView.counts" :key="n.label" class="count">
            <dt class="meta">{{ n.label }}</dt>
            <dd :class="{ none: n.value === NO_COUNT }">{{ n.value }}</dd>
          </div>
          <div class="count issues">
            <RouterLink to="/issues">{{ openIssuesLabel(overview.Stats.OpenIssues) }}</RouterLink>
          </div>
        </dl>
      </section>

      <section aria-labelledby="s2">
        <div class="bar">
          <h2 id="s2" class="sec">Configs and processes</h2>
          <div class="bulk" role="group" aria-label="All processes">
            <button type="button" class="btn quiet sm" :disabled="!!offline" :title="offline" @click="bulk('start')">Start all</button>
            <button type="button" class="btn quiet sm" :disabled="!!offline" :title="offline" @click="bulk('stop')">Stop all</button>
            <button type="button" class="btn quiet sm" :disabled="!!offline" :title="offline" @click="bulk('restart')">Restart all</button>
          </div>
        </div>
        <p v-if="!configs.length" class="muted">No config files found. Add a program to get started.</p>
        <ConfigTable v-else ref="table" :configs="shown" :now-ms="nowMs" :offline="offline" :note="offline && BACK_NOTE" @process="onProcess" @config="onConfig" @watch="onWatch" />
        <p class="meta">Each ⋯ menu shows the matching <span class="mono">supervisorctl</span> command in grey. Copy it with the button beside it, or ⌘C.</p>
      </section>

      <RecentlyRemoved :stamp="removedStamp" @restored="reload" />
    </template>

    <SignalDialog v-if="signalFor" :process="signalFor.FullName" @send="sendSignal" @cancel="signalFor = null" />
    <ConfirmDialog
      v-if="flow.pending.value"
      :title="flow.pending.value.title"
      :message="flow.pending.value.message"
      :confirm-label="flow.pending.value.confirmLabel"
      :danger="flow.pending.value.danger"
      @confirm="flow.confirm"
      @cancel="flow.cancel"
    />
    <ConfirmDialog
      v-if="pendingBulk"
      :title="pendingBulk.ask.title"
      :message="pendingBulk.ask.message"
      :confirm-label="pendingBulk.ask.confirmLabel"
      @confirm="confirmBulk"
      @cancel="pendingBulk = null"
    />
  </div>
</template>

<style scoped>
.page { display: flex; flex-direction: column; gap: 20px; }
section { display: flex; flex-direction: column; gap: 10px; }
header { display: flex; flex-wrap: wrap; align-items: flex-end; gap: 16px; }
.titles { display: flex; flex-direction: column; gap: 4px; margin-right: auto; min-width: 0; }
.title { margin: 0; font-family: var(--display); font-optical-sizing: auto; font-size: 30px; font-weight: 650; line-height: 1.1; letter-spacing: -0.015em; }
.meta { margin: 0; color: var(--ink3); font-size: 12.5px; }
.mono { font-family: var(--mono); font-size: 12.5px; }
.muted { margin: 0; color: var(--ink2); }
.sec { margin: 0 auto 0 0; font-size: 15px; font-weight: 600; }
.tools { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; }
.search { position: relative; display: flex; align-items: center; }
.search svg { position: absolute; left: 9px; color: var(--ink3); pointer-events: none; }
.search .input { width: 220px; padding-left: 28px; padding-right: 36px; }
.search .kbd { position: absolute; right: 8px; pointer-events: none; }
/* The native clear button would sit under the shortcut hint. */
.search .input::-webkit-search-cancel-button { display: none; }
.bar { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
/* Start / Stop / Restart all: one quiet group, one click away. */
.bulk { display: inline-flex; border: 1px solid var(--line2); border-radius: var(--radius-sm); overflow: hidden; }
.bulk .btn { border-radius: 0; }
.bulk .btn + .btn { border-left: 1px solid var(--line2); }
/* Disabled, the group looks sunken (like any disabled button), not just grey. */
.bulk .btn:disabled { background: var(--sunken); }

.health { flex-direction: row; flex-wrap: wrap; align-items: center; gap: 12px 28px; padding: 16px 20px; border: 1px solid var(--line); border-radius: 10px; background: var(--raised); }
.verdict { display: flex; align-items: center; gap: 12px; margin-right: auto; min-width: 0; }
.mark { display: flex; flex: none; align-items: center; justify-content: center; width: 36px; height: 36px; border-radius: 50%; }
.m-bad { background: var(--bad-soft); color: var(--bad); }
.m-warn { background: var(--warn-soft); color: var(--warn); }
.m-ok { background: var(--ok-soft); color: var(--ok); }
.m-idle { background: var(--idle-soft); color: var(--idle); }
.words { display: flex; flex-direction: column; gap: 1px; min-width: 0; }
.sentence { font-size: 16px; font-weight: 600; }
.link { padding: 0; border: 0; background: none; color: var(--accent-text); font: inherit; cursor: pointer; }
.link:hover { text-decoration: underline; }
.counts { display: flex; flex-wrap: wrap; gap: 4px 24px; margin: 0; }
.count { display: flex; flex-direction: column; }
.count dt { order: 2; }
.count dd { margin: 0; font-size: 20px; font-weight: 600; line-height: 1.2; font-variant-numeric: tabular-nums; }
.count dd.none { color: var(--ink3); }
.count.issues { justify-content: center; }
.count.issues a { color: var(--accent-text); text-decoration: none; }
.count.issues a:hover { text-decoration: underline; }

.error { padding: 12px; border: 1px solid var(--failing-dot); border-radius: var(--radius-md); background: var(--failing-bg); color: var(--failing-fg); }
.error p { margin: 4px 0 8px; user-select: text; }
.sr-only { position: absolute; left: -9999px; }
</style>
