<script setup lang="ts">
// A live log (spec §5.3 Logs tab): stdout/stderr switch, Pause/Resume
// (output keeps arriving while paused and shows on resume), search
// highlight, Clear (with confirmation), Save… and Open in editor. The Go
// side polls supervisord; this view only applies visor:log chunks. It stops
// following when it unmounts.
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import { Events } from '@wailsio/runtime'
import { LogService, ServerService, type LogChunkView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { VisorLog } from '../../bindings/events'
import { describeError } from '../lib/faults'
import { applyChunk, countMatches, emptyLog, highlight, noLogMessage, shownLines, type Stream } from '../lib/logview'
import { showToast } from '../lib/toast'
import ConfirmDialog from './ConfirmDialog.vue'

// running: supervisord clears the logs of a running process only; for any
// other it answers success and leaves the files alone.
const props = defineProps<{ process: string; stdoutLog: string; stderrLog: string; initialStream: Stream; running: boolean }>()

const stream = ref<Stream>(props.initialStream)
const paused = ref(false)
const query = ref('')
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

const shown = computed(() => shownLines(paused.value ? frozen.value : live.value))
const waiting = computed(() => (paused.value ? live.value.next - frozen.value.next : 0))
const matches = computed(() => countMatches(shown.value, query.value))
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
    await ServerService.ClearLogs(props.process)
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

function onKey(e: KeyboardEvent) {
  if (e.metaKey && !e.altKey && !e.ctrlKey && !e.shiftKey && e.key === 'f') {
    e.preventDefault()
    search.value?.focus()
    search.value?.select()
  }
}

// The parent keys this view by process, so only the stream changes here.
watch(stream, () => void follow())
onMounted(() => {
  window.addEventListener('keydown', onKey)
  void follow()
})
onBeforeUnmount(() => {
  gen++
  off()
  window.removeEventListener('keydown', onKey)
  void unfollow()
})
</script>

<template>
  <div class="logs">
    <div class="bar">
      <span class="mono name">{{ process }}</span>
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
      <button type="button" class="btn" :disabled="!!noLog || !!error || !!lost" @click="togglePause">{{ paused ? 'Resume' : 'Pause' }}</button>
      <label for="logq" class="sr-only">Search log</label>
      <input id="logq" ref="search" v-model="query" type="search" placeholder="Search log" autocomplete="off" />
      <span v-if="query" class="count" aria-live="polite">{{ matches === 0 ? 'No matches' : matches === 1 ? '1 match' : `${matches} matches` }}</span>
      <span class="spacer" />
      <button
        type="button"
        class="btn"
        :disabled="!running"
        :title="running ? undefined : 'supervisord can clear the logs of a running process only'"
        @click="confirmClear = true"
      >
        Clear
      </button>
      <button type="button" class="btn" :disabled="!!noLog || saving" @click="save">{{ saving ? 'Saving…' : 'Save…' }}</button>
      <button
        type="button"
        class="btn"
        :disabled="!editorPath"
        :title="editorPath || 'This stream has no log file'"
        @click="openInEditor"
      >
        Open in editor
      </button>
    </div>

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
    <pre
      v-else
      ref="pre"
      class="log"
      tabindex="0"
      :aria-label="`${stream} log of ${process}`"
      :aria-busy="loading"
      @scroll="onScroll"
    ><template v-if="loading">Loading…</template><template v-else-if="!shown.length"><span class="faint">No output yet. New lines appear here as they are written.</span></template><template v-else><template v-for="l in shown" :key="l.id"><span v-if="l.note" class="note">— {{ l.text }} —</span><template v-else><span class="ln" aria-hidden="true">{{ l.n }}</span><template v-for="(s, j) in highlight(l.text, query)" :key="j"><mark v-if="s.match">{{ s.text }}</mark><template v-else>{{ s.text }}</template></template></template>
</template></template></pre>
    <div class="follow-note" aria-live="polite">
      <template v-if="paused">Paused. New lines are buffered and appear when you resume.{{ waiting > 0 ? ` ${waiting} new ${waiting === 1 ? 'line' : 'lines'} waiting.` : '' }}</template>
      <template v-else-if="!noLog && !error && !lost">Following live. New lines appear as they are written.</template>
    </div>

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
.logs { display: flex; flex-direction: column; gap: 10px; }
.bar { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
.mono { font-family: var(--font-mono); font-size: 12px; }
.name { font-weight: 600; }
input[type='search'] { min-height: var(--control); flex: 1 1 180px; max-width: 260px; box-sizing: border-box; padding: 0 10px; border: 1px solid var(--line-strong); border-radius: var(--radius-sm); background: var(--bg); color: var(--text); font: inherit; }
.count { color: var(--text-muted); font-size: 12px; }
.spacer { flex: 1; }
.log {
  margin: 0;
  min-height: 300px;
  max-height: max(300px, calc(100vh - 440px));
  padding: 14px;
  overflow: auto;
  background: var(--log-bg);
  color: var(--log-fg);
  border-radius: var(--radius-md);
  font-family: var(--font-mono);
  font-size: 12px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-word;
  user-select: text;
}
.ln { display: inline-block; min-width: 28px; margin-right: 14px; text-align: right; color: var(--log-muted); user-select: none; }
.note, .faint { color: var(--log-muted); font-style: italic; }
mark { background: var(--log-mark-bg); color: var(--log-mark-fg); border-radius: 2px; }
.empty-pane { display: flex; flex-direction: column; align-items: flex-start; gap: 6px; min-height: 120px; padding: 16px; border: 1px dashed var(--line-strong); border-radius: var(--radius-md); }
.empty-pane p { margin: 0; color: var(--text-muted); }
.lost { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 8px; padding: 10px 12px; border: 1px solid var(--failing-dot); border-radius: var(--radius-md); background: var(--failing-bg); color: var(--failing-fg); user-select: text; }
.follow-note { color: var(--text-muted); min-height: 1.4em; }
.sr-only { position: absolute; left: -9999px; }
</style>
