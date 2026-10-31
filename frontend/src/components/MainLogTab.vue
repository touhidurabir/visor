<script setup lang="ts">
// supervisord's own log, parsed: level chips, date range and search, newest
// lines last. Filters live in the URL query, so a reload or a shared link
// keeps them. Follow re-reads every 2 seconds while the tab is open and the
// window is visible.
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ServerService } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import type { MainLogLineView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { describeError } from '../lib/faults'
import { DATE_OPTIONS, dateRange, type DatePreset } from '../lib/issues-page'
import { DEFAULT_GROUPS, LEVEL_GROUPS, footerNote, groupsParam, levelClass, levelsFor, logTime, parseGroups, type LevelGroup } from '../lib/main-log'

const FOLLOW_MS = 2000
const DEBOUNCE_MS = 200
const PRESETS = DATE_OPTIONS.map((o) => o.value)

const route = useRoute()
const router = useRouter()
const str = (v: unknown) => (typeof v === 'string' ? v : '')

const groups = ref<LevelGroup[]>(parseGroups(route.query.lv))
const datePreset = ref<DatePreset>(PRESETS.includes(str(route.query.d) as DatePreset) ? (str(route.query.d) as DatePreset) : 'any')
const fromDay = ref(str(route.query.from))
const toDay = ref(str(route.query.to))
const typed = ref(str(route.query.q))
const query = ref(typed.value.trim())
const follow = ref(false)
const lines = ref<MainLogLineView[]>([])
const matched = ref(0)
const truncated = ref(false)
const loaded = ref(false)
const fetching = ref(0)
const error = ref('')
const scroller = ref<HTMLElement>()

let seq = 0
let debounce: ReturnType<typeof setTimeout> | undefined
let timer: ReturnType<typeof setInterval> | undefined

const nothingPicked = () => groups.value.length === 0

async function load(stick = true) {
  const mine = ++seq
  if (nothingPicked()) {
    lines.value = []
    matched.value = 0
    error.value = ''
    loaded.value = true
    return
  }
  const r = dateRange(datePreset.value, Date.now(), fromDay.value, toDay.value)
  fetching.value++
  try {
    const res = await ServerService.MainLogLines({ Levels: levelsFor(groups.value), FromMs: r.fromMs, ToMs: r.toMs, Query: query.value, Limit: 2000 })
    if (mine !== seq) return
    const el = scroller.value
    const atEnd = !el || el.scrollHeight - el.scrollTop - el.clientHeight < 24
    lines.value = res.Lines ?? []
    matched.value = res.Matched
    truncated.value = res.Truncated
    error.value = ''
    if (stick && (atEnd || !loaded.value)) {
      await nextTick()
      if (scroller.value) scroller.value.scrollTop = scroller.value.scrollHeight
    }
  } catch (e) {
    if (mine === seq) error.value = describeError(e)
  } finally {
    fetching.value--
    if (mine === seq) loaded.value = true
  }
}

function syncUrl() {
  const q: Record<string, string> = {}
  if (groupsParam(groups.value) !== groupsParam(DEFAULT_GROUPS)) q.lv = groupsParam(groups.value) || 'none'
  if (datePreset.value !== 'any') q.d = datePreset.value
  if (datePreset.value === 'custom') {
    if (fromDay.value) q.from = fromDay.value
    if (toDay.value) q.to = toDay.value
  }
  if (query.value) q.q = query.value
  void router.replace({ query: q })
}

function changed() {
  syncUrl()
  void load()
}

function toggleGroup(g: LevelGroup) {
  groups.value = groups.value.includes(g) ? groups.value.filter((x) => x !== g) : [...groups.value, g]
  changed()
}

watch(typed, (v) => {
  clearTimeout(debounce)
  debounce = setTimeout(() => {
    if (v.trim() === query.value) return
    query.value = v.trim()
    changed()
  }, DEBOUNCE_MS)
})
watch(datePreset, (p) => {
  if (p !== 'custom' || fromDay.value || toDay.value) changed()
  else syncUrl()
})
watch([fromDay, toDay], () => {
  if (datePreset.value === 'custom') changed()
})

// Follow polls only while it is on and the window is visible.
function stopTimer() {
  clearInterval(timer)
  timer = undefined
}
function syncTimer() {
  stopTimer()
  if (follow.value && document.visibilityState === 'visible') timer = setInterval(() => void load(), FOLLOW_MS)
}
function onVisibility() {
  if (document.visibilityState === 'visible' && follow.value) void load()
  syncTimer()
}
watch(follow, (on) => {
  syncTimer()
  if (on) void load()
})

onMounted(() => {
  // A "none" level chip set (all chips off) round-trips as lv=none.
  if (route.query.lv === 'none') groups.value = []
  document.addEventListener('visibilitychange', onVisibility)
  void load()
})
onBeforeUnmount(() => {
  document.removeEventListener('visibilitychange', onVisibility)
  clearTimeout(debounce)
  stopTimer()
  seq++
})
</script>

<template>
  <div class="logtab">
    <div class="tools">
      <div class="seg" role="group" aria-label="Levels">
        <button v-for="g in LEVEL_GROUPS" :key="g.key" type="button" :class="{ on: groups.includes(g.key) }" :aria-pressed="groups.includes(g.key)" @click="toggleGroup(g.key)">{{ g.label }}</button>
      </div>
      <label for="ld" class="sr-only">Date</label>
      <select id="ld" v-model="datePreset">
        <option v-for="o in DATE_OPTIONS" :key="o.value" :value="o.value">{{ o.label }}</option>
      </select>
      <template v-if="datePreset === 'custom'">
        <label for="ldf" class="sr-only">From date</label>
        <input id="ldf" v-model="fromDay" type="date" :max="toDay || undefined" />
        <span class="muted" aria-hidden="true">to</span>
        <label for="ldt" class="sr-only">To date</label>
        <input id="ldt" v-model="toDay" type="date" :min="fromDay || undefined" />
      </template>
      <label for="lq" class="sr-only">Search the log</label>
      <input id="lq" v-model="typed" type="search" placeholder="Search the log" autocomplete="off" />
      <button type="button" class="btn" :disabled="fetching > 0" @click="load()">Refresh</button>
      <label class="follow"><input v-model="follow" type="checkbox" /> Follow</label>
    </div>

    <div v-if="error" class="notice" role="alert">
      <strong>Can’t show the supervisord log.</strong>
      <p>{{ error }}</p>
      <button type="button" class="btn" @click="load()">Try again</button>
    </div>
    <div v-else class="box">
      <div ref="scroller" class="scroll" tabindex="0" aria-label="supervisord log lines" :aria-busy="!loaded || fetching > 0">
        <p v-if="loaded && nothingPicked()" class="none">Pick at least one level to see lines.</p>
        <p v-else-if="loaded && !lines.length" class="none">No log lines match your filters.</p>
        <div v-for="(l, i) in lines" :key="i" class="line">
          <time class="at">{{ logTime(l.AtMs) }}</time>
          <span :class="['lvl', levelClass(l.Level)]">{{ l.Level }}</span>
          <span class="text">{{ l.Text }}</span>
        </div>
      </div>
      <div class="foot">
        <span>{{ footerNote(lines.length, matched) }}</span>
        <span v-if="truncated">Only the newest 4 MiB of the log were read; older lines are not searched.</span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.logtab { display: flex; flex-direction: column; gap: 14px; }
.tools { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; }
.seg { display: flex; gap: 3px; padding: 3px; background: var(--seg-bg); border-radius: 8px; }
.seg button { min-height: 28px; padding: 0 12px; border: 0; border-radius: 6px; background: transparent; font: inherit; color: var(--stopped-fg); cursor: pointer; }
.seg button.on { background: var(--seg-on); color: var(--text); box-shadow: 0 1px 2px rgba(0, 0, 0, 0.12); }
select, input[type='date'], input[type='search'] { min-height: 32px; box-sizing: border-box; padding: 0 8px; border: 1px solid var(--line-strong); border-radius: var(--radius-sm); background: var(--bg); color: var(--text); font: inherit; }
input[type='search'] { width: 200px; padding: 0 10px; }
.follow { display: inline-flex; align-items: center; gap: 6px; cursor: pointer; }
.muted { color: var(--text-muted); }
.btn { display: inline-flex; align-items: center; min-height: 32px; padding: 0 12px; border-radius: var(--radius-sm); border: 1px solid var(--line-strong); background: var(--bg); color: var(--text); font: inherit; white-space: nowrap; cursor: pointer; }
.btn:hover:not(:disabled) { background: var(--bg-hover); }
.btn:disabled { color: var(--text-faint); cursor: default; }
.box { border: 1px solid var(--line); border-radius: 10px; overflow: hidden; }
.scroll { max-height: calc(100vh - 300px); min-height: 240px; overflow: auto; background: var(--bg); }
.line { display: grid; grid-template-columns: 124px 48px minmax(0, 1fr); gap: 10px; align-items: baseline; padding: 4px 12px; border-top: 1px solid var(--line); font-family: var(--font-mono); font-size: 12px; line-height: 1.5; }
.line:first-child { border-top: 0; }
.at { color: var(--text-muted); white-space: nowrap; }
.text { white-space: pre-wrap; overflow-wrap: anywhere; user-select: text; }
.lvl { justify-self: start; padding: 0 6px; border-radius: 999px; font-size: 11px; font-weight: 600; }
.lvl.err { background: var(--failing-bg); color: var(--failing-fg); }
.lvl.warn { background: var(--warning-bg); color: var(--warning-fg); }
.lvl.info { background: var(--stopped-bg); color: var(--stopped-fg); }
.lvl.debug { background: transparent; color: var(--text-muted); border: 1px solid var(--line); }
.none { margin: 0; padding: 14px; color: var(--text-muted); }
.foot { display: flex; flex-wrap: wrap; justify-content: space-between; gap: 6px 16px; padding: 10px 14px; border-top: 1px solid var(--line); color: var(--text-muted); }
.notice { padding: 12px; border: 1px solid var(--line-strong); border-radius: var(--radius-md); background: var(--bg-subtle); }
.notice p { margin: 4px 0 8px; user-select: text; }
.sr-only { position: absolute; left: -9999px; }
</style>
