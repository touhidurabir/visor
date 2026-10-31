<script setup lang="ts">
// Add new, "Write it" (spec §7.4): a file name and a text editor, with the
// checks and the plain-English description following as you type.
import { computed, ref } from 'vue'
import { RouterLink } from 'vue-router'
import AddHeader from '../components/AddHeader.vue'
import ChecksCard from '../components/ChecksCard.vue'
import CreatedCard from '../components/CreatedCard.vue'
import DescribeCard from '../components/DescribeCard.vue'
import IniEditor, { type LineMark } from '../components/IniEditor.vue'
import { resetNewConfig, useNewConfig } from '../lib/new-config'

const n = useNewConfig()
const { draft } = n
const editor = ref<InstanceType<typeof IniEditor>>()

const marks = computed<LineMark[]>(() =>
  n.upToDate.value ? (n.checks.value ?? []).filter((c) => c.Level !== 'ok' && c.Line > 0).map((c) => ({ line: c.Line, level: c.Level as 'error' | 'warning' })) : [],
)
const cantSave = computed(() => {
  if (n.busy.value) return 'Wait for the current action to finish.'
  if (!draft.state.fileName.trim()) return 'Give the file a name first.'
  if (n.hasErrors.value) return 'Fix the errors in the checks first.'
  return ''
})
</script>

<template>
  <div class="page">
    <AddHeader mode="raw" />
    <p v-if="n.loadError.value" class="alert" role="alert">
      Couldn’t get ready. {{ n.loadError.value }}
      <button type="button" class="btn" @click="n.retry">Try again</button>
    </p>
    <p v-else-if="!n.ready.value" class="muted">Loading…</p>
    <div v-else class="grid">
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
        <div class="muted">Try fixing the problems on the right: checks re-run as you type.</div>
      </div>

      <div class="col">
        <ChecksCard :checks="n.checks.value" :failed="n.checkFailed.value" @goto="(l: number) => editor?.goToLine(l)" />
        <DescribeCard :content="n.checked.value?.content ?? null" />
        <p v-if="n.failure.value" class="alert" role="alert">{{ n.failure.value }}</p>
        <CreatedCard v-if="n.created.value" v-bind="{ path: n.created.value.path, fileName: n.created.value.fileName, result: n.created.value.res }" @open="resetNewConfig" />
        <div class="buttons">
          <RouterLink class="btn" to="/" @click="resetNewConfig">Cancel</RouterLink>
          <button type="button" class="btn primary" :disabled="!!cantSave" :title="cantSave || undefined" @click="n.save">
            {{ n.busy.value ? 'Saving…' : 'Save & apply' }}
          </button>
        </div>
        <div class="muted right">Saving writes the file, asks supervisord to re-read its configs, and adds the new program.</div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.page { display: flex; flex-direction: column; gap: 18px; }
.grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(400px, 1fr)); gap: 18px; align-items: start; }
.col { display: flex; flex-direction: column; gap: 16px; min-width: 0; }
.col:first-child { gap: 10px; }
.name-row { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
.strong { font-weight: 600; }
.muted { color: var(--text-muted); }
.right { text-align: right; }
.mono { font-family: var(--font-mono); font-size: 12px; }
.inp { min-height: 32px; width: 200px; padding: 0 10px; border: 1px solid var(--line-strong); border-radius: var(--radius-sm); background: var(--bg); color: var(--text); box-sizing: border-box; }
.inp:focus-visible { outline: 2px solid var(--focus); outline-offset: 1px; }
.buttons { display: flex; flex-wrap: wrap; gap: 8px; justify-content: flex-end; }
.btn { display: inline-flex; align-items: center; gap: 6px; min-height: 32px; padding: 0 12px; border-radius: var(--radius-sm); border: 1px solid var(--line-strong); background: var(--bg); color: var(--text); font: inherit; text-decoration: none; white-space: nowrap; cursor: pointer; box-sizing: border-box; }
.btn:hover:not(:disabled) { background: var(--bg-hover); }
.btn:disabled { color: var(--text-faint); cursor: default; }
.btn.primary { background: var(--accent); border-color: var(--accent); color: var(--on-accent); font-weight: 600; }
.btn.primary:hover:not(:disabled) { background: var(--accent-hover); border-color: var(--accent-hover); }
.btn.primary:disabled { opacity: 0.55; color: var(--on-accent); }
.alert { margin: 0; padding: 12px; border: 1px solid var(--failing-dot); border-radius: var(--radius-md); background: var(--failing-bg); color: var(--failing-fg); display: flex; flex-wrap: wrap; gap: 8px; align-items: center; user-select: text; }
</style>
