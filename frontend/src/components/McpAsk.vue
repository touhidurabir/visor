<script setup lang="ts">
// Visor asking before an AI tool's change (MCP, M9): "Claude Code wants to
// restart horizon on prod", one question at a time. Deny has focus first;
// Esc or the backdrop denies. No answer within a minute is a Deny (Go).
import { Events } from '@wailsio/runtime'
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { VisorMcpAsk, VisorMcpAskDone } from '../../bindings/events'
import { Answer, Get } from '../../bindings/github.com/touhidurabir/visor/internal/app/mcpservice'
import type { Question } from '../../bindings/github.com/touhidurabir/visor/internal/mcp/models'

const props = defineProps<{ compact?: boolean }>()
const queue = ref<Question[]>([])
const q = computed(() => queue.value[0])
const denyBtn = ref<HTMLButtonElement>()
const allowBtn = ref<HTMLButtonElement>()
let returnFocus: HTMLElement | null = null
// "Allow for 10 minutes" isn't offered where Visor always asks: production
// servers, and KILL.
const canAllow10 = computed(() => !!q.value && !q.value.production && !q.value.force)
const titleId = `mcp-ask-${Math.random().toString(36).slice(2)}`

const drop = (id: string) => (queue.value = queue.value.filter((x) => x.id !== id))

async function answer(allow: boolean, minutes = 0) {
  const cur = q.value
  if (!cur) return
  drop(cur.id)
  try {
    await Answer(cur.id, allow, minutes)
  } catch {
    // Already answered or timed out: nothing left to do.
  }
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    e.preventDefault()
    e.stopPropagation()
    void answer(false)
  } else if (e.key === 'Tab') {
    // Focus stays in the question (Deny first, Allow last).
    if (e.shiftKey && document.activeElement === denyBtn.value) {
      e.preventDefault()
      allowBtn.value?.focus()
    } else if (!e.shiftKey && document.activeElement === allowBtn.value) {
      e.preventDefault()
      denyBtn.value?.focus()
    }
  }
}

watch(q, (cur, prev) => {
  if (cur && !prev) returnFocus = document.activeElement as HTMLElement | null
  if (cur) void nextTick(() => denyBtn.value?.focus())
  else if (prev) returnFocus?.focus?.()
})

let offs: (() => void)[] = []
onMounted(async () => {
  offs = [
    Events.On(VisorMcpAsk, (ev: { data: Question }) => {
      if (!queue.value.some((x) => x.id === ev.data.id)) queue.value = [...queue.value, ev.data]
    }),
    Events.On(VisorMcpAskDone, (ev: { data: string }) => drop(ev.data)),
  ]
  try {
    const waiting = (await Get()).Asks ?? []
    for (const w of waiting) if (!queue.value.some((x) => x.id === w.id)) queue.value = [...queue.value, w]
  } catch {
    // The MCP service isn't there (a test build): nothing to ask.
  }
})
onUnmounted(() => offs.forEach((off) => off()))
</script>

<template>
  <Teleport to="body">
    <div v-if="q" class="scrim" @click.self="answer(false)" @keydown="onKeydown">
      <div :class="['dialog', { compact: props.compact }]" role="alertdialog" aria-modal="true" :aria-labelledby="titleId" data-testid="mcp-ask">
        <div class="head">
          <h2 :id="titleId">{{ q.client }} wants to {{ q.verb }} <span class="mono">{{ q.subject }}</span> on {{ q.machine }}</h2>
          <span v-if="q.production" class="badge b-bad">Production</span>
        </div>
        <p>Asked through MCP. If nobody answers within a minute, Visor says no.<template v-if="q.production"> Production servers always ask.</template><template v-if="queue.length > 1"> {{ queue.length - 1 }} more waiting.</template></p>
        <div class="actions">
          <button ref="denyBtn" type="button" class="btn" @click="answer(false)">Deny</button>
          <button v-if="canAllow10" type="button" class="btn" @click="answer(true, 10)">{{ props.compact ? '10 min' : 'Allow for 10 minutes' }}</button>
          <button ref="allowBtn" type="button" class="btn primary" @click="answer(true)">Allow</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.scrim { position: fixed; inset: 0; z-index: 30; display: flex; align-items: center; justify-content: center; background: var(--scrim); }
.dialog { width: min(440px, calc(100vw - 48px)); padding: 20px; border-radius: var(--radius-lg); background: var(--menu-bg); color: var(--text); border: 1px solid var(--line-strong); box-shadow: var(--menu-shadow); user-select: text; }
.dialog.compact { width: calc(100vw - 24px); padding: 14px; }
.head { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; margin: 0 0 8px; }
h2 { margin: 0; font-size: 15px; font-weight: 650; line-height: 1.35; }
.compact h2 { font-size: 14px; }
.mono { font-family: var(--mono); font-size: 0.95em; }
p { margin: 0 0 16px; color: var(--text-muted); line-height: 1.45; }
.actions { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 8px; }
</style>
