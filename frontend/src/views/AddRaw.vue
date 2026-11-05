<script setup lang="ts">
// Add new, "Write it": a file name and a text editor, with a plain-English
// summary above and every line explained beside it as you type: problems
// listed first ("To fix before saving") and highlighted, with fixes that
// apply to the text. A draft copied from another config says what it
// renamed and points at the settings that still name the original. Save
// sits in the sticky bar at the bottom, with the reason when it can't.
import { computed, nextTick, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import type { EditView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import AddHeader from '../components/AddHeader.vue'
import CreatedCard from '../components/CreatedCard.vue'
import IniEditor from '../components/IniEditor.vue'
import LineByLine from '../components/LineByLine.vue'
import SaveBar from '../components/SaveBar.vue'
import { doubtMarks, liveDoubts, mergeMarks, renamesNote } from '../lib/copy-draft'
import { addLines, applyEdit, errorsOnLines, explainMarks, fixToSave } from '../lib/explain'
import { openLog, openPath, reveal } from '../lib/finder'
import { saveReason } from '../lib/fix-list'
import { resetNewConfig, useNewConfig } from '../lib/new-config'

const n = useNewConfig()
const { draft } = n
const editor = ref<InstanceType<typeof IniEditor>>()
const results = ref<HTMLElement>()

const view = computed(() => n.explained.value)
const doubts = computed(() => (n.copied.value ? liveDoubts(n.copied.value.doubts, draft.state, n.copied.value.copy) : []))
const fileInput = ref<HTMLInputElement>()
const marks = computed(() => mergeMarks(n.upToDate.value ? explainMarks(view.value) : [], doubtMarks(doubts.value)))
const blocking = computed(() => (n.upToDate.value ? (view.value?.SaveErrors ?? n.errorCount.value) : 0))
const reason = computed(() => saveReason(view.value) || fixToSave(blocking.value || n.errorCount.value, errorsOnLines(view.value)))

const bar = computed<{ tone: 'ok' | 'bad' | 'warn' | 'idle'; status: string }>(() => {
  if (n.busy.value) return { tone: 'idle', status: 'Saving…' }
  if (n.created.value) return n.created.value.res.Errors?.length ? { tone: 'warn', status: 'Saved, with problems.' } : { tone: 'ok', status: 'Saved and applied.' }
  if (!draft.state.fileName.trim()) return { tone: 'bad', status: 'Give the file a name to save.' }
  if (!n.upToDate.value) return { tone: 'idle', status: 'Checking…' }
  if (n.hasErrors.value) return { tone: 'bad', status: `${reason.value}.` }
  return { tone: 'ok', status: 'Ready to save.' }
})
const cantSave = computed(() => n.busy.value || !draft.state.fileName.trim() || n.hasErrors.value)

// A doubt about the file name (line 0) points at the name field.
function goto(line: number) {
  if (!line) fileInput.value?.focus()
  else editor.value?.goToLine(line)
}
function fix(e: EditView) {
  draft.setContent(applyEdit(draft.state.content, e))
  void nextTick(() => editor.value?.goToLine(e.Insert ? e.Line + 1 : e.Line))
}
function add(after: number, lines: string[]) {
  draft.setContent(addLines(draft.state.content, after, lines))
  void nextTick(() => editor.value?.goToLine(after + 1))
}

// The result of a save sits above the bar; bring it into view.
watch(
  () => [n.created.value, n.failure.value],
  ([c, f]) => {
    if (c || f) void nextTick(() => results.value?.scrollIntoView({ block: 'nearest' }))
  },
)
</script>

<template>
  <div class="page">
    <AddHeader mode="raw" />
    <p v-if="n.loadError.value" class="alert" role="alert">
      Couldn’t get ready. {{ n.loadError.value }}
      <button type="button" class="btn" @click="n.retry">Try again</button>
    </p>
    <p v-else-if="!n.ready.value" class="muted">Loading…</p>
    <p v-if="n.ready.value && view && !view.ParseError" class="summary" data-testid="explain-summary">{{ view.Summary }}</p>

    <div v-if="n.ready.value && n.copied.value" class="copied" role="status" data-testid="copy-note">
      <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="9" y="9" width="11" height="11" rx="2" /><path d="M5 15V6a2 2 0 0 1 2-2h8" /></svg>
      <span>
        <strong>Copied from {{ n.copied.value.fileName }}.</strong>
        <template v-if="n.copied.value.renames.length"> Renamed so both can run: <span class="mono">{{ renamesNote(n.copied.value.renames) }}</span>.</template>
        Nothing is written until you save.
      </span>
    </div>

    <div v-if="n.ready.value" class="grid">
      <div class="col">
        <div class="name-row">
          <label for="fname" class="strong">File name</label>
          <input
            id="fname"
            ref="fileInput"
            class="input mono"
            :value="draft.state.fileName"
            spellcheck="false"
            autocomplete="off"
            @input="draft.setFileName(($event.target as HTMLInputElement).value)"
          />
          <span class="meta">in <span class="mono">{{ n.dir.value }}/</span></span>
        </div>
        <div v-if="doubts.length" class="doubts" role="group" aria-labelledby="doubts-title" data-testid="copy-doubts">
          <span id="doubts-title" class="th">Check the copy</span>
          <ul>
            <li v-for="(d, i) in doubts" :key="i">
              <button type="button" class="doubt" @click="goto(d.Line)">
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 3 2 20h20L12 3zM12 10v4M12 17v.01" /></svg>
                <span><span class="sr">Warning: </span><strong>{{ d.Line ? `Line ${d.Line}` : 'File name' }}</strong> · {{ d.Message }}.</span>
              </button>
            </li>
          </ul>
        </div>
        <IniEditor
          ref="editor"
          :model-value="draft.state.content"
          :marks="marks"
          label="Config contents"
          @update:model-value="draft.setContent"
        />
        <p class="meta">The explanation on the right follows as you type.</p>
      </div>

      <LineByLine
        :view="view"
        :failed="n.checkFailed.value"
        mode="editor"
        fix-list
        :read-only="n.busy.value || !n.upToDate.value"
        @goto="goto"
        @edit="fix"
        @add="add"
        @reveal="reveal"
        @open="openPath"
        @open-log="openLog"
      />
    </div>

    <template v-if="n.ready.value">
      <div ref="results" class="results">
        <p v-if="n.failure.value" class="alert" role="alert">{{ n.failure.value }}</p>
        <CreatedCard v-if="n.created.value" v-bind="{ path: n.created.value.path, fileName: n.created.value.fileName, result: n.created.value.res }" @open="resetNewConfig" />
      </div>
      <SaveBar :tone="bar.tone" :status="bar.status" note="Saving writes the file, asks supervisord to re-read its configs, and adds the new program.">
        <RouterLink class="btn" to="/" @click="resetNewConfig">Cancel</RouterLink>
        <button type="button" class="btn primary" data-testid="save-new" :disabled="cantSave" @click="n.save">
          {{ n.busy.value ? 'Saving…' : 'Save & apply' }}
        </button>
      </SaveBar>
    </template>
  </div>
</template>

<style scoped>
.page { display: flex; flex-direction: column; gap: 16px; min-height: 100%; }
.grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(420px, 1fr)); gap: 20px; align-items: start; }
.summary { margin: 0; max-width: 80ch; color: var(--ink2); line-height: 1.55; user-select: text; }
.col { display: flex; flex-direction: column; gap: 10px; min-width: 0; }
.name-row { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
.strong { font-weight: 600; }
.muted { margin: 0; color: var(--ink2); }
.meta { margin: 0; color: var(--ink3); font-size: 12.5px; }
.mono { font-family: var(--mono); font-size: 12px; }
.name-row .input { width: 200px; }
.th { font-size: 12px; font-weight: 600; color: var(--ink3); }
.copied { display: flex; align-items: baseline; gap: 10px; padding: 10px 14px; border: 1px solid var(--line); border-radius: var(--radius-md); background: var(--accent-soft); color: var(--ink); line-height: 1.5; user-select: text; }
.copied svg { flex: none; position: relative; top: 3px; color: var(--accent-text); }
.copied .mono { overflow-wrap: anywhere; }
.doubts { display: flex; flex-direction: column; gap: 6px; padding: 10px 14px; border-radius: var(--radius-md); background: var(--warn-soft); }
.doubts ul { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 4px; }
.doubt { display: flex; align-items: baseline; gap: 8px; padding: 2px 0; border: 0; background: none; color: var(--warn); font: inherit; text-align: left; cursor: pointer; user-select: text; }
.doubt svg { flex: none; position: relative; top: 2px; }
.doubt:hover strong { text-decoration: underline; }
.sr { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0 0 0 0); white-space: nowrap; }
.results { display: flex; flex-direction: column; gap: 12px; }
.results:empty { display: none; }
.alert { margin: 0; padding: 12px; border: 1px solid var(--bad-dot); border-radius: var(--radius-md); background: var(--bad-soft); color: var(--bad); display: flex; flex-wrap: wrap; gap: 8px; align-items: center; user-select: text; }
</style>
