<script setup lang="ts">
// Add new, "Fill a form" (spec §7.4): start from a template (or a copy of a
// config), fill in the settings, and watch the file that will be written,
// with its problem lines marked. Each field shows the check that applies to
// it; "N things to check" lists them all, above one card per program
// that explains every line. The text is the same one "Write it" edits. Save sits in the sticky bar at the bottom.
import { computed, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { ConfigService } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import AddHeader from '../components/AddHeader.vue'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import CopyPicker from '../components/CopyPicker.vue'
import CreatedCard from '../components/CreatedCard.vue'
import ExplainPanel from '../components/ExplainPanel.vue'
import SaveBar from '../components/SaveBar.vue'
import { explainMarks } from '../lib/explain'
import { openLog, openPath, reveal } from '../lib/finder'
import { checkItems, formReason, saveReason, thingsToCheck, type CheckItem } from '../lib/fix-list'
import type { TemplateInfo } from '../lib/draft'
import { FORM_SECTIONS, fieldLabel, fieldNotes, nameProblem, type FormField } from '../lib/form-fields'
import { forgetCopy, resetNewConfig, useNewConfig } from '../lib/new-config'

const n = useNewConfig()
const { draft } = n

/** Why the form could not be filled from the text, or what it cannot show. */
const notice = ref<string | null>(null)
const picking = ref(false)

// Coming from "Write it": read the text into the fields (once the draft is ready).
watch(
  n.ready,
  async (ready) => {
    if (!ready) return
    const problem = await draft.toForm()
    if (problem) {
      notice.value = `Visor can’t read the text as a program (${problem}). The form shows what it had before; changing a field replaces the text.`
      return
    }
    const rendered = await ConfigService.RenderProgram(draft.state.name, draft.state.values)
    notice.value = significant(rendered) === significant(draft.state.content) ? null : 'The text has comments, other settings or more programs than the form shows. Changing a field here rewrites the text without them; “Write it” keeps them.'
  },
  { immediate: true },
)

/** The lines that mean something: no blanks, no comments. */
function significant(text: string): string {
  return text
    .split('\n')
    .map((l) => l.trim())
    .filter((l) => l && !l.startsWith(';') && !l.startsWith('#'))
    .join('\n')
}

/** A template waiting for a yes, because choosing it would overwrite edits. */
const pendingTemplate = ref<TemplateInfo | null>(null)
function applyTemplate(t: TemplateInfo) {
  forgetCopy()
  notice.value = null
  void draft.applyTemplate(t)
}
function chooseTemplate(t: TemplateInfo) {
  if (draft.isDirty()) pendingTemplate.value = t
  else applyTemplate(t)
}
function confirmTemplate() {
  const t = pendingTemplate.value
  pendingTemplate.value = null
  if (t) applyTemplate(t)
}

const notes = computed(() => (n.upToDate.value ? fieldNotes(n.checks.value ?? []) : {}))

function valueOf(key: string): string {
  return key === 'name' ? draft.state.name : (draft.state.values[key] ?? '')
}
function optionsOf(f: FormField): string[] {
  const v = valueOf(f.key)
  return v && f.options && !f.options.includes(v) ? [...f.options, v] : (f.options ?? [])
}
function noteOf(f: FormField): { level: string; text: string } {
  if (f.key === 'name') {
    const p = nameProblem(draft.state.name)
    if (p) return { level: 'error', text: p }
  }
  const note = notes.value[f.key]
  return note ?? { level: 'help', text: f.help }
}
function onInput(f: FormField, e: Event) {
  void draft.setField(f.key, (e.target as HTMLInputElement | HTMLSelectElement).value)
}

// The file preview, one row per line, with the worst problem of each line.
const previewLines = computed(() => {
  const marks = new Map((n.upToDate.value ? explainMarks(n.explained.value) : []).map((m) => [m.line, m.level]))
  return draft.state.content.replace(/\n$/, '').split('\n').map((text, i) => ({ text, level: marks.get(i + 1) ?? '' }))
})

// "N things to check": every error and warning, under the field it is about.
const items = computed<CheckItem[]>(() => (n.upToDate.value ? checkItems(n.checks.value ?? [], fieldLabel) : []))
function focusField(key: string) {
  document.getElementById(`f-${key}`)?.focus()
}

const nameBad = computed(() => nameProblem(draft.state.name) !== null)
const errorTotal = computed(() => (n.upToDate.value ? n.errorCount.value : 0) + (nameBad.value && !notes.value.name ? 1 : 0))
const bar = computed<{ tone: 'ok' | 'bad' | 'warn' | 'idle'; status: string }>(() => {
  if (n.busy.value) return { tone: 'idle', status: 'Saving…' }
  if (n.created.value) return n.created.value.res.Errors?.length ? { tone: 'warn', status: 'Saved, with problems.' } : { tone: 'ok', status: 'Saved and applied.' }
  if (nameBad.value) return { tone: 'bad', status: 'Give the program a name to save.' }
  if (!n.upToDate.value) return { tone: 'idle', status: 'Checking…' }
  if (n.hasErrors.value) return { tone: 'bad', status: `${formReason(items.value) || saveReason(n.explained.value) || 'Fix the errors in the form to save'}.` }
  return { tone: 'ok', status: 'Ready to save.' }
})
const cantSave = computed(() => n.busy.value || nameBad.value || n.hasErrors.value)

const ICON = { error: 'M18 6 6 18M6 6l12 12', warning: 'M12 3 2 20h20L12 3zM12 10v4M12 17v.01', ok: 'm5 12 5 5 9-10' }
const iconOf = (level: string) => ICON[level as keyof typeof ICON] ?? ''
</script>

<template>
  <div class="page">
    <AddHeader mode="form" />
    <p v-if="n.loadError.value" class="alert" role="alert">
      Couldn’t get ready. {{ n.loadError.value }}
      <button type="button" class="btn" @click="n.retry">Try again</button>
    </p>
    <p v-else-if="!n.ready.value" class="muted">Loading…</p>
    <template v-else>
      <div class="templates">
        <span id="tpl-title" class="sec">Start from a template</span>
        <div class="chips">
          <div class="chips" role="group" aria-label="Templates">
            <button
              v-for="t in n.templates.value"
              :key="t.ID"
              type="button"
              class="btn chip"
              :class="{ on: draft.state.templateId === t.ID }"
              :aria-pressed="draft.state.templateId === t.ID"
              @click="chooseTemplate(t)"
            >
              <svg v-if="draft.state.templateId === t.ID" class="check" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m5 12 5 5 9-10" /></svg>
              {{ t.Label }}
            </button>
          </div>
          <button type="button" class="btn quiet chip" @click="picking = true">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="9" y="9" width="11" height="11" rx="2" /><path d="M5 15V6a2 2 0 0 1 2-2h8" /></svg>
            Copy an existing config…
          </button>
        </div>
        <p v-if="draft.state.templateId" class="meta">Paths under <span class="mono">/srv/acme</span> and the names are placeholders: change them to match your project.</p>
      </div>

      <p v-if="notice" class="banner" role="status">{{ notice }}</p>

      <div class="grid">
        <div class="col">
          <fieldset v-for="(s, si) in FORM_SECTIONS" :key="s.title" class="card set" :aria-labelledby="`set-${si}`">
            <h3 :id="`set-${si}`" class="set-title">{{ s.title }}</h3>
            <div v-for="f in s.fields" :key="f.key" class="field" :class="{ wide: f.wide }">
              <label :for="`f-${f.key}`" class="flabel">
                {{ f.label }} <span class="key">{{ f.key === 'name' ? '[program:…]' : f.key }}</span>
              </label>
              <select v-if="f.options" :id="`f-${f.key}`" class="input" :value="valueOf(f.key)" :aria-describedby="`m-${f.key}`" @change="onInput(f, $event)">
                <option v-for="o in optionsOf(f)" :key="o" :value="o">{{ o }}</option>
              </select>
              <input
                v-else
                :id="`f-${f.key}`"
                class="input"
                :class="{ mono: f.mono, err: noteOf(f).level === 'error', warn: noteOf(f).level === 'warning' }"
                :value="valueOf(f.key)"
                :aria-invalid="noteOf(f).level === 'error' || undefined"
                :aria-describedby="`m-${f.key}`"
                spellcheck="false"
                autocomplete="off"
                @input="onInput(f, $event)"
              />
              <span :id="`m-${f.key}`" class="msg" :class="noteOf(f).level">
                <svg v-if="iconOf(noteOf(f).level)" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path :d="iconOf(noteOf(f).level)" /></svg>
                <span>{{ noteOf(f).text }}</span>
              </span>
            </div>
          </fieldset>
        </div>

        <aside class="col">
          <section class="card box" aria-labelledby="file-title">
            <div class="head">
              <h2 id="file-title">File that will be written</h2>
              <span v-if="n.upToDate.value && errorTotal" class="badge b-bad">{{ errorTotal }} to fix</span>
            </div>
            <div class="meta mono path">{{ n.dir.value }}/{{ draft.state.fileName }}</div>
            <div class="ini" role="group" aria-label="File contents">
              <div v-for="(l, i) in previewLines" :key="i" :class="['pline', l.level]">
                <span class="pmark">
                  <svg v-if="l.level" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path :d="iconOf(l.level)" /></svg>
                  <span v-if="l.level" class="sr">{{ l.level === 'error' ? 'Error on this line: ' : 'Warning on this line: ' }}</span>
                </span>
                <span class="ptext">{{ l.text }}</span>
              </div>
            </div>
            <span class="meta"><RouterLink to="/new">Switch to writing it by hand</RouterLink> (keeps what you entered)</span>
          </section>

          <ExplainPanel
            :view="n.explained.value"
            :failed="n.checkFailed.value"
            mode="editor"
            :actions="false"
            @reveal="reveal"
            @open="openPath"
            @open-log="openLog"
          >
            <div v-if="items.length" class="checks" role="group" aria-labelledby="checks-title" data-testid="things-to-check">
              <span id="checks-title" class="th">{{ thingsToCheck(items.length) }}</span>
              <ul>
                <li v-for="(c, i) in items" :key="i" :class="c.level === 'error' ? 't-bad' : 't-warn'">
                  <component :is="c.key ? 'button' : 'span'" :type="c.key ? 'button' : undefined" class="check-item" @click="c.key && focusField(c.key)">
                    <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path :d="iconOf(c.level)" /></svg>
                    <span><span class="sr">{{ c.level === 'error' ? 'Error' : 'Warning' }}: </span><strong>{{ c.label }}</strong> · {{ c.text }}</span>
                  </component>
                </li>
              </ul>
            </div>
          </ExplainPanel>
        </aside>
      </div>

      <div v-if="n.failure.value || n.created.value" class="results">
        <p v-if="n.failure.value" class="alert" role="alert">{{ n.failure.value }}</p>
        <CreatedCard v-if="n.created.value" v-bind="{ path: n.created.value.path, fileName: n.created.value.fileName, result: n.created.value.res }" @open="resetNewConfig" />
      </div>
      <SaveBar :tone="bar.tone" :status="bar.status" note="Saving writes the file, asks supervisord to re-read its configs, and adds the new program.">
        <RouterLink class="btn" to="/" @click="resetNewConfig">Cancel</RouterLink>
        <button type="button" class="btn primary" :disabled="cantSave" @click="n.save">
          {{ n.busy.value ? 'Saving…' : 'Save & apply' }}
        </button>
      </SaveBar>
    </template>
    <CopyPicker v-if="picking" @close="picking = false" />
    <ConfirmDialog
      v-if="pendingTemplate"
      title="Replace what you entered?"
      :message="`The “${pendingTemplate.Label}” template replaces the text and settings you changed.`"
      confirm-label="Replace"
      @confirm="confirmTemplate"
      @cancel="pendingTemplate = null"
    />
  </div>
</template>

<style scoped>
.page { display: flex; flex-direction: column; gap: 18px; min-height: 100%; }
.templates { display: flex; flex-direction: column; gap: 8px; }
.sec { font-size: 13.5px; font-weight: 600; }
.chips { display: flex; flex-wrap: wrap; gap: 8px; }
.chip.on { border-color: var(--accent); background: var(--accent-soft); color: var(--accent-text); font-weight: 600; }
.chip.on:hover { background: var(--accent-soft); }
.grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(420px, 1fr)); gap: 20px; align-items: start; }
.col { display: flex; flex-direction: column; gap: 14px; min-width: 0; }
.card { border: 1px solid var(--line); border-radius: 10px; background: var(--raised); min-width: 0; }

/* A fieldset on a two-column grid, its title inside the card (a heading
   that names the fieldset: WebKit draws a <legend> on the border, even
   floated). Each field takes three rows of the grid (label, control,
   message) as a subgrid, so a label that wraps pushes its whole row down,
   not just its own box. */
.set { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); column-gap: 16px; row-gap: 0; margin: 0; padding: 16px 18px 6px; }
.set-title { grid-column: 1 / -1; margin: 0 0 12px; font-size: 15px; font-weight: 600; }
.field { display: grid; grid-row: span 3; grid-template-rows: subgrid; min-width: 0; }
.field.wide { grid-column: 1 / -1; }
.flabel { display: flex; flex-wrap: wrap; align-items: baseline; gap: 2px 6px; margin-bottom: 5px; font-weight: 600; }
.key { font-family: var(--mono); font-size: 11.5px; font-weight: 400; color: var(--ink3); }
.field .input { width: 100%; }
.input.mono { font-family: var(--mono); font-size: 12px; }
.input.err { border-color: var(--bad-dot); }
.input.warn { border-color: var(--warn-dot); }
.msg { display: flex; align-items: baseline; gap: 5px; min-height: 1em; padding: 5px 0 12px; font-size: 12px; overflow-wrap: anywhere; }
.msg svg { flex: none; position: relative; top: 1px; }
.msg.ok { color: var(--ok); }
.msg.warning { color: var(--warn); }
.msg.error { color: var(--bad); }
.msg.help { color: var(--ink3); }

.box { display: flex; flex-direction: column; gap: 10px; padding: 14px 16px; }
.head { display: flex; justify-content: space-between; align-items: center; gap: 8px; }
h2 { margin: 0; font-size: 15px; font-weight: 600; }
.mono { font-family: var(--mono); font-size: 12px; }
.path { overflow-wrap: anywhere; user-select: text; }
.muted { color: var(--ink2); margin: 0; }
.meta { margin: 0; color: var(--ink3); font-size: 12.5px; }
.th { font-size: 12px; font-weight: 600; color: var(--ink3); }
.ini { padding: 8px 0; border: 1px solid var(--line); border-radius: var(--radius-sm); background: var(--sunken); font-family: var(--mono); font-size: 12px; line-height: 21px; user-select: text; }
.pline { display: grid; grid-template-columns: 22px minmax(0, 1fr); }
.pline.error { background: var(--bad-soft); }
.pline.warning { background: var(--warn-soft); }
.pmark { display: flex; justify-content: center; padding-top: 4px; }
.pline.error .pmark { color: var(--bad); }
.pline.warning .pmark { color: var(--warn); }
.ptext { padding-right: 10px; white-space: pre-wrap; overflow-wrap: anywhere; }
.checks { display: flex; flex-direction: column; gap: 6px; }
.checks ul { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 4px; }
.check-item { display: flex; align-items: baseline; gap: 8px; padding: 2px 0; border: 0; background: none; color: inherit; font: inherit; text-align: left; user-select: text; }
.check-item svg { flex: none; position: relative; top: 1px; }
button.check-item { cursor: pointer; }
button.check-item:hover strong { text-decoration: underline; }
.sr { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0 0 0 0); white-space: nowrap; }
a:not(.btn) { color: var(--accent-text); }
.banner { margin: 0; padding: 10px 14px; border-radius: var(--radius-md); background: var(--warn-soft); color: var(--warn); }
.results { display: flex; flex-direction: column; gap: 12px; }
.alert { margin: 0; padding: 12px; border: 1px solid var(--bad-dot); border-radius: var(--radius-md); background: var(--bad-soft); color: var(--bad); display: flex; flex-wrap: wrap; gap: 8px; align-items: center; user-select: text; }
</style>
