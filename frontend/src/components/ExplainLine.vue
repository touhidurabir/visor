<script setup lang="ts">
// One explained line of a config: its number, the line as written (key
// highlighted apart from its value), what it means, its problems with how
// to fix them, and the files and folders it points to, checked on disk.
//
// mode "overview" puts the code beside the meaning and sends fixes to the
// editor; mode "editor" sits beside an editor, so the code goes above the
// meaning and fixes apply straight to the text. actions=false hides the
// fix and go-to-line buttons (the Add new form has no text to change).
import { computed } from 'vue'
import type { EditView, ExplainLineView, LinePathView, ProblemView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { fileSize } from '../lib/explain'
import { codeTokens } from '../lib/explain-cards'
import { openFolderLabel, revealLabel, showFileLabel } from '../lib/platform'

const props = defineProps<{ line: ExplainLineView; mode: 'overview' | 'editor'; readOnly: boolean; actions: boolean }>()
const emit = defineEmits<{
  edit: [edit: EditView]
  add: [after: number, lines: string[]]
  goto: [line: number]
  open: [path: string]
  reveal: [path: string]
  openLog: [path: string]
  viewLog: [process: string, stream: string]
}>()

const l = computed(() => props.line)
const editor = computed(() => props.mode === 'editor')
const tokens = computed(() => codeTokens(l.value.Text, l.value.Kind))

function levelWord(p: ProblemView) {
  return p.Level === 'error' ? 'Error.' : 'Warning.'
}
function fixLabel() {
  return editor.value ? 'Apply fix' : 'Apply fix in the editor'
}
function addLabel() {
  const n = l.value.Add?.length ?? 0
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
  <li :id="l.Kind === 'unset' ? undefined : `explain-line-${l.Line}`" :class="['row', mode, l.Level]" tabindex="-1">
    <span class="num" :aria-label="l.Kind === 'unset' ? 'Not in the file' : `Line ${l.Line}`"><template v-if="l.Kind !== 'unset'">{{ l.Line }}</template></span>
    <code :class="['code', { unset: l.Kind === 'unset' }]"><span v-for="(t, i) in tokens" :key="i" :class="`c-${t.part}`">{{ t.text }}</span></code>
    <span class="meaning">
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
            <button type="button" class="btn sm" :disabled="readOnly" @click="emit('add', l.Line, l.Add)">{{ addLabel() }}</button>
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
            <button type="button" class="link" :disabled="!p.Exists" :title="p.Exists ? undefined : 'Nothing to show: the file doesn’t exist'" @click="emit('reveal', p.Path)">{{ revealLabel() }}</button>
          </template>
          <template v-else-if="p.Kind === 'dir'">
            <button type="button" class="link" :disabled="!p.Exists" :title="p.Exists ? undefined : 'Nothing to open: the folder doesn’t exist'" @click="emit('open', p.Path)">{{ openFolderLabel() }}</button>
          </template>
          <template v-else>
            <button type="button" class="link" :disabled="!p.Exists" @click="emit('openLog', p.Path)">Open in editor</button>
            <button type="button" class="link" :disabled="!p.Exists" @click="emit('reveal', p.Path)">{{ showFileLabel() }}</button>
            <button v-if="!editor && p.Process" type="button" class="link" @click="emit('viewLog', p.Process, p.Stream)">View in Logs tab</button>
          </template>
        </span>
      </span>
    </span>
  </li>
</template>

<style scoped>
.row { display: grid; gap: 0 14px; padding: 9px 18px; border-top: 1px solid var(--line); line-height: 1.5; outline: none; }
.row.overview { grid-template-columns: 26px minmax(0, 360px) minmax(0, 1fr); }
/* Beside the editor there is no room for two columns: the line as written
   on top, what it means under it, and a gap before the next line. */
.row.editor { grid-template-columns: 26px minmax(0, 1fr); row-gap: 4px; padding: 10px 16px 12px; }
.row.editor .meaning { grid-column: 2; }
.row:focus-visible, .row:target { box-shadow: inset 3px 0 0 var(--accent); }
/* Problem rows keep their tint; the word and icon say which kind. */
.row.error { background: var(--bad-soft); }
.row.warning, .row.suggestion { background: var(--warn-soft); }
.num { color: var(--ink3); font-family: var(--mono); font-size: 12.5px; text-align: right; font-variant-numeric: tabular-nums; }
.row.error .num { color: var(--bad); }
.row.warning .num, .row.suggestion .num { color: var(--warn); }
/* The line as written: mono, the key (or the section's name) in the
   accent's text colour, the value in ink, punctuation and comments quiet.
   Code and paths wrap anywhere, so a long path never breaks at a hyphen
   and leaves half a line empty. */
.code { min-width: 0; color: var(--ink); font-family: var(--mono); font-size: 12.5px; overflow-wrap: anywhere; white-space: pre-wrap; user-select: text; }
.c-key { color: var(--accent-text); font-weight: 600; }
.c-name { color: var(--ink); font-weight: 600; }
.c-kind, .c-punct, .c-comment { color: var(--ink3); }
.c-comment { font-style: italic; }
.code.unset { font-style: italic; color: var(--ink2); }
.meaning { display: flex; flex-direction: column; gap: 6px; min-width: 0; user-select: text; }
.problem { display: flex; flex-direction: column; gap: 6px; }
.problem.error { color: var(--bad); }
.problem.warning, .problem.suggestion { color: var(--warn); }
.word { display: flex; align-items: baseline; gap: 6px; }
.ic { flex: none; position: relative; top: 2px; }
.fix { color: var(--ink); }
.live { color: var(--ink3); font-size: 12px; }
code.new { align-self: flex-start; max-width: 100%; box-sizing: border-box; padding: 4px 8px; border: 1px solid var(--line); border-radius: 8px; background: var(--sunken); color: var(--ink); font-family: var(--mono); font-size: 12.5px; word-break: break-all; }
.acts { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
.path { display: flex; flex-wrap: wrap; align-items: center; gap: 4px 10px; color: var(--ink); font-size: 13px; }
.path .meta { color: var(--ink3); font-size: 12.5px; }
.path .p { min-width: 0; font-family: var(--mono); font-size: 12px; word-break: break-all; }
.tag { height: 20px; font-size: 11.5px; }
.links { display: inline-flex; flex-wrap: wrap; gap: 4px 10px; }
.link { padding: 0; border: 0; background: none; color: var(--accent-text); font: inherit; font-size: 12.5px; white-space: nowrap; cursor: pointer; }
.link:hover:not(:disabled) { text-decoration: underline; }
.link:disabled { color: var(--ink3); cursor: default; }
@media (max-width: 760px) {
  .row.overview { grid-template-columns: 26px minmax(0, 1fr); }
  .row.overview .meaning { grid-column: 2; }
}
</style>
