<script setup lang="ts">
// Add new, "Import files": config files from elsewhere (a repo, a file an
// assistant wrote), picked with Choose files… or dropped on the page. Each
// is a card: the name it gets in the include folder (its own, with the
// include setting's extension), whether it's ready, its problems, and Edit
// to open it in the editor with the explanation beside it. They are checked
// together (a program name two of them share is a duplicate), then added in
// one go, and started if "Start them after adding" is ticked. The originals
// are only read.
import { computed, nextTick, ref } from 'vue'
import { RouterLink } from 'vue-router'
import type { EditView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import AddHeader from '../components/AddHeader.vue'
import ExplainPanel from '../components/ExplainPanel.vue'
import IniEditor from '../components/IniEditor.vue'
import ReadOnlyBanner from '../components/ReadOnlyBanner.vue'
import SaveBar from '../components/SaveBar.vue'
import { addLines, applyEdit, explainMarks } from '../lib/explain'
import { openLog, openPath, reveal } from '../lib/finder'
import { fixItems } from '../lib/fix-list'
import { readOnlyReason, useFolder } from '../lib/folder'
import { addFiles, addLabel, MAX_IMPORT_FILES, nativeDrops, programsOf, removeFile, resetImport, resultLine, targetName, useImport, type ImportFile } from '../lib/import-files'
import { showToast } from '../lib/toast'
import { configRoute } from '../lib/links'

const im = useImport()
const picker = ref<HTMLInputElement>()
const editors = ref<Record<number, InstanceType<typeof IniEditor> | null>>({})
const dragging = ref(false)
/** dragenter/dragleave fire for every child, so the highlight follows a count. */
let depth = 0

const folder = useFolder()
const readOnly = computed(() => readOnlyReason(folder.value))

const view = (f: ImportFile) => im.views.value[f.id] ?? null
const checked = (f: ImportFile) => im.upToDate.value && !!view(f)
const errors = (f: ImportFile) => fixItems(view(f)).filter((i) => i.level === 'error')

/** The status chip of one file. */
function status(f: ImportFile): { tone: 'ok' | 'bad' | 'warn' | 'idle'; text: string } {
  if (f.readError) return { tone: 'bad', text: 'Can’t use it' }
  if (!f.include) return { tone: 'idle', text: 'Left out' }
  if (!checked(f)) return { tone: 'idle', text: 'Checking…' }
  const n = view(f)?.SaveErrors ?? 0
  if (n) return { tone: 'bad', text: n === 1 ? '1 problem to fix' : `${n} problems to fix` }
  return { tone: 'ok', text: 'Ready' }
}

const renamed = (f: ImportFile) => f.fileName !== f.source && f.fileName === targetName(f.source, im.ext.value)

const bar = computed<{ tone: 'ok' | 'bad' | 'warn' | 'idle'; status: string }>(() => {
  if (im.busy.value) return { tone: 'idle', status: 'Adding…' }
  if (im.checkFailed.value) return { tone: 'bad', status: `Couldn’t check the files: ${im.checkFailed.value}` }
  if (readOnly.value) return { tone: 'bad', status: `${readOnly.value}.` }
  if (!im.files.length) return { tone: 'idle', status: 'Choose or drop the config files to add.' }
  if (!im.included.value.length) return { tone: 'idle', status: 'Tick a file to add it.' }
  if (!im.upToDate.value) return { tone: 'idle', status: 'Checking…' }
  const b = im.blocked.value
  if (b.length) return { tone: 'bad', status: `Fix ${b.length === 1 ? b[0].fileName : `${b.length} files`} to add, or untick ${b.length === 1 ? 'it' : 'them'}.` }
  return { tone: 'ok', status: `${im.included.value.length === 1 ? 'Ready to add.' : `All ${im.included.value.length} ready to add.`}` }
})
const cantAdd = computed(() => !!readOnly.value || im.busy.value || !im.included.value.length || !im.upToDate.value || im.blocked.value.length > 0)

function choose() {
  picker.value?.click()
}
async function add(list: File[]) {
  const skipped = await addFiles(list)
  if (skipped) showToast(`Visor imports at most ${MAX_IMPORT_FILES} files at a time; ${skipped === 1 ? 'one was' : `${skipped} were`} left out.`, 'error')
}
function picked(e: Event) {
  const input = e.target as HTMLInputElement
  if (input.files?.length) void add(Array.from(input.files))
  input.value = '' // the same file can be picked again
}
function dragEnter(e: DragEvent) {
  if (!e.dataTransfer?.types.includes('Files')) return
  depth++
  dragging.value = true
}
function dragLeave() {
  depth = Math.max(0, depth - 1)
  if (!depth) dragging.value = false
}
function dropped(e: DragEvent) {
  depth = 0
  dragging.value = false
  // The desktop app gets drops natively (lib/import-files followDrops); the
  // page only reads them in the server build.
  if (nativeDrops()) return
  // A file dropped on an open editor goes into its text (CodeMirror's own drop).
  if ((e.target as HTMLElement | null)?.closest?.('.cm-editor')) return
  e.preventDefault()
  const list = e.dataTransfer?.files
  if (list?.length && !im.busy.value) void add(Array.from(list))
}

function goto(f: ImportFile, line: number) {
  f.open = true
  void nextTick(() => editors.value[f.id]?.goToLine(line || 1))
}
function fix(f: ImportFile, e: EditView) {
  f.content = applyEdit(f.content, e)
  void nextTick(() => editors.value[f.id]?.goToLine(e.Insert ? e.Line + 1 : e.Line))
}
function addAfter(f: ImportFile, after: number, lines: string[]) {
  f.content = addLines(f.content, after, lines)
  void nextTick(() => editors.value[f.id]?.goToLine(after + 1))
}
</script>

<template>
  <div class="page" data-file-drop-target @dragenter="dragEnter" @dragover.prevent @dragleave="dragLeave" @drop="dropped">
    <AddHeader mode="import" />
    <ReadOnlyBanner />

    <section :class="['drop', { on: dragging, small: im.files.length > 0 }]" aria-labelledby="drop-title" data-testid="import-drop">
      <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 15V3" /><path d="m7 8 5-5 5 5" /><path d="M20 15v4a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2v-4" /></svg>
      <div class="drop-text">
        <h2 id="drop-title" class="strong">{{ im.files.length ? 'Add more files' : 'Bring in configs you already have' }}</h2>
        <p class="meta">
          Drop <span class="mono">.ini</span> or <span class="mono">.conf</span> files here, from a repo or written by an assistant. Visor copies them into
          <span class="mono">{{ im.dir.value || 'the include folder' }}/</span>; the originals stay as they are.
        </p>
      </div>
      <button type="button" class="btn" :disabled="im.busy.value" @click="choose">Choose files…</button>
      <input ref="picker" class="hidden" type="file" multiple data-testid="import-picker" aria-label="Choose config files" @change="picked" />
    </section>

    <ul v-if="im.files.length" class="files" aria-label="Files to import">
      <li v-for="f in im.files" :key="f.id" :class="['file', { out: !f.include }]" :data-testid="`import-file-${f.source}`">
        <div class="head">
          <label class="pick">
            <input v-model="f.include" type="checkbox" :disabled="!!f.readError || im.busy.value" :aria-label="`Add ${f.source}`" />
          </label>
          <div class="names">
            <span class="source mono" :title="f.source">{{ f.source }}</span>
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><path d="M5 12h14" /><path d="m13 6 6 6-6 6" /></svg>
            <input
              v-model.trim="f.fileName"
              class="input mono name"
              spellcheck="false"
              autocomplete="off"
              :disabled="!!f.readError || im.busy.value"
              :aria-label="`File name for ${f.source}`"
            />
          </div>
          <span :class="['badge', `b-${status(f).tone}`]" data-testid="import-status"><span :class="['dot', `d-${status(f).tone}`]" aria-hidden="true" />{{ status(f).text }}</span>
          <span class="acts">
            <button v-if="!f.readError" type="button" class="btn sm" :aria-expanded="f.open" @click="f.open = !f.open">{{ f.open ? 'Done' : 'Edit' }}</button>
            <button type="button" class="btn quiet icon sm" :disabled="im.busy.value" :aria-label="`Remove ${f.source} from the list`" title="Remove from the list" @click="removeFile(f.id)">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><path d="M6 6l12 12M18 6L6 18" /></svg>
            </button>
          </span>
        </div>

        <p v-if="f.readError" class="why t-bad">{{ f.readError }}</p>
        <template v-else>
          <p class="meta sub">
            <template v-if="programsOf(view(f)).length">
              {{ programsOf(view(f)).length === 1 ? 'Program' : 'Programs' }}: <span class="mono">{{ programsOf(view(f)).join(', ') }}</span>.
            </template>
            <template v-if="renamed(f)"> Renamed from {{ f.source }}: supervisord here loads <span class="mono">*{{ im.ext.value }}</span> files only.</template>
          </p>
          <ul v-if="f.include && !f.open && checked(f) && errors(f).length" class="problems" :aria-label="`Problems in ${f.fileName}`">
            <li v-for="(p, i) in errors(f).slice(0, 4)" :key="i">
              <button type="button" class="problem" @click="goto(f, p.line)">
                <strong>{{ p.line ? `Line ${p.line}` : 'The file' }}</strong> · {{ p.text }}
              </button>
            </li>
            <li v-if="errors(f).length > 4" class="meta">and {{ errors(f).length - 4 }} more: Edit to see them all.</li>
          </ul>
          <div v-if="f.open" class="grid">
            <IniEditor
              :ref="(el) => (editors[f.id] = el as InstanceType<typeof IniEditor> | null)"
              v-model="f.content"
              :read-only="im.busy.value"
              :marks="checked(f) ? explainMarks(view(f)) : []"
              :label="`Contents of ${f.fileName}`"
            />
            <ExplainPanel
              :view="view(f)"
              :failed="im.checkFailed.value"
              mode="editor"
              fix-list
              :read-only="im.busy.value || !checked(f)"
              @goto="(l) => goto(f, l)"
              @edit="(e) => fix(f, e)"
              @add="(after, lines) => addAfter(f, after, lines)"
              @reveal="reveal"
              @open="openPath"
              @open-log="openLog"
            />
          </div>
        </template>
      </li>
    </ul>

    <div v-if="im.failure.value || im.results.value" class="results">
      <p v-if="im.failure.value" class="alert" role="alert">{{ im.failure.value }}</p>
      <section v-if="im.results.value" class="done" aria-labelledby="done-title" data-testid="import-results">
        <strong id="done-title">{{ im.results.value.every((r) => resultLine(r).ok) ? 'Added' : 'Added, with problems' }}</strong>
        <ul>
          <li v-for="r in im.results.value" :key="r.Path" :class="resultLine(r).ok ? 'ok' : 'bad'">
            <RouterLink v-if="r.Written" :to="configRoute(r.Path)" class="mono">{{ r.FileName }}</RouterLink>
            <span v-else class="mono">{{ r.FileName }}</span>
            · {{ resultLine(r).text }}
          </li>
        </ul>
      </section>
    </div>

    <SaveBar :tone="bar.tone" :status="bar.status" note="Each file is written to the include folder and its programs are added to supervisord.">
      <button v-if="im.checkFailed.value" type="button" class="btn" @click="im.recheck">Check again</button>
      <label class="start"><input v-model="im.startAfter.value" type="checkbox" :disabled="im.busy.value" />Start them after adding</label>
      <RouterLink class="btn" to="/" @click="resetImport">Cancel</RouterLink>
      <button type="button" class="btn primary" data-testid="import-add" :disabled="cantAdd" @click="im.importAll">
        {{ im.busy.value ? 'Adding…' : addLabel(im.included.value.length) }}
      </button>
    </SaveBar>
  </div>
</template>

<style scoped>
.page { display: flex; flex-direction: column; gap: 16px; min-height: 100%; }
.hidden { display: none; }
.strong { margin: 0; font-size: 14px; font-weight: 600; }
.meta { margin: 0; color: var(--ink3); font-size: 12.5px; }
.mono { font-family: var(--mono); font-size: 12px; }
.drop { display: flex; flex-wrap: wrap; align-items: center; gap: 14px; padding: 28px 24px; border: 1.5px dashed var(--line2); border-radius: var(--radius-md); background: var(--sunken); color: var(--ink2); transition: border-color 0.15s, background 0.15s; }
.drop.small { padding: 14px 18px; }
.drop.on, .page.file-drop-target-active .drop { border-color: var(--accent); background: var(--accent-soft); }
.drop svg { flex: none; color: var(--accent-text); }
.drop-text { display: flex; flex-direction: column; gap: 2px; flex: 1 1 320px; min-width: 0; }
.files { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 12px; }
.file { display: flex; flex-direction: column; gap: 8px; padding: 12px 16px; border: 1px solid var(--line); border-radius: 10px; background: var(--raised); min-width: 0; }
.file.out { opacity: 0.7; }
.head { display: flex; flex-wrap: wrap; align-items: center; gap: 10px; }
.pick { display: flex; align-items: center; }
.pick input { width: 15px; height: 15px; margin: 0; accent-color: var(--accent); }
.names { display: flex; align-items: center; gap: 8px; flex: 1 1 360px; min-width: 0; color: var(--ink3); }
.source { color: var(--ink); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 40%; user-select: text; }
.name { flex: 1 1 200px; min-width: 140px; max-width: 320px; }
.acts { display: flex; gap: 4px; margin-left: auto; }
.sub { padding-left: 25px; }
.why { margin: 0; padding-left: 25px; font-size: 12.5px; }
.problems { list-style: none; margin: 0; padding: 8px 12px; display: flex; flex-direction: column; gap: 4px; border-radius: var(--radius-md); background: var(--bad-soft); }
.problem { padding: 0; border: 0; background: none; color: var(--bad); font: inherit; font-size: 12.5px; text-align: left; cursor: pointer; user-select: text; }
.problem:hover strong { text-decoration: underline; }
.grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(420px, 1fr)); gap: 20px; align-items: start; }
.results { display: flex; flex-direction: column; gap: 12px; }
.alert { margin: 0; padding: 12px; border: 1px solid var(--bad-dot); border-radius: var(--radius-md); background: var(--bad-soft); color: var(--bad); user-select: text; }
.done { display: flex; flex-direction: column; gap: 8px; padding: 14px 16px; border: 1px solid var(--line); border-radius: 10px; background: var(--raised); }
.done ul { margin: 0; padding-left: 18px; display: flex; flex-direction: column; gap: 4px; user-select: text; }
.done li.bad { color: var(--bad); }
.done a { color: var(--accent-text); }
.start { display: flex; align-items: center; gap: 6px; margin-right: 4px; white-space: nowrap; }
.start input { width: 15px; height: 15px; margin: 0; accent-color: var(--accent); }
</style>
