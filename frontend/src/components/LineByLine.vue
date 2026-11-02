<script setup lang="ts">
// "Line by line": every line of a config with what
// it means; problems highlighted with how to fix them; the files and
// folders a line points to, checked on disk, with ways to open them.
//
// mode "overview" shows the code beside each meaning and sends fixes to
// the editor; mode "editor" sits beside an editor, so it shows only line
// numbers and applies fixes straight to the text. actions=false hides the
// fix and go-to-line buttons (the Add new form has no text to change).
import { computed, ref } from 'vue'
import type { EditView, ExplainLineView, ExplainView, LinePathView, ProblemView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { fileSize, isProblem } from '../lib/explain'

const props = withDefaults(
  defineProps<{ view: ExplainView | null; failed?: string | null; mode: 'overview' | 'editor'; readOnly?: boolean; actions?: boolean }>(),
  { failed: null, readOnly: false, actions: true },
)
const emit = defineEmits<{
  edit: [edit: EditView]
  add: [after: number, lines: string[]]
  goto: [line: number]
  open: [path: string]
  reveal: [path: string]
  openLog: [path: string]
  viewLog: [process: string, stream: string]
}>()

const onlyProblems = ref(false)
const editor = computed(() => props.mode === 'editor')
const sections = computed(() =>
  (props.view?.Sections ?? [])
    .map((s) => ({ ...s, Lines: (s.Lines ?? []).filter((l) => !onlyProblems.value || isProblem(l)) }))
    .filter((s) => s.Lines.length),
)
const problemCount = computed(() => (props.view?.Errors ?? 0) + (props.view?.Suggestions ?? 0))

function heading(name: string, program: string) {
  return program ? `Program ${program}` : `[${name}]`
}
function levelWord(p: ProblemView) {
  return p.Level === 'error' ? 'Error.' : 'Warning.'
}
function fixLabel() {
  return editor.value ? 'Apply fix' : 'Apply fix in the editor'
}
function addLabel(l: ExplainLineView) {
  const n = l.Add?.length ?? 0
  const what = n === 1 ? 'Add this line' : n === 2 ? 'Add both lines' : `Add ${n} lines`
  return editor.value ? what : `${what} in the editor`
}
function gotoLabel(line: number) {
  return editor.value ? `Go to line ${line}` : `Edit line ${line}`
}
function tag(p: LinePathView): { text: string; cls: string } {
  if (!p.Exists) {
    if (p.Kind === 'log') return { text: 'not created yet', cls: 'auto' }
    return { text: 'missing', cls: 'bad' }
  }
  const size = p.Kind === 'log' ? fileSize(p.Size) : ''
  return { text: size ? `found · ${size}` : 'found', cls: 'ok' }
}
</script>

<template>
  <section class="lbl" aria-labelledby="lbl-title" data-testid="line-by-line">
    <div class="head">
      <div class="title">
        <h2 id="lbl-title">Line by line</h2>
        <span v-if="!editor" class="muted">Every file or folder a line points to is checked on disk and can be opened from here.</span>
      </div>
      <div v-if="view && !view.ParseError" class="seg" role="group" aria-label="Which lines to show">
        <button type="button" :aria-pressed="!onlyProblems" @click="onlyProblems = false">All lines</button>
        <button type="button" :aria-pressed="onlyProblems" @click="onlyProblems = true">Only problems<span v-if="problemCount"> · {{ problemCount }}</span></button>
      </div>
    </div>

    <p v-if="failed" class="pad muted">Visor can’t explain this file. {{ failed }}</p>
    <p v-else-if="!view" class="pad muted">Loading…</p>

    <div v-else-if="view.ParseError" class="pad">
      <div class="problem error" role="alert">
        <span><strong>Error<template v-if="view.ParseError.Line"> on line {{ view.ParseError.Line }}</template>.</strong> {{ view.ParseError.Message }}</span>
        <span v-if="view.ParseError.Fix" class="fix"><strong>How to fix:</strong> {{ view.ParseError.Fix }}</span>
        <span v-if="actions && view.ParseError.Line" class="acts">
          <button type="button" class="btn" @click="emit('goto', view.ParseError.Line)">{{ gotoLabel(view.ParseError.Line) }}</button>
        </span>
      </div>
    </div>

    <template v-else>
      <ul v-if="view.FileProblems?.length" class="file-problems">
        <li v-for="(p, i) in view.FileProblems" :key="i" :class="['problem', p.Level]">
          <span><strong>{{ levelWord(p) }}</strong> {{ p.Message }}</span>
          <span v-if="p.Fix" class="fix"><strong>How to fix:</strong> {{ p.Fix }}</span>
          <span v-if="actions && (p.Edit || p.Line)" class="acts">
            <button v-if="p.Edit" type="button" class="btn primary" :disabled="readOnly" @click="emit('edit', p.Edit)">{{ fixLabel() }}</button>
            <button v-if="p.Line" type="button" class="btn" @click="emit('goto', p.Line)">{{ gotoLabel(p.Line) }}</button>
          </span>
        </li>
      </ul>

      <p v-if="!sections.length" class="pad muted">{{ onlyProblems ? 'No problems found.' : 'This file has no settings yet.' }}</p>

      <div v-for="s in sections" :key="s.Name" class="group">
        <h3 class="group-head">{{ heading(s.Name, s.Program) }}</h3>
        <ol :class="['rows', mode]">
          <li
            v-for="l in s.Lines"
            :id="l.Kind === 'unset' ? undefined : `explain-line-${l.Line}`"
            :key="l.Kind + l.Line"
            :class="['row', l.Level]"
            tabindex="-1"
          >
            <span class="num" :aria-label="l.Kind === 'unset' ? 'Not in the file' : `Line ${l.Line}`">
              <svg v-if="l.Level === 'error'" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.6" stroke-linecap="round" aria-hidden="true"><path d="M18 6 6 18M6 6l12 12" /></svg>
              <svg v-else-if="l.Level" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" aria-hidden="true"><path d="M12 8v5M12 16h.01" /><circle cx="12" cy="12" r="9" /></svg>
              <template v-if="l.Kind !== 'unset'">{{ l.Line }}</template>
            </span>
            <span v-if="!editor" :class="['code', { unset: l.Kind === 'unset' }]">{{ l.Text }}</span>
            <span class="meaning">
              <span v-if="editor && l.Kind === 'unset'" class="unset-name">{{ l.Text }}</span>
              <span>{{ l.Meaning }}</span>

              <span v-for="(p, i) in l.Problems" :key="i" :class="['problem', p.Level]">
                <span><strong>{{ levelWord(p) }}</strong> {{ p.Message }}<span v-if="p.Live" class="live"> (supervisord)</span></span>
                <span v-if="p.Fix" class="fix"><strong>How to fix:</strong> {{ p.Fix }}</span>
                <code v-if="p.Edit" class="new">{{ p.Edit.Text }}</code>
                <span v-if="actions" class="acts">
                  <button v-if="p.Edit" type="button" class="btn primary" :disabled="readOnly" @click="emit('edit', p.Edit)">{{ fixLabel() }}</button>
                  <button v-if="l.Line && (!editor || !p.Edit)" type="button" :class="['btn', { primary: !p.Edit && !editor }]" @click="emit('goto', l.Line)">
                    {{ gotoLabel(l.Line) }}
                  </button>
                </span>
              </span>

              <span v-if="l.Suggest" class="problem suggestion">
                <span><strong>Suggestion.</strong> {{ l.Suggest }}</span>
                <template v-if="l.Add?.length && actions">
                  <code v-for="a in l.Add" :key="a" class="new">{{ a }}</code>
                  <span class="acts">
                    <button type="button" class="btn" :disabled="readOnly" @click="emit('add', l.Line, l.Add)">{{ addLabel(l) }}</button>
                  </span>
                </template>
              </span>

              <span v-for="p in l.Paths" :key="p.Kind + p.Path" class="path">
                <span class="p">
                  <span class="muted">{{ p.Label }}</span>
                  <code>{{ p.Path }}</code>
                  <span :class="['tag', tag(p).cls]">{{ tag(p).text }}</span>
                  <span v-if="p.Auto" class="tag auto">named by supervisord</span>
                </span>
                <span class="acts">
                  <template v-if="p.Kind === 'bin'">
                    <button type="button" class="btn sm" :disabled="!p.Exists" :title="p.Exists ? undefined : 'Nothing to show: the file doesn’t exist'" @click="emit('reveal', p.Path)">Reveal in Finder</button>
                  </template>
                  <template v-else-if="p.Kind === 'dir'">
                    <button type="button" class="btn sm" :disabled="!p.Exists" :title="p.Exists ? undefined : 'Nothing to open: the folder doesn’t exist'" @click="emit('open', p.Path)">Open in Finder</button>
                  </template>
                  <template v-else>
                    <button type="button" class="btn sm" :disabled="!p.Exists" @click="emit('openLog', p.Path)">Open in editor</button>
                    <button type="button" class="btn sm" :disabled="!p.Exists" @click="emit('reveal', p.Path)">Show in Finder</button>
                    <button v-if="!editor && p.Process" type="button" class="link" @click="emit('viewLog', p.Process, p.Stream)">View in Logs tab</button>
                  </template>
                </span>
              </span>
            </span>
          </li>
        </ol>
      </div>
    </template>
  </section>
</template>

<style scoped>
.lbl { border: 1px solid var(--line); border-radius: 10px; overflow: hidden; min-width: 0; }
.head { display: flex; flex-wrap: wrap; justify-content: space-between; align-items: center; gap: 10px; padding: 12px 18px; border-bottom: 1px solid var(--line); }
.title { display: flex; flex-direction: column; gap: 2px; }
h2 { margin: 0; font-size: 15px; }
.muted { color: var(--text-muted); }
.pad { margin: 0; padding: 16px 18px; }
.seg { display: flex; gap: 3px; padding: 3px; border-radius: 8px; background: var(--seg-bg); }
.seg button { padding: 3px 11px; border: 0; border-radius: 6px; background: transparent; color: var(--text-muted); font: inherit; cursor: pointer; white-space: nowrap; }
.seg button[aria-pressed='true'] { background: var(--seg-on); color: var(--text); font-weight: 600; }
.file-problems { list-style: none; margin: 0; padding: 12px 18px; display: flex; flex-direction: column; gap: 8px; border-bottom: 1px solid var(--line); }
.group-head { margin: 0; padding: 8px 18px; background: var(--bg-subtle); color: var(--text-muted); font-size: 12px; font-weight: 600; border-bottom: 1px solid var(--line); }
.group + .group .group-head { border-top: 1px solid var(--line); }
.rows { list-style: none; margin: 0; padding: 0; }
.row { display: grid; gap: 0 16px; padding: 10px 18px 10px 0; border-top: 1px solid var(--line); line-height: 1.5; outline: none; }
.row:first-child { border-top: 0; }
.rows.overview .row { grid-template-columns: 52px minmax(0, 340px) minmax(0, 1fr); }
.rows.editor .row { grid-template-columns: 44px minmax(0, 1fr); }
.row:focus-visible, .row:target { box-shadow: inset 3px 0 0 var(--accent); }
.row.error { background: var(--failing-bg); }
.row.warning, .row.suggestion { background: var(--warning-bg); }
.num { display: inline-flex; justify-content: flex-end; align-items: baseline; gap: 4px; color: var(--text-faint); font-family: var(--font-mono); font-size: 12px; padding-top: 1px; }
.row.error .num { color: var(--failing-fg); }
.row.warning .num, .row.suggestion .num { color: var(--warning-fg); }
.code { font-family: var(--font-mono); font-size: 12.5px; overflow-wrap: anywhere; user-select: text; }
.code.unset, .unset-name { font-style: italic; color: var(--warning-fg); font-family: var(--font-mono); font-size: 12.5px; }
.row:not(.warning):not(.suggestion) .code.unset { color: var(--text-muted); }
.meaning { display: flex; flex-direction: column; gap: 8px; min-width: 0; user-select: text; }
.problem { display: flex; flex-direction: column; gap: 6px; }
.problem.error { color: var(--failing-fg); }
.problem.warning, .problem.suggestion { color: var(--warning-fg); }
.file-problems .problem { padding: 10px 12px; border-radius: var(--radius-sm); }
.file-problems .problem.error { background: var(--failing-bg); }
.file-problems .problem.warning { background: var(--warning-bg); }
.fix { color: var(--text); }
.live { color: var(--text-muted); font-size: 12px; }
code.new { align-self: flex-start; padding: 2px 7px; border-radius: 5px; background: var(--bg); color: var(--text); font-family: var(--font-mono); font-size: 12px; overflow-wrap: anywhere; }
.acts { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
.path { display: flex; flex-direction: column; gap: 6px; padding: 8px 10px; border: 1px solid var(--line); border-radius: 8px; background: var(--bg); color: var(--text); }
.path .p { display: flex; flex-wrap: wrap; align-items: center; gap: 4px 8px; }
.path code { font-family: var(--font-mono); font-size: 12px; overflow-wrap: anywhere; }
.tag { padding: 1px 8px; border-radius: 999px; font-size: 11px; font-weight: 600; white-space: nowrap; }
.tag.ok { background: var(--running-bg); color: var(--running-fg); }
.tag.bad { background: var(--failing-bg); color: var(--failing-fg); }
.tag.auto { background: var(--stopped-bg); color: var(--stopped-fg); }
.btn { display: inline-flex; align-items: center; min-height: 30px; padding: 0 12px; border-radius: var(--radius-sm); border: 1px solid var(--line-strong); background: var(--bg); color: var(--text); font: inherit; white-space: nowrap; cursor: pointer; box-sizing: border-box; }
.btn.sm { min-height: 26px; padding: 0 10px; font-size: 12px; }
.btn:hover:not(:disabled) { background: var(--bg-hover); }
.btn:disabled { color: var(--text-faint); cursor: default; }
.btn.primary { background: var(--accent); border-color: var(--accent); color: var(--on-accent); font-weight: 600; }
.btn.primary:hover:not(:disabled) { background: var(--accent-hover); border-color: var(--accent-hover); }
.btn.primary:disabled { opacity: 0.55; color: var(--on-accent); }
.link { padding: 0; border: 0; background: none; color: var(--accent-text); font: inherit; font-size: 12px; cursor: pointer; }
.link:hover { text-decoration: underline; }
@media (max-width: 760px) {
  .rows.overview .row { grid-template-columns: 44px minmax(0, 1fr); }
  .rows.overview .meaning { grid-column: 2; }
}
</style>
