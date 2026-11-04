<script setup lang="ts">
// The server dashboard (spec §5.2): header, changed-on-disk banner, six
// stats and the configs and processes table (open issues live on the Issues
// page). All data comes from the live overview; this view only wires menu ids
// to service calls.
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ServerService } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import type { ConfigView, ProcessView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import ApplyReport from '../components/ApplyReport.vue'
import ChangedBanner from '../components/ChangedBanner.vue'
import ConfigTable from '../components/ConfigTable.vue'
import ActionMenu from '../components/ActionMenu.vue'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import SignalDialog from '../components/SignalDialog.vue'
import StatTile from '../components/StatTile.vue'
import RecentlyRemoved from '../components/RecentlyRemoved.vue'
import { useApplyFlow } from '../lib/apply-flow'
import { describeError } from '../lib/faults'
import { filterConfigs } from '../lib/filter'
import { formatDuration } from '../lib/format'
import { configGroups, bulkConfirm, serverMenu, type BulkConfirm } from '../lib/menus'
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
const shown = computed(() => filterConfigs(configs.value, query.value))
const server = computed(() => overview.value?.Server)

const subtitle = computed(() => {
  const s = server.value
  if (!s) return ''
  return [s.Version && `supervisord ${s.Version}`, s.UptimeMs > 0 && `up ${formatDuration(s.UptimeMs)}`]
    .filter(Boolean)
    .join(' · ')
})
const incDir = computed(() => {
  const path = configs.value.find((c) => c.Path)?.Path ?? ''
  return path.split('/').slice(-2, -1)[0] ?? ''
})
const programs = computed(() => new Set(configs.value.flatMap((c) => (c.Processes ?? []).map((p) => p.Program || p.Group))).size)
const longestUp = computed(() => {
  const starts = configs.value.flatMap((c) => c.Processes ?? []).filter((p) => p.Status === 'running' && p.StartMs > 0)
  if (!starts.length) return 'none running'
  return `longest up ${formatDuration(nowMs.value - Math.min(...starts.map((p) => p.StartMs)))}`
})

const menuItems = serverMenu()

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
      return void run(() => ServerService.ShutdownDaemon()).then(ifOk(() => showToast('supervisord shut down. Start it from a terminal to bring your processes back.')))
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
        <div>
          <h1>{{ overview.Server.Name }}</h1>
          <div class="sub">
            {{ subtitle }}<template v-if="subtitle && overview.Server.ConfigPath"> · </template
            ><span class="mono">{{ overview.Server.ConfigPath }}</span>
          </div>
        </div>
        <div class="tools">
          <label for="q" class="sr-only">Search processes and config files</label>
          <input id="q" ref="search" v-model="query" type="search" placeholder="Search  ⌘F" autocomplete="off" />
          <button type="button" class="btn" :disabled="checking" @click="checkForChanges">
            {{ checking ? 'Checking…' : 'Check for changes' }}
          </button>
          <RouterLink class="btn primary" to="/new">+ Add new</RouterLink>
          <ActionMenu :items="menuItems" label="Server actions" heading="Whole server" @select="onServer" />
        </div>
      </header>

      <div v-if="overview.Server.Error" class="error" role="alert">
        <strong>Can’t reach supervisord</strong>
        <p>{{ overview.Server.Error }}</p>
        <p>Check that supervisord is running, or open Doctor to find out why.</p>
      </div>

      <ChangedBanner v-if="configs.some((c) => c.Changed || c.Uncertain)" :configs="configs" :busy="flow.running.value" @apply="flow.applyAll" />
      <ApplyReport v-if="flow.report.value" :report="flow.report.value" :busy="flow.running.value" @undo="flow.undo" @dismiss="flow.dismiss" />

      <section aria-labelledby="s1">
        <h2 id="s1" class="label">Overview</h2>
        <div class="tiles">
          <StatTile label="Configs" :value="overview.Stats.Configs" :hint="incDir ? `in ${incDir}` : 'include files'" />
          <StatTile label="Processes" :value="overview.Stats.Processes" :hint="`from ${programs} ${programs === 1 ? 'program' : 'programs'}`" />
          <StatTile label="Running" :value="overview.Stats.Running" :hint="longestUp" tone="running" />
          <StatTile label="Failing" :value="overview.Stats.Failing" hint="FATAL or BACKOFF" tone="failing" />
          <StatTile label="Stopped" :value="overview.Stats.Stopped" hint="by you or exited" />
          <StatTile label="Open issues" :value="overview.Stats.OpenIssues" hint="View all →" to="/issues" />
        </div>
      </section>

      <section aria-labelledby="s2">
        <div class="bar">
          <h2 id="s2" class="label">Configs and processes</h2>
          <div class="bulk">
            <button type="button" class="btn" @click="bulk('start')">Start all</button>
            <button type="button" class="btn" @click="bulk('stop')">Stop all</button>
            <button type="button" class="btn" @click="bulk('restart')">Restart all</button>
          </div>
        </div>
        <p v-if="!configs.length" class="muted">No config files found. Add a program to get started.</p>
        <ConfigTable v-else :configs="shown" :now-ms="nowMs" @process="onProcess" @config="onConfig" @watch="onWatch" />
        <p class="muted">Grey text in each menu is the matching <span class="mono">supervisorctl</span> command. Copy it with the button beside it, or ⌘C.</p>
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
.page { display: flex; flex-direction: column; gap: 22px; }
section { display: flex; flex-direction: column; gap: 10px; }
header { display: flex; flex-wrap: wrap; align-items: flex-start; justify-content: space-between; gap: 12px; }
h1 { margin: 0; font-size: 22px; font-weight: 650; }
.sub { margin-top: 3px; color: var(--text-muted); }
.mono { font-family: var(--font-mono); font-size: 12px; }
.muted { margin: 0; color: var(--text-muted); }
.tools, .bulk { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; }
.bar { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 8px; }
.label { margin: 0; font-size: 11px; font-weight: 600; letter-spacing: 0.04em; text-transform: uppercase; color: var(--text-muted); }
.tiles { display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: 12px; }
input[type='search'] { min-height: var(--control); width: 200px; box-sizing: border-box; padding: 0 10px; border: 1px solid var(--line-strong); border-radius: var(--radius-sm); background: var(--bg); color: var(--text); font: inherit; }
a:not(.btn) { color: var(--accent-text); }
.error { padding: 12px; border: 1px solid var(--failing-dot); border-radius: var(--radius-md); background: var(--failing-bg); color: var(--failing-fg); }
.error p { margin: 4px 0 8px; user-select: text; }
.sr-only { position: absolute; left: -9999px; }
</style>
