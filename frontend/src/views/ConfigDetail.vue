<script setup lang="ts">
// One config file (spec §5.3): header with Watch and file actions, process
// chips, and the Overview / Logs / Config / History tabs. The config and its
// processes come from the live overview; log files, the last error and the
// line-by-line explanation come from ServerService.ConfigDetail, reloaded
// whenever a process in this file changes state. The tab, process, log
// stream and editor line live in the URL (?tab=…&process=group:name
// &stream=stderr&line=9), so links land on them.
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { ServerService, type ConfigDetailView, type EditView, type ProcessView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import ConfigEditor from '../components/ConfigEditor.vue'
import ExplainSummary from '../components/ExplainSummary.vue'
import HistoryList from '../components/HistoryList.vue'
import LastErrorCard from '../components/LastErrorCard.vue'
import LineByLine from '../components/LineByLine.vue'
import LogView from '../components/LogView.vue'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import { openLog, openPath, reveal } from '../lib/finder'
import { setPendingEdit } from '../lib/pending-edit'
import { describeError } from '../lib/faults'
import { bulkConfirm, type BulkConfirm } from '../lib/menus'
import { chipSince } from '../lib/format'
import { stateLook } from '../lib/state'
import type { Stream } from '../lib/logview'
import { useOverview } from '../lib/overview'
import { configRoute } from '../lib/links'
import { showToast } from '../lib/toast'

const TABS = [
  { id: 'overview', label: 'Overview' },
  { id: 'logs', label: 'Logs' },
  { id: 'config', label: 'Config' },
  { id: 'history', label: 'History' },
] as const
type Tab = (typeof TABS)[number]['id']

const route = useRoute()
const router = useRouter()
const { overview, error: loadError, reload } = useOverview()

const path = computed(() => {
  const p = route.params.path
  return Array.isArray(p) ? p.join('/') : (p ?? '')
})
const config = computed(() => overview.value?.Configs?.find((c) => c.Path !== '' && c.Path === path.value))
const procs = computed(() => config.value?.Processes ?? [])
const detail = ref<ConfigDetailView | null>(null)
const detailError = ref<string | null>(null)
const busy = ref(false)
const nowMs = ref(Date.now())
const tabEls = ref<HTMLButtonElement[]>([])

const tab = computed<Tab>(() => {
  const t = String(route.query.tab ?? '')
  return (TABS.find((x) => x.id === t)?.id ?? 'overview') as Tab
})
const selected = computed<ProcessView | undefined>(() => {
  const q = String(route.query.process ?? '')
  return (
    procs.value.find((p) => p.FullName === q) ??
    procs.value.find((p) => p.FullName === detail.value?.LastError?.Process) ??
    procs.value[0]
  )
})
const selectedLogs = computed(() => detail.value?.Logs?.find((l) => l.Process === selected.value?.FullName))

const failing = computed(() => procs.value.filter((p) => p.Status === 'failing').length)
const running = computed(() => procs.value.filter((p) => p.Status === 'running').length)
const programs = computed(() => detail.value?.Programs ?? new Set(procs.value.map((p) => p.Program || p.Group)).size)
const counts = computed(() => {
  const n = procs.value.length
  return `${programs.value} ${programs.value === 1 ? 'program' : 'programs'} · ${n} ${n === 1 ? 'process' : 'processes'}`
})

/** ?stream= picks the log; else a failing process opens on stderr, where its errors are, when stderr has its own log. */
const initialStream = computed<Stream>(() => {
  const q = String(route.query.stream ?? '')
  if (q === 'stdout' || q === 'stderr') return q
  const p = selected.value
  const l = selectedLogs.value
  return p?.Status === 'failing' && l?.Stderr && l.Stderr !== l.Stdout ? 'stderr' : 'stdout'
})

function go(patch: Record<string, string>) {
  const q: Record<string, string> = {}
  for (const [k, v] of Object.entries({ ...route.query, ...patch })) if (typeof v === 'string' && v) q[k] = v
  void router.replace(configRoute(path.value, q))
}

function pickTab(t: Tab) {
  go({ tab: t === 'overview' ? '' : t, line: '', stream: '' })
}

function onTabKey(e: KeyboardEvent, i: number) {
  const n = TABS.length
  const to = e.key === 'ArrowRight' ? (i + 1) % n : e.key === 'ArrowLeft' ? (i + n - 1) % n : e.key === 'Home' ? 0 : e.key === 'End' ? n - 1 : -1
  if (to < 0) return
  e.preventDefault()
  pickTab(TABS[to].id)
  tabEls.value[to]?.focus()
}

let loading: Promise<void> | null = null
let again = false
async function loadDetail() {
  if (!path.value) return
  if (loading) {
    again = true
    return loading
  }
  loading = (async () => {
    do {
      again = false
      const want = path.value
      try {
        const d = await ServerService.ConfigDetail(want)
        if (want === path.value) {
          detail.value = d
          detailError.value = null
        }
      } catch (e) {
        if (want === path.value) detailError.value = describeError(e)
      }
    } while (again)
  })()
  try {
    await loading
  } finally {
    loading = null
  }
}

// Reload the explanation and the last error when a process here changes
// state or the file changes on disk.
const stateKey = computed(
  () => procs.value.map((p) => `${p.FullName}=${p.State}/${p.PID}/${p.StopMs}`).join(',') + `|${config.value?.Changed}|${config.value?.Uncertain}`,
)
watch([path, stateKey], ([p], [oldP]) => {
  if (p !== oldP) detail.value = null
  void loadDetail()
})

/** The line in the Config tab editor (?line=N), opened from the Overview. */
const editorLine = computed(() => Number(route.query.line ?? 0) || 0)
function editLine(line: number) {
  go({ tab: 'config', line: String(line) })
}
function fixInEditor(edit: EditView) {
  setPendingEdit(path.value, { edit })
  go({ tab: 'config', line: String(edit.Insert ? edit.Line + 1 : edit.Line) })
}
function addInEditor(after: number, lines: string[]) {
  setPendingEdit(path.value, { after, lines })
  go({ tab: 'config', line: String(after + 1) })
}
function viewLog(process: string, stream: string) {
  go({ tab: 'logs', process, stream })
}
/** "Caused by line N": brings that line of the explanation into view. */
function showLine(line: number) {
  const el = document.getElementById(`explain-line-${line}`)
  el?.scrollIntoView({ block: 'center', behavior: 'smooth' })
  el?.focus({ preventScroll: true })
}

let tick: ReturnType<typeof setInterval>
onMounted(() => {
  tick = setInterval(() => (nowMs.value = Date.now()), 10_000)
  void loadDetail()
})
onBeforeUnmount(() => clearInterval(tick))

/** Runs an action; a failure becomes an error toast in plain English. */
async function run(fn: () => Promise<unknown>, done?: string): Promise<boolean> {
  busy.value = true
  try {
    await fn()
    if (done) showToast(done)
    return true
  } catch (e) {
    showToast(describeError(e), 'error')
    return false
  } finally {
    busy.value = false
    void loadDetail()
  }
}

const fileCalls = {
  start: () => ServerService.StartFile(path.value),
  stop: () => ServerService.StopFile(path.value),
  restart: () => ServerService.RestartFile(path.value),
}
const pendingBulk = ref<{ what: keyof typeof fileCalls; ask: BulkConfirm } | null>(null)

function bulk(what: keyof typeof fileCalls) {
  const ask = bulkConfirm(what, procs.value.length, config.value?.FileName)
  if (ask) pendingBulk.value = { what, ask }
  else void run(fileCalls[what])
}

function confirmBulk() {
  const p = pendingBulk.value
  pendingBulk.value = null
  if (p) void run(fileCalls[p.what])
}
const toggleWatch = () => {
  const c = config.value
  if (c) void run(() => ServerService.SetWatch(c.Path, !c.Watched)).then((ok) => {
      if (ok) void reload()
    })
}
const restart = (name: string) => void run(() => ServerService.Restart(name), `Restarted ${name}.`)
</script>

<template>
  <div :class="['page', { fill: tab === 'logs' && !!config }]">
    <nav class="crumbs" aria-label="Breadcrumb">
      <RouterLink to="/">{{ overview?.Server.Name ?? 'Local' }}</RouterLink> / {{ config?.FileName ?? path.split('/').pop() }}
    </nav>

    <div v-if="loadError" class="alert" role="alert">
      <strong>Can’t load this config</strong>
      <p>{{ loadError }}</p>
      <button type="button" class="btn" @click="reload">Try again</button>
    </div>
    <p v-else-if="!overview" class="muted">Loading…</p>
    <div v-else-if="!config" class="alert" role="alert">
      <strong>This config isn’t one of supervisord’s config files</strong>
      <p class="mono">{{ path }}</p>
      <p>It may have been moved or deleted, or the [include] files= setting no longer matches it.</p>
      <RouterLink class="btn" to="/">Back to the dashboard</RouterLink>
    </div>
    <template v-else>
      <header>
        <div class="title">
          <div class="h1row">
            <h1>{{ config.FileName }}</h1>
            <span v-if="failing > 0" class="badge b-bad"><span class="dot d-bad" aria-hidden="true" />{{ failing }} failing</span>
            <span v-else-if="procs.length && running === procs.length" class="badge b-ok"><span class="dot d-ok" aria-hidden="true" />All running</span>
            <span v-if="config.Changed" class="badge b-warn">Changed on disk</span>
          </div>
          <p class="meta sub"><span class="mono">{{ path }}</span> · {{ counts }}</p>
        </div>
        <div class="tools">
          <button
            type="button"
            :class="['btn', { 'watch-on': config.Watched }]"
            :aria-pressed="config.Watched"
            :title="config.Watched ? 'Watching: Visor notifies you when a process in this file fails' : 'Watch: Visor stays quiet about this file until you do'"
            @click="toggleWatch"
          >
            <svg v-if="config.Watched" width="14" height="14" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9" /><path d="M10.3 21a1.9 1.9 0 0 0 3.4 0" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" /></svg>
            <svg v-else width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9" /><path d="M10.3 21a1.9 1.9 0 0 0 3.4 0" /></svg>
            {{ config.Watched ? 'Watching' : 'Watch' }}
          </button>
          <div class="bulk" role="group" :aria-label="`All processes in ${config.FileName}`">
            <button type="button" class="btn quiet" :disabled="busy" @click="bulk('start')">Start all</button>
            <button type="button" class="btn quiet" :disabled="busy" @click="bulk('stop')">Stop all</button>
            <button type="button" class="btn quiet" :disabled="busy" @click="bulk('restart')">Restart all</button>
          </div>
          <button type="button" class="btn quiet" @click="reveal(config.Path)">Reveal in Finder</button>
        </div>
      </header>

      <div v-if="overview.Server.Error" class="alert" role="alert">
        <strong>Can’t reach supervisord</strong>
        <p>{{ overview.Server.Error }}</p>
      </div>

      <p v-if="!procs.length" class="muted">supervisord runs no process from this file. Apply it to load its programs.</p>
      <div v-else class="chips" role="group" aria-label="Processes in this config">
        <button
          v-for="p in procs"
          :key="p.FullName"
          type="button"
          :class="['btn', 'chip', { on: p.FullName === selected?.FullName, failing: p.Status === 'failing' }]"
          :aria-pressed="p.FullName === selected?.FullName"
          :title="`${p.FullName} · ${p.State}`"
          @click="go({ process: p.FullName })"
        >
          <span :class="['dot', `d-${stateLook(p.State, p.Status).tone}`]" aria-hidden="true" />
          <span class="mono name">{{ p.Name }}</span>
          <span :class="['since', { 't-bad': p.Status === 'failing' }]">{{ chipSince(p, nowMs) }}</span>
        </button>
      </div>

      <div class="tabs" role="tablist" aria-label="Config views">
        <button
          v-for="(t, i) in TABS"
          :id="`tab-${t.id}`"
          :key="t.id"
          ref="tabEls"
          type="button"
          role="tab"
          :class="{ on: tab === t.id }"
          :aria-selected="tab === t.id"
          :aria-controls="`panel-${t.id}`"
          :tabindex="tab === t.id ? 0 : -1"
          @click="pickTab(t.id)"
          @keydown="onTabKey($event, i)"
        >
          {{ t.label }}
        </button>
      </div>

      <div :id="`panel-${tab}`" :class="['panel', { fill: tab === 'logs' }]" role="tabpanel" :aria-labelledby="`tab-${tab}`">
        <div v-if="detailError" class="alert detail-error" role="alert">
          <span>Couldn’t read this config’s details. {{ detailError }}</span>
          <button type="button" class="btn" @click="loadDetail">Retry</button>
        </div>

        <div v-if="tab === 'overview'" class="overview">
          <LastErrorCard
            v-if="detail?.LastError"
            :error="detail.LastError"
            :busy="busy"
            @restart="restart(detail.LastError.Process)"
            @open-stderr="openLog(detail.LastError.StderrLog)"
            @open-config="openPath(config.Path)"
            @show-line="showLine"
          />
          <ExplainSummary :view="detail ? detail.Explain : null" :failed="detail?.ExplainError || null" />
          <LineByLine
            :key="config.Path"
            :view="detail ? detail.Explain : null"
            :failed="detail?.ExplainError || null"
            mode="overview"
            @goto="editLine"
            @edit="fixInEditor"
            @add="addInEditor"
            @open="openPath"
            @reveal="reveal"
            @open-log="openLog"
            @view-log="viewLog"
          />
        </div>

        <template v-else-if="tab === 'logs'">
          <LogView
            v-if="selected && (detail || detailError)"
            :key="selected.FullName"
            :process="selected.FullName"
            :stdout-log="selectedLogs?.Stdout ?? ''"
            :stderr-log="selectedLogs?.Stderr ?? ''"
            :initial-stream="initialStream"
            :running="selected.State === 'RUNNING' || selected.State === 'STARTING'"
          />
          <p v-else-if="!selected" class="muted">There is no process to show logs for.</p>
          <p v-else class="muted">Loading…</p>
        </template>

        <ConfigEditor
          v-else-if="tab === 'config'"
          :key="config.Path"
          :path="config.Path"
          :file-name="config.FileName"
          :changed="config.Changed"
          :uncertain="config.Uncertain"
          :line="editorLine"
        />

        <template v-else-if="tab === 'history'">
          <HistoryList
            v-if="selected"
            :process="selected.FullName"
            :version="`${selected.State}/${selected.StartMs}/${selected.StopMs}`"
            :now-ms="nowMs"
          />
          <p v-else class="muted">There is no process to show history for.</p>
        </template>
      </div>
    </template>
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
.page { display: flex; flex-direction: column; gap: 16px; }
/* The Logs tab: the log pane takes the rest of the window and scrolls itself. */
.page.fill { height: 100%; min-height: 560px; }
.panel.fill { display: flex; flex: 1 1 0; flex-direction: column; min-height: 0; }
.crumbs { color: var(--ink3); font-size: 12.5px; }
.crumbs a { color: var(--accent-text); text-decoration: none; }
.crumbs a:hover { text-decoration: underline; }
header { display: flex; flex-wrap: wrap; align-items: flex-end; gap: 12px 16px; }
.title { display: flex; flex-direction: column; gap: 4px; margin-right: auto; min-width: 0; }
.h1row { display: flex; flex-wrap: wrap; align-items: center; gap: 10px; }
h1 { margin: 0; font-family: var(--display); font-optical-sizing: auto; font-size: 30px; font-weight: 650; line-height: 1.1; letter-spacing: -0.015em; }
.meta { margin: 0; color: var(--ink3); font-size: 12.5px; }
.sub { overflow-wrap: anywhere; user-select: text; }
.mono { font-family: var(--mono); font-size: 12.5px; }
.muted { margin: 0; color: var(--ink2); }
.tools { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; }
.btn.watch-on, .btn.watch-on:hover:not(:disabled) { background: var(--accent-soft); border-color: transparent; color: var(--accent-text); }
/* Start / Stop / Restart all: one quiet group, as on the dashboard. */
.bulk { display: inline-flex; border: 1px solid var(--line2); border-radius: var(--radius-sm); overflow: hidden; }
.bulk .btn { border-radius: 0; }
.bulk .btn + .btn { border-left: 1px solid var(--line2); }
.bulk .btn:disabled { background: var(--sunken); }
/* Process chips: plain, except the failing one, tinted with its state and
   time in words. The picked one gets a ring. */
.chips { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; }
.chip .name { font-size: 12.5px; }
.chip .since { color: var(--ink3); font-size: 12.5px; }
.chip.failing, .chip.failing:hover:not(:disabled) { background: var(--bad-soft); border-color: transparent; }
.chip.failing .name, .chip.failing .since { font-weight: 600; }
.chip.failing .since { color: var(--bad); }
.chip.on { border-color: var(--accent); box-shadow: 0 0 0 1px var(--accent); }
.chip.failing.on { border-color: var(--bad-dot); box-shadow: 0 0 0 1px var(--bad-dot); }
.tabs { flex-wrap: wrap; }
.overview { display: flex; flex-direction: column; gap: 16px; min-width: 0; }
.alert { margin: 0; padding: 12px; border: 1px solid var(--bad-dot); border-radius: var(--radius-md); background: var(--bad-soft); color: var(--bad); }
.detail-error { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 8px; margin-bottom: 16px; }
.alert p { margin: 4px 0 8px; user-select: text; }
</style>
