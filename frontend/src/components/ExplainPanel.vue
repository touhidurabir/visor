<script setup lang="ts">
// "What this config does": one card per section of a config (ExplainCard),
// each with the program's live state, what it does in one sentence and,
// opened, its lines with what they mean, their problems and fixes, and the
// paths they point to. Above the cards: how many programs and problems,
// Expand all / Collapse all, and All lines / Only problems. The slot goes
// under the header (the Add new form's "things to check").
//
// processes are the processes supervisord runs from this file, from the
// live overview, so a card's state follows the monitor; null (a new file)
// shows no state. Failing cards start open, and so does the only card;
// the rest start closed (lib/explain-cards).
//
// mode "overview" puts each line beside its meaning and sends fixes to the
// editor; mode "editor" sits beside an editor, so lines are stacked over
// their meaning and fixes apply to the text. fixList adds "To fix before
// saving" above the cards, each problem linking to its line (goto).
// The Overview opens on "Only problems" when the file has any (flow 2);
// the choice is made once, when the explanation first arrives, and stays
// the user's after that. A failing card is never hidden by it.
import { computed, nextTick, ref, watch } from 'vue'
import type { EditView, ExplainView, ProcessView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { buildCards, cardOfLine, emptyOpen, hiddenNote, nextOpen, sectionCounts, setAll, toggled, type OpenState } from '../lib/explain-cards'
import { fixItems } from '../lib/fix-list'
import { opensOnProblems } from '../lib/overview'
import ExplainCard from './ExplainCard.vue'

const props = withDefaults(
  defineProps<{
    view: ExplainView | null
    failed?: string | null
    mode: 'overview' | 'editor'
    readOnly?: boolean
    actions?: boolean
    fixList?: boolean
    processes?: ProcessView[] | null
  }>(),
  { failed: null, readOnly: false, actions: true, fixList: false, processes: null },
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

const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`
const ready = computed(() => !props.failed && !!props.view && !props.view.ParseError)
/** Every card, whatever the filter: what the open state is decided on. */
const all = computed(() => buildCards(ready.value ? props.view : null, props.processes, false).cards)
const shown = computed(() => buildCards(ready.value ? props.view : null, props.processes, onlyProblems.value))
const problemCount = computed(() => (props.view?.Errors ?? 0) + (props.view?.Suggestions ?? 0))
const toFix = computed(() => (props.fixList && !props.failed ? fixItems(props.view) : []))

const openState = ref<OpenState>(emptyOpen())
watch(
  all,
  (cards) => {
    if (cards.length) openState.value = nextOpen(openState.value, cards)
  },
  { immediate: true },
)
const keys = computed(() => shown.value.cards.map((c) => c.key))
const toggle = (key: string) => (openState.value = toggled(openState.value, key))
const expandAll = () => (openState.value = setAll(openState.value, keys.value, true))
const collapseAll = () => (openState.value = setAll(openState.value, keys.value, false))

/**
 * showLine brings line n into view: opens its card (and shows all lines
 * when "Only problems" hides it), then scrolls to it and focuses it.
 */
async function showLine(n: number) {
  const key = cardOfLine(all.value, n)
  if (!key) return
  const card = shown.value.cards.find((c) => c.key === key)
  if (!card || !card.lines.some((l) => l.Kind !== 'unset' && l.Line === n)) onlyProblems.value = false
  openState.value = toggled(openState.value, key, true)
  await nextTick()
  const el = document.getElementById(`explain-line-${n}`)
  el?.scrollIntoView({ block: 'center', behavior: 'smooth' })
  el?.focus({ preventScroll: true })
}
defineExpose({ showLine })
</script>

<template>
  <section class="panel" aria-labelledby="explain-title" data-testid="explain">
    <div class="head">
      <div class="title">
        <div class="line">
          <h2 id="explain-title">What this config does</h2>
          <span v-if="ready" class="sub" data-testid="explain-counts">· {{ sectionCounts(view) }}</span>
        </div>
        <div v-if="ready && view" class="counts">
          <span v-if="view.Errors" class="badge b-bad">{{ plural(view.Errors, 'error', 'errors') }}</span>
          <span v-if="view.Suggestions" class="badge b-warn">{{ plural(view.Suggestions, 'suggestion', 'suggestions') }}</span>
          <span class="badge b-idle">{{ plural(view.Settings, 'setting', 'settings') }} explained</span>
          <span v-if="view.Paths" class="badge b-idle">{{ plural(view.Paths, 'path', 'paths') }} checked</span>
        </div>
        <span class="meta">From the file, no AI.<template v-if="!editor"> Every file or folder a line points to is checked on disk and can be opened from here.</template></span>
      </div>
      <div v-if="ready" class="controls">
        <button type="button" class="btn sm" :disabled="!keys.length" @click="expandAll">Expand all</button>
        <button type="button" class="btn sm" :disabled="!keys.length" @click="collapseAll">Collapse all</button>
        <div class="seg" role="group" aria-label="Which lines to show">
          <button type="button" :aria-pressed="!onlyProblems" @click="onlyProblems = false">All lines</button>
          <button type="button" :aria-pressed="onlyProblems" @click="onlyProblems = true">{{ problemCount ? `Only problems · ${problemCount}` : 'Only problems' }}</button>
        </div>
      </div>
    </div>

    <div v-if="$slots.default" class="slot"><slot /></div>

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
        <span class="word"><svg class="ic" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.6" stroke-linecap="round" aria-hidden="true"><path d="M18 6 6 18M6 6l12 12" /></svg><span><strong>Visor can’t read this file as a config yet. Error<template v-if="view.ParseError.Line"> on line {{ view.ParseError.Line }}</template>.</strong> {{ view.ParseError.Message }}</span></span>
        <span v-if="view.ParseError.Fix" class="fix"><strong>How to fix:</strong> {{ view.ParseError.Fix }}</span>
        <span v-if="actions && view.ParseError.Line" class="acts">
          <button type="button" class="btn sm" @click="emit('goto', view.ParseError.Line)">{{ editor ? `Go to line ${view.ParseError.Line}` : `Edit line ${view.ParseError.Line}` }}</button>
        </span>
      </div>
    </div>

    <template v-else>
      <ul v-if="view.FileProblems?.length" class="file-problems">
        <li v-for="(p, i) in view.FileProblems" :key="i" :class="['problem', p.Level]">
          <span class="word"><svg v-if="p.Level === 'error'" class="ic" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.6" stroke-linecap="round" aria-hidden="true"><path d="M18 6 6 18M6 6l12 12" /></svg><svg v-else class="ic" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" aria-hidden="true"><path d="M12 8v5M12 16h.01" /><circle cx="12" cy="12" r="9" /></svg><span><strong>{{ p.Level === 'error' ? 'Error.' : 'Warning.' }}</strong> {{ p.Message }}</span></span>
          <span v-if="p.Fix" class="fix"><strong>How to fix:</strong> {{ p.Fix }}</span>
          <span v-if="actions && (p.Edit || p.Line)" class="acts">
            <button v-if="p.Edit" type="button" class="btn primary sm" :disabled="readOnly" @click="emit('edit', p.Edit)">{{ editor ? 'Apply fix' : 'Apply fix in the editor' }}</button>
            <button v-if="p.Line" type="button" class="btn sm" @click="emit('goto', p.Line)">{{ editor ? `Go to line ${p.Line}` : `Edit line ${p.Line}` }}</button>
          </span>
        </li>
      </ul>

      <p v-if="!shown.cards.length" class="pad muted">{{ onlyProblems ? 'No problems found.' : 'This file has no settings yet.' }}</p>

      <ExplainCard
        v-for="c in shown.cards"
        :key="c.key"
        :card="c"
        :open="openState.open.has(c.key)"
        :mode="mode"
        :read-only="readOnly"
        :actions="actions"
        @toggle="toggle(c.key)"
        @edit="(e) => emit('edit', e)"
        @add="(a, ls) => emit('add', a, ls)"
        @goto="(n) => emit('goto', n)"
        @open="(p) => emit('open', p)"
        @reveal="(p) => emit('reveal', p)"
        @open-log="(p) => emit('openLog', p)"
        @view-log="(p, st) => emit('viewLog', p, st)"
      />

      <p v-if="onlyProblems && shown.hidden" class="hidden-note">
        {{ hiddenNote(shown.hidden) }}
        <button type="button" class="link" @click="onlyProblems = false">Show all lines</button>
      </p>
    </template>
  </section>
</template>

<style scoped>
.panel { min-width: 0; overflow: hidden; border: 1px solid var(--line); border-radius: 10px; background: var(--raised); }
.head { display: flex; flex-wrap: wrap; justify-content: space-between; align-items: flex-start; gap: 10px 14px; padding: 14px 18px 12px; }
.title { display: flex; flex-direction: column; gap: 6px; margin-right: auto; min-width: 0; }
.line { display: flex; flex-wrap: wrap; align-items: baseline; gap: 2px 6px; }
h2 { margin: 0; font-size: 15px; font-weight: 600; }
.sub { color: var(--ink2); font-size: 13.5px; }
.counts { display: flex; flex-wrap: wrap; gap: 6px; }
.meta { color: var(--ink3); font-size: 12.5px; }
.controls { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; }
.controls .seg { margin-left: 4px; }
.muted { color: var(--ink2); }
.slot { padding: 0 18px 12px; }
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
.problem { display: flex; flex-direction: column; gap: 6px; }
.problem.error { color: var(--bad); }
.problem.warning { color: var(--warn); }
.file-problems .problem { padding: 10px 12px; border-radius: var(--radius-sm); }
.file-problems .problem.error { background: var(--bad-soft); }
.file-problems .problem.warning { background: var(--warn-soft); }
.word { display: flex; align-items: baseline; gap: 6px; }
.ic { flex: none; position: relative; top: 2px; }
.fix { color: var(--ink); }
.acts { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
.hidden-note { display: flex; flex-wrap: wrap; align-items: baseline; gap: 4px 8px; margin: 0; padding: 10px 18px; border-top: 1px solid var(--line); color: var(--ink3); font-size: 12.5px; }
.link { padding: 0; border: 0; background: none; color: var(--accent-text); font: inherit; cursor: pointer; }
.link:hover { text-decoration: underline; }
</style>
