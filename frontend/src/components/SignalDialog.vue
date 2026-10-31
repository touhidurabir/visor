<script setup lang="ts">
// Pick a signal to send to one process. Esc or the backdrop cancels.
import { nextTick, onMounted, ref } from 'vue'

const props = defineProps<{ process: string }>()
const emit = defineEmits<{ send: [sig: string]; cancel: [] }>()

const SIGNALS = [
  { name: 'HUP', hint: 'Reload; most daemons re-read their config' },
  { name: 'USR1', hint: 'User-defined; often reopens log files' },
  { name: 'USR2', hint: 'User-defined' },
  { name: 'INT', hint: 'Interrupt, like Ctrl+C' },
  { name: 'QUIT', hint: 'Quit and dump core' },
  { name: 'TERM', hint: 'Ask the process to stop' },
  { name: 'KILL', hint: 'Force stop; the process cannot clean up' },
]
const selected = ref('HUP')
const sel = ref<HTMLElement>()
onMounted(() => void nextTick(() => sel.value?.querySelector<HTMLInputElement>('input:checked')?.focus()))

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    e.preventDefault()
    e.stopPropagation()
    emit('cancel')
  }
}
</script>

<template>
  <Teleport to="body">
    <div class="scrim" @click.self="emit('cancel')" @keydown="onKeydown">
      <form class="dialog" role="dialog" aria-modal="true" aria-labelledby="sig-title" @submit.prevent="emit('send', selected)">
        <h2 id="sig-title">Send signal</h2>
        <p>Choose a signal to send to <code>{{ props.process }}</code>.</p>
        <div ref="sel" class="list" role="radiogroup" aria-labelledby="sig-title">
          <label v-for="s in SIGNALS" :key="s.name" :class="{ on: selected === s.name }">
            <input v-model="selected" type="radio" name="signal" :value="s.name" />
            <code>{{ s.name }}</code>
            <span>{{ s.hint }}</span>
          </label>
        </div>
        <div class="actions">
          <button type="button" class="btn" @click="emit('cancel')">Cancel</button>
          <button type="submit" class="btn primary">Send signal</button>
        </div>
      </form>
    </div>
  </Teleport>
</template>

<style scoped>
.scrim { position: fixed; inset: 0; z-index: 20; display: flex; align-items: center; justify-content: center; background: var(--scrim); }
.dialog {
  width: min(440px, calc(100vw - 48px));
  padding: 20px;
  border-radius: var(--radius-lg);
  background: var(--menu-bg);
  color: var(--text);
  border: 1px solid var(--line-strong);
  box-shadow: var(--menu-shadow);
}
h2 { margin: 0 0 8px; font-size: 15px; font-weight: 650; }
p { margin: 0 0 12px; color: var(--text-muted); }
code { font-family: var(--font-mono); font-size: 12px; }
.list { display: flex; flex-direction: column; gap: 2px; margin-bottom: 18px; }
label { display: flex; align-items: center; gap: 10px; min-height: 32px; padding: 0 8px; border-radius: 6px; cursor: pointer; }
label:hover { background: var(--bg-hover); }
label.on { background: var(--bg-active); }
label code { width: 48px; font-weight: 600; }
label span { color: var(--text-muted); font-size: 12px; }
.actions { display: flex; justify-content: flex-end; gap: 8px; }
.btn { min-height: 32px; padding: 0 14px; border-radius: var(--radius-sm); border: 1px solid var(--line-strong); background: var(--bg); color: var(--text); font: inherit; cursor: pointer; }
.btn:hover { background: var(--bg-hover); }
.btn.primary { background: var(--accent); border-color: var(--accent); color: var(--on-accent); font-weight: 600; }
.btn.primary:hover { background: var(--accent-hover); }
</style>
