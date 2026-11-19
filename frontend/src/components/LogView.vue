<script setup lang="ts">
// A live log (spec §5.3 Logs tab): stdout/stderr switch, Pause/Resume
// (output keeps arriving while paused and shows on resume), level marks
// and filters, search highlight over the loaded lines (and, when Settings
// allows, a search of the whole file), Clear (with confirmation), Save…
// and Open in editor. The Go side polls supervisord; this view only applies
// visor:log chunks. It stops following when it unmounts.
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import { Events } from '@wailsio/runtime'
import { type LogChunkView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { useApi } from '../lib/api'
import { currentTab, LOCAL } from '../lib/tabs'
import { withConsent } from '../lib/production'
import { VisorLog } from '../../bindings/events'
import { describeError } from '../lib/faults'
import { onMenu } from '../lib/menu'
import { applyChunk, countMatches, emptyLog, highlight, levelCounts, levelOf, LEVELS, markedLines, MAX_MARKS, noLogMessage, onlyLevels, shownLines, sizeText, WHOLE_LIMIT, type Level, type Stream } from '../lib/logview'
import { currentSettings, loadSettings } from '../lib/settings'
import { showToast } from '../lib/toast'
import ConfirmDialog from './ConfirmDialog.vue'

// Bound to this view's tab for its whole life (a follow it stops is that tab's).
const { LogService, ServerService } = useApi()
// A server's log files are on the server: no editor here (Save… works).
const local = currentTab.value === LOCAL

// running: supervisord clears the logs of a running process only; for any
// other it answers success and leaves the files alone.
const props = defineProps<{ process: string; stdoutLog: string; stderrLog: string; initialStream: Stream; running: boolean }>()

const stream = ref<Stream>(props.initialStream)
const paused = ref(false)
const query = ref('')
/** query as searched: set once typing pauses, so each key doesn't redraw the log. */
const q = ref('')
const live = shallowRef(emptyLog())
const frozen = shallowRef(emptyLog())
const loading = ref(true)
const noLog = ref<string | null>(null)
const error = ref<string | null>(null)
/** Set when the Go side gave up after repeated poll failures. */
const lost = ref<string | null>(null)
const confirmClear = ref(false)
const saving = ref(false)
const pre = ref<HTMLElement>()
const search = ref<HTMLInputElement>()

let followId: string | null = null
let early: LogChunkView[] = [] // chunks that arrived before Follow returned its ID
let gen = 0
let stick = true // keep the view scrolled to the newest line

const all = computed(() => shownLines(paused.value ? frozen.value : live.value))
const waiting = computed(() => (paused.value ? live.value.next - frozen.value.next : 0))

// Level filters: none picked shows every line. A chip shows while its level
// is in the loaded lines, or while it is picked.
const levels = shallowRef<ReadonlySet<Level>>(new Set())
const counts = computed(() => levelCounts(all.value))
const chips = computed(() => LEVELS.filter((l) => counts.value[l] > 0 || levels.value.has(l)))
const LEVEL_NAMES: Record<Level, string> = { error: 'Errors', warn: 'Warnings', info: 'Info', debug: 'Debug' }
function toggleLevel(l: Level) {
  const next = new Set(levels.value)
  if (!next.delete(l)) next.add(l)
  levels.value = next
}

const shown = computed(() => onlyLevels(all.value, levels.value))
const matches = computed(() => countMatches(shown.value, q.value))
const marked = computed(() => markedLines(shown.value, q.value))

// Whole-file search (Settings: off by default): Go reads the log 1 MiB at a
// time from the end, so the newest matches come first and Stop is between
// steps.
const wholeAllowed = computed(() => currentSettings.value?.Settings.searchWholeLog ?? false)
interface Whole {
  query: string
  lines: string[]
  size: number
  read: number
  running: boolean
  stopped: boolean
  capped: boolean
  error: string | null
}
const whole = shallowRef<Whole | null>(null)
let wholeGen = 0

async function searchWhole() {
  const text = query.value
  if (!text || !wholeAllowed.value) return
  const my = ++wholeGen
  let w: Whole = { query: text, lines: [], size: 0, read: 0, running: true, stopped: false, capped: false, error: null }
  whole.value = w
  let before = -1
  try {
    for (;;) {
      const p = await LogService.Search(props.process, stream.value, text, before, WHOLE_LIMIT - w.lines.length)
      if (my !== wholeGen) return
      const size = before < 0 ? p.Size : w.size
      w = { ...w, lines: [...w.lines, ...(p.Lines ?? [])], size, read: size - p.Next, capped: p.Capped }
      whole.value = w
      if (p.Next === 0 || p.Capped) break
      before = p.Next
    }
  } catch (e) {
    if (my === wholeGen) w = { ...w, error: describeError(e) }
  }
  if (my === wholeGen) whole.value = { ...w, running: false }
}

function stopWhole() {
  wholeGen++
  if (whole.value) whole.value = { ...whole.value, running: false, stopped: true }
}

function closeWhole() {
  wholeGen++
  whole.value = null
}

const wholeStatus = computed(() => {
  const w = whole.value
  if (!w) return ''
  const n = w.lines.length
  const found = n === 0 ? 'no matches' : n === 1 ? '1 match' : `${n.toLocaleString()} matches`
  if (w.running) return `Searching the whole file… ${sizeText(w.read)} of ${sizeText(w.size)} read, ${found} so far.`
  if (w.error) return `Stopped after ${sizeText(w.read)} of ${sizeText(w.size)}: ${found}.`
  if (w.stopped) return `Stopped after ${sizeText(w.read)} of ${sizeText(w.size)}: ${found}.`
  if (w.capped) return `The newest ${WHOLE_LIMIT} matches in the whole file. Search for something more specific to see older ones.`
  return `${found.charAt(0).toUpperCase()}${found.slice(1)} in the whole file (${sizeText(w.size)}).`
})
const editorPath = computed(() => (stream.value === 'stdout' ? props.stdoutLog : props.stderrLog))

const off = Events.On(VisorLog, (ev) => {
  const c = ev.data
  if (followId === null) {
    if (early.length < 20) early.push(c)
  } else if (c.ID === followId) {
    receive(c)
  }
})

function receive(c: LogChunkView) {
  if (c.Error) {
    followId = null // the Go side already stopped this follow
    lost.value = describeError(c.Error)
    return
  }
  live.value = applyChunk(live.value, c)
  if (!paused.value) void scrollToEnd()
}

async function scrollToEnd() {
  if (!stick) return
  await nextTick()
  if (pre.value) pre.value.scrollTop = pre.value.scrollHeight
}

function onScroll() {
  const el = pre.value
  if (el) stick = el.scrollTop + el.clientHeight >= el.scrollHeight - 24
}

async function unfollow() {
  const id = followId
  followId = null
  if (id) await LogService.Unfollow(id).catch(() => {})
}

async function follow() {
  const my = ++gen
  await unfollow()
  live.value = emptyLog()
  frozen.value = emptyLog()
  early = []
  noLog.value = null
  error.value = null
  lost.value = null
  paused.value = false // a new stream starts live; a frozen view of the old one would be empty
  loading.value = true
  stick = true
  try {
    const id = await LogService.Follow(props.process, stream.value)
    if (my !== gen) {
      void LogService.Unfollow(id).catch(() => {})
      return
    }
    followId = id
    for (const c of early) if (c.ID === id) receive(c)
    early = []
  } catch (e) {
    if (my !== gen) return
    noLog.value = noLogMessage(e)
    if (!noLog.value) error.value = describeError(e)
  } finally {
    if (my === gen) {
      loading.value = false
      void scrollToEnd() // the first chunk may have arrived while "Loading…" showed
    }
  }
}

function togglePause() {
  if (paused.value) {
    paused.value = false
    stick = true
    void scrollToEnd()
  } else {
    frozen.value = live.value
    paused.value = true
  }
}

async function clearLogs() {
  confirmClear.value = false
  try {
    await withConsent(() => ServerService.ClearLogs(props.process)) // its dialog asked
    showToast(`Cleared the logs of ${props.process}.`)
  } catch (e) {
    showToast(describeError(e), 'error')
  }
}

async function save() {
  if (saving.value) return
  saving.value = true
  try {
    const path = await LogService.Save(props.process, stream.value)
    if (path) showToast(`Saved the ${stream.value} log to ${path}.`)
  } catch (e) {
    showToast(describeError(e), 'error')
  } finally {
    saving.value = false
  }
}

async function openInEditor() {
  try {
    await ServerService.OpenLog(editorPath.value)
  } catch (e) {
    showToast(describeError(e), 'error')
  }
}

// Find (⌘F from the menu bar, Ctrl+F on Linux) searches the log.
function focusSearch() {
  search.value?.focus()
  search.value?.select()
}
let offFind: () => void = () => {}

let typing: ReturnType<typeof setTimeout> | undefined
watch(query, (v) => {
  clearTimeout(typing)
  if (v === '') {
    q.value = ''
    closeWhole()
  } else typing = setTimeout(() => (q.value = v), 200)
})

// The parent keys this view by process, so only the stream changes here.
watch(stream, () => {
  closeWhole()
  void follow()
})
onMounted(() => {
  offFind = onMenu('find', focusSearch)
  if (!currentSettings.value) loadSettings().catch(() => {}) // whole-file search stays off until it loads
  void follow()
})
onBeforeUnmount(() => {
  gen++
  wholeGen++
  clearTimeout(typing)
  off()
  offFind()
  void unfollow()
})
</script>

<template>
  <div class="logs">
    <div class="bar">
      <!-- Left: what you are watching. Right: what you can do with the file. -->
      <div class="watching">
        <div class="seg" role="group" aria-label="Log stream">
          <button
            v-for="s in (['stdout', 'stderr'] as const)"
            :key="s"
            type="button"
            :class="{ on: stream === s }"
            :aria-pressed="stream === s"
            @click="stream = s"
          >
            {{ s }}
          </button>
        </div>
        <button type="button" class="btn sm" :disabled="!!noLog || !!error || !!lost" @click="togglePause">
          <svg v-if="paused" width="12" height="12" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M7 5v14l12-7z" /></svg>
          <svg v-else width="12" height="12" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><rect x="6" y="5" width="4" height="14" rx="1" /><rect x="14" y="5" width="4" height="14" rx="1" /></svg>
          {{ paused ? 'Resume' : 'Pause' }}
        </button>
        <label for="logq" class="sr-only">Search log</label>
        <input id="logq" ref="search" v-model="query" class="input search" type="search" placeholder="Search log" autocomplete="off" @keydown.enter="searchWhole" />
        <span v-if="q && !whole" class="meta" aria-live="polite">{{ matches === 0 ? 'No matches' : matches === 1 ? '1 match' : `${matches.toLocaleString()} matches` }}{{ matches > MAX_MARKS ? ` · the newest ${MAX_MARKS.toLocaleString()} highlighted` : '' }}</span>
        <span class="meta follow" role="status" data-testid="follow-state">
          <template v-if="paused">
            <span class="dot d-warn" aria-hidden="true" /><span title="New lines are buffered and appear when you resume.">Paused{{ waiting > 0 ? ` · ${waiting} new ${waiting === 1 ? 'line' : 'lines'} waiting` : '' }}</span>
          </template>
          <template v-else-if="!noLog && !error && !lost">
            <span class="dot d-ok" aria-hidden="true" /><span title="New lines appear as they are written.">Following live</span>
          </template>
        </span>
      </div>
      <div class="file">
        <button type="button" class="btn quiet sm" :disabled="!!noLog || saving" @click="save">{{ saving ? 'Saving…' : 'Save…' }}</button>
        <button
          v-if="local"
          type="button"
          class="btn quiet sm"
          :disabled="!editorPath"
          :title="editorPath || 'This stream has no log file'"
          @click="openInEditor"
        >
          Open in editor
        </button>
        <button
          type="button"
          class="btn quiet sm danger"
          :disabled="!running"
          :title="running ? 'Empties the stdout and stderr logs of this process' : 'supervisord can clear the logs of a running process only'"
          @click="confirmClear = true"
        >
          Clear
        </button>
      </div>
    </div>

    <div v-if="chips.length && !whole && !noLog && !error" class="levels" role="group" aria-label="Show only these levels">
      <button
        v-for="l in chips"
        :key="l"
        type="button"
        class="chip"
        :class="`lv-${l}`"
        :aria-pressed="levels.has(l)"
        @click="toggleLevel(l)"
      >
        <span class="swatch" aria-hidden="true" />{{ LEVEL_NAMES[l] }} <span class="n">{{ counts[l].toLocaleString() }}</span>
      </button>
      <span class="meta">{{ levels.size ? 'Showing only the picked levels. Lines with no level are hidden.' : 'Pick a level to show only its lines.' }}</span>
    </div>

    <p v-if="q && !whole && !noLog && !error" class="scope" data-testid="search-scope">
      <template v-if="wholeAllowed">
        Searching the {{ all.length.toLocaleString() }} loaded lines.
        <button type="button" class="btn sm" @click="searchWhole">Search the whole file</button>
        or press Enter.
      </template>
      <template v-else>
        Searching only the {{ all.length.toLocaleString() }} loaded lines, not the whole file. To search all of it, turn on
        <RouterLink to="/settings">Search the whole log file</RouterLink> in Settings. It is slower: every search reads the whole file, and on a server downloads it over SSH.
      </template>
    </p>

    <div v-if="lost && !noLog && !error" class="lost" role="alert">
      <div>
        <strong>Lost the log stream.</strong>
        {{ lost }}
      </div>
      <button type="button" class="btn" @click="follow">Retry</button>
    </div>
    <div v-if="noLog" class="empty-pane">
      <strong>{{ noLog }}</strong>
      <p v-if="stream === 'stderr' && noLog.startsWith('stderr')">Switch to stdout to see what it writes to stderr.</p>
      <p v-else>The config gives {{ stream }} no log file ({{ stream }}_logfile), so there is nothing to show.</p>
    </div>
    <div v-else-if="error" class="empty-pane" role="alert">
      <strong>Couldn’t follow this log</strong>
      <p>{{ error }}</p>
      <button type="button" class="btn" @click="follow">Try again</button>
    </div>
    <!-- One block per line: the number column is its own, so a long line
         wraps under its text, not under the number. -->
    <section v-else-if="whole" class="whole" aria-label="Whole-file search results">
      <div class="whole-bar">
        <span :role="whole.error ? 'alert' : 'status'" class="meta">{{ wholeStatus }}<template v-if="whole.error"> {{ whole.error }}</template></span>
        <span class="btns">
          <button v-if="whole.running" type="button" class="btn sm" @click="stopWhole">Stop</button>
          <button type="button" class="btn sm" @click="closeWhole">Back to the live log</button>
        </span>
      </div>
      <pre class="log" tabindex="0" :aria-label="`Lines of the ${stream} log of ${process} matching ${whole.query}, newest first`"><span v-if="!whole.lines.length" class="note">{{ whole.running ? 'Searching…' : 'Nothing in the file matches.' }}</span><span v-for="(t, i) in whole.lines" :key="i" class="line hit" :class="levelOf(t) && `lv-${levelOf(t)}`"><span class="tx"><template v-for="(s, j) in highlight(t, whole.query)" :key="j"><mark v-if="s.match">{{ s.text }}</mark><template v-else>{{ s.text }}</template></template></span></span></pre>
    </section>
    <pre
      v-else
      ref="pre"
      class="log"
      tabindex="0"
      :aria-label="`${stream} log of ${process}`"
      :aria-busy="loading"
      @scroll="onScroll"
    ><span v-if="loading" class="note">Loading…</span><span v-else-if="!all.length" class="note">No output yet. New lines appear here as they are written.</span><span v-else-if="!shown.length" class="note">No lines of the picked levels in the loaded lines.</span><template v-else><span v-for="l in shown" :key="l.id" v-memo="[l.n, l.text, l.level, q, marked.has(l.id)]" class="line" :class="l.level && `lv-${l.level}`"><span class="ln" aria-hidden="true">{{ l.note ? '' : l.n }}</span><span v-if="l.note" class="note">— {{ l.text }} —</span><span v-else class="tx"><template v-for="(s, j) in highlight(l.text, marked.has(l.id) ? q : '')" :key="j"><mark v-if="s.match">{{ s.text }}</mark><template v-else>{{ s.text }}</template></template></span></span></template></pre>

    <ConfirmDialog
      v-if="confirmClear"
      title="Clear these logs?"
      :message="`supervisord empties both the stdout and the stderr log files of ${process}. This can’t be undone.`"
      confirm-label="Clear logs"
      @confirm="clearLogs"
      @cancel="confirmClear = false"
    />
  </div>
</template>

<style scoped>
.logs { display: flex; flex: 1 1 auto; flex-direction: column; gap: 12px; min-height: 0; }
.bar { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 8px 16px; }
.watching, .file { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
.file { gap: 4px; }
.search { width: 220px; height: var(--control-sm); }
.meta { color: var(--ink3); font-size: 12.5px; }
.follow { display: inline-flex; align-items: center; gap: 6px; margin-left: 4px; }
.follow:empty { display: none; }
/* The log fills what is left of the window (the page sets the height) and
   scrolls itself. */
.log {
  flex: 1 1 0;
  margin: 0;
  min-height: 300px;
  padding: 14px 0;
  overflow: auto;
  background: var(--log-bg);
  color: var(--log-ink);
  border-radius: 10px;
  font-family: var(--mono);
  font-size: 12.5px;
  line-height: 1.7;
  white-space: pre-wrap;
  user-select: text;
}
.line { display: grid; grid-template-columns: 52px minmax(0, 1fr); column-gap: 14px; padding-right: 14px; }
.tx { overflow-wrap: anywhere; }
.ln { text-align: right; color: var(--log-dim); font-variant-numeric: tabular-nums; user-select: none; }
.log > .note { display: block; padding: 0 14px 0 80px; }
.note { color: var(--log-dim); font-style: italic; }
mark { background: var(--log-mark-bg); color: var(--log-mark-fg); border-radius: 2px; }
/* Level marks: a bar in the number gutter, and a tint for errors and
   warnings. The level word is in the line's own text. */
.line.lv-error { background: var(--log-error-soft); }
.line.lv-warn { background: var(--log-warn-soft); }
.line[class*='lv-'] { box-shadow: inset 3px 0 0 var(--lv); }
.lv-error { --lv: var(--log-error); }
.lv-warn { --lv: var(--log-warn); }
.lv-info { --lv: var(--log-info); }
.lv-debug { --lv: var(--log-dim); }
.levels { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; margin-top: -4px; }
.chip { display: inline-flex; align-items: center; gap: 6px; height: var(--control-sm); padding: 0 10px; border: 1px solid var(--line2); border-radius: 999px; background: var(--raised); color: var(--ink2); font: inherit; font-size: 12.5px; cursor: pointer; }
.chip[aria-pressed='true'] { border-color: var(--accent); background: var(--accent-soft); color: var(--ink); font-weight: 600; }
.chip .swatch { width: 8px; height: 8px; border-radius: 2px; background: var(--lv); }
.chip.lv-warn .swatch { background: var(--warn-dot); }
.chip.lv-error .swatch { background: var(--bad-dot); }
.chip.lv-info .swatch { background: var(--accent); }
.chip.lv-debug .swatch { background: var(--idle-dot); }
.chip .n { color: var(--ink3); font-variant-numeric: tabular-nums; }
.scope { margin: -4px 0 0; color: var(--ink3); font-size: 12.5px; }
.line.hit { grid-template-columns: minmax(0, 1fr); padding-left: 14px; }
.whole { display: flex; flex: 1 1 auto; flex-direction: column; gap: 8px; min-height: 0; }
.whole-bar { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 8px; }
.whole-bar .btns { display: flex; gap: 6px; }
.empty-pane { display: flex; flex-direction: column; align-items: flex-start; gap: 6px; min-height: 120px; padding: 16px; border: 1px dashed var(--line2); border-radius: var(--radius-md); }
.empty-pane p { margin: 0; color: var(--ink2); }
.lost { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 8px; padding: 10px 12px; border: 1px solid var(--bad-dot); border-radius: var(--radius-md); background: var(--bad-soft); color: var(--bad); user-select: text; }
.sr-only { position: absolute; left: -9999px; }
</style>
