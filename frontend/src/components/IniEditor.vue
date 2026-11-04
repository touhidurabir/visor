<script setup lang="ts">
// A CodeMirror 6 editor for supervisord INI files (the "properties" mode:
// [sections], key=value, ; comments). Colours come from the app's tokens as
// CSS variables, so the editor follows the light/dark theme live with no
// reconfiguration. Lines named in `marks` get a red or amber wash and an
// icon in the gutter, so the kind never rests on colour alone.
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Compartment, EditorState, RangeSet, StateEffect, StateField, type Extension } from '@codemirror/state'
import { minimalSetup } from 'codemirror'
import { Decoration, EditorView, GutterMarker, gutter, lineNumbers, highlightActiveLine, type DecorationSet } from '@codemirror/view'
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

// The gutter icon: an x for an error, a triangle for a warning, with the
// word for screen readers and as a tooltip.
const ICON_PATH = { error: 'M18 6 6 18M6 6l12 12', warning: 'M12 3 2 20h20L12 3zM12 10v4M12 17v.01' }
const ICON_WORD = { error: 'Error on this line', warning: 'Warning on this line' }
const SVG_NS = 'http://www.w3.org/2000/svg'
// The spacer (an invisible icon that keeps the gutter's width) carries no
// mark, word or tooltip.
class MarkIcon extends GutterMarker {
  constructor(
    readonly level: LineMark['level'],
    readonly spacer = false,
  ) {
    super()
  }
  eq(other: MarkIcon) {
    return other.level === this.level && other.spacer === this.spacer
  }
  toDOM() {
    const el = document.createElement('span')
    el.className = `cm-mark-icon ${this.level}`
    if (this.spacer) {
      const blank = document.createElementNS(SVG_NS, 'svg')
      blank.setAttribute('width', '13')
      blank.setAttribute('height', '13')
      el.append(blank)
      return el
    }
    el.dataset.mark = this.level
    el.title = ICON_WORD[this.level]
    const svg = document.createElementNS(SVG_NS, 'svg')
    for (const [k, v] of Object.entries({ width: '13', height: '13', viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', 'stroke-width': '2.4', 'stroke-linecap': 'round', 'stroke-linejoin': 'round', 'aria-hidden': 'true' }))
      svg.setAttribute(k, v)
    const path = document.createElementNS(SVG_NS, 'path')
    path.setAttribute('d', ICON_PATH[this.level])
    svg.append(path)
    const word = document.createElement('span')
    word.className = 'cm-mark-word'
    word.textContent = ICON_WORD[this.level]
    el.append(svg, word)
    return el
  }
}
const iconField = StateField.define<RangeSet<GutterMarker>>({
  create: () => RangeSet.empty,
  update(icons, tr) {
    icons = icons.map(tr.changes)
    for (const e of tr.effects) {
      if (!e.is(setMarks)) continue
      const doc = tr.state.doc
      const worst = new Map<number, LineMark['level']>()
      for (const m of e.value) if (m.line >= 1 && m.line <= doc.lines && worst.get(m.line) !== 'error') worst.set(m.line, m.level)
      icons = RangeSet.of(
        [...worst].sort((a, b) => a[0] - b[0]).map(([line, level]) => new MarkIcon(level).range(doc.line(line).from)),
      )
    }
    return icons
  },
})
const markGutter = gutter({
  class: 'cm-mark-gutter',
  markers: (v) => v.state.field(iconField),
  initialSpacer: () => new MarkIcon('error', true),
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
  '.cm-mark-error': { backgroundColor: 'var(--bad-soft)' },
  '.cm-mark-warning': { backgroundColor: 'var(--warn-soft)' },
  '.cm-mark-gutter .cm-gutterElement': { display: 'flex', alignItems: 'flex-start', justifyContent: 'center', boxSizing: 'border-box', width: '20px', paddingTop: '4px' },
  '.cm-mark-icon': { display: 'inline-flex' },
  '.cm-mark-icon.error': { color: 'var(--bad)' },
  '.cm-mark-icon.warning': { color: 'var(--warn)' },
  '.cm-mark-word': { position: 'absolute', width: '1px', height: '1px', overflow: 'hidden', clip: 'rect(0 0 0 0)', whiteSpace: 'nowrap' },
})

function extensions(): Extension[] {
  return [
    minimalSetup,
    iconField,
    markGutter,
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
