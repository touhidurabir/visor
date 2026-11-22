<script setup lang="ts">
// The Config tab (spec §7): edit the file with live checks, see exactly what
// a save changes, save with a backup, apply only this file's programs, and
// restore an older version. Nothing is written without a diff preview.
//
// The file is read with a hash; a save is refused when the file no longer
// has it (edited elsewhere), and then the edits stay here until the person
// chooses what to do with them.
//
// As in Add new, Save sits in the sticky bar at the bottom with where saving
// stands, and the explanation starts with "To fix before saving".
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { type ApplyResultView, type BackupView, type EditView, type ExplainView, type FileView, type LoadedDiffView, type ProcessView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { ConfigService } from '../lib/api'
import { classifySaveError, saveErrorText } from '../lib/config-errors'
import { addLines, applyEdit, explainMarks, fixToSave } from '../lib/explain'
import { saveReason } from '../lib/fix-list'
import { openLog, openPath, reveal } from '../lib/finder'
import { takePendingEdit } from '../lib/pending-edit'
import { copyText } from '../lib/clipboard'
import { diffLines, diffStats } from '../lib/diff'
import { dropDraft, keepDraft, takeDraft } from '../lib/edit-drafts'
import { describeError } from '../lib/faults'
import { historyTime } from '../lib/format'
import { showToast } from '../lib/toast'
import DialogShell from './DialogShell.vue'
import DiffView from './DiffView.vue'
import ExplainPanel from './ExplainPanel.vue'
import IniEditor from './IniEditor.vue'
import RestoreDialog from './RestoreDialog.vue'
import SaveBar from './SaveBar.vue'
import { currentTab, LOCAL, tabHref } from '../lib/tabs'
import { onProduction, withConsentOnce } from '../lib/production'
import { groupRestartNote } from '../lib/apply-flow'

// line (from ?line=N) is the line to show when the editor opens. processes
// are the file's processes from the live overview, for each card's state.
// serverReadOnly: the server is set to Read only, so supervisord is never
// asked whether it already runs a changed file.
const props = defineProps<{ path: string; fileName: string; changed: boolean; uncertain: boolean; serverReadOnly?: boolean; line?: number; processes?: ProcessView[] }>()

const CHECK_DELAY_MS = 300

const file = ref<FileView | null>(null)
const loadError = ref<string | null>(null)
const text = ref('')
const diskNewer = ref<FileView | null>(null) // the file on disk when it is not the one that was opened
const failure = ref<string | null>(null)
const busy = ref<'save' | 'restore' | 'apply' | null>(null)
const result = ref<{ title: string; res: ApplyResultView } | null>(null)
const restoredDraft = ref(false)

const explained = ref<ExplainView | null>(null)
const checkFailed = ref<string | null>(null)
const checkedText = ref<string | null>(null)
const loadedDiff = ref<LoadedDiffView | null>(null)
const loadedDiffError = ref<string | null>(null)
// What Visor knows from asking supervisord (LoadedDiff asks it first).
const loadedStatus = computed(() => {
  const v = loadedDiff.value
  if (!v) return ''
  if (props.serverReadOnly) return 'This server is Read only, so Visor doesn’t ask supervisord whether it already runs this file. If it was applied another way, this may be out of date.'
  if (v.CheckError) return `Couldn’t check with supervisord: ${v.CheckError}`
  if (v.Confirmed) return 'Checked with supervisord: it hasn’t loaded these changes yet. Apply them to load them.'
  return 'Visor checks with supervisord while this file shows as changed.'
})

const preview = ref(false)
const compare = ref(false)
const restoreOpen = ref(false)
const backups = ref<BackupView[]>([])
const editor = ref<InstanceType<typeof IniEditor>>()

const dirty = computed(() => !!file.value && text.value !== file.value.Content)
const writable = computed(() => !!file.value?.Writable)
const remote = computed(() => currentTab.value !== LOCAL)
const errorCount = computed(() => explained.value?.SaveErrors ?? 0)
const marks = computed(() => explainMarks(explained.value))
const saveDiff = computed(() => diffLines(file.value?.Content ?? '', text.value))
const saveStats = computed(() => diffStats(saveDiff.value))
const groupNote = computed(() => groupRestartNote(props.processes))
const cantSave = computed(() => {
  if (busy.value) return 'Wait for the current action to finish.'
  if (!writable.value) return props.serverReadOnly ? 'This server is set to Read only.' : remote.value ? 'Visor can’t write this file on the server.' : 'Visor can’t write this file.'
  if (diskNewer.value) return 'The file changed on disk. Choose what to do with your edits first.'
  if (!dirty.value) return 'There is nothing to save yet.'
  if (checkedText.value === text.value && errorCount.value) return saveReason(explained.value) || fixToSave(errorCount.value)
  return ''
})
const bar = computed<{ tone: 'ok' | 'bad' | 'warn' | 'idle'; status: string }>(() => {
  if (busy.value === 'save') return { tone: 'idle', status: 'Saving…' }
  if (busy.value === 'restore') return { tone: 'idle', status: 'Restoring…' }
  if (busy.value === 'apply') return { tone: 'idle', status: 'Applying…' }
  if (!writable.value) return { tone: 'warn', status: props.serverReadOnly ? 'Read only: this server is set to Read only.' : 'Read-only: Visor can’t write this file.' }
  if (diskNewer.value) return { tone: 'warn', status: 'Edited elsewhere: choose what to do with your edits first.' }
  if (dirty.value && checkedText.value === text.value && errorCount.value) return { tone: 'bad', status: `${cantSave.value}.` }
  if (dirty.value) return { tone: 'warn', status: 'Unsaved changes' }
  return { tone: 'idle', status: 'No changes to save' }
})

// ---- loading ----

let gone = false
onBeforeUnmount(() => {
  gone = true
  clearTimeout(timer)
})

async function load(keepEdits = false) {
  try {
    const f = await ConfigService.Read(props.path)
    if (gone) return
    file.value = f
    loadError.value = null
    diskNewer.value = null
    const draft = keepEdits ? text.value : takeDraft(props.path, f.Hash)
    restoredDraft.value = !keepEdits && draft !== null && draft !== f.Content
    text.value = draft ?? f.Content
    // A fix chosen on the Overview: applied to the text, not saved.
    // Its line numbers are the file's, so not when unsaved edits came back.
    const pending = keepEdits ? undefined : takePendingEdit(props.path)
    if (pending && f.Writable && restoredDraft.value) {
      showToast('The fix was not applied because your unsaved edits are back in the editor. Apply it again from the explanation beside the editor.', 'info')
    } else if (pending && f.Writable) {
      text.value = 'edit' in pending ? applyEdit(text.value, pending.edit) : addLines(text.value, pending.after, pending.lines)
    }
    await runChecks()
  } catch (e) {
    if (!gone) loadError.value = describeError(e)
  }
}
void load()

/**
 * Looks at the file on disk again. When it is not what the editor was
 * opened with: no edits → take it; edits → keep them and say so.
 */
async function refreshFromDisk() {
  try {
    const f = await ConfigService.Read(props.path)
    if (gone || !file.value || f.Hash === file.value.Hash) return
    if (!dirty.value) {
      file.value = f
      text.value = f.Content
      diskNewer.value = null
      void runChecks()
    } else {
      diskNewer.value = f
    }
  } catch (e) {
    if (!gone) failure.value = `Visor can’t read ${props.fileName} any more. ${describeError(e)} Copy your edits before you leave this page.`
  }
}

let loadedSeq = 0
async function loadLoadedDiff() {
  const seq = ++loadedSeq // only the latest answer is shown
  if (!props.changed && !props.uncertain) {
    loadedDiff.value = null
    return
  }
  try {
    const v = await ConfigService.LoadedDiff(props.path)
    if (gone || seq !== loadedSeq) return
    loadedDiff.value = v
    loadedDiffError.value = null
  } catch (e) {
    if (gone || seq !== loadedSeq) return
    loadedDiffError.value = describeError(e)
  }
}
void loadLoadedDiff()
// The file changed on disk or was applied: look at it again.
watch(
  () => [props.changed, props.uncertain],
  () => {
    loadedDiff.value = null // flagged again: not the last answer
    void loadLoadedDiff()
    if (!busy.value) void refreshFromDisk()
  },
)

// ---- checks ----

let timer: ReturnType<typeof setTimeout> | undefined
let checkSeq = 0

async function runChecks() {
  clearTimeout(timer)
  const mine = ++checkSeq
  const t = text.value
  try {
    const v = await ConfigService.Explain(props.fileName, t, props.path)
    if (mine !== checkSeq || gone) return
    explained.value = v
    checkFailed.value = null
    checkedText.value = t
  } catch (e) {
    if (mine !== checkSeq || gone) return
    checkFailed.value = describeError(e)
  }
}

watch(text, () => {
  if (file.value) keepDraft(props.path, file.value.Hash, file.value.Content, text.value)
  clearTimeout(timer)
  timer = setTimeout(() => void runChecks(), CHECK_DELAY_MS)
})

// Fixes from the explanation change the text; saving still shows the diff.
function fix(e: EditView) {
  text.value = applyEdit(text.value, e)
  void nextTick(() => editor.value?.goToLine(e.Insert ? e.Line + 1 : e.Line))
}
function add(after: number, lines: string[]) {
  text.value = addLines(text.value, after, lines)
  void nextTick(() => editor.value?.goToLine(after + 1))
}

// Open on ?line=N once the editor is there, and when it changes.
watch(
  () => [props.line, editor.value] as const,
  ([line, ed]) => {
    if (line && ed) void nextTick(() => ed.goToLine(line))
  },
  { immediate: true },
)

// ---- save ----

async function startSave() {
  if (cantSave.value) return
  await runChecks()
  if (errorCount.value) {
    showToast(`${saveReason(explained.value) || fixToSave(errorCount.value)}: see To fix before saving.`, 'error')
    return
  }
  failure.value = null
  preview.value = true
}

function reportResult(title: string, res: ApplyResultView, toast: string) {
  result.value = { title, res }
  showToast(res.Errors?.length ? `${title}, but ${res.Errors.length} could not apply.` : toast, res.Errors?.length ? 'error' : 'info')
}

async function confirmSave() {
  const f = file.value
  if (!f) return
  preview.value = false
  busy.value = 'save'
  failure.value = null
  result.value = null
  try {
    // The preview asked: a production server doesn't ask again.
    const res = await withConsentOnce(() => ConfigService.Save(props.path, text.value, f.Hash))
    dropDraft(props.path)
    await load()
    reportResult('Saved and applied', res, `Saved and applied ${props.fileName}.`)
    void loadLoadedDiff()
  } catch (e) {
    await failed(e)
  } finally {
    busy.value = null
  }
}

/** A save, restore or apply that threw: say why, and find out what state the file is in. */
async function failed(e: unknown) {
  failure.value = saveErrorText(e, props.fileName)
  const kind = classifySaveError(e)
  const wasText = text.value
  if (kind === 'changed') {
    await refreshFromDisk()
    if (diskNewer.value) failure.value = null // its own banner says it, with the choices
  } else if (kind === 'readonly') {
    if (file.value) file.value = { ...file.value, Writable: false }
  } else if (kind === 'other' || kind === 'rejected') {
    // "Saved, but applying failed": the file did change, so take its new hash.
    // Refused by supervisord: Visor put the previous version back, so the
    // file differs from the edits, which stay a draft (unless putting it
    // back failed and the file still holds them).
    try {
      const f = await ConfigService.Read(props.path)
      if (f.Content === wasText && file.value && f.Hash !== file.value.Hash) {
        file.value = f
        dropDraft(props.path)
      }
    } catch {
      // The failure above stands.
    }
  }
}

// ---- restore ----

async function openRestore() {
  try {
    backups.value = (await ConfigService.Backups(props.path)) ?? []
    restoreOpen.value = true
  } catch (e) {
    showToast(describeError(e), 'error')
  }
}

async function confirmRestore(id: string) {
  const b = backups.value.find((x) => x.ID === id)
  restoreOpen.value = false
  busy.value = 'restore'
  failure.value = null
  result.value = null
  try {
    const res = await withConsentOnce(() => ConfigService.Restore(props.path, id)) // its dialog asked
    dropDraft(props.path)
    await load()
    const when = b ? historyTime(new Date(b.At).getTime(), Date.now()) : 'the backup'
    reportResult('Restored and applied', res, `Restored ${props.fileName} from ${when}.`)
    void loadLoadedDiff()
  } catch (e) {
    await failed(e)
  } finally {
    busy.value = null
  }
}

// ---- apply a file that was changed elsewhere ----

async function applyDisk() {
  busy.value = 'apply'
  failure.value = null
  result.value = null
  try {
    const res = await ConfigService.ApplyFile(props.path)
    reportResult('Applied', res, `Applied ${props.fileName}.`)
    void loadLoadedDiff()
  } catch (e) {
    failure.value = describeError(e)
  } finally {
    busy.value = null
  }
}

// ---- a file that changed under the editor ----

const diskDiff = computed(() => diffLines(diskNewer.value?.Content ?? '', text.value))

async function copyEdits(done = 'Copied your edits.') {
  try {
    await copyText(text.value)
    showToast(done)
  } catch (e) {
    showToast(describeError(e), 'error')
  }
}

function loadNewVersion() {
  const f = diskNewer.value
  if (!f) return
  compare.value = false
  dropDraft(props.path)
  file.value = f
  text.value = f.Content
  diskNewer.value = null
  failure.value = null
  void runChecks()
}

function keepMyEdits() {
  const f = diskNewer.value
  if (!f) return
  compare.value = false
  file.value = f // the next save is checked against this version, and its preview shows what it replaces
  keepDraft(props.path, f.Hash, f.Content, text.value)
  diskNewer.value = null
  failure.value = null
}

const resultLines = computed(() => {
  const r = result.value?.res
  if (!r) return []
  const out: string[] = []
  if (r.Added?.length) out.push(`Added: ${r.Added.join(', ')}`)
  if (r.Changed?.length) out.push(`Restarted with the new settings: ${r.Changed.join(', ')}`)
  if (r.Removed?.length) out.push(`Removed: ${r.Removed.join(', ')}`)
  if (!out.length) out.push('Nothing needed to change in supervisord, so no process restarted.')
  return out
})
const resultTitle = computed(() => (result.value?.res.Errors?.length ? `${result.value.title}, with problems` : result.value?.title ?? ''))
</script>

<template>
  <div class="editor-tab">
    <p v-if="loadError" class="alert" role="alert">
      Couldn’t read this file. {{ loadError }}
      <button type="button" class="btn" @click="load()">Try again</button>
    </p>
    <p v-else-if="!file" class="muted">Loading…</p>
    <div v-else class="grid">
      <div class="col">
        <div v-if="!writable && serverReadOnly" class="banner warn" role="status">
          <strong>This server is set to Read only.</strong>
          <span>Visor shows its configs but changes nothing. Turn off Read only in the server’s settings to edit them.</span>
        </div>
        <div v-else-if="!writable && remote" class="banner warn" role="status">
          <strong>Visor can’t write this file on the server.</strong>
          <span>
            The SSH user can’t change it or its folder, so the editor is read-only.
            <RouterLink :to="tabHref('/report')">The server report</RouterLink> says how to allow it: turn on Use sudo, or let the user write the folder.
          </span>
        </div>
        <div v-else-if="!writable" class="banner warn" role="status">
          <strong>Visor can’t write this file.</strong>
          <span>
            The file or its folder is read-only for you, so the editor is read-only too. Open Doctor to see how to fix the permissions, or edit the file with
            sudo in your own editor.
          </span>
          <RouterLink class="btn" to="/doctor">Open Doctor</RouterLink>
        </div>

        <div v-if="diskNewer" class="banner warn" role="alert">
          <strong>{{ fileName }} changed on disk after you opened it.</strong>
          <span>Your edits are still here and nothing was saved over the other change.</span>
          <span class="actions">
            <button type="button" class="btn" @click="compare = true">Review the differences</button>
            <button type="button" class="btn" @click="copyEdits()">Copy my edits</button>
          </span>
        </div>

        <div v-if="failure" class="banner bad" role="alert">
          <span>{{ failure }}</span>
          <button v-if="!diskNewer" type="button" class="btn" @click="copyEdits()">Copy my edits</button>
        </div>

        <p v-if="restoredDraft" class="note" role="status">Your unsaved edits from earlier are back in the editor.</p>

        <IniEditor
          ref="editor"
          v-model="text"
          :read-only="!writable || !!busy"
          :marks="marks"
          :label="`Editor for ${fileName}`"
        />
      </div>

      <div class="col">
        <section v-if="result" class="card" aria-labelledby="result-title">
          <div class="card-head">
            <strong id="result-title">{{ resultTitle }}</strong>
            <button type="button" class="link" @click="result = null">Dismiss</button>
          </div>
          <ul class="plain">
            <li v-for="l in resultLines" :key="l">{{ l }}</li>
          </ul>
          <ul v-if="result.res.Errors?.length" class="plain errs">
            <li v-for="e in result.res.Errors" :key="e">{{ e }}</li>
          </ul>
          <p v-if="result.res.Errors?.length" class="muted">
            The file is saved. Fix the problem above and save again, or restore the backup.
          </p>
        </section>

        <section v-if="changed || uncertain" class="card" aria-labelledby="loaded-title">
          <strong id="loaded-title">Changed on disk since supervisord last loaded it</strong>
          <p v-if="loadedDiffError" class="muted">{{ loadedDiffError }}</p>
          <template v-else-if="loadedDiff">
            <p class="muted" role="status" data-testid="loaded-status">{{ loadedStatus }}</p>
            <p v-if="!loadedDiff.Known" class="muted">Visor has no earlier copy of this file to compare with.</p>
            <DiffView
              v-else
              :from="loadedDiff.Loaded"
              :to="loadedDiff.Disk"
              label="Disk compared with Visor’s last record of what supervisord loaded"
              empty="Only comments or spacing changed. Applying makes Visor record it as loaded."
            />
          </template>
          <div>
            <button v-if="!serverReadOnly" type="button" class="btn primary" :disabled="!!busy || dirty" :title="dirty ? 'Save or discard your edits first' : undefined" @click="applyDisk">
              {{ busy === 'apply' ? 'Applying…' : 'Apply changes' }}
            </button>
          </div>
        </section>

        <ExplainPanel
          :view="explained"
          :failed="checkFailed"
          :processes="processes ?? []"
          mode="editor"
          fix-list
          :read-only="!writable || !!busy || checkedText !== text"
          @goto="(n: number) => editor?.goToLine(n)"
          @edit="fix"
          @add="add"
          @reveal="reveal"
          @open="openPath"
          @open-log="openLog"
        />
      </div>
    </div>

    <SaveBar v-if="file" :tone="bar.tone" :status="bar.status" note="A timestamped backup is saved before every write.">
      <button type="button" class="btn" @click="copyEdits(dirty ? 'Copied your edits.' : `Copied ${fileName}.`)">Copy</button>
      <button type="button" class="btn" :disabled="!!busy" @click="openRestore">Restore a backup…</button>
      <button type="button" class="btn" :disabled="!!busy" @click="runChecks">Validate</button>
      <button type="button" class="btn primary" :disabled="!!cantSave" @click="startSave">
        {{ busy === 'save' ? 'Saving…' : 'Save & apply' }}
      </button>
    </SaveBar>

    <DialogShell v-if="preview" :title="`Review the change${onProduction()}`" wide @close="preview = false">
      <p class="lead">
        <strong>{{ fileName }}</strong>: {{ saveStats.added }} {{ saveStats.added === 1 ? 'line' : 'lines' }} added, {{ saveStats.removed }}
        {{ saveStats.removed === 1 ? 'line' : 'lines' }} removed.
      </p>
      <DiffView :lines="saveDiff" label="Changes to be saved" />
      <p class="muted">
        Visor saves a backup first, then applies this file only. Programs whose settings changed restart; everything else keeps running.
      </p>
      <p v-if="groupNote" class="muted" data-testid="group-note">{{ groupNote }}</p>
      <template #actions>
        <button type="button" class="btn" @click="preview = false">Cancel</button>
        <button type="button" class="btn primary" data-autofocus @click="confirmSave">Save &amp; apply</button>
      </template>
    </DialogShell>

    <DialogShell v-if="compare && diskNewer" title="Your edits vs. the file on disk" wide @close="compare = false">
      <p class="lead">
        Lines marked <strong>−</strong> are in the file on disk now and would be lost if you keep your edits; lines marked <strong>+</strong> are yours.
      </p>
      <DiffView :lines="diskDiff" label="File on disk compared with your edits" empty="Your edits match the file on disk." />
      <template #actions>
        <button type="button" class="btn" @click="compare = false">Close</button>
        <button type="button" class="btn" @click="copyEdits()">Copy my edits</button>
        <button type="button" class="btn" @click="loadNewVersion">Load the new version</button>
        <button type="button" class="btn primary" @click="keepMyEdits">Keep my edits</button>
      </template>
    </DialogShell>

    <RestoreDialog
      v-if="restoreOpen"
      :file-name="fileName"
      :backups="backups"
      :current="file?.Content ?? null"
      :dirty="dirty"
      :busy="busy === 'restore'"
      :writable="writable"
      @restore="confirmRestore"
      @close="restoreOpen = false"
    />
  </div>
</template>

<style scoped>
.grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(380px, 1fr)); gap: 16px; align-items: start; }
.col { display: flex; flex-direction: column; gap: 10px; min-width: 0; }
.col + .col { gap: 16px; }
.editor-tab { display: flex; flex-direction: column; gap: 16px; }
.muted { margin: 0; color: var(--text-muted); }
.note { margin: 0; color: var(--text-muted); }
.lead { margin: 0; line-height: 1.45; }
.link { border: 0; background: transparent; color: var(--accent-text); font: inherit; cursor: pointer; padding: 0; }
.link:hover { text-decoration: underline; }
.card { border: 1px solid var(--line); border-radius: 10px; padding: 16px; display: flex; flex-direction: column; gap: 10px; min-width: 0; }
.card-head { display: flex; flex-wrap: wrap; justify-content: space-between; align-items: center; gap: 8px; }
.plain { margin: 0; padding-left: 18px; display: flex; flex-direction: column; gap: 4px; user-select: text; }
.errs { color: var(--failing-fg); }
.banner { display: flex; flex-wrap: wrap; align-items: center; gap: 8px 10px; padding: 10px 14px; border-radius: var(--radius-md); }
.banner.warn { background: var(--warning-bg); color: var(--warning-fg); }
.banner.bad { background: var(--failing-bg); color: var(--failing-fg); }
.banner span:not(.actions) { flex: 1 1 260px; user-select: text; }
.actions { display: flex; flex-wrap: wrap; gap: 8px; }
.alert { margin: 0; padding: 12px; border: 1px solid var(--failing-dot); border-radius: var(--radius-md); background: var(--failing-bg); color: var(--failing-fg); display: flex; flex-wrap: wrap; gap: 8px; align-items: center; }
</style>
