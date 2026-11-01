<script setup lang="ts">
// Add new, "Fill a form" (spec §7.4): start from a template, fill in the
// settings, and watch the file that will be written. Each field shows the
// check that applies to it. The text is the same one "Write it" edits.
import { computed, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { ConfigService } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import AddHeader from '../components/AddHeader.vue'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import CreatedCard from '../components/CreatedCard.vue'
import DescribeCard from '../components/DescribeCard.vue'
import type { TemplateInfo } from '../lib/draft'
import { FORM_SECTIONS, fieldNotes, fileChecks, nameProblem, type FormField } from '../lib/form-fields'
import { resetNewConfig, useNewConfig } from '../lib/new-config'

const n = useNewConfig()
const { draft } = n

/** Why the form could not be filled from the text, or what it cannot show. */
const notice = ref<string | null>(null)

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
function chooseTemplate(t: TemplateInfo) {
  if (draft.isDirty()) pendingTemplate.value = t
  else void draft.applyTemplate(t)
}
function confirmTemplate() {
  const t = pendingTemplate.value
  pendingTemplate.value = null
  if (t) void draft.applyTemplate(t)
}

const notes = computed(() => (n.upToDate.value ? fieldNotes(n.checks.value ?? []) : {}))
const fieldKeys = new Set(FORM_SECTIONS.flatMap((s) => s.fields.map((f) => f.key)))
/** Findings the form has no field for (an error on a setting only "Write it" can set, or on the file). */
const otherProblems = computed(() => {
  if (!n.upToDate.value) return []
  return (n.checks.value ?? []).filter((c) => c.Level === 'error' && (!c.Program && !c.Key ? true : c.Key !== '' && !fieldKeys.has(c.Key)))
})
const otherFileOnly = computed(() => fileChecks(n.checks.value ?? []).length)

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

const nameBad = computed(() => nameProblem(draft.state.name) !== null)
const errorTotal = computed(() => (n.upToDate.value ? n.errorCount.value : 0) + (nameBad.value && !notes.value.name ? 1 : 0))
const status = computed(() => {
  if (!n.upToDate.value) return 'Checking…'
  if (errorTotal.value) return `${errorTotal.value} to fix`
  return 'Ready to save'
})
const cantSave = computed(() => {
  if (n.busy.value) return 'Wait for the current action to finish.'
  if (nameBad.value) return 'Give the program a name first.'
  if (n.hasErrors.value) return 'Fix the errors in the form first.'
  return ''
})
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
        <span class="label">Start from a template</span>
        <div class="chips" role="group" aria-label="Templates">
          <button
            v-for="t in n.templates.value"
            :key="t.ID"
            type="button"
            class="chip"
            :class="{ on: draft.state.templateId === t.ID }"
            :aria-pressed="draft.state.templateId === t.ID"
            @click="chooseTemplate(t)"
          >
            {{ t.Label }}
          </button>
        </div>
        <p v-if="draft.state.templateId" class="muted small">Paths under /srv/acme and the names are placeholders: change them to match your project.</p>
      </div>

      <p v-if="notice" class="banner" role="status">{{ notice }}</p>

      <div class="grid">
        <div class="col">
          <fieldset v-for="s in FORM_SECTIONS" :key="s.title" class="card">
            <legend>{{ s.title }}</legend>
            <div class="fields">
              <div v-for="f in s.fields" :key="f.key" class="field" :class="{ wide: f.wide }">
                <label :for="`f-${f.key}`" class="flabel">
                  {{ f.label }} <span class="mono key">{{ f.key === 'name' ? '[program:…]' : f.key }}</span>
                </label>
                <select v-if="f.options" :id="`f-${f.key}`" class="inp" :value="valueOf(f.key)" :aria-describedby="`m-${f.key}`" @change="onInput(f, $event)">
                  <option v-for="o in optionsOf(f)" :key="o" :value="o">{{ o }}</option>
                </select>
                <input
                  v-else
                  :id="`f-${f.key}`"
                  class="inp"
                  :class="{ mono: f.mono, err: noteOf(f).level === 'error' }"
                  :value="valueOf(f.key)"
                  :aria-invalid="noteOf(f).level === 'error' || undefined"
                  :aria-describedby="`m-${f.key}`"
                  spellcheck="false"
                  autocomplete="off"
                  @input="onInput(f, $event)"
                />
                <span :id="`m-${f.key}`" class="msg" :class="noteOf(f).level">{{ noteOf(f).text }}</span>
              </div>
            </div>
          </fieldset>
        </div>

        <div class="col">
          <section class="card" aria-labelledby="file-title">
            <div class="head">
              <strong id="file-title">File that will be written</strong>
              <span class="muted" role="status">{{ status }}</span>
            </div>
            <div class="mono muted path">{{ n.dir.value }}/{{ draft.state.fileName }}</div>
            <pre class="mono ini" aria-label="File contents">{{ draft.state.content }}</pre>
            <ul v-if="otherProblems.length" class="problems">
              <li v-for="(c, i) in otherProblems" :key="i">{{ c.Line > 0 ? `line ${c.Line}: ` : '' }}{{ c.Message }}</li>
            </ul>
            <p v-if="otherFileOnly && !otherProblems.length" class="muted small">Some notes are about the file itself; “Write it” shows all checks.</p>
            <div><RouterLink to="/new">Switch to writing it by hand</RouterLink> (keeps what you entered)</div>
          </section>

          <DescribeCard :content="n.checked.value?.content ?? null" />

          <p v-if="n.failure.value" class="alert" role="alert">{{ n.failure.value }}</p>
          <CreatedCard v-if="n.created.value" v-bind="{ path: n.created.value.path, fileName: n.created.value.fileName, result: n.created.value.res }" @open="resetNewConfig" />

          <div class="buttons">
            <RouterLink class="btn" to="/" @click="resetNewConfig">Cancel</RouterLink>
            <button type="button" class="btn primary" :disabled="!!cantSave" :title="cantSave || undefined" @click="n.save">
              {{ n.busy.value ? 'Saving…' : 'Save & apply' }}
            </button>
          </div>
        </div>
      </div>
    </template>
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
.page { display: flex; flex-direction: column; gap: 18px; }
.templates { display: flex; flex-direction: column; gap: 8px; }
.label { font-size: 11px; font-weight: 600; letter-spacing: 0.04em; text-transform: uppercase; color: var(--text-muted); }
.chips { display: flex; flex-wrap: wrap; gap: 8px; }
.chip { min-height: 32px; padding: 0 12px; border-radius: 999px; border: 1px solid var(--line-strong); background: var(--bg); font: inherit; color: var(--text); cursor: pointer; }
.chip:hover { background: var(--bg-hover); }
.chip.on { border-color: var(--accent); background: var(--selected-bg); color: var(--watch-on-fg); font-weight: 600; }
.grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(420px, 1fr)); gap: 18px; align-items: start; }
.col { display: flex; flex-direction: column; gap: 16px; min-width: 0; }
.card { border: 1px solid var(--line); border-radius: 10px; padding: 16px; display: flex; flex-direction: column; gap: 12px; margin: 0; min-width: 0; }
legend { padding: 0 6px; font-weight: 650; }
.fields { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 12px; }
.field { display: flex; flex-direction: column; gap: 4px; min-width: 0; }
.field.wide { grid-column: 1 / -1; }
.flabel { font-weight: 600; }
.key { font-weight: 400; color: var(--text-faint); }
.inp { min-height: 32px; padding: 0 10px; border: 1px solid var(--line-strong); border-radius: var(--radius-sm); font: inherit; background: var(--bg); color: var(--text); box-sizing: border-box; width: 100%; }
.inp.mono { font-family: var(--font-mono); font-size: 12px; }
.inp.err { border-color: var(--failing-dot); }
.inp:focus-visible { outline: 2px solid var(--focus); outline-offset: 1px; }
.msg { font-size: 12px; min-height: 1em; overflow-wrap: anywhere; }
.msg.ok { color: var(--running-fg); }
.msg.warning { color: var(--warning-fg); }
.msg.error { color: var(--failing-fg); }
.msg.help { color: var(--text-muted); }
.head { display: flex; justify-content: space-between; align-items: center; gap: 8px; }
.mono { font-family: var(--font-mono); font-size: 12px; }
.path { overflow-wrap: anywhere; }
.muted { color: var(--text-muted); margin: 0; }
.small { font-size: 12px; }
.ini { margin: 0; padding: 12px; background: var(--bg-subtle); border: 1px solid var(--line); border-radius: var(--radius-sm); line-height: 1.7; white-space: pre-wrap; overflow-wrap: anywhere; user-select: text; }
.problems { margin: 0; padding-left: 18px; color: var(--failing-fg); }
a { color: var(--accent-text); }
.banner { margin: 0; padding: 10px 14px; border-radius: var(--radius-md); background: var(--warning-bg); color: var(--warning-fg); }
.buttons { display: flex; flex-wrap: wrap; gap: 8px; justify-content: flex-end; }
.btn { display: inline-flex; align-items: center; gap: 6px; min-height: 32px; padding: 0 12px; border-radius: var(--radius-sm); border: 1px solid var(--line-strong); background: var(--bg); color: var(--text); font: inherit; text-decoration: none; white-space: nowrap; cursor: pointer; box-sizing: border-box; }
.btn:hover:not(:disabled) { background: var(--bg-hover); }
.btn:disabled { color: var(--text-faint); cursor: default; }
.btn.primary { background: var(--accent); border-color: var(--accent); color: var(--on-accent); font-weight: 600; }
.btn.primary:hover:not(:disabled) { background: var(--accent-hover); border-color: var(--accent-hover); }
.btn.primary:disabled { opacity: 0.55; color: var(--on-accent); }
.alert { margin: 0; padding: 12px; border: 1px solid var(--failing-dot); border-radius: var(--radius-md); background: var(--failing-bg); color: var(--failing-fg); display: flex; flex-wrap: wrap; gap: 8px; align-items: center; user-select: text; }
</style>
