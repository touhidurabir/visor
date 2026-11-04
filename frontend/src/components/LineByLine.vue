<script setup lang="ts">
// "Line by line": every line of a config with what
// it means; problems highlighted with how to fix them; the files and
// folders a line points to, checked on disk, with ways to open them.
//
// mode "overview" shows the code beside each meaning and sends fixes to
// the editor; mode "editor" sits beside an editor, so it shows only line
// numbers and applies fixes straight to the text. actions=false hides the
// fix and go-to-line buttons (the Add new form has no text to change).
// fixList adds "To fix before saving" above the lines: each problem in one
// line, linking to its line in the editor (goto).
// The Overview opens on "Only problems" when the file has any (flow 2);
// the choice is made once, when the explanation first arrives, and stays
// the user's after that.
import { computed, ref, watch } from 'vue'
import type { EditView, ExplainLineView, ExplainView, LinePathView, ProblemView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { fileSize, isProblem } from '../lib/explain'
import { fixItems } from '../lib/fix-list'
import { opensOnProblems } from '../lib/overview'

const props = withDefaults(
  defineProps<{ view: ExplainView | null; failed?: string | null; mode: 'overview' | 'editor'; readOnly?: boolean; actions?: boolean; fixList?: boolean }>(),
  { failed: null, readOnly: false, actions: true, fixList: false },
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
let decided = false
watch(
  () => props.view,
  (v) => {
    if (decided || !v) return
    decided = true
    if (!editor.value) onlyProblems.value = opensOnProblems(v)
  },
  { immediate: true },
)
const sections = computed(() =>
  (props.view?.Sections ?? [])
    .map((s) => ({ ...s, Lines: (s.Lines ?? []).filter((l) => !onlyProblems.value || isProblem(l)) }))
    .filter((s) => s.Lines.length),
)
const problemCount = computed(() => (props.view?.Errors ?? 0) + (props.view?.Suggestions ?? 0))
const toFix = computed(() => (props.fixList && !props.failed ? fixItems(props.view) : []))

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
    if (p.Kind === 'log') return { text: 'not created yet', cls: 'b-idle' }
    return { text: 'missing', cls: 'b-bad' }
  }
  const size = p.Kind === 'log' ? fileSize(p.Size) : ''
  return { text: size ? `found · ${size}` : 'found', cls: 'b-ok' }
}
</script>

<template>
  <section class="lbl" aria-labelledby="lbl-title" data-testid="line-by-line">
    <div class="head">
      <div class="title">
        <h2 id="lbl-title">Line by line</h2>
        <span v-if="!editor" class="meta">Every file or folder a line points to is checked on disk and can be opened from here.</span>
      </div>
      <div v-if="view && !view.ParseError" class="seg" role="group" aria-label="Which lines to show">
        <button type="button" :aria-pressed="!onlyProblems" @click="onlyProblems = false">All lines</button>
        <button type="button" :aria-pressed="onlyProblems" @click="onlyProblems = true">{{ problemCount ? `Only problems · ${problemCount}` : 'Only problems' }}</button>
      </div>
    </div>

    <div v-if="toFix.length" class="to-fix" role="group" aria-labelledby="to-fix-title" data-testid="to-fix">
      <span id="to-fix-title" class="th">To fix before saving</span>
      <ul>
        <li v-for="(f, i) in toFix" :key="i" :class="f.level === 'error' ? 't-bad' : 't-warn'">
          <component :is="f.line ? 'button' : 'span'" :type="f.line ? 'button' : undefined" class="fix-item" @click="f.line && emit('goto', f.line)">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path :d="f.level === 'error' ? 'M18 6 6 18M6 6l12 12' : 'M12 3 2 20h20L12 3zM12 10v4M12 17v.01'" /></svg>
            <span><span class="sr">{{ f.level === 'error' ? 'Error' : 'Warning' }}: </span><strong>{{ f.line ? `Line ${f.line}` : 'The file' }}</strong> · {{ f.text }}<span v-if="f.level !== 'error'" class="still"> (it can still be saved)</span></span>
          </component>
        </li>
      </ul>
    </div>

    <p v-if="failed" class="pad muted">Visor can’t explain this file. {{ failed }}</p>
    <p v-else-if="!view" class="pad muted">Loading…</p>

    <div v-else-if="view.ParseError" class="pad">
      <div class="problem error" role="alert">
        <span class="word"><svg class="ic" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.6" stroke-linecap="round" aria-hidden="true"><path d="M18 6 6 18M6 6l12 12" /></svg><span><strong>Error<template v-if="view.ParseError.Line"> on line {{ view.ParseError.Line }}</template>.</strong> {{ view.ParseError.Message }}</span></span>
        <span v-if="view.ParseError.Fix" class="fix"><strong>How to fix:</strong> {{ view.ParseError.Fix }}</span>
        <span v-if="actions && view.ParseError.Line" class="acts">
          <button type="button" class="btn sm" @click="emit('goto', view.ParseError.Line)">{{ gotoLabel(view.ParseError.Line) }}</button>
        </span>
      </div>
    </div>

    <template v-else>
      <ul v-if="view.FileProblems?.length" class="file-problems">
        <li v-for="(p, i) in view.FileProblems" :key="i" :class="['problem', p.Level]">
          <span class="word"><svg v-if="p.Level === 'error'" class="ic" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.6" stroke-linecap="round" aria-hidden="true"><path d="M18 6 6 18M6 6l12 12" /></svg><svg v-else class="ic" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" aria-hidden="true"><path d="M12 8v5M12 16h.01" /><circle cx="12" cy="12" r="9" /></svg><span><strong>{{ levelWord(p) }}</strong> {{ p.Message }}</span></span>
          <span v-if="p.Fix" class="fix"><strong>How to fix:</strong> {{ p.Fix }}</span>
          <span v-if="actions && (p.Edit || p.Line)" class="acts">
            <button v-if="p.Edit" type="button" class="btn primary sm" :disabled="readOnly" @click="emit('edit', p.Edit)">{{ fixLabel() }}</button>
            <button v-if="p.Line" type="button" class="btn sm" @click="emit('goto', p.Line)">{{ gotoLabel(p.Line) }}</button>
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
            <span class="num" :aria-label="l.Kind === 'unset' ? 'Not in the file' : `Line ${l.Line}`"><template v-if="l.Kind !== 'unset'">{{ l.Line }}</template></span>
            <span v-if="!editor" :class="['code', { unset: l.Kind === 'unset' }]">{{ l.Text }}</span>
            <span class="meaning">
              <span v-if="editor && l.Kind === 'unset'" class="unset-name">{{ l.Text }}</span>
              <span>{{ l.Meaning }}</span>

              <span v-for="(p, i) in l.Problems" :key="i" :class="['problem', p.Level]">
                <span class="word"><svg v-if="p.Level === 'error'" class="ic" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.6" stroke-linecap="round" aria-hidden="true"><path d="M18 6 6 18M6 6l12 12" /></svg><svg v-else class="ic" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" aria-hidden="true"><path d="M12 8v5M12 16h.01" /><circle cx="12" cy="12" r="9" /></svg><span><strong>{{ levelWord(p) }}</strong> {{ p.Message }}<span v-if="p.Live" class="live"> (supervisord)</span></span></span>
                <span v-if="p.Fix" class="fix"><strong>How to fix:</strong> {{ p.Fix }}</span>
                <code v-if="p.Edit" class="new">{{ p.Edit.Text }}</code>
                <span v-if="actions" class="acts">
                  <button v-if="p.Edit" type="button" class="btn primary sm" :disabled="readOnly" @click="emit('edit', p.Edit)">{{ fixLabel() }}</button>
                  <button v-if="l.Line && (!editor || !p.Edit)" type="button" class="btn sm" @click="emit('goto', l.Line)">
                    {{ gotoLabel(l.Line) }}
                  </button>
                </span>
              </span>

              <span v-if="l.Suggest" class="problem suggestion">
                <span class="word"><svg class="ic" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M9 18h6M10 21h4" /><path d="M12 3a6 6 0 0 0-3.6 10.8c.6.5 1 1.2 1.1 2h5c.1-.8.5-1.5 1.1-2A6 6 0 0 0 12 3Z" /></svg><span><strong>Suggestion.</strong> {{ l.Suggest }}</span></span>
                <template v-if="l.Add?.length && actions">
                  <code v-for="a in l.Add" :key="a" class="new">{{ a }}</code>
                  <span class="acts">
                    <button type="button" class="btn sm" :disabled="readOnly" @click="emit('add', l.Line, l.Add)">{{ addLabel(l) }}</button>
                  </span>
                </template>
              </span>

              <!-- Each path on one line: what it is, where, whether it exists, then links. -->
              <span v-for="p in l.Paths" :key="p.Kind + p.Path" class="path">
                <span class="meta">{{ p.Label }}</span>
                <code class="p">{{ p.Path }}</code>
                <span :class="['badge', 'tag', tag(p).cls]">{{ tag(p).text }}</span>
                <span v-if="p.Auto" class="badge tag b-idle">named by supervisord</span>
                <span class="links">
                  <template v-if="p.Kind === 'bin'">
                    <button type="button" class="link" :disabled="!p.Exists" :title="p.Exists ? undefined : 'Nothing to show: the file doesn’t exist'" @click="emit('reveal', p.Path)">Reveal in Finder</button>
                  </template>
                  <template v-else-if="p.Kind === 'dir'">
                    <button type="button" class="link" :disabled="!p.Exists" :title="p.Exists ? undefined : 'Nothing to open: the folder doesn’t exist'" @click="emit('open', p.Path)">Open in Finder</button>
                  </template>
                  <template v-else>
                    <button type="button" class="link" :disabled="!p.Exists" @click="emit('openLog', p.Path)">Open in editor</button>
                    <button type="button" class="link" :disabled="!p.Exists" @click="emit('reveal', p.Path)">Show in Finder</button>
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
.lbl { min-width: 0; overflow: hidden; border: 1px solid var(--line); border-radius: 10px; background: var(--raised); }
.head { display: flex; flex-wrap: wrap; justify-content: space-between; align-items: center; gap: 10px; padding: 12px 18px; }
.title { display: flex; flex-direction: column; gap: 2px; margin-right: auto; }
h2 { margin: 0; font-size: 15px; font-weight: 600; }
.meta { color: var(--ink3); font-size: 12.5px; }
.muted { color: var(--ink2); }
.to-fix { display: flex; flex-direction: column; gap: 6px; padding: 12px 18px; border-top: 1px solid var(--line); background: var(--sunken); }
.to-fix .th { font-size: 12px; font-weight: 600; color: var(--ink3); }
.to-fix ul { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 4px; }
.fix-item { display: flex; align-items: baseline; gap: 8px; padding: 2px 0; border: 0; background: none; color: inherit; font: inherit; text-align: left; user-select: text; }
.fix-item svg { flex: none; position: relative; top: 2px; }
button.fix-item { cursor: pointer; }
button.fix-item:hover strong { text-decoration: underline; }
.still { color: var(--ink3); }
.sr { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0 0 0 0); white-space: nowrap; }
.pad { margin: 0; padding: 16px 18px; border-top: 1px solid var(--line); }
.file-problems { list-style: none; margin: 0; padding: 12px 18px; display: flex; flex-direction: column; gap: 8px; border-top: 1px solid var(--line); }
.group-head { margin: 0; padding: 8px 18px; border-top: 1px solid var(--line); background: var(--sunken); color: var(--ink3); font-size: 12px; font-weight: 600; }
.rows { list-style: none; margin: 0; padding: 0; }
.row { display: grid; gap: 0 14px; padding: 9px 18px; border-top: 1px solid var(--line); line-height: 1.5; outline: none; }
.rows.overview .row { grid-template-columns: 26px minmax(0, 360px) minmax(0, 1fr); }
.rows.editor .row { grid-template-columns: 26px minmax(0, 1fr); }
.row:focus-visible, .row:target { box-shadow: inset 3px 0 0 var(--accent); }
/* Problem rows keep their tint; the word and icon say which kind. */
.row.error { background: var(--bad-soft); }
.row.warning, .row.suggestion { background: var(--warn-soft); }
.num { color: var(--ink3); font-family: var(--mono); font-size: 12.5px; text-align: right; font-variant-numeric: tabular-nums; }
.row.error .num { color: var(--bad); }
.row.warning .num, .row.suggestion .num { color: var(--warn); }
/* Code and paths wrap anywhere, so a long path never breaks at a hyphen
   and leaves half a line empty. */
.code { font-family: var(--mono); font-size: 12.5px; overflow-wrap: anywhere; user-select: text; }
.code.unset, .unset-name { font-family: var(--mono); font-size: 12.5px; font-style: italic; color: var(--ink2); }
.meaning { display: flex; flex-direction: column; gap: 6px; min-width: 0; user-select: text; }
.problem { display: flex; flex-direction: column; gap: 6px; }
.problem.error { color: var(--bad); }
.problem.warning, .problem.suggestion { color: var(--warn); }
.word { display: flex; align-items: baseline; gap: 6px; }
.ic { flex: none; position: relative; top: 2px; }
.file-problems .problem { padding: 10px 12px; border-radius: var(--radius-sm); }
.file-problems .problem.error { background: var(--bad-soft); }
.file-problems .problem.warning { background: var(--warn-soft); }
.fix { color: var(--ink); }
.live { color: var(--ink3); font-size: 12px; }
code.new { align-self: flex-start; max-width: 100%; box-sizing: border-box; padding: 4px 8px; border: 1px solid var(--line); border-radius: 8px; background: var(--sunken); color: var(--ink); font-family: var(--mono); font-size: 12.5px; word-break: break-all; }
.acts { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
.path { display: flex; flex-wrap: wrap; align-items: center; gap: 4px 10px; color: var(--ink); font-size: 13px; }
.path .p { min-width: 0; font-family: var(--mono); font-size: 12px; word-break: break-all; }
.tag { height: 20px; font-size: 11.5px; }
.links { display: inline-flex; flex-wrap: wrap; gap: 4px 10px; }
.link { padding: 0; border: 0; background: none; color: var(--accent-text); font: inherit; font-size: 12.5px; white-space: nowrap; cursor: pointer; }
.link:hover:not(:disabled) { text-decoration: underline; }
.link:disabled { color: var(--ink3); cursor: default; }
@media (max-width: 760px) {
  .rows.overview .row { grid-template-columns: 26px minmax(0, 1fr); }
  .rows.overview .meaning { grid-column: 2; }
}
</style>
