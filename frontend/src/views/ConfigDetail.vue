<script setup lang="ts">
// One config file (spec §5.3): header with Watch and file actions, process
// chips, and the Overview / Logs / Config / History tabs. The config and its
// processes come from the live overview; paths, log files and the last
// error come from ServerService.ConfigDetail, reloaded whenever a process
// in this file changes state. The tab and process live in the URL
// (?tab=…&process=group:name), so dashboard links land on them.
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { ServerService, type ConfigDetailView, type ProcessView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import HistoryList from '../components/HistoryList.vue'
import LastErrorCard from '../components/LastErrorCard.vue'
import LogView from '../components/LogView.vue'
import PathsCard from '../components/PathsCard.vue'
import { describeError } from '../lib/faults'
import { sinceLabel } from '../lib/format'
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
const subtitle = computed(() => {
  const n = procs.value.length
  return [path.value, `${programs.value} ${programs.value === 1 ? 'program' : 'programs'}`, `${n} ${n === 1 ? 'process' : 'processes'}`].join(' · ')
})

/** A failing process opens on stderr, where its errors are, when stderr has its own log. */
const initialStream = computed<Stream>(() => {
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
  go({ tab: t === 'overview' ? '' : t })
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

// Reload paths and the last error when a process here changes state.
const stateKey = computed(() => procs.value.map((p) => `${p.FullName}=${p.State}/${p.PID}/${p.StopMs}`).join(','))
watch([path, stateKey], ([p], [oldP]) => {
  if (p !== oldP) detail.value = null
  void loadDetail()
})

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

const startAll = () => run(() => ServerService.StartFile(path.value))
const stopAll = () => run(() => ServerService.StopFile(path.value))
const restartAll = () => run(() => ServerService.RestartFile(path.value))
/** Finder and editor actions change nothing in supervisord: no busy state, no reload. */
async function finder(fn: () => Promise<unknown>) {
  try {
    await fn()
  } catch (e) {
    showToast(describeError(e), 'error')
  }
}
const reveal = (p: string) => void finder(() => ServerService.Reveal(p))
const openPath = (p: string) => void finder(() => ServerService.OpenInEditor(p))
const toggleWatch = () => {
  const c = config.value
  if (c) void run(() => ServerService.SetWatch(c.Path, !c.Watched)).then((ok) => {
      if (ok) void reload()
    })
}
const restart = (name: string) => void run(() => ServerService.Restart(name), `Restarted ${name}.`)
</script>

<template>
  <div class="page">
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
            <span v-if="failing > 0" class="badge failing"><span class="dot" aria-hidden="true" />{{ failing }} failing</span>
            <span v-else-if="procs.length && running === procs.length" class="badge running"><span class="dot" aria-hidden="true" />all running</span>
            <span v-if="config.Changed" class="badge warning">changed on disk</span>
          </div>
          <div class="mono sub">{{ subtitle }}</div>
        </div>
        <div class="tools">
          <button
            type="button"
            :class="['btn', { 'watch-on': config.Watched }]"
            :aria-pressed="config.Watched"
            :title="config.Watched ? 'Visor notifies you when a process in this file fails' : 'Visor stays quiet about this file'"
            @click="toggleWatch"
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9" /><path d="M10.3 21a1.9 1.9 0 0 0 3.4 0" /></svg>
            {{ config.Watched ? 'Watching: notify on failure' : 'Watch for failures' }}
          </button>
          <button type="button" class="btn" :disabled="busy" @click="startAll">Start all</button>
          <button type="button" class="btn" :disabled="busy" @click="stopAll">Stop all</button>
          <button type="button" class="btn" :disabled="busy" @click="restartAll">Restart all</button>
          <button type="button" class="btn" @click="reveal(config.Path)">Reveal in Finder</button>
        </div>
      </header>

      <div v-if="overview.Server.Error" class="alert" role="alert">
        <strong>Can’t reach supervisord</strong>
        <p>{{ overview.Server.Error }}</p>
      </div>

      <section class="chips-wrap" aria-labelledby="procs-label">
        <span id="procs-label" class="label">Processes in this config</span>
        <p v-if="!procs.length" class="muted">supervisord runs no process from this file. Apply it to load its programs.</p>
        <div v-else class="chips" role="group" aria-labelledby="procs-label">
          <button
            v-for="p in procs"
            :key="p.FullName"
            type="button"
            :class="['chip', { on: p.FullName === selected?.FullName }]"
            :aria-pressed="p.FullName === selected?.FullName"
            :title="`${p.FullName} · ${p.State}`"
            @click="go({ process: p.FullName })"
          >
            <span :class="['dot', p.Status]" aria-hidden="true" />
            <span class="mono">{{ p.Name }}</span>
            <span class="since">{{ sinceLabel(p, nowMs) || p.State }}</span>
          </button>
        </div>
      </section>

      <div class="tabs" role="tablist" aria-label="Config views">
        <button
          v-for="(t, i) in TABS"
          :id="`tab-${t.id}`"
          :key="t.id"
          ref="tabEls"
          type="button"
          role="tab"
          :class="['tab', { on: tab === t.id }]"
          :aria-selected="tab === t.id"
          :aria-controls="`panel-${t.id}`"
          :tabindex="tab === t.id ? 0 : -1"
          @click="pickTab(t.id)"
          @keydown="onTabKey($event, i)"
        >
          {{ t.label }}
        </button>
      </div>

      <div :id="`panel-${tab}`" role="tabpanel" :aria-labelledby="`tab-${tab}`">
        <div v-if="detailError" class="alert detail-error" role="alert">
          <span>Couldn’t read this config’s details. {{ detailError }}</span>
          <button type="button" class="btn" @click="loadDetail">Retry</button>
        </div>

        <div v-if="tab === 'overview'" class="grid">
          <div class="col">
            <LastErrorCard
              v-if="detail?.LastError"
              :error="detail.LastError"
              :busy="busy"
              @restart="restart(detail.LastError.Process)"
              @open-stderr="openPath(detail.LastError.StderrLog)"
              @open-config="openPath(config.Path)"
            />
            <PathsCard v-if="detail" :paths="detail.Paths ?? []" @open="openPath" @reveal="reveal" />
            <p v-else-if="!detailError" class="muted">Loading…</p>
          </div>
          <section class="card" aria-labelledby="describe-title">
            <div class="card-head">
              <strong id="describe-title">What this config does</strong>
              <span class="muted small">Generated from the file, no AI</span>
            </div>
            <p class="muted">A plain-English description of each program, with settings worth a look. Coming in this release.</p>
          </section>
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

        <section v-else-if="tab === 'config'" class="card" aria-labelledby="editor-title">
          <div class="card-head">
            <strong id="editor-title">Editor</strong>
            <button type="button" class="btn" @click="openPath(config.Path)">Open in editor</button>
          </div>
          <p class="muted">
            Editing this file in Visor, with checks, a diff against what supervisord runs and a backup before every save, is coming in
            this release. Until then, open it in your editor and use Check for changes on the dashboard.
          </p>
        </section>

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
  </div>
</template>

<style scoped>
.page { display: flex; flex-direction: column; gap: 18px; }
.crumbs { color: var(--text-muted); }
.crumbs a { color: var(--accent-text); }
header { display: flex; flex-wrap: wrap; align-items: flex-start; justify-content: space-between; gap: 12px; }
.title { display: flex; flex-direction: column; gap: 6px; min-width: 0; }
.h1row { display: flex; flex-wrap: wrap; align-items: center; gap: 10px; }
h1 { margin: 0; font-size: 22px; font-weight: 650; }
.sub { color: var(--text-muted); overflow-wrap: anywhere; user-select: text; }
.mono { font-family: var(--font-mono); font-size: 12px; }
.muted { margin: 0; color: var(--text-muted); }
.small { font-size: 12px; }
.tools { display: flex; flex-wrap: wrap; gap: 8px; }
.btn { display: inline-flex; align-items: center; gap: 6px; min-height: 32px; padding: 0 12px; border-radius: var(--radius-sm); border: 1px solid var(--line-strong); background: var(--bg); color: var(--text); font: inherit; text-decoration: none; white-space: nowrap; cursor: pointer; box-sizing: border-box; }
.btn:hover:not(:disabled) { background: var(--bg-hover); }
.btn:disabled { color: var(--text-faint); cursor: default; }
.btn.watch-on, .btn.watch-on:hover { background: var(--selected-bg); border-color: var(--watch-on-border); color: var(--watch-on-fg); }
.badge { display: inline-flex; align-items: center; gap: 6px; padding: 2px 8px; border-radius: 999px; font-size: 12px; font-weight: 600; white-space: nowrap; }
.dot { width: 8px; height: 8px; border-radius: 50%; display: inline-block; flex: none; background: var(--stopped-dot); }
.badge.failing { background: var(--failing-bg); color: var(--failing-fg); }
.badge.running { background: var(--running-bg); color: var(--running-fg); }
.badge.warning { background: var(--warning-bg); color: var(--warning-fg); }
.failing .dot, .dot.failing { background: var(--failing-dot); }
.running .dot, .dot.running { background: var(--running-dot); }
.dot.other { background: var(--warning-dot); }
.chips-wrap { display: flex; flex-direction: column; gap: 8px; }
.label { font-size: 11px; font-weight: 600; letter-spacing: 0.04em; text-transform: uppercase; color: var(--text-muted); }
.chips { display: flex; flex-wrap: wrap; gap: 8px; }
.chip { display: inline-flex; align-items: center; gap: 6px; min-height: 30px; padding: 0 10px; border-radius: var(--radius-sm); border: 1px solid var(--line-strong); background: var(--bg); color: var(--text); font: inherit; font-size: 12px; cursor: pointer; }
.chip:hover { background: var(--bg-hover); }
.chip.on, .chip.on:hover { border-color: var(--accent-text); background: var(--selected-bg); }
.since { color: var(--text-muted); }
.tabs { display: flex; flex-wrap: wrap; border-bottom: 1px solid var(--line); }
.tab { min-height: 36px; padding: 0 4px; margin-right: 18px; border: 0; border-bottom: 2px solid transparent; background: transparent; font: inherit; color: var(--stopped-fg); cursor: pointer; }
.tab.on { border-bottom-color: var(--accent-text); color: var(--text); font-weight: 600; }
.grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(380px, 1fr)); gap: 16px; align-items: start; }
.col { display: flex; flex-direction: column; gap: 16px; min-width: 0; }
.card { border: 1px solid var(--line); border-radius: 10px; padding: 16px; display: flex; flex-direction: column; gap: 10px; }
.card-head { display: flex; flex-wrap: wrap; justify-content: space-between; align-items: center; gap: 8px; }
.alert { margin: 0; padding: 12px; border: 1px solid var(--failing-dot); border-radius: var(--radius-md); background: var(--failing-bg); color: var(--failing-fg); }
.detail-error { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 8px; margin-bottom: 16px; }
.alert p { margin: 4px 0 8px; user-select: text; }
</style>
