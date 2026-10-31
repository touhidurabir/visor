<script setup lang="ts">
// Every issue Visor has recorded (spec §5.4): filter by open/resolved, search,
// resolve open ones, and page back through history 50 at a time. The list
// reloads when an issue opens or the open count changes.
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RouterLink, type RouteLocationRaw } from 'vue-router'
import { Events } from '@wailsio/runtime'
import { IssueService } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import type { IssueCounts, IssueView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { VisorIssueOpened } from '../../bindings/events'
import { describeError } from '../lib/faults'
import { historyTime } from '../lib/format'
import { issueKind, lasted, statusLabel } from '../lib/issue-status'
import { configRoute } from '../lib/links'
import { useOverview } from '../lib/overview'
import { currentSettings, loadSettings } from '../lib/settings'
import { showToast } from '../lib/toast'

type Filter = 'open' | 'resolved' | 'all'
const PAGE = 50
const DEBOUNCE_MS = 200

const { overview } = useOverview()
const filter = ref<Filter>('open')
const typed = ref('')
const query = ref('')
const rows = ref<IssueView[]>([])
const counts = ref<IssueCounts>({ Open: 0, Resolved: 0 })
const hasMore = ref(false)
const loaded = ref(false)
const loading = ref(false)
const loadError = ref('')
const nowMs = ref(Date.now())

let seq = 0 // a newer request makes older responses stale
let debounce: ReturnType<typeof setTimeout> | undefined
let tick: ReturnType<typeof setInterval> | undefined
let offOpened: (() => void) | undefined

/**
 * load replaces the list: the first page, or (refresh) as many rows as are
 * already shown so a live update does not collapse "Load older".
 */
async function load(keep = 0) {
  const mine = ++seq
  const limit = Math.max(PAGE, keep)
  // The list and the counts load independently: failing counts only leave
  // the filter labels stale, never blank the list.
  const counted = IssueService.Counts().then(
    (c) => {
      if (mine === seq) counts.value = c
    },
    () => {}, // keep the last counts
  )
  try {
    const list = (await IssueService.List({ Status: filter.value, Kind: '', FromMs: 0, ToMs: 0, BeforeMs: 0, BeforeID: '', Limit: limit, Query: query.value })) ?? []
    if (mine !== seq) return
    rows.value = list
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
    const list = (await IssueService.List({ Status: filter.value, Kind: '', FromMs: 0, ToMs: 0, BeforeMs: last.OpenedMs, BeforeID: last.ID, Limit: PAGE, Query: query.value })) ?? []
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

function pick(f: Filter) {
  if (f === filter.value) return
  filter.value = f
  void load()
}

watch(typed, (v) => {
  clearTimeout(debounce)
  debounce = setTimeout(() => {
    if (v.trim() === query.value) return
    query.value = v.trim()
    void load()
  }, DEBOUNCE_MS)
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

const configs = computed(() => overview.value?.Configs ?? [])
const watched = (i: IssueView) => configs.value.find((c) => c.Path === i.Config)?.Watched ?? true
function detailTo(i: IssueView): RouteLocationRaw | null {
  if (!i.Process || !i.Config) return null
  return configRoute(i.Config, { tab: 'logs', process: i.Process })
}

const filters = computed(() => [
  { key: 'open' as const, label: `Open (${counts.value.Open})` },
  { key: 'resolved' as const, label: `Resolved (${counts.value.Resolved})` },
  { key: 'all' as const, label: 'All' },
])
const total = computed(() => ({ open: counts.value.Open, resolved: counts.value.Resolved, all: counts.value.Open + counts.value.Resolved })[filter.value])
const note = computed(() => {
  const n = rows.value.length
  if (query.value) return `${n} matching ${n === 1 ? 'issue' : 'issues'}`
  if (filter.value === 'open') return `Showing ${n} open ${n === 1 ? 'issue' : 'issues'}`
  return `Showing ${n} of ${total.value} issues`
})
const empty = computed(() => {
  if (query.value) return 'No issues match your search.'
  return { open: 'No open issues.', resolved: 'No resolved issues yet.', all: 'No issues yet.' }[filter.value]
})
</script>

<template>
  <section class="page">
    <header>
      <div>
        <h1>Issues</h1>
        <p class="intro">
          An issue opens when a process goes FATAL, exits when it should not, or keeps restarting. FATAL issues close by themselves once the process runs steadily again; you can also mark any issue resolved.
        </p>
      </div>
      <div class="tools">
        <div class="seg" role="group" aria-label="Filter">
          <button v-for="f in filters" :key="f.key" type="button" :class="{ on: filter === f.key }" :aria-pressed="filter === f.key" @click="pick(f.key)">{{ f.label }}</button>
        </div>
        <label for="iq" class="sr-only">Search issues</label>
        <input id="iq" v-model="typed" type="search" placeholder="Search issues" autocomplete="off" />
      </div>
    </header>

    <div v-if="loadError" class="error" role="alert">
      <strong>Could not load issues.</strong>
      <p>{{ loadError }}</p>
      <button type="button" class="btn" @click="load()">Try again</button>
    </div>

    <div v-else class="box">
      <div class="scroll">
        <div class="table">
          <div class="row head label"><span>Kind</span><span>Process · what happened</span><span>Started</span><span>Lasted</span><span>Status</span><span /></div>
          <p v-if="loaded && !rows.length" class="none">{{ empty }}</p>
          <div v-for="i in rows" :key="i.ID" class="row">
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
            <span class="soft">{{ statusLabel(i, watched(i)) }}</span>
            <div class="acts">
              <RouterLink v-if="detailTo(i)" class="btn" :to="detailTo(i)!">Logs</RouterLink>
              <button v-if="i.Status === 'open'" type="button" class="btn" @click="resolve(i)">Resolve</button>
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
  </section>
</template>

<style scoped>
.page { display: flex; flex-direction: column; gap: 18px; }
header { display: flex; flex-wrap: wrap; align-items: flex-end; justify-content: space-between; gap: 12px; }
h1 { margin: 0; font-size: 22px; font-weight: 650; }
.intro { margin: 4px 0 0; max-width: 640px; color: var(--text-muted); }
.tools { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; }
.seg { display: flex; gap: 3px; padding: 3px; background: var(--seg-bg); border-radius: 8px; }
.seg button { min-height: 28px; padding: 0 12px; border: 0; border-radius: 6px; background: transparent; font: inherit; color: var(--stopped-fg); cursor: pointer; }
.seg button.on { background: var(--seg-on); color: var(--text); box-shadow: 0 1px 2px rgba(0, 0, 0, 0.12); }
input[type='search'] { min-height: 32px; width: 200px; box-sizing: border-box; padding: 0 10px; border: 1px solid var(--line-strong); border-radius: var(--radius-sm); background: var(--bg); color: var(--text); font: inherit; }
.box { border: 1px solid var(--line); border-radius: 10px; overflow: hidden; }
.scroll { overflow-x: auto; }
.table { min-width: 940px; }
.row { display: grid; grid-template-columns: 120px minmax(220px, 2.2fr) 150px 110px 150px 150px; align-items: center; gap: 12px; padding: 10px 14px; border-top: 1px solid var(--line); }
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
