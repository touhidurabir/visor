<script setup lang="ts">
// Add new, "Write it": a file name and a text editor, with
// a plain-English summary above and every line explained beside it as you
// type: problems highlighted, with fixes that apply to the text.
import { computed, nextTick, ref } from 'vue'
import { RouterLink } from 'vue-router'
import type { EditView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import AddHeader from '../components/AddHeader.vue'
import CreatedCard from '../components/CreatedCard.vue'
import IniEditor from '../components/IniEditor.vue'
import LineByLine from '../components/LineByLine.vue'
import { addLines, applyEdit, errorsOnLines, explainMarks, fixToSave } from '../lib/explain'
import { openLog, openPath, reveal } from '../lib/finder'
import { resetNewConfig, useNewConfig } from '../lib/new-config'

const n = useNewConfig()
const { draft } = n
const editor = ref<InstanceType<typeof IniEditor>>()

const view = computed(() => n.explained.value)
const marks = computed(() => (n.upToDate.value ? explainMarks(view.value) : []))
const blocking = computed(() => (n.upToDate.value ? (view.value?.SaveErrors ?? n.errorCount.value) : 0))
const cantSave = computed(() => {
  if (n.busy.value) return 'Wait for the current action to finish.'
  if (!draft.state.fileName.trim()) return 'Give the file a name first.'
  if (n.hasErrors.value) return `${fixToSave(blocking.value || n.errorCount.value, errorsOnLines(view.value))}: see Line by line.`
  return ''
})
const saveLabel = computed(() => {
  if (n.busy.value) return 'Saving…'
  if (n.hasErrors.value) return fixToSave(blocking.value || n.errorCount.value, errorsOnLines(view.value))
  return 'Save & apply'
})

function fix(e: EditView) {
  draft.setContent(applyEdit(draft.state.content, e))
  void nextTick(() => editor.value?.goToLine(e.Insert ? e.Line + 1 : e.Line))
}
function add(after: number, lines: string[]) {
  draft.setContent(addLines(draft.state.content, after, lines))
  void nextTick(() => editor.value?.goToLine(after + 1))
}
</script>

<template>
  <div class="page">
    <AddHeader mode="raw" />
    <p v-if="n.loadError.value" class="alert" role="alert">
      Couldn’t get ready. {{ n.loadError.value }}
      <button type="button" class="btn" @click="n.retry">Try again</button>
    </p>
    <p v-else-if="!n.ready.value" class="muted">Loading…</p>
    <p v-if="n.ready.value && view && !view.ParseError" class="summary" data-testid="explain-summary">
      {{ view.Summary }}
      <span v-if="n.hasErrors.value" class="bad">It can’t be saved yet: {{ fixToSave(blocking || n.errorCount.value, errorsOnLines(view)).replace('Fix', 'fix').replace(' to save', '') }} first.</span>
    </p>
    <div v-if="n.ready.value" class="grid">
      <div class="col">
        <div class="name-row">
          <label for="fname" class="strong">File name</label>
          <input
            id="fname"
            class="inp mono"
            :value="draft.state.fileName"
            spellcheck="false"
            autocomplete="off"
            @input="draft.setFileName(($event.target as HTMLInputElement).value)"
          />
          <span class="mono muted">in {{ n.dir.value }}/</span>
        </div>
        <IniEditor
          ref="editor"
          :model-value="draft.state.content"
          :marks="marks"
          label="Config contents"
          @update:model-value="draft.setContent"
        />
        <div class="muted">The explanation on the right follows as you type.</div>
      </div>

      <div class="col">
        <LineByLine
          :view="view"
          :failed="n.checkFailed.value"
          mode="editor"
          :read-only="n.busy.value || !n.upToDate.value"
          @goto="(l: number) => editor?.goToLine(l)"
          @edit="fix"
          @add="add"
          @reveal="reveal"
          @open="openPath"
          @open-log="openLog"
        />
      </div>
    </div>
    <template v-if="n.ready.value">
      <p v-if="n.failure.value" class="alert" role="alert">{{ n.failure.value }}</p>
      <CreatedCard v-if="n.created.value" v-bind="{ path: n.created.value.path, fileName: n.created.value.fileName, result: n.created.value.res }" @open="resetNewConfig" />
      <div class="buttons">
        <span class="muted">Saving writes the file, asks supervisord to re-read its configs, and adds the new program.</span>
        <RouterLink class="btn" to="/" @click="resetNewConfig">Cancel</RouterLink>
        <button type="button" class="btn primary" data-testid="save-new" :disabled="!!cantSave" :title="cantSave || undefined" @click="n.save">
          {{ saveLabel }}
        </button>
      </div>
    </template>
  </div>
</template>

<style scoped>
.page { display: flex; flex-direction: column; gap: 18px; }
.grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(400px, 1fr)); gap: 18px; align-items: start; }
.summary { margin: 0; max-width: 900px; font-size: 14px; line-height: 1.55; user-select: text; }
.summary .bad { color: var(--failing-fg); }
.col { display: flex; flex-direction: column; gap: 16px; min-width: 0; }
.col:first-child { gap: 10px; }
.name-row { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
.strong { font-weight: 600; }
.muted { color: var(--text-muted); }
.mono { font-family: var(--font-mono); font-size: 12px; }
.inp { min-height: 32px; width: 200px; padding: 0 10px; border: 1px solid var(--line-strong); border-radius: var(--radius-sm); background: var(--bg); color: var(--text); box-sizing: border-box; }
.inp:focus-visible { outline: 2px solid var(--focus); outline-offset: 1px; }
.buttons { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; justify-content: flex-end; }
.buttons .muted { margin-right: auto; }
.btn { display: inline-flex; align-items: center; gap: 6px; min-height: 32px; padding: 0 12px; border-radius: var(--radius-sm); border: 1px solid var(--line-strong); background: var(--bg); color: var(--text); font: inherit; text-decoration: none; white-space: nowrap; cursor: pointer; box-sizing: border-box; }
.btn:hover:not(:disabled) { background: var(--bg-hover); }
.btn:disabled { color: var(--text-faint); cursor: default; }
.btn.primary { background: var(--accent); border-color: var(--accent); color: var(--on-accent); font-weight: 600; }
.btn.primary:hover:not(:disabled) { background: var(--accent-hover); border-color: var(--accent-hover); }
.btn.primary:disabled { opacity: 0.55; color: var(--on-accent); }
.alert { margin: 0; padding: 12px; border: 1px solid var(--failing-dot); border-radius: var(--radius-md); background: var(--failing-bg); color: var(--failing-fg); display: flex; flex-wrap: wrap; gap: 8px; align-items: center; user-select: text; }
</style>
