<script setup lang="ts">
// A CodeMirror 6 editor for supervisord INI files (the "properties" mode:
// [sections], key=value, ; comments). Colours come from the app's tokens as
// CSS variables, so the editor follows the light/dark theme live with no
// reconfiguration. Lines named in `marks` get a red or amber wash.
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Compartment, EditorState, StateEffect, StateField, type Extension } from '@codemirror/state'
import { minimalSetup } from 'codemirror'
import { Decoration, EditorView, lineNumbers, highlightActiveLine, type DecorationSet } from '@codemirror/view'
import { HighlightStyle, StreamLanguage, syntaxHighlighting } from '@codemirror/language'
import { properties } from '@codemirror/legacy-modes/mode/properties'
import { tags } from '@lezer/highlight'

export interface LineMark {
  line: number
  level: 'error' | 'warning'
}

const props = defineProps<{ modelValue: string; readOnly?: boolean; marks?: LineMark[]; label: string }>()
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()

const host = ref<HTMLDivElement>()
let view: EditorView | null = null
const editable = new Compartment()

const setMarks = StateEffect.define<LineMark[]>()
const markField = StateField.define<DecorationSet>({
  create: () => Decoration.none,
  update(deco, tr) {
    deco = deco.map(tr.changes)
    for (const e of tr.effects) {
      if (!e.is(setMarks)) continue
      const doc = tr.state.doc
      const ranges = e.value
        .filter((m) => m.line >= 1 && m.line <= doc.lines)
        .sort((a, b) => a.line - b.line)
        .map((m) => Decoration.line({ class: `cm-mark-${m.level}` }).range(doc.line(m.line).from))
      deco = Decoration.set(ranges, true)
    }
    return deco
  },
  provide: (f) => EditorView.decorations.from(f),
})

const highlight = HighlightStyle.define([
  { tag: tags.heading, color: 'var(--warning-fg)', fontWeight: '600' },
  { tag: tags.definition(tags.variableName), color: 'var(--accent-text)' },
  { tag: tags.string, color: 'var(--text)' },
  { tag: tags.comment, color: 'var(--text-muted)', fontStyle: 'italic' },
])

const theme = EditorView.theme({
  '&': { color: 'var(--text)', backgroundColor: 'var(--bg)', fontSize: '12px', maxHeight: '60vh', minHeight: '240px' },
  '&.cm-focused': { outline: '2px solid var(--focus)', outlineOffset: '1px' },
  '.cm-scroller': { fontFamily: 'var(--font-mono)', lineHeight: '1.7', overflow: 'auto' },
  '.cm-content': { caretColor: 'var(--text)', padding: '10px 0' },
  '.cm-cursor, .cm-dropCursor': { borderLeftColor: 'var(--text)' },
  '.cm-gutters': { backgroundColor: 'var(--bg)', color: 'var(--text-faint)', border: 'none', paddingLeft: '4px' },
  '.cm-lineNumbers .cm-gutterElement': { padding: '0 10px 0 6px', minWidth: '28px' },
  '.cm-activeLine': { backgroundColor: 'var(--bg-subtle)' },
  '.cm-activeLineGutter': { backgroundColor: 'transparent', color: 'var(--text)' },
  '&.cm-focused .cm-selectionBackground, .cm-selectionBackground, ::selection': { backgroundColor: 'var(--menu-cmd-hover)' },
  '.cm-mark-error': { backgroundColor: 'var(--failing-bg)' },
  '.cm-mark-warning': { backgroundColor: 'var(--warning-bg)' },
})

function extensions(): Extension[] {
  return [
    minimalSetup,
    lineNumbers(),
    highlightActiveLine(),
    StreamLanguage.define(properties),
    syntaxHighlighting(highlight),
    EditorView.lineWrapping,
    EditorView.contentAttributes.of({ 'aria-label': props.label, 'aria-multiline': 'true' }),
    markField,
    theme,
    editable.of([EditorView.editable.of(!props.readOnly), EditorState.readOnly.of(!!props.readOnly)]),
    EditorView.updateListener.of((u) => {
      if (u.docChanged) emit('update:modelValue', u.state.doc.toString())
    }),
  ]
}

onMounted(() => {
  view = new EditorView({
    parent: host.value,
    state: EditorState.create({ doc: props.modelValue, extensions: extensions() }),
  })
  applyMarks()
})
onBeforeUnmount(() => {
  view?.destroy()
  view = null
})

watch(
  () => props.modelValue,
  (v) => {
    if (view && v !== view.state.doc.toString()) {
      view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: v } })
    }
  },
)
watch(
  () => props.readOnly,
  (ro) => view?.dispatch({ effects: editable.reconfigure([EditorView.editable.of(!ro), EditorState.readOnly.of(!!ro)]) }),
)
function applyMarks() {
  view?.dispatch({ effects: setMarks.of(props.marks ?? []) })
}
watch(() => props.marks, applyMarks, { deep: true })

/** Puts the cursor at the start of a 1-based line and scrolls it into view. */
function goToLine(n: number) {
  if (!view) return
  const doc = view.state.doc
  const line = doc.line(Math.min(Math.max(n, 1), doc.lines))
  view.dispatch({ selection: { anchor: line.from }, effects: EditorView.scrollIntoView(line.from, { y: 'center' }) })
  view.focus()
}
defineExpose({ goToLine })
</script>

<template>
  <div ref="host" class="editor" :class="{ readonly: readOnly }" data-testid="ini-editor" />
</template>

<style scoped>
.editor { border: 1px solid var(--line-strong); border-radius: var(--radius-md); overflow: hidden; background: var(--bg); min-width: 0; }
.editor.readonly { background: var(--bg-subtle); }
.editor.readonly :deep(.cm-editor), .editor.readonly :deep(.cm-gutters) { background: var(--bg-subtle); }
</style>
