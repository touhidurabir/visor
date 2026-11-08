<script setup lang="ts">
// One section of a config as a card that folds: the header (always shown)
// names the program, its live state, what it does in one sentence, and
// what supervisord says when it is failing; opened, it lists the
// section's lines (ExplainLine). The header's button is what toggles it,
// so the keyboard works; a click anywhere else on the header does too.
import { computed, useId } from 'vue'
import type { EditView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { cardCounts, type Card } from '../lib/explain-cards'
import ExplainLine from './ExplainLine.vue'
import StateText from './StateText.vue'

const props = defineProps<{ card: Card; open: boolean; mode: 'overview' | 'editor'; readOnly: boolean; actions: boolean }>()
const emit = defineEmits<{
  toggle: []
  edit: [edit: EditView]
  add: [after: number, lines: string[]]
  goto: [line: number]
  open: [path: string]
  reveal: [path: string]
  openLog: [path: string]
  viewLog: [process: string, stream: string]
}>()

const id = useId()
const bodyId = `${id}-lines`
const descId = `${id}-desc`
const counts = computed(() => cardCounts(props.card))
const s = computed(() => props.card.section)

/** A click on the header outside the button toggles too, unless it selects text. */
function onHead(e: MouseEvent) {
  if ((e.target as HTMLElement).closest('button, a')) return
  if (window.getSelection()?.toString()) return
  emit('toggle')
}
</script>

<template>
  <article :class="['card', mode, { failing: card.failing, open }]" :data-card="card.key">
    <div class="head" @click="onHead">
      <h3>
        <button type="button" class="toggle" :aria-expanded="open" :aria-controls="bodyId" :aria-describedby="s.Summary || s.Failure ? descId : undefined" @click="emit('toggle')">
          <svg class="chev" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m9 6 6 6-6 6" /></svg>
          <span class="title">
            <span v-if="card.title.kind" class="kind">{{ card.title.kind }}</span>
            <span v-else class="sr">Program </span>
            <span class="name">{{ card.title.name }}</span>
          </span>
          <StateText v-if="card.state" class="state" :state="card.state.state" :status="card.state.status" :label="card.state.label" />
          <span v-if="counts.length" class="counts">
            <span v-for="c in counts" :key="c.text" :class="['badge', c.cls]">{{ c.text }}</span>
          </span>
        </button>
      </h3>
      <p v-if="s.Summary || s.Failure" :id="descId" class="desc">
        <span v-if="s.Summary" class="summary">{{ s.Summary }}</span>
        <span v-if="s.Failure" class="failure t-bad">{{ s.Failure }}</span>
      </p>
    </div>
    <div :id="bodyId" class="body">
      <template v-if="open">
        <ol v-if="card.lines.length" class="rows">
          <ExplainLine
            v-for="l in card.lines"
            :key="l.Kind + l.Line"
            :line="l"
            :mode="mode"
            :read-only="readOnly"
            :actions="actions"
            @edit="(e) => emit('edit', e)"
            @add="(a, ls) => emit('add', a, ls)"
            @goto="(n) => emit('goto', n)"
            @open="(p) => emit('open', p)"
            @reveal="(p) => emit('reveal', p)"
            @open-log="(p) => emit('openLog', p)"
            @view-log="(p, st) => emit('viewLog', p, st)"
          />
        </ol>
      </template>
    </div>
  </article>
</template>

<style scoped>
.card { min-width: 0; border-top: 1px solid var(--line); }
.card.failing { box-shadow: inset 3px 0 0 var(--bad-dot); }
.head { display: flex; flex-direction: column; gap: 3px; padding: 10px 18px 12px 12px; cursor: pointer; }
.head:hover { background: var(--hover); }
.editor .head { padding-right: 16px; }
h3 { margin: 0; font-size: inherit; font-weight: inherit; }
.toggle { display: flex; flex-wrap: wrap; align-items: center; gap: 4px 10px; width: 100%; min-height: var(--control-sm); padding: 0; border: 0; border-radius: var(--radius-sm); background: none; color: var(--ink); font: inherit; text-align: left; cursor: pointer; }
.chev { flex: none; width: 12px; color: var(--ink3); transition: transform 0.12s ease; }
.open .chev { transform: rotate(90deg); }
.title { display: inline-flex; align-items: baseline; gap: 6px; min-width: 0; }
/* The program's name stands out: mono, semibold, in ink. */
.name { font-family: var(--mono); font-size: 13.5px; font-weight: 650; overflow-wrap: anywhere; }
.kind { color: var(--ink3); font-size: 12.5px; font-weight: 600; }
.counts { display: inline-flex; flex-wrap: wrap; gap: 6px; margin-left: auto; }
.counts .badge { height: 20px; font-size: 11.5px; }
.desc { display: flex; flex-direction: column; gap: 2px; margin: 0; padding-left: 22px; line-height: 1.45; user-select: text; }
.summary { color: var(--ink2); }
.failure { font-weight: 600; }
.rows { list-style: none; margin: 0; padding: 0; }
.sr { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0 0 0 0); white-space: nowrap; }
@media (prefers-reduced-motion: reduce) {
  .chev { transition: none; }
}
</style>
