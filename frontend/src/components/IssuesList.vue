<script setup lang="ts">
// Every issue Visor has recorded (spec §5.4): stats, filters (status, kind,
// date, notified, search), multi-select with bulk resolve and delete, and
// paging back through history 50 at a time. Filters live in the URL query,
// like the log tab's. The list reloads when an issue opens or the open count
// changes.
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter, type RouteLocationRaw } from 'vue-router'
import { Events } from '@wailsio/runtime'
import { IssueService } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import type { IssueStats, IssueView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { VisorIssueOpened } from '../../bindings/events'
import ConfirmDialog from './ConfirmDialog.vue'
import { describeError } from '../lib/faults'
import { historyTime } from '../lib/format'
import { issueKind, lasted, statusLabel } from '../lib/issue-status'
import {
  DATE_OPTIONS,
  KIND_OPTIONS,
  allSelected,
  dateRange,
  deleteAsk,
  issueFiltersQuery,
  issues as issuesLabel,
  keepLoaded,
  parseIssueFilters,
  resolveAsk,
  selectionActions,
  toggleAll,
  type Ask,
  type DatePreset,
  type MsRange,
  type StatusFilter,
} from '../lib/issues-page'
import { configRoute } from '../lib/links'
import { useOverview } from '../lib/overview'
import { currentSettings, loadSettings } from '../lib/settings'
import { showToast } from '../lib/toast'

type Filter = StatusFilter
const PAGE = 50
const DEBOUNCE_MS = 200

const route = useRoute()
const router = useRouter()
const initial = parseIssueFilters(route.query)
const { overview } = useOverview()
const filter = ref<Filter>(initial.status)
const kind = ref(initial.kind)
const datePreset = ref<DatePreset>(initial.date)
const fromDay = ref(initial.from)
const toDay = ref(initial.to)
const notified = ref(initial.notified)
const typed = ref(initial.q)
const query = ref(initial.q)
const rows = ref<IssueView[]>([])
const stats = ref<IssueStats>({ Total: 0, Open: 0, Resolved: 0, Last24h: 0, Last7d: 0, Last30d: 0 })
const hasMore = ref(false)
const loaded = ref(false)
const loading = ref(false)
const loadError = ref('')
const nowMs = ref(Date.now())
const selected = ref<Set<string>>(new Set())
const pending = ref<{ ids: string[]; remove: boolean; ask: Ask } | null>(null)
const busy = ref(false)
const fetching = ref(0) // loads in flight, for aria-busy

let seq = 0 // a newer request makes older responses stale
let range: MsRange = { fromMs: 0, toMs: 0 } // the date range of the last load, reused to page
let debounce: ReturnType<typeof setTimeout> | undefined
let tick: ReturnType<typeof setInterval> | undefined
let offOpened: (() => void) | undefined

const filtered = computed(() => query.value !== '' || kind.value !== '' || datePreset.value !== 'any' || notified.value)

/**
 * load replaces the list: the first page, or (refresh) as many rows as are
 * already shown so a live update does not collapse "Load older".
 */
async function load(keep = 0) {
  fetching.value++
  try {
    await loadOnce(keep)
  } finally {
    fetching.value--
  }
}

async function loadOnce(keep: number) {
  const mine = ++seq
  const limit = Math.max(PAGE, keep)
  range = dateRange(datePreset.value, Date.now(), fromDay.value, toDay.value)
  // The list and the stats load independently: failing stats only leave
  // the strip and filter labels stale, never blank the list.
  const counted = IssueService.Stats().then(
    (c) => {
      if (mine === seq) stats.value = c
    },
    () => {}, // keep the last stats
  )
  try {
    const list = (await IssueService.List({ Status: filter.value, Kind: kind.value, Notified: notified.value, FromMs: range.fromMs, ToMs: range.toMs, BeforeMs: 0, BeforeID: '', Limit: limit, Query: query.value })) ?? []
    if (mine !== seq) return
    rows.value = list
    selected.value = keepLoaded(list, selected.value)
    hasMore.value = list.length >= limit
    loadError.value = ''
  } catch (e) {
    if (mine === seq) loadError.value = describeError(e)
  } finally {
    if (mine === seq) loaded.value = true
  }
  await counted
}

async function older() {
  const last = rows.value[rows.value.length - 1]
  if (!last || loading.value) return
  const mine = seq
  loading.value = true
  try {
    const list = (await IssueService.List({ Status: filter.value, Kind: kind.value, Notified: notified.value, FromMs: range.fromMs, ToMs: range.toMs, BeforeMs: last.OpenedMs, BeforeID: last.ID, Limit: PAGE, Query: query.value })) ?? []
    if (mine !== seq) return // filter or search changed meanwhile
    const have = new Set(rows.value.map((r) => r.ID))
    rows.value = [...rows.value, ...list.filter((r) => !have.has(r.ID))]
    hasMore.value = list.length >= PAGE
  } catch (e) {
    showToast(describeError(e), 'error')
  } finally {
    loading.value = false
  }
}

const refresh = () => load(rows.value.length)

function syncUrl() {
  const q = issueFiltersQuery({ status: filter.value, kind: kind.value, date: datePreset.value, from: fromDay.value, to: toDay.value, q: query.value, notified: notified.value })
  void router.replace({ query: q })
}

/** A filter changed: the old selection no longer describes what is shown. */
function reload() {
  syncUrl()
  selected.value = new Set()
  void load()
}

function pick(f: Filter) {
  if (f === filter.value) return
  filter.value = f
  reload()
}

watch(typed, (v) => {
  clearTimeout(debounce)
  debounce = setTimeout(() => {
    if (v.trim() === query.value) return
    query.value = v.trim()
    reload()
  }, DEBOUNCE_MS)
})
watch([kind, notified], reload)
watch(datePreset, (p) => {
  // Choosing "Custom range…" waits for a day; every other choice loads now.
  if (p !== 'custom' || fromDay.value || toDay.value) reload()
  else syncUrl()
})
watch([fromDay, toDay], () => {
  if (datePreset.value === 'custom') reload()
})

// The open count moves when issues open or close by themselves.
watch(
  () => overview.value?.Stats.OpenIssues,
  (n, was) => {
    if (loaded.value && was !== undefined && n !== was) void refresh()
  },
)

onMounted(() => {
  void load()
  loadSettings().catch(() => {}) // without it, the retention line stays hidden
  tick = setInterval(() => (nowMs.value = Date.now()), 10_000)
  offOpened = Events.On(VisorIssueOpened, () => void refresh())
})
onBeforeUnmount(() => {
  clearTimeout(debounce)
  clearInterval(tick)
  offOpened?.()
  seq++ // drop in-flight responses
})

async function resolve(i: IssueView) {
  try {
    await IssueService.Resolve(i.ID)
  } catch (e) {
    showToast(describeError(e), 'error')
  }
  void refresh()
}

// Selection.
const actions = computed(() => selectionActions(rows.value, selected.value))
const everyRow = computed(() => allSelected(rows.value, selected.value))
const someRows = computed(() => selected.value.size > 0 && !everyRow.value)
function toggle(id: string) {
  const next = new Set(selected.value)
  if (!next.delete(id)) next.add(id)
  selected.value = next
}
const toggleEvery = () => (selected.value = toggleAll(rows.value, selected.value))
const clearSelection = () => (selected.value = new Set())
const setIndeterminate = (el: unknown) => {
  if (el instanceof HTMLInputElement) el.indeterminate = someRows.value
}

function askResolve() {
  const ids = actions.value.resolve
  if (ids.length) pending.value = { ids, remove: false, ask: resolveAsk(ids.length) }
}
function askDelete(ids: string[], single = false) {
  if (ids.length) pending.value = { ids, remove: true, ask: deleteAsk(ids.length, single) }
}

async function confirmPending() {
  const p = pending.value
  if (!p || busy.value) return
  busy.value = true
  pending.value = null
  try {
    const n = p.remove ? await IssueService.Delete(p.ids) : await IssueService.ResolveMany(p.ids)
    showToast(`${p.remove ? 'Deleted' : 'Resolved'} ${issuesLabel(n)}`)
    const next = new Set(selected.value)
    p.ids.forEach((id) => next.delete(id))
    selected.value = next
  } catch (e) {
    showToast(describeError(e), 'error')
  } finally {
    busy.value = false
  }
  void refresh()
}

const configs = computed(() => overview.value?.Configs ?? [])
const watched = (i: IssueView) => configs.value.find((c) => c.Path === i.Config)?.Watched ?? true
function detailTo(i: IssueView): RouteLocationRaw | null {
  if (!i.Process || !i.Config) return null
  return configRoute(i.Config, { tab: 'logs', process: i.Process })
}

const filters = computed(() => [
  { key: 'open' as const, label: `Open (${stats.value.Open})` },
  { key: 'resolved' as const, label: `Resolved (${stats.value.Resolved})` },
  { key: 'all' as const, label: 'All' },
])
const tiles = computed(() => [
  { label: 'Total', n: stats.value.Total },
  { label: 'Open', n: stats.value.Open },
  { label: 'Resolved', n: stats.value.Resolved },
  { label: 'Last 24 hours', n: stats.value.Last24h },
  { label: 'Last 7 days', n: stats.value.Last7d },
  { label: 'Last 30 days', n: stats.value.Last30d },
])
const total = computed(() => ({ open: stats.value.Open, resolved: stats.value.Resolved, all: stats.value.Total })[filter.value])
const note = computed(() => {
  const n = rows.value.length
  if (filtered.value) return `${n} matching ${n === 1 ? 'issue' : 'issues'}`
  if (filter.value === 'open') return `Showing ${n} open ${n === 1 ? 'issue' : 'issues'}`
  return `Showing ${n} of ${total.value} issues`
})
const empty = computed(() => {
  if (filtered.value) return 'No issues match your filters.'
  return { open: 'No open issues.', resolved: 'No resolved issues yet.', all: 'No issues yet.' }[filter.value]
})
</script>

<template>
  <div class="list">
    <p class="intro">
      An issue opens when a process goes FATAL, exits when it should not, or keeps restarting. FATAL issues close by themselves once the process runs steadily again; you can also mark any issue resolved.
    </p>

    <dl class="stats" aria-label="Issue totals">
      <div v-for="t in tiles" :key="t.label" class="tile">
        <dt>{{ t.label }}</dt>
        <dd>{{ t.n }}</dd>
      </div>
    </dl>

    <div class="tools">
      <div class="seg" role="group" aria-label="Filter">
        <button v-for="f in filters" :key="f.key" type="button" :class="{ on: filter === f.key }" :aria-pressed="filter === f.key" @click="pick(f.key)">{{ f.label }}</button>
      </div>
      <label for="ik" class="sr-only">Kind</label>
      <select id="ik" v-model="kind">
        <option v-for="o in KIND_OPTIONS" :key="o.value" :value="o.value">{{ o.label }}</option>
      </select>
      <label for="id" class="sr-only">Date</label>
      <select id="id" v-model="datePreset">
        <option v-for="o in DATE_OPTIONS" :key="o.value" :value="o.value">{{ o.label }}</option>
      </select>
      <template v-if="datePreset === 'custom'">
        <label for="idf" class="sr-only">From date</label>
        <input id="idf" v-model="fromDay" type="date" :max="toDay || undefined" />
        <span class="muted" aria-hidden="true">to</span>
        <label for="idt" class="sr-only">To date</label>
        <input id="idt" v-model="toDay" type="date" :min="fromDay || undefined" />
      </template>
      <label class="check" title="Only issues Visor sent a notification for"><input v-model="notified" type="checkbox" /> Notified only</label>
      <label for="iq" class="sr-only">Search issues</label>
      <input id="iq" v-model="typed" type="search" placeholder="Search issues" autocomplete="off" />
    </div>

    <div v-if="selected.size" class="bulk" role="region" aria-label="Selected issues">
      <span>{{ selected.size }} selected</span>
      <button type="button" class="btn" :disabled="!actions.resolve.length || busy" @click="askResolve">Resolve {{ actions.resolve.length }}</button>
      <button type="button" class="btn danger-text" :disabled="!actions.remove.length || busy" @click="askDelete(actions.remove)">Delete {{ actions.remove.length }}</button>
      <button type="button" class="btn" @click="clearSelection">Clear selection</button>
    </div>

    <div v-if="loadError" class="error" role="alert">
      <strong>Could not load issues.</strong>
      <p>{{ loadError }}</p>
      <button type="button" class="btn" @click="load()">Try again</button>
    </div>

    <div v-else class="box">
      <div class="scroll">
        <div class="table" :aria-busy="!loaded || fetching > 0">
          <div class="row head label">
            <span class="pick"><input :ref="setIndeterminate" type="checkbox" aria-label="Select all loaded issues" :checked="everyRow" :disabled="!rows.length" @change="toggleEvery" /></span>
            <span>Kind</span><span>Process · what happened</span><span>Started</span><span>Lasted</span><span>Status</span><span />
          </div>
          <p v-if="loaded && !rows.length" class="none">{{ empty }}</p>
          <div v-for="i in rows" :key="i.ID" :class="['row', { picked: selected.has(i.ID) }]">
            <span class="pick"><input type="checkbox" :aria-label="`Select ${i.Process || 'supervisord'} issue`" :checked="selected.has(i.ID)" @change="toggle(i.ID)" /></span>
            <span><span :class="['badge', issueKind(i.Kind).cls]"><span class="dot" aria-hidden="true" />{{ issueKind(i.Kind).label }}</span></span>
            <div class="what">
              <div>
                <span class="proc">{{ i.Process || 'supervisord' }}</span>
                <span class="muted" :title="i.Config"> · {{ i.Config ? i.Config.split('/').pop() : i.Process ? 'config removed' : 'socket' }}</span>
              </div>
              <div class="msg" :title="i.Note ? `${i.Message} (${i.Note})` : i.Message">{{ i.Message }}</div>
            </div>
            <span class="soft">{{ historyTime(i.OpenedMs, nowMs) }}</span>
            <span class="soft">{{ lasted(i, nowMs) }}</span>
            <div class="soft status">
              <div>{{ statusLabel(i, watched(i)) }}</div>
              <div v-if="i.Notified" class="notified" title="Visor sent a notification">
                <svg viewBox="0 0 16 16" width="12" height="12" aria-hidden="true"><path fill="currentColor" d="M8 1.5a4 4 0 0 0-4 4v2.3L2.8 10.2a.8.8 0 0 0 .7 1.1h9a.8.8 0 0 0 .7-1.1L12 7.8V5.5a4 4 0 0 0-4-4Zm-1.6 11a1.6 1.6 0 0 0 3.2 0H6.4Z" /></svg>
                Notified
              </div>
            </div>
            <div class="acts">
              <RouterLink v-if="detailTo(i)" class="btn" :to="detailTo(i)!">Logs</RouterLink>
              <button v-if="i.Status === 'open'" type="button" class="btn" @click="resolve(i)">Resolve</button>
              <button v-else type="button" class="btn" :disabled="busy" @click="askDelete([i.ID], true)">Delete</button>
            </div>
          </div>
          <div class="foot">
            <span>{{ note }}</span>
            <button v-if="hasMore" type="button" class="btn" :disabled="loading" @click="older">Load older</button>
          </div>
        </div>
      </div>
    </div>
    <p v-if="currentSettings" class="muted">
      Resolved issues are kept on this Mac for {{ currentSettings.Settings.retentionDays }} days.
      <RouterLink to="/settings">Change in Settings</RouterLink>
    </p>
    <ConfirmDialog
      v-if="pending"
      :title="pending.ask.title"
      :message="pending.ask.message"
      :confirm-label="pending.ask.confirmLabel"
      :danger="pending.remove"
      @confirm="confirmPending"
      @cancel="pending = null"
    />
  </div>
</template>

<style scoped>
.list { display: flex; flex-direction: column; gap: 16px; }
.stats { display: grid; grid-template-columns: repeat(auto-fit, minmax(120px, 1fr)); gap: 10px; margin: 0; }
.tile { padding: 10px 12px; border: 1px solid var(--line); border-radius: 10px; }
.tile dt { font-size: 11px; font-weight: 600; letter-spacing: 0.04em; text-transform: uppercase; color: var(--text-muted); }
.tile dd { margin: 2px 0 0; font-size: 22px; font-weight: 650; font-variant-numeric: tabular-nums; }
input[type='date'] { min-height: 32px; box-sizing: border-box; padding: 0 8px; border: 1px solid var(--line-strong); border-radius: var(--radius-sm); background: var(--bg); color: var(--text); font: inherit; }
.bulk { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; padding: 8px 12px; border: 1px solid var(--watch-on-border); border-radius: 10px; background: var(--selected-bg); }
.bulk > span { margin-right: 6px; font-weight: 600; }
.danger-text { color: var(--failing-fg); }
.pick { display: flex; align-items: center; }
.check { display: inline-flex; align-items: center; gap: 6px; cursor: pointer; }
.picked { background: var(--selected-bg); }
.status .notified { display: inline-flex; align-items: center; gap: 4px; font-size: 12px; color: var(--text-muted); }
.intro { margin: 0; max-width: 640px; color: var(--text-muted); }
.tools { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; }
.seg { display: flex; gap: 3px; padding: 3px; background: var(--seg-bg); border-radius: 8px; }
.seg button { min-height: 28px; padding: 0 12px; border: 0; border-radius: 6px; background: transparent; font: inherit; color: var(--stopped-fg); cursor: pointer; }
.seg button.on { background: var(--seg-on); color: var(--text); box-shadow: 0 1px 2px rgba(0, 0, 0, 0.12); }
input[type='search'] { min-height: 32px; width: 200px; box-sizing: border-box; padding: 0 10px; border: 1px solid var(--line-strong); border-radius: var(--radius-sm); background: var(--bg); color: var(--text); font: inherit; }
.box { border: 1px solid var(--line); border-radius: 10px; overflow: hidden; }
.scroll { overflow-x: auto; }
/* Columns (24 + 120 + 200 + 130 + 90 + 130 + 150), 6 gaps and the padding: 950px. */
.table { min-width: 950px; }
.row { display: grid; grid-template-columns: 24px 120px minmax(200px, 1fr) 130px 90px 130px 150px; align-items: center; gap: 12px; padding: 10px 14px; border-top: 1px solid var(--line); }
.head { border-top: 0; background: var(--bg-subtle); }
.label { font-size: 11px; font-weight: 600; letter-spacing: 0.04em; text-transform: uppercase; color: var(--text-muted); }
.what { min-width: 0; }
.proc { font-family: var(--font-mono); font-size: 12px; font-weight: 600; }
.msg { color: var(--stopped-fg); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.soft { color: var(--stopped-fg); }
.muted { margin: 0; color: var(--text-muted); }
.muted a { color: var(--accent-text); }
.none { margin: 0; padding: 14px; border-top: 1px solid var(--line); color: var(--text-muted); }
.acts { display: flex; gap: 6px; justify-content: flex-end; }
.foot { display: flex; justify-content: space-between; align-items: center; padding: 12px 14px; border-top: 1px solid var(--line); color: var(--text-muted); }
.btn { display: inline-flex; align-items: center; min-height: 32px; padding: 0 12px; border-radius: var(--radius-sm); border: 1px solid var(--line-strong); background: var(--bg); color: var(--text); font: inherit; text-decoration: none; white-space: nowrap; cursor: pointer; }
.btn:hover:not(:disabled) { background: var(--bg-hover); }
.btn:disabled { color: var(--text-faint); cursor: default; }
.badge { display: inline-flex; align-items: center; gap: 6px; padding: 2px 8px; border-radius: 999px; font-size: 12px; font-weight: 600; white-space: nowrap; }
.dot { width: 8px; height: 8px; border-radius: 50%; flex: none; }
.failing { background: var(--failing-bg); color: var(--failing-fg); }
.failing .dot { background: var(--failing-dot); }
.warning { background: var(--warning-bg); color: var(--warning-fg); }
.warning .dot { background: var(--warning-dot); }
.stopped { background: var(--stopped-bg); color: var(--stopped-fg); }
.stopped .dot { background: var(--stopped-dot); }
.error { padding: 12px; border: 1px solid var(--failing-dot); border-radius: var(--radius-md); background: var(--failing-bg); color: var(--failing-fg); }
.error p { margin: 4px 0 8px; user-select: text; }
.sr-only { position: absolute; left: -9999px; }
</style>
